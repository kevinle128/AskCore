package settings

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

// projectSettingsDir is the name of the project settings folder in a project
// root.
const projectSettingsDir = ".ask"

// Project tells a Layered value which project layer to read.
type Project struct {
	// Root is the canonical project root: an absolute, clean path with its
	// symbolic links resolved. An empty Root means that the session has no
	// project layer, for example when <root>/.ask is the Ask home.
	Root string
	// Trusted is the trust decision for Root. The project file is read only
	// when it is true.
	Trusted bool
}

// settingsPath returns the path of the project settings file.
func (p Project) settingsPath() string {
	return filepath.Join(p.Root, projectSettingsDir, settingsFile)
}

// readProjectSettings reads a project settings file. Call it only for a
// trusted project. A missing file gives empty fields and no problem text.
//
// A committed project file usually has mode 0644 and can belong to the user
// that made the checkout, so the user file rules (0600, owner) do not apply.
// Other rules apply: the path, with its symbolic links resolved, must be a
// regular file of at most maxSettingsFileLen bytes before the open. The open
// does not follow a link and does not block on a FIFO. The open file must
// again be a regular file. The content must be one JSON object.
//
// The problem text is fixed and never contains file content. The function
// never creates a file or a folder.
func readProjectSettings(path string) (map[string]json.RawMessage, string) {
	resolved, err := filepath.EvalSymlinks(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]json.RawMessage{}, ""
	}
	if err != nil {
		return nil, problemText("cannot resolve file", err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return nil, problemText("cannot read file", err)
	}
	if !info.Mode().IsRegular() {
		return nil, "not a regular file"
	}
	if info.Size() > maxSettingsFileLen {
		return nil, "file too large"
	}
	f, err := openNoFollow(resolved, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, problemText("cannot open file", err)
	}
	defer func() { _ = f.Close() }()
	info, err = f.Stat()
	if err != nil {
		return nil, problemText("cannot read file", err)
	}
	if !info.Mode().IsRegular() {
		return nil, "not a regular file"
	}
	b, err := io.ReadAll(io.LimitReader(f, maxSettingsFileLen+1))
	if err != nil {
		return nil, problemText("cannot read file", err)
	}
	if len(b) > maxSettingsFileLen {
		return nil, "file too large"
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(b, &fields); err != nil || fields == nil {
		return nil, "not a JSON object"
	}
	return fields, ""
}

// problemText returns prefix and the system error text of err. It drops the
// path of an *os.PathError, because the diagnostic has the path in its own
// field.
func problemText(prefix string, err error) string {
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		err = pathErr.Err
	}
	return prefix + ": " + err.Error()
}
