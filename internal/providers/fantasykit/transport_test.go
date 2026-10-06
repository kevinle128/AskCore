package fantasykit

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClientSendsBodyWithSortedKeysAndSameContent(t *testing.T) {
	const in = `{"stream":true,"model":"m","messages":[{"role":"user","content":"a <b> & é"}],"thinking":{"type":"adaptive"},"max_tokens":9}`
	var got string
	var length int64
	base := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		b, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		got, length = string(b), r.ContentLength
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})}
	req, err := http.NewRequest(http.MethodPost, "https://example.test/v1/messages", strings.NewReader(in))
	require.NoError(t, err)
	resp, err := Client(base, nil, nil).Do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()

	assert.Equal(t, `{"max_tokens":9,"messages":[{"role":"user","content":"a <b> & é"}],"model":"m","stream":true,"thinking":{"type":"adaptive"}}`, got)
	assert.EqualValues(t, len(got), length, "the length follows the rewritten body")
}

func TestClientKeepsBodyThatIsNoJSONObject(t *testing.T) {
	var got string
	base := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		b, _ := io.ReadAll(r.Body)
		got = string(b)
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})}
	req, err := http.NewRequest(http.MethodPost, "https://example.test/x", strings.NewReader(`[3,1,2]`))
	require.NoError(t, err)
	resp, err := Client(base, nil, nil).Do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, `[3,1,2]`, got)
}
