package protocol

// Provider setup methods. The leader answers them itself; a client reaches
// them through the router, which routes each answer back by request id.
const (
	ACPProvidersList    = "_ask/providers/list"
	ACPProvidersAPIKey  = "_ask/providers/api_key"
	ACPLoginStart       = "_ask/providers/login/start"
	ACPLoginInput       = "_ask/providers/login/input"
	ACPLoginWait        = "_ask/providers/login/wait"
	ACPLoginCancel      = "_ask/providers/login/cancel"
	ACPProvidersLogout  = "_ask/providers/logout"
	ACPProvidersChanged = "_ask/providers/changed"

	// ACPProvidersReleaseClient is an internal request of the router. It tells
	// the host that a client is gone, so its open sign-ins end. No client sends it.
	ACPProvidersReleaseClient = "_ask/providers/release_client"
)

// ACPLoginState is the credential state of one login option.
type ACPLoginState string

const (
	ACPLoginStateSignedIn ACPLoginState = "signed_in"
	ACPLoginStateAPIKey   ACPLoginState = "api_key"
	ACPLoginStateEnv      ACPLoginState = "env"
	ACPLoginStateNone     ACPLoginState = "none"
)

// ACPLoginKind is the way a sign-in asks the user to proceed.
type ACPLoginKind string

const (
	ACPLoginKindBrowser        ACPLoginKind = "browser"
	ACPLoginKindBrowserAddress ACPLoginKind = "browser_address"
	ACPLoginKindCopyCode       ACPLoginKind = "copy_code"
	ACPLoginKindDevice         ACPLoginKind = "device"
)

// ACPLoginStatus is the end state of a sign-in or of a key save. A sign-in
// ends signed_in and a key save ends saved when the credential is kept.
type ACPLoginStatus string

const (
	ACPLoginStatusSignedIn  ACPLoginStatus = "signed_in"
	ACPLoginStatusSaved     ACPLoginStatus = "saved"
	ACPLoginStatusRefused   ACPLoginStatus = "refused"
	ACPLoginStatusFailed    ACPLoginStatus = "failed"
	ACPLoginStatusExpired   ACPLoginStatus = "expired"
	ACPLoginStatusCancelled ACPLoginStatus = "cancelled"
)

// ACPLoginReason is a fixed token that tells why a sign-in did not end signed
// in. It never holds provider text.
type ACPLoginReason string

const (
	ACPLoginReasonInvalidKey   ACPLoginReason = "invalid_key"
	ACPLoginReasonAccessDenied ACPLoginReason = "access_denied"
	ACPLoginReasonExpired      ACPLoginReason = "expired"
	ACPLoginReasonNetwork      ACPLoginReason = "network"
	ACPLoginReasonCancelled    ACPLoginReason = "cancelled"
	// ACPLoginReasonRemoved means another client removed the provider while
	// this sign-in was open.
	ACPLoginReasonRemoved  ACPLoginReason = "removed"
	ACPLoginReasonBusy     ACPLoginReason = "busy"
	ACPLoginReasonInternal ACPLoginReason = "internal"
)

// Valid reports whether r is one of the fixed reason tokens.
func (r ACPLoginReason) Valid() bool {
	switch r {
	case ACPLoginReasonInvalidKey, ACPLoginReasonAccessDenied, ACPLoginReasonExpired, ACPLoginReasonNetwork,
		ACPLoginReasonCancelled, ACPLoginReasonRemoved, ACPLoginReasonBusy, ACPLoginReasonInternal:
		return true
	}
	return false
}

// ACPProviderChange is what happened to a provider in a changed notification.
type ACPProviderChange string

const (
	ACPProviderChangeSaved   ACPProviderChange = "saved" // a sign-in or a key save kept a credential
	ACPProviderChangeRemoved ACPProviderChange = "removed"
)

// ACPFallbackReason tells why a session did not start on the saved default.
type ACPFallbackReason string

const (
	ACPFallbackNoCredential ACPFallbackReason = "no_credential" // no credential and no environment key
	ACPFallbackNotInCatalog ACPFallbackReason = "not_in_catalog"
)

// Every Generation below is the one counter of the leader instance, as decimal
// text. No field in this file holds a secret except ACPAPIKeyRequest.Key and
// ACPLoginInputRequest.Code, which no log and no reply may repeat.

// ACPLoginOption is one way to sign in to a provider and its current state.
// EnvVar names the environment variable that supplies a key, when one does.
type ACPLoginOption struct {
	ProviderID string        `json:"providerId"`
	MethodID   string        `json:"methodId"`
	Name       string        `json:"name"`
	State      ACPLoginState `json:"state"`
	EnvVar     string        `json:"envVar,omitempty"`
}

// ACPProviderStatus tells whether a provider can serve models now.
type ACPProviderStatus struct {
	ProviderID string `json:"providerId"`
	Usable     bool   `json:"usable"`
	ModelCount int    `json:"modelCount"`
}

// ACPProvidersResult answers the list method.
type ACPProvidersResult struct {
	Generation uint64              `json:"generation,string"`
	Options    []ACPLoginOption    `json:"options"`
	Providers  []ACPProviderStatus `json:"providers"`
}

// ACPAPIKeyRequest saves an API key. LoginID is made by the client and is
// unique for that client; it lets login/cancel stop the key check before the
// key is stored. Key is a secret: it is never logged and never echoed.
type ACPAPIKeyRequest struct {
	LoginID    string `json:"loginId"`
	ProviderID string `json:"providerId"`
	Key        string `json:"key"`
}

// ACPLoginStartRequest opens a browser, address or device sign-in.
type ACPLoginStartRequest struct {
	ProviderID string `json:"providerId"`
	MethodID   string `json:"methodId"`
}

// ACPLoginStartResult tells the client what to show. ExpiresAt is RFC 3339 and
// is set for a device sign-in only.
type ACPLoginStartResult struct {
	LoginID   string       `json:"loginId"`
	Kind      ACPLoginKind `json:"kind"`
	URL       string       `json:"url,omitempty"`
	UserCode  string       `json:"userCode,omitempty"`
	ExpiresAt string       `json:"expiresAt,omitempty"`
}

// ACPLoginInputRequest gives a sign-in the code that the user got, or the
// pasted callback address of a browser_address sign-in. Code is a secret: it
// is never logged and never echoed.
type ACPLoginInputRequest struct {
	LoginID string `json:"loginId"`
	Code    string `json:"code"`
}

// ACPLoginIDRequest names a sign-in for wait and cancel.
type ACPLoginIDRequest struct {
	LoginID string `json:"loginId"`
}

// ACPLoginCancelResult tells whether the leader stopped the attempt before it
// stored anything. It is false when the credential was already saved.
type ACPLoginCancelResult struct {
	Cancelled bool `json:"cancelled"`
}

// ACPLoginOutcome is the end of a sign-in or of a key save. It has no free
// text: Reason is one of the fixed tokens. NotChecked is true when a key was
// saved without a live check; an outcome without it decodes as checked. The
// client reads the model count from the model list that it fetches after the
// outcome.
type ACPLoginOutcome struct {
	LoginID      string         `json:"loginId"`
	Status       ACPLoginStatus `json:"status"`
	Reason       ACPLoginReason `json:"reason,omitempty"`
	HTTPStatus   int            `json:"httpStatus,omitempty"`
	ProviderCode string         `json:"providerCode,omitempty"`
	NotChecked   bool           `json:"notChecked,omitempty"`
	Account      string         `json:"account,omitempty"`
	Generation   uint64         `json:"generation,omitempty,string"`
}

// ACPLogoutRequest removes the stored credential of one login option.
// SessionID is optional: the caller's session, whose model the removal can
// make unusable.
type ACPLogoutRequest struct {
	ProviderID string `json:"providerId"`
	MethodID   string `json:"methodId"`
	SessionID  string `json:"sessionId,omitempty"`
}

// ACPLogoutResult tells what the removal did. EnvVar is set when an
// environment key still applies. ClearedModel is the model id of the caller's
// session when the removal made it unusable; ClearPending is true when that
// session is running and clears at the end of the run.
type ACPLogoutResult struct {
	Removed      bool   `json:"removed"`
	EnvVar       string `json:"envVar,omitempty"`
	ClearedModel string `json:"clearedModel,omitempty"`
	ClearPending bool   `json:"clearPending,omitempty"`
	Generation   uint64 `json:"generation,string"`
}

// ACPProvidersChangedNotification tells every initialized client that the
// credentials of a provider changed.
type ACPProvidersChangedNotification struct {
	Generation uint64            `json:"generation,string"`
	ProviderID string            `json:"providerId"`
	Change     ACPProviderChange `json:"change"`
}

// ACPModelsRequest asks for the model list. SessionID uses the same JSON key
// as ACPSessionRequest, so an old body decodes with UsableOnly false. A leader
// that knows the flag answers with usable rows only and echoes it.
type ACPModelsRequest struct {
	SessionID  string `json:"sessionId"`
	UsableOnly bool   `json:"usableOnly,omitempty"`
}

// ACPModelFallback says that a session started on another model because the
// saved default was not usable. Reason is a fixed token.
type ACPModelFallback struct {
	SavedDefault string            `json:"savedDefault"`
	Reason       ACPFallbackReason `json:"reason"`
}
