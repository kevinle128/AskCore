package main

import (
	"encoding/json"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestEvidenceCommand(t *testing.T) {
	bin := t.TempDir() + "/evidence"
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build %v %s", err, out)
	}
	out, err := exec.Command(bin, "--scenario", "all", "--evidence", evidenceFixture(t)).CombinedOutput()
	if err != nil {
		t.Fatalf("evidence command failed: %v %s", err, out)
	}
}

func TestEvidenceRequiredRecords(t *testing.T) {
	dir := evidenceFixture(t)
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	var document struct {
		Gates []struct {
			Name      string
			Automated struct {
				Command   string
				Artifacts []json.RawMessage
			}
		}
	}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &document); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"G1", "G2", "G3", "G6"} {
		command := ""
		artifacts := 0
		for _, gate := range document.Gates {
			if gate.Name == name {
				command = gate.Automated.Command
				artifacts = len(gate.Automated.Artifacts)
			}
		}
		t.Run(name+"-command", func(t *testing.T) {
			if command == "" {
				t.Fatalf("%s has no automated command", name)
			}
		})
		t.Run(name+"-artifact", func(t *testing.T) {
			if artifacts == 0 {
				t.Fatalf("%s has no automated artifact", name)
			}
		})
	}
}

func TestEvidenceValidationControls(t *testing.T) {
	dir := evidenceFixture(t)
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m evidenceManifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	if err := validateManifest(m, dir); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"command", "artifact", "hash", "readiness", "duplicate", "optional"} {
		t.Run(name, func(t *testing.T) {
			var broken evidenceManifest
			if err := json.Unmarshal(data, &broken); err != nil {
				t.Fatal(err)
			}
			switch name {
			case "command":
				broken.Gates[0].Automated.Command = ""
			case "artifact":
				broken.Gates[0].Automated.Artifacts = nil
			case "hash":
				broken.Gates[0].Automated.Artifacts[0].SHA256 = strings.Repeat("0", 64)
			case "readiness":
				broken.D14Ready = true
			case "duplicate":
				broken.Gates = append(broken.Gates, broken.Gates[0])
			case "optional":
				delete(broken.Optional, "G4")
			}
			if err := validateManifest(broken, dir); err == nil {
				t.Fatalf("invalid %s evidence accepted", name)
			}
		})
	}
}
func TestScratchBoundary(t *testing.T) {
	data, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "module askcore-t0-inline\n") || strings.Contains(string(data), "replace ") {
		t.Fatal("module isolation changed")
	}
	if _, err := os.Stat("go.work"); !os.IsNotExist(err) {
		t.Fatal("scratch workspace file exists or cannot be checked")
	}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range files {
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range f.Imports {
			p, err := strconv.Unquote(item.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(strings.ToLower(p), "askcore") || strings.HasPrefix(p, ".") || filepath.IsAbs(p) {
				t.Fatalf("root/local import %q in %s", p, path)
			}
		}
	}
	cmd := exec.Command("go", "env", "GOWORK")
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(out)) != "off" {
		t.Fatalf("workspace not off: %s", out)
	}
}

func TestOptionalCapabilities(t *testing.T) {
	dir := evidenceFixture(t)
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m evidenceManifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"G4", "G5", "pixel", "palette", "keepalive", "modifyOtherKeys"} {
		t.Run(name, func(t *testing.T) {
			r := m.Optional[name]
			if r.Result == "not-run" || r.Result == "unsupported" {
				if r.Reason == "" {
					t.Fatal("capability skip lacks reason")
				}
				t.Skip(r.Reason)
			}
			if r.Result != "pass" {
				t.Fatalf("capability result %q", r.Result)
			}
			if err := validateResult(r, dir); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func evidenceFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, path := range []string{"module-graph.txt", "phase4-final-race.txt", "TestG1Stock-ordered2000.ansi", "TestG2Selector.ansi", "TestG3ResizeDuringStream.ansi", "TestG6Input-true.ansi"} {
		data, err := os.ReadFile(filepath.Join("evidence", path))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, path), data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	if data, err := os.ReadFile("evidence/phase5-clean-race.txt"); err == nil {
		if err := os.WriteFile(filepath.Join(dir, "phase5-clean-race.txt"), data, 0644); err != nil {
			t.Fatal(err)
		}
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err := writeEvidence(dir); err != nil {
		t.Fatal(err)
	}
	return dir
}
