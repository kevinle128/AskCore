package providers

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
)

// Origin tells if the user model configuration made or changed a row.
type Origin string

const (
	// OriginBuiltin is a row of the catalog, the metadata overlay or an
	// account list that the configuration did not change.
	OriginBuiltin Origin = "builtin"
	// OriginUser is a row that the configuration added, replaced, moved to
	// another base URL, or changed with an override.
	OriginUser Origin = "user"
)

const (
	customContextWindow = 128_000
	customMaxTokens     = 16_384
)

// ModelConfig is the user model configuration in the form that this package
// reads. The caller maps the file to it. It holds no key and no header: a
// header value can be a secret, and a row of a ModelSet is copied to many
// places.
type ModelConfig struct {
	Providers []ProviderConfig
}

// ProviderConfig is the configuration of one provider.
type ProviderConfig struct {
	ID string
	// BaseURL, when set, is the base URL of every row of the provider. On a
	// built-in provider it moves the built-in rows too; their API, compat and
	// effort words stay.
	BaseURL string
	// API and Compat are the defaults of the Models rows only. They do not
	// change built-in rows.
	API    API
	Compat Compat
	// Models are rows that the configuration adds. A row replaces the row of
	// the same provider, id and API.
	Models []ModelSpec
	// Overrides patch the rows of this provider, keyed by model id.
	Overrides map[string]ModelPatch
}

// ModelSpec is one row that the configuration adds. A zero field takes the
// default of a custom row: a context window of 128000, an output limit of
// 16384, text input, no reasoning, zero prices, and the id as the name.
type ModelSpec struct {
	ID   string
	Name string
	// API is the wire API. When it is empty, the API of the provider applies.
	API API
	// Reasoning, when nil, is true only when ThinkingLevelMap is set.
	Reasoning        *bool
	ThinkingLevelMap ThinkingLevelMap
	Input            []string
	Cost             Price
	ContextWindow    int
	MaxTokens        int
	// Compat, when nil, is the provider compat when it matches the API, else
	// the zero compat of the API.
	Compat Compat
}

// ModelPatch changes metadata of existing rows. A nil or empty field changes
// nothing.
//
// On a row that the configuration did not add, a patch can set the name, the
// limits, the price and the input types, and can only turn thinking levels
// off: a level set to nil is unsupported, and a level with a word must keep
// the word that the row has. The family rule of the row owns the request
// shape, so API, Compat and a new effort word are refused. On a row that the
// configuration added, Compat and new words are allowed. API is refused on
// every row, because it is part of the row identity.
//
// A refused field gives one problem; the other fields still apply.
type ModelPatch struct {
	Name          *string
	ContextWindow *int
	MaxTokens     *int
	// Cost sets some prices. The prices that it does not set come from the
	// base price of the row, and the row then has one flat price. A patch
	// that sets no price keeps the price schedule of the row.
	Cost             *PricePatch
	Input            []string
	ThinkingLevelMap ThinkingLevelMap
	API              API
	Compat           Compat
}

// PricePatch sets prices of a row in micro-USD per 1_000_000 tokens, as
// Price does. A nil field keeps the price of the row.
type PricePatch struct {
	Input      *int64
	Output     *int64
	CacheRead  *int64
	CacheWrite *int64
}

func (p PricePatch) over(base Price) (out Price, all bool) {
	out, all = base, true
	for _, f := range []struct {
		v   *int64
		dst *int64
	}{{p.Input, &out.Input}, {p.Output, &out.Output}, {p.CacheRead, &out.CacheRead}, {p.CacheWrite, &out.CacheWrite}} {
		if f.v == nil {
			all = false
			continue
		}
		*f.dst = *f.v
	}
	return out, all
}

func (p PricePatch) empty() bool {
	return p.Input == nil && p.Output == nil && p.CacheRead == nil && p.CacheWrite == nil
}

// ConfigProblem is one part of the configuration that NewModelSet did not
// apply. Path names the place with schema names and list positions only, for
// example "providers[0].models[2].api". Neither field holds a provider id, a
// model id or another value of the file, so a problem is safe to log.
// An override problem has no position, because the overrides of a provider
// are keyed by model id.
type ConfigProblem struct {
	Path string
	Text string
}

type configState struct {
	rows     []Model
	origins  map[Ref]Origin
	problems []ConfigProblem
	// added are the rows that a ModelSpec made.
	added map[Ref]bool
}

// repointedProviders returns the providers whose base URL the configuration
// changes for at least one row. The account list of such a provider belongs
// to the account at the default host, so it does not tell the access at the
// new host.
func repointedProviders(rows []Model, configs []ModelConfig) map[string]bool {
	out := map[string]bool{}
	for _, cfg := range configs {
		for _, pc := range cfg.Providers {
			if pc.BaseURL == "" {
				continue
			}
			out[pc.ID] = out[pc.ID] || slices.ContainsFunc(rows, func(m Model) bool { return m.Provider == pc.ID && m.BaseURL != pc.BaseURL })
		}
	}
	return out
}

// applyConfigs applies each configuration in order, so a later one wins.
func applyConfigs(rows []Model, configs []ModelConfig) configState {
	st := configState{rows: rows, origins: map[Ref]Origin{}, added: map[Ref]bool{}}
	for _, cfg := range configs {
		for i, pc := range cfg.Providers {
			st.applyProvider(fmt.Sprintf("providers[%d]", i), pc)
		}
	}
	return st
}

func (st *configState) problem(path, text string) {
	st.problems = append(st.problems, ConfigProblem{Path: path, Text: text})
}

// applyProvider applies the base URL, then the rows, then the overrides of
// one provider. Overrides come last, so an explicit field wins over every
// other source on each build.
func (st *configState) applyProvider(path string, pc ProviderConfig) {
	if pc.ID == "" {
		st.problem(path, "the provider has no id")
		return
	}
	if pc.BaseURL != "" {
		for i, m := range st.rows {
			if m.Provider == pc.ID && m.BaseURL != pc.BaseURL {
				st.rows[i].BaseURL = pc.BaseURL
				st.origins[refOf(m)] = OriginUser
			}
		}
	}
	own := map[Ref]bool{}
	for j, spec := range pc.Models {
		rowPath := fmt.Sprintf("%s.models[%d]", path, j)
		m, ok := st.customRow(rowPath, pc, spec)
		if !ok {
			continue
		}
		ref := refOf(m)
		if own[ref] {
			st.problem(rowPath, "the provider has this model id and api twice; the first row is used")
			continue
		}
		own[ref] = true
		if i := slices.IndexFunc(st.rows, func(r Model) bool { return refOf(r) == ref }); i >= 0 {
			st.rows[i] = m
		} else {
			st.rows = append(st.rows, m)
		}
		st.origins[ref] = OriginUser
		st.added[ref] = true
	}
	for _, id := range slices.Sorted(maps.Keys(pc.Overrides)) {
		patch := pc.Overrides[id]
		// An override that names no row changes nothing: an account list can
		// hide a row that a later list shows again.
		for i, m := range st.rows {
			if m.Provider != pc.ID || m.ID != id {
				continue
			}
			patched, applied := st.applyPatch(path+".modelOverrides", m, patch)
			if applied {
				st.rows[i] = patched
				st.origins[refOf(m)] = OriginUser
			}
		}
	}
}

// customRow builds the row of one ModelSpec with the custom defaults. ok is
// false when the spec cannot make a valid row; the problem is recorded.
func (st *configState) customRow(path string, pc ProviderConfig, spec ModelSpec) (Model, bool) {
	if spec.ID == "" {
		st.problem(path+".id", "the model has no id")
		return Model{}, false
	}
	api := cmp.Or(spec.API, pc.API)
	switch {
	case api == "":
		st.problem(path+".api", "the model has no api; set api on the model or on the provider")
		return Model{}, false
	case !knownAPI(api):
		st.problem(path+".api", "the api is not supported")
		return Model{}, false
	}
	baseURL := pc.BaseURL
	if baseURL == "" {
		if i := slices.IndexFunc(st.rows, func(m Model) bool { return m.Provider == pc.ID && m.API == api && m.BaseURL != "" }); i >= 0 {
			baseURL = st.rows[i].BaseURL
		}
	}
	if baseURL == "" {
		st.problem(path, "the model has no baseUrl; set baseUrl on the provider")
		return Model{}, false
	}
	m := Model{
		ID: spec.ID, Name: cmp.Or(spec.Name, spec.ID), Provider: pc.ID, API: api, BaseURL: baseURL,
		Input:         []string{"text"},
		ContextWindow: customContextWindow, MaxTokens: customMaxTokens,
		Reasoning: spec.ThinkingLevelMap != nil,
		Cost:      Cost{Tiers: []Tier{{Above: 0, Price: spec.Cost}}},
		Compat:    zeroCompat(api),
	}
	if pc.Compat != nil && compatMatchesAPI(pc.Compat, api) {
		m.Compat = pc.Compat
	}
	if spec.Compat != nil {
		m.Compat = spec.Compat
	}
	if spec.Reasoning != nil {
		m.Reasoning = *spec.Reasoning
	}
	if spec.Input != nil {
		if !validInput(spec.Input) {
			st.problem(path+".input", "input must list text or image")
			return Model{}, false
		}
		m.Input = slices.Clone(spec.Input)
	}
	if spec.ContextWindow < 0 || spec.MaxTokens < 0 {
		st.problem(path, "contextWindow and maxTokens must be positive")
		return Model{}, false
	}
	if spec.ContextWindow > 0 {
		m.ContextWindow = spec.ContextWindow
	}
	if spec.MaxTokens > 0 {
		m.MaxTokens = spec.MaxTokens
	}
	if !validPrice(spec.Cost) {
		st.problem(path+".cost", "a price must not be negative")
		return Model{}, false
	}
	if spec.ThinkingLevelMap != nil {
		if !validLevels(spec.ThinkingLevelMap) {
			st.problem(path+".thinkingLevelMap", "a key is not a thinking level")
			return Model{}, false
		}
		m.ThinkingLevelMap = spec.ThinkingLevelMap
	}
	if err := Validate(m); err != nil {
		st.problem(path+".compat", "the compat does not match the api")
		return Model{}, false
	}
	return cloneModel(m), true
}

// applyPatch applies the allowed fields of patch to m. applied is false when
// no field changed the row.
func (st *configState) applyPatch(path string, m Model, p ModelPatch) (Model, bool) {
	added := st.added[refOf(m)]
	out := cloneModel(m)
	applied := false
	if p.API != "" && p.API != m.API {
		st.problem(path+".api", "an override cannot change the api of a model")
	}
	if p.Compat != nil {
		switch {
		case !added:
			st.problem(path+".compat", "an override cannot change the compat of a built-in model")
		case !compatMatchesAPI(p.Compat, m.API):
			st.problem(path+".compat", "the compat does not match the api")
		default:
			out.Compat, applied = p.Compat, true
		}
	}
	if p.Name != nil {
		if *p.Name == "" {
			st.problem(path+".name", "the name is empty")
		} else {
			out.Name, applied = *p.Name, true
		}
	}
	if p.ContextWindow != nil {
		if *p.ContextWindow <= 0 {
			st.problem(path+".contextWindow", "contextWindow must be positive")
		} else {
			out.ContextWindow, applied = *p.ContextWindow, true
		}
	}
	if p.MaxTokens != nil {
		if *p.MaxTokens <= 0 {
			st.problem(path+".maxTokens", "maxTokens must be positive")
		} else {
			out.MaxTokens, applied = *p.MaxTokens, true
		}
	}
	if p.Cost != nil && !p.Cost.empty() {
		var base Price
		if len(m.Cost.Tiers) > 0 {
			base = m.Cost.Tiers[0].Price
		}
		price, all := p.Cost.over(base)
		if !validPrice(price) {
			st.problem(path+".cost", "a price must not be negative")
		} else {
			// A price that the patch omits stays unknown on a row whose
			// prices are unknown.
			out.Cost, out.CostUnknown, applied = Cost{Tiers: []Tier{{Above: 0, Price: price}}}, m.CostUnknown && !all, true
		}
	}
	if p.Input != nil {
		if !validInput(p.Input) {
			st.problem(path+".input", "input must list text or image")
		} else {
			out.Input, applied = slices.Clone(p.Input), true
		}
	}
	if p.ThinkingLevelMap != nil {
		switch {
		case !validLevels(p.ThinkingLevelMap):
			st.problem(path+".thinkingLevelMap", "a key is not a thinking level")
		case !added && !onlyNarrows(m, p.ThinkingLevelMap):
			st.problem(path+".thinkingLevelMap", "an override can only turn levels off on a built-in model")
		default:
			levels := out.ThinkingLevelMap
			if levels == nil {
				levels = ThinkingLevelMap{}
			}
			for l, w := range p.ThinkingLevelMap {
				if w != nil {
					w = stringPointer(*w)
				}
				levels[l] = w
			}
			out.ThinkingLevelMap, applied = levels, true
		}
	}
	if !applied {
		return m, false
	}
	if err := Validate(out); err != nil {
		st.problem(path, "the override makes the model invalid; it is not used")
		return m, false
	}
	return out, true
}

// onlyNarrows is true when every entry of levels turns a level off or keeps
// the word that m sends for that level today.
func onlyNarrows(m Model, levels ThinkingLevelMap) bool {
	for l, w := range levels {
		if w == nil {
			continue
		}
		if !m.Reasoning || !thinkingSupported(m, l) {
			return false
		}
		current := string(l)
		if word, ok := m.ThinkingLevelMap[l]; ok {
			current = *word
		}
		if *w != current {
			return false
		}
	}
	return true
}

func knownAPI(api API) bool {
	return api == APIAnthropicMessages || api == APIOpenAICompletions || api == APIOpenAIResponses
}

func zeroCompat(api API) Compat {
	switch api {
	case APIAnthropicMessages:
		return AnthropicCompat{}
	case APIOpenAICompletions:
		return CompletionsCompat{}
	case APIOpenAIResponses:
		return ResponsesCompat{}
	default:
		return nil
	}
}

func validInput(input []string) bool {
	if len(input) == 0 {
		return false
	}
	return !slices.ContainsFunc(input, func(s string) bool { return s != "text" && s != "image" })
}

func validPrice(p Price) bool {
	return p.Input >= 0 && p.Output >= 0 && p.CacheRead >= 0 && p.CacheWrite >= 0
}

func validLevels(levels ThinkingLevelMap) bool {
	for l := range levels {
		if !slices.Contains(thinkingRank, l) {
			return false
		}
	}
	return true
}
