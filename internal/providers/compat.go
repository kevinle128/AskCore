package providers

// Compat is the quirk record. The set is sealed by unexported isCompat.
// The concrete value must match Model.API. Validate rejects a mismatch.
type Compat interface{ isCompat() }

// AnthropicCompat is the messages-wire quirk record. Zero is valid.
type AnthropicCompat struct{}

func (AnthropicCompat) isCompat() {}

// CompletionsCompat is the openai-completions quirk record.
type CompletionsCompat struct {
	ThinkingFormat          string
	SupportsDeveloperRole   bool
	SupportsStore           bool
	SupportsReasoningEffort bool
	SupportsStreamOptions   bool
	// SupportsFinishReason, when true, makes a stream that ends without
	// finish_reason settle as ErrStreamIncomplete. The Token Plan row sets
	// this true so a zero value cannot silently skip the check.
	SupportsFinishReason                        bool
	MaxTokensField                              string
	RequiresReasoningContentOnAssistantMessages bool
	RequiresThinkingAsText                      bool
	SessionAffinityFormat                       string
}

func (CompletionsCompat) isCompat() {}

// ResponsesCompat is the openai-responses quirk record.
// Store is data. The H4 OpenAI row sets it false.
type ResponsesCompat struct {
	Store                      bool
	SessionAffinityFormat      string
	SupportsLongCacheRetention bool
	SupportsMaxOutputTokens    bool
	SupportsStrictMode         bool
}

func (ResponsesCompat) isCompat() {}
