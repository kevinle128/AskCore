package pipeline

import (
	"context"
	"encoding/json"
	"path"
	"strings"
)

// CommandGuard is a BeforeTool handler that blocks a short list of commands
// that destroy a machine: a recursive delete of the root or home directory,
// mkfs, a write to a disk device, a fork bomb and a recursive chmod or chown
// of the root. It reads the command of a bash or monitor call and ignores every
// other tool. It is a safety net against a model mistake, not a security
// boundary: a shell has endless ways to spell a command, and the guard checks
// the plain ones. A block is an error result for the model; the run goes on.
func CommandGuard(ctx context.Context, in ToolCallInfo, next Next[ToolCallInfo, *BeforeToolCallResult]) (*BeforeToolCallResult, error) {
	if in.Call.Name == "bash" || in.Call.Name == "monitor" {
		var args struct {
			Command string `json:"command"`
		}
		// Arguments that do not decode as a command are the tool's to reject.
		if err := json.Unmarshal(in.Args, &args); err == nil {
			if reason := dangerousReason(args.Command, 0); reason != "" {
				return &BeforeToolCallResult{Block: true, Reason: "blocked by command-guard: " + reason}, nil
			}
		}
	}
	return next(ctx, in)
}

var _ BeforeToolHandler = CommandGuard

// maxShellDepth bounds the nesting of `sh -c "..."` strings that the guard
// opens.
const maxShellDepth = 3

const forkBomb = ":(){:|:&};:"

func dangerousReason(command string, depth int) string {
	segments, syntax := splitCommand(command)
	if strings.Contains(strings.Join(strings.Fields(syntax), ""), forkBomb) {
		return "fork bomb"
	}
	for _, seg := range segments {
		if r := segmentReason(seg, depth); r != "" {
			return r
		}
	}
	return ""
}

// segment is one simple command: its words and the targets of its output
// redirections.
type segment struct {
	words     []string
	redirects []string
}

// splitCommand cuts a command line into simple commands at ; & | and newline,
// removes quotes and reads the word after > or >> as a redirection target. It
// is a scanner, not a shell parser.
func splitCommand(s string) ([]segment, string) {
	var (
		out        []segment
		cur        segment
		word       strings.Builder
		syntax     strings.Builder
		quoted     strings.Builder
		inWord     bool
		quote      rune
		afterRedir bool
	)
	flushWord := func() {
		if !inWord {
			return
		}
		w := word.String()
		word.Reset()
		inWord = false
		if afterRedir {
			cur.redirects = append(cur.redirects, w)
			afterRedir = false
			return
		}
		cur.words = append(cur.words, w)
	}
	flushSeg := func() {
		flushWord()
		afterRedir = false
		if len(cur.words) > 0 || len(cur.redirects) > 0 {
			out = append(out, cur)
		}
		cur = segment{}
	}
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		c := runes[i]
		// Quoted text is data. A shell -c argument is checked separately.
		if quote == 0 && c != '\'' && c != '"' {
			syntax.WriteRune(c)
		}
		switch {
		case quote != 0:
			switch {
			case c == quote:
				// Double quotes can contain a command substitution.
				if quote == '"' && (strings.Contains(quoted.String(), "$(") || strings.ContainsRune(quoted.String(), '`')) {
					syntax.WriteString(quoted.String())
				}
				quote = 0
			case c == '\\' && quote == '"' && i+1 < len(runes):
				i++
				word.WriteRune(runes[i])
			default:
				word.WriteRune(c)
				quoted.WriteRune(c)
			}
		case c == '\'' || c == '"':
			quote, inWord = c, true
			quoted.Reset()
		case c == '\\' && i+1 < len(runes):
			i++
			word.WriteRune(runes[i])
			inWord = true
		case c == ';' || c == '&' || c == '|' || c == '\n' || c == '(' || c == ')':
			flushSeg()
		case c == '>':
			flushWord()
			for i+1 < len(runes) && runes[i+1] == '>' {
				i++
			}
			afterRedir = true
		case c == ' ' || c == '\t':
			flushWord()
		default:
			word.WriteRune(c)
			inWord = true
		}
	}
	flushSeg()
	return out, syntax.String()
}

// wrappers run the command that follows them; the guard looks through them.
var wrappers = map[string]bool{
	"sudo": true, "doas": true, "env": true, "command": true, "nice": true,
	"time": true, "exec": true, "nohup": true, "builtin": true,
}

// wrapperOptionsWithValue lists options per wrapper. For example, -n takes
// a value for nice but is a flag for sudo; -p is a flag for command.
var wrapperOptionsWithValue = map[string]string{
	"sudo": " -u -g -h -p -C -D -R -T -U --user --group --host --prompt --close-from --chdir --chroot --command-timeout --other-user ",
	"doas": " -u ",
	"env":  " -u -C -S --unset --chdir --split-string ",
	"nice": " -n --adjustment ",
	"time": " -o -f --output --format ",
	"exec": " -a ",
}

var shells = map[string]bool{"sh": true, "bash": true, "zsh": true, "dash": true, "ksh": true}

func segmentReason(seg segment, depth int) string {
	for _, target := range seg.redirects {
		if isDiskDevice(target) {
			return "write to a disk device"
		}
	}
	words := seg.words
	for len(words) > 0 {
		w := words[0]
		switch {
		case isAssignment(w):
			words = words[1:]
		case wrappers[path.Base(w)]:
			valueOptions := wrapperOptionsWithValue[path.Base(w)]
			words = words[1:]
			for len(words) > 0 && strings.HasPrefix(words[0], "-") {
				takesValue := strings.Contains(valueOptions, " "+words[0]+" ")
				words = words[1:]
				if takesValue && len(words) > 0 {
					words = words[1:]
				}
			}
		default:
			return commandReason(path.Base(w), words[1:], depth)
		}
	}
	return ""
}

func commandReason(name string, args []string, depth int) string {
	switch {
	case name == "rm":
		if recursive(args, "rR", "recursive") && anyTarget(args, isRootOrHome) {
			return "recursive delete of the root or home directory"
		}
	case name == "mkfs" || strings.HasPrefix(name, "mkfs."):
		return "filesystem creation"
	case name == "dd":
		for _, a := range args {
			if v, ok := strings.CutPrefix(a, "of="); ok && isDiskDevice(v) {
				return "write to a disk device"
			}
		}
	case name == "chmod" || name == "chown" || name == "chgrp":
		if recursive(args, "R", "recursive") && anyTarget(args, isRoot) {
			return "recursive permission change of the root directory"
		}
	case shells[name] && depth < maxShellDepth:
		for i, a := range args {
			if strings.HasPrefix(a, "-") && strings.Contains(a, "c") && !strings.HasPrefix(a, "--") && i+1 < len(args) {
				return dangerousReason(args[i+1], depth+1)
			}
		}
	}
	return ""
}

// recursive reports whether args hold a short flag with one of the letters in
// shorts, or the long flag long.
func recursive(args []string, shorts, long string) bool {
	for _, a := range args {
		if a == "--" {
			return false
		}
		if a == "--"+long {
			return true
		}
		if strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") && strings.ContainsAny(a[1:], shorts) {
			return true
		}
	}
	return false
}

// anyTarget reports whether a non-option word of args satisfies match. After
// "--" every word is a target.
func anyTarget(args []string, match func(string) bool) bool {
	endOfOptions := false
	for _, a := range args {
		switch {
		case !endOfOptions && a == "--":
			endOfOptions = true
		case !endOfOptions && strings.HasPrefix(a, "-"):
		case match(a):
			return true
		}
	}
	return false
}

func isRoot(p string) bool {
	if p == "/*" {
		return true
	}
	return p != "" && strings.Trim(p, "/") == ""
}

func isRootOrHome(p string) bool {
	if isRoot(p) {
		return true
	}
	for _, h := range []string{"~", "$HOME", "${HOME}"} {
		if p == h || p == h+"/" || p == h+"/*" {
			return true
		}
	}
	return false
}

var diskPrefixes = []string{
	"/dev/sd", "/dev/hd", "/dev/vd", "/dev/xvd", "/dev/nvme", "/dev/disk", "/dev/rdisk", "/dev/mmcblk",
}

func isDiskDevice(p string) bool {
	for _, prefix := range diskPrefixes {
		if strings.HasPrefix(p, prefix) {
			return true
		}
	}
	return false
}

// isAssignment reports whether w is a NAME=value word that precedes a command.
func isAssignment(w string) bool {
	name, _, ok := strings.Cut(w, "=")
	if !ok || name == "" {
		return false
	}
	for i, c := range name {
		letter := c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
		if !letter && (i == 0 || c < '0' || c > '9') {
			return false
		}
	}
	return true
}
