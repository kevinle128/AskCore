package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestManualPasteReport(t *testing.T) {
	bin := t.TempDir() + "/paste"
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build %v %s", err, out)
	}
	path := t.TempDir() + "/draft.json"
	f := spawnG6(t, bin, true, "g6", "", path)
	f.waitLine(func(s string) bool { return strings.HasSuffix(s, "true") })
	var text strings.Builder
	for i := 0; i < 2000; i++ {
		if i > 0 {
			text.WriteString("\r\n")
		}
		fmt.Fprintf(&text, "%04d\t界😀\x1b[13;2u", i)
	}
	f.input("\x1b[200~" + text.String() + "\x1b[201~")
	normalized := strings.ReplaceAll(text.String(), "\r\n", "\n")
	f.draft(normalized, 1, 0, true)
	f.finish("quit", 0, 0)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var report struct {
		Text, SHA256           string
		Lines, Pastes, Submits int
	}
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	if report.Text != normalized || report.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(normalized))) || report.Lines != 2000 || report.Pastes != 1 || report.Submits != 0 {
		t.Fatal("manual draft report loses paste integrity")
	}
}
