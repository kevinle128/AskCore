package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"AskCore/internal/auth"
)

func runAuth(ctx context.Context, argv []string, stdin io.Reader, stdout, stderr io.Writer, service *auth.Service) int {
	o, err := parseAuthArgs(argv)
	if err != nil {
		report(stderr, "Error:", err)
		return 1
	}
	if o.help {
		if _, err := io.WriteString(stdout, authUsage); err != nil {
			return 1
		}
		return 0
	}
	if service == nil {
		report(stderr, "Error: auth is unavailable")
		return 1
	}
	methods := []string{}
	for _, m := range service.Methods {
		if m.Provider == o.provider && !slices.Contains(methods, m.ID) {
			methods = append(methods, m.ID)
		}
	}
	if len(methods) == 0 {
		report(stderr, "Error: unsupported auth provider")
		return 1
	}
	if o.action == "logout" {
		err = service.Logout(ctx, o.provider, o.method)
	} else {
		if o.method == "" {
			if !isTerminal(stdin) {
				report(stderr, "Error: noninteractive login requires --method")
				return 1
			}
			report(stderr, "Choose a method:", strings.Join(methods, ", "))
			o.method, err = readPrivateLine(ctx, stdin)
			if err != nil {
				report(stderr, "Error: cannot read method selection")
				return 1
			}
			o.method = strings.TrimSpace(o.method)
		}
		if !slices.Contains(methods, o.method) {
			report(stderr, "Error: unsupported auth method")
			return 1
		}
		hostID := ""
		if o.method == "openai-chatgpt" {
			hostID, err = service.Store.HostID(ctx)
			if err != nil {
				report(stderr, "Error:", err)
				return 1
			}
			o.interaction = "browser"
		}
		request := auth.LoginRequest{Interaction: o.interaction, NewAccount: o.newAccount, HostID: hostID,
			Input: func(ctx context.Context) (string, error) { return readPrivateLine(ctx, stdin) },
			Notify: func(ctx context.Context, n auth.LoginNotice) error {
				if err := ctx.Err(); err != nil {
					return err
				}
				for _, line := range []string{n.Message, n.URL, n.UserCode} {
					if line != "" {
						if _, err := fmt.Fprintln(stderr, line); err != nil {
							return err
						}
					}
				}
				return nil
			},
		}
		if o.method == "api-key" {
			report(stderr, "Enter the API key on private stdin:")
		}
		err = service.Login(ctx, o.provider, o.method, request)
	}
	if err != nil {
		if errors.Is(err, errPrivateInterrupt) {
			return 130
		}
		report(stderr, "Error:", err)
		return 1
	}
	if o.action == "logout" {
		report(stderr, "Local credential removed. Environment keys can still be used.")
	} else {
		report(stderr, "Credential saved.")
	}
	return 0
}
