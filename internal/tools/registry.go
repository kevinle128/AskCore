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
// all methods are safe for concurrent use.
type Registry struct {
	mu      sync.RWMutex
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
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, taken := r.index[decl.Name]; taken {
		return fmt.Errorf("tools: tool %q is already registered", decl.Name)
	}
	if r.index == nil {
		r.index = map[string]int{}
	}
	r.index[decl.Name] = len(r.entries)
	r.entries = append(r.entries, entry{tool: t, src: src, decl: decl, params: params})
	return nil
}

// Lookup finds a tool by its exact, case-sensitive name.
func (r *Registry) Lookup(name string) (Tool, SourceInfo, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	i, ok := r.index[name]
	if !ok {
		return nil, SourceInfo{}, false
	}
	return r.entries[i].tool, r.entries[i].src, true
}

// Decls returns the declarations in registration order.
func (r *Registry) Decls() []protocol.ToolDecl {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]protocol.ToolDecl, len(r.entries))
	for i, e := range r.entries {
		out[i] = e.decl
	}
	return out
}

// Prepare turns the raw arguments of a call into the arguments Execute sees.
// Missing or null input becomes {}. Values are coerced to the schema types
// and then validated. A validation failure returns an error whose text is
// meant for the model.
func (r *Registry) Prepare(name string, raw json.RawMessage) (json.RawMessage, error) {
	r.mu.RLock()
	i, ok := r.index[name]
	var params *shape
	if ok {
		params = r.entries[i].params
	}
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("tools: unknown tool %q", name)
	}
	return prepare(name, params, raw)
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
