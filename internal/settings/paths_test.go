package settings

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestEnsurePrivateDir(t *testing.T) {
	root := t.TempDir()

	t.Run("creates a missing directory with mode 0700", func(t *testing.T) {
		dir := filepath.Join(root, "new")
		if err := EnsurePrivateDir(dir); err != nil {
			t.Fatal(err)
		}
		info, err := os.Lstat(dir)
		if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
			t.Fatalf("created %v %v", info, err)
		}
		if err := EnsurePrivateDir(dir); err != nil {
			t.Fatalf("second call: %v", err)
		}
	})

	t.Run("corrects a wrong mode of an owned directory", func(t *testing.T) {
		dir := filepath.Join(root, "open")
		if err := os.Mkdir(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := EnsurePrivateDir(dir); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(dir)
		if err != nil || info.Mode().Perm() != 0700 {
			t.Fatalf("mode not corrected: %v %v", info, err)
		}
	})

	t.Run("refuses a symbolic link", func(t *testing.T) {
		target := filepath.Join(root, "target")
		if err := os.Mkdir(target, 0700); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(root, "link")
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		if err := EnsurePrivateDir(link); !errors.Is(err, ErrUnsafeStore) {
			t.Fatalf("link accepted: %v", err)
		}
	})

	t.Run("refuses a parent that is a symbolic link", func(t *testing.T) {
		target := filepath.Join(root, "real-parent")
		if err := os.Mkdir(target, 0700); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(root, "link-parent")
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		if err := EnsurePrivateDir(filepath.Join(link, "child")); !errors.Is(err, ErrUnsafeStore) {
			t.Fatalf("link parent accepted: %v", err)
		}
	})

	t.Run("refuses a file", func(t *testing.T) {
		file := filepath.Join(root, "file")
		if err := os.WriteFile(file, nil, 0600); err != nil {
			t.Fatal(err)
		}
		if err := EnsurePrivateDir(file); !errors.Is(err, ErrUnsafeStore) {
			t.Fatalf("file accepted: %v", err)
		}
	})

	t.Run("does not create a parent", func(t *testing.T) {
		if err := EnsurePrivateDir(filepath.Join(root, "absent", "child")); err == nil {
			t.Fatal("missing parent accepted")
		}
	})

	t.Run("refuses a relative path", func(t *testing.T) {
		if err := EnsurePrivateDir("relative"); !errors.Is(err, ErrUnsafeStore) {
			t.Fatalf("relative path accepted: %v", err)
		}
	})

	t.Run("refuses a foreign owner", func(t *testing.T) {
		// Only a privileged user can give a directory away, so the test runs as
		// root and gives the directory to another user.
		if os.Geteuid() != 0 {
			t.Skip("needs root to change the owner of a directory")
		}
		dir := filepath.Join(root, "foreign")
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chown(dir, 12345, 12345); err != nil {
			t.Fatal(err)
		}
		if err := EnsurePrivateDir(dir); !errors.Is(err, ErrUnsafeStore) {
			t.Fatalf("foreign owner accepted: %v", err)
		}
	})
}

func TestResolveHome(t *testing.T) {
	if got, err := ResolveHome("/explicit"); err != nil || got != "/explicit" {
		t.Fatalf("explicit: %q %v", got, err)
	}
	t.Setenv("ASK_HOME", "/from-env")
	if got, err := ResolveHome(""); err != nil || got != "/from-env" {
		t.Fatalf("env: %q %v", got, err)
	}
	t.Setenv("ASK_HOME", "")
	t.Setenv("HOME", "/users/test")
	if got, err := ResolveHome(""); err != nil || got != "/users/test/.ask" {
		t.Fatalf("default: %q %v", got, err)
	}
	if _, err := ResolveHome("relative"); !errors.Is(err, ErrUnsafeStore) {
		t.Fatalf("relative accepted: %v", err)
	}
}

func TestCheckPrivateDir(t *testing.T) {
	root := t.TempDir()

	t.Run("reports a missing directory and creates nothing", func(t *testing.T) {
		dir := filepath.Join(root, "missing")
		exists, err := CheckPrivateDir(dir)
		if exists || err != nil {
			t.Fatalf("missing: %v %v", exists, err)
		}
		if _, err := os.Lstat(dir); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("the check made the directory: %v", err)
		}
	})

	t.Run("accepts a private directory", func(t *testing.T) {
		dir := filepath.Join(root, "private")
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(dir, 0700); err != nil {
			t.Fatal(err)
		}
		exists, err := CheckPrivateDir(dir)
		if !exists || err != nil {
			t.Fatalf("private: %v %v", exists, err)
		}
	})

	t.Run("refuses a wrong mode and leaves it alone", func(t *testing.T) {
		dir := filepath.Join(root, "open")
		if err := os.Mkdir(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if _, err := CheckPrivateDir(dir); !errors.Is(err, ErrUnsafeStore) {
			t.Fatalf("open mode accepted: %v", err)
		}
		info, err := os.Lstat(dir)
		if err != nil || info.Mode().Perm() != 0755 {
			t.Fatalf("the check changed the mode: %v %v", info, err)
		}
	})

	t.Run("refuses a symbolic link", func(t *testing.T) {
		target := filepath.Join(root, "link-target")
		if err := os.Mkdir(target, 0700); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(root, "link")
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		if _, err := CheckPrivateDir(link); !errors.Is(err, ErrUnsafeStore) {
			t.Fatalf("link accepted: %v", err)
		}
	})

	t.Run("refuses a file", func(t *testing.T) {
		file := filepath.Join(root, "file")
		if err := os.WriteFile(file, nil, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := CheckPrivateDir(file); !errors.Is(err, ErrUnsafeStore) {
			t.Fatalf("file accepted: %v", err)
		}
	})

	t.Run("refuses a relative path", func(t *testing.T) {
		if _, err := CheckPrivateDir("relative"); !errors.Is(err, ErrUnsafeStore) {
			t.Fatalf("relative path accepted: %v", err)
		}
	})
}
