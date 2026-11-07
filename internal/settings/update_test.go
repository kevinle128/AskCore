package settings

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// newLayered returns the settings of one session of the fixture.
func (f layeredFixture) newLayered(t *testing.T, trusted bool) *Layered {
	t.Helper()
	l, err := NewLayered(f.home, Project{Root: f.root, Trusted: trusted})
	if err != nil {
		t.Fatal(err)
	}
	return l
}

// setKey returns an edit that sets key to the JSON value raw.
func setKey(key, raw string) func(map[string]json.RawMessage) error {
	return func(fields map[string]json.RawMessage) error {
		fields[key] = json.RawMessage(raw)
		return nil
	}
}

// readBytes returns the bytes of path, or nil when the file does not exist.
func readBytes(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// readObject decodes the JSON object in path.
func readObject(t *testing.T, path string) map[string]json.RawMessage {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(readBytes(t, path), &fields); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return fields
}

// updateDiagnostic returns the diagnostic of an Update refusal.
func updateDiagnostic(t *testing.T, err error) Diagnostic {
	t.Helper()
	var refusal *UpdateError
	if !errors.As(err, &refusal) {
		t.Fatalf("error %v has no diagnostic", err)
	}
	return refusal.Diagnostic
}

func projectLockPath(home, root string) string {
	sum := sha256.Sum256([]byte(root))
	return filepath.Join(home, "project-settings-"+hex.EncodeToString(sum[:])[:16]+".lock")
}

func TestUpdateRefusesProjectWriteWhenUntrusted(t *testing.T) {
	f := newLayeredFixture(t)
	ctx := context.Background()
	err := f.newLayered(t, false).Update(ctx, ScopeProject, setKey(thinkingFoldKey, `"collapsed"`))
	if !errors.Is(err, ErrUntrusted) {
		t.Fatalf("untrusted project write: %v", err)
	}
	noRoot, err := NewLayered(f.home, Project{Trusted: true})
	if err != nil {
		t.Fatal(err)
	}
	if err = noRoot.Update(ctx, ScopeProject, setKey(thinkingFoldKey, `"collapsed"`)); !errors.Is(err, ErrUntrusted) {
		t.Fatalf("project write with no project root: %v", err)
	}
	if _, err = os.Lstat(filepath.Join(f.root, projectSettingsDir)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("project folder touched: %v", err)
	}
	entries, err := os.ReadDir(f.home)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("home changed: %v", entries)
	}
}

func TestCorruptSettingsNotOverwrittenAndReported(t *testing.T) {
	f := newLayeredFixture(t)
	ctx := context.Background()
	const corrupt = `{"thinkingFold":`
	f.writeUser(t, corrupt)
	f.writeProject(t, corrupt, 0644)
	l := f.newLayered(t, true)
	for _, tc := range []struct {
		scope Scope
		path  string
	}{
		{ScopeUser, f.userPath()},
		{ScopeProject, f.projectPath()},
	} {
		err := l.Update(ctx, tc.scope, setKey(defaultModelKey, `"a/b@c"`))
		if !errors.Is(err, ErrUnsafeStore) {
			t.Fatalf("%s: corrupt file accepted: %v", tc.scope, err)
		}
		if d := updateDiagnostic(t, err); d.Path != tc.path || d.Layer != Layer(tc.scope) {
			t.Fatalf("%s: diagnostic %+v does not name %s", tc.scope, d, tc.path)
		}
		if got := readBytes(t, tc.path); string(got) != corrupt {
			t.Fatalf("%s: corrupt file overwritten: %q", tc.scope, got)
		}
	}
	// A user file with the wrong mode is unsafe and also keeps its bytes.
	f.writeUser(t, `{"theme":"dark"}`)
	if err := os.Chmod(f.userPath(), 0644); err != nil {
		t.Fatal(err)
	}
	err := l.Update(ctx, ScopeUser, setKey(thinkingFoldKey, `"collapsed"`))
	if !errors.Is(err, ErrUnsafeStore) || updateDiagnostic(t, err).Path != f.userPath() {
		t.Fatalf("group-readable user file accepted: %v", err)
	}
	if got := readBytes(t, f.userPath()); string(got) != `{"theme":"dark"}` {
		t.Fatalf("unsafe user file overwritten: %q", got)
	}
}

func TestUpdateRefusesInvalidCandidate(t *testing.T) {
	f := newLayeredFixture(t)
	ctx := context.Background()
	l := f.newLayered(t, true)
	err := l.Update(ctx, ScopeUser, setKey(thinkingFoldKey, `7`))
	if !errors.Is(err, ErrInvalidSetting) || updateDiagnostic(t, err).Key != thinkingFoldKey {
		t.Fatalf("invalid thinkingFold accepted: %v", err)
	}
	if got := readBytes(t, f.userPath()); got != nil {
		t.Fatalf("refused write made a file: %q", got)
	}
	err = l.Update(ctx, ScopeProject, setKey(thinkingFoldKey, `"sideways"`))
	if !errors.Is(err, ErrInvalidSetting) || updateDiagnostic(t, err).Key != thinkingFoldKey {
		t.Fatalf("invalid project thinkingFold accepted: %v", err)
	}
	if _, err = os.Lstat(filepath.Join(f.root, projectSettingsDir)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("refused project write made the folder: %v", err)
	}

	// An existing invalid value blocks an unrelated write and is named.
	const bad = `{"defaultProjectTrust":"maybe","theme":"dark"}`
	f.writeUser(t, bad)
	err = l.Update(ctx, ScopeUser, setKey(thinkingFoldKey, `"collapsed"`))
	if !errors.Is(err, ErrInvalidSetting) {
		t.Fatalf("existing invalid value did not block the write: %v", err)
	}
	if d := updateDiagnostic(t, err); d.Key != defaultProjectTrustKey || d.Path != f.userPath() {
		t.Fatalf("diagnostic %+v does not name the bad key", d)
	}
	if got := readBytes(t, f.userPath()); string(got) != bad {
		t.Fatalf("file changed: %q", got)
	}

	// An edit that replaces the invalid value is valid, and null is the same as no value.
	f.writeUser(t, `{"defaultModel":"not an id","thinkingFold":null}`)
	if err = l.SetDefaultModel(ctx, "a/b@c"); err != nil {
		t.Fatalf("a save over an invalid default model: %v", err)
	}
	if got := string(readObject(t, f.userPath())[defaultModelKey]); got != `"a/b@c"` {
		t.Fatalf("default model %s", got)
	}
}

func TestSaveDefaultRefusedWhenProjectSetsDefaultModel(t *testing.T) {
	f := newLayeredFixture(t)
	ctx := context.Background()
	const user = `{"theme":"dark"}`
	f.writeUser(t, user)
	f.writeProject(t, `{"defaultModel":"p/q@r"}`, 0644)
	err := f.newLayered(t, true).SetDefaultModel(ctx, "a/b@c")
	if !errors.Is(err, ErrShadowed) {
		t.Fatalf("shadowed save accepted: %v", err)
	}
	if d := updateDiagnostic(t, err); d.Key != defaultModelKey || d.Layer != LayerProject || d.Path != f.projectPath() {
		t.Fatalf("diagnostic %+v does not name the project key", d)
	}
	if got := readBytes(t, f.userPath()); string(got) != user {
		t.Fatalf("user file changed: %q", got)
	}
	// A key that the project does not set is not shadowed.
	if err = f.newLayered(t, true).Update(ctx, ScopeUser, setKey(thinkingFoldKey, `"truncated"`)); err != nil {
		t.Fatalf("unshadowed key refused: %v", err)
	}
	// An untrusted project does not apply, so it shadows nothing.
	if err = f.newLayered(t, false).SetDefaultModel(ctx, "a/b@c"); err != nil {
		t.Fatalf("save with an untrusted project: %v", err)
	}
	// A key that a project cannot set does not shadow the user value.
	f.writeProject(t, `{"defaultProjectTrust":"always"}`, 0644)
	if err = f.newLayered(t, true).Update(ctx, ScopeUser, setKey(defaultProjectTrustKey, `"never"`)); err != nil {
		t.Fatalf("barred project key shadowed a user write: %v", err)
	}
}

func TestUpdateKeepsUnknownAndExternalKeys(t *testing.T) {
	f := newLayeredFixture(t)
	ctx := context.Background()
	f.writeUser(t, `{"theme":"dark","nested":{"a":[1,2]}}`)
	f.writeProject(t, `{"projectOnly":{"x":1}}`, 0644)
	l := f.newLayered(t, true)
	for _, tc := range []struct {
		scope Scope
		path  string
		mode  os.FileMode
	}{
		{ScopeUser, f.userPath(), 0600},
		{ScopeProject, f.projectPath(), 0644},
	} {
		if err := l.Update(ctx, tc.scope, setKey(thinkingFoldKey, `"collapsed"`)); err != nil {
			t.Fatalf("%s: first update: %v", tc.scope, err)
		}
		// Another process adds a key between the two updates.
		fields := readObject(t, tc.path)
		fields["external"] = json.RawMessage(`true`)
		b, err := json.Marshal(fields)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(tc.path, b, tc.mode); err != nil {
			t.Fatal(err)
		}
		if err = l.Update(ctx, tc.scope, setKey("other", `"x"`)); err != nil {
			t.Fatalf("%s: second update: %v", tc.scope, err)
		}
	}
	user := readObject(t, f.userPath())
	for key, want := range map[string]string{"theme": `"dark"`, "nested": `{"a":[1,2]}`, thinkingFoldKey: `"collapsed"`, "external": `true`, "other": `"x"`} {
		if got := string(user[key]); got != want {
			t.Fatalf("user key %s: got %s, want %s", key, got, want)
		}
	}
	project := readObject(t, f.projectPath())
	for _, key := range []string{"projectOnly", thinkingFoldKey, "external", "other"} {
		if _, ok := project[key]; !ok {
			t.Fatalf("project key %s lost: %v", key, project)
		}
	}
	var only map[string]int
	if err := json.Unmarshal(project["projectOnly"], &only); err != nil || only["x"] != 1 {
		t.Fatalf("project unknown key changed: %s %v", project["projectOnly"], err)
	}
}

func TestUpdateKeepsOldBytesWhenRenameFails(t *testing.T) {
	f := newLayeredFixture(t)
	ctx := context.Background()
	const user = `{"theme":"dark"}`
	f.writeUser(t, user)
	l := f.newLayered(t, false)
	l.files.ops.rename = func(string, string) error { return errors.New("rename failed") }
	if err := l.Update(ctx, ScopeUser, setKey(thinkingFoldKey, `"collapsed"`)); err == nil {
		t.Fatal("update with a failed rename returned no error")
	}
	if got := readBytes(t, f.userPath()); string(got) != user {
		t.Fatalf("file changed after a failed rename: %q", got)
	}
	if left, _ := filepath.Glob(filepath.Join(f.home, ".auth-*")); len(left) != 0 {
		t.Fatalf("temporary files left: %v", left)
	}
	if eff, err := l.Load(ctx); err != nil || eff.ThinkingFold != ThinkingFoldExpanded {
		t.Fatalf("view changed after a failed write: %q %v", eff.ThinkingFold, err)
	}

	l.files.ops.rename = os.Rename
	l.files.ops.syncDir = func(*os.File) error { return errors.New("directory sync failed") }
	if err := l.Update(ctx, ScopeUser, setKey(thinkingFoldKey, `"collapsed"`)); !errors.Is(err, ErrIndeterminate) {
		t.Fatalf("failed directory sync: %v", err)
	}
}

func TestProjectUpdateWritesTempInProjectDir(t *testing.T) {
	f := newLayeredFixture(t)
	ctx := context.Background()
	l := f.newLayered(t, true)
	var temps []string
	l.files.ops.rename = func(from, to string) error {
		temps = append(temps, from)
		return os.Rename(from, to)
	}
	if err := l.Update(ctx, ScopeProject, setKey(thinkingFoldKey, `"collapsed"`)); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(f.root, projectSettingsDir)
	if len(temps) != 1 || filepath.Dir(temps[0]) != dir {
		t.Fatalf("temporary files %v not in %s", temps, dir)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != settingsFile {
		t.Fatalf("project folder has more than the settings file: %v", entries)
	}
	if _, err = os.Stat(projectLockPath(f.home, f.root)); err != nil {
		t.Fatalf("lock sidecar not in the home: %v", err)
	}
	info, err := os.Stat(f.projectPath())
	if err != nil || info.Mode().Perm() != 0644 {
		t.Fatalf("new project file mode: %v %v", info, err)
	}
	eff, err := l.Load(ctx)
	if err != nil || eff.ThinkingFold != ThinkingFoldCollapsed || eff.Sources[thinkingFoldKey] != LayerProject {
		t.Fatalf("project value not loaded: %+v %v", eff, err)
	}

	if err = os.Chmod(f.projectPath(), 0640); err != nil {
		t.Fatal(err)
	}
	if err = l.Update(ctx, ScopeProject, setKey(defaultModelKey, `"a/b@c"`)); err != nil {
		t.Fatal(err)
	}
	if info, err = os.Stat(f.projectPath()); err != nil || info.Mode().Perm() != 0640 {
		t.Fatalf("existing project file mode not kept: %v %v", info, err)
	}
}

func TestProjectUpdateRefusesUnsafeEntries(t *testing.T) {
	f := newLayeredFixture(t)
	ctx := context.Background()
	l := f.newLayered(t, true)
	other := filepath.Join(t.TempDir(), "other.json")
	if err := os.WriteFile(other, []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(f.root, projectSettingsDir), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, f.projectPath()); err != nil {
		t.Fatal(err)
	}
	err := l.Update(ctx, ScopeProject, setKey(thinkingFoldKey, `"collapsed"`))
	if !errors.Is(err, ErrUnsafeStore) || updateDiagnostic(t, err).Path != f.projectPath() {
		t.Fatalf("write through a symbolic link accepted: %v", err)
	}
	if info, err := os.Lstat(f.projectPath()); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("link replaced: %v %v", info, err)
	}
	if got := readBytes(t, other); string(got) != `{}` {
		t.Fatalf("link target changed: %q", got)
	}
}

func TestProjectUpdateRefusesUserOnlyKey(t *testing.T) {
	f := newLayeredFixture(t)
	ctx := context.Background()
	l := f.newLayered(t, true)
	for _, key := range []string{defaultProjectTrustKey, "DefaultProjectTrust", "env", "Providers"} {
		err := l.Update(ctx, ScopeProject, setKey(key, `"always"`))
		if !errors.Is(err, ErrProjectKeyBarred) || updateDiagnostic(t, err).Key != key {
			t.Fatalf("project write of %s accepted: %v", key, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(f.root, projectSettingsDir)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("refused write made the project folder: %v", err)
	}
	// A barred key that the file already has does not block other writes.
	f.writeProject(t, `{"env":{"A":"1"}}`, 0644)
	if err := l.Update(ctx, ScopeProject, setKey(thinkingFoldKey, `"truncated"`)); err != nil {
		t.Fatalf("write next to an existing barred key: %v", err)
	}
	var env map[string]string
	if err := json.Unmarshal(readObject(t, f.projectPath())["env"], &env); err != nil || len(env) != 1 || env["A"] != "1" {
		t.Fatalf("existing barred key changed: %v %v", env, err)
	}
}

func TestUpdateNoChangeNoWrite(t *testing.T) {
	f := newLayeredFixture(t)
	ctx := context.Background()
	f.writeUser(t, `{"thinkingFold":"collapsed"}`)
	f.writeProject(t, `{"thinkingFold":"truncated"}`, 0644)
	past := time.Now().Add(-time.Hour).Truncate(time.Second)
	for _, path := range []string{f.userPath(), f.projectPath()} {
		if err := os.Chtimes(path, past, past); err != nil {
			t.Fatal(err)
		}
	}
	l := f.newLayered(t, true)
	l.files.ops.rename = func(string, string) error { return errors.New("no write expected") }
	if err := l.Update(ctx, ScopeUser, setKey(thinkingFoldKey, `"collapsed"`)); err != nil {
		t.Fatalf("user: %v", err)
	}
	if err := l.Update(ctx, ScopeProject, setKey(thinkingFoldKey, `"truncated"`)); err != nil {
		t.Fatalf("project: %v", err)
	}
	if err := l.Update(ctx, ScopeUser, func(map[string]json.RawMessage) error { return nil }); err != nil {
		t.Fatalf("empty edit: %v", err)
	}
	for _, path := range []string{f.userPath(), f.projectPath()} {
		info, err := os.Stat(path)
		if err != nil || !info.ModTime().Equal(past) {
			t.Fatalf("%s written: %v %v", path, info.ModTime(), err)
		}
	}
}

func TestUpdateReturnsTheEditError(t *testing.T) {
	f := newLayeredFixture(t)
	want := errors.New("edit failed")
	err := f.newLayered(t, false).Update(context.Background(), ScopeUser, func(fields map[string]json.RawMessage) error {
		fields[thinkingFoldKey] = json.RawMessage(`"collapsed"`)
		return want
	})
	if !errors.Is(err, want) {
		t.Fatalf("got %v", err)
	}
	if got := readBytes(t, f.userPath()); got != nil {
		t.Fatalf("failed edit wrote a file: %q", got)
	}
}

// A save of the id that the trusted project already sets is not shadowed: the
// value applies in this project either way, so the user file takes it.
func TestSaveDefaultEqualToProjectValueIsSaved(t *testing.T) {
	f := newLayeredFixture(t)
	ctx := context.Background()
	f.writeUser(t, `{"theme":"dark"}`)
	f.writeProject(t, `{"defaultModel":"p/q@r"}`, 0644)
	if err := f.newLayered(t, true).SetDefaultModel(ctx, "p/q@r"); err != nil {
		t.Fatalf("save of the project value refused: %v", err)
	}
	if got := string(readObject(t, f.userPath())[defaultModelKey]); got != `"p/q@r"` {
		t.Fatalf("default model %s", got)
	}
	// Another id is still shadowed.
	if err := f.newLayered(t, true).SetDefaultModel(ctx, "a/b@c"); !errors.Is(err, ErrShadowed) {
		t.Fatalf("shadowed save accepted: %v", err)
	}
	// A project value that is not valid shadows every id.
	f.writeProject(t, `{"defaultModel":"not an id"}`, 0644)
	if err := f.newLayered(t, true).SetDefaultModel(ctx, "x/y@z"); !errors.Is(err, ErrShadowed) {
		t.Fatalf("save under an invalid project value accepted: %v", err)
	}
}

func TestSaveDefaultSameAsUserFileStillShadowedByProject(t *testing.T) {
	f := newLayeredFixture(t)
	ctx := context.Background()
	f.writeUser(t, `{"defaultModel":"a/b@c"}`)
	f.writeProject(t, `{"defaultModel":"p/q@r"}`, 0644)
	if err := f.newLayered(t, true).SetDefaultModel(ctx, "a/b@c"); !errors.Is(err, ErrShadowed) {
		t.Fatalf("unchanged save under a project value accepted: %v", err)
	}
	// An untrusted project does not shadow anything.
	if err := f.newLayered(t, false).SetDefaultModel(ctx, "a/b@c"); err != nil {
		t.Fatalf("save refused without a trusted project: %v", err)
	}
}
