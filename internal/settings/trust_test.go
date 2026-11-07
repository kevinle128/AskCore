package settings

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func boolPtr(v bool) *bool { return &v }

func newTrustStoreInHome(t *testing.T) (*TrustStore, string) {
	t.Helper()
	home := filepath.Join(t.TempDir(), "home")
	t.Setenv("ASK_HOME", home)
	s, err := NewTrustStore("")
	if err != nil {
		t.Fatal(err)
	}
	return s, home
}

func TestTrustStoreMissingFileIsEmpty(t *testing.T) {
	s, home := newTrustStoreInHome(t)
	ctx := context.Background()

	got, err := s.Read(ctx)
	if err != nil || len(got) != 0 || got == nil {
		t.Fatalf("read without a home: %v %v", got, err)
	}
	if _, err := os.Lstat(home); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read created the home: %v", err)
	}

	if err := os.Mkdir(home, 0700); err != nil {
		t.Fatal(err)
	}
	got, err = s.Read(ctx)
	if err != nil || len(got) != 0 {
		t.Fatalf("read without a file: %v %v", got, err)
	}
	entries, err := os.ReadDir(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("read created %d files in the home", len(entries))
	}
}

func TestTrustStoreReadsSavedValues(t *testing.T) {
	s, home := newTrustStoreInHome(t)
	if err := os.Mkdir(home, 0700); err != nil {
		t.Fatal(err)
	}
	body := `{"/a": true, "/b": false, "/c": null}`
	if err := os.WriteFile(filepath.Join(home, "trust.json"), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := s.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got["/a"] != true || got["/b"] != false {
		t.Fatalf("entries %v", got)
	}
	if _, ok := got["/c"]; ok {
		t.Fatal("a null value must read as no entry")
	}
}

func TestCorruptTrustStoreGivesUntrusted(t *testing.T) {
	if !errors.Is(ErrTrustStoreCorrupt, ErrUnsafeStore) {
		t.Fatal("ErrTrustStoreCorrupt must wrap ErrUnsafeStore")
	}
	cases := []struct {
		name  string
		setup func(t *testing.T, path string)
	}{
		{"invalid JSON", func(t *testing.T, path string) { writeTrustFile(t, path, `{"/a": tru`, 0600) }},
		{"not an object", func(t *testing.T, path string) { writeTrustFile(t, path, `["/a"]`, 0600) }},
		{"null document", func(t *testing.T, path string) { writeTrustFile(t, path, `null`, 0600) }},
		{"value not a bool", func(t *testing.T, path string) { writeTrustFile(t, path, `{"/a": "yes"}`, 0600) }},
		{"mode 0644", func(t *testing.T, path string) { writeTrustFile(t, path, `{"/a": true}`, 0644) }},
		{"symbolic link", func(t *testing.T, path string) {
			target := filepath.Join(t.TempDir(), "target.json")
			writeTrustFile(t, target, `{"/a": true}`, 0600)
			if err := os.Symlink(target, path); err != nil {
				t.Fatal(err)
			}
		}},
		{"directory", func(t *testing.T, path string) {
			if err := os.Mkdir(path, 0700); err != nil {
				t.Fatal(err)
			}
		}},
		{"too large", func(t *testing.T, path string) {
			writeTrustFile(t, path, `{"/a": true, "pad": null`+strings.Repeat(" ", maxSettingsFileLen)+`}`, 0600)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, home := newTrustStoreInHome(t)
			if err := os.Mkdir(home, 0700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(home, "trust.json")
			tc.setup(t, path)
			got, err := s.Read(context.Background())
			if !errors.Is(err, ErrTrustStoreCorrupt) {
				t.Fatalf("read error %v, want ErrTrustStoreCorrupt", err)
			}
			if got != nil {
				t.Fatalf("a corrupt store returned entries %v", got)
			}
			if !strings.Contains(err.Error(), path) {
				t.Fatalf("error %q does not name the path", err)
			}
		})
	}
}

func TestTrustStoreUnsafeHomeIsCorrupt(t *testing.T) {
	s, home := newTrustStoreInHome(t)
	if err := os.Mkdir(home, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(home, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Read(context.Background()); !errors.Is(err, ErrTrustStoreCorrupt) {
		t.Fatalf("read in an open home: %v", err)
	}
}

func TestTrustStoreUpdateRefusesCorruptFile(t *testing.T) {
	for _, mode := range []os.FileMode{0600, 0644} {
		t.Run(fmt.Sprintf("mode %o", mode), func(t *testing.T) {
			s, home := newTrustStoreInHome(t)
			if err := os.Mkdir(home, 0700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(home, "trust.json")
			before := []byte(`{"/a": tru`)
			writeTrustFile(t, path, string(before), mode)
			err := s.Update(context.Background(), map[string]*bool{"/b": boolPtr(true)})
			if !errors.Is(err, ErrTrustStoreCorrupt) {
				t.Fatalf("update error %v, want ErrTrustStoreCorrupt", err)
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Fatalf("a corrupt file changed: %q", after)
			}
			info, err := os.Stat(path)
			if err != nil || info.Mode().Perm() != mode {
				t.Fatalf("mode changed: %v %v", info, err)
			}
		})
	}
}

func TestTrustStoreUpdateKeepsOtherEntries(t *testing.T) {
	_, home := newTrustStoreInHome(t)
	ctx := context.Background()
	first, err := NewTrustStore("")
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewTrustStore(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Update(ctx, map[string]*bool{"/z/repo": boolPtr(true), "/a/old": boolPtr(true)}); err != nil {
		t.Fatal(err)
	}
	if err := second.Update(ctx, map[string]*bool{"/m/other": boolPtr(false), "/a/old": nil}); err != nil {
		t.Fatal(err)
	}
	// A delete of a key that is not there changes nothing and is not an error.
	if err := first.Update(ctx, map[string]*bool{"/not/there": nil}); err != nil {
		t.Fatal(err)
	}
	got, err := first.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got["/z/repo"] != true || got["/m/other"] != false {
		t.Fatalf("entries %v", got)
	}
	path := filepath.Join(home, "trust.json")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"/m/other\": false,\n  \"/z/repo\": true\n}\n"
	if string(b) != want {
		t.Fatalf("file %q, want %q", b, want)
	}
	for _, name := range []string{"trust.json", "trust.json.lock"} {
		info, err := os.Stat(filepath.Join(home, name))
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("%s: %v %v", name, info, err)
		}
	}
	info, err := os.Stat(home)
	if err != nil || info.Mode().Perm() != 0700 {
		t.Fatalf("home: %v %v", info, err)
	}
	entries, err := os.ReadDir(home)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "trust.json" && e.Name() != "trust.json.lock" {
			t.Fatalf("unexpected file %s left in the home", e.Name())
		}
	}
}

func TestTrustStoreUpdateKeepsExternalEdit(t *testing.T) {
	s, home := newTrustStoreInHome(t)
	ctx := context.Background()
	if err := s.Update(ctx, map[string]*bool{"/a": boolPtr(true)}); err != nil {
		t.Fatal(err)
	}
	// Another tool writes the file between two updates of this store.
	writeTrustFile(t, filepath.Join(home, "trust.json"), `{"/a": true, "/ext": false}`, 0600)
	if err := s.Update(ctx, map[string]*bool{"/b": boolPtr(true)}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got["/ext"] != false || got["/a"] != true || got["/b"] != true {
		t.Fatalf("entries %v", got)
	}
}

func TestTrustStoreUpdateRefusesBadKeys(t *testing.T) {
	s, home := newTrustStoreInHome(t)
	for _, key := range []string{"", "relative/path", "/a/../b", "/a/", "/a//b"} {
		err := s.Update(context.Background(), map[string]*bool{key: boolPtr(true)})
		if err == nil {
			t.Fatalf("key %q accepted", key)
		}
	}
	if _, err := os.Lstat(filepath.Join(home, "trust.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a refused update wrote the file: %v", err)
	}
}

func TestTrustStoreConcurrentUpdates(t *testing.T) {
	_, home := newTrustStoreInHome(t)
	ctx := context.Background()
	const writers = 8
	var wg sync.WaitGroup
	errs := make(chan error, writers)
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// One store for each writer: the file lock, not a process mutex, must serialize them.
			s, err := NewTrustStore(home)
			if err != nil {
				errs <- err
				return
			}
			errs <- s.Update(ctx, map[string]*bool{fmt.Sprintf("/project/%d", i): boolPtr(i%2 == 0)})
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent update: %v", err)
		}
	}
	s, err := NewTrustStore(home)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != writers {
		t.Fatalf("got %d entries, want %d: %v", len(got), writers, got)
	}
	for i := 0; i < writers; i++ {
		v, ok := got[fmt.Sprintf("/project/%d", i)]
		if !ok || v != (i%2 == 0) {
			t.Fatalf("entry %d: %v %v", i, v, ok)
		}
	}
}

func TestTrustStoreUpdateKeepsOldBytesWhenCommitFails(t *testing.T) {
	s, home := newTrustStoreInHome(t)
	ctx := context.Background()
	if err := s.Update(ctx, map[string]*bool{"/a": boolPtr(true)}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, "trust.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s.files.ops.rename = func(string, string) error { return errors.New("rename failed") }
	if err := s.Update(ctx, map[string]*bool{"/b": boolPtr(true)}); err == nil {
		t.Fatal("update with a failed rename returned no error")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("file changed after a failed commit: %q", after)
	}
}

func writeTrustFile(t *testing.T, path, body string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}
