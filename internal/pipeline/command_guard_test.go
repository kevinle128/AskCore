package pipeline_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/pipeline"
	"AskCore/pkg/protocol"
)

func guardRegistry() *pipeline.Registry {
	r := pipeline.NewRegistry()
	r.OnBeforeTool(pipeline.CommandGuard)
	return r
}

func toolCall(t *testing.T, name string, args map[string]any) pipeline.ToolCallInfo {
	t.Helper()
	raw, err := json.Marshal(args)
	require.NoError(t, err)
	return pipeline.ToolCallInfo{Call: protocol.ToolCall{ID: "c1", Name: name, Arguments: raw}, Args: raw}
}

func TestCommandGuardBlocksCatastrophicCommands(t *testing.T) {
	cases := []struct{ name, command string }{
		{"rm root", "rm -rf /"},
		{"rm root glob", "rm -rf /*"},
		{"rm home tilde", "rm -fr ~"},
		{"rm home slash", "rm -r ~/"},
		{"rm home variable", "rm -rf $HOME"},
		{"rm root with sudo and long flags", "sudo rm --recursive --force --no-preserve-root /"},
		{"rm root after a chain", "cd /tmp && rm -rf /"},
		{"rm root in a shell string", `bash -c "rm -rf /"`},
		{"command default path flag", "command -p rm -rf /"},
		{"sudo noninteractive flag", "sudo -n rm -rf /"},
		{"sudo user option", "sudo -u root rm -rf /"},
		{"nice adjustment option", "nice -n 5 rm -rf /"},
		{"env unset option", "env -u LANG rm -rf /"},
		{"doas noninteractive flag", "doas -n rm -rf /"},
		{"mkfs", "mkfs.ext4 /dev/sda1"},
		{"mkfs bare", "mkfs /dev/sda"},
		{"dd to a disk", "dd if=/dev/zero of=/dev/sda bs=1M"},
		{"redirect to a disk", "echo x > /dev/nvme0n1"},
		{"fork bomb", ":(){ :|:& };:"},
		{"fork bomb in a shell string", `bash -c ':(){ :|:& };:'`},
		{"fork bomb in a substitution", `printf '%s\n' "$(:(){ :|:& };:)"`},
		{"fork bomb in a backtick substitution", "echo \"`:(){ :|:& };:`\""},
		{"chmod root", "chmod -R 777 /"},
		{"chown root", "chown -R nobody /"},
	}
	r := guardRegistry()
	for _, tc := range cases {
		for _, tool := range []string{"bash", "monitor"} {
			t.Run(tc.name+"/"+tool, func(t *testing.T) {
				res, err := r.BeforeTool(context.Background(), toolCall(t, tool, map[string]any{"command": tc.command}))
				require.NoError(t, err)
				require.NotNil(t, res, "the command is blocked")
				assert.True(t, res.Block)
				assert.Contains(t, res.Reason, "blocked by command-guard: ")
				assert.False(t, res.Terminate, "the run continues after a block")
			})
		}
	}
}

func TestCommandGuardAllowsOrdinaryCommands(t *testing.T) {
	commands := []string{
		"rm -rf ./build-out",
		"rm -rf /tmp/build-out",
		"rm -rf ~/project/build",
		"rm -r --  -weird",
		"rm file.txt",
		"dd if=a of=b",
		"dd if=/dev/zero of=./disk.img bs=1M count=1",
		"echo mkfs",
		`printf '%s\n' ':(){ :|:& };:'`,
		`printf '%s\n' ":(){ :|:& };:"`,
		"echo rm -rf /",
		"grep -r mkfs docs",
		"echo hi > /dev/null",
		"ls -l /dev/sda",
		"chmod -R 755 ./dist",
		"chown -R me ./dist",
		"go test ./...",
		"",
	}
	r := guardRegistry()
	for _, c := range commands {
		t.Run(c, func(t *testing.T) {
			res, err := r.BeforeTool(context.Background(), toolCall(t, "bash", map[string]any{"command": c}))
			require.NoError(t, err)
			assert.Nil(t, res, "the call is allowed")
		})
	}
}

func TestCommandGuardIgnoresOtherTools(t *testing.T) {
	r := guardRegistry()
	res, err := r.BeforeTool(context.Background(), toolCall(t, "write", map[string]any{
		"path": "notes.sh", "content": "rm -rf /\nmkfs /dev/sda\n",
	}))
	require.NoError(t, err)
	assert.Nil(t, res)
}

func TestCommandGuardPassesOnMalformedArguments(t *testing.T) {
	r := guardRegistry()
	in := pipeline.ToolCallInfo{Call: protocol.ToolCall{Name: "bash"}, Args: json.RawMessage(`{"command": 5}`)}
	res, err := r.BeforeTool(context.Background(), in)
	require.NoError(t, err)
	assert.Nil(t, res)
}
