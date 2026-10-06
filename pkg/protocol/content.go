package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

// Discriminator values of the "type" field in a content block.
const (
	blockTypeText     = "text"
	blockTypeThinking = "thinking"
	blockTypeImage    = "image"
	blockTypeToolCall = "toolCall"
)

// UserBlock is a content block that can appear in a user message, a tool
// result, or a system message (text only). The implementations are Text and Image.
type UserBlock interface{ userBlock() }

// AssistantBlock is a content block that can appear in an assistant message.
// The implementations are Text, Thinking and ToolCall. An assistant message
// never holds an image.
type AssistantBlock interface{ assistantBlock() }

// Text is a plain text block.
type Text struct {
	Text          string
	TextSignature *string
}

// Thinking is a reasoning block. When Redacted is true, the encrypted payload
// is in ThinkingSignature and Thinking can be empty.
type Thinking struct {
	Thinking          string
	ThinkingSignature *string
	Redacted          *bool
}

// Image is a base64 image block.
type Image struct {
	Data     string
	MimeType string
}

// ToolCall is a request from the model to run a tool. Arguments is a JSON
// object. A nil Arguments encodes as the empty object.
type ToolCall struct {
	ID               string
	Name             string
	Arguments        json.RawMessage
	ThoughtSignature *string
	Namespace        *string
}

func (Text) userBlock()          {}
func (Image) userBlock()         {}
func (Text) assistantBlock()     {}
func (Thinking) assistantBlock() {}
func (ToolCall) assistantBlock() {}

// MarshalJSON encodes the block with the "text" discriminator.
func (t Text) MarshalJSON() ([]byte, error) {
	return marshalJSON(struct {
		Type          string  `json:"type"`
		Text          string  `json:"text"`
		TextSignature *string `json:"textSignature,omitempty"`
	}{blockTypeText, t.Text, t.TextSignature})
}

// UnmarshalJSON decodes a block and checks its discriminator.
func (t *Text) UnmarshalJSON(b []byte) error {
	var w struct {
		Type          string  `json:"type"`
		Text          string  `json:"text"`
		TextSignature *string `json:"textSignature"`
	}
	if err := json.Unmarshal(b, &w); err != nil {
		return err
	}
	if err := checkBlockType(w.Type, blockTypeText); err != nil {
		return err
	}
	*t = Text{Text: w.Text, TextSignature: w.TextSignature}
	return nil
}

// MarshalJSON encodes the block with the "thinking" discriminator.
func (t Thinking) MarshalJSON() ([]byte, error) {
	return marshalJSON(struct {
		Type              string  `json:"type"`
		Thinking          string  `json:"thinking"`
		ThinkingSignature *string `json:"thinkingSignature,omitempty"`
		Redacted          *bool   `json:"redacted,omitempty"`
	}{blockTypeThinking, t.Thinking, t.ThinkingSignature, t.Redacted})
}

// UnmarshalJSON decodes a block and checks its discriminator.
func (t *Thinking) UnmarshalJSON(b []byte) error {
	var w struct {
		Type              string  `json:"type"`
		Thinking          string  `json:"thinking"`
		ThinkingSignature *string `json:"thinkingSignature"`
		Redacted          *bool   `json:"redacted"`
	}
	if err := json.Unmarshal(b, &w); err != nil {
		return err
	}
	if err := checkBlockType(w.Type, blockTypeThinking); err != nil {
		return err
	}
	*t = Thinking{Thinking: w.Thinking, ThinkingSignature: w.ThinkingSignature, Redacted: w.Redacted}
	return nil
}

// MarshalJSON encodes the block with the "image" discriminator.
func (i Image) MarshalJSON() ([]byte, error) {
	return marshalJSON(struct {
		Type     string `json:"type"`
		Data     string `json:"data"`
		MimeType string `json:"mimeType"`
	}{blockTypeImage, i.Data, i.MimeType})
}

// UnmarshalJSON decodes a block and checks its discriminator.
func (i *Image) UnmarshalJSON(b []byte) error {
	var w struct {
		Type     string `json:"type"`
		Data     string `json:"data"`
		MimeType string `json:"mimeType"`
	}
	if err := json.Unmarshal(b, &w); err != nil {
		return err
	}
	if err := checkBlockType(w.Type, blockTypeImage); err != nil {
		return err
	}
	*i = Image{Data: w.Data, MimeType: w.MimeType}
	return nil
}

// MarshalJSON encodes the block with the "toolCall" discriminator. The
// arguments must be a JSON object; nil becomes the empty object.
func (c ToolCall) MarshalJSON() ([]byte, error) {
	args := c.Arguments
	if len(bytes.TrimSpace(args)) == 0 {
		args = json.RawMessage(`{}`)
	}
	if !isJSONObject(args) {
		return nil, fmt.Errorf("protocol: tool call %q arguments are not a JSON object", c.ID)
	}
	return marshalJSON(struct {
		Type             string          `json:"type"`
		ID               string          `json:"id"`
		Name             string          `json:"name"`
		Arguments        json.RawMessage `json:"arguments"`
		ThoughtSignature *string         `json:"thoughtSignature,omitempty"`
		Namespace        *string         `json:"namespace,omitempty"`
	}{blockTypeToolCall, c.ID, c.Name, args, c.ThoughtSignature, c.Namespace})
}

// UnmarshalJSON decodes a block and checks its discriminator. Absent or null
// arguments decode to nil; any other non-object value is an error.
func (c *ToolCall) UnmarshalJSON(b []byte) error {
	var w struct {
		Type             string          `json:"type"`
		ID               string          `json:"id"`
		Name             string          `json:"name"`
		Arguments        json.RawMessage `json:"arguments"`
		ThoughtSignature *string         `json:"thoughtSignature"`
		Namespace        *string         `json:"namespace"`
	}
	if err := json.Unmarshal(b, &w); err != nil {
		return err
	}
	if err := checkBlockType(w.Type, blockTypeToolCall); err != nil {
		return err
	}
	args := w.Arguments
	if isJSONNull(args) {
		args = nil
	} else if !isJSONObject(args) {
		return fmt.Errorf("protocol: tool call %q arguments are not a JSON object", w.ID)
	}
	*c = ToolCall{ID: w.ID, Name: w.Name, Arguments: args, ThoughtSignature: w.ThoughtSignature, Namespace: w.Namespace}
	return nil
}

// checkBlockType returns an error when the decoded discriminator is not the
// wanted one.
func checkBlockType(got, want string) error {
	if got != want {
		return fmt.Errorf("protocol: block type %q is not %q", got, want)
	}
	return nil
}

// peekType reads the "type" discriminator of a JSON object.
func peekType(raw []byte) (string, error) {
	var w struct {
		Type *string `json:"type"`
	}
	if err := json.Unmarshal(raw, &w); err != nil {
		return "", err
	}
	if w.Type == nil {
		return "", errors.New("protocol: block has no type")
	}
	return *w.Type, nil
}

func decodeUserBlock(raw json.RawMessage) (UserBlock, error) {
	typ, err := peekType(raw)
	if err != nil {
		return nil, err
	}
	switch typ {
	case blockTypeText:
		var t Text
		if err := t.UnmarshalJSON(raw); err != nil {
			return nil, err
		}
		return t, nil
	case blockTypeImage:
		var i Image
		if err := i.UnmarshalJSON(raw); err != nil {
			return nil, err
		}
		return i, nil
	default:
		return nil, fmt.Errorf("protocol: block type %q is not allowed in user content", typ)
	}
}

func decodeAssistantBlock(raw json.RawMessage) (AssistantBlock, error) {
	typ, err := peekType(raw)
	if err != nil {
		return nil, err
	}
	switch typ {
	case blockTypeText:
		var t Text
		if err := t.UnmarshalJSON(raw); err != nil {
			return nil, err
		}
		return t, nil
	case blockTypeThinking:
		var t Thinking
		if err := t.UnmarshalJSON(raw); err != nil {
			return nil, err
		}
		return t, nil
	case blockTypeToolCall:
		var c ToolCall
		if err := c.UnmarshalJSON(raw); err != nil {
			return nil, err
		}
		return c, nil
	default:
		return nil, fmt.Errorf("protocol: block type %q is not allowed in assistant content", typ)
	}
}

// splitContent turns absent or null content into no elements, a JSON string
// into one synthetic text block (when allowString is set), and an array into
// its raw elements.
func splitContent(raw json.RawMessage, allowString bool) (elems []json.RawMessage, text *string, err error) {
	t := bytes.TrimSpace(raw)
	switch {
	case len(t) == 0 || isJSONNull(t):
		return nil, nil, nil
	case t[0] == '"':
		if !allowString {
			return nil, nil, errors.New("protocol: content must be an array of blocks")
		}
		var s string
		if err := json.Unmarshal(t, &s); err != nil {
			return nil, nil, err
		}
		return nil, &s, nil
	case t[0] == '[':
		if err := json.Unmarshal(t, &elems); err != nil {
			return nil, nil, err
		}
		return elems, nil, nil
	default:
		return nil, nil, errors.New("protocol: content must be a string or an array of blocks")
	}
}

// decodeUserBlocks decodes user-style content into a non-nil slice.
func decodeUserBlocks(raw json.RawMessage, allowString bool) ([]UserBlock, error) {
	elems, text, err := splitContent(raw, allowString)
	if err != nil {
		return nil, err
	}
	out := make([]UserBlock, 0, len(elems)+1)
	if text != nil {
		out = append(out, Text{Text: *text})
	}
	for i, e := range elems {
		b, err := decodeUserBlock(e)
		if err != nil {
			return nil, fmt.Errorf("content[%d]: %w", i, err)
		}
		out = append(out, b)
	}
	return out, nil
}

func decodeAssistantBlocks(raw json.RawMessage) ([]AssistantBlock, error) {
	elems, _, err := splitContent(raw, false)
	if err != nil {
		return nil, err
	}
	out := make([]AssistantBlock, 0, len(elems))
	for i, e := range elems {
		b, err := decodeAssistantBlock(e)
		if err != nil {
			return nil, fmt.Errorf("content[%d]: %w", i, err)
		}
		out = append(out, b)
	}
	return out, nil
}

// decodeTexts decodes system content: a string or an array of text blocks.
func decodeTexts(raw json.RawMessage) ([]Text, error) {
	elems, text, err := splitContent(raw, true)
	if err != nil {
		return nil, err
	}
	out := make([]Text, 0, len(elems)+1)
	if text != nil {
		out = append(out, Text{Text: *text})
	}
	for i, e := range elems {
		var t Text
		if err := t.UnmarshalJSON(e); err != nil {
			return nil, fmt.Errorf("content[%d]: %w", i, err)
		}
		out = append(out, t)
	}
	return out, nil
}

// marshalBlocks encodes a block slice as a JSON array. A nil slice becomes [].
func marshalBlocks[T any](blocks []T) (json.RawMessage, error) {
	var buf bytes.Buffer
	buf.WriteByte('[')
	for i, b := range blocks {
		if any(b) == nil {
			return nil, fmt.Errorf("protocol: content[%d] is nil", i)
		}
		enc, err := marshalJSON(b)
		if err != nil {
			return nil, fmt.Errorf("content[%d]: %w", i, err)
		}
		if i > 0 {
			buf.WriteByte(',')
		}
		buf.Write(enc)
	}
	buf.WriteByte(']')
	return buf.Bytes(), nil
}

// marshalJSON encodes v without HTML escaping, so the text stays readable and
// byte-stable on the wire.
func marshalJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// isJSONNull reports whether raw is the JSON null literal or empty (absent).
func isJSONNull(raw []byte) bool {
	t := bytes.TrimSpace(raw)
	return len(t) == 0 || string(t) == "null"
}

func isJSONObject(raw []byte) bool {
	t := bytes.TrimSpace(raw)
	return len(t) > 0 && t[0] == '{' && json.Valid(t)
}

func clonePtr[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func cloneRaw(r json.RawMessage) json.RawMessage {
	if r == nil {
		return nil
	}
	return append(json.RawMessage{}, r...)
}

func cloneText(t Text) Text {
	t.TextSignature = clonePtr(t.TextSignature)
	return t
}

func cloneThinking(t Thinking) Thinking {
	t.ThinkingSignature = clonePtr(t.ThinkingSignature)
	t.Redacted = clonePtr(t.Redacted)
	return t
}

func cloneToolCall(c ToolCall) ToolCall {
	c.Arguments = cloneRaw(c.Arguments)
	c.ThoughtSignature = clonePtr(c.ThoughtSignature)
	c.Namespace = clonePtr(c.Namespace)
	return c
}

func cloneUserBlocks(in []UserBlock) []UserBlock {
	if in == nil {
		return nil
	}
	out := make([]UserBlock, len(in))
	for i, b := range in {
		switch value := b.(type) {
		case Text:
			b = cloneText(value)
		case *Text:
			if value != nil {
				copy := cloneText(*value)
				b = &copy
			}
		case *Image:
			b = clonePtr(value)
		}
		out[i] = b
	}
	return out
}

// CloneAssistantBlock returns a deep copy of a block: pointers and raw JSON
// are not shared with the original.
func CloneAssistantBlock(b AssistantBlock) AssistantBlock {
	switch v := b.(type) {
	case Text:
		return cloneText(v)
	case *Text:
		if v != nil {
			copy := cloneText(*v)
			return &copy
		}
	case Thinking:
		return cloneThinking(v)
	case *Thinking:
		if v != nil {
			copy := cloneThinking(*v)
			return &copy
		}
	case ToolCall:
		return cloneToolCall(v)
	case *ToolCall:
		if v != nil {
			copy := cloneToolCall(*v)
			return &copy
		}
	}
	return b
}

func cloneAssistantBlocks(in []AssistantBlock) []AssistantBlock {
	if in == nil {
		return nil
	}
	out := make([]AssistantBlock, len(in))
	for i, b := range in {
		out[i] = CloneAssistantBlock(b)
	}
	return out
}
