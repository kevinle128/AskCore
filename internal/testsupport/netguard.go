package testsupport

import (
	"errors"
	"net"
	"net/http"
	"testing"
)

// ErrNetworkGuard is the error of a request that the network guard refused.
var ErrNetworkGuard = errors.New("network guard: request to a host other than loopback")

// LoopbackOnly returns an HTTP client whose transport sends a request only to
// 127.0.0.1, ::1 or localhost, such as an httptest server. A request to any
// other host fails the test and returns ErrNetworkGuard. A fixture gives this
// client to code that must stay offline in a test, so a fixture that forgets
// to turn that code off fails instead of calling a real server.
func LoopbackOnly(t testing.TB) *http.Client {
	return &http.Client{Transport: &loopbackGuard{t: t, base: http.DefaultTransport}}
}

type loopbackGuard struct {
	t    testing.TB
	base http.RoundTripper
}

func (g *loopbackGuard) RoundTrip(req *http.Request) (*http.Response, error) {
	host := req.URL.Hostname()
	if host == "localhost" {
		return g.base.RoundTrip(req)
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return g.base.RoundTrip(req)
	}
	// A request body must be closed by the transport, also on refusal.
	if req.Body != nil {
		_ = req.Body.Close()
	}
	g.t.Errorf("network guard: %s %s://%s%s is not a loopback request", req.Method, req.URL.Scheme, req.URL.Host, req.URL.Path)
	return nil, ErrNetworkGuard
}
