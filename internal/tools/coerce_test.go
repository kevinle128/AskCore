package tools_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"AskCore/internal/tools"
)

func prepareWith(t *testing.T, params, raw string) (string, error) {
	t.Helper()
	var r tools.Registry
	require.NoError(t, r.Register(stub("probe", params), tools.SourceInfo{}))
	var in json.RawMessage
	if raw != "" {
		in = json.RawMessage(raw)
	}
	out, err := r.Prepare("probe", in)
	return string(out), err
}

func object(props string) string {
	return `{"type":"object","properties":` + props + `}`
}

type coerceCase struct{ in, want string }

func TestCoerceTable(t *testing.T) {
	rows := []struct {
		name   string
		params string
		cases  []coerceCase
	}{
		{"integer from string", object(`{"n":{"type":"integer"}}`), []coerceCase{
			{`{"n":"5"}`, `{"n":5}`},
		}},
		{"number from string", object(`{"n":{"type":"number"}}`), []coerceCase{
			{`{"n":"2.5"}`, `{"n":2.5}`},
		}},
		{"boolean true from string and 1", object(`{"b":{"type":"boolean"}}`), []coerceCase{
			{`{"b":"true"}`, `{"b":true}`},
			{`{"b":1}`, `{"b":true}`},
		}},
		{"boolean false from string and 0", object(`{"b":{"type":"boolean"}}`), []coerceCase{
			{`{"b":"false"}`, `{"b":false}`},
			{`{"b":0}`, `{"b":false}`},
		}},
		{"string from number and boolean", object(`{"s":{"type":"string"}}`), []coerceCase{
			{`{"s":5}`, `{"s":"5"}`},
			{`{"s":true}`, `{"s":"true"}`},
		}},
		{"null in anyOf kept", object(`{"n":{"anyOf":[{"type":"integer"},{"type":"null"}]}}`), []coerceCase{
			{`{"n":null}`, `{"n":null}`},
		}},
		{"optional null removed", object(`{"n":{"type":"integer"},"s":{"type":"string"}}`), []coerceCase{
			{`{"n":null,"s":"x"}`, `{"s":"x"}`},
		}},
		{"object root from missing or null", object(`{"n":{"type":"integer"}}`), []coerceCase{
			{``, `{}`},
			{`null`, `{}`},
			{"  \n", `{}`},
		}},
		{"anyOf valid arm unchanged", object(`{"n":{"anyOf":[{"type":"string"},{"type":"integer"}]}}`), []coerceCase{
			{`{"n":"5"}`, `{"n":"5"}`},
		}},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			for _, c := range row.cases {
				got, err := prepareWith(t, row.params, c.in)
				require.NoError(t, err, "input %q", c.in)
				assert.Equal(t, c.want, got, "input %q", c.in)
			}
		})
	}
	t.Run("integer from fractional string is an error", func(t *testing.T) {
		_, err := prepareWith(t, object(`{"n":{"type":"integer"}}`), `{"n":"5.7"}`)
		require.EqualError(t, err, "Validation failed for tool \"probe\":\n"+
			"  - n: must be integer\n"+
			"\n"+
			"Received arguments:\n"+
			"{\n  \"n\": \"5.7\"\n}")
	})
}

func TestCoerceFirstDeclaredTypeWins(t *testing.T) {
	got, err := prepareWith(t, object(`{"v":{"type":["string","number"]}}`), `{"v":true}`)
	require.NoError(t, err)
	assert.Equal(t, `{"v":"true"}`, got)

	got, err = prepareWith(t, object(`{"v":{"type":["number","string"]}}`), `{"v":true}`)
	require.NoError(t, err)
	assert.Equal(t, `{"v":1}`, got)

	got, err = prepareWith(t, object(`{"v":{"type":["number","string"]}}`), `{"v":"7"}`)
	require.NoError(t, err)
	assert.Equal(t, `{"v":"7"}`, got)
}

func TestCoerceNested(t *testing.T) {
	params := `{"type":"object","required":["n"],"properties":{
		"n":{"type":"integer"},
		"xs":{"type":"array","items":{"type":"integer"}},
		"flags":{"type":"object","additionalProperties":{"type":"boolean"}}}}`
	got, err := prepareWith(t, params, `{"n":null,"xs":["1","2e3"],"flags":{"a":"true","b":0},"extra":"5"}`)
	require.NoError(t, err)
	assert.Equal(t, `{"extra":"5","flags":{"a":true,"b":false},"n":0,"xs":[1,2000]}`, got)
}

func TestCoerceAnyOfCoercesIntoFirstFittingArm(t *testing.T) {
	got, err := prepareWith(t, object(`{"n":{"anyOf":[{"type":"integer"},{"type":"boolean"}]}}`), `{"n":"12"}`)
	require.NoError(t, err)
	assert.Equal(t, `{"n":12}`, got)
}
