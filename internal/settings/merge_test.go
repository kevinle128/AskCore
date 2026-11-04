package settings

import (
	"encoding/json"
	"testing"
)

func rawFields(t *testing.T, s string) map[string]json.RawMessage {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestMergeNestedObjectsAndReplacesArrays(t *testing.T) {
	user := rawFields(t, `{
		"a": {"x": 1, "y": {"p": true, "q": "keep"}},
		"list": [1, 2, 3],
		"scalar": "user",
		"kept": 1.50,
		"nulled": "user",
		"obj2scalar": {"z": 1}
	}`)
	project := rawFields(t, `{
		"a": {"y": {"p": false, "n": null}, "z": [9]},
		"list": [4],
		"scalar": "project",
		"nulled": null,
		"obj2scalar": 7,
		"new": {"k": 1e3}
	}`)
	got := mergeFields(user, project)
	want := map[string]string{
		"a":          `{"x":1,"y":{"p":false,"q":"keep"},"z":[9]}`,
		"list":       `[4]`,
		"scalar":     `"project"`,
		"kept":       `1.50`,
		"nulled":     `"user"`,
		"obj2scalar": `7`,
		"new":        `{"k": 1e3}`,
	}
	if len(got) != len(want) {
		t.Fatalf("merged keys: %v", got)
	}
	for key, value := range want {
		if string(got[key]) != value {
			t.Fatalf("%s = %s, want %s", key, got[key], value)
		}
	}
	// The inputs stay the same.
	if string(user["scalar"]) != `"user"` || len(project) != 6 {
		t.Fatalf("inputs changed: %v %v", user, project)
	}
}

func TestMergeIgnoresNullOnlyInProject(t *testing.T) {
	got := mergeFields(rawFields(t, `{"a":null}`), rawFields(t, `{"b":null}`))
	if string(got["a"]) != "null" {
		t.Fatalf("user null: %s", got["a"])
	}
	if _, ok := got["b"]; ok {
		t.Fatalf("project null was added: %s", got["b"])
	}
}

func TestFilterProjectKeysTable(t *testing.T) {
	project := rawFields(t, `{"Trust":true,"env":{"A":"b"},"defaultModel":"x","Hooks":[],"deviceID":"d"}`)
	kept, dropped := filterProjectKeys(project)
	if len(kept) != 1 || kept["defaultModel"] == nil {
		t.Fatalf("kept: %v", kept)
	}
	want := []string{"Hooks", "Trust", "deviceID", "env"}
	if len(dropped) != len(want) {
		t.Fatalf("dropped: %v", dropped)
	}
	for i, key := range want {
		if dropped[i] != key {
			t.Fatalf("dropped: %v, want %v", dropped, want)
		}
	}
}
