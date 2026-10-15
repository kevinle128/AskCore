package providers

// API is the wire protocol of one model record. One record has one API.
type API string

const (
	APIAnthropicMessages API = "anthropic-messages"
	APIOpenAICompletions API = "openai-completions"
	APIOpenAIResponses   API = "openai-responses"
)

const (
	ProviderTokenPlan   = "alibaba-token-plan"
	ProviderOpenAI      = "openai"
	ProviderAnthropic   = "anthropic"
	ProviderXAI         = "xai"
	ModelClaudeSonnet46 = "claude-sonnet-4-6"
	ModelGrok47         = "grok-4.7"
	ModelDeepSeekFlash  = "deepseek-v4.1-flash"
	ModelGPT55          = "gpt-5.5"
	ModelGPT56Sol       = "gpt-5.6-sol"

	TokenPlanMessagesURL    = "https://token-plan.ap-southeast-1.maas.aliyuncs.com/apps/anthropic"
	TokenPlanCompletionsURL = "https://token-plan.ap-southeast-1.maas.aliyuncs.com/compatible-mode/v1"
	OpenAIURL               = "https://api.openai.com/v1"
	AnthropicURL            = "https://api.anthropic.com"
	XAIURL                  = "https://api.x.ai/v1"

	// LongContextAbove is the OpenAI input-token threshold (input + cache
	// read + cache write) above which the second price tier applies.
	LongContextAbove int64 = 272_000
)

// Ref names one catalog row. API empty is legal only when a single row has
// that provider and model id. Token Plan messages and completions share
// provider and model id, so callers pass API.
type Ref struct {
	Provider string
	ID       string
	API      API
}
