package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	"AskCore/internal/providers"
	"AskCore/internal/settings"
)

const (
	// listPageBound bounds the paging. One Anthropic page holds up to 1000
	// ids, so a real account never needs more than one page.
	listPageBound  = 20
	modelNameLimit = 128
	subjectLimit   = 255
)

var (
	errListShape  = fmt.Errorf("%w: unexpected answer", ErrModelListUnavailable)
	errListPaging = fmt.Errorf("%w: paging did not end", ErrModelListUnavailable)
	errListBody   = fmt.Errorf("%w: answer too large or unreadable", ErrModelListUnavailable)
)

// AccountModel is one model id of an account list, in the provider's order.
type AccountModel struct {
	ID          string
	DisplayName string
}

// AccountModels is the model list of one account. State is known only when
// the provider answered with a list; otherwise it is unknown and Items is
// empty, which permits inference with every catalog model. Key is the cache
// key from AccountKey; an empty Key means that the result must not be cached.
type AccountModels struct {
	Key      string
	Provider string
	Method   string
	State    settings.AccountModelsState
	Items    []AccountModel
}

// AccountKey returns the cache key of an account: provider, method and an
// identity. The identity is the OAuth subject when the credential has one, or
// KeyedHash(salt, key) for an API key; otherwise it is empty. The key never
// holds a token, a key, or the credential generation, because a token refresh
// changes the generation but not the account.
//
// Take salt from the data that ModelCache.Update supplies. With no valid salt
// an API key has no safe identity, so the result is "" and the caller must not
// cache it: a shared key for all API keys of a provider would show the list of
// one key for another.
func AccountKey(provider, method string, c settings.Credential, salt []byte) string {
	if !cleanKeyPart(provider) || !cleanKeyPart(method) {
		return ""
	}
	identity := ""
	switch {
	case method == "api-key":
		if c.APIKey == "" {
			return ""
		}
		hash := settings.KeyedHash(salt, c.APIKey)
		if hash == "" {
			return ""
		}
		identity = "key:" + hash
	case c.OAuth != nil && c.OAuth.Subject != "":
		if !cleanSubject(c.OAuth.Subject) {
			return ""
		}
		identity = "sub:" + c.OAuth.Subject
	}
	return provider + "/" + method + "/" + identity
}

func cleanKeyPart(v string) bool {
	return cleanModelName(v) && !strings.Contains(v, "/")
}

// cleanSubject accepts an OAuth subject for a cache key. A subject is an
// opaque id that is never shown, so it can hold '@' and be as long as OpenID
// Connect allows (255 bytes); it must be printable ASCII, so that the key
// stays one clean JSON string.
func cleanSubject(v string) bool {
	if v == "" || len(v) > subjectLimit {
		return false
	}
	for i := 0; i < len(v); i++ {
		if v[i] < 0x20 || v[i] > 0x7e {
			return false
		}
	}
	return true
}

// cleanModelName accepts a model id or name that a terminal can show as is and
// that cannot look like an address: printable ASCII, no '@', at most 128 bytes.
// It stops escape sequences from a provider answer.
func cleanModelName(v string) bool {
	if v == "" || len(v) > modelNameLimit || strings.Contains(v, "@") {
		return false
	}
	for i := 0; i < len(v); i++ {
		if v[i] < 0x20 || v[i] > 0x7e {
			return false
		}
	}
	return true
}

// cleanModels drops items with a bad id and display names that are bad. It
// keeps the provider's order.
func cleanModels(items []AccountModel) []AccountModel {
	out := make([]AccountModel, 0, len(items))
	for _, item := range items {
		if !cleanModelName(item.ID) {
			continue
		}
		if !cleanModelName(item.DisplayName) {
			item.DisplayName = ""
		}
		out = append(out, item)
	}
	return out
}

// ListModels returns the model list of the current credential of provider.
// It resolves the credential as Resolve does: a saved record first, refreshed
// when it is about to expire, and an environment key only when no record
// exists. With no credential, or for a method that has no list, it sends no
// request and the state is unknown. A refusal or failure also gives unknown,
// with an error for the log only: the error never holds provider text.
//
// salt is the cache salt for the API-key identity in Key (see AccountKey).
func (s *Service) ListModels(ctx context.Context, provider string, salt []byte) (AccountModels, error) {
	out := AccountModels{Provider: provider, State: settings.AccountModelsUnknown}
	c, err := s.currentCredential(ctx, provider)
	if err != nil {
		return out, err
	}
	out.Method = c.Method
	out.Key = AccountKey(provider, c.Method, c, salt)
	if c.RefreshState != nil {
		return out, ErrRecovery
	}
	method, found := s.listMethod(provider, c.Method)
	if !found {
		if c.Method == "api-key" {
			return out, nil
		}
		return out, fmt.Errorf("%w: %s/%s", ErrMethod, provider, c.Method)
	}
	if method.ListModels == nil {
		return out, nil
	}
	if c.Method != "api-key" {
		fresh, finish, err := s.freshOAuth(ctx, provider, method, c)
		if err != nil {
			return out, err
		}
		// The list request does not rotate the grant, so a shutdown drain
		// need not wait for it.
		finish()
		c = fresh
	}
	got, err := method.ListModels(ctx, c)
	if err != nil || got.State != settings.AccountModelsKnown {
		return out, err
	}
	out.State = settings.AccountModelsKnown
	out.Items = got.Items
	return out, nil
}

// CurrentAccountKey returns the cache key (see AccountKey) of the credential
// that ListModels would use for provider now, or "" when there is no
// credential or no safe key. It reads local state only and never refreshes a
// token: the OAuth subject in the key does not change on a refresh.
func (s *Service) CurrentAccountKey(ctx context.Context, provider string, salt []byte) string {
	c, err := s.currentCredential(ctx, provider)
	if err != nil {
		return ""
	}
	return AccountKey(provider, c.Method, c, salt)
}

// currentCredential returns the saved credential of provider, or an
// environment key as an api-key credential when no record exists. It never
// refreshes a token.
func (s *Service) currentCredential(ctx context.Context, provider string) (settings.Credential, error) {
	if s == nil || s.Store == nil {
		return settings.Credential{}, ErrNoCredential
	}
	c, _, err := s.Store.Read(ctx, provider)
	if err != nil {
		return settings.Credential{}, err
	}
	if c.Method == "" {
		env := s.Env
		if env == nil {
			env = os.LookupEnv
		}
		key := providers.LookupKey(provider, env)
		if key == "" {
			return settings.Credential{}, ErrNoCredential
		}
		c = settings.Credential{Method: "api-key", APIKey: key}
	}
	return c, nil
}

// listMethod finds the method of a credential. An API key can belong to more
// than one method of a provider (one per API); the first one with a list
// function is used.
func (s *Service) listMethod(provider, id string) (Method, bool) {
	var match Method
	found := false
	for _, m := range s.Methods {
		if m.Provider != provider || m.ID != id {
			continue
		}
		if m.ListModels != nil {
			return m, true
		}
		if !found {
			match, found = m, true
		}
	}
	return match, found
}

// NativeKeyModelList returns the list function for an API key of provider, on
// the native bounded client. It returns nil for a provider with no account
// list, such as Alibaba Token Plan.
func NativeKeyModelList(o NativeOptions, provider string) func(context.Context, settings.Credential) (AccountModels, error) {
	route, ok := keyListRoute(provider)
	if !ok {
		return nil
	}
	if route.anthropic {
		return modelLister(listClient(o, route), route, url.Values{"limit": {"1000"}}, anthropicPage)
	}
	return modelLister(listClient(o, route), route, nil, dataPage)
}

// listPage parses one answer page: the items, and the cursor of the next page
// when more pages follow. ok is false for an answer of an unexpected shape.
type listPage func(body []byte) (items []AccountModel, next string, more, ok bool)

// modelLister returns a list function that reads every page of route. The
// result is known only when all pages parse. A page bound hit, a repeated
// cursor or a missing cursor give unknown, because a cut list would hide
// models that the account can use.
func modelLister(client *http.Client, route listRoute, first url.Values, parse listPage) func(context.Context, settings.Credential) (AccountModels, error) {
	return func(ctx context.Context, c settings.Credential) (AccountModels, error) {
		unknown := AccountModels{State: settings.AccountModelsUnknown}
		secret := c.APIKey
		if c.OAuth != nil {
			secret = c.OAuth.AccessToken
		}
		if strings.TrimSpace(secret) == "" {
			return unknown, ErrNoCredential
		}
		var items []AccountModel
		seen := map[string]bool{}
		cursor := ""
		for range listPageBound {
			query := url.Values{}
			for k, v := range first {
				query[k] = v
			}
			if cursor != "" {
				query.Set("after_id", cursor)
			}
			body, err := fetchListPage(ctx, client, route, secret, query)
			if err != nil {
				return unknown, err
			}
			page, next, more, ok := parse(body)
			if !ok {
				return unknown, errListShape
			}
			items = append(items, page...)
			if !more {
				return AccountModels{State: settings.AccountModelsKnown, Items: cleanModels(items)}, nil
			}
			if next == "" || seen[next] {
				return unknown, errListPaging
			}
			seen[next] = true
			cursor = next
		}
		return unknown, errListPaging
	}
}

// fetchListPage sends one list request and returns a 2xx body. Any other
// answer gives a *ModelListError with the status and a short code only.
func fetchListPage(ctx context.Context, client *http.Client, route listRoute, secret string, query url.Values) ([]byte, error) {
	req, err := route.request(ctx, secret, query)
	if err != nil {
		return nil, ErrModelListUnavailable
	}
	status, body, err := getListPage(client, req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if errors.Is(err, errListRefused) {
			return nil, fmt.Errorf("%w: %w", ErrModelListUnavailable, errListRefused)
		}
		if status != 0 {
			return nil, errListBody
		}
		return nil, fmt.Errorf("%w: %w", ErrModelListUnavailable, ErrHTTPExchange)
	}
	if status < 200 || status >= 300 {
		return nil, &ModelListError{HTTPStatus: status, Code: refusalFrom(body, secret)}
	}
	return body, nil
}

// dataPage parses the OpenAI-style list that OpenAI API keys and xAI use:
// {"data":[{"id":...}]}, one page.
func dataPage(body []byte) ([]AccountModel, string, bool, bool) {
	var answer struct {
		Data *[]struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &answer) != nil || answer.Data == nil {
		return nil, "", false, false
	}
	items := make([]AccountModel, 0, len(*answer.Data))
	for _, m := range *answer.Data {
		items = append(items, AccountModel{ID: m.ID})
	}
	return items, "", false, true
}

// anthropicPage parses one page of the Anthropic list. The next page starts
// after last_id while has_more is true.
func anthropicPage(body []byte) ([]AccountModel, string, bool, bool) {
	var answer struct {
		Data *[]struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
		} `json:"data"`
		HasMore bool   `json:"has_more"`
		LastID  string `json:"last_id"`
	}
	if json.Unmarshal(body, &answer) != nil || answer.Data == nil {
		return nil, "", false, false
	}
	items := make([]AccountModel, 0, len(*answer.Data))
	for _, m := range *answer.Data {
		items = append(items, AccountModel{ID: m.ID, DisplayName: m.DisplayName})
	}
	return items, answer.LastID, answer.HasMore, true
}

// chatGPTPage adapts the ChatGPT parser to the page form; the list has one page.
func chatGPTPage(body []byte) ([]AccountModel, string, bool, bool) {
	items, ok := parseChatGPTModels(body)
	return items, "", false, ok
}

// parseChatGPTModels reads the model list that a ChatGPT sign-in token gets
// from the OpenAI model endpoint. Only entries with visibility "list" are for
// a model picker; the server order is kept. ok is false when the models field
// is missing or an entry has no slug, because such an answer is not a list
// that Ask understands.
func parseChatGPTModels(body []byte) ([]AccountModel, bool) {
	var answer struct {
		Models *[]struct {
			Slug        string `json:"slug"`
			DisplayName string `json:"display_name"`
			Visibility  string `json:"visibility"`
		} `json:"models"`
	}
	if json.Unmarshal(body, &answer) != nil || answer.Models == nil {
		return nil, false
	}
	items := []AccountModel{}
	for _, m := range *answer.Models {
		if m.Slug == "" {
			return nil, false
		}
		if m.Visibility == "list" {
			items = append(items, AccountModel{ID: m.Slug, DisplayName: m.DisplayName})
		}
	}
	return items, true
}
