// Command spawncaller stands in for a second binary, such as ask-server, that
// reaches the leader through ConnectOrSpawn. It prints the instance id of the
// leader it reached. The E2E tests of cmd/tui compile and run it.
package main

import (
	"context"
	"fmt"
	"os"

	"AskCore/internal/leader"
	"AskCore/pkg/protocol"
)

func main() {
	paths, err := leader.ResolvePaths(os.Getenv("ASK_HOME"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "spawncaller:", err)
		os.Exit(1)
	}
	exe, _ := os.Executable()
	c, err := leader.ConnectOrSpawn(context.Background(), leader.ConnectConfig{
		Paths:     paths,
		Hello:     protocol.LeaderRegister{ClientKind: "daemon", Build: "caller"},
		CallerExe: exe,
		Warn:      func(string) {},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "spawncaller:", err)
		os.Exit(1)
	}
	fmt.Println(c.Info.InstanceID)
	_ = c.Close()
}
