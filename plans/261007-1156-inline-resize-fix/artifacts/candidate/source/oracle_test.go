package main

import (
	"fmt"
	vt "github.com/charmbracelet/x/vt"
	"reflect"
	"strings"
	"testing"
	"time"
)

type snapshot struct {
	Rows, History []string
	X, Y          int
	Alt           bool
}

func inspect(e *vt.Emulator) snapshot {
	s := snapshot{Alt: e.IsAltScreen()}
	p := e.CursorPosition()
	s.X, s.Y = p.X, p.Y
	for y := 0; y < e.Height(); y++ {
		var b strings.Builder
		for x := 0; x < e.Width(); x++ {
			c := e.CellAt(x, y)
			if c == nil {
				b.WriteByte(' ')
			} else {
				b.WriteString(c.Content)
			}
		}
		s.Rows = append(s.Rows, strings.TrimRight(b.String(), " "))
	}
	for y := 0; y < e.ScrollbackLen(); y++ {
		var b strings.Builder
		for x := 0; x < e.Width(); x++ {
			c := e.ScrollbackCellAt(x, y)
			if c == nil {
				b.WriteByte(' ')
			} else {
				b.WriteString(c.Content)
			}
		}
		s.History = append(s.History, strings.TrimRight(b.String(), " "))
	}
	return s
}
func compare(got, want snapshot) error {
	if !reflect.DeepEqual(got, want) {
		return fmt.Errorf("terminal got %#v want %#v", got, want)
	}
	return nil
}
func TestOracle(t *testing.T) {
	cases := []struct {
		name, input string
		want        snapshot
	}{
		{"history", "one\r\ntwo\r\ntri\r\nfour", snapshot{[]string{"two", "tri", "four"}, []string{"one"}, 4, 2, false}},
		{"wrap", "abcdef", snapshot{[]string{"abcde", "f", ""}, nil, 1, 1, false}},
		{"erase-motion", "abcde\r\x1b[2CX\x1b[K", snapshot{[]string{"abX", "", ""}, nil, 3, 0, false}},
		{"alt", "main\x1b[?1049halt\x1b[?1049l", snapshot{[]string{"main", "", ""}, nil, 4, 0, false}},
		{"active-alt", "main\x1b[?1049hA\r\nB\r\nC\r\nD", snapshot{[]string{"B", "C", "D"}, nil, 1, 2, true}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for _, chunk := range []int{1, 2, 4096} {
				e := vt.NewEmulator(5, 3)
				e.SetScrollbackSize(100)
				for i := 0; i < len(c.input); i += chunk {
					j := min(i+chunk, len(c.input))
					if _, err := e.Write([]byte(c.input[i:j])); err != nil {
						t.Fatal(err)
					}
				}
				got := inspect(e)
				e.Close()
				if err := compare(got, c.want); err != nil {
					t.Fatal(err)
				}
				bad := c.want
				bad.X++
				if compare(got, bad) == nil {
					t.Fatal("wrong cursor accepted")
				}
				bad = c.want
				bad.History = []string{"missing"}
				if compare(got, bad) == nil {
					t.Fatal("wrong history accepted")
				}
			}
		})
	}
}

func TestOracleQuery(t *testing.T) {
	e := vt.NewEmulator(5, 3)
	defer e.Close()
	response := make(chan string, 1)
	go func() {
		b := make([]byte, 64)
		n, err := e.Read(b)
		if err != nil {
			response <- "error: " + err.Error()
			return
		}
		response <- string(b[:n])
	}()
	if _, err := e.Write([]byte("ab\x1b[6n")); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-response:
		if got != "\x1b[1;3R" {
			t.Fatalf("CPR got %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("CPR did not respond")
	}
}
func TestOracleUnicodeByteSplit(t *testing.T) {
	for _, chunk := range []int{1, 2, 99} {
		e := vt.NewEmulator(5, 3)
		data := []byte("é界")
		for i := 0; i < len(data); i += chunk {
			if _, err := e.Write(data[i:min(i+chunk, len(data))]); err != nil {
				t.Fatal(err)
			}
		}
		got := e.CursorPosition()
		if got.X != 3 || got.Y != 0 {
			t.Fatalf("split=%d cursor=%v", chunk, got)
		}
		if !strings.Contains(e.String(), "é界") {
			t.Fatalf("split=%d rows=%q", chunk, e.String())
		}
		e.Close()
	}
}

func TestOracleFinalNegativeControls(t *testing.T) {
	e := vt.NewEmulator(52, 15)
	defer e.Close()
	if _, err := e.Write([]byte("T0 startup\r\n")); err != nil {
		t.Fatal(err)
	}
	want := snapshot{Rows: []string{"T0 startup", "", "", "", "", "", "", "", "", "", "", "", "", "", ""}, X: 0, Y: 1}
	got := inspect(e)
	if err := compare(got, want); err != nil {
		t.Fatal(err)
	}
	bad := want
	bad.X = 1
	if compare(got, bad) == nil {
		t.Fatal("wrong final cursor accepted")
	}
	bad = want
	bad.Rows = append([]string(nil), want.Rows...)
	bad.Rows[0] = ""
	if compare(got, bad) == nil {
		t.Fatal("missing startup accepted")
	}
}
