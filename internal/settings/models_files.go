package settings

import (
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
)

const modelsFile = "models.json"

// ModelsFiles is the process memory of the user file <ASK_HOME>/models.json.
// Use one value for each process. Load reads the file again only when the
// file changed, and keeps the last valid copy when an edit breaks the file.
// It takes no timer and no watcher: the caller calls Load at the points where
// it builds its model view.
type ModelsFiles struct {
	home    string
	path    string
	builtIn map[string]bool

	// readSeq numbers the reads in the order they start.
	readSeq atomic.Uint64

	mu         sync.Mutex
	loaded     bool
	storedSeq  uint64
	stamp      modelsStamp
	generation uint64
	file       ModelsFile
	problems   []Diagnostic
	// hasValid is true when file is the last valid copy of the file.
	hasValid bool
}

// modelsStamp identifies one state of the file. On a read, it comes from the
// FileInfo of the opened file, so it belongs to the bytes that were read. The
// mode is part of it, so a chmod that makes the file valid is seen. problem is
// the text of a file problem, so a fixed home folder is seen too.
type modelsStamp struct {
	exists  bool
	mode    os.FileMode
	modTime int64
	size    int64
	problem string
}

// NewModelsFiles constructs the memory of <home>/models.json. Empty home uses
// ASK_HOME or ~/.ask. builtIn holds the ids of the built-in providers: a
// provider that is not built in needs baseUrl and apiKey for its models.
func NewModelsFiles(home string, builtIn []string) (*ModelsFiles, error) {
	home, err := ResolveHome(home)
	if err != nil {
		return nil, err
	}
	set := make(map[string]bool, len(builtIn))
	for _, id := range builtIn {
		set[id] = true
	}
	return &ModelsFiles{home: home, path: filepath.Join(home, modelsFile), builtIn: set}, nil
}

// Path returns the path of the user models.json.
func (m *ModelsFiles) Path() string { return m.path }

// Load returns the current models file, its problems and a generation. The
// generation increases each time the result changes; a caller that has the
// same generation can keep what it built from the earlier result. The first
// Load returns generation 1 or more, also for a missing file.
//
// A valid file replaces the last valid copy. A file that cannot be read or
// is not valid JSONC keeps the last valid copy and adds the problem "the last
// valid copy is used". A missing file (or home) clears the copy. Load creates
// nothing. The file and the problems are read only for the caller.
func (m *ModelsFiles) Load() (file ModelsFile, problems []Diagnostic, generation uint64) {
	return m.store(m.read())
}

// modelsRead is the result of one read of the file.
type modelsRead struct {
	seq         uint64
	stamp       modelsStamp
	parsed      ModelsFile
	problems    []Diagnostic
	fileProblem *Diagnostic
}

// read reads and parses the file without the lock.
func (m *ModelsFiles) read() modelsRead {
	r := modelsRead{seq: m.readSeq.Add(1)}
	data, info, problemPath, err := readUserFileBytes(m.home, modelsFile)
	switch {
	case err != nil:
		text := userProblemText(err)
		r.stamp = m.lstatStamp(text)
		r.fileProblem = &Diagnostic{Layer: LayerUser, Path: problemPath, Text: text}
	case info == nil:
		r.stamp = modelsStamp{}
	default:
		r.stamp = modelsStamp{exists: true, mode: info.Mode(), modTime: info.ModTime().UnixNano(), size: info.Size()}
		r.parsed, r.problems, err = parseModelsFile(data, m.path, m.builtIn)
		if err != nil {
			r.stamp.problem = err.Error()
			r.fileProblem = &Diagnostic{Layer: LayerUser, Path: m.path, Text: err.Error()}
		}
	}
	return r
}

// store keeps r when it started after the stored read. Two loads can end in
// the other order; the read that started first may hold older bytes, so it
// must not replace the result of a later read.
func (m *ModelsFiles) store(r modelsRead) (ModelsFile, []Diagnostic, uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.loaded && (r.seq < m.storedSeq || r.stamp == m.stamp) {
		return m.file, slices.Clip(m.problems), m.generation
	}
	m.loaded = true
	m.storedSeq = r.seq
	m.stamp = r.stamp
	m.generation++
	switch {
	case r.fileProblem != nil:
		m.problems = []Diagnostic{*r.fileProblem}
		if m.hasValid {
			m.problems = append(m.problems, Diagnostic{Layer: LayerUser, Path: m.path, Text: "the last valid copy is used"})
		}
	case !r.stamp.exists:
		m.file, m.problems, m.hasValid = ModelsFile{}, nil, false
	default:
		m.file, m.problems, m.hasValid = r.parsed, r.problems, true
	}
	return m.file, slices.Clip(m.problems), m.generation
}

// lstatStamp returns the stamp of a file that has a file problem. The file
// was not read, so the stamp comes from the entry itself.
func (m *ModelsFiles) lstatStamp(problem string) modelsStamp {
	stamp := modelsStamp{problem: problem}
	if info, err := os.Lstat(m.path); err == nil {
		stamp.exists = true
		stamp.mode = info.Mode()
		stamp.modTime = info.ModTime().UnixNano()
		stamp.size = info.Size()
	}
	return stamp
}
