package providers

import (
	"cmp"
	"maps"
	"regexp"
	"slices"
	"strings"
)

// AccountView is what the model list of one provider account tells about
// access.
type AccountView struct {
	// Known is true when the list of the account was read. When it is false,
	// access is unknown: every row of the provider stays, because inference
	// can still work.
	Known bool
	// IDs are the model ids that the vendor list gives, in any form: an alias
	// or a dated snapshot.
	IDs []string
}

// ModelSet is the merged model list that a session can select from. It is an
// immutable snapshot: a holder replaces the whole set to change it, and every
// method returns rows that share no storage with the set. A nil set has no
// rows.
type ModelSet struct {
	rows []Model
}

// NewModelSet merges the rows of the catalog (normally GeneratedCatalog), the
// metadata overlay (rows from MapModelsDev, so each one already matches a
// shown family rule) and the account lists, keyed by provider.
//
//   - The first row of a provider, id and API wins. An overlay row only adds a
//     model that base lacks; it never replaces a base row.
//   - A provider with no entry, or with Known false, keeps all its rows.
//   - A provider with Known true keeps only the rows that the list names,
//     directly or through an alias. A known empty list keeps no row, also no
//     hand-written row: the vendor said that the account has no model.
//   - A listed id with no row gets a fallback row when a shown family rule
//     matches it. Ask cannot build a correct request for any other id, so
//     such an id gets no row.
//
// The rows are in the display order of Ask, never in the vendor order.
func NewModelSet(base, overlay []Model, accounts map[string]AccountView) *ModelSet {
	seen := map[Ref]bool{}
	var merged []Model
	for _, m := range slices.Concat(base, overlay) {
		if k := refOf(m); !seen[k] {
			seen[k] = true
			merged = append(merged, cloneModel(m))
		}
	}
	rows := slices.DeleteFunc(merged, func(m Model) bool {
		v := accounts[m.Provider]
		return v.Known && !slices.ContainsFunc(v.IDs, func(id string) bool { return sameModelID(m.ID, id) })
	})
	for _, provider := range slices.Sorted(maps.Keys(accounts)) {
		v := accounts[provider]
		if !v.Known {
			continue
		}
		// Sorted ids put an alias before its dated snapshots, so a listed
		// snapshot matches the fallback row of its listed alias and does not
		// add a second row.
		listed := slices.Compact(slices.Sorted(slices.Values(v.IDs)))
		for _, id := range listed {
			if slices.ContainsFunc(rows, func(m Model) bool { return m.Provider == provider && sameModelID(m.ID, id) }) {
				continue
			}
			if m, ok := fallbackRow(provider, id); ok {
				rows = append(rows, m)
			}
		}
	}
	sortForDisplay(rows)
	return &ModelSet{rows: rows}
}

// All returns every row in display order.
func (s *ModelSet) All() []Model {
	if s == nil {
		return nil
	}
	out := make([]Model, 0, len(s.rows))
	for _, m := range s.rows {
		out = append(out, cloneModel(m))
	}
	return out
}

// Models returns the rows of one provider in display order.
func (s *ModelSet) Models(provider string) []Model {
	if s == nil {
		return nil
	}
	var out []Model
	for _, m := range s.rows {
		if m.Provider == provider {
			out = append(out, cloneModel(m))
		}
	}
	return out
}

// Find returns the one row for ref, with the rules and errors of the
// package-level Find: an empty API that matches two rows (Token Plan) is an
// error that names both APIs.
func (s *ModelSet) Find(ref Ref) (Model, error) {
	var rows []Model
	if s != nil {
		rows = s.rows
	}
	m, err := findIn(rows, ref)
	if err != nil {
		return Model{}, err
	}
	return cloneModel(m), nil
}

// Contains is true when a row has the provider and id of ref, and the API of
// ref or any API when ref has none. Unlike Find, an id on two APIs is not an
// error: the set has the model.
func (s *ModelSet) Contains(ref Ref) bool {
	if s == nil {
		return false
	}
	return slices.ContainsFunc(s.rows, func(m Model) bool {
		return m.Provider == ref.Provider && m.ID == ref.ID && (ref.API == "" || m.API == ref.API)
	})
}

func refOf(m Model) Ref { return Ref{Provider: m.Provider, ID: m.ID, API: m.API} }

// fallbackRow builds the row of a listed id that no source describes. The
// family rule gives the request data; the limits stay 0 and the cost is
// unknown, so a display shows them as unknown and RequestLimits gives the
// request the family defaults. The id is vendor data, so it passes the same
// text check as a models.dev id.
func fallbackRow(provider, id string) (Model, bool) {
	if !validModelText(id) {
		return Model{}, false
	}
	rule, ok := ruleFor(provider, id)
	if !ok || rule.hidden != "" {
		return Model{}, false
	}
	reasoning, levels, ok := rule.thinkingOf(true, rule.words)
	if !ok {
		return Model{}, false
	}
	m := Model{
		ID: id, Name: id, Provider: provider,
		API: rule.api, BaseURL: rule.baseURL, Compat: rule.compat,
		Reasoning: reasoning, ThinkingLevelMap: levels,
		Input:       []string{"text"},
		CostUnknown: true,
	}
	if Validate(m) != nil {
		return Model{}, false
	}
	return m, true
}

// datedID matches the dated snapshot form of a model id: Anthropic adds
// -YYYYMMDD and OpenAI adds -YYYY-MM-DD to the alias.
var datedID = regexp.MustCompile(`^(.+)-(\d{8}|\d{4}-\d{2}-\d{2})$`)

// splitDate returns the alias of a dated id and its date as YYYYMMDD. An id
// with no date returns itself and an empty date.
func splitDate(id string) (alias, date string) {
	if g := datedID.FindStringSubmatch(id); g != nil {
		return g[1], strings.ReplaceAll(g[2], "-", "")
	}
	return id, ""
}

// sameModelID is true when a and b are the same id, or when one is a dated
// snapshot of the other. Two different snapshots are different models.
func sameModelID(a, b string) bool {
	if a == b {
		return true
	}
	aAlias, aDate := splitDate(a)
	bAlias, bDate := splitDate(b)
	return (aDate != "" && bDate == "" && aAlias == b) || (bDate != "" && aDate == "" && bAlias == a)
}

// displayKey holds the parts of the display order of one row.
type displayKey struct {
	provider int
	rule     int
	version  []string
	alias    string
	date     string
}

// digitRuns finds the version numbers in an id.
var digitRuns = regexp.MustCompile(`\d+`)

// sortForDisplay sorts rows by provider (the order of the compiled catalog),
// family rule (the order of the rule table; no rule last), version (newest
// first), alias (an alias before its snapshots, newest snapshot first), id
// and API. Every row has a fixed place, so the order never depends on the
// order of the input.
func sortForDisplay(rows []Model) {
	var providerOrder []string
	for _, m := range catalog() {
		if !slices.Contains(providerOrder, m.Provider) {
			providerOrder = append(providerOrder, m.Provider)
		}
	}
	keys := make(map[Ref]displayKey, len(rows))
	for _, m := range rows {
		k := displayKey{provider: slices.Index(providerOrder, m.Provider), rule: ruleIndex(m.Provider, m.ID)}
		if k.provider < 0 {
			k.provider = len(providerOrder)
		}
		if k.rule < 0 {
			k.rule = len(familyRules)
		}
		k.alias, k.date = splitDate(m.ID)
		k.version = digitRuns.FindAllString(k.alias, -1)
		keys[refOf(m)] = k
	}
	slices.SortFunc(rows, func(a, b Model) int {
		ka, kb := keys[refOf(a)], keys[refOf(b)]
		return cmp.Or(
			cmp.Compare(ka.provider, kb.provider),
			strings.Compare(a.Provider, b.Provider),
			cmp.Compare(ka.rule, kb.rule),
			-compareVersions(ka.version, kb.version),
			strings.Compare(ka.alias, kb.alias),
			compareDates(ka.date, kb.date),
			strings.Compare(a.ID, b.ID),
			strings.Compare(string(a.API), string(b.API)),
		)
	})
}

// compareVersions compares two lists of decimal numbers part by part. A list
// that has more parts after an equal prefix is the larger one, so 5.5 comes
// after 5.
func compareVersions(a, b []string) int {
	for i := range min(len(a), len(b)) {
		if c := compareDecimal(a[i], b[i]); c != 0 {
			return c
		}
	}
	return cmp.Compare(len(a), len(b))
}

// compareDecimal compares two digit strings as numbers of any length.
func compareDecimal(a, b string) int {
	a, b = strings.TrimLeft(a, "0"), strings.TrimLeft(b, "0")
	return cmp.Or(cmp.Compare(len(a), len(b)), strings.Compare(a, b))
}

// compareDates puts no date (the alias) first, then the newest date.
func compareDates(a, b string) int {
	switch {
	case a == b:
		return 0
	case a == "":
		return -1
	case b == "":
		return 1
	default:
		return -strings.Compare(a, b)
	}
}

// cloneModel returns a copy of m that shares no storage that a caller can
// change. SamplingParams values are copied one level deep.
func cloneModel(m Model) Model {
	m.Input = slices.Clone(m.Input)
	m.Headers = maps.Clone(m.Headers)
	m.SamplingParams = maps.Clone(m.SamplingParams)
	m.Cost.Tiers = slices.Clone(m.Cost.Tiers)
	if m.ThinkingLevelMap != nil {
		levels := make(ThinkingLevelMap, len(m.ThinkingLevelMap))
		for l, w := range m.ThinkingLevelMap {
			if w != nil {
				w = stringPointer(*w)
			}
			levels[l] = w
		}
		m.ThinkingLevelMap = levels
	}
	return m
}
