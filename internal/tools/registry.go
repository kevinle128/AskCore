package tools

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"AskCore/pkg/protocol"
)

// Registry holds the tools of one agent. The zero value is ready to use, and
// all methods are safe for concurrent use. It is copy-on-write: Register and
// Unregister replace the current Snapshot, and a Snapshot never changes, so a
// run can keep one for a whole turn while the registry changes.
type Registry struct {
	mu   sync.Mutex
	snap *Snapshot
}

// Snapshot is an immutable view of the registry at one moment. A nil
// Snapshot holds no tools.
type Snapshot struct {
	entries []entry
	index   map[string]int
}

type entry struct {
	tool   Tool
	src    SourceInfo
	decl   protocol.ToolDecl
	params *shape
}

const schemaURL = "mem:///parameters.json"

// Register adds a tool. It fails when the tool has no name or no parameters
// schema, when the schema does not compile, or when the name is taken.
func (r *Registry) Register(t Tool, src SourceInfo) error {
	decl := t.Decl()
	if decl.Name == "" {
		return errors.New("tools: tool has no name")
	}
	params, err := compileParameters(decl.Parameters)
	if err != nil {
		return fmt.Errorf("tools: tool %q: %w", decl.Name, err)
	}
	decl.Parameters = bytes.Clone(decl.Parameters)
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, _, taken := r.snap.Lookup(decl.Name); taken {
		return fmt.Errorf("tools: tool %q is already registered", decl.Name)
	}
	var entries []entry
	if r.snap != nil {
		entries = r.snap.entries
	}
	r.snap = newSnapshot(append(entries[:len(entries):len(entries)], entry{tool: t, src: src, decl: decl, params: params}))
	return nil
}

// Unregister removes the tool with the given name and reports whether it was
// registered. A Snapshot taken before keeps the tool.
func (r *Registry) Unregister(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	i, ok := r.snap.find(name)
	if !ok {
		return false
	}
	old := r.snap.entries
	entries := make([]entry, 0, len(old)-1)
	entries = append(append(entries, old[:i]...), old[i+1:]...)
	r.snap = newSnapshot(entries)
	return true
}

// Snapshot returns the current tools. A nil Registry gives a nil Snapshot.
func (r *Registry) Snapshot() *Snapshot {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.snap
}

// Lookup finds a tool of the current snapshot by its exact, case-sensitive name.
func (r *Registry) Lookup(name string) (Tool, SourceInfo, bool) { return r.Snapshot().Lookup(name) }

// Decls returns the declarations of the current snapshot in registration order.
func (r *Registry) Decls() []protocol.ToolDecl { return r.Snapshot().Decls() }

// Prepare prepares arguments with the current snapshot (see Snapshot.Prepare).
func (r *Registry) Prepare(name string, raw json.RawMessage) (json.RawMessage, error) {
	return r.Snapshot().Prepare(name, raw)
}

func newSnapshot(entries []entry) *Snapshot {
	s := &Snapshot{entries: entries, index: make(map[string]int, len(entries))}
	for i, e := range entries {
		s.index[e.decl.Name] = i
	}
	return s
}

func (s *Snapshot) find(name string) (int, bool) {
	if s == nil {
		return 0, false
	}
	i, ok := s.index[name]
	return i, ok
}

// Lookup finds a tool by its exact, case-sensitive name.
func (s *Snapshot) Lookup(name string) (Tool, SourceInfo, bool) {
	i, ok := s.find(name)
	if !ok {
		return nil, SourceInfo{}, false
	}
	return s.entries[i].tool, s.entries[i].src, true
}

// Decls returns the declarations in registration order. The caller may
// change the result; the snapshot keeps its own copy.
func (s *Snapshot) Decls() []protocol.ToolDecl {
	if s == nil {
		return []protocol.ToolDecl{}
	}
	out := make([]protocol.ToolDecl, len(s.entries))
	for i, e := range s.entries {
		out[i] = e.decl
		out[i].Parameters = bytes.Clone(e.decl.Parameters)
	}
	return out
}

// Prepare turns the raw arguments of a call into the arguments Execute sees.
// Missing or null input becomes {}. Values are coerced to the schema types
// and then validated. A validation failure returns an error whose text is
// meant for the model.
func (s *Snapshot) Prepare(name string, raw json.RawMessage) (json.RawMessage, error) {
	i, ok := s.find(name)
	if !ok {
		return nil, fmt.Errorf("tools: unknown tool %q", name)
	}
	return prepare(name, s.entries[i].params, raw)
}

func compileParameters(raw json.RawMessage) (*shape, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return nil, errors.New("no parameters schema")
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("parameters schema: %w", err)
	}
	c := jsonschema.NewCompiler()
	c.UseLoader(noLoader{})
	if err := c.AddResource(schemaURL, doc); err != nil {
		return nil, fmt.Errorf("parameters schema: %w", err)
	}
	sch, err := c.Compile(schemaURL)
	if err != nil {
		return nil, fmt.Errorf("parameters schema: %w", err)
	}
	return buildShape(doc, sch), nil
}

// noLoader keeps a schema self-contained: a $ref to a file or a URL would
// otherwise read the disk or the network while a tool registers.
type noLoader struct{}

func (noLoader) Load(url string) (any, error) {
	return nil, fmt.Errorf("external schema %s is not allowed", url)
}
