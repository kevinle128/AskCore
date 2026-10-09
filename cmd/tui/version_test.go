package main

import (
	"bytes"
	"encoding/json"
	"testing"

	"AskCore/pkg/protocol"

	"github.com/stretchr/testify/require"
)

func TestVersionJSON(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"version", "--json"}, bytes.NewReader(nil), &out, &errOut)
	require.Zero(t, code, errOut.String())
	var info protocol.VersionInfo
	require.NoError(t, json.Unmarshal(out.Bytes(), &info))
	require.Equal(t, "ask", info.Name)
	require.Equal(t, protocol.LeaderProtocolVersion, info.LeaderProtocol)
	require.Equal(t, 1, info.ACPVersion)
	require.NotEmpty(t, info.Build)
	require.Equal(t, buildIdentity(), info.Build)
	require.Equal(t, 1, bytes.Count(out.Bytes(), []byte("\n")), "one JSON object on one line")
}

func TestVersionText(t *testing.T) {
	var out, errOut bytes.Buffer
	require.Zero(t, run([]string{"version"}, bytes.NewReader(nil), &out, &errOut))
	require.Equal(t, "ask "+buildIdentity()+"\n", out.String())
}

func TestVersionRejectsUnknownArgument(t *testing.T) {
	var out, errOut bytes.Buffer
	require.Equal(t, 1, run([]string{"version", "--bogus"}, bytes.NewReader(nil), &out, &errOut))
	require.Contains(t, errOut.String(), "ask version --help")
	require.Empty(t, out.String())
}

func TestVersionHelp(t *testing.T) {
	var out, errOut bytes.Buffer
	require.Zero(t, run([]string{"version", "--help"}, bytes.NewReader(nil), &out, &errOut))
	require.Contains(t, out.String(), "ask version")
}
