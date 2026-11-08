package settings

import (
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"syscall"
	"testing"
	"time"
)

const validModelsBody = `{
  // local server
  "providers": {"local": {"baseUrl": "http://127.0.0.1:11434/v1", "api": "openai-completions",
    "apiKey": "ollama", "models": [{"id": "qwen3-coder:30b"}]}},
}`

func newModelsHome(t *testing.T) (string, *ModelsFiles) {
	t.Helper()
	home := filepath.Join(t.TempDir(), "home")
	if err := os.Mkdir(home, 0700); err != nil {
		t.Fatal(err)
	}
	m, err := NewModelsFiles(home, []string{"anthropic", "openai"})
	if err != nil {
		t.Fatal(err)
	}
	return home, m
}

func writeModels(t *testing.T, m *ModelsFiles, body string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(m.Path(), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(m.Path(), mode); err != nil {
		t.Fatal(err)
	}
}

// setMTime gives the file a known modification time, so a test does not
// depend on the clock resolution of the file system.
func setMTime(t *testing.T, path string, at time.Time) {
	t.Helper()
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
}

func problemTexts(problems []Diagnostic) []string {
	out := make([]string, 0, len(problems))
	for _, p := range problems {
		out = append(out, p.Text)
	}
	return out
}

func TestModelsFilesKeepsLastValidCopyAfterBadEdit(t *testing.T) {
	_, m := newModelsHome(t)
	base := time.Unix(1_800_000_000, 0)
	writeModels(t, m, validModelsBody, 0600)
	setMTime(t, m.Path(), base)
	first, problems, gen1 := m.Load()
	if len(problems) != 0 || len(first.Providers["local"].Models) != 1 || gen1 == 0 {
		t.Fatalf("first load: %+v %v %d", first, problemTexts(problems), gen1)
	}
	if _, _, again := m.Load(); again != gen1 {
		t.Fatalf("an unchanged file changed the generation: %d to %d", gen1, again)
	}

	writeModels(t, m, `{"providers": {"local": `, 0600)
	setMTime(t, m.Path(), base.Add(time.Second))
	kept, problems, gen2 := m.Load()
	if !reflect.DeepEqual(kept, first) {
		t.Fatalf("the last valid copy was not kept: %+v", kept)
	}
	want := []string{"invalid JSON at line 1: unexpected end of file", "the last valid copy is used"}
	if !reflect.DeepEqual(problemTexts(problems), want) {
		t.Fatalf("problems %q", problemTexts(problems))
	}
	if problems[0].Path != m.Path() || problems[0].Layer != LayerUser {
		t.Fatalf("problem %+v", problems[0])
	}
	if gen2 != gen1+1 {
		t.Fatalf("generation %d after one change from %d", gen2, gen1)
	}
	if _, _, again := m.Load(); again != gen2 {
		t.Fatalf("an unchanged bad file changed the generation: %d to %d", gen2, again)
	}

	writeModels(t, m, `{"providers": {"other": {"baseUrl": "http://h"}}}`, 0600)
	setMTime(t, m.Path(), base.Add(2*time.Second))
	next, problems, gen3 := m.Load()
	if len(problems) != 0 || next.Providers["other"].BaseURL != "http://h" || len(next.Providers) != 1 || gen3 != gen2+1 {
		t.Fatalf("a valid edit must replace the copy: %+v %v %d", next, problemTexts(problems), gen3)
	}
}

func TestModelsFilesBadFileWithoutEarlierCopy(t *testing.T) {
	_, m := newModelsHome(t)
	writeModels(t, m, `[1]`, 0600)
	file, problems, gen := m.Load()
	if len(file.Providers) != 0 || gen == 0 {
		t.Fatalf("file %+v gen %d", file, gen)
	}
	if !reflect.DeepEqual(problemTexts(problems), []string{"not a JSON object"}) {
		t.Fatalf("problems %q", problemTexts(problems))
	}
}

func TestModelsFilesSameSizeEditIsRead(t *testing.T) {
	_, m := newModelsHome(t)
	base := time.Unix(1_800_000_000, 0)
	writeModels(t, m, `{"providers": {"aaaa": {"baseUrl": "http://h"}}}`, 0600)
	setMTime(t, m.Path(), base)
	_, _, gen1 := m.Load()
	writeModels(t, m, `{"providers": {"bbbb": {"baseUrl": "http://h"}}}`, 0600)
	setMTime(t, m.Path(), base.Add(time.Second))
	file, _, gen2 := m.Load()
	if _, ok := file.Providers["bbbb"]; !ok || gen2 != gen1+1 {
		t.Fatalf("the same-size edit was not read: %+v gen %d to %d", file, gen1, gen2)
	}
}

func TestModelsFilesFIFOIsRefusedWithoutBlocking(t *testing.T) {
	_, m := newModelsHome(t)
	if err := syscall.Mkfifo(m.Path(), 0600); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	problems := loadModelsWithin(t, m)
	if len(problems) != 1 || problems[0].Text != "unsafe or invalid file: file owner, type or mode" {
		t.Fatalf("problems %q", problemTexts(problems))
	}
	// The entry check refuses the FIFO before the open. The open itself must
	// not block either, for a FIFO that replaces the file after the check.
	done := make(chan error, 1)
	go func() {
		_, _, err := readPrivateFile(m.Path())
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("the opened FIFO must be refused")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the open blocked on a FIFO")
	}
}

func TestModelsFilesSymlinkIsRefused(t *testing.T) {
	home, m := newModelsHome(t)
	target := filepath.Join(home, "target.json")
	if err := os.WriteFile(target, []byte(validModelsBody), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, m.Path()); err != nil {
		t.Fatal(err)
	}
	file, problems, _ := m.Load()
	if len(file.Providers) != 0 || len(problems) != 1 || problems[0].Path != m.Path() {
		t.Fatalf("file %+v problems %+v", file, problems)
	}
	if _, _, err := readPrivateFile(m.Path()); err == nil {
		t.Fatal("the open must not follow a link")
	}
}

func TestModelsFilesUserFileModeIsChecked(t *testing.T) {
	skipAsRoot(t)
	_, m := newModelsHome(t)
	writeModels(t, m, validModelsBody, 0644)
	file, problems, gen1 := m.Load()
	if len(file.Providers) != 0 || len(problems) != 1 || problems[0].Text != "unsafe or invalid file: file owner, type or mode" {
		t.Fatalf("file %+v problems %q", file, problemTexts(problems))
	}
	// A chmod changes neither the content nor the modification time, but the
	// next load must read the file.
	if err := os.Chmod(m.Path(), 0600); err != nil {
		t.Fatal(err)
	}
	file, problems, gen2 := m.Load()
	if len(problems) != 0 || len(file.Providers) != 1 || gen2 != gen1+1 {
		t.Fatalf("after chmod: %+v %q gen %d to %d", file, problemTexts(problems), gen1, gen2)
	}
}

func TestModelsFilesHomeModeIsChecked(t *testing.T) {
	skipAsRoot(t)
	home, m := newModelsHome(t)
	writeModels(t, m, validModelsBody, 0600)
	if err := os.Chmod(home, 0755); err != nil {
		t.Fatal(err)
	}
	_, problems, gen1 := m.Load()
	if len(problems) != 1 || problems[0].Path != home {
		t.Fatalf("problems %+v", problems)
	}
	if err := os.Chmod(home, 0700); err != nil {
		t.Fatal(err)
	}
	file, problems, gen2 := m.Load()
	if len(problems) != 0 || len(file.Providers) != 1 || gen2 != gen1+1 {
		t.Fatalf("after the home fix: %+v %q gen %d to %d", file, problemTexts(problems), gen1, gen2)
	}
}

func TestModelsFilesMissingFileClearsCopy(t *testing.T) {
	home, m := newModelsHome(t)
	file, problems, gen0 := m.Load()
	if gen0 == 0 || len(file.Providers) != 0 || len(problems) != 0 {
		t.Fatalf("missing file: %+v %v %d", file, problems, gen0)
	}
	if _, _, again := m.Load(); again != gen0 {
		t.Fatal("a file that stays missing changed the generation")
	}
	writeModels(t, m, validModelsBody, 0600)
	if file, _, gen1 := m.Load(); len(file.Providers) != 1 || gen1 != gen0+1 {
		t.Fatalf("written file: %+v gen %d", file, gen1)
	}
	if err := os.Remove(m.Path()); err != nil {
		t.Fatal(err)
	}
	file, problems, gen2 := m.Load()
	if len(file.Providers) != 0 || len(problems) != 0 || gen2 != gen0+2 {
		t.Fatalf("removed file: %+v %v gen %d", file, problems, gen2)
	}
	// A bad file after the removal has no copy to keep.
	writeModels(t, m, `{`, 0600)
	if _, problems, _ := m.Load(); len(problems) != 1 {
		t.Fatalf("problems %q", problemTexts(problems))
	}
	// A missing home is a missing file; Load creates nothing.
	missing, err := NewModelsFiles(filepath.Join(home, "absent"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if file, problems, gen := missing.Load(); len(file.Providers) != 0 || len(problems) != 0 || gen != 1 {
		t.Fatalf("missing home: %+v %v %d", file, problems, gen)
	}
	if _, err := os.Stat(filepath.Join(home, "absent")); !os.IsNotExist(err) {
		t.Fatalf("Load created the home: %v", err)
	}
}

func TestModelsFilesConcurrentLoads(t *testing.T) {
	_, m := newModelsHome(t)
	writeModels(t, m, validModelsBody, 0600)
	var wg sync.WaitGroup
	gens := make(chan uint64, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			file, problems, gen := m.Load()
			if len(file.Providers) != 1 || len(problems) != 0 {
				t.Errorf("load: %+v %v", file, problems)
			}
			gens <- gen
		}()
	}
	wg.Wait()
	close(gens)
	for gen := range gens {
		if gen != 1 {
			t.Fatalf("an unchanged file gave generation %d", gen)
		}
	}
}

func loadModelsWithin(t *testing.T, m *ModelsFiles) []Diagnostic {
	t.Helper()
	done := make(chan []Diagnostic, 1)
	go func() {
		_, problems, _ := m.Load()
		done <- problems
	}()
	select {
	case problems := <-done:
		return problems
	case <-time.After(5 * time.Second):
		t.Fatal("load blocked")
	}
	return nil
}

func TestModelsFilesOlderReadThatEndsLastIsNotStored(t *testing.T) {
	_, m := newModelsHome(t)
	base := time.Unix(1_800_000_000, 0)
	writeModels(t, m, `{"providers": {"aaaa": {"baseUrl": "http://h"}}}`, 0600)
	setMTime(t, m.Path(), base)
	older := m.read()
	writeModels(t, m, `{"providers": {"bbbb": {"baseUrl": "http://h"}}}`, 0600)
	setMTime(t, m.Path(), base.Add(time.Second))
	newer := m.read()

	_, _, gen := m.store(newer)
	file, _, genAfter := m.store(older)
	if _, ok := file.Providers["bbbb"]; !ok || genAfter != gen {
		t.Fatalf("the older read replaced the newer one: %+v gen %d to %d", file, gen, genAfter)
	}
	file, _, genLoad := m.Load()
	if _, ok := file.Providers["bbbb"]; !ok || genLoad != gen {
		t.Fatalf("an unchanged file gave a new result: %+v gen %d to %d", file, gen, genLoad)
	}
}
