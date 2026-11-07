// Package tasks runs background work for one Agent. The Registry owns the
// goroutines of its tasks, gives each task one terminal state, and passes
// exactly one notice for each task to a Deliver function that the Agent
// supplies.
//
// See README.md in this folder for what belongs here and the import rules.
package tasks
