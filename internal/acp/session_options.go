package acp

import (
	"encoding/json"
	"regexp"

	"AskCore/pkg/protocol"
)

var toolNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// InvalidParams returns an error that the adapter sends as invalid_params with
// detail in the message. detail MUST be fixed text or text that the caller
// checked, never raw client input. A Factory uses it for a failure that the
// client caused.
func InvalidParams(detail string) error {
	return newKindError(protocol.ACPErrInvalidParams, detail)
}

// flagsFromMeta reads the per-session flags of a session/new call. No key gives
// the zero value. Unknown fields are ignored. A value of the wrong type, text
// over ACPSessionPromptMaxBytes, a tool name that is not a plain name, a model
// option over its bounds, or a thinking option that is not a level is an
// invalid_params error with a fixed text. The model patterns are not resolved
// here: the Factory resolves them against the model list.
func flagsFromMeta(meta map[string]any) (protocol.ACPSessionOptions, error) {
	raw, ok := meta[protocol.ACPSessionOptionsMetaKey]
	if !ok {
		return protocol.ACPSessionOptions{}, nil
	}
	const bad = "the session options are malformed"
	if _, isObject := raw.(map[string]any); !isObject {
		return protocol.ACPSessionOptions{}, InvalidParams(bad)
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return protocol.ACPSessionOptions{}, InvalidParams(bad)
	}
	var opts protocol.ACPSessionOptions
	if err := json.Unmarshal(data, &opts); err != nil {
		return protocol.ACPSessionOptions{}, InvalidParams(bad)
	}
	if opts.PromptBytes() > protocol.ACPSessionPromptMaxBytes {
		return protocol.ACPSessionOptions{}, InvalidParams("the session prompt text is too large")
	}
	for _, name := range opts.Tools {
		if !toolNamePattern.MatchString(name) {
			return protocol.ACPSessionOptions{}, InvalidParams("a tool name is not valid")
		}
	}
	if len(opts.Model) > protocol.ACPSessionModelMaxBytes {
		return protocol.ACPSessionOptions{}, InvalidParams("the model option is too long")
	}
	if len(opts.Models) > protocol.ACPSessionModelsMax {
		return protocol.ACPSessionOptions{}, InvalidParams("the models option has too many entries")
	}
	for _, entry := range opts.Models {
		if len(entry) > protocol.ACPSessionModelMaxBytes {
			return protocol.ACPSessionOptions{}, InvalidParams("a models entry is too long")
		}
	}
	if opts.Thinking != "" && !validThinking(opts.Thinking) {
		return protocol.ACPSessionOptions{}, InvalidParams("the thinking option is not a thinking level")
	}
	return opts, nil
}
