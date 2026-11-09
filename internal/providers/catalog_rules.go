package providers

import (
	"regexp"
	"slices"
)

// offMode tells how a model family turns thinking off.
type offMode int

const (
	// offByWord: the request sends the effort word "none". Off is supported
	// only when "none" is in the allowed words and in the models.dev values.
	offByWord offMode = iota
	// offByWire: the wire format has its own switch for off (Anthropic
	// `thinking: {type: disabled}`). No effort word is sent for off.
	offByWire
	// offRejected: the vendor refuses a request with thinking off. The map has
	// nil for off, so ClampThinkingLevel walks up to the lowest effort.
	offRejected
)

// offWord is the map word for a supported off level. The Responses adapter
// sends it; the Anthropic adapter sends `thinking: disabled` and no word.
const offWord = "none"

// familyRule is the local knowledge that makes a request to one model family
// correct. Every generated row must match a rule: models.dev data never sets
// the API, the base URL or the compat record, and it can only narrow the
// effort words. A rule with a hidden reason drops its rows, because Ask has
// no evidence that its request shape works.
type familyRule struct {
	provider string
	pattern  *regexp.Regexp
	hidden   string

	api     API
	baseURL string
	compat  Compat
	// maxTokens and contextWindow are the request defaults when the row has
	// no value: the smallest documented value in the family.
	maxTokens     int
	contextWindow int
	// words are the allowed effort words. offWord in the list is the off
	// word of an offByWord family.
	words []string
	off   offMode
	// thinkingOffOnly marks a family that accepts only a request with thinking
	// off. Its rows have Reasoning false.
	thinkingOffOnly bool
}

// family compiles an id pattern that must match the whole id.
func family(provider, pattern string) familyRule {
	return familyRule{provider: provider, pattern: regexp.MustCompile(`^(?:` + pattern + `)$`)}
}

func (r familyRule) hide(reason string) familyRule { r.hidden = reason; return r }

func (r familyRule) request(api API, baseURL string, compat Compat, maxTokens, contextWindow int) familyRule {
	r.api, r.baseURL, r.compat, r.maxTokens, r.contextWindow = api, baseURL, compat, maxTokens, contextWindow
	return r
}

func (r familyRule) thinking(off offMode, words ...string) familyRule {
	r.off, r.words = off, words
	return r
}

func (r familyRule) offOnly() familyRule { r.thinkingOffOnly = true; return r }

const (
	datedSuffix         = `(-\d{4}-\d{2}-\d{2})?`
	noXAIEvidence       = "no live result"
	noTokenPlanEvidence = "no live result: the test account has no purchased plan"
)

func anthropicRequest(r familyRule, maxTokens, contextWindow int) familyRule {
	return r.request(APIAnthropicMessages, AnthropicURL, AnthropicCompat{}, maxTokens, contextWindow)
}

// openAIRequest leaves SupportsLongCacheRetention off: no live request proved
// it for these families. The hand-written rows keep it.
func openAIRequest(r familyRule, maxTokens, contextWindow int) familyRule {
	return r.request(APIOpenAIResponses, OpenAIURL, ResponsesCompat{Store: false, SupportsMaxOutputTokens: true}, maxTokens, contextWindow)
}

// familyRules is the rule table. The first rule whose provider and pattern
// match an id decides that id. A shown family has a live request through
// `ask -p` (2026-10-28) for its request shape; the vendor docs give the
// effort words and the default limits. An id that matches no rule is hidden.
var familyRules = []familyRule{
	// Anthropic 4.5 refuses adaptive thinking (HTTP 400); thinking off works.
	// Ask has no budget-token thinking, so these rows only run with thinking off.
	anthropicRequest(family(ProviderAnthropic, `claude-(haiku|sonnet|opus)-4-5(-\d{8})?`), 64_000, 200_000).offOnly(),
	anthropicRequest(family(ProviderAnthropic, `claude-(sonnet|opus)-4-6`), 128_000, 1_000_000).
		thinking(offByWire, "low", "medium", "high", "max"),
	anthropicRequest(family(ProviderAnthropic, `claude-opus-4-[78]|claude-opus-5|claude-sonnet-5|claude-haiku-5-5`), 128_000, 1_000_000).
		thinking(offByWire, "low", "medium", "high", "xhigh", "max"),
	// These models refuse `thinking: {type: disabled}`.
	anthropicRequest(family(ProviderAnthropic, `claude-fable-5(-1)?|claude-opus-5-5|claude-sonnet-5-5`), 128_000, 1_000_000).
		thinking(offRejected, "low", "medium", "high", "xhigh", "max"),

	openAIRequest(family(ProviderOpenAI, `gpt-4\.1(-mini)?`+datedSuffix), 32_768, 1_047_576).offOnly(),
	openAIRequest(family(ProviderOpenAI, `gpt-4o(-mini)?`+datedSuffix), 16_384, 128_000).offOnly(),
	// gpt-5 refuses the word "none" (HTTP 400), so off is not a level.
	openAIRequest(family(ProviderOpenAI, `gpt-5(-mini|-nano)?`+datedSuffix), 128_000, 400_000).
		thinking(offByWord, "minimal", "low", "medium", "high"),
	openAIRequest(family(ProviderOpenAI, `gpt-5-pro`+datedSuffix), 272_000, 400_000).
		thinking(offByWord, "high"),
	openAIRequest(family(ProviderOpenAI, `gpt-5\.1`+datedSuffix), 128_000, 400_000).
		thinking(offByWord, offWord, "low", "medium", "high"),
	openAIRequest(family(ProviderOpenAI, `gpt-5\.[245](-mini|-nano)?`+datedSuffix), 128_000, 400_000).
		thinking(offByWord, offWord, "low", "medium", "high", "xhigh"),
	openAIRequest(family(ProviderOpenAI, `gpt-5\.[245]-pro`+datedSuffix), 128_000, 400_000).
		thinking(offByWord, "medium", "high", "xhigh"),
	openAIRequest(family(ProviderOpenAI, `gpt-5\.3-codex(-spark)?`), 32_000, 128_000).
		thinking(offByWord, offWord, "low", "medium", "high", "xhigh"),
	openAIRequest(family(ProviderOpenAI, `gpt-5\.6(-luna|-sol|-terra)?`), 128_000, 1_050_000).
		thinking(offByWord, offWord, "low", "medium", "high", "xhigh", "max"),
	// gpt-6-astra and gpt-6.1-sol refuse the word "none" (HTTP 400), so off is
	// not a level and the clamp walks up to the lowest effort. The other gpt-6
	// models accept "none".
	openAIRequest(family(ProviderOpenAI, `gpt-6-astra|gpt-6\.1-sol`), 128_000, 1_050_000).
		thinking(offRejected, "low", "medium", "high", "xhigh", "max"),
	openAIRequest(family(ProviderOpenAI, `gpt-6-(luna|sol)|gpt-6\.1-terra`), 128_000, 1_050_000).
		thinking(offByWord, offWord, "low", "medium", "high", "xhigh", "max"),
	openAIRequest(family(ProviderOpenAI, `o3(-pro)?`+datedSuffix), 100_000, 200_000).
		thinking(offByWord, "low", "medium", "high"),
	family(ProviderOpenAI, `gpt-daybreak-[a-z0-9.-]+`).hide("no vendor doc and no live result"),

	// The grok families are hidden for lack of live evidence: no xAI
	// credential was available when the rows were reviewed. The hand-written
	// grok-4.7 row stays.
	// xAI answers HTTP 400 "does not support parameter reasoningEffort" for
	// both grok-4.20 ids (live, 2026-11-09), so these rows never get an
	// effort level, also when a later change shows them.
	family(ProviderXAI, `grok-4\.20-0309-non-reasoning`).hide(noXAIEvidence).offOnly(),
	family(ProviderXAI, `grok-4\.20-0309-reasoning`).hide(noXAIEvidence).offOnly(),
	family(ProviderXAI, `grok-4\.3`).hide(noXAIEvidence),
	family(ProviderXAI, `grok-4\.[5-7]`).hide(noXAIEvidence),
	family(ProviderXAI, `grok-build-0\.1`).hide(noXAIEvidence),

	// Every Token Plan model answered 403 AccessDenied.Unpurchased, so no
	// request shape has a live result. The hand-written deepseek rows stay.
	family(ProviderTokenPlan, `deepseek-v4(\.1)?-(flash|pro)(-\d{4})?`).hide(noTokenPlanEvidence),
	family(ProviderTokenPlan, `qwen3\.[67]-(flash|plus|max)`).hide(noTokenPlanEvidence),
	family(ProviderTokenPlan, `qwen3\.8-(flash|max)`).hide(noTokenPlanEvidence),
	family(ProviderTokenPlan, `glm-5(\.[1-3])?`).hide(noTokenPlanEvidence),
	family(ProviderTokenPlan, `kimi-k2\.[5-7](-code)?`).hide(noTokenPlanEvidence),
	family(ProviderTokenPlan, `MiniMax-M2\.5`).hide("Ask has no request format for its thinking parameter"),
}

// ruleFor returns the first rule whose provider and pattern match id.
func ruleFor(provider, id string) (familyRule, bool) {
	if i := ruleIndex(provider, id); i >= 0 {
		return familyRules[i], true
	}
	return familyRule{}, false
}

// ruleIndex returns the position in familyRules of the rule that decides id,
// or -1 when no rule matches. Hidden rules count, so the position also orders
// rows by family.
func ruleIndex(provider, id string) int {
	return slices.IndexFunc(familyRules, func(r familyRule) bool {
		return r.provider == provider && r.pattern.MatchString(id)
	})
}

// RequestLimits returns the output limit and the context window that a request
// for m uses. A value above 0 on the row wins. Otherwise the default of the
// family rule applies, so a row with unknown limits still sends a useful
// output limit. With no rule, an unknown value stays 0.
func RequestLimits(m Model) (maxTokens, contextWindow int) {
	maxTokens, contextWindow = max(m.MaxTokens, 0), max(m.ContextWindow, 0)
	if maxTokens > 0 && contextWindow > 0 {
		return maxTokens, contextWindow
	}
	r, ok := ruleFor(m.Provider, m.ID)
	if !ok || r.hidden != "" || r.api != m.API {
		return maxTokens, contextWindow
	}
	if maxTokens == 0 {
		maxTokens = r.maxTokens
	}
	if contextWindow == 0 {
		contextWindow = r.contextWindow
	}
	return maxTokens, contextWindow
}

// thinkingOf builds the Reasoning flag and the full thinking map of a row from
// its rule and the models.dev effort values. ok is false when the row cannot
// make a valid request: the family refuses off and no effort word is left.
func (r familyRule) thinkingOf(reasoning bool, values []string) (on bool, levels ThinkingLevelMap, ok bool) {
	var words []string
	if reasoning && !r.thinkingOffOnly {
		for _, w := range r.words {
			if slices.Contains(values, w) {
				words = append(words, w)
			}
		}
	}
	offSupported := r.off == offByWire || (r.off == offByWord && slices.Contains(words, offWord))
	efforts := slices.DeleteFunc(slices.Clone(words), func(w string) bool { return w == offWord })
	if len(efforts) == 0 {
		if r.off == offRejected {
			return false, nil, false
		}
		return false, nil, true
	}
	levels = ThinkingLevelMap{}
	for _, l := range thinkingRank {
		levels[l] = nil
	}
	if offSupported {
		levels[thinkingRank[0]] = stringPointer(offWord)
	}
	for _, l := range thinkingRank[1:] {
		if slices.Contains(efforts, string(l)) {
			levels[l] = stringPointer(string(l))
		}
	}
	return true, levels, true
}
