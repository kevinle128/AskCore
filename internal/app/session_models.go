package app

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"go.uber.org/zap"

	"AskCore/internal/auth"
	"AskCore/internal/providers"
	"AskCore/internal/settings"
	"AskCore/pkg/protocol"
)

// errModelConfigChanged stops a request on a row that the user models.json
// made when the file no longer has the endpoint of that row. The key and the
// headers of the row are gone with it, so no request is sent.
var errModelConfigChanged = errors.New(protocol.ModelConfigChanged)

// maxFilePriceUSD is the highest price per 1_000_000 tokens that the user
// file can set. It is the bound of the models.dev mapping.
const maxFilePriceUSD = 1_000_000

// modelKey is the endpoint of a row: provider, wire API and base URL. The key
// and the headers of the user file bind to it, never to a provider name
// alone, so a row and its key always come from the same file.
type modelKey struct {
	provider string
	api      providers.API
	baseURL  string
}

func keyOf(m providers.Model) modelKey {
	return modelKey{provider: m.Provider, api: m.API, baseURL: m.BaseURL}
}

// userEntry is what the user file gives one endpoint. It holds the raw value
// templates only: auth resolves them per request, so no secret is stored.
type userEntry struct {
	apiKey string
	// endpoint is the base URL that the file wrote apiKey for: the baseUrl of
	// the provider entry, or the built-in URL of the row when it sets none.
	endpoint string
	// headers are the raw provider header templates.
	headers map[string]string
	// modelHeaders are the raw header templates of one model id.
	modelHeaders map[string]map[string]string
	// row is one row with this endpoint, for the dry run of the key source.
	row providers.Model
}

// userModels is the part of one snapshot that the user models.json gives.
type userModels struct {
	path string
	// entries are the endpoints that the file configures.
	entries map[modelKey]userEntry
	// known are the endpoints of every row of the snapshot.
	known map[modelKey]bool
	// former are the endpoints that the tables of this process had in any
	// snapshot. A row with such an endpoint came from the file.
	former map[modelKey]bool
	// problems are the problems of the file and of its mapping.
	problems []settings.Diagnostic
}

// userFile is one result of the loader of the user file.
type userFile struct {
	path     string
	file     settings.ModelsFile
	problems []settings.Diagnostic
	existing []string
}

// existingProviders lists the providers that exist without the user file: the
// providers of the catalog rows base and of the registered sign-in methods. A
// provider of the file that is not in this list is one that the file defines.
func existingProviders(base []providers.Model, service *auth.Service) []string {
	out := providerIDs(base)
	if service != nil {
		for _, m := range service.Methods {
			if !slices.Contains(out, m.Provider) {
				out = append(out, m.Provider)
			}
		}
	}
	slices.Sort(out)
	return out
}

// modelMapping maps the user file to the input of NewModelSet. It keeps the
// file position of each provider and model, so that a problem names the
// place in the file.
type modelMapping struct {
	config providers.ModelConfig
	path   string
	// providerPos and modelPos are the file positions of
	// config.Providers[i] and of its Models[j].
	providerPos []int
	modelPos    [][]int
	problems    []settings.Diagnostic
}

func mapModelsFile(f userFile) modelMapping {
	mm := modelMapping{path: f.path}
	ids := slices.SortedFunc(maps.Keys(f.file.Providers), func(a, b string) int {
		return f.file.Providers[a].Position - f.file.Providers[b].Position
	})
	for _, id := range ids {
		e := f.file.Providers[id]
		key := fmt.Sprintf("providers[%d]", e.Position)
		pc := providers.ProviderConfig{ID: id, BaseURL: e.BaseURL, API: providers.API(e.API)}
		if e.Compat != nil {
			pc.Compat = mm.compat(key+".compat", e.Compat, pc.API)
		}
		positions := make([]int, 0, len(e.Models))
		for _, m := range e.Models {
			mkey := fmt.Sprintf("%s.models[%d]", key, m.Position)
			api := providers.API(m.API)
			if api == "" {
				api = pc.API
			}
			spec := providers.ModelSpec{
				ID: m.ID, Name: m.Name, API: providers.API(m.API), Reasoning: m.Reasoning,
				ThinkingLevelMap: thinkingLevels(m.ThinkingLevelMap), Input: m.Input,
				ContextWindow: m.ContextWindow, MaxTokens: m.MaxTokens,
			}
			if m.Cost != nil {
				spec.Cost = flatPrice(mm.price(mkey+".cost", m.Cost))
			}
			if m.Compat != nil {
				spec.Compat = mm.compat(mkey+".compat", m.Compat, api)
			}
			pc.Models = append(pc.Models, spec)
			positions = append(positions, m.Position)
		}
		if len(e.ModelOverrides) > 0 {
			pc.Overrides = map[string]providers.ModelPatch{}
		}
		okey := key + ".modelOverrides"
		defined := !slices.Contains(f.existing, id)
		for _, modelID := range slices.Sorted(maps.Keys(e.ModelOverrides)) {
			o := e.ModelOverrides[modelID]
			if defined && !slices.ContainsFunc(e.Models, func(m settings.ModelEntry) bool { return m.ID == modelID }) {
				mm.problem(okey, "the override names no model of this provider; it is ignored")
				continue
			}
			if o.Reasoning != nil {
				mm.problem(okey+".reasoning", "an override cannot change reasoning; use thinkingLevelMap")
			}
			pc.Overrides[modelID] = mm.patch(okey, o, overrideAPI(e, modelID, o))
		}
		mm.config.Providers = append(mm.config.Providers, pc)
		mm.providerPos = append(mm.providerPos, e.Position)
		mm.modelPos = append(mm.modelPos, positions)
	}
	return mm
}

// overrideAPI is the API that decodes the compat of an override: the api of
// the override, else of the provider, else of the file row with that id.
func overrideAPI(e settings.ProviderEntry, modelID string, o settings.ModelOverride) providers.API {
	if o.API != "" {
		return providers.API(o.API)
	}
	if e.API != "" {
		return providers.API(e.API)
	}
	for _, m := range e.Models {
		if m.ID == modelID && m.API != "" {
			return providers.API(m.API)
		}
	}
	return ""
}

func (mm *modelMapping) problem(key, text string) {
	mm.problems = append(mm.problems, settings.Diagnostic{Layer: settings.LayerUser, Path: mm.path, Key: key, Text: text})
}

func (mm *modelMapping) patch(key string, o settings.ModelOverride, api providers.API) providers.ModelPatch {
	p := providers.ModelPatch{API: providers.API(o.API), Input: o.Input, ThinkingLevelMap: thinkingLevels(o.ThinkingLevelMap)}
	if o.Name != "" {
		p.Name = &o.Name
	}
	if o.ContextWindow > 0 {
		p.ContextWindow = &o.ContextWindow
	}
	if o.MaxTokens > 0 {
		p.MaxTokens = &o.MaxTokens
	}
	if o.Cost != nil {
		price := mm.price(key+".cost", o.Cost)
		p.Cost = &price
	}
	if o.Compat != nil {
		p.Compat = mm.compat(key+".compat", o.Compat, api)
	}
	return p
}

// price maps the prices of the file, in USD per 1_000_000 tokens, to micro-USD.
// A price above the bound is a problem and is left out.
func (mm *modelMapping) price(key string, c *settings.CostEntry) providers.PricePatch {
	var out providers.PricePatch
	for _, f := range []struct {
		name string
		usd  *float64
		dst  **int64
	}{{"input", c.Input, &out.Input}, {"output", c.Output, &out.Output}, {"cacheRead", c.CacheRead, &out.CacheRead}, {"cacheWrite", c.CacheWrite, &out.CacheWrite}} {
		if f.usd == nil {
			continue
		}
		if *f.usd > maxFilePriceUSD {
			mm.problem(key+"."+f.name, "the price is too large; it is ignored")
			continue
		}
		v := int64(math.Round(*f.usd * 1_000_000))
		*f.dst = &v
	}
	return out
}

// compat decodes a raw compat object for api. The keys are the field names of
// the compat record of api with a lower-case first letter. A key that the
// record does not have, or a value of the wrong type, is a problem and is
// left out.
func (mm *modelMapping) compat(key string, raw map[string]json.RawMessage, api providers.API) providers.Compat {
	var target any
	switch api {
	case providers.APIAnthropicMessages:
		target = &providers.AnthropicCompat{}
	case providers.APIOpenAICompletions:
		target = &providers.CompletionsCompat{}
	case providers.APIOpenAIResponses:
		target = &providers.ResponsesCompat{}
	default:
		mm.problem(key, "compat needs a supported api; it is ignored")
		return nil
	}
	v := reflect.ValueOf(target).Elem()
	for _, name := range slices.Sorted(maps.Keys(raw)) {
		field := v.FieldByName(exportedName(name))
		if name == "" || !field.IsValid() || !unicode.IsLower(rune(name[0])) {
			mm.problem(key, "unknown compat key for this api; it is ignored")
			continue
		}
		if err := json.Unmarshal(raw[name], field.Addr().Interface()); err != nil {
			mm.problem(key, "a compat value has the wrong type; it is ignored")
		}
	}
	return v.Interface().(providers.Compat)
}

// flatPrice is the price of a new row: a price that p omits is zero.
func flatPrice(p providers.PricePatch) providers.Price {
	var out providers.Price
	for _, f := range []struct {
		v   *int64
		dst *int64
	}{{p.Input, &out.Input}, {p.Output, &out.Output}, {p.CacheRead, &out.CacheRead}, {p.CacheWrite, &out.CacheWrite}} {
		if f.v != nil {
			*f.dst = *f.v
		}
	}
	return out
}

func exportedName(name string) string {
	if name == "" {
		return ""
	}
	return strings.ToUpper(name[:1]) + name[1:]
}

func thinkingLevels(in map[string]*string) providers.ThinkingLevelMap {
	if in == nil {
		return nil
	}
	out := make(providers.ThinkingLevelMap, len(in))
	for level, word := range in {
		out[protocol.ThinkingLevel(level)] = word
	}
	return out
}

// configProblems maps the problems of NewModelSet to diagnostics with the
// file positions of the user file.
func (mm *modelMapping) configProblems(problems []providers.ConfigProblem) []settings.Diagnostic {
	out := make([]settings.Diagnostic, 0, len(problems))
	for _, p := range problems {
		out = append(out, settings.Diagnostic{Layer: settings.LayerUser, Path: mm.path, Key: mm.filePath(p.Path), Text: p.Text})
	}
	return out
}

// filePath replaces the list positions of the leading "providers[i]" and
// ".models[j]" of a problem path with the positions of the file.
func (mm *modelMapping) filePath(path string) string {
	i, rest, ok := cutIndex(path, "providers[")
	if !ok || i >= len(mm.providerPos) {
		return path
	}
	out := fmt.Sprintf("providers[%d]", mm.providerPos[i])
	if j, tail, ok := cutIndex(rest, ".models["); ok && j < len(mm.modelPos[i]) {
		return out + fmt.Sprintf(".models[%d]", mm.modelPos[i][j]) + tail
	}
	return out + rest
}

// cutIndex reads "<prefix><n>]" at the start of s.
func cutIndex(s, prefix string) (n int, rest string, ok bool) {
	after, found := strings.CutPrefix(s, prefix)
	if !found {
		return 0, s, false
	}
	end := strings.IndexByte(after, ']')
	if end < 0 {
		return 0, s, false
	}
	n, err := strconv.Atoi(after[:end])
	if err != nil || n < 0 {
		return 0, s, false
	}
	return n, after[end+1:], true
}

// userTables builds the key and header tables from the rows of a build. A
// provider entry of the file configures the endpoint of each row of that
// provider when it sets a base URL, an apiKey or headers, or when the row is
// one of its models. An override with headers configures the endpoint of its
// row. former is the set of the previous snapshot, or nil.
func userTables(f userFile, rows []providers.Model, former map[modelKey]bool) userModels {
	u := userModels{path: f.path, entries: map[modelKey]userEntry{}, known: map[modelKey]bool{}, former: maps.Clone(former)}
	if u.former == nil {
		u.former = map[modelKey]bool{}
	}
	for _, r := range rows {
		k := keyOf(r)
		u.known[k] = true
		e, ok := f.file.Providers[r.Provider]
		if !ok {
			continue
		}
		fileRow := slices.IndexFunc(e.Models, func(m settings.ModelEntry) bool {
			return m.ID == r.ID && providers.API(cmp.Or(m.API, e.API)) == r.API
		})
		overrideHeaders := e.ModelOverrides[r.ID].Headers
		providerWide := e.BaseURL != "" || e.APIKey != "" || len(e.Headers) > 0
		if !providerWide && fileRow < 0 && len(overrideHeaders) == 0 {
			continue
		}
		entry, seen := u.entries[k]
		if !seen {
			entry = userEntry{row: r, modelHeaders: map[string]map[string]string{}}
		}
		if providerWide || fileRow >= 0 {
			entry.apiKey, entry.headers = e.APIKey, e.Headers
			entry.endpoint = cmp.Or(e.BaseURL, r.BaseURL)
		}
		headers := map[string]string{}
		if fileRow >= 0 {
			maps.Copy(headers, e.Models[fileRow].Headers)
		}
		maps.Copy(headers, overrideHeaders)
		if len(headers) > 0 {
			entry.modelHeaders[r.ID] = headers
		}
		u.entries[k] = entry
		u.former[k] = true
	}
	return u
}

// lookupState is how the user file applies to the row of a request.
type lookupState int

const (
	// lookupPlain: the file does not configure the endpoint; the built-in
	// rules bind the key.
	lookupPlain lookupState = iota
	// lookupConfig: the file configures the endpoint.
	lookupConfig
	// lookupChanged: the file made the endpoint and no longer has it.
	lookupChanged
)

// lookup finds the file entry of the endpoint of m. A nil snapshot has no
// file.
func (s *modelSnapshot) lookup(m providers.Model) (userEntry, lookupState) {
	if s == nil {
		return userEntry{}, lookupPlain
	}
	k := keyOf(m)
	if e, ok := s.user.entries[k]; ok {
		return e, lookupConfig
	}
	if s.user.former[k] && !s.user.known[k] {
		return userEntry{}, lookupChanged
	}
	return userEntry{}, lookupPlain
}

// notInConfig reports whether m is a row that the user file made and that the
// current file no longer has.
func (s *modelSnapshot) notInConfig(m providers.Model) bool {
	_, state := s.lookup(m)
	return state == lookupChanged
}

// configKey is the models.json key of the entry, or nil. Its endpoint comes
// from the file, so auth refuses the key for a row of another endpoint.
func (e userEntry) configKey() *auth.ConfigKey {
	if e.apiKey == "" {
		return nil
	}
	return &auth.ConfigKey{Value: e.apiKey, Endpoint: e.endpoint}
}

// fileUsable reports whether the snapshot has a file key for provider that the
// key source selects. The check is a dry run: it never runs a command, and a
// command counts as usable; a failed run is reported at the request.
func (s *modelSnapshot) fileUsable(ctx context.Context, service *auth.Service, provider string) bool {
	if s == nil || service == nil {
		return false
	}
	for _, e := range s.user.entries {
		if e.row.Provider != provider || e.apiKey == "" {
			continue
		}
		src, err := service.KeySource(ctx, auth.ResolveRequest{Model: e.row, Config: e.configKey()})
		if err == nil && src.Kind == auth.SourceConfig {
			return true
		}
	}
	return false
}

// usableRule is the one rule of every usable check: a provider is usable when
// the credential store says so, or when the stored snapshot has a file key
// for it. The demo provider of initial is always usable.
func usableRule(initial providers.Model, service *auth.Service, stored func() *modelSnapshot) func(ctx context.Context, provider string) bool {
	return func(ctx context.Context, provider string) bool {
		if initial.ID != "" && provider == initial.Provider {
			return true
		}
		return providerUsable(ctx, service, provider) || stored().fileUsable(ctx, service, provider)
	}
}

// requestModel is a request-local copy of m with the expanded file headers.
// commands are the value commands that the headers ran. The copy never goes
// back into the agent state or the snapshot.
func requestModel(ctx context.Context, values *auth.Values, e userEntry, m providers.Model) (providers.Model, []string, error) {
	templates := map[string]string{}
	maps.Copy(templates, e.headers)
	maps.Copy(templates, e.modelHeaders[m.ID])
	if len(templates) == 0 {
		return m, nil, nil
	}
	out := m
	out.Headers = maps.Clone(m.Headers)
	if out.Headers == nil {
		out.Headers = map[string]string{}
	}
	var commands []string
	for _, name := range slices.Sorted(maps.Keys(templates)) {
		raw := templates[name]
		v, err := values.Expand(ctx, m.Provider+" header", raw)
		if err != nil {
			return providers.Model{}, nil, err
		}
		out.Headers[name] = v
		if command, ok := strings.CutPrefix(raw, "!"); ok {
			commands = append(commands, command)
		}
	}
	return out, commands, nil
}

// keyCommand is the command of an apiKey template, or "".
func keyCommand(raw string) string {
	command, _ := strings.CutPrefix(raw, "!")
	if command == raw {
		return ""
	}
	return command
}

// ModelView is the model list of a process together with the key and header
// tables of the user models.json. The request path reads it with an atomic
// load: no file access, no lock and no build. A nil view has no file.
type ModelView struct {
	load func() *modelSnapshot
}

func (v *ModelView) snapshot() *modelSnapshot {
	if v == nil || v.load == nil {
		return nil
	}
	return v.load()
}

// Set returns the model list of the view.
func (v *ModelView) Set() *providers.ModelSet {
	if s := v.snapshot(); s != nil {
		return s.set
	}
	return nil
}

// Problems returns the problems of the user models.json and of its mapping.
func (v *ModelView) Problems() []settings.Diagnostic {
	if s := v.snapshot(); s != nil {
		return slices.Clone(s.user.problems)
	}
	return nil
}

// ModelsFile states for ModelsFileState.
const (
	ModelsFileLoaded  = "loaded"
	ModelsFileProblem = "problem"
	ModelsFileMissing = "missing"
)

// ModelsFileState returns the path of the user models.json and its state:
// "missing" when there is no file, "problem" when the file has a problem, else
// "loaded".
func (v *ModelView) ModelsFileState() (path, state string) {
	s := v.snapshot()
	if s == nil {
		return "", ModelsFileMissing
	}
	path = s.user.path
	switch {
	case len(s.user.problems) > 0:
		return path, ModelsFileProblem
	case !fileExists(path):
		return path, ModelsFileMissing
	}
	return path, ModelsFileLoaded
}

func fileExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// ProviderKeySource is the key source of one provider.
type ProviderKeySource struct {
	Provider string
	// Source is the safe source name of auth.Source, or "none" when no source
	// has a key, or "problem" when a value of the user file cannot give one.
	Source string
	// Problem is the safe text of a value problem. It names a variable or a
	// label, never a value.
	Problem string
}

// KeySources names, for each provider of the model list, the source that a
// request on its first row would use. It uses the same source selection as
// the request, as a dry run: it never runs a command and never shows a value.
func (v *ModelView) KeySources(ctx context.Context, service *auth.Service) []ProviderKeySource {
	s := v.snapshot()
	if s == nil {
		return nil
	}
	var out []ProviderKeySource
	seen := map[string]bool{}
	for _, m := range s.set.All() {
		if seen[m.Provider] {
			continue
		}
		seen[m.Provider] = true
		e, _ := s.lookup(m)
		row := ProviderKeySource{Provider: m.Provider}
		src, err := service.KeySource(ctx, auth.ResolveRequest{Model: m, Config: e.configKey()})
		switch {
		case err == nil:
			row.Source = src.Name
		case errors.Is(err, auth.ErrValue):
			row.Source, row.Problem = "problem", err.Error()
		default:
			row.Source = "none"
		}
		out = append(out, row)
	}
	return out
}

// LoadModelView reads the user models.json once and builds the model list from
// the cache in home, the catalog and that file, with no network call. A
// process with no refresher (`ask -p`, `ask settings show`) uses it. A nil
// service gives unknown account access and the catalog providers only.
func LoadModelView(ctx context.Context, home string, service *auth.Service) (*ModelView, error) {
	base := providers.GeneratedCatalog()
	existing := existingProviders(base, service)
	files, err := settings.NewModelsFiles(home, existing)
	if err != nil {
		return nil, err
	}
	file, problems, _ := files.Load()
	// A home that cannot hold a cache gives the catalog alone.
	cache, _ := settings.NewModelCache(home)
	log := zap.NewNop()
	f := userFile{path: files.Path(), file: file, problems: problems, existing: existing}
	snap := buildModelSnapshot(ctx, base, readModelCache(ctx, cache, log), service, log, f, nil)
	return &ModelView{load: func() *modelSnapshot { return &snap }}, nil
}
