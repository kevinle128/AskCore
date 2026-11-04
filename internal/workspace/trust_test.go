package workspace_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"AskCore/internal/workspace"
)

func TestNearestEntryChildBeatsParent(t *testing.T) {
	entries := map[string]bool{"/work": true, "/work/repo": false}

	got := workspace.NearestEntry(entries, "/work/repo/sub")
	assert.Equal(t, workspace.Entry{Found: true, Trusted: false, Path: "/work/repo", Ancestor: true}, got)

	got = workspace.NearestEntry(entries, "/work/repo")
	assert.Equal(t, workspace.Entry{Found: true, Trusted: false, Path: "/work/repo"}, got)

	got = workspace.NearestEntry(map[string]bool{"/work": true}, "/work/repo")
	assert.Equal(t, workspace.Entry{Found: true, Trusted: true, Path: "/work", Ancestor: true}, got)

	got = workspace.NearestEntry(map[string]bool{"/": true}, "/work/repo")
	assert.Equal(t, workspace.Entry{Found: true, Trusted: true, Path: "/", Ancestor: true}, got)

	got = workspace.NearestEntry(map[string]bool{"/work/other": true}, "/work/repo")
	assert.Equal(t, workspace.Entry{}, got)

	// A key is a path, not a prefix: "/work/re" is not an ancestor of "/work/repo".
	got = workspace.NearestEntry(map[string]bool{"/work/re": true}, "/work/repo")
	assert.Equal(t, workspace.Entry{}, got)

	assert.Equal(t, workspace.Entry{}, workspace.NearestEntry(nil, "/work/repo"))
}

func TestDecideOrder(t *testing.T) {
	saved := func(trusted, ancestor bool) workspace.Entry {
		return workspace.Entry{Found: true, Trusted: trusted, Path: "/p", Ancestor: ancestor}
	}
	type want struct {
		trusted bool
		source  workspace.TrustSource
		prompt  bool
	}
	cases := []struct {
		name string
		in   workspace.DecideInput
		want want
	}{
		{"approve beats everything", workspace.DecideInput{Override: workspace.OverrideApprove, HasGatedFiles: true, StoreCorrupt: true, Entry: saved(false, false), Default: workspace.DefaultTrustNever}, want{true, workspace.SourceFlag, false}},
		{"deny beats everything", workspace.DecideInput{Override: workspace.OverrideDeny, Entry: saved(true, false), Default: workspace.DefaultTrustAlways, UIAvailable: true}, want{false, workspace.SourceFlag, false}},
		{"deny with no gated files", workspace.DecideInput{Override: workspace.OverrideDeny}, want{false, workspace.SourceFlag, false}},
		{"no gated files", workspace.DecideInput{StoreCorrupt: true, Entry: saved(false, false), Default: workspace.DefaultTrustNever}, want{true, workspace.SourceNoGatedFiles, false}},
		{"corrupt store beats saved entry", workspace.DecideInput{HasGatedFiles: true, StoreCorrupt: true, Entry: saved(true, false), UIAvailable: true}, want{false, workspace.SourceStoreError, false}},
		{"corrupt store beats default always", workspace.DecideInput{HasGatedFiles: true, StoreCorrupt: true, Default: workspace.DefaultTrustAlways}, want{false, workspace.SourceStoreError, false}},
		{"corrupt store gives no prompt", workspace.DecideInput{HasGatedFiles: true, StoreCorrupt: true, Default: workspace.DefaultTrustAsk, UIAvailable: true}, want{false, workspace.SourceStoreError, false}},
		{"saved true", workspace.DecideInput{HasGatedFiles: true, Entry: saved(true, false), Default: workspace.DefaultTrustNever}, want{true, workspace.SourceSaved, false}},
		{"saved false", workspace.DecideInput{HasGatedFiles: true, Entry: saved(false, false), Default: workspace.DefaultTrustAlways}, want{false, workspace.SourceSaved, false}},
		{"saved on ancestor", workspace.DecideInput{HasGatedFiles: true, Entry: saved(true, true), UIAvailable: true}, want{true, workspace.SourceSavedAncestor, false}},
		{"saved false on ancestor", workspace.DecideInput{HasGatedFiles: true, Entry: saved(false, true), UIAvailable: true}, want{false, workspace.SourceSavedAncestor, false}},
		{"default always", workspace.DecideInput{HasGatedFiles: true, Default: workspace.DefaultTrustAlways}, want{true, workspace.SourceDefaultAlways, false}},
		{"default never", workspace.DecideInput{HasGatedFiles: true, Default: workspace.DefaultTrustNever, UIAvailable: true}, want{false, workspace.SourceDefaultNever, false}},
		{"default ask with UI", workspace.DecideInput{HasGatedFiles: true, Default: workspace.DefaultTrustAsk, UIAvailable: true}, want{false, workspace.SourcePrompt, true}},
		{"default ask without UI", workspace.DecideInput{HasGatedFiles: true, Default: workspace.DefaultTrustAsk}, want{false, workspace.SourceNoUI, false}},
		{"empty default is ask", workspace.DecideInput{HasGatedFiles: true, UIAvailable: true}, want{false, workspace.SourcePrompt, true}},
		{"unknown default is ask", workspace.DecideInput{HasGatedFiles: true, Default: "sometimes"}, want{false, workspace.SourceNoUI, false}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := workspace.Decide(tc.in)
			assert.Equal(t, tc.want.trusted, got.Trusted, "trusted")
			assert.Equal(t, tc.want.source, got.Source, "source")
			assert.Equal(t, tc.want.prompt, got.NeedsPrompt, "needs prompt")
		})
	}
}

// TestDecideNeverTrustsWhileAPromptIsDue checks every input combination: a
// decision that asks for a prompt is untrusted, and a corrupt store with gated
// files and no override is always untrusted.
func TestDecideNeverTrustsWhileAPromptIsDue(t *testing.T) {
	overrides := []workspace.Override{workspace.OverrideNone, workspace.OverrideApprove, workspace.OverrideDeny}
	entries := []workspace.Entry{{}, {Found: true, Trusted: true, Path: "/p"}, {Found: true, Path: "/p"}, {Found: true, Trusted: true, Path: "/", Ancestor: true}, {Found: true, Path: "/", Ancestor: true}}
	defaults := []workspace.DefaultTrust{workspace.DefaultTrustAsk, workspace.DefaultTrustAlways, workspace.DefaultTrustNever}
	for _, o := range overrides {
		for _, gated := range []bool{false, true} {
			for _, corrupt := range []bool{false, true} {
				for _, e := range entries {
					for _, d := range defaults {
						for _, ui := range []bool{false, true} {
							in := workspace.DecideInput{Override: o, HasGatedFiles: gated, StoreCorrupt: corrupt, Entry: e, Default: d, UIAvailable: ui}
							got := workspace.Decide(in)
							name := fmt.Sprintf("%+v", in)
							assert.NotEmpty(t, got.Source, name)
							if got.NeedsPrompt {
								assert.False(t, got.Trusted, name)
								assert.True(t, ui, name)
							}
							if o == workspace.OverrideNone && gated && corrupt {
								assert.Equal(t, workspace.Decision{Source: workspace.SourceStoreError}, got, name)
							}
						}
					}
				}
			}
		}
	}
}

func TestCorruptTrustStoreGivesUntrusted(t *testing.T) {
	got := workspace.Decide(workspace.DecideInput{
		HasGatedFiles: true,
		StoreCorrupt:  true,
		Default:       workspace.DefaultTrustAlways,
		UIAvailable:   true,
	})
	assert.Equal(t, workspace.Decision{Trusted: false, Source: workspace.SourceStoreError}, got)
}

func TestPlainFolderTrustedWithoutDecision(t *testing.T) {
	got := workspace.Decide(workspace.DecideInput{Default: workspace.DefaultTrustNever, UIAvailable: true})
	assert.Equal(t, workspace.Decision{Trusted: true, Source: workspace.SourceNoGatedFiles}, got)
}

func TestTrustSourceWireWords(t *testing.T) {
	words := map[workspace.TrustSource]string{
		workspace.SourceFlag:          "flag",
		workspace.SourceNoGatedFiles:  "no_gated_files",
		workspace.SourceStoreError:    "store_error",
		workspace.SourceSaved:         "saved",
		workspace.SourceSavedAncestor: "saved_ancestor",
		workspace.SourceDefaultAlways: "default_always",
		workspace.SourceDefaultNever:  "default_never",
		workspace.SourcePrompt:        "prompt",
		workspace.SourceNoUI:          "no_ui",
	}
	for source, word := range words {
		assert.Equal(t, word, string(source))
	}
	assert.Equal(t, "ask", string(workspace.DefaultTrustAsk))
	assert.Equal(t, "always", string(workspace.DefaultTrustAlways))
	assert.Equal(t, "never", string(workspace.DefaultTrustNever))
}
