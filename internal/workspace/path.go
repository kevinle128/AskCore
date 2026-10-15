package workspace

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// ResolvePath turns a path argument of a tool into a clean absolute path. It
// strips one leading "@", accepts a file:// URL, expands "~" and "~/" to the
// home directory, and joins a relative path with cwd. It never checks that the
// path exists and it does not limit the path to cwd.
func ResolvePath(cwd, p string) (string, error) {
	p = strings.TrimPrefix(p, "@")
	if p == "" {
		return "", errors.New("workspace: empty path")
	}
	if strings.HasPrefix(p, "file://") {
		u, err := url.Parse(p)
		if err != nil {
			return "", fmt.Errorf("workspace: file URL %q: %w", p, err)
		}
		if u.Host != "" && u.Host != "localhost" {
			return "", fmt.Errorf("workspace: file URL %q names host %q; only local paths are supported", p, u.Host)
		}
		if u.Path == "" {
			return "", fmt.Errorf("workspace: file URL %q has no path", p)
		}
		p = filepath.FromSlash(u.Path)
	}
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("workspace: expand ~: %w", err)
		}
		p = filepath.Join(home, p[1:])
	}
	if !filepath.IsAbs(p) {
		if cwd == "" {
			return "", fmt.Errorf("workspace: relative path %q needs a working directory", p)
		}
		p = filepath.Join(cwd, p)
	}
	return filepath.Clean(p), nil
}

const (
	narrowNoBreakSpace = " "
	curlyApostrophe    = "’"
)

var (
	// meridiemRE finds the " AM." or " PM." of a macOS screenshot name, where
	// newer macOS versions write a narrow no-break space before AM or PM.
	meridiemRE = regexp.MustCompile(` (AM|PM)\.`)
	// shellEscapeRE finds a backslash that escapes a shell-special character.
	shellEscapeRE = regexp.MustCompile("\\\\([ \\t()\\[\\]{}'\"&;!$#*?<>|`\\\\~])")
)

// PathVariants returns other spellings of abs that a file on disk can have:
// the shell-unescaped form, and the forms of macOS screenshot and file names
// (narrow no-break space before AM or PM, decomposed Unicode, curly
// apostrophe), alone and combined. The result has no duplicate and does not
// hold abs. A caller tries the variants only when abs does not exist.
func PathVariants(abs string) []string {
	bases := []string{abs}
	if un := shellEscapeRE.ReplaceAllString(abs, "$1"); un != abs {
		bases = append(bases, un)
	}
	transforms := []func(string) string{
		func(s string) string { return meridiemRE.ReplaceAllString(s, narrowNoBreakSpace+"$1.") },
		func(s string) string { return norm.NFD.String(s) },
		func(s string) string { return strings.ReplaceAll(s, "'", curlyApostrophe) },
	}
	seen := map[string]bool{abs: true}
	var out []string
	for _, base := range bases {
		// Each bit of mask selects one transform; mask 0 keeps the base itself.
		for mask := 0; mask < 1<<len(transforms); mask++ {
			v := base
			for i, tf := range transforms {
				if mask&(1<<i) != 0 {
					v = tf(v)
				}
			}
			if !seen[v] {
				seen[v] = true
				out = append(out, v)
			}
		}
	}
	return out
}
