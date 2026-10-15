package leader

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
)

var (
	// ErrInvalidMessage means the bytes are not one JSON-RPC message object.
	ErrInvalidMessage = errors.New("leader: invalid JSON-RPC message")
	// ErrInvalidID means a request id is not a number or a string.
	ErrInvalidID = errors.New("leader: invalid JSON-RPC id")
	// ErrInvalidClientID means a client id has a character outside letters, digits, "-" and "_".
	// The leader makes client ids itself. This rule keeps them the same in every JSON escaping.
	ErrInvalidClientID = errors.New("leader: invalid client id")
	// ErrDuplicateID means the client already has an active request with this id.
	ErrDuplicateID = errors.New("leader: duplicate active request id")
)

// Kind tells what a message is.
type Kind int

const (
	// KindRequest has a method and an id.
	KindRequest Kind = iota + 1
	// KindNotification has a method and no id.
	KindNotification
	// KindResponse has no method.
	KindResponse
)

// Outcome is the result of Forward.
type Outcome struct {
	Kind   Kind
	Method string
	// Msg is the message to send to the agent. It is empty when Drop is true.
	Msg json.RawMessage
	// Drop is true when the message must not reach the agent.
	Drop bool
}

// Restored is a response that went back through the table.
type Restored struct {
	ClientID string
	Method   string
	// Msg is the response with the original id bytes put back.
	Msg json.RawMessage
	// Dropped is true when the client left before the response arrived.
	Dropped bool
	// Tag is the value given to ForwardTagged.
	Tag any
}

type pendingID struct {
	clientID string
	method   string
	original json.RawMessage
	canon    string
	internal string
	tag      any
	dropped  bool
}

// IDTable gives every client request a leader-wide id and restores the client id
// on the response. The leader makes the internal id, never the client.
type IDTable struct {
	mu       sync.Mutex
	seq      uint64
	pending  map[string]*pendingID        // internal id (JSON text) -> route
	byClient map[string]map[string]string // client -> canonical id -> internal id
}

// NewIDTable returns an empty table.
func NewIDTable() *IDTable {
	return &IDTable{pending: map[string]*pendingID{}, byClient: map[string]map[string]string{}}
}

// Pending returns the number of active requests of a client.
func (t *IDTable) Pending(clientID string) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.byClient[clientID])
}

// hasPending includes requests whose callers disconnected before the host answered.
func (t *IDTable) hasPending() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.pending) != 0
}

// Forward prepares a client message for the agent. A request gets an internal
// id. A cancel notification is rewritten through the sender's own requests only.
// Other messages pass unchanged.
func (t *IDTable) Forward(clientID string, msg json.RawMessage) (Outcome, error) {
	return t.ForwardTagged(clientID, msg, nil)
}

// ForwardTagged is Forward with a value that Restore returns with the response.
// The router uses it to remember what a request was for.
func (t *IDTable) ForwardTagged(clientID string, msg json.RawMessage, tag any) (Outcome, error) {
	if !validClientID(clientID) {
		return Outcome{}, ErrInvalidClientID
	}
	m, err := decodeObject(msg)
	if err != nil {
		return Outcome{}, err
	}
	method, hasMethod, err := methodOf(m)
	if err != nil {
		return Outcome{}, err
	}
	id, hasID := m["id"]
	switch {
	case !hasMethod:
		return Outcome{Kind: KindResponse, Msg: msg}, nil
	case !hasID:
		if method == "$/cancel_request" {
			return t.forwardCancel(clientID, m)
		}
		return Outcome{Kind: KindNotification, Method: method, Msg: msg}, nil
	}
	canon, err := canonicalID(id)
	if err != nil {
		return Outcome{}, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, busy := t.byClient[clientID][canon]; busy {
		return Outcome{}, fmt.Errorf("%w: %s", ErrDuplicateID, canon)
	}
	t.seq++
	internal := strconv.Quote(clientID + ":" + strconv.FormatUint(t.seq, 10))
	t.pending[internal] = &pendingID{clientID: clientID, method: method, original: bytes.Clone(id), canon: canon, internal: internal, tag: tag}
	if t.byClient[clientID] == nil {
		t.byClient[clientID] = map[string]string{}
	}
	t.byClient[clientID][canon] = internal
	m["id"] = json.RawMessage(internal)
	out, err := encodeObject(m)
	if err != nil {
		t.releaseRoute(t.pending[internal])
		return Outcome{}, err
	}
	return Outcome{Kind: KindRequest, Method: method, Msg: out}, nil
}

func (t *IDTable) forwardCancel(clientID string, m map[string]json.RawMessage) (Outcome, error) {
	drop := Outcome{Kind: KindNotification, Method: "$/cancel_request", Drop: true}
	params, err := decodeObject(m["params"])
	if err != nil || hasFoldDuplicate(params) {
		// The SDK reads requestId without regard to case and takes the last key.
		return drop, nil
	}
	canon, err := canonicalID(params["requestId"])
	if err != nil {
		return drop, nil
	}
	t.mu.Lock()
	internal, ok := t.byClient[clientID][canon]
	t.mu.Unlock()
	if !ok {
		return drop, nil
	}
	params["requestId"] = json.RawMessage(internal)
	if m["params"], err = encodeObject(params); err != nil {
		return Outcome{}, err
	}
	out, err := encodeObject(m)
	if err != nil {
		return Outcome{}, err
	}
	return Outcome{Kind: KindNotification, Method: "$/cancel_request", Msg: out}, nil
}

// Restore puts the original id bytes back on an agent response and ends the
// route. It reports false for a message that is not a response to a known request.
func (t *IDTable) Restore(msg json.RawMessage) (Restored, bool) {
	m, err := decodeObject(msg)
	if err != nil {
		return Restored{}, false
	}
	if _, hasMethod := m["method"]; hasMethod {
		return Restored{}, false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	route, ok := t.pending[string(m["id"])]
	if !ok {
		return Restored{}, false
	}
	m["id"] = route.original
	out, err := encodeObject(m)
	if err != nil {
		return Restored{}, false
	}
	t.releaseRoute(route)
	return Restored{ClientID: route.clientID, Method: route.method, Msg: out, Dropped: route.dropped, Tag: route.tag}, true
}

// DropClient frees the ids of a client. A response that arrives later is still
// returned by Restore with Dropped set, so the router can release what the
// request created (a session, a Follow stream).
func (t *IDTable) DropClient(clientID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, internal := range t.byClient[clientID] {
		if route := t.pending[internal]; route != nil {
			route.dropped = true
		}
	}
	delete(t.byClient, clientID)
}

// releaseRoute removes a route. The caller holds the lock.
func (t *IDTable) releaseRoute(route *pendingID) {
	delete(t.pending, route.internal)
	if route.dropped {
		return
	}
	if ids := t.byClient[route.clientID]; ids != nil {
		delete(ids, route.canon)
		if len(ids) == 0 {
			delete(t.byClient, route.clientID)
		}
	}
}

func validClientID(id string) bool {
	if id == "" {
		return false
	}
	for _, c := range id {
		letter := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
		if !letter && (c < '0' || c > '9') && c != '-' && c != '_' {
			return false
		}
	}
	return true
}

func decodeObject(raw []byte) (map[string]json.RawMessage, error) {
	trimmed := bytes.TrimLeft(raw, " \t\r\n")
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, ErrInvalidMessage
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidMessage, err)
	}
	return m, nil
}

// encodeObject writes the object without HTML escaping, so the bytes of the
// values stay as they came.
func encodeObject(m map[string]json.RawMessage) (json.RawMessage, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(m); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

func methodOf(m map[string]json.RawMessage) (string, bool, error) {
	raw, ok := m["method"]
	if !ok {
		return "", false, nil
	}
	var method string
	if err := json.Unmarshal(raw, &method); err != nil {
		return "", false, fmt.Errorf("%w: method is not a string", ErrInvalidMessage)
	}
	return method, true, nil
}

// canonicalID returns one key for every spelling of the same id. Numbers that
// name the same value (1, 1.0, 1e0) share a key. A string and a number never do.
func canonicalID(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", ErrInvalidID
	}
	switch raw[0] {
	case '"':
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return "", ErrInvalidID
		}
		return "s:" + s, nil
	case '-', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		return canonicalNumber(string(raw))
	default:
		return "", ErrInvalidID
	}
}

// canonicalNumber normalizes a JSON number with string operations only. A huge
// exponent never allocates memory.
func canonicalNumber(text string) (string, error) {
	neg := strings.HasPrefix(text, "-")
	text = strings.TrimPrefix(text, "-")
	mantissa, expText, hasExp := strings.Cut(strings.ToLower(text), "e")
	intPart, frac, _ := strings.Cut(mantissa, ".")
	var exp int64
	if hasExp {
		e, err := strconv.ParseInt(strings.TrimPrefix(expText, "+"), 10, 32)
		if err != nil {
			// The exponent does not fit. The text is the key, with its sign.
			if neg {
				return "n:raw:-" + text, nil
			}
			return "n:raw:" + text, nil
		}
		exp = e
	}
	digits := strings.TrimLeft(intPart+frac, "0")
	exp -= int64(len(frac))
	trimmed := strings.TrimRight(digits, "0")
	exp += int64(len(digits) - len(trimmed))
	if trimmed == "" {
		return "n:0", nil
	}
	sign := ""
	if neg {
		sign = "-"
	}
	return "n:" + sign + trimmed + "e" + strconv.FormatInt(exp, 10), nil
}
