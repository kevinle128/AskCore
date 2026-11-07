package settings

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"strconv"
)

// ModelsFile is the decoded user models.json. It holds only the entries that
// passed the checks; each dropped entry has a Diagnostic. The values are read
// only: a caller must not change the maps or slices.
type ModelsFile struct {
	// Providers maps a provider id (store rule: a-z, 0-9 and -) to its entry.
	Providers map[string]ProviderEntry
}

// ProviderEntry is one entry of "providers". An empty string is an absent
// field. APIKey and the header values are raw strings: this package does not
// interpret a "$" or "!" prefix.
type ProviderEntry struct {
	// Position is the place of the entry in the "providers" object, counted
	// with the entries that were dropped, so a problem path can name it.
	Position int
	BaseURL  string
	API      string
	APIKey   string
	Headers  map[string]string
	// Compat is the raw compat object. Its keys depend on the API, so the
	// mapping to a provider type decodes it.
	Compat         map[string]json.RawMessage
	Models         []ModelEntry
	ModelOverrides map[string]ModelOverride
}

// ModelOverride is a set of model fields. An entry of "modelOverrides" has
// these fields only; a row of "models" has them and an id. A zero or nil field
// is absent.
type ModelOverride struct {
	Name      string
	API       string
	Reasoning *bool
	// ThinkingLevelMap maps a thinking level to the provider effort word, or
	// to nil when the model does not support that level.
	ThinkingLevelMap map[string]*string
	// Input holds "text" and "image" only.
	Input         []string
	Cost          *CostEntry
	ContextWindow int
	MaxTokens     int
	Headers       map[string]string
	Compat        map[string]json.RawMessage
}

// ModelEntry is one row of "models".
type ModelEntry struct {
	// Position is the place of the row in "models", counted with the rows
	// that were dropped.
	Position int
	ID       string
	ModelOverride
}

// CostEntry holds the prices of a model. A nil field is absent.
type CostEntry struct {
	Input      *float64
	Output     *float64
	CacheRead  *float64
	CacheWrite *float64
}

// thinkingLevels are the protocol thinking levels. The authority is the
// ThinkingLevel constants of pkg/protocol, which this package cannot import.
var thinkingLevels = []string{"off", "minimal", "low", "medium", "high", "xhigh", "max"}

var (
	modelsFileKeys    = []string{"providers"}
	providerEntryKeys = []string{"baseUrl", "api", "apiKey", "headers", "compat", "models", "modelOverrides"}
	overrideKeys      = []string{"name", "api", "reasoning", "thinkingLevelMap", "input", "cost", "contextWindow", "maxTokens", "headers", "compat"}
	modelEntryKeys    = append([]string{"id"}, overrideKeys...)
	costEntryKeys     = []string{"input", "output", "cacheRead", "cacheWrite"}
)

type modelsDecoder struct {
	path     string
	builtIn  map[string]bool
	problems []Diagnostic
}

// errEntryDropped marks a value problem that drops the entry that holds it.
// The Diagnostic is already recorded.
var errEntryDropped = errors.New("entry dropped")

// parseModelsFile decodes the bytes of a models.json file. A file problem (not
// valid JSONC, or a top level that is not an object) is returned as an error
// whose text holds no file content. Entry problems are Diagnostics; their
// entries are dropped and the rest of the file loads.
func parseModelsFile(data []byte, path string, builtIn map[string]bool) (ModelsFile, []Diagnostic, error) {
	clean, err := stripJSONC(data)
	if err != nil {
		return ModelsFile{}, nil, err
	}
	root, err := decodeJSONTree(clean)
	if err != nil {
		return ModelsFile{}, nil, err
	}
	if root.kind != nodeObject {
		return ModelsFile{}, nil, errors.New("not a JSON object")
	}
	d := &modelsDecoder{path: path, builtIn: builtIn}
	file := ModelsFile{Providers: map[string]ProviderEntry{}}
	d.unknownKeys(root, "", modelsFileKeys)
	providers := root.field("providers")
	if providers == nil || providers.kind == nodeNull {
		return file, d.problems, nil
	}
	if providers.kind != nodeObject {
		d.add("providers", "must be an object; the providers are ignored")
		return file, d.problems, nil
	}
	for i, id := range providers.keys {
		key := fmt.Sprintf("providers[%d]", i)
		entry, err := d.provider(providers.vals[i], id, key)
		if err != nil {
			continue
		}
		entry.Position = i
		file.Providers[id] = entry
	}
	return file, d.problems, nil
}

func (d *modelsDecoder) add(key, text string) {
	d.problems = append(d.problems, Diagnostic{Layer: LayerUser, Path: d.path, Key: key, Text: text})
}

func (d *modelsDecoder) drop(key, text, entry string) error {
	d.add(key, text+"; the "+entry+" is ignored")
	return errEntryDropped
}

// unknownKeys gives one problem for each key of obj that is not in known. The
// Key is the path of obj, because the unknown name is user text.
func (d *modelsDecoder) unknownKeys(obj *jsonNode, key string, known []string) {
	for _, name := range obj.keys {
		if contains(known, name) {
			continue
		}
		text := "unknown key; it is ignored"
		if s := nearestKey(name, known); s != "" {
			text += "; did you mean " + s + "?"
		}
		d.add(key, text)
	}
}

func (d *modelsDecoder) provider(n *jsonNode, id, key string) (ProviderEntry, error) {
	var p ProviderEntry
	if !validProvider(id) {
		return p, d.drop(key, "provider id must use only a-z, 0-9 and -", "provider")
	}
	if n.kind != nodeObject {
		return p, d.drop(key, "must be an object", "provider")
	}
	d.unknownKeys(n, key, providerEntryKeys)
	var err error
	if p.BaseURL, err = d.str(n, key, "baseUrl", "provider"); err != nil {
		return p, err
	}
	if p.BaseURL != "" && !validBaseURL(p.BaseURL) {
		return p, d.drop(key+".baseUrl", "baseUrl must be an http or https URL with a host and no user name or password", "provider")
	}
	if p.API, err = d.str(n, key, "api", "provider"); err != nil {
		return p, err
	}
	if p.APIKey, err = d.str(n, key, "apiKey", "provider"); err != nil {
		return p, err
	}
	if p.Headers, err = d.headers(n, key, "provider"); err != nil {
		return p, err
	}
	if p.Compat, err = d.compat(n, key, "provider"); err != nil {
		return p, err
	}
	declaredModels := 0
	if models := n.field("models"); models != nil && models.kind != nodeNull {
		if models.kind != nodeArray {
			return p, d.drop(key+".models", "must be an array", "provider")
		}
		declaredModels = len(models.items)
		for i, item := range models.items {
			m, err := d.model(item, fmt.Sprintf("%s.models[%d]", key, i), p.API)
			if err == nil {
				m.Position = i
				p.Models = append(p.Models, m)
			}
		}
	}
	if overrides := n.field("modelOverrides"); overrides != nil && overrides.kind != nodeNull {
		if overrides.kind != nodeObject {
			return p, d.drop(key+".modelOverrides", "must be an object", "provider")
		}
		p.ModelOverrides = map[string]ModelOverride{}
		for i, modelID := range overrides.keys {
			okey := fmt.Sprintf("%s.modelOverrides[%d]", key, i)
			if modelID == "" {
				d.add(okey, "model id must not be empty; the override is ignored")
				continue
			}
			o, err := d.override(overrides.vals[i], okey, "override", overrideKeys)
			if err == nil {
				p.ModelOverrides[modelID] = o
			}
		}
	}
	if declaredModels > 0 {
		if !d.builtIn[id] {
			if p.BaseURL == "" {
				return p, d.drop(key+".baseUrl", "baseUrl is required for the models of a provider that is not built in", "provider")
			}
			if p.APIKey == "" {
				return p, d.drop(key+".apiKey", "apiKey is required for the models of a provider that is not built in", "provider")
			}
		}
	} else if !hasFieldOtherThanModels(n) {
		return p, d.drop(key, "a provider without models needs one more field", "provider")
	}
	return p, nil
}

// validBaseURL rejects a URL that would fail only at the first request, and a
// URL with user info, which is a secret that logs of the endpoint would show.
func validBaseURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && u.User == nil
}

func hasFieldOtherThanModels(n *jsonNode) bool {
	for i, name := range n.keys {
		if name != "models" && contains(providerEntryKeys, name) && n.vals[i].kind != nodeNull {
			return true
		}
	}
	return false
}

// model decodes one row of "models". providerAPI is the api of the provider;
// a row needs an api on the row or on the provider.
func (d *modelsDecoder) model(n *jsonNode, key, providerAPI string) (ModelEntry, error) {
	var m ModelEntry
	o, err := d.override(n, key, "model", modelEntryKeys)
	if err != nil {
		return m, err
	}
	m.ModelOverride = o
	if m.ID, err = d.str(n, key, "id", "model"); err != nil {
		return m, err
	}
	if m.ID == "" {
		return m, d.drop(key+".id", "id is required", "model")
	}
	if m.API == "" && providerAPI == "" {
		return m, d.drop(key+".api", "api is required on the model or on the provider", "model")
	}
	return m, nil
}

// override decodes the model fields of n. entry names what a problem drops.
func (d *modelsDecoder) override(n *jsonNode, key, entry string, known []string) (ModelOverride, error) {
	var o ModelOverride
	if n.kind != nodeObject {
		return o, d.drop(key, "must be an object", entry)
	}
	d.unknownKeys(n, key, known)
	var err error
	if o.Name, err = d.str(n, key, "name", entry); err != nil {
		return o, err
	}
	if o.API, err = d.str(n, key, "api", entry); err != nil {
		return o, err
	}
	if v := n.field("reasoning"); v != nil && v.kind != nodeNull {
		if v.kind != nodeBool {
			return o, d.drop(key+".reasoning", "must be true or false", entry)
		}
		b := v.b
		o.Reasoning = &b
	}
	if o.ThinkingLevelMap, err = d.thinkingMap(n, key, entry); err != nil {
		return o, err
	}
	if o.Input, err = d.input(n, key, entry); err != nil {
		return o, err
	}
	if o.Cost, err = d.cost(n, key, entry); err != nil {
		return o, err
	}
	if o.ContextWindow, err = d.positiveInt(n, key, "contextWindow", entry); err != nil {
		return o, err
	}
	if o.MaxTokens, err = d.positiveInt(n, key, "maxTokens", entry); err != nil {
		return o, err
	}
	if o.Headers, err = d.headers(n, key, entry); err != nil {
		return o, err
	}
	if o.Compat, err = d.compat(n, key, entry); err != nil {
		return o, err
	}
	return o, nil
}

// str reads an optional non-empty string field. Absent and null give "".
func (d *modelsDecoder) str(n *jsonNode, key, name, entry string) (string, error) {
	v := n.field(name)
	if v == nil || v.kind == nodeNull {
		return "", nil
	}
	if v.kind != nodeString {
		return "", d.drop(key+"."+name, "must be a string", entry)
	}
	if v.str == "" {
		return "", d.drop(key+"."+name, "must not be empty", entry)
	}
	return v.str, nil
}

func (d *modelsDecoder) positiveInt(n *jsonNode, key, name, entry string) (int, error) {
	v := n.field(name)
	if v == nil || v.kind == nodeNull {
		return 0, nil
	}
	if v.kind == nodeNumber {
		if i, err := strconv.ParseInt(string(v.num), 10, 64); err == nil && i > 0 && i <= math.MaxInt32 {
			return int(i), nil
		}
	}
	return 0, d.drop(key+"."+name, "must be a positive integer", entry)
}

func (d *modelsDecoder) headers(n *jsonNode, key, entry string) (map[string]string, error) {
	v := n.field("headers")
	if v == nil || v.kind == nodeNull {
		return nil, nil
	}
	if v.kind != nodeObject {
		return nil, d.drop(key+".headers", "must be an object", entry)
	}
	out := make(map[string]string, len(v.keys))
	for i, name := range v.keys {
		hkey := fmt.Sprintf("%s.headers[%d]", key, i)
		if name == "" {
			return nil, d.drop(hkey, "header name must not be empty", entry)
		}
		if v.vals[i].kind != nodeString {
			return nil, d.drop(hkey, "header value must be a string", entry)
		}
		out[name] = v.vals[i].str
	}
	return out, nil
}

func (d *modelsDecoder) compat(n *jsonNode, key, entry string) (map[string]json.RawMessage, error) {
	v := n.field("compat")
	if v == nil || v.kind == nodeNull {
		return nil, nil
	}
	if v.kind != nodeObject {
		return nil, d.drop(key+".compat", "must be an object", entry)
	}
	out := make(map[string]json.RawMessage, len(v.keys))
	for i, name := range v.keys {
		out[name] = v.vals[i].raw
	}
	return out, nil
}

func (d *modelsDecoder) thinkingMap(n *jsonNode, key, entry string) (map[string]*string, error) {
	v := n.field("thinkingLevelMap")
	if v == nil || v.kind == nodeNull {
		return nil, nil
	}
	if v.kind != nodeObject {
		return nil, d.drop(key+".thinkingLevelMap", "must be an object", entry)
	}
	out := make(map[string]*string, len(v.keys))
	for i, level := range v.keys {
		lkey := fmt.Sprintf("%s.thinkingLevelMap[%d]", key, i)
		if !contains(thinkingLevels, level) {
			return nil, d.drop(lkey, "key must be a thinking level (off, minimal, low, medium, high, xhigh, max)", entry)
		}
		switch val := v.vals[i]; val.kind {
		case nodeNull:
			out[level] = nil
		case nodeString:
			s := val.str
			out[level] = &s
		default:
			return nil, d.drop(lkey, "value must be a string or null", entry)
		}
	}
	return out, nil
}

func (d *modelsDecoder) input(n *jsonNode, key, entry string) ([]string, error) {
	v := n.field("input")
	if v == nil || v.kind == nodeNull {
		return nil, nil
	}
	if v.kind != nodeArray {
		return nil, d.drop(key+".input", "must be an array", entry)
	}
	out := make([]string, 0, len(v.items))
	for i, item := range v.items {
		if item.kind != nodeString || (item.str != "text" && item.str != "image") {
			return nil, d.drop(fmt.Sprintf("%s.input[%d]", key, i), "must be text or image", entry)
		}
		out = append(out, item.str)
	}
	return out, nil
}

func (d *modelsDecoder) cost(n *jsonNode, key, entry string) (*CostEntry, error) {
	v := n.field("cost")
	if v == nil || v.kind == nodeNull {
		return nil, nil
	}
	ckey := key + ".cost"
	if v.kind != nodeObject {
		return nil, d.drop(ckey, "must be an object", entry)
	}
	d.unknownKeys(v, ckey, costEntryKeys)
	c := &CostEntry{}
	for _, f := range []struct {
		name string
		dst  **float64
	}{{"input", &c.Input}, {"output", &c.Output}, {"cacheRead", &c.CacheRead}, {"cacheWrite", &c.CacheWrite}} {
		p := v.field(f.name)
		if p == nil || p.kind == nodeNull {
			continue
		}
		var price float64
		var err error
		if p.kind == nodeNumber {
			price, err = strconv.ParseFloat(string(p.num), 64)
		}
		if p.kind != nodeNumber || err != nil || price < 0 || math.IsInf(price, 0) {
			return nil, d.drop(ckey+"."+f.name, "must be a number that is not negative", entry)
		}
		*f.dst = &price
	}
	return c, nil
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// nearestKey returns the schema key within two edits of name, or "". The
// result is a schema name, never user text.
func nearestKey(name string, known []string) string {
	best, bestDist := "", 3
	for _, k := range known {
		if dist := editDistance(name, k, bestDist); dist < bestDist {
			best, bestDist = k, dist
		}
	}
	return best
}

// editDistance returns the Levenshtein distance of a and b, or limit when the
// distance is limit or more.
func editDistance(a, b string, limit int) int {
	ra, rb := []rune(a), []rune(b)
	if d := len(ra) - len(rb); d >= limit || -d >= limit {
		return limit
	}
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		rowMin := cur[0]
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
			rowMin = min(rowMin, cur[j])
		}
		if rowMin >= limit {
			return limit
		}
		prev, cur = cur, prev
	}
	return min(prev[len(rb)], limit)
}

type nodeKind int

const (
	nodeNull nodeKind = iota
	nodeBool
	nodeNumber
	nodeString
	nodeArray
	nodeObject
)

// jsonNode is a decoded JSON value that keeps the order of object keys, so a
// problem can name an entry by its position. For a duplicate key the last
// value wins, as in encoding/json.
type jsonNode struct {
	kind  nodeKind
	b     bool
	num   json.Number
	str   string
	items []*jsonNode
	keys  []string
	vals  []*jsonNode
	// raw is the JSON text of the value, kept for the compat values.
	raw json.RawMessage
}

func (n *jsonNode) field(key string) *jsonNode {
	for i := len(n.keys) - 1; i >= 0; i-- {
		if n.keys[i] == key {
			return n.vals[i]
		}
	}
	return nil
}

// decodeJSONTree decodes one JSON value that fills data. The error text gives
// a line number and no content.
func decodeJSONTree(data []byte) (*jsonNode, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	root, err := decodeNode(dec, data)
	if err != nil {
		return nil, jsonProblem(data, dec, err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("invalid JSON at line %d: data after the top value", lineAt(data, int(dec.InputOffset())))
	}
	return root, nil
}

func jsonProblem(data []byte, dec *json.Decoder, err error) error {
	offset := int(dec.InputOffset())
	var syntax *json.SyntaxError
	if errors.As(err, &syntax) {
		offset = int(syntax.Offset)
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return fmt.Errorf("invalid JSON at line %d: unexpected end of file", lineAt(data, len(data)))
	}
	return fmt.Errorf("invalid JSON at line %d", lineAt(data, offset))
}

func decodeNode(dec *json.Decoder, data []byte) (*jsonNode, error) {
	start := dec.InputOffset()
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	n, err := nodeFromToken(dec, data, tok)
	if err != nil {
		return nil, err
	}
	n.raw = bytes.TrimSpace(data[start:dec.InputOffset()])
	if len(n.raw) > 0 && (n.raw[0] == ',' || n.raw[0] == ':') {
		n.raw = bytes.TrimSpace(n.raw[1:])
	}
	return n, nil
}

func nodeFromToken(dec *json.Decoder, data []byte, tok json.Token) (*jsonNode, error) {
	switch v := tok.(type) {
	case nil:
		return &jsonNode{kind: nodeNull}, nil
	case bool:
		return &jsonNode{kind: nodeBool, b: v}, nil
	case json.Number:
		return &jsonNode{kind: nodeNumber, num: v}, nil
	case string:
		return &jsonNode{kind: nodeString, str: v}, nil
	case json.Delim:
		switch v {
		case '[':
			n := &jsonNode{kind: nodeArray}
			for dec.More() {
				item, err := decodeNode(dec, data)
				if err != nil {
					return nil, err
				}
				n.items = append(n.items, item)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return n, nil
		case '{':
			n := &jsonNode{kind: nodeObject}
			for dec.More() {
				keyTok, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key, ok := keyTok.(string)
				if !ok {
					return nil, errors.New("object key is not a string")
				}
				val, err := decodeNode(dec, data)
				if err != nil {
					return nil, err
				}
				n.keys = append(n.keys, key)
				n.vals = append(n.vals, val)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return n, nil
		}
	}
	return nil, errors.New("unexpected JSON token")
}
