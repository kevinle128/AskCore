package fantasykit

import "net/http"

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

// Client wraps base so each response is witnessed and idle-touched.
func Client(base *http.Client, w *Witness, touch func()) *http.Client {
	return wrapClient(base, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		rt := http.DefaultTransport
		if base != nil && base.Transport != nil {
			rt = base.Transport
		}
		if w != nil {
			w.SetBase(rt)
			resp, err := w.RoundTrip(req)
			if err != nil || resp == nil || resp.Body == nil {
				return resp, err
			}
			resp.Body = TouchBody(resp.Body, touch)
			return resp, nil
		}
		resp, err := rt.RoundTrip(req)
		if err != nil || resp == nil || resp.Body == nil {
			return resp, err
		}
		resp.Body = TouchBody(resp.Body, touch)
		return resp, nil
	}))
}
