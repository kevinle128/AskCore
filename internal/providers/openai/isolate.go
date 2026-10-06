package openai

import "net/http"

var isolateAllow = []string{
	"Authorization",
	"Content-Type",
	"Accept",
	"User-Agent",
}

// Isolate is an http.RoundTripper that drops OpenAI-Organization,
// OpenAI-Project, and any header not in the allowlist (Authorization,
// Content-Type, Accept, User-Agent, plus model headers).
func Isolate(base http.RoundTripper, extra map[string]string) http.RoundTripper {
	allow := make(map[string]struct{}, len(isolateAllow)+len(extra))
	for _, k := range isolateAllow {
		allow[http.CanonicalHeaderKey(k)] = struct{}{}
	}
	hdr := make(http.Header, len(extra))
	for k, v := range extra {
		ck := http.CanonicalHeaderKey(k)
		allow[ck] = struct{}{}
		hdr.Set(ck, v)
	}
	if base == nil {
		base = http.DefaultTransport
	}
	return isolateRT{base: base, allow: allow, extra: hdr}
}

type isolateRT struct {
	base  http.RoundTripper
	allow map[string]struct{}
	extra http.Header
}

func (t isolateRT) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	// Clone can leave Body nil after the original reader is spent; GetBody
	// rebuilds it so the isolated request still carries the JSON.
	if req.GetBody != nil {
		body, err := req.GetBody()
		if err != nil {
			return nil, err
		}
		clone.Body = body
	}
	next := make(http.Header, len(t.allow))
	for k, vs := range clone.Header {
		ck := http.CanonicalHeaderKey(k)
		if ck == "Openai-Organization" || ck == "Openai-Project" {
			continue
		}
		if _, ok := t.allow[ck]; !ok {
			continue
		}
		next[ck] = vs
	}
	for k, vs := range t.extra {
		next[k] = vs
	}
	clone.Header = next
	return t.base.RoundTrip(clone)
}
