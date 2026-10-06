package providers

import (
	"encoding/json"
	"fmt"
)

// AuthSnapshot is the request-local inference credential and destination binding.
// Its fields contain no persisted refresh material.
type AuthSnapshot struct {
	Provider    string
	Method      string
	Profile     string
	Source      string
	AccountID   string
	Generation  uint64
	Endpoint    string
	BillingHint string
	AccessToken string
}

// String never prints bearer material or account identity.
func (a AuthSnapshot) String() string {
	return fmt.Sprintf("AuthSnapshot{provider:%q method:%q profile:%q source:%q generation:%d endpoint:%q billing:%q access:[redacted]}", a.Provider, a.Method, a.Profile, a.Source, a.Generation, a.Endpoint, a.BillingHint)
}

// GoString protects formatted diagnostic output.
func (a AuthSnapshot) GoString() string { return a.String() }

// MarshalJSON omits access material and account identity from diagnostics.
func (a AuthSnapshot) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Provider, Method, Profile, Source, Endpoint, BillingHint string
		Generation                                               uint64
	}{a.Provider, a.Method, a.Profile, a.Source, a.Endpoint, a.BillingHint, a.Generation})
}

// ValidFor checks the destination and method binding before use.
func (a AuthSnapshot) ValidFor(provider, endpoint string) bool {
	return a.Provider == provider && a.Endpoint == endpoint && a.Method != "" && a.AccessToken != ""
}
