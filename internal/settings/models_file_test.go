package settings

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

var testBuiltIn = map[string]bool{"anthropic": true, "openai": true, "xai": true}

func parseModels(t *testing.T, body string) (ModelsFile, []Diagnostic) {
	t.Helper()
	file, problems, err := parseModelsFile([]byte(body), "/home/models.json", testBuiltIn)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return file, problems
}

func problemKeys(problems []Diagnostic) []string {
	keys := make([]string, 0, len(problems))
	for _, p := range problems {
		keys = append(keys, p.Key+": "+p.Text)
	}
	return keys
}

func TestModelsFileParsesPiExample(t *testing.T) {
	file, problems := parseModels(t, `{
  // A local Ollama server.
  "providers": {
    "ollama": {
      "baseUrl": "http://localhost:11434/v1",
      "api": "openai-completions",
      "apiKey": "ollama", /* any literal works for a local server */
      "compat": { "supportsDeveloperRole": false, "supportsReasoningEffort": false },
      "models": [
        { "id": "llama3.1:8b" },
        {
          "id": "qwen2.5-coder:7b",
          "name": "Qwen 2.5 Coder",
          "reasoning": true,
          "thinkingLevelMap": { "off": null, "high": "high", },
          "input": ["text", "image"],
          "cost": { "input": 0, "output": 0.5 },
          "contextWindow": 128000,
          "maxTokens": 32000,
          "headers": { "X-Trace": "$TRACE_ID" },
        },
      ],
    },
    "anthropic": {
      "modelOverrides": { "claude-sonnet-4-5": { "contextWindow": 200000, "api": "anthropic-messages" } },
    },
  },
}`)
	if len(problems) != 0 {
		t.Fatalf("problems: %v", problemKeys(problems))
	}
	ollama, ok := file.Providers["ollama"]
	if !ok {
		t.Fatal("the ollama provider is missing")
	}
	if ollama.BaseURL != "http://localhost:11434/v1" || ollama.API != "openai-completions" || ollama.APIKey != "ollama" {
		t.Fatalf("provider fields: %+v", ollama)
	}
	if string(ollama.Compat["supportsDeveloperRole"]) != "false" || len(ollama.Compat) != 2 {
		t.Fatalf("compat: %v", ollama.Compat)
	}
	if len(ollama.Models) != 2 || ollama.Models[0].ID != "llama3.1:8b" || ollama.Models[0].API != "" {
		t.Fatalf("models: %+v", ollama.Models)
	}
	m := ollama.Models[1]
	high, half, zero := "high", 0.5, 0.0
	want := ModelEntry{Position: 1, ID: "qwen2.5-coder:7b", ModelOverride: ModelOverride{
		Name: "Qwen 2.5 Coder", Reasoning: ptrBool(true),
		ThinkingLevelMap: map[string]*string{"off": nil, "high": &high},
		Input:            []string{"text", "image"},
		Cost:             &CostEntry{Input: &zero, Output: &half},
		ContextWindow:    128000, MaxTokens: 32000,
		Headers: map[string]string{"X-Trace": "$TRACE_ID"},
	}}
	if !reflect.DeepEqual(m, want) {
		t.Fatalf("model\n got %+v\nwant %+v", m, want)
	}
	o := file.Providers["anthropic"].ModelOverrides["claude-sonnet-4-5"]
	if o.ContextWindow != 200000 || o.API != "anthropic-messages" {
		t.Fatalf("override: %+v", o)
	}
}

func ptrBool(b bool) *bool { return &b }

func TestModelsFileUnknownKeyIsProblemAndLoads(t *testing.T) {
	file, problems := parseModels(t, `{"providers": {"local": {
		"baseUrl": "http://h/v1", "api": "openai-completions", "apiKey": "k",
		"models": [{"id": "m", "contxtWindow": 1000, "zzzzzzzzzz": 1}]}}}`)
	if len(file.Providers["local"].Models) != 1 {
		t.Fatalf("the row must load: %+v", file)
	}
	want := []string{
		"providers[0].models[0]: unknown key; it is ignored; did you mean contextWindow?",
		"providers[0].models[0]: unknown key; it is ignored",
	}
	if got := problemKeys(problems); !reflect.DeepEqual(got, want) {
		t.Fatalf("problems %q", got)
	}
}

func TestModelsFileBadRowDropsOnlyThatRow(t *testing.T) {
	file, problems := parseModels(t, `{"providers": {"local": {
		"baseUrl": "http://h/v1", "api": "openai-completions", "apiKey": "k",
		"models": [
			{"id": "a"},
			{"id": "b", "contextWindow": 128000.5},
			{"id": "c", "maxTokens": 0},
			{"id": "d", "input": ["text", "audio"]},
			{"id": "e", "thinkingLevelMap": {"huge": "x"}},
			{"id": "f", "thinkingLevelMap": {"high": 3}},
			{"id": "g", "cost": {"input": -1}},
			{"name": "no id"},
			"not an object",
			{"id": "h"}
		]}}}`)
	var ids []string
	for _, m := range file.Providers["local"].Models {
		ids = append(ids, m.ID)
	}
	if !reflect.DeepEqual(ids, []string{"a", "h"}) {
		t.Fatalf("rows %v", ids)
	}
	want := []string{
		"providers[0].models[1].contextWindow: must be a positive integer; the model is ignored",
		"providers[0].models[2].maxTokens: must be a positive integer; the model is ignored",
		"providers[0].models[3].input[1]: must be text or image; the model is ignored",
		"providers[0].models[4].thinkingLevelMap[0]: key must be a thinking level (off, minimal, low, medium, high, xhigh, max); the model is ignored",
		"providers[0].models[5].thinkingLevelMap[0]: value must be a string or null; the model is ignored",
		"providers[0].models[6].cost.input: must be a number that is not negative; the model is ignored",
		"providers[0].models[7].id: id is required; the model is ignored",
		"providers[0].models[8]: must be an object; the model is ignored",
	}
	if got := problemKeys(problems); !reflect.DeepEqual(got, want) {
		t.Fatalf("problems\n%s", strings.Join(got, "\n"))
	}
}

func TestModelsFileProviderRules(t *testing.T) {
	file, problems := parseModels(t, `{"providers": {
		"nobase": {"api": "openai-completions", "apiKey": "k", "models": [{"id": "m"}]},
		"nokey": {"baseUrl": "http://h", "api": "openai-completions", "models": [{"id": "m"}]},
		"emptykey": {"baseUrl": "http://h", "apiKey": "", "api": "openai-completions", "models": [{"id": "m"}]},
		"empty": {},
		"onlymodels": {"models": []},
		"noapi": {"baseUrl": "http://h", "apiKey": "k", "models": [{"id": "m"}, {"id": "n", "api": "openai-responses"}]},
		"openai": {"models": [{"id": "gpt-x", "api": "openai-responses"}]},
		"xai": {"baseUrl": "http://proxy/v1"},
		"badheader": {"baseUrl": "http://h", "headers": {"X-A": 1}}
	}}`)
	var got []string
	for id := range file.Providers {
		got = append(got, id)
	}
	if len(got) != 3 || file.Providers["openai"].Models == nil || file.Providers["xai"].BaseURL == "" {
		t.Fatalf("providers %v", got)
	}
	if rows := file.Providers["noapi"].Models; len(rows) != 1 || rows[0].ID != "n" {
		t.Fatalf("noapi rows: %+v", rows)
	}
	want := []string{
		"providers[0].baseUrl: baseUrl is required for the models of a provider that is not built in; the provider is ignored",
		"providers[1].apiKey: apiKey is required for the models of a provider that is not built in; the provider is ignored",
		"providers[2].apiKey: must not be empty; the provider is ignored",
		"providers[3]: a provider without models needs one more field; the provider is ignored",
		"providers[4]: a provider without models needs one more field; the provider is ignored",
		"providers[5].models[0].api: api is required on the model or on the provider; the model is ignored",
		"providers[8].headers[0]: header value must be a string; the provider is ignored",
	}
	if got := problemKeys(problems); !reflect.DeepEqual(got, want) {
		t.Fatalf("problems\n%s", strings.Join(got, "\n"))
	}
}

func TestModelsFileAllRowsDroppedKeepsProvider(t *testing.T) {
	file, problems := parseModels(t, `{"providers": {"local": {
		"baseUrl": "http://h/v1", "api": "openai-completions", "apiKey": "k",
		"models": [{"id": "a", "maxTokens": -1}]}}}`)
	if _, ok := file.Providers["local"]; !ok || len(problems) != 1 {
		t.Fatalf("file %+v problems %v", file, problemKeys(problems))
	}
}

func TestModelsFileProviderIDMustMatchStoreRule(t *testing.T) {
	file, problems := parseModels(t, `{"providers": {
		"Local": {"baseUrl": "http://h"},
		"my_local": {"baseUrl": "http://h"},
		"a/b": {"baseUrl": "http://h"},
		"ok-1": {"baseUrl": "http://h"}
	}}`)
	if len(file.Providers) != 1 || file.Providers["ok-1"].BaseURL != "http://h" {
		t.Fatalf("providers %+v", file.Providers)
	}
	if len(problems) != 3 {
		t.Fatalf("problems %v", problemKeys(problems))
	}
	for i, p := range problems {
		if p.Key != "providers["+string(rune('0'+i))+"]" || p.Text != "provider id must use only a-z, 0-9 and -; the provider is ignored" {
			t.Fatalf("problem %d: %+v", i, p)
		}
	}
}

func TestModelsFileDiagnosticsHoldNoUserText(t *testing.T) {
	const marker = "zq-marker-7"
	bodies := []string{
		// The marker as a provider id that breaks the store rule.
		`{"providers": {"Zq-Marker-7": {"baseUrl": "http://h"}}}`,
		// The marker as a provider id, a model id, an unknown key and values.
		`{"providers": {"` + marker + `": {"baseUrl": "` + marker + `", "apiKey": "` + marker + `", "api": "` + marker + `",
			"headers": {"` + marker + `": 5},
			"models": [{"id": "` + marker + `", "` + marker + `": "` + marker + `", "contextWindow": "` + marker + `"}],
			"modelOverrides": {"` + marker + `": {"input": ["` + marker + `"]}}}}}`,
		// The marker as a model id, an override id, unknown keys and a level key.
		`{"` + marker + `": 1, "providers": {"local": {"baseUrl": "h", "apiKey": "k", "api": "a",
			"models": [{"id": "` + marker + `", "` + marker + `": "` + marker + `", "contextWindow": "` + marker + `"},
				{"id": "x", "thinkingLevelMap": {"` + marker + `": "` + marker + `"}}],
			"modelOverrides": {"` + marker + `": {"input": ["` + marker + `"], "` + marker + `": 1}}}}}`,
		// The marker in a broken file.
		`{"providers": {"` + marker + `": `,
		`{"providers": /* ` + marker,
	}
	for i, body := range bodies {
		_, problems, err := parseModelsFile([]byte(body), "/home/models.json", testBuiltIn)
		if err != nil {
			problems = append(problems, Diagnostic{Text: err.Error()})
		}
		if len(problems) == 0 {
			t.Fatalf("body %d: no problem", i)
		}
		for _, p := range problems {
			all := strings.ToLower(string(p.Layer) + p.Path + p.Key + p.Text)
			if strings.Contains(all, marker) {
				t.Fatalf("body %d: diagnostic %+v holds user text", i, p)
			}
		}
	}
}

func TestModelsFileFileProblems(t *testing.T) {
	cases := map[string]string{
		"not JSON":       "{\n\"providers\": {\n\"a\" 1}}",
		"array":          `[1, 2]`,
		"empty":          ``,
		"two values":     `{} {}`,
		"unterminated":   `{"providers": {`,
		"open comment":   "{\n/* x",
		"string at root": `"text"`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			_, _, err := parseModelsFile([]byte(body), "/home/models.json", testBuiltIn)
			if err == nil {
				t.Fatal("a file problem must be an error")
			}
		})
	}
	_, _, err := parseModelsFile([]byte("{\n\"providers\": {\n\"a\" 1}}"), "p", nil)
	if err == nil || err.Error() != "invalid JSON at line 3" {
		t.Fatalf("error %v", err)
	}
}

func TestModelsFileCompatKeepsRawValues(t *testing.T) {
	file, _ := parseModels(t, `{"providers": {"xai": {"compat": {"maxTokensField": "max_tokens", "n": 1.5, "o": {"a": [1]}}}}}`)
	got := file.Providers["xai"].Compat
	want := map[string]json.RawMessage{
		"maxTokensField": json.RawMessage(`"max_tokens"`),
		"n":              json.RawMessage(`1.5`),
		"o":              json.RawMessage(`{"a": [1]}`),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("compat %q", got)
	}
}

func TestModelsFileBaseURLMustBeHTTPWithHostAndNoUserInfo(t *testing.T) {
	file, problems := parseModels(t, `{"providers": {
		"noscheme": {"baseUrl": "proxy.example/v1", "apiKey": "k"},
		"ftp": {"baseUrl": "ftp://proxy.example/v1", "apiKey": "k"},
		"nohost": {"baseUrl": "https:///v1", "apiKey": "k"},
		"opaque": {"baseUrl": "https:proxy.example", "apiKey": "k"},
		"userinfo": {"baseUrl": "https://user:tok-secret@proxy.example/v1", "apiKey": "k"},
		"good": {"baseUrl": "https://proxy.example:8443/v1", "apiKey": "k"},
		"local": {"baseUrl": "http://127.0.0.1:11434/v1", "apiKey": "k"}
	}}`)
	if len(file.Providers) != 2 || file.Providers["good"].BaseURL == "" || file.Providers["local"].BaseURL == "" {
		t.Fatalf("providers %+v", file.Providers)
	}
	const text = ".baseUrl: baseUrl must be an http or https URL with a host and no user name or password; the provider is ignored"
	want := []string{"providers[0]" + text, "providers[1]" + text, "providers[2]" + text, "providers[3]" + text, "providers[4]" + text}
	got := problemKeys(problems)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("problems\n%s", strings.Join(got, "\n"))
	}
	for _, p := range got {
		if strings.Contains(p, "proxy.example") || strings.Contains(p, "secret") {
			t.Fatalf("problem holds the value: %s", p)
		}
	}
}
