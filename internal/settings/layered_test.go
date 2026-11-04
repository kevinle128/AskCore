package settings

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// layeredFixture holds a temporary Ask home and a project root.
type layeredFixture struct {
	home string
	root string
}

func newLayeredFixture(t *testing.T) layeredFixture {
	t.Helper()
	dir := t.TempDir()
	f := layeredFixture{home: filepath.Join(dir, "home"), root: filepath.Join(dir, "project")}
	if err := os.Mkdir(f.home, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(f.root, 0755); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f layeredFixture) userPath() string { return filepath.Join(f.home, settingsFile) }

func (f layeredFixture) projectPath() string {
	return filepath.Join(f.root, projectSettingsDir, settingsFile)
}

func (f layeredFixture) writeUser(t *testing.T, content string) {
	t.Helper()
	if err := os.WriteFile(f.userPath(), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(f.userPath(), 0600); err != nil {
		t.Fatal(err)
	}
}

func (f layeredFixture) writeProject(t *testing.T, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(f.projectPath()), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.projectPath(), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(f.projectPath(), mode); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(f.projectPath(), 0644) })
}

func (f layeredFixture) load(t *testing.T, trusted bool) Effective {
	t.Helper()
	l, err := NewLayered(f.home, Project{Root: f.root, Trusted: trusted})
	if err != nil {
		t.Fatal(err)
	}
	eff, err := l.Load(context.Background())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return eff
}

func skipAsRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root ignores file modes")
	}
}

// diagnosticsFor returns the diagnostics of one layer.
func diagnosticsFor(eff Effective, layer Layer) []Diagnostic {
	var out []Diagnostic
	for _, d := range eff.Diagnostics {
		if d.Layer == layer {
			out = append(out, d)
		}
	}
	return out
}

func TestLayeredProjectOverridesUser(t *testing.T) {
	f := newLayeredFixture(t)
	f.writeUser(t, `{"defaultModel":"openai/a@openai-responses"}`)
	f.writeProject(t, `{"defaultModel":"openai/b@openai-responses"}`, 0644)

	eff := f.load(t, true)
	if eff.DefaultModel != "openai/b@openai-responses" || eff.Sources[defaultModelKey] != LayerProject {
		t.Fatalf("trusted project: %q from %q", eff.DefaultModel, eff.Sources[defaultModelKey])
	}
	if len(eff.Diagnostics) != 0 {
		t.Fatalf("diagnostics: %+v", eff.Diagnostics)
	}

	eff = f.load(t, false)
	if eff.DefaultModel != "openai/a@openai-responses" || eff.Sources[defaultModelKey] != LayerUser {
		t.Fatalf("untrusted project: %q from %q", eff.DefaultModel, eff.Sources[defaultModelKey])
	}
}

func TestUntrustedProjectSettingsNeverOpened(t *testing.T) {
	skipAsRoot(t)
	f := newLayeredFixture(t)
	f.writeProject(t, `{not json`, 0)

	eff := f.load(t, false)
	if len(eff.Diagnostics) != 0 {
		t.Fatalf("untrusted project gave diagnostics: %+v", eff.Diagnostics)
	}

	eff = f.load(t, true)
	got := diagnosticsFor(eff, LayerProject)
	if len(got) != 1 || got[0].Path != f.projectPath() {
		t.Fatalf("trusted project diagnostics: %+v", eff.Diagnostics)
	}

	// The same file with a readable mode is still invalid, and still never
	// read while untrusted.
	if err := os.Chmod(f.projectPath(), 0644); err != nil {
		t.Fatal(err)
	}
	if eff = f.load(t, false); len(eff.Diagnostics) != 0 {
		t.Fatalf("untrusted invalid project gave diagnostics: %+v", eff.Diagnostics)
	}
	got = diagnosticsFor(f.load(t, true), LayerProject)
	if len(got) != 1 || got[0].Path != f.projectPath() {
		t.Fatalf("trusted invalid project diagnostics: %+v", got)
	}
	if strings.Contains(got[0].Text, "not json") || strings.Contains(got[0].Text, "'n'") {
		t.Fatalf("diagnostic shows file content: %q", got[0].Text)
	}
}

func TestLoadNeverCreatesProjectDir(t *testing.T) {
	f := newLayeredFixture(t)
	if err := os.WriteFile(filepath.Join(f.root, "AGENTS.md"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	before := walkTree(t, f.root)
	for _, trusted := range []bool{false, true} {
		eff := f.load(t, trusted)
		if len(eff.Diagnostics) != 0 {
			t.Fatalf("trusted=%v diagnostics: %+v", trusted, eff.Diagnostics)
		}
	}
	after := walkTree(t, f.root)
	if strings.Join(before, "\n") != strings.Join(after, "\n") {
		t.Fatalf("project tree changed:\nbefore %v\nafter  %v", before, after)
	}
	if _, err := os.Lstat(filepath.Join(f.root, projectSettingsDir)); !os.IsNotExist(err) {
		t.Fatalf(".ask exists after load: %v", err)
	}
}

func walkTree(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		out = append(out, path+" "+info.Mode().String())
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestProjectCannotSetUserOnlyKeys(t *testing.T) {
	f := newLayeredFixture(t)
	f.writeUser(t, `{"defaultProjectTrust":"never"}`)
	f.writeProject(t, `{"defaultProjectTrust":"always","defaultModel":"openai/b@openai-responses"}`, 0644)

	eff := f.load(t, true)
	if eff.DefaultProjectTrust != ProjectTrustNever || eff.Sources[defaultProjectTrustKey] != LayerUser {
		t.Fatalf("defaultProjectTrust: %q from %q", eff.DefaultProjectTrust, eff.Sources[defaultProjectTrustKey])
	}
	if eff.DefaultModel != "openai/b@openai-responses" {
		t.Fatalf("other project key lost: %q", eff.DefaultModel)
	}
	got := diagnosticsFor(eff, LayerProject)
	if len(got) != 1 || got[0].Key != defaultProjectTrustKey || got[0].Path != f.projectPath() {
		t.Fatalf("diagnostics: %+v", eff.Diagnostics)
	}
}

func TestTrustedProjectOverridesThinkingFold(t *testing.T) {
	f := newLayeredFixture(t)
	f.writeUser(t, `{"thinkingFold":"truncated"}`)
	f.writeProject(t, `{"thinkingFold":"collapsed"}`, 0644)

	eff := f.load(t, true)
	if eff.ThinkingFold != ThinkingFoldCollapsed || eff.Sources[thinkingFoldKey] != LayerProject {
		t.Fatalf("trusted: %q from %q", eff.ThinkingFold, eff.Sources[thinkingFoldKey])
	}
	if len(eff.Diagnostics) != 0 {
		t.Fatalf("diagnostics: %+v", eff.Diagnostics)
	}
	eff = f.load(t, false)
	if eff.ThinkingFold != ThinkingFoldTruncated || eff.Sources[thinkingFoldKey] != LayerUser {
		t.Fatalf("untrusted: %q from %q", eff.ThinkingFold, eff.Sources[thinkingFoldKey])
	}
}

func TestProjectDeniedKeysDroppedWithDiagnostic(t *testing.T) {
	denied := []string{
		"trust", "httpProxy", "env", "shellPath", "shellCommandPrefix",
		"npmCommand", "providers", "hooks", "cacheWarming", "deviceId",
	}
	for _, key := range denied {
		for _, value := range []string{`"x"`, `{"nested":{"deep":"x"}}`} {
			t.Run(key+"/"+value, func(t *testing.T) {
				f := newLayeredFixture(t)
				f.writeUser(t, `{"`+key+`":{"nested":{"user":"kept"}}}`)
				f.writeProject(t, `{"`+key+`":`+value+`,"thinkingFold":"collapsed"}`, 0644)

				l, err := NewLayered(f.home, Project{Root: f.root, Trusted: true})
				if err != nil {
					t.Fatal(err)
				}
				eff, err := l.Load(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				if eff.ThinkingFold != ThinkingFoldCollapsed {
					t.Fatalf("other project key lost: %q", eff.ThinkingFold)
				}
				got := diagnosticsFor(eff, LayerProject)
				if len(got) != 1 || got[0].Key != key || got[0].Path != f.projectPath() {
					t.Fatalf("diagnostics: %+v", eff.Diagnostics)
				}
				if raw := string(eff.merged[key]); raw != `{"nested":{"user":"kept"}}` {
					t.Fatalf("merged %s = %s, want the user value only", key, raw)
				}
			})
		}
	}
}

func TestDeniedKeyMatchIgnoresCase(t *testing.T) {
	f := newLayeredFixture(t)
	f.writeProject(t, `{"DefaultProjectTrust":"always","HTTPPROXY":"http://evil","defaultModel":"openai/b@openai-responses"}`, 0644)

	eff := f.load(t, true)
	if eff.DefaultProjectTrust != ProjectTrustAsk || eff.Sources[defaultProjectTrustKey] != LayerDefault {
		t.Fatalf("defaultProjectTrust: %q from %q", eff.DefaultProjectTrust, eff.Sources[defaultProjectTrustKey])
	}
	for _, key := range []string{"DefaultProjectTrust", "HTTPPROXY"} {
		if _, ok := eff.merged[key]; ok {
			t.Fatalf("%s reached the merged settings", key)
		}
	}
	got := diagnosticsFor(eff, LayerProject)
	if len(got) != 2 {
		t.Fatalf("want two diagnostics, got %+v", got)
	}
	keys := []string{got[0].Key, got[1].Key}
	if strings.Join(keys, ",") != "DefaultProjectTrust,HTTPPROXY" {
		t.Fatalf("diagnostic keys: %v", keys)
	}
	if eff.DefaultModel != "openai/b@openai-responses" {
		t.Fatalf("other project key lost: %q", eff.DefaultModel)
	}
}

func TestProjectKeyCaseMismatchGivesDidYouMean(t *testing.T) {
	f := newLayeredFixture(t)
	f.writeProject(t, `{"DefaultModel":"openai/b@openai-responses","someNewKey":1}`, 0644)

	eff := f.load(t, true)
	if eff.DefaultModel != "" || eff.Sources[defaultModelKey] != LayerDefault {
		t.Fatalf("case-folded key was read: %q from %q", eff.DefaultModel, eff.Sources[defaultModelKey])
	}
	got := diagnosticsFor(eff, LayerProject)
	if len(got) != 1 || got[0].Key != "DefaultModel" || !strings.Contains(got[0].Text, defaultModelKey) {
		t.Fatalf("diagnostics: %+v", eff.Diagnostics)
	}
}

func TestInvalidKnownValueGivesDefaultAndDiagnostic(t *testing.T) {
	f := newLayeredFixture(t)
	f.writeUser(t, `{"thinkingFold":3,"defaultModel":" x","defaultProjectTrust":"maybe"}`)

	eff := f.load(t, false)
	if eff.ThinkingFold != ThinkingFoldExpanded || eff.DefaultModel != "" || eff.DefaultProjectTrust != ProjectTrustAsk {
		t.Fatalf("values: %+v", eff)
	}
	for _, key := range shippedKeys {
		if eff.Sources[key] != LayerDefault {
			t.Fatalf("source of %s: %q", key, eff.Sources[key])
		}
	}
	got := diagnosticsFor(eff, LayerUser)
	if len(got) != 3 {
		t.Fatalf("want three diagnostics, got %+v", eff.Diagnostics)
	}
	seen := map[string]bool{}
	for _, d := range got {
		if d.Path != f.userPath() {
			t.Fatalf("diagnostic path: %+v", d)
		}
		seen[d.Key] = true
	}
	for _, key := range shippedKeys {
		if !seen[key] {
			t.Fatalf("no diagnostic for %s: %+v", key, got)
		}
	}

	// A bad project value gives the default, not the user value, and names
	// the project file.
	f.writeUser(t, `{"thinkingFold":"truncated"}`)
	f.writeProject(t, `{"thinkingFold":"sideways"}`, 0644)
	eff = f.load(t, true)
	if eff.ThinkingFold != ThinkingFoldExpanded || eff.Sources[thinkingFoldKey] != LayerDefault {
		t.Fatalf("bad project value: %q from %q", eff.ThinkingFold, eff.Sources[thinkingFoldKey])
	}
	if got := diagnosticsFor(eff, LayerProject); len(got) != 1 || got[0].Key != thinkingFoldKey || got[0].Path != f.projectPath() {
		t.Fatalf("project diagnostics: %+v", eff.Diagnostics)
	}
}

func TestCorruptSettingsReported(t *testing.T) {
	f := newLayeredFixture(t)
	f.writeUser(t, `{"defaultModel":`)
	f.writeProject(t, `{"thinkingFold":"collapsed"}`, 0644)

	eff := f.load(t, true)
	got := diagnosticsFor(eff, LayerUser)
	if len(got) != 1 || got[0].Path != f.userPath() {
		t.Fatalf("user diagnostics: %+v", eff.Diagnostics)
	}
	if eff.DefaultModel != "" || eff.Sources[defaultModelKey] != LayerDefault {
		t.Fatalf("corrupt user layer was used: %q", eff.DefaultModel)
	}
	if eff.ThinkingFold != ThinkingFoldCollapsed || eff.Sources[thinkingFoldKey] != LayerProject {
		t.Fatalf("project layer lost: %q", eff.ThinkingFold)
	}
	b, err := os.ReadFile(f.userPath())
	if err != nil || string(b) != `{"defaultModel":` {
		t.Fatalf("user file changed: %q %v", b, err)
	}

	// A user file that is not owner-only is reported, not read.
	f.writeUser(t, `{"defaultModel":"openai/a@openai-responses"}`)
	if err := os.Chmod(f.userPath(), 0644); err != nil {
		t.Fatal(err)
	}
	eff = f.load(t, false)
	if got := diagnosticsFor(eff, LayerUser); len(got) != 1 || got[0].Path != f.userPath() || eff.DefaultModel != "" {
		t.Fatalf("unsafe user file: %q %+v", eff.DefaultModel, eff.Diagnostics)
	}
}

func TestUserLayerReadsUserFileOnly(t *testing.T) {
	skipAsRoot(t)
	f := newLayeredFixture(t)
	f.writeUser(t, `{"defaultProjectTrust":"always","thinkingFold":"truncated"}`)
	f.writeProject(t, `{"thinkingFold":"collapsed"}`, 0)

	l, err := NewLayered(f.home, Project{Root: f.root, Trusted: true})
	if err != nil {
		t.Fatal(err)
	}
	eff, err := l.UserLayer(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(eff.Diagnostics) != 0 {
		t.Fatalf("diagnostics: %+v", eff.Diagnostics)
	}
	if eff.DefaultProjectTrust != ProjectTrustAlways || eff.ThinkingFold != ThinkingFoldTruncated {
		t.Fatalf("user layer: %+v", eff)
	}
	if eff.Sources[thinkingFoldKey] != LayerUser {
		t.Fatalf("source: %q", eff.Sources[thinkingFoldKey])
	}
}

func TestSourcesNameDefaultForMissingKeys(t *testing.T) {
	f := newLayeredFixture(t)
	f.writeUser(t, `{}`)
	f.writeProject(t, `{}`, 0644)

	eff := f.load(t, true)
	for _, key := range shippedKeys {
		if eff.Sources[key] != LayerDefault {
			t.Fatalf("source of %s: %q", key, eff.Sources[key])
		}
	}
	if eff.DefaultModel != "" || eff.ThinkingFold != ThinkingFoldExpanded || eff.DefaultProjectTrust != ProjectTrustAsk {
		t.Fatalf("defaults: %+v", eff)
	}
	if len(eff.Diagnostics) != 0 {
		t.Fatalf("diagnostics: %+v", eff.Diagnostics)
	}
}

func TestLoadWithMissingHomeCreatesNothing(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	l, err := NewLayered(home, Project{})
	if err != nil {
		t.Fatal(err)
	}
	eff, err := l.Load(context.Background())
	if err != nil || len(eff.Diagnostics) != 0 {
		t.Fatalf("load: %+v %v", eff.Diagnostics, err)
	}
	if _, err := os.Lstat(home); !os.IsNotExist(err) {
		t.Fatalf("home exists after load: %v", err)
	}
}

func TestLoadReturnsContextError(t *testing.T) {
	f := newLayeredFixture(t)
	l, err := NewLayered(f.home, Project{Root: f.root, Trusted: true})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := l.Load(ctx); err == nil {
		t.Fatal("Load with a cancelled context gave no error")
	}
	if _, err := l.UserLayer(ctx); err == nil {
		t.Fatal("UserLayer with a cancelled context gave no error")
	}
}

func TestNewLayeredRefusesRelativeRoot(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	for _, root := range []string{"project", "/a/../b", "/a/b/"} {
		if _, err := NewLayered(home, Project{Root: root, Trusted: true}); err == nil {
			t.Fatalf("root %q was accepted", root)
		}
	}
}
