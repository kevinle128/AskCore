package settings

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"unicode"
)

const (
	settingsFile       = "settings.json"
	defaultModelKey    = "defaultModel"
	thinkingFoldKey    = "thinkingFold"
	maxModelIDLen      = 256
	maxSettingsFileLen = 1 << 20
)

// defaultProjectTrustKey is a user-only key: a project file cannot set it.
const defaultProjectTrustKey = "defaultProjectTrust"

const defaultToolsKey = "defaultTools"

const skillsMaxContextTokensKey = "skillsMaxContextTokens"

// EnabledModelsKey is the key of the model scope: an array of model patterns
// and globs.
const EnabledModelsKey = "enabledModels"

// Bounds of the enabledModels array. They match the bounds of the --models
// session option.
const (
	maxEnabledModels     = 64
	maxEnabledModelBytes = 256
)

var toolEntry = regexp.MustCompile(`^[+-]?[a-z][a-z0-9_]*$`)

// SettingsStore reads and writes the user settings file in one owner-only home.
// It is the settings of a session with no project layer. It follows the same
// file rules as AuthStore: a lock sidecar, a 0600 file in a 0700 home,
// read-merge-write that keeps fields it does not own, and an atomic replace
// with a directory sync. Unlike Layered.Load, its reads also take the lock
// and create the home (or correct its mode), as the writes do.
type SettingsStore struct {
	layered *Layered
}

// NewSettingsStore constructs a store. Empty home uses ASK_HOME or ~/.ask.
func NewSettingsStore(home string) (*SettingsStore, error) {
	layered, err := NewLayered(home, Project{})
	if err != nil {
		return nil, err
	}
	return &SettingsStore{layered: layered}, nil
}

// DefaultModel returns the saved default model id (provider/id@api), or an
// empty string when no default was saved. A saved string that is not a valid
// id is returned as it is, so that the caller can name it in a fallback note.
func (s *SettingsStore) DefaultModel(ctx context.Context) (string, error) {
	fields, err := s.readLocked(ctx)
	if err != nil {
		return "", err
	}
	raw, ok := fields[defaultModelKey]
	if !ok {
		return "", nil
	}
	var id string
	if err := json.Unmarshal(raw, &id); err != nil {
		return "", fmt.Errorf("%w: invalid default model", ErrUnsafeStore)
	}
	return id, nil
}

// SetDefaultModel saves the default model id and keeps every other field.
func (s *SettingsStore) SetDefaultModel(ctx context.Context, modelID string) error {
	return s.layered.SetDefaultModel(ctx, modelID)
}

// readLocked reads the user file under the lock of the user file. It creates
// the home, or corrects its mode, as a write does.
func (s *SettingsStore) readLocked(ctx context.Context) (map[string]json.RawMessage, error) {
	unlock, err := s.layered.lockUser(ctx)
	if err != nil {
		return nil, err
	}
	defer unlock()
	fields, _, err := s.layered.readUserFile()
	return fields, err
}

// ThinkingFold is the fold mode that an ended thinking step takes in the TUI.
// The zero value is not valid; use one of the constants.
type ThinkingFold string

const (
	// ThinkingFoldExpanded shows all lines of an ended thinking step. It is the
	// default.
	ThinkingFoldExpanded ThinkingFold = "expanded"
	// ThinkingFoldTruncated shows the last lines of an ended thinking step.
	ThinkingFoldTruncated ThinkingFold = "truncated"
	// ThinkingFoldCollapsed shows an ended thinking step as one row.
	ThinkingFoldCollapsed ThinkingFold = "collapsed"
)

// thinkingFoldValue returns the fold of a raw thinkingFold value and true. A
// value that is not one of the known words gives ThinkingFoldExpanded and
// false.
func thinkingFoldValue(raw json.RawMessage) (ThinkingFold, bool) {
	var word string
	if json.Unmarshal(raw, &word) != nil {
		return ThinkingFoldExpanded, false
	}
	switch f := ThinkingFold(word); f {
	case ThinkingFoldExpanded, ThinkingFoldTruncated, ThinkingFoldCollapsed:
		return f, true
	}
	return ThinkingFoldExpanded, false
}

// ThinkingFold returns the saved thinkingFold value. A missing file, a missing
// key or an unknown value gives ThinkingFoldExpanded and no error. A file that
// cannot be read gives ThinkingFoldExpanded and the error, so that a caller
// that does not report the error still gets a valid value.
func (s *SettingsStore) ThinkingFold(ctx context.Context) (ThinkingFold, error) {
	fields, err := s.readLocked(ctx)
	if err != nil {
		return ThinkingFoldExpanded, err
	}
	return decodeEffective(fields, nil, "", "").ThinkingFold, nil
}

func validModelID(id string) bool {
	if id == "" || len(id) > maxModelIDLen || strings.TrimSpace(id) != id {
		return false
	}
	for _, r := range id {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return false
		}
	}
	return true
}

// ensureHomeShared creates the home when it is missing. Two processes can both
// see it missing and race to create it; the loser sees "exists" and checks the
// folder again, which now passes or fails on its real owner and mode.
func ensureHomeShared(home string) error {
	err := ensureHome(home)
	if errors.Is(err, os.ErrExist) {
		err = ensureHome(home)
	}
	return err
}

// decodeSettings decodes the bytes of a settings file into its top-level
// fields. Nil data (a missing file) gives empty fields.
func decodeSettings(data []byte) (map[string]json.RawMessage, error) {
	if data == nil {
		return map[string]json.RawMessage{}, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil || fields == nil {
		return nil, fmt.Errorf("%w: invalid JSON", ErrUnsafeStore)
	}
	return fields, nil
}

// readUserFileBytes reads <home>/<name> with the user file rules: an
// owner-only 0700 home, and an owner-only 0600 regular file of at most 1 MiB
// that is not a link. A missing home or file gives nil data, nil info and no
// error. On success, info is the FileInfo of the opened file, so it always
// belongs to data. On an error, problemPath is the path that has the problem:
// the home or the file. It creates nothing.
func readUserFileBytes(home, name string) (data []byte, info os.FileInfo, problemPath string, err error) {
	exists, err := CheckPrivateDir(home)
	if err != nil {
		return nil, nil, home, err
	}
	if !exists {
		return nil, nil, "", nil
	}
	path := filepath.Join(home, name)
	if err = checkEntry(path, 0600); err != nil {
		return nil, nil, path, err
	}
	data, info, err = readPrivateFile(path)
	if err != nil {
		return nil, nil, path, err
	}
	return data, info, "", nil
}

// readPrivateFile opens path without following a link and without blocking on
// a FIFO, then checks the opened file: an owner-only 0600 regular file of at
// most 1 MiB. A missing file gives nil data, nil info and no error.
func readPrivateFile(path string) ([]byte, os.FileInfo, error) {
	f, err := openNoFollow(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return nil, nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || !owner(info) {
		return nil, nil, fmt.Errorf("%w: file mode", ErrUnsafeStore)
	}
	b, err := io.ReadAll(io.LimitReader(f, maxSettingsFileLen+1))
	if err != nil {
		return nil, nil, err
	}
	if len(b) > maxSettingsFileLen {
		return nil, nil, fmt.Errorf("%w: file too large", ErrUnsafeStore)
	}
	if b == nil {
		b = []byte{}
	}
	return b, info, nil
}

// Layer names the source of a settings value.
type Layer string

const (
	// LayerDefault is the built-in default value.
	LayerDefault Layer = "default"
	// LayerUser is <ASK_HOME>/settings.json.
	LayerUser Layer = "user"
	// LayerProject is <project>/.ask/settings.json.
	LayerProject Layer = "project"
)

// ProjectTrustDefault is the defaultProjectTrust value of the user settings:
// the trust of a project that has gated files and no saved decision.
type ProjectTrustDefault string

const (
	// ProjectTrustAsk asks the user when a UI is available. It is the default.
	ProjectTrustAsk ProjectTrustDefault = "ask"
	// ProjectTrustAlways trusts such a project.
	ProjectTrustAlways ProjectTrustDefault = "always"
	// ProjectTrustNever does not trust such a project.
	ProjectTrustNever ProjectTrustDefault = "never"
)

// shippedKeys are the keys that Effective decodes, in a fixed order.
var shippedKeys = []string{defaultModelKey, thinkingFoldKey, defaultProjectTrustKey, defaultToolsKey, skillsMaxContextTokensKey, EnabledModelsKey}

// Diagnostic is one problem that a load found. It never contains file
// content: only the layer, the path, the key and a fixed text.
type Diagnostic struct {
	Layer Layer
	// Path is the file (or the home folder) that has the problem.
	Path string
	// Key is the settings key, or empty for a problem with the whole file.
	Key  string
	Text string
}

// Effective is the settings view of one session: typed values with the
// defaults applied, the source layer of each shipped key, and the problems
// that the load found.
type Effective struct {
	// DefaultModel is the provider/id@api id of the start model, or empty.
	DefaultModel string
	ThinkingFold ThinkingFold
	// DefaultProjectTrust always comes from the user layer or the default.
	DefaultProjectTrust ProjectTrustDefault
	// DefaultTools is the defaultTools array: nil when unset, and a non-nil
	// empty slice when the file sets an empty array (no built-in tools).
	DefaultTools *[]string
	// SkillsMaxContextTokens is the skillsMaxContextTokens value: 0 when
	// unset, else a positive count of tokens for the skill list.
	SkillsMaxContextTokens int
	// EnabledModels is the enabledModels array: the model patterns and globs
	// of the scope, or nil when unset. The settings package does not resolve
	// them; it checks only that each entry is a short non-empty string.
	EnabledModels []string
	// Sources maps each shipped key to the layer of its value.
	Sources     map[string]Layer
	Diagnostics []Diagnostic
	// merged holds the merged fields with the barred project keys removed.
	merged map[string]json.RawMessage
	// userPath and projectPath are the files of the two layers.
	userPath, projectPath string
}

// KeyPath returns the file whose layer gives the value of key, or "" when the
// key has its default value.
func (eff Effective) KeyPath(key string) string {
	switch eff.Sources[key] {
	case LayerUser:
		return eff.userPath
	case LayerProject:
		return eff.projectPath
	}
	return ""
}

// Layered reads the user settings file and, for a trusted project, the
// project settings file of one session. The trust decision arrives through
// the constructor. A read takes no lock, because each write replaces the file
// atomically, and it creates no file or folder. Update is the only writer: it
// creates the home, the lock sidecars in the home and, for a project write,
// the folder <project>/.ask.
type Layered struct {
	home    string
	project Project
	files   *AuthStore
}

// NewLayered constructs the settings of one session. Empty home uses ASK_HOME
// or ~/.ask. A non-empty project root must be an absolute, clean path.
func NewLayered(home string, project Project) (*Layered, error) {
	home, err := ResolveHome(home)
	if err != nil {
		return nil, err
	}
	if project.Root != "" && (!filepath.IsAbs(project.Root) || filepath.Clean(project.Root) != project.Root) {
		return nil, fmt.Errorf("settings: project root %q is not an absolute clean path", project.Root)
	}
	files, err := NewAuthStore(home)
	if err != nil {
		return nil, err
	}
	return &Layered{home: home, project: project, files: files}, nil
}

// Load reads the user file and, when the project is trusted, the project
// file, and returns the merged view. An untrusted project file is never
// opened. A layer that cannot be read counts as empty and gives a diagnostic.
// The project keys that a project cannot set are dropped, each with a
// diagnostic, and the rest of the project file still applies. Load returns an
// error only when ctx is done.
func (l *Layered) Load(ctx context.Context) (Effective, error) {
	if err := ctx.Err(); err != nil {
		return Effective{}, err
	}
	var diags []Diagnostic
	user, diag := l.readUser()
	if diag != nil {
		diags = append(diags, *diag)
	}
	project := map[string]json.RawMessage{}
	projectPath := ""
	if l.project.Trusted && l.project.Root != "" {
		if err := ctx.Err(); err != nil {
			return Effective{}, err
		}
		path := l.project.settingsPath()
		projectPath = path
		fields, problem := readProjectSettings(path)
		if problem != "" {
			diags = append(diags, Diagnostic{Layer: LayerProject, Path: path, Text: problem})
		}
		var dropped []string
		project, dropped = filterProjectKeys(fields)
		for _, key := range dropped {
			diags = append(diags, Diagnostic{
				Layer: LayerProject, Path: path, Key: key,
				Text: "a project file cannot set this key; it is ignored",
			})
		}
		diags = append(diags, caseMismatchDiagnostics(project, path)...)
	}
	eff := decodeEffective(user, project, l.userPath(), projectPath)
	eff.Diagnostics = append(diags, eff.Diagnostics...)
	return eff, nil
}

// UserLayer returns the view of the user file only. It never reads the
// project file. The trust decision uses it before the project trust is
// known. It returns an error only when ctx is done.
func (l *Layered) UserLayer(ctx context.Context) (Effective, error) {
	if err := ctx.Err(); err != nil {
		return Effective{}, err
	}
	user, diag := l.readUser()
	eff := decodeEffective(user, nil, l.userPath(), "")
	if diag != nil {
		eff.Diagnostics = append([]Diagnostic{*diag}, eff.Diagnostics...)
	}
	return eff, nil
}

func (l *Layered) userPath() string { return filepath.Join(l.home, settingsFile) }

// readUser reads the user file with the user file rules (owner-only 0600
// regular file in an owner-only 0700 home). A missing home or file gives
// empty fields. It creates nothing.
func (l *Layered) readUser() (map[string]json.RawMessage, *Diagnostic) {
	fields, path, err := l.readUserFile()
	if err != nil {
		return map[string]json.RawMessage{}, &Diagnostic{Layer: LayerUser, Path: path, Text: userProblemText(err)}
	}
	return fields, nil
}

// readUserFile reads the user file with the rules of readUser. On an error it
// returns the path that has the problem: the home or the file.
func (l *Layered) readUserFile() (map[string]json.RawMessage, string, error) {
	data, _, problemPath, err := readUserFileBytes(l.home, settingsFile)
	if err != nil {
		return nil, problemPath, err
	}
	fields, err := decodeSettings(data)
	if err != nil {
		return nil, l.userPath(), err
	}
	return fields, "", nil
}

// lockUser creates the home, or corrects its mode, and takes the lock of the
// user file.
func (l *Layered) lockUser(ctx context.Context) (func(), error) {
	if err := ensureHomeShared(l.home); err != nil {
		return nil, err
	}
	return lockSidecar(ctx, filepath.Join(l.home, settingsFile+".lock"))
}

// userProblemText returns a fixed text for a user file error.
func userProblemText(err error) string {
	if errors.Is(err, ErrUnsafeStore) {
		return "unsafe or invalid file: " + strings.TrimPrefix(err.Error(), ErrUnsafeStore.Error()+": ")
	}
	return problemText("cannot read file", err)
}

// caseMismatchDiagnostics reports each project key that differs from a
// shipped key only by case. Such a key is not read. Other unknown keys give
// no diagnostic, because a newer build can own them.
func caseMismatchDiagnostics(project map[string]json.RawMessage, path string) []Diagnostic {
	keys := make([]string, 0, len(project))
	for key := range project {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var diags []Diagnostic
	for _, key := range keys {
		for _, shipped := range shippedKeys {
			if key != shipped && strings.EqualFold(key, shipped) {
				diags = append(diags, Diagnostic{
					Layer: LayerProject, Path: path, Key: key,
					Text: "unknown key; did you mean " + shipped + "?",
				})
			}
		}
	}
	return diags
}

// decodeEffective merges the layers and reads each shipped key by its exact
// name from the merged fields. A known key with a bad type or value gives the
// default and a diagnostic for the layer that set it.
func decodeEffective(user, project map[string]json.RawMessage, userPath, projectPath string) Effective {
	merged := mergeFields(user, project)
	eff := Effective{
		ThinkingFold:        ThinkingFoldExpanded,
		DefaultProjectTrust: ProjectTrustAsk,
		Sources:             make(map[string]Layer, len(shippedKeys)),
		merged:              merged,
		userPath:            userPath,
		projectPath:         projectPath,
	}
	for _, key := range shippedKeys {
		eff.Sources[key] = LayerDefault
		raw, ok := merged[key]
		if !ok || isJSONNull(raw) {
			continue
		}
		layer, path := LayerUser, userPath
		if value, ok := project[key]; ok && !isJSONNull(value) {
			layer, path = LayerProject, projectPath
		}
		if problem := eff.set(key, raw); problem != "" {
			eff.Diagnostics = append(eff.Diagnostics, Diagnostic{Layer: layer, Path: path, Key: key, Text: problem})
			continue
		}
		eff.Sources[key] = layer
	}
	return eff
}

// set decodes one shipped key into eff. It returns a fixed problem text and
// leaves the default when the value is not valid.
func (eff *Effective) set(key string, raw json.RawMessage) string {
	switch key {
	case defaultModelKey:
		var id string
		if json.Unmarshal(raw, &id) != nil || !validModelID(id) {
			return "not a valid model id; the default is used"
		}
		eff.DefaultModel = id
	case thinkingFoldKey:
		fold, ok := thinkingFoldValue(raw)
		if !ok {
			return `must be "expanded", "truncated" or "collapsed"; the default is used`
		}
		eff.ThinkingFold = fold
	case defaultProjectTrustKey:
		var word string
		if json.Unmarshal(raw, &word) != nil {
			return `must be "ask", "always" or "never"; the default is used`
		}
		switch value := ProjectTrustDefault(word); value {
		case ProjectTrustAsk, ProjectTrustAlways, ProjectTrustNever:
			eff.DefaultProjectTrust = value
		default:
			return `must be "ask", "always" or "never"; the default is used`
		}
	case defaultToolsKey:
		return eff.setDefaultTools(raw)
	case skillsMaxContextTokensKey:
		var n int
		if json.Unmarshal(raw, &n) != nil || n < 1 {
			return "must be a positive integer; it is ignored"
		}
		eff.SkillsMaxContextTokens = n
	case EnabledModelsKey:
		return eff.setEnabledModels(raw)
	}
	return ""
}

// setEnabledModels reads the enabledModels array. A bad value leaves the key
// unset.
func (eff *Effective) setEnabledModels(raw json.RawMessage) string {
	const problem = "must be an array of at most 64 model patterns of 1 to 256 bytes; it is ignored"
	var entries []string
	if json.Unmarshal(raw, &entries) != nil || len(entries) > maxEnabledModels {
		return problem
	}
	for _, e := range entries {
		if e == "" || len(e) > maxEnabledModelBytes {
			return problem
		}
	}
	if entries == nil {
		entries = []string{}
	}
	eff.EnabledModels = entries
	return ""
}

// setDefaultTools reads the defaultTools array: plain names, or +name and
// -name entries, never both. A bad value leaves the key unset.
func (eff *Effective) setDefaultTools(raw json.RawMessage) string {
	const problem = `must be an array of tool names, or of +name and -name entries but not both; it is ignored`
	var entries []string
	if json.Unmarshal(raw, &entries) != nil {
		return problem
	}
	var plain, delta bool
	for _, e := range entries {
		if !toolEntry.MatchString(e) {
			return problem
		}
		if e[0] == '+' || e[0] == '-' {
			delta = true
		} else {
			plain = true
		}
	}
	if plain && delta {
		return problem
	}
	if entries == nil {
		entries = []string{}
	}
	eff.DefaultTools = &entries
	return ""
}

// Scope names the settings file that Update writes.
type Scope string

const (
	// ScopeUser is <ASK_HOME>/settings.json.
	ScopeUser Scope = "user"
	// ScopeProject is <project>/.ask/settings.json.
	ScopeProject Scope = "project"
)

var (
	// ErrUntrusted refuses a project write when the session has no trusted
	// project.
	ErrUntrusted = errors.New("settings: the project is not trusted")
	// ErrShadowed refuses a user write of a key that the trusted project file
	// sets: the new user value would not apply in this project.
	ErrShadowed = errors.New("settings: the project settings set this key")
	// ErrInvalidSetting refuses a write when a known key of the new file has a
	// value that is not valid.
	ErrInvalidSetting = errors.New("settings: invalid value")
	// ErrProjectKeyBarred refuses a project write of a key that only the user
	// file can set or that is on the deny list.
	ErrProjectKeyBarred = errors.New("settings: a project file cannot set this key")
)

// UpdateError is a refusal of Update. Err is one of ErrUnsafeStore,
// ErrUntrusted, ErrShadowed, ErrInvalidSetting or ErrProjectKeyBarred (with
// errors.Is), and Diagnostic names the layer, the path and the key. It never
// contains file content.
type UpdateError struct {
	Diagnostic Diagnostic
	Err        error
}

func (e *UpdateError) Error() string {
	msg := e.Err.Error()
	if e.Diagnostic.Path != "" {
		msg += ": " + e.Diagnostic.Path
	}
	if e.Diagnostic.Key != "" {
		msg += ": " + e.Diagnostic.Key
	}
	return msg + ": " + e.Diagnostic.Text
}

func (e *UpdateError) Unwrap() error { return e.Err }

// refusal returns an UpdateError.
func refusal(err error, layer Layer, path, key, text string) *UpdateError {
	return &UpdateError{Err: err, Diagnostic: Diagnostic{Layer: layer, Path: path, Key: key, Text: text}}
}

// unsafeRefusal returns the refusal of a file that cannot be read safely.
func unsafeRefusal(cause error, layer Layer, path, text string) *UpdateError {
	if !errors.Is(cause, ErrUnsafeStore) {
		cause = fmt.Errorf("%w: %w", ErrUnsafeStore, cause)
	}
	return refusal(cause, layer, path, "", text)
}

// SetDefaultModel saves the default model id in the user file and keeps every
// other field. When the trusted project file sets defaultModel to another
// value, it returns ErrShadowed and writes nothing. When the project value is
// the same id, the id applies in this project either way, so the save goes on.
func (l *Layered) SetDefaultModel(ctx context.Context, modelID string) error {
	if !validModelID(modelID) {
		return errors.New("invalid default model id")
	}
	value, err := json.Marshal(modelID)
	if err != nil {
		return err
	}
	eff, err := l.Load(ctx)
	if err != nil {
		return err
	}
	allowShadow := eff.Sources[defaultModelKey] == LayerProject && eff.DefaultModel == modelID
	if !allowShadow {
		// A save that changes nothing in the user file must still report a
		// project value that overrides it.
		if err := l.checkShadow([]string{defaultModelKey}); err != nil {
			return err
		}
	}
	return l.updateUser(ctx, func(fields map[string]json.RawMessage) error {
		fields[defaultModelKey] = value
		return nil
	}, allowShadow)
}

// Update is the only writer of the settings files. It reads the file of scope
// fresh under a lock, applies edit to a copy of its fields and writes the
// result atomically. Fields that edit does not change keep their bytes, so
// unknown keys and keys that another process wrote stay.
//
// Update refuses, writes nothing and returns an *UpdateError when:
//   - scope is ScopeProject and the session has no trusted project
//     (ErrUntrusted); then it takes no lock and touches no file;
//   - the file cannot be read safely (ErrUnsafeStore); a corrupt file keeps
//     its bytes;
//   - a project edit changes a key that a project cannot set
//     (ErrProjectKeyBarred);
//   - a known key of the new fields has a value that is not valid
//     (ErrInvalidSetting), also when the edit did not change that key;
//   - a user edit changes a key that the trusted project file sets
//     (ErrShadowed).
//
// An error from edit is returned as it is. When the edit changes nothing,
// nothing is written. A failure before the rename keeps the old file. A
// failure after the rename returns ErrIndeterminate.
//
// The user file is written with mode 0600 in the home. The project file keeps
// its mode, or gets 0644 when it is new. The folder <project>/.ask is created
// with mode 0755 only when a write follows. The lock of a project file is in
// the home, so that no lock file appears in the project.
func (l *Layered) Update(ctx context.Context, scope Scope, edit func(fields map[string]json.RawMessage) error) error {
	if edit == nil {
		return errors.New("settings: update needs an edit")
	}
	switch scope {
	case ScopeUser:
		return l.updateUser(ctx, edit, false)
	case ScopeProject:
		return l.updateProject(ctx, edit)
	}
	return fmt.Errorf("settings: unknown scope %q", scope)
}

// updateUser writes the user file. allowShadow skips the shadow check, for a
// value that the caller found equal to the value of the project file.
func (l *Layered) updateUser(ctx context.Context, edit func(map[string]json.RawMessage) error, allowShadow bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	unlock, err := l.lockUser(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	fields, problemPath, err := l.readUserFile()
	if err != nil {
		return unsafeRefusal(err, LayerUser, problemPath, userProblemText(err))
	}
	path := l.userPath()
	candidate, changed, err := applyEdit(fields, edit)
	if err != nil {
		return err
	}
	if err = validateCandidate(candidate, LayerUser, path); err != nil {
		return err
	}
	if !allowShadow {
		if err = l.checkShadow(changed); err != nil {
			return err
		}
	}
	if len(changed) == 0 {
		return nil
	}
	b, err := json.Marshal(candidate)
	if err != nil {
		return err
	}
	return l.files.commitIn(ctx, l.home, path, b, 0600)
}

func (l *Layered) updateProject(ctx context.Context, edit func(map[string]json.RawMessage) error) error {
	if !l.project.Trusted || l.project.Root == "" {
		path := ""
		if l.project.Root != "" {
			path = l.project.settingsPath()
		}
		return refusal(ErrUntrusted, LayerProject, path, "", "the project is not trusted; nothing is written")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := ensureHomeShared(l.home); err != nil {
		return err
	}
	unlock, err := lockSidecar(ctx, l.projectLockPath())
	if err != nil {
		return err
	}
	defer unlock()
	dir := filepath.Join(l.project.Root, projectSettingsDir)
	path := l.project.settingsPath()
	fields, mode, dirExists, err := readProjectForWrite(dir, path)
	if err != nil {
		return err
	}
	candidate, changed, err := applyEdit(fields, edit)
	if err != nil {
		return err
	}
	for _, key := range changed {
		if projectKeyBarred(key) {
			return refusal(ErrProjectKeyBarred, LayerProject, path, key, "a project file cannot set this key; nothing is written")
		}
	}
	// A barred key that the file already has never applies, so its value is
	// not checked.
	kept, _ := filterProjectKeys(candidate)
	if err = validateCandidate(kept, LayerProject, path); err != nil {
		return err
	}
	if len(changed) == 0 {
		return nil
	}
	if !dirExists {
		if err = makeProjectSettingsDir(dir); err != nil {
			return err
		}
	}
	b, err := json.MarshalIndent(candidate, "", "  ")
	if err != nil {
		return err
	}
	return l.files.commitIn(ctx, dir, path, append(b, '\n'), mode)
}

// projectLockPath returns the lock sidecar of the project file. It is in the
// home and its name has the first 16 hex digits of the SHA-256 of the root.
func (l *Layered) projectLockPath() string {
	sum := sha256.Sum256([]byte(l.project.Root))
	return filepath.Join(l.home, "project-settings-"+hex.EncodeToString(sum[:])[:16]+".lock")
}

// readProjectForWrite reads the project file before a write. The folder must
// be a real folder and the file a regular file, not a symbolic link, because
// the rename replaces the entry itself. It returns the fields, the mode that
// the new file gets, and whether the folder exists.
func readProjectForWrite(dir, path string) (map[string]json.RawMessage, os.FileMode, bool, error) {
	const newMode os.FileMode = 0644
	info, err := os.Lstat(dir)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]json.RawMessage{}, newMode, false, nil
	}
	if err != nil {
		return nil, 0, false, unsafeRefusal(err, LayerProject, dir, problemText("cannot read folder", err))
	}
	if !info.IsDir() {
		return nil, 0, false, unsafeRefusal(errors.New("not a folder"), LayerProject, dir, "not a folder")
	}
	info, err = os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]json.RawMessage{}, newMode, true, nil
	}
	if err != nil {
		return nil, 0, false, unsafeRefusal(err, LayerProject, path, problemText("cannot read file", err))
	}
	if !info.Mode().IsRegular() {
		return nil, 0, false, unsafeRefusal(errors.New("not a regular file"), LayerProject, path, "not a regular file")
	}
	fields, problem := readProjectSettings(path)
	if problem != "" {
		return nil, 0, false, unsafeRefusal(errors.New(problem), LayerProject, path, problem)
	}
	return fields, info.Mode().Perm(), true, nil
}

// makeProjectSettingsDir creates <project>/.ask with mode 0755. When another
// process made it first, the entry must still be a real folder.
func makeProjectSettingsDir(dir string) error {
	err := os.Mkdir(dir, 0755)
	if err == nil {
		return nil
	}
	if !errors.Is(err, os.ErrExist) {
		return err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return unsafeRefusal(errors.New("not a folder"), LayerProject, dir, "not a folder")
	}
	return nil
}

// applyEdit runs edit on a copy of fields. It returns the copy and the keys
// whose value the edit added, removed or changed, in sorted order.
func applyEdit(fields map[string]json.RawMessage, edit func(map[string]json.RawMessage) error) (map[string]json.RawMessage, []string, error) {
	candidate := make(map[string]json.RawMessage, len(fields)+1)
	for key, value := range fields {
		candidate[key] = value
	}
	if err := edit(candidate); err != nil {
		return nil, nil, err
	}
	var changed []string
	for key, value := range candidate {
		if old, ok := fields[key]; !ok || !bytes.Equal(old, value) {
			changed = append(changed, key)
		}
	}
	for key := range fields {
		if _, ok := candidate[key]; !ok {
			changed = append(changed, key)
		}
	}
	sort.Strings(changed)
	return candidate, changed, nil
}

// validateCandidate checks each known key of fields. A null value is the same
// as no value.
func validateCandidate(fields map[string]json.RawMessage, layer Layer, path string) error {
	for _, key := range shippedKeys {
		raw, ok := fields[key]
		if !ok || isJSONNull(raw) {
			continue
		}
		var scratch Effective
		if scratch.set(key, raw) != "" {
			return refusal(ErrInvalidSetting, layer, path, key, "the value is not valid; nothing is written")
		}
	}
	return nil
}

// checkShadow refuses a user write of a key that the trusted project file
// sets. A project file that cannot be read applies nothing, and a key that a
// project cannot set never applies, so neither shadows a user key.
func (l *Layered) checkShadow(changed []string) error {
	if len(changed) == 0 || !l.project.Trusted || l.project.Root == "" {
		return nil
	}
	path := l.project.settingsPath()
	fields, problem := readProjectSettings(path)
	if problem != "" {
		return nil
	}
	kept, _ := filterProjectKeys(fields)
	for _, key := range changed {
		if value, ok := kept[key]; ok && !isJSONNull(value) {
			return refusal(ErrShadowed, LayerProject, path, key, "the project settings set this key; the user value would not apply here")
		}
	}
	return nil
}
