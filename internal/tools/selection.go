package tools

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// Selection origins name the source of a selection in errors.
const (
	OriginDefaultTools = "defaultTools"
	OriginToolsFlag    = "--tools"
)

var selectionEntry = regexp.MustCompile(`^[+-]?[a-z][a-z0-9_]*$`)

// ref is a tool name and the origin that asked for it.
type ref struct{ name, origin string }

// Selection picks the built-in tools of a session. The zero value keeps every
// built-in. It never applies to extra (non-builtin) tools: callers pass only
// built-ins to Resolve and register the extras themselves.
type Selection struct {
	// restrict starts from only instead of from every built-in.
	restrict bool
	only     []ref
	add      []ref
	// remove wins over only and add.
	remove []ref
}

// SelectionError is an unknown tool name in a selection. Known lists the
// names that Resolve was given.
type SelectionError struct {
	Origin string
	Name   string
	Known  []string
}

func (e *SelectionError) Error() string {
	return fmt.Sprintf("%s: unknown tool %q; known tools: %s", e.Origin, e.Name, strings.Join(e.Known, ", "))
}

// ParseDefaultTools parses the defaultTools setting. Plain names are the base
// set and +name and -name apply on top of it; with no plain name the base is
// every built-in. Mixing plain names with deltas is an error. An empty
// non-nil slice selects no built-ins, and a nil slice selects all of them.
func ParseDefaultTools(entries []string) (Selection, error) {
	if entries == nil {
		return Selection{}, nil
	}
	s := Selection{restrict: len(entries) == 0}
	var plain, delta bool
	for _, e := range entries {
		if !selectionEntry.MatchString(e) {
			return Selection{}, fmt.Errorf("%s: invalid entry %q; use a tool name, +name or -name", OriginDefaultTools, e)
		}
		r := ref{name: strings.TrimLeft(e, "+-"), origin: OriginDefaultTools}
		switch e[0] {
		case '+':
			delta = true
			s.add = append(s.add, r)
		case '-':
			delta = true
			s.remove = append(s.remove, r)
		default:
			plain = true
			s.only = append(s.only, r)
		}
	}
	if plain && delta {
		return Selection{}, errors.New(OriginDefaultTools + ": plain names cannot be mixed with +name or -name entries")
	}
	s.restrict = s.restrict || plain
	return s, nil
}

// FlagSelection parses the --tools names or --no-tools. The flag takes plain
// names only. No names and no none flag is the zero selection.
func FlagSelection(names []string, none bool) (Selection, error) {
	if none && len(names) > 0 {
		return Selection{}, errors.New(OriginToolsFlag + " and --no-tools cannot be used together")
	}
	if none {
		return Selection{restrict: true}, nil
	}
	if len(names) == 0 {
		return Selection{}, nil
	}
	s := Selection{restrict: true}
	for _, n := range names {
		if !selectionEntry.MatchString(n) || n[0] == '+' || n[0] == '-' {
			return Selection{}, fmt.Errorf("%s: invalid tool name %q", OriginToolsFlag, n)
		}
		s.only = append(s.only, ref{name: n, origin: OriginToolsFlag})
	}
	return s, nil
}

// IsZero reports whether s keeps every built-in.
func (s Selection) IsZero() bool {
	return !s.restrict && len(s.add) == 0 && len(s.remove) == 0
}

// Over returns the selection that applies when s is set on top of base. A
// non-zero s replaces base, but the removals of base still apply.
func (s Selection) Over(base Selection) Selection {
	if s.IsZero() {
		return base
	}
	out := s
	out.remove = append(slices.Clone(s.remove), base.remove...)
	return out
}

// Resolve returns the builtins that s keeps, in the order of builtins. A name
// that is not in builtins gives a *SelectionError. It has no side effects.
func (s Selection) Resolve(builtins []Tool) ([]Tool, error) {
	known := make([]string, len(builtins))
	for i, t := range builtins {
		known[i] = t.Decl().Name
	}
	for _, list := range [][]ref{s.only, s.add, s.remove} {
		for _, r := range list {
			if !slices.Contains(known, r.name) {
				return nil, &SelectionError{Origin: r.origin, Name: r.name, Known: known}
			}
		}
	}
	keep := func(name string) bool {
		if slices.ContainsFunc(s.remove, func(r ref) bool { return r.name == name }) {
			return false
		}
		if !s.restrict {
			return true
		}
		match := func(r ref) bool { return r.name == name }
		return slices.ContainsFunc(s.only, match) || slices.ContainsFunc(s.add, match)
	}
	out := make([]Tool, 0, len(builtins))
	for i, t := range builtins {
		if keep(known[i]) {
			out = append(out, t)
		}
	}
	return out, nil
}
