package settings

import (
	"strings"
	"testing"
)

func TestEnabledModelsSetting(t *testing.T) {
	tests := []struct {
		name   string
		user   string
		want   []string
		source Layer
	}{
		{"absent", `{}`, nil, LayerDefault},
		{"null", `{"enabledModels":null}`, nil, LayerDefault},
		{"empty array", `{"enabledModels":[]}`, []string{}, LayerUser},
		{"patterns and globs", `{"enabledModels":["anthropic/*","gpt-5.5:low","local/qwen3-coder:30b"]}`,
			[]string{"anthropic/*", "gpt-5.5:low", "local/qwen3-coder:30b"}, LayerUser},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newLayeredFixture(t)
			f.writeUser(t, tt.user)

			eff := f.load(t, false)

			wantEqual(t, tt.want, eff.EnabledModels, "EnabledModels")
			wantEqual(t, tt.source, eff.Sources[EnabledModelsKey], "source")
			if len(eff.Diagnostics) != 0 {
				t.Errorf("diagnostics: %v", eff.Diagnostics)
			}
		})
	}
}

func TestEnabledModelsBadValueIsDiagnosedAndUnset(t *testing.T) {
	for name, value := range map[string]string{
		"string":      `"anthropic/*"`,
		"object":      `{"a":1}`,
		"non-string":  `["a",1]`,
		"null entry":  `["a",null]`,
		"empty entry": `[""]`,
		"long entry":  `["` + strings.Repeat("x", maxEnabledModelBytes+1) + `"]`,
		"too many":    `[` + strings.TrimSuffix(strings.Repeat(`"m",`, maxEnabledModels+1), ",") + `]`,
	} {
		t.Run(name, func(t *testing.T) {
			f := newLayeredFixture(t)
			f.writeUser(t, `{"enabledModels":`+value+`}`)

			eff := f.load(t, false)

			if eff.EnabledModels != nil {
				t.Errorf("EnabledModels: want nil, got %v", eff.EnabledModels)
			}
			wantEqual(t, LayerDefault, eff.Sources[EnabledModelsKey], "source")
			if len(eff.Diagnostics) != 1 {
				t.Fatalf("diagnostics: want 1, got %v", eff.Diagnostics)
			}
			d := eff.Diagnostics[0]
			wantEqual(t, EnabledModelsKey, d.Key, "key")
			wantEqual(t, LayerUser, d.Layer, "layer")
			wantEqual(t, f.userPath(), d.Path, "path")
			if strings.Contains(d.Text, "xxx") {
				t.Errorf("diagnostic text %q holds file content", d.Text)
			}
		})
	}
}

func TestEnabledModelsProjectLayerAndKeyPath(t *testing.T) {
	f := newLayeredFixture(t)
	f.writeUser(t, `{"enabledModels":["anthropic/*"]}`)
	f.writeProject(t, `{"enabledModels":["openai/gpt-5.5"]}`, 0644)

	trusted := f.load(t, true)
	wantEqual(t, []string{"openai/gpt-5.5"}, trusted.EnabledModels, "trusted project value")
	wantEqual(t, LayerProject, trusted.Sources[EnabledModelsKey], "source")
	wantEqual(t, f.projectPath(), trusted.KeyPath(EnabledModelsKey), "project path")

	untrusted := f.load(t, false)
	wantEqual(t, []string{"anthropic/*"}, untrusted.EnabledModels, "untrusted project is ignored")
	wantEqual(t, f.userPath(), untrusted.KeyPath(EnabledModelsKey), "user path")
	wantEqual(t, "", untrusted.KeyPath(defaultToolsKey), "a default value has no path")
}
