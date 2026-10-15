package tools

// SourceKind says what kind of owner registered a tool.
type SourceKind string

const (
	SourceBuiltin   SourceKind = "builtin"
	SourceExtension SourceKind = "extension"
	SourceMCP       SourceKind = "mcp"
)

// SourceInfo records where a registered tool came from. Name is the
// extension or MCP server name; Path is the file it was loaded from, if any.
type SourceInfo struct {
	Kind SourceKind
	Name string
	Path string
}
