// Package jsonlstore implements store.SessionStore with one JSONL file for each
// session. It works on Unix only: write ownership is a flock on a lock file.
//
// See README.md in this folder for the file layout and the repair rules.
package jsonlstore
