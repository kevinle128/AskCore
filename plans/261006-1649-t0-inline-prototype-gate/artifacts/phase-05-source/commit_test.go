package main

import (
	"io"
	"os"
	"testing"
)

func TestCommitStringObservation(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "output")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	w := &faultOutput{File: file, errors: make(chan error, 1), commits: make(chan commitWrite, 1)}
	if _, err := io.WriteString(w, "G1[0000]"); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-w.commits:
		if event.N != 8 {
			t.Fatalf("observed n=%d", event.N)
		}
	default:
		t.Fatal("stock io.WriteString bypassed output observer")
	}
}

func TestCommitUnderlyingFailureLatch(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "output")
	if err != nil {
		t.Fatal(err)
	}
	file.Close()
	w := &faultOutput{File: file, errors: make(chan error, 1), commits: make(chan commitWrite, 2)}
	if _, err := w.Write([]byte("G1[0000]")); err == nil {
		t.Fatal("closed output returned success")
	}
	if !w.failed {
		t.Fatal("actual output failure was not latched")
	}
}

func TestCommitFaultPrefixes(t *testing.T) {
	for _, kind := range []string{"zero", "partial", "short", "permanent"} {
		t.Run(kind, func(t *testing.T) {
			file, err := os.CreateTemp(t.TempDir(), "output")
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			w := &faultOutput{File: file, errors: make(chan error, 1), commits: make(chan commitWrite, 2)}
			w.armCommit(kind)
			body := "G1[0000]"
			n, err := io.WriteString(w, body)
			if kind == "short" {
				if err != nil {
					t.Fatal("fixture short-nil path lost")
				}
			} else if err == nil {
				t.Fatal("injected write failure absent")
			}
			want := 4
			if kind == "zero" {
				want = 0
			}
			if n != want || w.prefix != want {
				t.Fatalf("written=%d metadata=%d want=%d", n, w.prefix, want)
			}
			event := <-w.commits
			if event.Err == nil || event.N != want {
				t.Fatal("observer did not normalize failure")
			}
			if _, err := io.WriteString(w, "G1[0001]"); err == nil {
				t.Fatal("failed output accepted more transcript")
			}
			if _, err := file.Seek(0, io.SeekStart); err != nil {
				t.Fatal(err)
			}
			data, err := io.ReadAll(file)
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != body[:want] {
				t.Fatalf("actual prefix=%q", data)
			}
			if w.blocked != 1 {
				t.Fatal("blocked attempt missing")
			}
		})
	}
}

func TestCommitFrameIsNotInsertion(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "output")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	w := &faultOutput{File: file, errors: make(chan error, 1), commits: make(chan commitWrite, 1)}
	if _, err := w.Write([]byte("editor> G1[0000]")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-w.commits:
		t.Fatal("renderer frame falsely confirmed insertion")
	default:
	}
}
func TestCommitRejectUnsafeCleanup(t *testing.T) {
	if cleanupBytes([]byte("\x1b[2J\x1b[<1u\x1b[?2004l")) {
		t.Fatal("clear screen accepted as mode cleanup")
	}
}
