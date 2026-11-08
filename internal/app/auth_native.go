package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"sync"

	"AskCore/internal/auth"
	"AskCore/internal/providers"
	"AskCore/internal/sandbox"
	"AskCore/internal/settings"
)

// NewNativeAuth composes local credentials and native provider protocols.
// HTTP boundaries remain separate from inference and request capture.
func NewNativeAuth(home string, env func(string) (string, bool), options auth.NativeOptions) (*auth.Service, error) {
	store, err := settings.NewAuthStore(home)
	if err != nil {
		return nil, err
	}
	methods := auth.NativeMethods(options)
	for _, row := range []struct {
		provider string
		api      providers.API
		endpoint string
	}{
		{providers.ProviderTokenPlan, providers.APIAnthropicMessages, providers.TokenPlanMessagesURL},
		{providers.ProviderTokenPlan, providers.APIOpenAICompletions, providers.TokenPlanCompletionsURL},
		{"anthropic", providers.APIAnthropicMessages, "https://api.anthropic.com"},
		{providers.ProviderOpenAI, providers.APIOpenAIResponses, providers.OpenAIURL},
		{"xai", providers.APIOpenAIResponses, "https://api.x.ai/v1"},
	} {
		// NativeKeyModelList is nil for Token Plan: its list is the plan
		// catalog, not the models that the key can use.
		methods = append(methods, auth.Method{Provider: row.provider, ID: "api-key", API: row.api, Profile: "api-key", Endpoint: row.endpoint, ListModels: auth.NativeKeyModelList(options, row.provider)})
	}
	return &auth.Service{Store: store, Methods: methods, Env: env, Now: options.Now, RunCommand: runValueCommand}, nil
}

// valueOutputLimit bounds the stdout of a value command. A key or a header
// value needs a few KiB; without a bound a wrong command could fill the memory
// of the leader that all sessions share.
const valueOutputLimit = 64 << 10

// errValueOutputTooLarge has fixed text, so no command output reaches an error.
var errValueOutputTooLarge = errors.New("output too large")

// runValueCommand runs a "!command" value of the user models.json in the shell,
// in the home folder of the user, with the whole process environment: a secret
// command often needs a credential variable that the tool environment drops.
// It returns stdout only. The context comes from the command cache of auth,
// which bounds the run. Stdout past valueOutputLimit stops the command.
func runValueCommand(ctx context.Context, command string) (string, error) {
	dir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var mu sync.Mutex
	var stdout bytes.Buffer
	tooLarge := false
	_, res, err := sandbox.Run(ctx, sandbox.Spec{
		Argv: sandbox.ShellArgv(command),
		Dir:  dir,
		Env:  os.Environ(),
		Observe: func(stream sandbox.Stream, chunk []byte) {
			if stream != sandbox.StreamStdout {
				return
			}
			mu.Lock()
			defer mu.Unlock()
			if tooLarge {
				return
			}
			if stdout.Len()+len(chunk) > valueOutputLimit {
				tooLarge = true
				stdout.Reset()
				cancel()
				return
			}
			stdout.Write(chunk)
		},
	})
	mu.Lock()
	defer mu.Unlock()
	if tooLarge {
		return "", errValueOutputTooLarge
	}
	if err != nil {
		return "", err
	}
	switch {
	case res.Err != nil:
		return "", res.Err
	case res.ExitCode == nil:
		return "", errors.New("the command was stopped by a signal")
	case *res.ExitCode != 0:
		return "", &auth.ExitError{Code: *res.ExitCode}
	}
	return stdout.String(), nil
}
