package main

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestAuthCommandParsing(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		fail bool
	}{
		{"key", []string{"login", "--provider", "anthropic", "--method", "api-key"}, false},
		{"local logout", []string{"logout", "--provider", "openai"}, false},
		{"secret argument", []string{"login", "--provider", "openai", "--api-key", "private-value"}, true},
		{"extra prompt", []string{"logout", "--provider", "openai", "hello"}, true},
		{"missing provider", []string{"login", "--method", "api-key"}, true},
		{"wrong replacement", []string{"login", "--provider", "anthropic", "--method", "anthropic-oauth", "--new-account"}, true},
		{"logout interaction", []string{"logout", "--provider", "anthropic", "--interaction", "browser"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseAuthArgs(tc.args)
			if (err != nil) != tc.fail {
				t.Fatalf("error=%v, want failure=%v", err, tc.fail)
			}
		})
	}
}

func TestPrivateInputBounds(t *testing.T) {
	for _, size := range []int{16 * 1024, 16*1024 + 1} {
		v, err := readPrivateLine(context.Background(), strings.NewReader(strings.Repeat("a", size)+"\n"))
		if size <= 16*1024 && (err != nil || len(v) != size) {
			t.Fatalf("valid line rejected: %v", err)
		}
		if size > 16*1024 && err == nil {
			t.Fatal("oversize accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := readPrivateLine(ctx, strings.NewReader("secret")); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}

func TestPrivateInputBoundsDiscardedBytes(t *testing.T) {
	if _, err := readPrivateLine(context.Background(), strings.NewReader(strings.Repeat("\r", authInputLimit+1)+"secret\n")); err == nil {
		t.Fatal("discarded bytes bypassed input limit")
	}
}
