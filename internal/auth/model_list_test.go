package auth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"AskCore/internal/providers"
	"AskCore/internal/settings"
)

// listFixture is a TLS test server that answers for every provider host. The
// client dials it for any address, so requests keep their real host names and
// pass through the real allow-list transport.
type listFixture struct {
	mu       sync.Mutex
	requests []*http.Request
	opts     NativeOptions
}

func newListFixture(t *testing.T, handler http.HandlerFunc) *listFixture {
	t.Helper()
	f := &listFixture{}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.requests = append(f.requests, r.Clone(context.Background()))
		f.mu.Unlock()
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	tr := srv.Client().Transport.(*http.Transport).Clone()
	addr := srv.Listener.Addr().String()
	tr.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, network, addr)
	}
	// The test certificate names example.com; the request keeps its real host.
	tr.TLSClientConfig.ServerName = "example.com"
	f.opts = NativeOptions{HTTPClient: &http.Client{Transport: tr}}
	return f
}

func (f *listFixture) seen() []*http.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*http.Request(nil), f.requests...)
}

func keyMethods(o NativeOptions) []Method {
	var methods []Method
	for _, provider := range []string{providers.ProviderOpenAI, providers.ProviderAnthropic, providers.ProviderXAI, providers.ProviderTokenPlan} {
		methods = append(methods, Method{Provider: provider, ID: "api-key", Profile: "api-key", ListModels: NativeKeyModelList(o, provider)})
	}
	return methods
}

func listService(t *testing.T, methods []Method, env map[string]string) *Service {
	t.Helper()
	store, err := settings.NewAuthStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return &Service{Store: store, Methods: methods, Env: func(name string) (string, bool) {
		v, ok := env[name]
		return v, ok
	}}
}

func saveCredential(t *testing.T, s *Service, provider string, c settings.Credential) {
	t.Helper()
	ctx := context.Background()
	_, rev, err := s.Store.Read(ctx, provider)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Store.Replace(ctx, provider, rev, c); err != nil {
		t.Fatal(err)
	}
}

func testSalt() []byte { return []byte(strings.Repeat("s", 32)) }

func writeJSON(w http.ResponseWriter, code int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = io.WriteString(w, body)
}

func ids(items []AccountModel) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.ID)
	}
	return out
}

func TestListModelsOpenAIKeyDropsUnsafeIDs(t *testing.T) {
	long := strings.Repeat("g", 129)
	f := newListFixture(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, fmt.Sprintf(`{"object":"list","data":[{"id":"gpt-5.5"},{"id":"evil\u001b[31m"},{"id":"user@example"},{"id":%q},{"id":""},{"id":"gpt-4.1-mini"}]}`, long))
	})
	s := listService(t, keyMethods(f.opts), nil)
	saveCredential(t, s, providers.ProviderOpenAI, settings.Credential{Method: "api-key", APIKey: "fake-openai-key"})
	got, err := s.ListModels(context.Background(), providers.ProviderOpenAI, testSalt())
	if err != nil {
		t.Fatal(err)
	}
	if got.State != settings.AccountModelsKnown || strings.Join(ids(got.Items), ",") != "gpt-5.5,gpt-4.1-mini" {
		t.Fatalf("list: %+v", got)
	}
	if got.Provider != providers.ProviderOpenAI || got.Method != "api-key" || got.Key != AccountKey(providers.ProviderOpenAI, "api-key", settings.Credential{APIKey: "fake-openai-key"}, testSalt()) {
		t.Fatalf("identity: %+v", got)
	}
	reqs := f.seen()
	if len(reqs) != 1 {
		t.Fatalf("requests: %d", len(reqs))
	}
	r := reqs[0]
	if r.Method != http.MethodGet || r.Host != "api.openai.com" || r.URL.Path != "/v1/models" || r.URL.RawQuery != "" || r.Header.Get("Authorization") != "Bearer fake-openai-key" || r.Header.Get("X-Api-Key") != "" || r.Header.Get("Cookie") != "" {
		t.Fatalf("request: %s %s %s %v", r.Method, r.Host, r.URL, r.Header)
	}
}

func TestListModelsXAIEnvKey(t *testing.T) {
	f := newListFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "api.x.ai" || r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != "Bearer fake-env-key" {
			t.Errorf("request: %s %s", r.Host, r.URL)
		}
		writeJSON(w, 200, `{"object":"list","data":[{"id":"grok-4.7","aliases":["grok-latest"]}]}`)
	})
	s := listService(t, keyMethods(f.opts), map[string]string{"XAI_API_KEY": "fake-env-key"})
	got, err := s.ListModels(context.Background(), providers.ProviderXAI, testSalt())
	if err != nil || got.State != settings.AccountModelsKnown || strings.Join(ids(got.Items), ",") != "grok-4.7" || got.Method != "api-key" {
		t.Fatalf("env key list: %+v, %v", got, err)
	}
	if got.Key == "" || strings.Contains(got.Key, "fake-env-key") {
		t.Fatalf("key: %q", got.Key)
	}
}

func TestListModelsAnthropicPaging(t *testing.T) {
	f := newListFixture(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("after_id") {
		case "":
			writeJSON(w, 200, `{"data":[{"id":"claude-opus-5-5","display_name":"Claude Opus 5.5"},{"id":"claude-sonnet-5","display_name":"Bad\u0007Name"}],"has_more":true,"first_id":"claude-opus-5-5","last_id":"claude-sonnet-5"}`)
		case "claude-sonnet-5":
			writeJSON(w, 200, `{"data":[{"id":"claude-haiku-4-5-20251001","display_name":"Claude Haiku 4.5"}],"has_more":false,"last_id":"claude-haiku-4-5-20251001"}`)
		default:
			writeJSON(w, 500, `{}`)
		}
	})
	s := listService(t, keyMethods(f.opts), nil)
	saveCredential(t, s, providers.ProviderAnthropic, settings.Credential{Method: "api-key", APIKey: "fake-anthropic-key"})
	got, err := s.ListModels(context.Background(), providers.ProviderAnthropic, testSalt())
	if err != nil {
		t.Fatal(err)
	}
	want := []AccountModel{{"claude-opus-5-5", "Claude Opus 5.5"}, {"claude-sonnet-5", ""}, {"claude-haiku-4-5-20251001", "Claude Haiku 4.5"}}
	if got.State != settings.AccountModelsKnown || fmt.Sprint(got.Items) != fmt.Sprint(want) {
		t.Fatalf("pages: %+v", got)
	}
	reqs := f.seen()
	if len(reqs) != 2 {
		t.Fatalf("requests: %d", len(reqs))
	}
	for i, r := range reqs {
		q := r.URL.Query()
		if r.Host != "api.anthropic.com" || r.URL.Path != "/v1/models" || q.Get("limit") != "1000" || r.Header.Get("X-Api-Key") != "fake-anthropic-key" || r.Header.Get("Anthropic-Version") != anthropicVersion || r.Header.Get("Authorization") != "" {
			t.Fatalf("page %d: %s %s %v", i, r.Host, r.URL, r.Header)
		}
	}
	if reqs[1].URL.Query().Get("after_id") != "claude-sonnet-5" {
		t.Fatalf("cursor: %s", reqs[1].URL)
	}
}

func TestListModelsAnthropicPagingBounds(t *testing.T) {
	t.Run("repeated cursor", func(t *testing.T) {
		f := newListFixture(t, func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, 200, `{"data":[{"id":"claude-opus-5"}],"has_more":true,"last_id":"same"}`)
		})
		list := NativeKeyModelList(f.opts, providers.ProviderAnthropic)
		got, err := list(context.Background(), settings.Credential{Method: "api-key", APIKey: "k"})
		if !errors.Is(err, errListPaging) || got.State != settings.AccountModelsUnknown || got.Items != nil || len(f.seen()) != 2 {
			t.Fatalf("repeated cursor: %+v, %v, %d requests", got, err, len(f.seen()))
		}
	})
	t.Run("page bound", func(t *testing.T) {
		var f *listFixture
		f = newListFixture(t, func(w http.ResponseWriter, r *http.Request) {
			page := len(f.seen())
			writeJSON(w, 200, fmt.Sprintf(`{"data":[{"id":"m-%d"}],"has_more":true,"last_id":"m-%d"}`, page, page))
		})
		list := NativeKeyModelList(f.opts, providers.ProviderAnthropic)
		got, err := list(context.Background(), settings.Credential{Method: "api-key", APIKey: "k"})
		if !errors.Is(err, errListPaging) || got.State != settings.AccountModelsUnknown || len(f.seen()) != listPageBound {
			t.Fatalf("bound: %+v, %v, %d requests", got, err, len(f.seen()))
		}
	})
	t.Run("missing cursor", func(t *testing.T) {
		f := newListFixture(t, func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, 200, `{"data":[],"has_more":true}`)
		})
		got, err := NativeKeyModelList(f.opts, providers.ProviderAnthropic)(context.Background(), settings.Credential{Method: "api-key", APIKey: "k"})
		if !errors.Is(err, errListPaging) || got.State != settings.AccountModelsUnknown {
			t.Fatalf("missing cursor: %+v, %v", got, err)
		}
	})
}

func TestListModelsChatGPTVisibility(t *testing.T) {
	f := newListFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "api.openai.com" || r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != "Bearer chatgpt-access" {
			t.Errorf("request: %s %s", r.Host, r.URL)
		}
		writeJSON(w, 200, `{"models":[{"slug":"gpt-5.6","display_name":"GPT-5.6","visibility":"list"},{"slug":"gpt-5.5-hidden","visibility":"hide"},{"slug":"gpt-5.5","display_name":"GPT-5.5","visibility":"list"}]}`)
	})
	s := listService(t, NativeMethods(f.opts), nil)
	saveCredential(t, s, providers.ProviderOpenAI, settings.Credential{Method: "openai-chatgpt", OAuth: &settings.OAuthCredential{AccessToken: "chatgpt-access", RefreshToken: "chatgpt-refresh", ExpiresAt: time.Now().Add(time.Hour), ClientID: "issued", Subject: "account-1", Issuer: chatGPTIssuer}})
	got, err := s.ListModels(context.Background(), providers.ProviderOpenAI, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []AccountModel{{"gpt-5.6", "GPT-5.6"}, {"gpt-5.5", "GPT-5.5"}}
	if got.State != settings.AccountModelsKnown || fmt.Sprint(got.Items) != fmt.Sprint(want) {
		t.Fatalf("chatgpt: %+v", got)
	}
	if got.Key != "openai/openai-chatgpt/sub:account-1" || got.Method != "openai-chatgpt" {
		t.Fatalf("key: %+v", got)
	}
}

func TestListModelsChatGPTMalformedIsUnknown(t *testing.T) {
	for _, body := range []string{`{"data":[]}`, `{"models":[{"display_name":"x","visibility":"list"}]}`, `not json`} {
		f := newListFixture(t, func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, body) })
		list := modelLister(listClient(f.opts, openAIListRoute), openAIListRoute, nil, chatGPTPage)
		got, err := list(context.Background(), settings.Credential{Method: "openai-chatgpt", OAuth: &settings.OAuthCredential{AccessToken: "a"}})
		if !errors.Is(err, errListShape) || got.State != settings.AccountModelsUnknown {
			t.Fatalf("body %q: %+v, %v", body, got, err)
		}
	}
}

func TestListModelsXAIOAuthRefreshesFirst(t *testing.T) {
	f := newListFixture(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Host == "auth.x.ai" && r.URL.Path == "/oauth2/token":
			writeJSON(w, 200, `{"access_token":"fresh-access","refresh_token":"fresh-refresh","expires_in":3600}`)
		case r.Host == "api.x.ai" && r.URL.Path == "/v1/models":
			if r.Header.Get("Authorization") != "Bearer fresh-access" {
				t.Errorf("list used token %q", r.Header.Get("Authorization"))
			}
			writeJSON(w, 200, `{"data":[{"id":"grok-4.7"}]}`)
		default:
			t.Errorf("unexpected destination %s %s", r.Host, r.URL)
			writeJSON(w, 404, `{}`)
		}
	})
	s := listService(t, NativeMethods(f.opts), nil)
	saveCredential(t, s, providers.ProviderXAI, settings.Credential{Method: "xai-oauth", OAuth: &settings.OAuthCredential{AccessToken: "stale-access", RefreshToken: "stale-refresh", ExpiresAt: time.Now().Add(-time.Minute)}})
	got, err := s.ListModels(context.Background(), providers.ProviderXAI, testSalt())
	if err != nil || got.State != settings.AccountModelsKnown || strings.Join(ids(got.Items), ",") != "grok-4.7" {
		t.Fatalf("xai oauth: %+v, %v", got, err)
	}
	if got.Key != "xai/xai-oauth/" {
		t.Fatalf("key without subject: %q", got.Key)
	}
	saved, _, err := s.Store.Read(context.Background(), providers.ProviderXAI)
	if err != nil || saved.OAuth.AccessToken != "fresh-access" {
		t.Fatalf("refresh not committed: %v", err)
	}
	if err := s.WaitRefresh(context.Background()); err != nil {
		t.Fatalf("refresh slot not released: %v", err)
	}
}

func TestListModelsRefusalsAreUnknown(t *testing.T) {
	cases := []struct {
		name, provider, body string
		code                 int
		wantCode             string
	}{
		{"openai 401", providers.ProviderOpenAI, `{"error":{"message":"Incorrect API key provided: fake-key","type":"invalid_request_error","code":"invalid_api_key"}}`, 401, "invalid_api_key"},
		{"anthropic 403", providers.ProviderAnthropic, `{"type":"error","error":{"type":"permission_error","message":"no access to fake-key"}}`, 403, "permission_error"},
		{"xai 400", providers.ProviderXAI, `{"code":"invalid-argument","error":"Incorrect API key provided: fake-key"}`, 400, ""},
		{"openai 500", providers.ProviderOpenAI, `{"error":{"message":"server fault"}}`, 500, ""},
		{"code holding the key", providers.ProviderOpenAI, `{"error":{"code":"fake-key"}}`, 401, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newListFixture(t, func(w http.ResponseWriter, r *http.Request) { writeJSON(w, tc.code, tc.body) })
			s := listService(t, keyMethods(f.opts), nil)
			saveCredential(t, s, tc.provider, settings.Credential{Method: "api-key", APIKey: "fake-key"})
			got, err := s.ListModels(context.Background(), tc.provider, testSalt())
			var refusal *ModelListError
			if !errors.As(err, &refusal) || refusal.HTTPStatus != tc.code || refusal.Code != tc.wantCode || !errors.Is(err, ErrModelListUnavailable) {
				t.Fatalf("error: %#v", err)
			}
			if got.State != settings.AccountModelsUnknown || got.Items != nil || got.Key == "" {
				t.Fatalf("result: %+v", got)
			}
			for _, leak := range []string{"fake-key", "Incorrect", "server fault", "no access"} {
				if strings.Contains(err.Error(), leak) || strings.Contains(fmt.Sprintf("%+v", got), leak) {
					t.Fatalf("result carries %q: %v %+v", leak, err, got)
				}
			}
		})
	}
}

func TestListModelsTimeoutIsUnknown(t *testing.T) {
	f := newListFixture(t, func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	got, err := NativeKeyModelList(f.opts, providers.ProviderOpenAI)(ctx, settings.Credential{Method: "api-key", APIKey: "k"})
	if !errors.Is(err, context.DeadlineExceeded) || got.State != settings.AccountModelsUnknown {
		t.Fatalf("timeout: %+v, %v", got, err)
	}
}

func TestListModelsEmptyKnownList(t *testing.T) {
	f := newListFixture(t, func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, `{"object":"list","data":[]}`) })
	got, err := NativeKeyModelList(f.opts, providers.ProviderOpenAI)(context.Background(), settings.Credential{Method: "api-key", APIKey: "k"})
	if err != nil || got.State != settings.AccountModelsKnown || len(got.Items) != 0 {
		t.Fatalf("empty: %+v, %v", got, err)
	}
	f = newListFixture(t, func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, `{"object":"list"}`) })
	got, err = NativeKeyModelList(f.opts, providers.ProviderOpenAI)(context.Background(), settings.Credential{Method: "api-key", APIKey: "k"})
	if !errors.Is(err, errListShape) || got.State != settings.AccountModelsUnknown {
		t.Fatalf("missing data: %+v, %v", got, err)
	}
}

func TestListModelsRedirectIsNotFollowed(t *testing.T) {
	f := newListFixture(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://other.example/v1/models", http.StatusFound)
	})
	got, err := NativeKeyModelList(f.opts, providers.ProviderOpenAI)(context.Background(), settings.Credential{Method: "api-key", APIKey: "k"})
	var refusal *ModelListError
	if !errors.As(err, &refusal) || refusal.HTTPStatus != http.StatusFound || got.State != settings.AccountModelsUnknown || len(f.seen()) != 1 {
		t.Fatalf("redirect: %+v, %v, %d requests", got, err, len(f.seen()))
	}
}

func TestListTransportRefusesOtherRequests(t *testing.T) {
	sent := 0
	base := nativeRT(func(*http.Request) (*http.Response, error) {
		sent++
		return nativeResponse(200, `{"data":[]}`), nil
	})
	good := func(route listRoute, secret string, query url.Values) *http.Request {
		req, err := route.request(context.Background(), secret, query)
		if err != nil {
			t.Fatal(err)
		}
		return req
	}
	cases := map[string]func() (listRoute, *http.Request){
		"post": func() (listRoute, *http.Request) {
			req := good(openAIListRoute, "k", nil)
			req.Method = http.MethodPost
			req.Body = io.NopCloser(strings.NewReader("{}"))
			return openAIListRoute, req
		},
		"other host": func() (listRoute, *http.Request) {
			req := good(openAIListRoute, "k", nil)
			req.URL.Host = "api.openai.com.evil.example"
			return openAIListRoute, req
		},
		"other provider host": func() (listRoute, *http.Request) {
			return xaiListRoute, good(openAIListRoute, "k", nil)
		},
		"other path": func() (listRoute, *http.Request) {
			req := good(openAIListRoute, "k", nil)
			req.URL.Path = "/v1/responses"
			return openAIListRoute, req
		},
		"plain http": func() (listRoute, *http.Request) {
			req := good(openAIListRoute, "k", nil)
			req.URL.Scheme = "http"
			return openAIListRoute, req
		},
		"redirect": func() (listRoute, *http.Request) {
			req := good(openAIListRoute, "k", nil)
			req.Response = &http.Response{StatusCode: http.StatusFound}
			return openAIListRoute, req
		},
		"cookie": func() (listRoute, *http.Request) {
			req := good(openAIListRoute, "k", nil)
			req.Header.Set("Cookie", "session=1")
			return openAIListRoute, req
		},
		"two credentials": func() (listRoute, *http.Request) {
			req := good(anthropicListRoute, "k", nil)
			req.Header.Set("Authorization", "Bearer k")
			return anthropicListRoute, req
		},
		"wrong credential header": func() (listRoute, *http.Request) {
			return anthropicListRoute, good(openAIListRoute, "k", nil)
		},
		"no credential": func() (listRoute, *http.Request) {
			req := good(openAIListRoute, "k", nil)
			req.Header.Del("Authorization")
			return openAIListRoute, req
		},
		"query on a route without paging": func() (listRoute, *http.Request) {
			return openAIListRoute, good(openAIListRoute, "k", url.Values{"after_id": {"x"}})
		},
		"unknown query key": func() (listRoute, *http.Request) {
			return anthropicListRoute, good(anthropicListRoute, "k", url.Values{"beta": {"x"}})
		},
	}
	for name, build := range cases {
		route, req := build()
		client := listClient(NativeOptions{HTTPClient: &http.Client{Transport: base}}, route)
		resp, err := client.Do(req)
		if resp != nil {
			_ = resp.Body.Close()
		}
		if !errors.Is(err, errListRefused) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if sent != 0 {
		t.Fatalf("refused requests reached the network: %d", sent)
	}
	client := listClient(NativeOptions{HTTPClient: &http.Client{Transport: base}}, anthropicListRoute)
	resp, err := client.Do(good(anthropicListRoute, "k", url.Values{"limit": {"1000"}, "after_id": {"m"}}))
	if err != nil || sent != 1 {
		t.Fatalf("allowed request: %v", err)
	}
	_ = resp.Body.Close()
}

func TestListModelsMethodsWithoutList(t *testing.T) {
	f := newListFixture(t, func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, `{"data":[]}`) })
	if NativeKeyModelList(f.opts, providers.ProviderTokenPlan) != nil {
		t.Fatal("Token Plan has a key list")
	}
	for _, m := range NativeMethods(f.opts) {
		if m.ID == "anthropic-oauth" && m.ListModels != nil {
			t.Fatal("anthropic-oauth has a list")
		}
	}
	s := listService(t, append(NativeMethods(f.opts), keyMethods(f.opts)...), nil)
	saveCredential(t, s, providers.ProviderAnthropic, settings.Credential{Method: "anthropic-oauth", OAuth: &settings.OAuthCredential{AccessToken: "a", RefreshToken: "r", ExpiresAt: time.Now().Add(-time.Minute)}})
	saveCredential(t, s, providers.ProviderTokenPlan, settings.Credential{Method: "api-key", APIKey: "plan-key"})
	for _, provider := range []string{providers.ProviderAnthropic, providers.ProviderTokenPlan} {
		got, err := s.ListModels(context.Background(), provider, testSalt())
		if err != nil || got.State != settings.AccountModelsUnknown || got.Items != nil || got.Key == "" {
			t.Fatalf("%s: %+v, %v", provider, got, err)
		}
	}
	if len(f.seen()) != 0 {
		t.Fatalf("a method without a list sent %d requests", len(f.seen()))
	}
	saved, _, err := s.Store.Read(context.Background(), providers.ProviderAnthropic)
	if err != nil || saved.OAuth.AccessToken != "a" {
		t.Fatalf("a method without a list refreshed its token: %v", err)
	}
}

func TestListModelsNoCredentialSendsNothing(t *testing.T) {
	f := newListFixture(t, func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, `{"data":[]}`) })
	s := listService(t, keyMethods(f.opts), nil)
	got, err := s.ListModels(context.Background(), providers.ProviderOpenAI, testSalt())
	if !errors.Is(err, ErrNoCredential) || got.State != settings.AccountModelsUnknown || got.Key != "" {
		t.Fatalf("no credential: %+v, %v", got, err)
	}
	var nilService *Service
	if _, err = nilService.ListModels(context.Background(), providers.ProviderOpenAI, nil); !errors.Is(err, ErrNoCredential) {
		t.Fatalf("nil service: %v", err)
	}
	if len(f.seen()) != 0 {
		t.Fatalf("requests without a credential: %d", len(f.seen()))
	}
}

func TestAccountKeyHoldsNoSecret(t *testing.T) {
	salt := testSalt()
	key := settings.Credential{Method: "api-key", APIKey: "sk-secret-value", Generation: 7}
	got := AccountKey("openai", "api-key", key, salt)
	if got == "" || !strings.HasPrefix(got, "openai/api-key/key:") || strings.Contains(got, "sk-secret-value") {
		t.Fatalf("api key: %q", got)
	}
	key.Generation = 8
	if AccountKey("openai", "api-key", key, salt) != got {
		t.Fatal("generation changed the key")
	}
	if other := AccountKey("openai", "api-key", settings.Credential{APIKey: "sk-other"}, salt); other == got {
		t.Fatal("two keys share an identity")
	}
	for _, bad := range [][]byte{nil, []byte("short")} {
		if k := AccountKey("openai", "api-key", key, bad); k != "" {
			t.Fatalf("api key without a valid salt: %q", k)
		}
	}
	oauth := settings.Credential{Method: "openai-chatgpt", Generation: 3, OAuth: &settings.OAuthCredential{AccessToken: "access-secret", RefreshToken: "refresh-secret", IDToken: "id-secret", Subject: "user-1"}}
	got = AccountKey("openai", "openai-chatgpt", oauth, nil)
	if got != "openai/openai-chatgpt/sub:user-1" {
		t.Fatalf("subject key: %q", got)
	}
	oauth.Generation = 4
	oauth.OAuth.AccessToken = "rotated-secret"
	if AccountKey("openai", "openai-chatgpt", oauth, nil) != got {
		t.Fatal("token refresh changed the key")
	}
	for _, subject := range []string{"user@example.com", "auth0|" + strings.Repeat("x", 200)} {
		c := settings.Credential{OAuth: &settings.OAuthCredential{Subject: subject}}
		if k := AccountKey("openai", "openai-chatgpt", c, nil); k != "openai/openai-chatgpt/sub:"+subject {
			t.Fatalf("subject %q: %q", subject, k)
		}
	}
	if k := AccountKey("xai", "xai-oauth", settings.Credential{OAuth: &settings.OAuthCredential{AccessToken: "x-secret"}}, salt); k != "xai/xai-oauth/" {
		t.Fatalf("no subject: %q", k)
	}
	for _, tc := range [][3]string{{"open/ai", "api-key", "sub"}, {"openai", "", "sub"}, {"openai", "openai-chatgpt", "bad\nsubject"}, {"openai", "openai-chatgpt", strings.Repeat("s", 256)}} {
		c := settings.Credential{OAuth: &settings.OAuthCredential{Subject: tc[2]}}
		if k := AccountKey(tc[0], tc[1], c, salt); k != "" {
			t.Fatalf("unsafe part %q: %q", tc, k)
		}
	}
}

// CurrentAccountKey gives the key that ListModels uses, from local state only:
// an expired OAuth token is not refreshed, because the key has no token part.
func TestCurrentAccountKeyMatchesListModelsOffline(t *testing.T) {
	f := newListFixture(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request %s %s", r.Host, r.URL)
		writeJSON(w, 500, `{}`)
	})
	s := listService(t, NativeMethods(f.opts), map[string]string{"OPENAI_API_KEY": "sk-env"})
	ctx := context.Background()
	if got, want := s.CurrentAccountKey(ctx, providers.ProviderOpenAI, testSalt()), AccountKey(providers.ProviderOpenAI, "api-key", settings.Credential{Method: "api-key", APIKey: "sk-env"}, testSalt()); got != want || got == "" {
		t.Fatalf("environment key: got %q, want %q", got, want)
	}
	if got := s.CurrentAccountKey(ctx, providers.ProviderOpenAI, nil); got != "" {
		t.Fatalf("an API key with no salt has no safe key: %q", got)
	}
	if got := s.CurrentAccountKey(ctx, providers.ProviderAnthropic, testSalt()); got != "" {
		t.Fatalf("no credential: %q", got)
	}

	saveCredential(t, s, providers.ProviderXAI, settings.Credential{Method: "xai-oauth", OAuth: &settings.OAuthCredential{AccessToken: "old", RefreshToken: "r", ExpiresAt: time.Now().Add(-time.Minute)}})
	if got := s.CurrentAccountKey(ctx, providers.ProviderXAI, testSalt()); got != "xai/xai-oauth/" {
		t.Fatalf("xai sign-in key: %q", got)
	}
	if n := len(f.seen()); n != 0 {
		t.Fatalf("%d requests", n)
	}
}
