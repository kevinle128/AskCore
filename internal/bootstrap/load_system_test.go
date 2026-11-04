package bootstrap

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadPromptFiles(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		user       map[string]string
		project    map[string]string
		noProject  bool
		replace    string
		appendFlag []string
		want       PromptFiles
		wantFrom   string // path relative to the named root: "user:", "project:" or the flag
	}{
		{name: "nothing", want: PromptFiles{}},
		{name: "user only", user: map[string]string{"SYSTEM.md": "U\n"}, want: PromptFiles{Custom: "U"}, wantFrom: "user:SYSTEM.md"},
		{
			name:     "project beats user",
			user:     map[string]string{"SYSTEM.md": "U"},
			project:  map[string]string{"SYSTEM.md": "P"},
			want:     PromptFiles{Custom: "P"},
			wantFrom: "project:SYSTEM.md",
		},
		{
			name:      "untrusted project is not read",
			user:      map[string]string{"SYSTEM.md": "U", "APPEND_SYSTEM.md": "UA"},
			project:   map[string]string{"SYSTEM.md": "P", "APPEND_SYSTEM.md": "PA"},
			noProject: true,
			want:      PromptFiles{Custom: "U", Append: []string{"UA"}},
			wantFrom:  "user:SYSTEM.md",
		},
		{
			name:     "blank project file falls through",
			user:     map[string]string{"SYSTEM.md": "U"},
			project:  map[string]string{"SYSTEM.md": " \n\t"},
			want:     PromptFiles{Custom: "U"},
			wantFrom: "user:SYSTEM.md",
		},
		{name: "blank file only", project: map[string]string{"SYSTEM.md": "\n"}, want: PromptFiles{}},
		{
			name:     "flag beats files",
			user:     map[string]string{"SYSTEM.md": "U"},
			project:  map[string]string{"SYSTEM.md": "P"},
			replace:  "F",
			want:     PromptFiles{Custom: "F"},
			wantFrom: FlagSource,
		},
		{
			name:     "blank flag is absent",
			project:  map[string]string{"SYSTEM.md": "P"},
			replace:  "  ",
			want:     PromptFiles{Custom: "P"},
			wantFrom: "project:SYSTEM.md",
		},
		{
			name:       "one append file then flags",
			user:       map[string]string{"APPEND_SYSTEM.md": "UA"},
			project:    map[string]string{"APPEND_SYSTEM.md": "PA"},
			appendFlag: []string{"f1", " ", "f2"},
			want:       PromptFiles{Append: []string{"PA", "f1", "f2"}},
		},
		{
			name:       "append file in user when project is blank",
			user:       map[string]string{"APPEND_SYSTEM.md": "UA"},
			project:    map[string]string{"APPEND_SYSTEM.md": ""},
			appendFlag: []string{"f"},
			want:       PromptFiles{Append: []string{"UA", "f"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			base := t.TempDir()
			userDir, projectDir := filepath.Join(base, "user"), filepath.Join(base, "project")
			for name, text := range tt.user {
				put(t, filepath.Join(userDir, name), text)
			}
			for name, text := range tt.project {
				put(t, filepath.Join(projectDir, name), text)
			}
			in := PromptInput{UserDir: userDir, ProjectDir: projectDir, Replace: tt.replace, Append: tt.appendFlag}
			if tt.noProject {
				in.ProjectDir = ""
			}

			got, err := LoadPromptFiles(in)
			require.NoError(t, err)

			wantFrom := tt.wantFrom
			switch {
			case strings.HasPrefix(wantFrom, "user:"):
				wantFrom = filepath.Join(userDir, strings.TrimPrefix(wantFrom, "user:"))
			case strings.HasPrefix(wantFrom, "project:"):
				wantFrom = filepath.Join(projectDir, strings.TrimPrefix(wantFrom, "project:"))
			}
			tt.want.CustomFrom = wantFrom
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestLoadPromptFiles_ExistingBadFileIsAnError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		setup func(t *testing.T, dir string)
		skip  bool
	}{
		{
			name: "unreadable",
			setup: func(t *testing.T, dir string) {
				path := filepath.Join(dir, "SYSTEM.md")
				put(t, path, "text")
				require.NoError(t, os.Chmod(path, 0))
			},
			skip: os.Geteuid() == 0,
		},
		{
			name: "over the size limit",
			setup: func(t *testing.T, dir string) {
				put(t, filepath.Join(dir, "SYSTEM.md"), strings.Repeat("x", MaxFileBytes+1))
			},
		},
		{
			name: "a folder",
			setup: func(t *testing.T, dir string) {
				require.NoError(t, os.MkdirAll(filepath.Join(dir, "SYSTEM.md"), 0o755))
			},
		},
		{
			name: "append file unreadable",
			setup: func(t *testing.T, dir string) {
				path := filepath.Join(dir, "APPEND_SYSTEM.md")
				put(t, path, "text")
				require.NoError(t, os.Chmod(path, 0))
			},
			skip: os.Geteuid() == 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if tt.skip {
				t.Skip("permission bits do not apply to root")
			}
			dir := t.TempDir()
			tt.setup(t, dir)

			_, err := LoadPromptFiles(PromptInput{ProjectDir: dir, UserDir: t.TempDir(), Replace: ""})

			require.Error(t, err)
			assert.Contains(t, err.Error(), dir)
		})
	}
}

func TestLoadPromptFiles_MaxSizeFileLoads(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	put(t, filepath.Join(dir, "SYSTEM.md"), strings.Repeat("x", MaxFileBytes))

	got, err := LoadPromptFiles(PromptInput{UserDir: dir})

	require.NoError(t, err)
	assert.Len(t, got.Custom, MaxFileBytes)
}
