package jsonlstore

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const (
	logExt  = ".jsonl"
	lockExt = ".lock"
	// maxDirName keeps the folder name far below the 255 byte limit of common
	// file systems.
	maxDirName = 200
	maxIDLen   = 64
)

var cwdReplacer = strings.NewReplacer("/", "-", `\`, "-", ":", "-")

// encodeCwd returns the folder name of a working directory: the path with
// separators and colons turned into dashes, between two pairs of dashes. Two
// directories can share a name; the cwd in the header of each file decides
// which sessions belong to a directory. A name that would be too long is cut
// and gets a hash of the full path, so long paths still tell apart.
func encodeCwd(cwd string) string {
	trimmed := strings.TrimPrefix(strings.TrimPrefix(cwd, "/"), `\`)
	name := cwdReplacer.Replace(trimmed)
	if len(name)+4 <= maxDirName {
		return "--" + name + "--"
	}
	sum := sha256.Sum256([]byte(cwd))
	// The cut backs up to a rune start, so a multi-byte character is never
	// split: a file system that takes only UTF-8 names refuses half of one.
	limit := maxDirName - 4 - 17
	for limit > 0 && !utf8.RuneStart(name[limit]) {
		limit--
	}
	return "--" + name[:limit] + "-" + hex.EncodeToString(sum[:8]) + "--"
}

// validID reports whether id is safe to use as a file name. Ids come from the
// command line and from a protocol, so a path separator or a dot must never
// reach the file system.
func validID(id string) bool {
	if id == "" || len(id) > maxIDLen {
		return false
	}
	for _, r := range id {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

func (s *Store) dir(cwd string) string { return filepath.Join(s.root, encodeCwd(cwd)) }

func (s *Store) logPath(cwd, id string) string { return filepath.Join(s.dir(cwd), id+logExt) }

func (s *Store) lockPath(cwd, id string) string { return filepath.Join(s.dir(cwd), id+lockExt) }
