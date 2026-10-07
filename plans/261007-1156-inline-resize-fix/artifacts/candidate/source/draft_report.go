package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

func writeDraftReport(path string, m model) error {
	report := struct {
		Text, SHA256                   string
		Lines, Pastes, Submits, Cursor int
		Negotiated                     bool
	}{m.text, fmt.Sprintf("%x", sha256.Sum256([]byte(m.text))), strings.Count(m.text, "\n") + 1, m.pastes, m.submits, m.cursor, m.negotiated}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0600)
}
