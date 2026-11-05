package bootstrap

import (
	"errors"
	"io/fs"
	"path/filepath"
	"strings"
)

// ContextInput names where the context files are.
type ContextInput struct {
	// Cwd is the canonical session folder.
	Cwd string
	// UserDir is the Ask home. Its AGENTS.md and CLAUDE.md are user-global.
	UserDir string
	// UserHome is the user's home folder. A git root equal to it is dropped.
	UserHome string
}

type ContextFile struct {
	Path string
	// Bytes is the size of the source file, not of the delivered text.
	Bytes int
}

type SkipReason uint8

const (
	// SkipTooLarge is a file over MaxFileBytes. It is never read past the cap.
	SkipTooLarge SkipReason = iota + 1
	// SkipUnreadable is an open or read error.
	SkipUnreadable
	// SkipDuplicate is a file whose trimmed content equals an earlier file in
	// the same folder.
	SkipDuplicate
	// SkipBudget is a file left out of the text; the broadest go first.
	SkipBudget
	// SkipTruncated is the most specific file, cut at a UTF-8 boundary.
	SkipTruncated
)

type Skip struct {
	Path   string
	Reason SkipReason
	// Bytes is the size of the source file; 0 when it could not be read.
	Bytes int
}

// Context is the loaded and rendered context of one session.
type Context struct {
	// Files is what Text holds: user-global first, then the git root down to cwd.
	Files []ContextFile
	// Skipped lists the files that were not delivered whole: load skips in walk
	// order, then budget skips.
	Skipped []Skip
	// Text is the framed and escaped reminder. It is "" when no file loaded and
	// is never longer than MaxTotalBytes.
	Text string
}

// LoadContext reads AGENTS.md and CLAUDE.md from UserDir, then from each
// folder between the git root and Cwd. With no git root only Cwd is read. It
// takes no trust input: context files load for every project. The error is a
// failed probe for .git; unreadable files are reported in Context.Skipped.
func LoadContext(in ContextInput) (Context, error) {
	dirs, err := contextDirs(in.Cwd, in.UserHome)
	if err != nil {
		return Context{}, err
	}
	if in.UserDir != "" {
		user := filepath.Clean(in.UserDir)
		dirs = append([]string{user}, withoutDir(dirs, user)...)
	}

	var files []loaded
	var skipped []Skip
	for _, dir := range dirs {
		seen := make(map[string]struct{}, len(contextNames))
		for _, name := range contextNames {
			path := filepath.Join(dir, name)
			data, size, err := readRegular(path)
			switch {
			case errors.Is(err, fs.ErrNotExist), errors.Is(err, errNotRegular):
				continue
			case errors.Is(err, errTooLarge):
				skipped = append(skipped, Skip{Path: path, Reason: SkipTooLarge, Bytes: int(size)})
				continue
			case err != nil:
				skipped = append(skipped, Skip{Path: path, Reason: SkipUnreadable})
				continue
			}
			text := strings.TrimSpace(string(data))
			if text == "" {
				continue
			}
			if _, dup := seen[text]; dup {
				skipped = append(skipped, Skip{Path: path, Reason: SkipDuplicate, Bytes: len(data)})
				continue
			}
			seen[text] = struct{}{}
			files = append(files, loaded{path: path, text: text, bytes: len(data)})
		}
	}

	kept, budget := fit(files, MaxTotalBytes)
	out := Context{Skipped: append(skipped, budget...)}
	for _, f := range kept {
		out.Files = append(out.Files, ContextFile{Path: f.path, Bytes: f.bytes})
	}
	out.Text = render(kept, budget)
	return out, nil
}

func withoutDir(dirs []string, drop string) []string {
	out := make([]string, 0, len(dirs))
	for _, d := range dirs {
		if d != drop {
			out = append(out, d)
		}
	}
	return out
}
