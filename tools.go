//go:build tools

// Tool dependencies tracked for `go generate` - never built into the binary.
package main

import (
	_ "github.com/golangci/golangci-lint/v2/cmd/golangci-lint"
	_ "github.com/bufbuild/buf/cmd/buf"
)
