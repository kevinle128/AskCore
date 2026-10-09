package leader

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolvePaths(t *testing.T) {
	p, err := ResolvePaths("/tmp/ask-home")
	require.NoError(t, err)
	require.Equal(t, Paths{
		Home:   "/tmp/ask-home",
		Socket: "/tmp/ask-home/leader.sock",
		Lock:   "/tmp/ask-home/leader.lock",
		Log:    "/tmp/ask-home/leader.log",
	}, p)

	_, err = ResolvePaths("relative/home")
	require.ErrorIs(t, err, ErrUnsafePath)

	p, err = ResolvePaths("")
	require.NoError(t, err)
	require.True(t, strings.HasSuffix(p.Home, string(filepath.Separator)+".ask"))
}

func TestEnsureHomeModes(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	require.NoError(t, EnsureHome(home))
	info, err := os.Stat(home)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0700), info.Mode().Perm())

	require.NoError(t, os.Chmod(home, 0755))
	require.NoError(t, EnsureHome(home))
	info, _ = os.Stat(home)
	require.Equal(t, os.FileMode(0700), info.Mode().Perm(), "a wide mode is tightened")

	link := filepath.Join(root, "link")
	require.NoError(t, os.Symlink(home, link))
	require.ErrorIs(t, EnsureHome(link), ErrUnsafePath, "a symlink is refused")

	file := filepath.Join(root, "file")
	require.NoError(t, os.WriteFile(file, nil, 0600))
	require.ErrorIs(t, EnsureHome(file), ErrUnsafePath, "a regular file is refused")

	require.Error(t, EnsureHome(filepath.Join(root, "missing", "home")), "the parent must exist")
}

func TestOpenPrivateCreatesOwnerOnlyFile(t *testing.T) {
	old := syscall.Umask(0)
	defer syscall.Umask(old)
	path := filepath.Join(t.TempDir(), "leader.lock")
	f, err := OpenPrivate(path, os.O_RDWR|os.O_CREATE)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0600), info.Mode().Perm())
}

func TestOpenPrivateTightensWideMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "leader.log")
	require.NoError(t, os.WriteFile(path, []byte("x"), 0644))
	require.NoError(t, os.Chmod(path, 0644))
	f, err := OpenPrivate(path, os.O_RDWR)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	info, _ := os.Stat(path)
	require.Equal(t, os.FileMode(0600), info.Mode().Perm())
}

func TestOpenPrivateRefusesSymlinkWithoutTouchingTarget(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	require.NoError(t, os.WriteFile(target, []byte("keep"), 0644))
	link := filepath.Join(dir, "leader.log")
	require.NoError(t, os.Symlink(target, link))

	_, err := OpenPrivate(link, os.O_RDWR|os.O_CREATE|os.O_TRUNC)
	require.ErrorIs(t, err, ErrUnsafePath)
	data, _ := os.ReadFile(target)
	require.Equal(t, "keep", string(data), "the target must stay unchanged")
	info, _ := os.Stat(target)
	require.Equal(t, os.FileMode(0644), info.Mode().Perm(), "the target mode must stay unchanged")
}

func TestOpenPrivateRefusesFIFOAndDirectoryWithoutBlocking(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "leader.lock")
	require.NoError(t, syscall.Mkfifo(fifo, 0600))
	_, err := OpenPrivate(fifo, os.O_RDWR|os.O_CREATE)
	require.ErrorIs(t, err, ErrUnsafePath)

	_, err = OpenPrivate(dir, os.O_RDONLY)
	require.ErrorIs(t, err, ErrUnsafePath)
}

func TestOpenPrivateHonorsTruncateAndAppendAfterChecks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "leader.log")
	require.NoError(t, os.WriteFile(path, []byte("old content"), 0600))

	f, err := OpenPrivate(path, os.O_RDWR|os.O_TRUNC)
	require.NoError(t, err)
	_, err = f.WriteString("new")
	require.NoError(t, err)
	require.NoError(t, f.Close())
	data, _ := os.ReadFile(path)
	require.Equal(t, "new", string(data))

	f, err = OpenPrivate(path, os.O_WRONLY|os.O_APPEND)
	require.NoError(t, err)
	_, err = f.WriteString("+more")
	require.NoError(t, err)
	require.NoError(t, f.Close())
	data, _ = os.ReadFile(path)
	require.Equal(t, "new+more", string(data))
}

func TestSocketPathTooLong(t *testing.T) {
	require.NoError(t, CheckSocketPath("/tmp/askl-1/leader.sock"))
	err := CheckSocketPath("/tmp/" + strings.Repeat("a", 200) + "/leader.sock")
	require.ErrorIs(t, err, ErrSocketPathTooLong)
	require.Contains(t, err.Error(), "ASK_HOME", "the error tells the user what to change")
}

func TestEnsureHomeRefusesSymlinkedParent(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	require.NoError(t, os.Mkdir(real, 0700))
	link := filepath.Join(root, "link")
	require.NoError(t, os.Symlink(real, link))
	// The credential store shares this home and refuses a symlinked parent.
	// The leader must apply the same rule.
	require.ErrorIs(t, EnsureHome(filepath.Join(link, "home")), ErrUnsafePath)
	require.NoError(t, EnsureHome(filepath.Join(real, "home")))
}

func TestConcurrentPrivateLogCreate(t *testing.T) {
	for range 100 {
		p := testPaths(t)
		errs := make(chan error, 4)
		start := make(chan struct{})
		for range 4 {
			go func() {
				<-start
				f, err := OpenLog(p)
				if f != nil {
					_ = f.Close()
				}
				errs <- err
			}()
		}
		close(start)
		for range 4 {
			require.NoError(t, <-errs)
		}
	}
}
