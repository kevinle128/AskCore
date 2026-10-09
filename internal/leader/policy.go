package leader

import "AskCore/pkg/protocol"

type callKind int

const (
	callSession      callKind = iota // forward a call that names a session
	callInitialize                   // answered by the router from the cached link result
	callAuthenticate                 // forward without a session
	callNewSession
	callFollow
	callUnfollow
	callTake
	callAttach
	callDetach
	callListLive
	callUnsupported // answered by the router with the unsupported error
)

// policy says how the router treats one method.
type policy struct {
	kind callKind
	// driverOnly methods change the session. Only its driver may call them.
	driverOnly bool
	// subscribe means a call by a non-member adds the caller as an observer
	// after every check has passed. It never grants driver authority.
	subscribe bool
}

// policies is the only method table. A method that is not here has no authority.
var policies = map[string]policy{
	"initialize":   {kind: callInitialize},
	"authenticate": {kind: callAuthenticate},
	"session/new":  {kind: callNewSession},

	"session/prompt": {kind: callSession, driverOnly: true},
	"session/cancel": {kind: callSession, driverOnly: true},

	protocol.ACPState:       {kind: callSession, subscribe: true},
	protocol.ACPModels:      {kind: callSession, subscribe: true},
	protocol.ACPUsage:       {kind: callSession, subscribe: true},
	protocol.ACPSetModel:    {kind: callSession, driverOnly: true},
	protocol.ACPSetThinking: {kind: callSession, driverOnly: true},
	protocol.ACPContinue:    {kind: callSession, driverOnly: true},
	protocol.ACPReset:       {kind: callSession, driverOnly: true},
	protocol.ACPSteer:       {kind: callSession, driverOnly: true},
	protocol.ACPFollowUp:    {kind: callSession, driverOnly: true},
	protocol.ACPRemove:      {kind: callSession, driverOnly: true},
	protocol.ACPCompact:     {kind: callSession, driverOnly: true},
	protocol.ACPFork:        {kind: callSession, driverOnly: true},
	protocol.ACPTree:        {kind: callSession, driverOnly: true},

	protocol.ACPFollow:   {kind: callFollow, subscribe: true},
	protocol.ACPUnfollow: {kind: callUnfollow},

	protocol.ACPTake:     {kind: callTake},
	protocol.ACPAttach:   {kind: callAttach},
	protocol.ACPDetach:   {kind: callDetach},
	protocol.ACPListLive: {kind: callListLive},

	// Durable session calls have no owner yet.
	"session/load":              {kind: callUnsupported},
	"session/close":             {kind: callUnsupported},
	"session/resume":            {kind: callUnsupported},
	"session/list":              {kind: callUnsupported},
	"session/set_mode":          {kind: callUnsupported},
	"session/set_config_option": {kind: callUnsupported},
	"logout":                    {kind: callUnsupported},
}

// sharedInteractions are reverse requests that every attached client may answer.
// The first valid answer wins. All other reverse requests go to the driver.
var sharedInteractions = map[string]bool{
	"session/request_permission": true,
}
