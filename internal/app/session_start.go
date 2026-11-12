package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"AskCore/internal/agent"
	"AskCore/internal/bootstrap"
	"AskCore/internal/providers"
	"AskCore/internal/settings"
	"AskCore/internal/skills"
	"AskCore/internal/tools"
	"AskCore/internal/workspace"
	"AskCore/pkg/protocol"
)

// ErrSessionFlags is wrapped by the error of PrepareSession when the tool
// flags of the session are not valid. The client caused it.
var ErrSessionFlags = errors.New("invalid session flags")

// SessionInput is what the files and flags of one session start depend on.
type SessionInput struct {
	// Home is the Ask home; empty selects ASK_HOME or the default home.
	Home string
	// UserHome is the home folder of the user. Its .agents/skills folder is a
	// user resource and not a project resource.
	UserHome string
	Cwd      string
	Trust    workspace.Override
	// Flags are the flags of this session. The zero value means no flags.
	Flags protocol.ACPSessionOptions
	// Now is the clock of the session date. Nil uses time.Now.
	Now func() time.Time
}

// SessionStart is everything that the files and flags decide for one session.
// It is read-only after PrepareSession returns, so concurrent sessions share
// nothing.
type SessionStart struct {
	Trust    TrustOutcome
	Settings settings.Effective
	// Tools is the flags over the defaultTools setting.
	Tools   tools.Selection
	Prompt  bootstrap.PromptFiles
	Context bootstrap.Context
	Skills  skills.Result
	Cwd     string
	// Date is the local ISO date of the start. A Reset keeps it.
	Date string
	// askHome tells a user context file from a project one in diagnostics.
	askHome string
}

// AgentSpec is what ACPParams.NewAgent gets for one session.
type AgentSpec struct {
	SessionID string
	Cwd       string
	// Start is the model that the session starts on; the zero model means no
	// provider is usable.
	Start   providers.Model
	Session SessionStart
	// Warn receives the warning lines that Render returns, each with the
	// "Warning: " prefix. Nil drops them.
	Warn func(line string)
	// Models is the model view of the process. The request path of the
	// session reads the key and header tables of the user models.json from
	// it. Nil means no user file.
	Models *ModelView
	// Thinking is the start thinking level of the session, or empty to keep
	// the default of the agent constructor.
	Thinking protocol.ThinkingLevel
}

// PrepareSession does all the file reads of a session start. The trust
// decision comes first, so no project file is opened before it. Then it
// selects the tools, reads the prompt files (the project ones only when the
// project is trusted), the context files (for every project) and the skills.
func PrepareSession(ctx context.Context, in SessionInput) (SessionStart, error) {
	trust, eff, err := LoadSessionSettings(ctx, in.Home, in.UserHome, in.Cwd, in.Trust)
	if err != nil {
		return SessionStart{}, err
	}
	askHome, err := settings.ResolveHome(in.Home)
	if err != nil {
		return SessionStart{}, err
	}

	var entries []string
	if eff.DefaultTools != nil {
		entries = *eff.DefaultTools
	}
	base, err := tools.ParseDefaultTools(entries)
	if err != nil {
		return SessionStart{}, err
	}
	flagged, err := tools.FlagSelection(in.Flags.Tools, in.Flags.NoTools)
	if err != nil {
		return SessionStart{}, fmt.Errorf("%w: %w", ErrSessionFlags, err)
	}

	promptIn := bootstrap.PromptInput{UserDir: askHome, Replace: in.Flags.SystemPrompt, Append: in.Flags.AppendSystemPrompt}
	if p := trust.Project(); p.Trusted {
		promptIn.ProjectDir = filepath.Join(p.Root, projectAskDir)
	}
	prompt, err := bootstrap.LoadPromptFiles(promptIn)
	if err != nil {
		return SessionStart{}, err
	}

	contextFiles, err := bootstrap.LoadContext(bootstrap.ContextInput{Cwd: trust.Root, UserDir: askHome, UserHome: in.UserHome})
	if err != nil {
		return SessionStart{}, err
	}

	now := in.Now
	if now == nil {
		now = time.Now
	}
	return SessionStart{
		Trust:    trust,
		Settings: eff,
		Tools:    flagged.Over(base),
		Prompt:   prompt,
		Context:  contextFiles,
		Skills:   skills.Load(skillRoots(trust, askHome, in.UserHome)),
		Cwd:      in.Cwd,
		Date:     now().Format("2006-01-02"),
		askHome:  askHome,
	}, nil
}

// projectAskDir is the folder of a project that holds its prompt files and
// skills. The trust check names the same folder in the workspace package.
const projectAskDir = ".ask"

var (
	askSkillsEntry     = filepath.Join(projectAskDir, "skills")
	projectSkillsEntry = filepath.Join(".agents", "skills")
)

// skillRoots lists the skills folders in precedence order: the user folders,
// then, for a trusted project, the skills entries of the gated list that the
// trust decision used. A project folder loads only when that check saw it.
// The project's own folders come first, then the ancestors nearest first.
func skillRoots(t TrustOutcome, askHome, userHome string) []skills.Root {
	roots := []skills.Root{{Dir: filepath.Join(askHome, "skills"), Scope: skills.ScopeUser}}
	if userHome != "" {
		roots = append(roots, skills.Root{Dir: filepath.Join(userHome, projectSkillsEntry), Scope: skills.ScopeUser})
	}
	if !t.Decision.Trusted {
		return roots
	}
	var ancestors []string
	for _, entry := range t.Gated {
		switch {
		case entry == askSkillsEntry, entry == projectSkillsEntry:
			roots = append(roots, skills.Root{Dir: filepath.Join(t.Root, entry), Scope: skills.ScopeProject})
		case filepath.IsAbs(entry) && strings.HasSuffix(entry, string(filepath.Separator)+projectSkillsEntry):
			ancestors = append(ancestors, entry)
		}
	}
	slices.SortFunc(ancestors, func(a, b string) int { return len(b) - len(a) })
	for _, dir := range ancestors {
		roots = append(roots, skills.Root{Dir: dir, Scope: skills.ScopeProject})
	}
	return roots
}

// ExpandInput is the RewriteInput of the agent of this session: it loads the
// skill that a prompt names with "/skill:<name>". A skill that the model cannot
// invoke loads too. The project skills are in the list only for a trusted
// project.
func (s SessionStart) ExpandInput(text string) (string, bool) {
	return skills.Expand(text, s.Skills)
}

// SkillRows lists the skills of the session for _ask/session/skills, in the
// order of the skill list.
func (s SessionStart) SkillRows() []protocol.ACPSkill {
	rows := make([]protocol.ACPSkill, len(s.Skills.Skills))
	for i, k := range s.Skills.Skills {
		scope := protocol.ACPSkillUser
		if k.Scope == skills.ScopeProject {
			scope = protocol.ACPSkillProject
		}
		rows[i] = protocol.ACPSkill{Name: k.Name, Description: k.Description, Path: k.File, Scope: scope, ModelInvocable: k.ModelInvocable}
	}
	return rows
}

// RenderResult is what Render builds for one session.
type RenderResult struct {
	SystemPrompt string
	Seed         []protocol.CustomMessage
	// Warnings are the lines for the user, each with the "Warning: " prefix.
	// A skill list that left skills out gives one.
	Warnings []string
}

// Render builds the system prompt and the seed items of the session over the
// tools that it registered and the context window of the start model (0 when
// unknown). The skill list gets the budget of the skillsMaxContextTokens
// setting, else 2% of the window. It is pure.
func (s SessionStart) Render(snap *tools.Snapshot, contextWindow int) RenderResult {
	res := RenderResult{SystemPrompt: agent.SystemPrompt(agent.PromptParts{
		Custom:   s.Prompt.Custom,
		Tools:    tools.PromptList(snap),
		Rules:    tools.PromptGuidelines(snap),
		Addendum: s.Prompt.Append,
		Cwd:      s.Cwd,
		Date:     s.Date,
	})}
	if s.Context.Text != "" {
		res.Seed = append(res.Seed, seedItem(protocol.CustomContextFiles, s.Context.Text))
	}
	if has(snap, "read") || has(snap, "bash") {
		listing, report := s.Skills.Listing(skills.BudgetFor(contextWindow, s.Settings.SkillsMaxContextTokens))
		if listing != "" {
			res.Seed = append(res.Seed, seedItem(protocol.CustomSkillList, listing))
		}
		if w := report.Warning(); w != "" {
			res.Warnings = append(res.Warnings, "Warning: "+w)
		}
	}
	return res
}

func has(snap *tools.Snapshot, name string) bool {
	_, _, ok := snap.Lookup(name)
	return ok
}

func seedItem(customType, text string) protocol.CustomMessage {
	return protocol.CustomMessage{CustomType: customType, Content: []protocol.UserBlock{protocol.Text{Text: text}}}
}

// Warnings returns the warning lines of a session start, each with the
// "Warning: " prefix: the trust warning, a line for a gated project that is
// not trusted (unless the trust store caused it, which the trust warning
// names), one line for each settings diagnostic, one summary line for the
// context files that were not delivered whole, and the skill problems: at most
// four lines of at most 256 bytes and then a line that counts the rest. The
// full list is in SkillDiagnostics and `ask settings show`. A line holds paths
// and fixed texts only.
func (s SessionStart) Warnings() []string {
	var lines []string
	if s.Trust.Warning != "" {
		lines = append(lines, "Warning: "+s.Trust.Warning)
	}
	if len(s.Trust.Gated) > 0 && !s.Trust.Decision.Trusted && s.Trust.Decision.Source != workspace.SourceStoreError {
		lines = append(lines, "Warning: project settings ignored: "+s.Trust.Root+" is not trusted. Use --approve, or save a decision in the TUI with /trust.")
	}
	for _, d := range s.Settings.Diagnostics {
		// A problem of a command line flag has no file: the key names the flag.
		if d.Path == "" && d.Key != "" {
			lines = append(lines, "Warning: "+d.Key+": "+d.Text)
			continue
		}
		line := "Warning: settings: " + d.Path
		if d.Key != "" {
			line += ": " + d.Key
		}
		lines = append(lines, line+": "+d.Text)
	}
	if n := len(s.contextDiagnostics()); n > 0 {
		lines = append(lines, fmt.Sprintf("Warning: %s; run ask settings show for details", plural(n, "context file was skipped", "context files were skipped")))
	}
	for _, d := range s.cappedSkillRows(len(skillsWarningPrefix)) {
		line := skillsWarningPrefix + d.Path
		if d.Path != "" {
			line += ": "
		}
		lines = append(lines, line+d.Text)
	}
	return lines
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// contextDiagnostics lists the context files that were not delivered whole.
// A duplicate is no problem and is not listed.
func (s SessionStart) contextDiagnostics() []protocol.ACPDiagnostic {
	var out []protocol.ACPDiagnostic
	for _, k := range s.Context.Skipped {
		text := contextSkipText(k.Reason)
		if text == "" {
			continue
		}
		out = append(out, protocol.ACPDiagnostic{Layer: s.contextLayer(k.Path), Path: k.Path, Key: "context", Text: text})
	}
	return out
}

// SkillDiagnostics lists the skills that did not load, in the form of the
// session diagnostics.
func (s SessionStart) SkillDiagnostics() []protocol.ACPDiagnostic {
	out := make([]protocol.ACPDiagnostic, 0, len(s.Skills.Diagnostics))
	for _, d := range s.Skills.Diagnostics {
		layer := string(settings.LayerUser)
		if d.Scope == skills.ScopeProject {
			layer = string(settings.LayerProject)
		}
		out = append(out, protocol.ACPDiagnostic{Layer: layer, Path: d.Path, Key: "skills", Text: d.Reason})
	}
	return out
}

const (
	maxSkillRows     = 4
	maxSkillRowBytes = 256
	// skillsWarningPrefix starts each skills line of Warnings. Its bytes count
	// against the 256 bytes of the line.
	skillsWarningPrefix = "Warning: skills: "
)

// cappedSkillDiagnostics is SkillDiagnostics cut to four rows for a consumer
// that adds no text in front of a row.
func (s SessionStart) cappedSkillDiagnostics() []protocol.ACPDiagnostic {
	return s.cappedSkillRows(0)
}

// cappedSkillRows is SkillDiagnostics cut to four rows. The path of a row is
// cut so that the row holds at most 256 bytes with reserve bytes in front of
// it. A last row without a path counts the rest.
func (s SessionStart) cappedSkillRows(reserve int) []protocol.ACPDiagnostic {
	all := s.SkillDiagnostics()
	if len(all) <= maxSkillRows {
		return capRows(all, reserve)
	}
	rest := all[maxSkillRows:]
	out := capRows(all[:maxSkillRows], reserve)
	return append(out, protocol.ACPDiagnostic{
		Layer: rest[0].Layer,
		Key:   "skills",
		Text:  plural(len(rest), "more skill problem", "more skill problems") + "; run ask settings show for details",
	})
}

func capRows(rows []protocol.ACPDiagnostic, reserve int) []protocol.ACPDiagnostic {
	out := slices.Clone(rows)
	for i, d := range out {
		room := max(maxSkillRowBytes-reserve-len(d.Text)-len(": "), 0)
		out[i].Path = cutBytes(d.Path, room)
	}
	return out
}

// cutBytes cuts s to at most n bytes on a rune boundary.
func cutBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// contextLayer is "user" for a file that lies directly in the Ask home, the only
// place where user context files are read, and "project" for the others. A
// project folder below the Ask home is still a project.
func (s SessionStart) contextLayer(path string) string {
	if s.askHome != "" && filepath.Dir(path) == s.askHome {
		return string(settings.LayerUser)
	}
	return string(settings.LayerProject)
}

// contextSkipText is the fixed text for a context file that was left out or
// cut, or "" for a skip that needs no report.
func contextSkipText(r bootstrap.SkipReason) string {
	switch r {
	case bootstrap.SkipTooLarge:
		return fmt.Sprintf("left out: the file is over %d MiB", bootstrap.MaxFileBytes>>20)
	case bootstrap.SkipUnreadable:
		return "left out: the file cannot be read"
	case bootstrap.SkipBudget:
		return fmt.Sprintf("left out: the context files are over %d KiB", bootstrap.MaxTotalBytes>>10)
	case bootstrap.SkipTruncated:
		return fmt.Sprintf("cut: the context files are over %d KiB", bootstrap.MaxTotalBytes>>10)
	case bootstrap.SkipOutsideProject:
		return "left out: the file links to a path outside the project"
	default:
		return ""
	}
}
