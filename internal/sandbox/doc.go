// Package sandbox runs one command in its own process group.
//
// It gives each command a filtered environment (ChildEnv), a bounded memory
// buffer with a head and a tail, an optional spill file for large output, a
// model view of the output (View), and a two-step group kill that leaves no
// process behind. It has no job table and no tool code. There is no Sandbox or
// Manager type yet, and no isolation beyond the process group: Unix only, no
// PTY, no cgroup.
//
// See README.md in this folder for what belongs here and the import rules.
package sandbox
