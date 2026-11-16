package providers

import (
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"

	"AskCore/pkg/protocol"
)

// ErrUnknownModel is the class of a model name or pattern that matches no row.
var ErrUnknownModel = errors.New("unknown model")

// ErrAmbiguousModel is the class of a model name or pattern that matches more
// than one row. The *ModelError gives the choices.
var ErrAmbiguousModel = errors.New("ambiguous model")

// ModelError is the error of a model lookup. errors.Is finds its class
// (ErrUnknownModel or ErrAmbiguousModel).
type ModelError struct {
	Kind error
	// Choices are the "provider/id@api" names of the rows that an ambiguous
	// name matches, in display order. It is empty for an unknown model.
	Choices []string
	text    string
}

func (e *ModelError) Error() string { return e.text }

func (e *ModelError) Unwrap() error { return e.Kind }

func unknownModel(text string) *ModelError {
	return &ModelError{Kind: ErrUnknownModel, text: text}
}

func ambiguousModel(text string, rows []Model) *ModelError {
	choices := make([]string, 0, len(rows))
	for _, m := range rows {
		choices = append(choices, rowName(m))
	}
	return &ModelError{Kind: ErrAmbiguousModel, Choices: choices, text: text}
}

func rowName(m Model) string { return m.Provider + "/" + m.ID + "@" + string(m.API) }

var errRefSyntax = errors.New("model name is not [provider/]id[@api]")

// ParseRef splits a model name of the form [provider/]id[@api]. The provider
// ends at the first "/" and the API starts after the last "@". A name with no
// "/" gives an empty provider, and a name with no "@" gives an empty API. An
// empty part next to a separator is an error. ParseRef reads no rows, so it
// does not know if the provider exists.
func ParseRef(s string) (Ref, error) {
	var ref Ref
	rest := s
	if provider, after, ok := strings.Cut(s, "/"); ok {
		if provider == "" || after == "" {
			return Ref{}, errRefSyntax
		}
		ref.Provider, rest = provider, after
	}
	if i := strings.LastIndex(rest, "@"); i >= 0 {
		if i == 0 || i == len(rest)-1 {
			return Ref{}, errRefSyntax
		}
		rest, ref.API = rest[:i], API(rest[i+1:])
	}
	if rest == "" {
		return Ref{}, errRefSyntax
	}
	ref.ID = rest
	return ref, nil
}

// PatternOptions changes how ResolvePattern reads a pattern.
type PatternOptions struct {
	// AllowInvalidLevel makes a suffix after the last ":" that is not a
	// thinking level a warning: the text before the colon is resolved and the
	// default level is used. When it is false, such a pattern is an unknown
	// model, so a typo on the command line does not silently start a session
	// with another level.
	AllowInvalidLevel bool
}

// Resolved is the result of ResolvePattern.
type Resolved struct {
	Model Model
	// Level is the thinking level that the pattern names, or empty. It is not
	// clamped: the caller applies an explicit level option first and then
	// ClampThinkingLevel of the model.
	Level protocol.ThinkingLevel
	// Warning is set when the pattern had an invalid level suffix that
	// AllowInvalidLevel accepted.
	Warning string
}

// ResolvePattern finds the one row that pattern names. The pattern has the
// form [provider/]id[@api][:level]. rows must be in display order, as
// ModelSet.All returns them; the first row in that order wins a fuzzy match.
//
// The steps are:
//
//  1. The whole pattern as an exact name, so a literal id such as
//     "qwen3-coder:30b" or "x:max" wins over a level suffix.
//  2. When the text after the last ":" is a thinking level: an alias or fuzzy
//     match whose real id ends in ":<level>" wins with no level. Otherwise the
//     text before the colon is resolved with all steps and the result gets the
//     level, so "kimi:high" never becomes "kimi-highspeed".
//  3. Otherwise an alias match (an alias and its dated snapshots), then a
//     fuzzy match (a case-insensitive part of the id or the name).
//  4. A pattern with an invalid level suffix that matched nothing: see
//     PatternOptions.AllowInvalidLevel.
//
// The first part of the pattern counts as a provider only when a row has that
// provider; else the whole text is an id, so a vendor id such as "qwen/qwen3"
// still matches. Exact steps compare the provider and the id exactly; alias
// and fuzzy steps fold case. A match in more than one provider, two rows that
// are equal after case folding, and one id on two APIs with no "@api" are
// ErrAmbiguousModel. No match is ErrUnknownModel.
func ResolvePattern(rows []Model, pattern string, opts PatternOptions) (Resolved, error) {
	if _, err := ParseRef(pattern); err != nil {
		return Resolved{}, err
	}
	r, ok, err := resolvePattern(rows, pattern, opts)
	if err != nil {
		return Resolved{}, err
	}
	if !ok {
		return Resolved{}, unknownPattern(rows, pattern)
	}
	return Resolved{Model: cloneModel(r.Model), Level: r.Level, Warning: r.Warning}, nil
}

// resolvePattern applies the steps of ResolvePattern. ok is false when no row
// matches; err is set only for a pattern that is ambiguous or malformed.
func resolvePattern(rows []Model, pattern string, opts PatternOptions) (Resolved, bool, error) {
	if m, ok, err := exactMatch(rows, pattern); err != nil || ok {
		return Resolved{Model: m}, ok, err
	}
	if base, level, ok := splitLevel(pattern); ok {
		// A fuzzy match that is ambiguous here is not an error: the text
		// before the colon can still name one row.
		if m, ok, err := looseMatch(rows, pattern); err == nil && ok &&
			strings.HasSuffix(strings.ToLower(m.ID), ":"+string(level)) {
			return Resolved{Model: m}, true, nil
		}
		if _, err := ParseRef(base); err != nil {
			return Resolved{}, false, nil
		}
		r, ok, err := resolvePattern(rows, base, opts)
		if err != nil || !ok {
			return Resolved{}, ok, err
		}
		if r.Warning == "" {
			r.Level = level
		}
		return r, true, nil
	}
	if m, ok, err := looseMatch(rows, pattern); err != nil || ok {
		return Resolved{Model: m}, ok, err
	}
	i := strings.LastIndex(pattern, ":")
	if i < 0 || !opts.AllowInvalidLevel {
		return Resolved{}, false, nil
	}
	prefix, suffix := pattern[:i], pattern[i+1:]
	if _, err := ParseRef(prefix); err != nil {
		return Resolved{}, false, nil
	}
	r, ok, err := resolvePattern(rows, prefix, opts)
	if err != nil || !ok {
		return Resolved{}, ok, err
	}
	return Resolved{Model: r.Model, Warning: fmt.Sprintf("invalid thinking level %q; the default level is used", suffix)}, true, nil
}

// unknownPattern builds the ErrUnknownModel of pattern. When the text before
// the last colon names a row, the suffix is the problem, so the text lists the
// thinking levels.
func unknownPattern(rows []Model, pattern string) error {
	text := fmt.Sprintf("unknown model %q", pattern)
	if i := strings.LastIndex(pattern, ":"); i >= 0 {
		prefix, suffix := pattern[:i], pattern[i+1:]
		if _, err := ParseRef(prefix); err == nil {
			if _, ok, err := resolvePattern(rows, prefix, PatternOptions{}); err == nil && ok {
				text += fmt.Sprintf(": %q is not a thinking level (%s)", suffix, levelWords())
			}
		}
	}
	return unknownModel(text)
}

// patternLevels are the words of a ":level" suffix: default (no level is
// chosen), then the clamp order. thinkingRank must not get default, because
// ClampThinkingLevel never sees it.
var patternLevels = append([]protocol.ThinkingLevel{protocol.ThinkingDefault}, thinkingRank...)

func splitLevel(pattern string) (base string, level protocol.ThinkingLevel, ok bool) {
	i := strings.LastIndex(pattern, ":")
	if i < 0 {
		return "", "", false
	}
	level = protocol.ThinkingLevel(pattern[i+1:])
	if !slices.Contains(patternLevels, level) {
		return "", "", false
	}
	return pattern[:i], level, true
}

func levelWords() string {
	words := make([]string, 0, len(patternLevels))
	for _, l := range patternLevels {
		words = append(words, string(l))
	}
	return strings.Join(words, ", ")
}

type candidate struct {
	rows []Model
	id   string
	api  API
}

// readPattern reads text as [provider/]id[@api] against rows. The first part
// is a provider only when a row has that provider (with fold, compared
// without case); else the whole text before "@" is the id. The rows of the
// result are the rows of that provider, or all rows, with the API of the
// pattern when it has one.
func readPattern(rows []Model, text string, fold bool) (candidate, bool) {
	ref, err := ParseRef(text)
	if err != nil {
		return candidate{}, false
	}
	same := func(a, b string) bool { return a == b || (fold && strings.EqualFold(a, b)) }
	c := candidate{id: ref.ID, api: ref.API}
	if ref.Provider == "" || !slices.ContainsFunc(rows, func(m Model) bool { return same(m.Provider, ref.Provider) }) {
		if ref.Provider != "" {
			c.id = ref.Provider + "/" + ref.ID
		}
		ref.Provider = ""
	}
	for _, m := range rows {
		if (ref.Provider == "" || same(m.Provider, ref.Provider)) && (ref.API == "" || m.API == ref.API) {
			c.rows = append(c.rows, m)
		}
	}
	return c, true
}

// exactMatch is the exact step: the provider and the id are compared exactly,
// and findIn decides between the APIs of one id. A bare id is looked up in
// every provider that has it.
func exactMatch(rows []Model, text string) (Model, bool, error) {
	c, ok := readPattern(rows, text, false)
	if !ok {
		return Model{}, false, nil
	}
	var providers []string
	for _, m := range c.rows {
		if m.ID == c.id && !slices.Contains(providers, m.Provider) {
			providers = append(providers, m.Provider)
		}
	}
	return pickProvider(rows, text, c, providers)
}

// pickProvider finishes a match: one provider must have the id, and findIn
// gives the row of that provider and id with the API of the pattern.
func pickProvider(rows []Model, text string, c candidate, providers []string) (Model, bool, error) {
	switch len(providers) {
	case 0:
		return Model{}, false, nil
	case 1:
		m, err := findIn(rows, Ref{Provider: providers[0], ID: c.id, API: c.api})
		if err != nil {
			return Model{}, false, err
		}
		return m, true, nil
	default:
		var matched []Model
		for _, m := range c.rows {
			if m.ID == c.id {
				matched = append(matched, m)
			}
		}
		return Model{}, false, ambiguousModel(fmt.Sprintf("ambiguous model %q: %s", text, strings.Join(namesOf(matched), ", ")), matched)
	}
}

func namesOf(rows []Model) []string {
	out := make([]string, 0, len(rows))
	for _, m := range rows {
		out = append(out, rowName(m))
	}
	return out
}

// looseMatch is the alias step and then the fuzzy step. Both fold case. Each
// step keeps the rows that match; rows of more than one provider are
// ambiguous. The first matching row in display order names the id, and
// pickProvider gives the row of that id with the API of the pattern.
func looseMatch(rows []Model, text string) (Model, bool, error) {
	c, ok := readPattern(rows, text, true)
	if !ok {
		return Model{}, false, nil
	}
	want := strings.ToLower(c.id)
	steps := []func(Model) bool{
		// An alias or a dated snapshot of the id. The alias comes first.
		func(m Model) bool { return sameModelID(strings.ToLower(m.ID), want) && !isDated(m.ID) },
		func(m Model) bool { return sameModelID(strings.ToLower(m.ID), want) },
		func(m Model) bool {
			return strings.Contains(strings.ToLower(m.ID), want) || strings.Contains(strings.ToLower(m.Name), want)
		},
	}
	for _, match := range steps {
		var hits []Model
		for _, m := range c.rows {
			if match(m) {
				hits = append(hits, m)
			}
		}
		if len(hits) == 0 {
			continue
		}
		if spread := providersOf(hits); len(spread) > 1 {
			return Model{}, false, ambiguousModel(fmt.Sprintf("ambiguous model %q: %s", text, strings.Join(namesOf(hits), ", ")), hits)
		}
		first := hits[0]
		if twins := foldTwins(c.rows, first); len(twins) > 1 {
			return Model{}, false, ambiguousModel(fmt.Sprintf("ambiguous model %q: %s", text, strings.Join(namesOf(twins), ", ")), twins)
		}
		exact := candidate{rows: c.rows, id: first.ID, api: c.api}
		return pickProvider(rows, text, exact, []string{first.Provider})
	}
	return Model{}, false, nil
}

func isDated(id string) bool {
	_, date := splitDate(id)
	return date != ""
}

func providersOf(rows []Model) []string {
	var out []string
	for _, m := range rows {
		if !slices.Contains(out, m.Provider) {
			out = append(out, m.Provider)
		}
	}
	return out
}

// foldTwins returns the rows whose provider and id equal those of m after
// case folding but differ in case. It returns nil when m has no such twin.
func foldTwins(rows []Model, m Model) []Model {
	var twins []Model
	for _, r := range rows {
		if strings.EqualFold(r.Provider, m.Provider) && strings.EqualFold(r.ID, m.ID) &&
			!slices.ContainsFunc(twins, func(t Model) bool { return t.Provider == r.Provider && t.ID == r.ID }) {
			twins = append(twins, r)
		}
	}
	if len(twins) < 2 {
		return nil
	}
	return twins
}

// ScopedModel is one row of a scope list with the level of its entry.
type ScopedModel struct {
	Model Model
	// Level is the thinking level of the entry, or empty.
	Level protocol.ThinkingLevel
}

// ScopeProblem tells why one scope entry added no row, or added a row with a
// warning.
type ScopeProblem struct {
	Entry string
	Text  string
}

// ExpandScope turns a scope list (a list of patterns and globs) into rows.
// An entry that has "*", "?" or "[" is a glob: it matches the lower-case
// "provider/id" or "id" of a row with path.Match, and it may end in
// ":<level>". Another entry is a pattern that ResolvePattern reads with
// AllowInvalidLevel true. The result keeps the entry order, then the display
// order of rows inside one entry, and has each row once: the first entry that
// adds a row gives its level. An entry that matches nothing, a bad glob and a
// pattern warning are problems, not errors.
func ExpandScope(rows []Model, entries []string) ([]ScopedModel, []ScopeProblem) {
	var out []ScopedModel
	var problems []ScopeProblem
	add := func(m Model, level protocol.ThinkingLevel) {
		if !slices.ContainsFunc(out, func(s ScopedModel) bool { return refOf(s.Model) == refOf(m) }) {
			out = append(out, ScopedModel{Model: cloneModel(m), Level: level})
		}
	}
	for _, entry := range entries {
		if !strings.ContainsAny(entry, "*?[") {
			r, err := ResolvePattern(rows, entry, PatternOptions{AllowInvalidLevel: true})
			if err != nil {
				problems = append(problems, ScopeProblem{Entry: entry, Text: err.Error()})
				continue
			}
			if r.Warning != "" {
				problems = append(problems, ScopeProblem{Entry: entry, Text: r.Warning})
			}
			add(r.Model, r.Level)
			continue
		}
		glob, level := entry, protocol.ThinkingLevel("")
		if base, l, ok := splitLevel(entry); ok {
			glob, level = base, l
		}
		glob = strings.ToLower(glob)
		if _, err := path.Match(glob, ""); err != nil {
			problems = append(problems, ScopeProblem{Entry: entry, Text: "the glob is not valid"})
			continue
		}
		matched := false
		for _, m := range rows {
			id := strings.ToLower(m.ID)
			full, _ := path.Match(glob, strings.ToLower(m.Provider)+"/"+id)
			bare, _ := path.Match(glob, id)
			if full || bare {
				matched = true
				add(m, level)
			}
		}
		if !matched {
			problems = append(problems, ScopeProblem{Entry: entry, Text: "the glob matches no model"})
		}
	}
	return out, problems
}
