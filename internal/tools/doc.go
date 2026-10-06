// Package tools holds the tool registry and every builtin tool.
//
// Calls run alone unless ConcurrencySafe approves their validated arguments.
// Started bodies drain without a time limit after cancellation. Each body must
// return promptly when its context is cancelled, and a process tool must stop
// the entire process group. The Agent remains busy until those bodies return.
//
// See README.md in this folder for what belongs here and the import rules.
package tools
