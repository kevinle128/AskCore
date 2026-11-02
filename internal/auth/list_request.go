package auth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"AskCore/internal/providers"
)

// listPageLimit caps one model-list answer. The biggest real list (OpenAI,
// about 130 ids) is far below it.
const listPageLimit = 1 << 20

// ErrModelListUnavailable means that Ask could not get a model list for the
// account. The access state is then unknown, which permits inference.
var ErrModelListUnavailable = errors.New("auth: model list unavailable")

// errListRefused means that the allow-list transport stopped a request before
// it left the process.
var errListRefused = errors.New("auth: model list request refused by the allow-list")

// ModelListError is a non-2xx answer of a model-list request. It holds only
// the HTTP status and a short provider code; the body is never kept, so
// provider message text cannot reach a user.
type ModelListError struct {
	HTTPStatus int
	Code       string
}

func (e *ModelListError) Error() string {
	return fmt.Sprintf("auth: model list unavailable (HTTP %d)", e.HTTPStatus)
}

// Is makes every ModelListError match ErrModelListUnavailable.
func (e *ModelListError) Is(target error) bool { return target == ErrModelListUnavailable }

// listRoute is the one read-only model-list request of a provider. The
// credential has exactly one header: a bearer token, or an Anthropic key.
type listRoute struct {
	endpoint  string
	anthropic bool
	// query holds the query keys that the request can have, for paging.
	query []string
}

var (
	openAIListRoute    = listRoute{endpoint: providers.OpenAIURL + "/models"}
	xaiListRoute       = listRoute{endpoint: providers.XAIURL + "/models"}
	anthropicListRoute = listRoute{endpoint: providers.AnthropicURL + "/v1/models", anthropic: true, query: []string{"limit", "after_id"}}
)

// keyListRoute returns the model-list request for an API key of provider.
// Alibaba Token Plan has none: its list is the plan catalog, not the models
// that the key can use.
func keyListRoute(provider string) (listRoute, bool) {
	switch provider {
	case providers.ProviderOpenAI:
		return openAIListRoute, true
	case providers.ProviderXAI:
		return xaiListRoute, true
	case providers.ProviderAnthropic:
		return anthropicListRoute, true
	default:
		return listRoute{}, false
	}
}

func (r listRoute) authHeader() string {
	if r.anthropic {
		return "X-Api-Key"
	}
	return "Authorization"
}

// request builds the GET with the credential. NativeKeyCheck and the model
// lists share it, so both send the same request shape.
func (r listRoute) request(ctx context.Context, secret string, query url.Values) (*http.Request, error) {
	target := r.endpoint
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if r.anthropic {
		req.Header.Set("x-api-key", secret)
		req.Header.Set("anthropic-version", anthropicVersion)
	} else {
		req.Header.Set("Authorization", "Bearer "+secret)
	}
	return req, nil
}

// listClient returns the native bounded client (15 s, no redirect) with no
// cookie jar and a transport that sends only the request of route.
func listClient(o NativeOptions, route listRoute) *http.Client {
	client := boundedClient(o)
	client.Jar = nil
	base := client.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	client.Transport = &listTransport{base: base, route: route}
	return client
}

// listTransport sends a credential only to the fixed model-list endpoint of
// one provider. A model-list request is a plain GET, so anything else (another
// method, host or path, a body, a cookie, a second credential header or a
// redirect) is a defect and never leaves the process.
type listTransport struct {
	base  http.RoundTripper
	route listRoute
}

func (t *listTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if !t.allowed(req) {
		if req.Body != nil {
			_ = req.Body.Close()
		}
		return nil, errListRefused
	}
	return t.base.RoundTrip(req)
}

func (t *listTransport) allowed(req *http.Request) bool {
	want, err := url.Parse(t.route.endpoint)
	if err != nil || req.URL == nil {
		return false
	}
	u := req.URL
	if req.Method != http.MethodGet || req.Response != nil || (req.Body != nil && req.Body != http.NoBody) {
		return false
	}
	if u.Scheme != want.Scheme || u.Host != want.Host || u.Path != want.Path || u.RawPath != "" || u.User != nil || u.Fragment != "" || u.Opaque != "" {
		return false
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return false
	}
	for key, values := range query {
		if !slices.Contains(t.route.query, key) || len(values) != 1 {
			return false
		}
	}
	if len(req.Header.Values("Cookie")) > 0 {
		return false
	}
	credentials := 0
	for _, name := range []string{"Authorization", "X-Api-Key", "Proxy-Authorization"} {
		credentials += len(req.Header.Values(name))
	}
	return credentials == 1 && len(req.Header.Values(t.route.authHeader())) == 1
}

// getListPage sends one model-list request. It returns the status and a body
// of at most listPageLimit bytes. A body over the limit gives the status and
// an error.
func getListPage(client *http.Client, req *http.Request) (int, []byte, error) {
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, listPageLimit+1))
	if err != nil {
		return resp.StatusCode, nil, err
	}
	if len(body) > listPageLimit {
		return resp.StatusCode, nil, errors.New("auth: model list answer too large")
	}
	return resp.StatusCode, body, nil
}

// refusalFrom keeps only the short provider code of a refusal body, and drops
// it when it holds the secret that the request sent.
func refusalFrom(body []byte, secret string) string {
	code := refusalCode(body)
	if strings.Contains(code, secret) {
		return ""
	}
	return code
}
