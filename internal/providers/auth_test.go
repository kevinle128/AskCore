package providers

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestAuthSnapshotRedactionAndBinding(t *testing.T) {
	a := AuthSnapshot{Provider: "openai", Method: "openai-chatgpt", Endpoint: "https://example.test", AccessToken: "secret-access", AccountID: "secret-account"}
	for _, s := range []string{fmt.Sprint(a), fmt.Sprintf("%#v", a)} {
		if strings.Contains(s, "secret-") {
			t.Fatalf("secret in diagnostic: %s", s)
		}
	}
	b, err := json.Marshal(a)
	if err != nil || strings.Contains(string(b), "secret-") {
		t.Fatalf("secret in JSON: %s %v", b, err)
	}
	if !a.ValidFor("openai", "https://example.test") || a.ValidFor("xai", "https://example.test") || a.ValidFor("openai", "https://other.test") {
		t.Fatal("destination binding failed")
	}
	var opts StreamOptions
	opts.APIKey = "legacy"
	if opts.Auth.Method != "" || opts.APIKey != "legacy" {
		t.Fatal("legacy key changed")
	}
}
