// Package cassette records the HTTP traffic of an LLM provider to a YAML file
// and replays it later, so tests run the real provider code without a live
// model.
//
// Replay serves the recorded responses in order. Request n must have the
// same method, URL and body as recorded request n; a JSON body is compared with
// its keys sorted. A request that does not match fails with a diff of the
// two bodies. Record sends every request to the real server and saves the
// cassette after each response, without auth or volatile headers.
package cassette

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/pmezard/go-difflib/difflib"
	vcr "gopkg.in/dnaeon/go-vcr.v4/pkg/cassette"
	"gopkg.in/dnaeon/go-vcr.v4/pkg/recorder"
)

// Mode selects what a Recorder does with a request.
type Mode int

const (
	// Replay serves the recorded responses and never calls the server.
	Replay Mode = iota
	// Record calls the server and saves every exchange to the cassette.
	Record
)

const rerecordHint = "re-record it with ASK_RECORD=1"

// Headers kept in a saved cassette. Every other header is dropped, so no
// credential or per-request id reaches the file.
var (
	keptRequestHeaders  = []string{"Content-Type", "Anthropic-Version", "Anthropic-Beta"}
	keptResponseHeaders = []string{"Content-Type"}
)

// Option changes a Recorder.
type Option func(*config)

type config struct {
	budget int
	real   http.RoundTripper
}

// WithBudget limits Record to n requests. The request after the limit fails
// and is not sent. Zero means no limit.
func WithBudget(n int) Option { return func(c *config) { c.budget = n } }

// WithRealTransport sets the transport that Record sends requests with. The
// default is http.DefaultTransport.
func WithRealTransport(rt http.RoundTripper) Option { return func(c *config) { c.real = rt } }

// Recorder is an http.RoundTripper that records or replays one cassette. It
// is safe for concurrent use.
type Recorder struct {
	mode   Mode
	path   string
	budget int
	vcr    *recorder.Recorder

	mu     sync.Mutex
	want   []*vcr.Interaction // Replay: the interactions in the file
	served int                // requests answered so far
	err    error              // first failure
	sent   string             // Record: body of the request in flight
}

// New opens the cassette at path, which must end in ".yaml". Replay needs an
// existing file. Record removes any file at path first, so a recording that
// fails early leaves no stale cassette, and starts an empty one.
func New(path string, mode Mode, opts ...Option) (*Recorder, error) {
	cfg := config{real: http.DefaultTransport}
	for _, o := range opts {
		o(&cfg)
	}
	name, ok := strings.CutSuffix(path, ".yaml")
	if !ok {
		return nil, fmt.Errorf("cassette %s: the path must end in .yaml", path)
	}
	r := &Recorder{mode: mode, path: path, budget: cfg.budget}

	vcrMode := recorder.ModeRecordOnly
	if mode == Record {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("cassette %s: %w", path, err)
		}
	}
	if mode == Replay {
		vcrMode = recorder.ModeReplayOnly
		c, err := vcr.Load(name)
		if err != nil {
			return nil, fmt.Errorf("cassette %s: %w; %s", path, err, rerecordHint)
		}
		r.want = c.Interactions
	}
	rec, err := recorder.New(name,
		recorder.WithMode(vcrMode),
		recorder.WithRealTransport(cfg.real),
		recorder.WithSkipRequestLatency(true),
		// RoundTrip checks request n against interaction n before go-vcr
		// sees it, and go-vcr serves the first unreplayed interaction, so
		// any request matches here.
		recorder.WithMatcher(func(*http.Request, vcr.Request) bool { return true }),
		recorder.WithHook(r.capture, recorder.AfterCaptureHook),
	)
	if err != nil {
		return nil, fmt.Errorf("cassette %s: %w", path, err)
	}
	r.vcr = rec
	return r, nil
}

// RoundTrip answers req from the cassette in Replay, or from the server in
// Record.
func (r *Recorder) RoundTrip(req *http.Request) (*http.Response, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return nil, r.err
	}
	n := r.served + 1
	if r.mode == Record && r.budget > 0 && n > r.budget {
		return nil, r.fail(fmt.Errorf("cassette %s: record budget of %d requests is used up; request %d was not sent", r.path, r.budget, n))
	}
	if r.mode == Replay && r.served >= len(r.want) {
		return nil, r.fail(fmt.Errorf("cassette %s: request %d is not recorded; the cassette has %d request(s); %s", r.path, n, len(r.want), rerecordHint))
	}

	body, err := readBody(req)
	if err != nil {
		return nil, err
	}
	if r.mode == Replay && !sameRequest(req, body, r.want[n-1].Request) {
		return nil, r.fail(r.mismatch(n, req, body))
	}
	r.sent = body
	resp, err := r.vcr.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	r.served = n
	if r.mode == Record {
		// Save now, so a killed run keeps every exchange it finished.
		if err := r.vcr.Stop(); err != nil {
			_ = resp.Body.Close()
			return nil, r.fail(fmt.Errorf("cassette %s: save: %w", r.path, err))
		}
	}
	return resp, nil
}

// Err returns the first failure of the recorder, or nil.
func (r *Recorder) Err() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.err
}

// Done reports an error when the run failed or, in Replay, when the run sent
// fewer requests than the cassette holds.
func (r *Recorder) Done() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return r.err
	}
	if r.mode == Replay && r.served < len(r.want) {
		return fmt.Errorf("cassette %s: the run sent %d of %d recorded requests; %s", r.path, r.served, len(r.want), rerecordHint)
	}
	return nil
}

func (r *Recorder) fail(err error) error {
	if r.err == nil {
		r.err = err
	}
	return err
}

// mismatch explains why request n does not match the n-th recorded request.
func (r *Recorder) mismatch(n int, req *http.Request, body string) error {
	rec := r.want[n-1].Request
	if req.Method != rec.Method || req.URL.String() != rec.URL {
		return fmt.Errorf("cassette %s: request %d is %s %s, the recording has %s %s; %s",
			r.path, n, req.Method, req.URL, rec.Method, rec.URL, rerecordHint)
	}
	diff, _ := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A:        difflib.SplitLines(canonical(rec.Body)),
		B:        difflib.SplitLines(canonical(body)),
		FromFile: "recorded",
		ToFile:   "sent",
		Context:  3,
	})
	return fmt.Errorf("cassette %s: request %d body does not match the recording; %s\n%s", r.path, n, rerecordHint, diff)
}

// sameRequest matches on method, URL and the canonical body.
func sameRequest(req *http.Request, body string, rec vcr.Request) bool {
	return req.Method == rec.Method && req.URL.String() == rec.URL && canonical(body) == canonical(rec.Body)
}

// readBody reads the body of req and puts an unread copy back.
func readBody(req *http.Request) (string, error) {
	if req.Body == nil || req.Body == http.NoBody {
		return "", nil
	}
	b, err := io.ReadAll(req.Body)
	if err != nil {
		return "", err
	}
	req.Body = io.NopCloser(bytes.NewReader(b))
	return string(b), nil
}

// canonical returns a JSON body indented with sorted keys, or the body as it
// is when it is not JSON.
func canonical(body string) string {
	dec := json.NewDecoder(strings.NewReader(body))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil || dec.More() {
		return body
	}
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return body
	}
	return string(out) + "\n"
}

// capture stores the request body as sent, then keeps only the allowed
// headers and drops the fields that change on every run. go-vcr records the
// body only when the transport reads it, so the body is set here.
func (r *Recorder) capture(i *vcr.Interaction) error {
	i.Request.Body = r.sent
	i.Request.Headers = keep(i.Request.Headers, keptRequestHeaders)
	i.Request.Form = nil
	i.Request.RemoteAddr = ""
	i.Response.Headers = keep(i.Response.Headers, keptResponseHeaders)
	i.Response.Duration = 0
	return nil
}

func keep(h http.Header, names []string) http.Header {
	out := http.Header{}
	for _, n := range names {
		if v := h.Values(n); len(v) > 0 {
			out[http.CanonicalHeaderKey(n)] = v
		}
	}
	return out
}
