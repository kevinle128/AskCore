package fantasykit

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func wrapClient(base *http.Client, rt http.RoundTripper) *http.Client {
	out := &http.Client{Timeout: 0}
	if base != nil {
		c := *base
		out = &c
	}
	out.Transport = rt
	return out
}

// canonicalBody returns req with its JSON object body rewritten so that the
// top-level keys are in sorted order. The SDKs merge extra fields in map
// iteration order, so two sends of one request would differ in key order. A
// fixed order makes the bytes of a request depend on its content alone, which
// lets the request log rebuild them exactly. Nested values keep their content. A
// body that is not a JSON object stays as it is.
func canonicalBody(req *http.Request) (*http.Request, error) {
	if req.Body == nil || req.Body == http.NoBody {
		return req, nil
	}
	raw, err := io.ReadAll(req.Body)
	_ = req.Body.Close()
	if err != nil {
		return nil, err
	}
	out := raw
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) == nil && fields != nil {
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false) // keep the text of nested values as the SDK wrote it
		if err := enc.Encode(fields); err == nil {
			out = bytes.TrimRight(buf.Bytes(), "\n")
		}
	}
	clone := req.Clone(req.Context())
	clone.Body = io.NopCloser(bytes.NewReader(out))
	clone.ContentLength = int64(len(out))
	clone.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(out)), nil }
	if clone.Header.Get("Content-Length") != "" {
		clone.Header.Set("Content-Length", strconv.Itoa(len(out)))
	}
	return clone, nil
}

// Client wraps base so each response is witnessed and idle-touched, and each
// request body has its top-level keys in sorted order.
func Client(base *http.Client, w *Witness, touch func()) *http.Client {
	return wrapClient(base, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		rt := http.DefaultTransport
		if base != nil && base.Transport != nil {
			rt = base.Transport
		}
		req, err := canonicalBody(req)
		if err != nil {
			return nil, err
		}
		if w != nil {
			w.SetBase(rt)
			resp, err := w.RoundTrip(req)
			if err != nil || resp == nil || resp.Body == nil {
				return resp, err
			}
			resp.Body = touchAndTrack(resp.Body, touch, bodyEndOf(req.Context()))
			return resp, nil
		}
		resp, err := rt.RoundTrip(req)
		if err != nil || resp == nil || resp.Body == nil {
			return resp, err
		}
		resp.Body = touchAndTrack(resp.Body, touch, bodyEndOf(req.Context()))
		return resp, nil
	}))
}
