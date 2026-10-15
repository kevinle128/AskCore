package tools

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

// echoLimit caps the raw arguments echoed in a validation error, so a huge
// bad argument cannot flood the model context. Pi has no cap.
const echoLimit = 2 * 1024

const truncatedMarker = "\n... (truncated)"

var englishPrinter = message.NewPrinter(language.English)

func prepare(name string, params *shape, raw json.RawMessage) (json.RawMessage, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		raw = json.RawMessage(`{}`)
	}
	v, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return nil, validationError(name, []issue{{path: "root", msg: "must be valid JSON"}}, raw)
	}
	dropOptionalNulls(v, params)
	v = coerce(v, params)
	if err := params.sch.Validate(v); err != nil {
		var ve *jsonschema.ValidationError
		if !errors.As(err, &ve) {
			return nil, err
		}
		return nil, validationError(name, collectIssues(ve, nil), raw)
	}
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(out.Bytes(), []byte("\n")), nil
}

type issue struct{ path, msg string }

func validationError(name string, issues []issue, raw []byte) error {
	sort.SliceStable(issues, func(i, j int) bool { return issues[i].path < issues[j].path })
	var b strings.Builder
	b.WriteString(`Validation failed for tool "` + name + `":` + "\n")
	for _, is := range issues {
		b.WriteString("  - " + is.path + ": " + is.msg + "\n")
	}
	b.WriteString("\nReceived arguments:\n")
	b.WriteString(echoArgs(raw))
	return errors.New(b.String())
}

func echoArgs(raw []byte) string {
	var b bytes.Buffer
	if json.Indent(&b, raw, "", "  ") != nil {
		b.Reset()
		b.Write(raw)
	}
	out := b.Bytes()
	if len(out) <= echoLimit {
		return string(out)
	}
	cut := echoLimit
	for cut > 0 && !utf8.RuneStart(out[cut]) {
		cut--
	}
	return string(out[:cut]) + truncatedMarker
}

// collectIssues flattens the error tree to its leaves, except that a failed
// anyOf or oneOf is one issue, as in TypeBox.
func collectIssues(e *jsonschema.ValidationError, out []issue) []issue {
	switch e.ErrorKind.(type) {
	case *kind.AnyOf, *kind.OneOf:
	default:
		if len(e.Causes) > 0 {
			for _, c := range e.Causes {
				out = collectIssues(c, out)
			}
			return out
		}
	}
	return append(out, issue{path: issuePath(e), msg: issueMessage(e.ErrorKind)})
}

func issuePath(e *jsonschema.ValidationError) string {
	path := strings.Join(e.InstanceLocation, ".")
	if req, ok := e.ErrorKind.(*kind.Required); ok && len(req.Missing) > 0 {
		if path == "" {
			return req.Missing[0]
		}
		return path + "." + req.Missing[0]
	}
	if path == "" {
		return "root"
	}
	return path
}

// issueMessage uses TypeBox's en_US wording, which is what Pi sends.
func issueMessage(k jsonschema.ErrorKind) string {
	switch k := k.(type) {
	case *kind.Required:
		return "must have required properties " + strings.Join(k.Missing, ", ")
	case *kind.Type:
		if len(k.Want) == 1 {
			return "must be " + k.Want[0]
		}
		return "must be either " + strings.Join(k.Want, " or ")
	case *kind.AdditionalProperties:
		return "must not have additional properties"
	case *kind.AnyOf:
		return "must match a schema in anyOf"
	case *kind.OneOf:
		return "must match exactly one schema in oneOf"
	case *kind.Enum:
		return "must be equal to one of the allowed values"
	case *kind.Const:
		return "must be equal to constant"
	case *kind.Not:
		return "must not be valid"
	case *kind.FalseSchema:
		return "schema is false"
	case *kind.Format:
		return fmt.Sprintf("must match format %q", k.Want)
	case *kind.Pattern:
		return fmt.Sprintf("must match pattern %q", k.Want)
	case *kind.MinLength:
		return fmt.Sprintf("must not have fewer than %d characters", k.Want)
	case *kind.MaxLength:
		return fmt.Sprintf("must not have more than %d characters", k.Want)
	case *kind.MinItems:
		return fmt.Sprintf("must not have fewer than %d items", k.Want)
	case *kind.MaxItems:
		return fmt.Sprintf("must not have more than %d items", k.Want)
	case *kind.MinProperties:
		return fmt.Sprintf("must not have fewer than %d properties", k.Want)
	case *kind.MaxProperties:
		return fmt.Sprintf("must not have more than %d properties", k.Want)
	case *kind.UniqueItems:
		return "must not have duplicate items"
	case *kind.Minimum:
		return "must be >= " + ratText(k.Want)
	case *kind.Maximum:
		return "must be <= " + ratText(k.Want)
	case *kind.ExclusiveMinimum:
		return "must be > " + ratText(k.Want)
	case *kind.ExclusiveMaximum:
		return "must be < " + ratText(k.Want)
	case *kind.MultipleOf:
		return "must be multiple of " + ratText(k.Want)
	}
	return k.LocalizedString(englishPrinter)
}

func ratText(r *big.Rat) string {
	if r.IsInt() {
		return r.Num().String()
	}
	f, _ := r.Float64()
	return strconv.FormatFloat(f, 'f', -1, 64)
}
