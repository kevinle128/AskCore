package settings

import (
	"bytes"
	"encoding/json"
	"sort"
	"strings"
)

// userOnlyKeys are keys that only the user settings file can set. A project
// file that sets one of them is not obeyed, whatever its trust.
var userOnlyKeys = []string{
	defaultProjectTrustKey,
}

// deniedProjectKeys are top-level keys that a project file cannot set. The
// key and all of its subtree are dropped. Most of these keys have no reader
// yet. They are on the list so that a later reader cannot take them from a
// project by accident.
var deniedProjectKeys = []string{
	"trust",
	"httpProxy",
	"env",
	"shellPath",
	"shellCommandPrefix",
	"npmCommand",
	"providers",
	"hooks",
	"cacheWarming",
	"deviceId",
}

// projectKeyBarred reports whether a project file cannot set key. The match
// ignores case: Go's JSON decoder matches struct fields without regard to
// case, so an exact match would let "DefaultProjectTrust" through to such a
// decoder.
func projectKeyBarred(key string) bool {
	for _, list := range [][]string{userOnlyKeys, deniedProjectKeys} {
		for _, barred := range list {
			if strings.EqualFold(key, barred) {
				return true
			}
		}
	}
	return false
}

// filterProjectKeys returns a copy of the project fields without the barred
// keys, and the barred keys that it removed, in sorted order.
func filterProjectKeys(project map[string]json.RawMessage) (map[string]json.RawMessage, []string) {
	kept := make(map[string]json.RawMessage, len(project))
	var dropped []string
	for key, value := range project {
		if projectKeyBarred(key) {
			dropped = append(dropped, key)
			continue
		}
		kept[key] = value
	}
	sort.Strings(dropped)
	return kept, dropped
}

// mergeFields returns the user fields with the project fields on top. When
// both values are JSON objects, they merge key by key at each level. Any other
// project value (an array, a string, a number, a bool) replaces the user
// value. A project null is ignored, at each level. Leaf values keep their
// bytes, so the merge does not change unknown keys or number formats. The
// inputs do not change.
func mergeFields(user, project map[string]json.RawMessage) map[string]json.RawMessage {
	out := make(map[string]json.RawMessage, len(user)+len(project))
	for key, value := range user {
		out[key] = value
	}
	for key, value := range project {
		if isJSONNull(value) {
			continue
		}
		if base, ok := out[key]; ok {
			if merged, ok := mergeObjects(base, value); ok {
				out[key] = merged
				continue
			}
		}
		out[key] = value
	}
	return out
}

// mergeObjects merges two raw JSON objects. It returns false when one of the
// values is not an object.
func mergeObjects(user, project json.RawMessage) (json.RawMessage, bool) {
	userFields, ok := jsonObject(user)
	if !ok {
		return nil, false
	}
	projectFields, ok := jsonObject(project)
	if !ok {
		return nil, false
	}
	b, err := json.Marshal(mergeFields(userFields, projectFields))
	if err != nil {
		return nil, false
	}
	return b, true
}

func jsonObject(raw json.RawMessage) (map[string]json.RawMessage, bool) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, false
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(trimmed, &fields) != nil || fields == nil {
		return nil, false
	}
	return fields, true
}

func isJSONNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}
