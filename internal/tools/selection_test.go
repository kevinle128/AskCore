package tools_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/tools"
)

func builtinStubs() []tools.Tool {
	var out []tools.Tool
	for _, n := range []string{"bash", "edit", "read", "write"} {
		out = append(out, stub(n, `{"type":"object"}`))
	}
	return out
}

func toolNames(ts []tools.Tool) []string {
	out := make([]string, 0, len(ts))
	for _, t := range ts {
		out = append(out, t.Decl().Name)
	}
	return out
}

func mustDefault(t *testing.T, entries []string) tools.Selection {
	t.Helper()
	s, err := tools.ParseDefaultTools(entries)
	require.NoError(t, err)
	return s
}

func mustFlag(t *testing.T, list []string, none bool) tools.Selection {
	t.Helper()
	s, err := tools.FlagSelection(list, none)
	require.NoError(t, err)
	return s
}

func TestSelectionResolve(t *testing.T) {
	tests := []struct {
		name string
		sel  tools.Selection
		want []string
	}{
		{"zero keeps all", tools.Selection{}, []string{"bash", "edit", "read", "write"}},
		{"nil defaultTools keeps all", mustDefault(t, nil), []string{"bash", "edit", "read", "write"}},
		{"allowlist keeps builtin order", mustDefault(t, []string{"write", "read"}), []string{"read", "write"}},
		{"minus delta", mustDefault(t, []string{"-bash"}), []string{"edit", "read", "write"}},
		{"plus delta of a known tool keeps all", mustDefault(t, []string{"+read"}), []string{"bash", "edit", "read", "write"}},
		{"plus and minus", mustDefault(t, []string{"+read", "-edit", "-bash"}), []string{"read", "write"}},
		{"empty list is none", mustDefault(t, []string{}), []string{}},
		{"flag allowlist", mustFlag(t, []string{"read", "bash"}, false), []string{"bash", "read"}},
		{"flag none", mustFlag(t, nil, true), []string{}},
		{"flag absent", mustFlag(t, nil, false), []string{"bash", "edit", "read", "write"}},
		{"flag over setting removal wins", mustFlag(t, []string{"read", "bash"}, false).Over(mustDefault(t, []string{"-bash"})), []string{"read"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.sel.Resolve(builtinStubs())
			require.NoError(t, err)
			assert.Equal(t, tt.want, toolNames(got))
		})
	}
}

func TestSelectionResolveDoesNotChangeInput(t *testing.T) {
	in := builtinStubs()
	_, err := mustDefault(t, []string{"read"}).Resolve(in)
	require.NoError(t, err)
	assert.Equal(t, []string{"bash", "edit", "read", "write"}, toolNames(in))
}

func TestSelectionResolveUnknownNameListsKnown(t *testing.T) {
	tests := []struct {
		name   string
		sel    tools.Selection
		origin string
		unk    string
	}{
		{"allowlist", mustDefault(t, []string{"read", "grep"}), "defaultTools", "grep"},
		{"plus", mustDefault(t, []string{"+nope"}), "defaultTools", "nope"},
		{"minus", mustDefault(t, []string{"-nope"}), "defaultTools", "nope"},
		{"flag", mustFlag(t, []string{"nope"}, false), "--tools", "nope"},
		{"setting removal under flag keeps its origin", mustFlag(t, []string{"read"}, false).Over(mustDefault(t, []string{"-nope"})), "defaultTools", "nope"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.sel.Resolve(builtinStubs())
			assert.Nil(t, got)
			var se *tools.SelectionError
			require.ErrorAs(t, err, &se)
			assert.Equal(t, tt.origin, se.Origin)
			assert.Equal(t, tt.unk, se.Name)
			assert.Equal(t, []string{"bash", "edit", "read", "write"}, se.Known)
			assert.Contains(t, err.Error(), "bash, edit, read, write")
			assert.Contains(t, err.Error(), tt.unk)
		})
	}
}

func TestSelectionOverPrecedence(t *testing.T) {
	setting := mustDefault(t, []string{"-bash", "-write"})

	t.Run("zero flag keeps the setting", func(t *testing.T) {
		got, err := tools.Selection{}.Over(setting).Resolve(builtinStubs())
		require.NoError(t, err)
		assert.Equal(t, []string{"edit", "read"}, toolNames(got))
	})
	t.Run("flag replaces an allowlist setting", func(t *testing.T) {
		got, err := mustFlag(t, []string{"write"}, false).Over(mustDefault(t, []string{"read"})).Resolve(builtinStubs())
		require.NoError(t, err)
		assert.Equal(t, []string{"write"}, toolNames(got))
	})
	t.Run("no-tools flag replaces the setting", func(t *testing.T) {
		got, err := mustFlag(t, nil, true).Over(mustDefault(t, []string{"+bash"})).Resolve(builtinStubs())
		require.NoError(t, err)
		assert.Empty(t, got)
	})
	t.Run("flag over unset setting", func(t *testing.T) {
		got, err := mustFlag(t, []string{"edit"}, false).Over(tools.Selection{}).Resolve(builtinStubs())
		require.NoError(t, err)
		assert.Equal(t, []string{"edit"}, toolNames(got))
	})
}

func TestSelectionIsZero(t *testing.T) {
	assert.True(t, tools.Selection{}.IsZero())
	assert.True(t, mustDefault(t, nil).IsZero())
	assert.True(t, mustFlag(t, nil, false).IsZero())
	assert.False(t, mustDefault(t, []string{}).IsZero())
	assert.False(t, mustDefault(t, []string{"-bash"}).IsZero())
	assert.False(t, mustFlag(t, nil, true).IsZero())
}

func TestParseDefaultToolsRejectsBadInput(t *testing.T) {
	for name, entries := range map[string][]string{
		"mixing plain and delta": {"read", "-bash"},
		"mixing delta first":     {"+read", "write"},
		"empty entry":            {""},
		"bare sign":              {"-"},
		"uppercase":              {"Read"},
		"hyphenated":             {"job-list"},
		"double sign":            {"+-read"},
		"leading digit":          {"1read"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := tools.ParseDefaultTools(entries)
			require.Error(t, err)
		})
	}
}

func TestParseDefaultToolsMixingErrorNamesSetting(t *testing.T) {
	_, err := tools.ParseDefaultTools([]string{"read", "-bash"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "defaultTools")
	assert.Contains(t, err.Error(), "mixed")
}

func TestFlagSelectionRejectsBadInput(t *testing.T) {
	_, err := tools.FlagSelection([]string{"read"}, true)
	require.Error(t, err)
	for _, bad := range []string{"", "-bash", "+bash", "Read"} {
		_, err := tools.FlagSelection([]string{bad}, false)
		require.Error(t, err, bad)
	}
}
