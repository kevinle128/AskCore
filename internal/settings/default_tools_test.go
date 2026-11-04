package settings

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func wantEqual(t *testing.T, want, got any, what string) {
	t.Helper()
	if !reflect.DeepEqual(want, got) {
		t.Errorf("%s: want %#v, got %#v", what, want, got)
	}
}

func TestDefaultToolsUnsetEmptyAndSet(t *testing.T) {
	tests := []struct {
		name   string
		user   string
		want   *[]string
		source Layer
	}{
		{"absent", `{}`, nil, LayerDefault},
		{"null", `{"defaultTools":null}`, nil, LayerDefault},
		{"empty array", `{"defaultTools":[]}`, &[]string{}, LayerUser},
		{"allowlist", `{"defaultTools":["read","grep"]}`, &[]string{"read", "grep"}, LayerUser},
		{"deltas", `{"defaultTools":["-bash","+echo"]}`, &[]string{"-bash", "+echo"}, LayerUser},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newLayeredFixture(t)
			f.writeUser(t, tt.user)

			eff := f.load(t, false)

			wantEqual(t, tt.want, eff.DefaultTools, "DefaultTools")
			wantEqual(t, tt.source, eff.Sources[defaultToolsKey], "source")
			if len(eff.Diagnostics) != 0 {
				t.Errorf("diagnostics: %v", eff.Diagnostics)
			}
		})
	}
}

func TestDefaultToolsBadValueIsDiagnosedAndUnset(t *testing.T) {
	for name, value := range map[string]string{
		"string":        `"read"`,
		"object":        `{"read":true}`,
		"number":        `3`,
		"non-string":    `["read",1]`,
		"null entry":    `["read",null]`,
		"empty entry":   `[""]`,
		"uppercase":     `["Read"]`,
		"hyphenated":    `["job-list"]`,
		"bare sign":     `["-"]`,
		"double sign":   `["+-read"]`,
		"mixed":         `["read","-bash"]`,
		"mixed reverse": `["+read","write"]`,
	} {
		t.Run(name, func(t *testing.T) {
			f := newLayeredFixture(t)
			f.writeUser(t, `{"defaultTools":`+value+`}`)

			eff := f.load(t, false)

			if eff.DefaultTools != nil {
				t.Errorf("DefaultTools: want nil, got %v", *eff.DefaultTools)
			}
			wantEqual(t, LayerDefault, eff.Sources[defaultToolsKey], "source")
			if len(eff.Diagnostics) != 1 {
				t.Fatalf("diagnostics: want 1, got %v", eff.Diagnostics)
			}
			d := eff.Diagnostics[0]
			wantEqual(t, defaultToolsKey, d.Key, "key")
			wantEqual(t, LayerUser, d.Layer, "layer")
			wantEqual(t, f.userPath(), d.Path, "path")
			if d.Text == "" || strings.Contains(d.Text, "Read") {
				t.Errorf("diagnostic text %q must be set and hold no file content", d.Text)
			}
		})
	}
}

func TestDefaultToolsProjectLayerReplacesUserArray(t *testing.T) {
	f := newLayeredFixture(t)
	f.writeUser(t, `{"defaultTools":["-bash","-write"]}`)
	f.writeProject(t, `{"defaultTools":["read","grep"]}`, 0644)

	eff := f.load(t, true)

	wantEqual(t, &[]string{"read", "grep"}, eff.DefaultTools, "DefaultTools")
	wantEqual(t, LayerProject, eff.Sources[defaultToolsKey], "source")
	if len(eff.Diagnostics) != 0 {
		t.Errorf("diagnostics: %v", eff.Diagnostics)
	}
}

func TestDefaultToolsProjectEmptyArrayReplacesUserArray(t *testing.T) {
	f := newLayeredFixture(t)
	f.writeUser(t, `{"defaultTools":["read"]}`)
	f.writeProject(t, `{"defaultTools":[]}`, 0644)

	eff := f.load(t, true)

	wantEqual(t, &[]string{}, eff.DefaultTools, "DefaultTools")
	wantEqual(t, LayerProject, eff.Sources[defaultToolsKey], "source")
}

func TestDefaultToolsProjectNullKeepsUserValue(t *testing.T) {
	f := newLayeredFixture(t)
	f.writeUser(t, `{"defaultTools":["read"]}`)
	f.writeProject(t, `{"defaultTools":null}`, 0644)

	eff := f.load(t, true)

	wantEqual(t, &[]string{"read"}, eff.DefaultTools, "DefaultTools")
	wantEqual(t, LayerUser, eff.Sources[defaultToolsKey], "source")
}

func TestDefaultToolsUntrustedProjectIsIgnored(t *testing.T) {
	f := newLayeredFixture(t)
	f.writeUser(t, `{"defaultTools":["-bash"]}`)
	f.writeProject(t, `{"defaultTools":["read"]}`, 0644)

	eff := f.load(t, false)

	wantEqual(t, &[]string{"-bash"}, eff.DefaultTools, "DefaultTools")
	wantEqual(t, LayerUser, eff.Sources[defaultToolsKey], "source")
}

func TestDefaultToolsBadProjectValueDiagnosesProjectAndLeavesUnset(t *testing.T) {
	f := newLayeredFixture(t)
	f.writeUser(t, `{"defaultTools":["-bash"]}`)
	f.writeProject(t, `{"defaultTools":["read","-bash"]}`, 0644)

	eff := f.load(t, true)

	if eff.DefaultTools != nil {
		t.Errorf("DefaultTools: want nil, got %v", *eff.DefaultTools)
	}
	got := diagnosticsFor(eff, LayerProject)
	if len(got) != 1 {
		t.Fatalf("project diagnostics: want 1, got %v", got)
	}
	wantEqual(t, defaultToolsKey, got[0].Key, "key")
	wantEqual(t, f.projectPath(), got[0].Path, "path")
}

func TestMergeReplacesDefaultToolsArray(t *testing.T) {
	got := mergeFields(
		rawFields(t, `{"defaultTools":["-bash","-write"]}`),
		rawFields(t, `{"defaultTools":["read"]}`),
	)

	var list []string
	if err := json.Unmarshal(got[defaultToolsKey], &list); err != nil {
		t.Fatal(err)
	}
	wantEqual(t, []string{"read"}, list, "merged defaultTools")
}
