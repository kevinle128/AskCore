package providers

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
)

// modelsDevProviders are the models.dev provider ids that Ask maps. The id of
// each is the Ask provider id.
var modelsDevProviders = []string{ProviderAnthropic, ProviderOpenAI, ProviderXAI, ProviderTokenPlan}

// ModelsDevProviders returns the provider filter of a models.dev fetch: the
// providers that Ask maps.
func ModelsDevProviders() []string { return slices.Clone(modelsDevProviders) }

// modelsDevFields are the only model fields that the mapper reads. models.dev
// also has api, npm, provider and header fields; the mapper never reads them,
// because the family rule alone decides where and how a request goes.
var modelsDevFields = []string{"id", "name", "limit", "cost", "reasoning", "reasoning_options", "modalities", "tool_call", "type", "status"}

// maxModelText is the longest id or name that the mapper keeps.
const maxModelText = 128

// maxPriceUSD is the highest price per 1_000_000 tokens that the mapper takes
// as real. A larger value is bad data, and the cost becomes unknown.
const maxPriceUSD = 1_000_000

type modelsDevProvider struct {
	Models map[string]json.RawMessage `json:"models"`
}

type modelsDevModel struct {
	ID               string                     `json:"id"`
	Name             string                     `json:"name"`
	Limit            *modelsDevLimit            `json:"limit"`
	Cost             *modelsDevCost             `json:"cost"`
	Reasoning        bool                       `json:"reasoning"`
	ReasoningOptions []modelsDevReasoningOption `json:"reasoning_options"`
	Modalities       modelsDevModalities        `json:"modalities"`
	ToolCall         bool                       `json:"tool_call"`
	Type             string                     `json:"type"`
	Status           string                     `json:"status"`
}

type modelsDevLimit struct {
	Context int `json:"context"`
	Output  int `json:"output"`
}

// modelsDevPrice is USD per 1_000_000 tokens.
type modelsDevPrice struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cache_read"`
	CacheWrite float64 `json:"cache_write"`
}

type modelsDevCost struct {
	modelsDevPrice
	Tiers []modelsDevTier `json:"tiers"`
}

type modelsDevTier struct {
	modelsDevPrice
	Tier struct {
		Type string `json:"type"`
		Size int64  `json:"size"`
	} `json:"tier"`
}

type modelsDevReasoningOption struct {
	Type   string   `json:"type"`
	Values []string `json:"values"`
}

type modelsDevModalities struct {
	Input  []string `json:"input"`
	Output []string `json:"output"`
}

// knownReasoningOptions are the reasoning option types that the mapper
// understands. Only effort gives words; the others carry no request data that
// Ask sends. A row with any other type is dropped, because its meaning is
// unknown.
var knownReasoningOptions = []string{"effort", "budget_tokens", "toggle"}

// MapModelsDev maps a models.dev api.json payload, or a subset of it with the
// same layout, to catalog rows. It reads only the Ask providers and drops
// every row that is not a chat model with tool calls, that is deprecated,
// that has a bad id, or that matches no shown family rule. The rows are sorted
// by provider and id. It fails only when the payload or a provider entry is
// not the expected JSON shape; a bad model entry is dropped.
func MapModelsDev(payload []byte) ([]Model, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(payload, &top); err != nil {
		return nil, fmt.Errorf("models.dev payload: %w", err)
	}
	if top == nil {
		return nil, errors.New("models.dev payload: not a JSON object")
	}
	var out []Model
	for _, provider := range modelsDevProviders {
		raw, ok := top[provider]
		if !ok {
			continue
		}
		var p modelsDevProvider
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("models.dev provider %s: %w", provider, err)
		}
		for _, rawModel := range p.Models {
			var row modelsDevModel
			if err := json.Unmarshal(rawModel, &row); err != nil {
				continue
			}
			if m, ok := mapModelsDevRow(provider, row); ok {
				out = append(out, m)
			}
		}
	}
	slices.SortFunc(out, compareModels)
	out = slices.CompactFunc(out, func(a, b Model) bool { return compareModels(a, b) == 0 })
	return out, nil
}

// compareModels orders rows by provider, id and API.
func compareModels(a, b Model) int {
	if c := strings.Compare(a.Provider, b.Provider); c != 0 {
		return c
	}
	if c := strings.Compare(a.ID, b.ID); c != 0 {
		return c
	}
	return strings.Compare(string(a.API), string(b.API))
}

// mapModelsDevRow maps one row. The family rule gives the request data; the
// row gives the name, limits, prices and narrows the effort words.
func mapModelsDevRow(provider string, row modelsDevModel) (Model, bool) {
	if !keepModelsDevRow(row) || !validModelText(row.ID) {
		return Model{}, false
	}
	rule, ok := ruleFor(provider, row.ID)
	if !ok || rule.hidden != "" {
		return Model{}, false
	}
	var efforts []string
	for _, o := range row.ReasoningOptions {
		if !slices.Contains(knownReasoningOptions, o.Type) {
			return Model{}, false
		}
		if o.Type == "effort" {
			efforts = append(efforts, o.Values...)
		}
	}
	reasoning, levels, ok := rule.thinkingOf(row.Reasoning, efforts)
	if !ok {
		return Model{}, false
	}
	m := Model{
		ID:               row.ID,
		Provider:         provider,
		API:              rule.api,
		BaseURL:          rule.baseURL,
		Compat:           rule.compat,
		Reasoning:        reasoning,
		ThinkingLevelMap: levels,
	}
	applyModelsDevMetadata(&m, row)
	return m, true
}

// keepModelsDevRow is true for a chat model that is not deprecated, takes
// tool calls and gives only text. This drops image, embedding, speech,
// transcription, video and realtime models. A missing context limit does not
// drop a row: the row shows the value as unknown, and RequestLimits gives the
// request a default.
func keepModelsDevRow(row modelsDevModel) bool {
	if !row.ToolCall || row.Status == "deprecated" {
		return false
	}
	if row.Type != "" && row.Type != "chat" {
		return false
	}
	return slices.Equal(row.Modalities.Output, []string{"text"})
}

// validModelText is true for printable ASCII without '@', at most
// maxModelText bytes. An escape sequence in a remote id or name could change
// the terminal, and '@' separates the id from the API in an ACP model id.
func validModelText(s string) bool {
	if s == "" || len(s) > maxModelText {
		return false
	}
	for i := 0; i < len(s); i++ {
		if c := s[i]; c < 0x20 || c > 0x7e || c == '@' {
			return false
		}
	}
	return true
}

// applyModelsDevMetadata sets the display and price data of m from row: the
// name, the input kinds, the limits and the cost. It sets no request data.
func applyModelsDevMetadata(m *Model, row modelsDevModel) {
	m.Name = row.ID
	if validModelText(row.Name) {
		m.Name = row.Name
	}
	m.Input = []string{"text"}
	if slices.Contains(row.Modalities.Input, "image") {
		m.Input = append(m.Input, "image")
	}
	if row.Limit != nil {
		m.ContextWindow = max(row.Limit.Context, 0)
		m.MaxTokens = max(row.Limit.Output, 0)
	}
	m.Cost, m.CostUnknown = costOfModelsDev(row.Cost)
}

// costOfModelsDev maps the base price and the context tiers to micro-USD. A
// missing cost, a bad price, or a tier of a type other than context makes the
// cost unknown, because Ask cannot price it correctly. The flat
// context_over_200k field is not read: the tiers carry the same data with
// their threshold.
func costOfModelsDev(c *modelsDevCost) (Cost, bool) {
	if c == nil {
		return Cost{}, true
	}
	base, ok := microPrice(c.modelsDevPrice)
	if !ok {
		return Cost{}, true
	}
	tiers := []Tier{{Above: 0, Price: base}}
	for _, t := range c.Tiers {
		if t.Tier.Type != "context" || t.Tier.Size <= 0 {
			return Cost{}, true
		}
		p, ok := microPrice(t.modelsDevPrice)
		if !ok {
			return Cost{}, true
		}
		tiers = append(tiers, Tier{Above: t.Tier.Size, Price: p})
	}
	slices.SortStableFunc(tiers, func(a, b Tier) int { return cmp.Compare(a.Above, b.Above) })
	return Cost{Tiers: tiers}, false
}

func microPrice(p modelsDevPrice) (Price, bool) {
	var out Price
	for _, f := range []struct {
		usd float64
		dst *int64
	}{{p.Input, &out.Input}, {p.Output, &out.Output}, {p.CacheRead, &out.CacheRead}, {p.CacheWrite, &out.CacheWrite}} {
		if math.IsNaN(f.usd) || f.usd < 0 || f.usd > maxPriceUSD {
			return Price{}, false
		}
		*f.dst = int64(math.Round(f.usd * tokensPerPrice))
	}
	return out, true
}
