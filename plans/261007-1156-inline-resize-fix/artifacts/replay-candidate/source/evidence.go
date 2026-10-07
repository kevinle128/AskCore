package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type evidenceArtifact struct {
	Path   string
	SHA256 string
}
type evidenceResult struct {
	Result    string
	Reason    string
	Command   string
	Artifacts []evidenceArtifact
}
type gateEvidence struct {
	Name         string
	Criteria     string
	Automated    evidenceResult
	RealTerminal evidenceResult
}
type evidenceManifest struct {
	Version                int
	CleanRerun             evidenceResult
	Environment            map[string]string
	Dimensions             [][2]int
	DependencyGraph        evidenceArtifact
	Gates                  []gateEvidence
	Optional               map[string]evidenceResult
	RendererFixClasses     int
	OutputOwnerAdaptations []string
	D14Ready               bool
	Recommendation         string
}

func artifact(dir, path string) (evidenceArtifact, error) {
	if filepath.IsAbs(path) || filepath.Clean(path) != path || path == ".." || strings.HasPrefix(path, ".."+string(filepath.Separator)) {
		return evidenceArtifact{}, fmt.Errorf("artifact path escapes evidence: %s", path)
	}
	data, err := os.ReadFile(filepath.Join(dir, path))
	if err != nil {
		return evidenceArtifact{}, err
	}
	sum := sha256.Sum256(data)
	return evidenceArtifact{path, hex.EncodeToString(sum[:])}, nil
}
func validateResult(r evidenceResult, dir string) error {
	switch r.Result {
	case "pass":
		if r.Command == "" || len(r.Artifacts) == 0 {
			return fmt.Errorf("PASS lacks command or artifact")
		}
	case "pending", "not-run", "unsupported", "fail":
		if r.Reason == "" {
			return fmt.Errorf("%s lacks reason", r.Result)
		}
	default:
		return fmt.Errorf("invalid result %q", r.Result)
	}
	for _, a := range r.Artifacts {
		got, err := artifact(dir, a.Path)
		if err != nil {
			return err
		}
		if got.SHA256 != a.SHA256 {
			return fmt.Errorf("artifact hash mismatch: %s", a.Path)
		}
	}
	return nil
}
func validateManifest(m evidenceManifest, dir string) error {
	if m.Version != 1 || len(m.Environment) == 0 || len(m.Dimensions) == 0 {
		return fmt.Errorf("missing environment or schema")
	}
	for _, key := range []string{"os", "go", "locale", "terminal", "emulator", "real_terminal", "source_commit", "bubbletea", "pty", "workspace"} {
		if m.Environment[key] == "" {
			return fmt.Errorf("missing environment %s", key)
		}
	}
	for _, size := range m.Dimensions {
		if size[0] <= 0 || size[1] <= 0 {
			return fmt.Errorf("invalid terminal dimensions")
		}
	}
	graph, err := artifact(dir, m.DependencyGraph.Path)
	if err != nil {
		return err
	}
	if graph.SHA256 != m.DependencyGraph.SHA256 {
		return fmt.Errorf("dependency graph hash mismatch")
	}
	required := map[string]bool{"G1": false, "G2": false, "G3": false, "G6": false}
	if err := validateResult(m.CleanRerun, dir); err != nil {
		return fmt.Errorf("clean rerun: %w", err)
	}
	ready := m.CleanRerun.Result == "pass"
	for _, g := range m.Gates {
		seen, known := required[g.Name]
		if !known || seen {
			return fmt.Errorf("unknown or duplicate gate %s", g.Name)
		}
		required[g.Name] = true
		if g.Criteria == "" {
			return fmt.Errorf("gate lacks criteria")
		}
		if g.Automated.Command == "" || len(g.Automated.Artifacts) == 0 {
			return fmt.Errorf("%s automated evidence lacks command or artifact", g.Name)
		}
		if err := validateResult(g.Automated, dir); err != nil {
			return fmt.Errorf("%s automated: %w", g.Name, err)
		}
		if err := validateResult(g.RealTerminal, dir); err != nil {
			return fmt.Errorf("%s real: %w", g.Name, err)
		}
		ready = ready && g.Automated.Result == "pass" && g.RealTerminal.Result == "pass"
	}
	for name, seen := range required {
		if !seen {
			return fmt.Errorf("missing gate %s", name)
		}
	}
	if m.D14Ready != ready {
		return fmt.Errorf("D14 readiness differs from blocking evidence")
	}
	for _, name := range []string{"G4", "G5", "pixel", "palette", "keepalive", "modifyOtherKeys"} {
		r, ok := m.Optional[name]
		if !ok {
			return fmt.Errorf("missing optional result %s", name)
		}
		if err := validateResult(r, dir); err != nil {
			return err
		}
	}
	if m.RendererFixClasses < 0 || m.RendererFixClasses > 5 {
		return fmt.Errorf("renderer fix budget exceeded")
	}
	return nil
}
func writeEvidence(dir string) error {
	log, err := os.ReadFile(filepath.Join(dir, "phase4-final-race.txt"))
	if err != nil {
		return err
	}
	if !regexp.MustCompile(`(?m)^PASS
ok\s+askcore-t0-inline\s+`).Match(log) || regexp.MustCompile(`(?m)^FAIL`).Match(log) {
		return fmt.Errorf("blocking test log does not report a completed PASS")
	}
	graph, err := artifact(dir, "module-graph.txt")
	if err != nil {
		return err
	}
	m := evidenceManifest{Version: 1, Environment: map[string]string{"os": "Darwin 25.6.0 arm64", "go": "go1.27.0", "locale": "C.UTF-8", "terminal": "PTY xterm-256color with explicit query responder", "emulator": "github.com/charmbracelet/x/vt v0.0.0-20261004011457-ad85c59fdf4e", "real_terminal": "iTerm2 3.7.3; access denied; observations pending", "source_commit": "d9279ab5adc341312550430d7579f18a05e6b17d (product baseline; scratch source checksums separate)", "bubbletea": "charm.land/bubbletea/v2 v2.0.10", "pty": "github.com/creack/pty v1.1.24", "workspace": "GOWORK=off; isolated askcore-t0-inline module"}, Dimensions: [][2]int{{40, 12}, {52, 15}, {13, 6}, {12, 6}, {2, 1}, {1, 1}, {10, 12}, {10, 4}, {40, 4}}, DependencyGraph: graph, Optional: map[string]evidenceResult{}, Recommendation: "Stock Println is the measured candidate with an ordered output observer, failure latch, and owned cleanup; D14 pending real-terminal evidence.", OutputOwnerAdaptations: []string{"explicit insertion WriteString observation preserving term.File", "complete-write confirmed frontier and synchronous error/short-write latch", "post-renderer owned mode2026 reset and filtered restoration after failure"}}
	m.CleanRerun = evidenceResult{Result: "pending", Reason: "Clean isolated module rerun not yet recorded."}
	if data, readErr := os.ReadFile(filepath.Join(dir, "phase5-clean-race.txt")); readErr == nil {
		if !regexp.MustCompile(`(?m)^PASS
ok\s+askcore-t0-inline\s+`).Match(data) || regexp.MustCompile(`(?m)^FAIL`).Match(data) {
			return fmt.Errorf("clean race log does not report completed PASS")
		}
		a, err := artifact(dir, "phase5-clean-race.txt")
		if err != nil {
			return err
		}
		m.CleanRerun = evidenceResult{Result: "pass", Command: "GOWORK=off GOCACHE=/private/tmp/askcore-t0-09kf6w9r-go-cache GOMODCACHE=/private/tmp/askcore-t0-mod-cache go test -race -v -count=1 -timeout 60s ./...", Artifacts: []evidenceArtifact{a}}
	} else if !os.IsNotExist(readErr) {
		return readErr
	}
	specs := []struct{ name, criteria, capture string }{
		{"G1", "2000 ordered unique lines; oversized/shrink/live cursor; stable ready prefix; real pending resize; partial-write stop/no replay", "TestG1Stock-ordered2000.ansi"},
		{"G2", "12 selector items in three rows; exact window/cursor; draft restore and stable-height history; real resize", "TestG2Selector.ansi"},
		{"G3", "hand-written CJK/emoji cells; widths1/2 height1; repeated real resize; exact-once committed output; native height reflow pending", "TestG3ResizeDuringStream.ansi"},
		{"G6", "actual key negotiation/fallback; one 2000-line PasteMsg; full normalized hash; all exit routes; partial sync cleanup", "TestG6Input-true.ansi"},
	}
	for _, spec := range specs {
		r := evidenceResult{Result: "pass", Command: "GOWORK=off GOCACHE=/private/tmp/askcore-t0-09kf6w9r-go-cache GOMODCACHE=/private/tmp/askcore-t0-mod-cache go test -race -v -count=1 -timeout 60s ./..."}
		for _, path := range []string{"phase4-final-race.txt", spec.capture} {
			a, err := artifact(dir, path)
			if err != nil {
				return err
			}
			r.Artifacts = append(r.Artifacts, a)
		}
		m.Gates = append(m.Gates, gateEvidence{spec.name, spec.criteria, r, evidenceResult{Result: "pending", Reason: "iTerm2 app access denied; no direct observation. See README manual checklist."}})
	}
	m.Optional["G4"] = evidenceResult{Result: "not-run", Reason: "No Kitty graphics responder or selected-terminal image capability observation; no image-history PASS claimed."}
	m.Optional["G5"] = evidenceResult{Result: "not-run", Reason: "M2 alternate-buffer transcript reconstruction is not exercised by this inline fixture; real terminal access unavailable."}
	for _, name := range []string{"pixel", "palette", "keepalive", "modifyOtherKeys"} {
		m.Optional[name] = evidenceResult{Result: "not-run", Reason: "Optional nonblocking probe not executed; selected real terminal inaccessible."}
	}
	if err := validateManifest(m, dir); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "manifest.json"), append(data, '\n'), 0644)
}
