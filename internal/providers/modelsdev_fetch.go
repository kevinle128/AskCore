package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
)

const (
	// modelsDevURL is the models.dev payload. Ask reads models.dev directly,
	// so that a new model id gets metadata without a new Ask build.
	modelsDevURL = "https://models.dev/api.json"
	// maxModelsDevBody caps the decoded body. The full payload was about 6 MB
	// on 2026-10-28; the cap leaves room to grow and stops a bad server from
	// filling memory. The transport removes gzip first, so the cap also
	// applies to a compressed body that expands.
	maxModelsDevBody = 32 << 20
	// modelsDevUserAgent is the same product name that the model adapters
	// send.
	modelsDevUserAgent = "AskCore"
)

// errModelsDevTooLarge is the read error when the body is longer than
// maxModelsDevBody.
var errModelsDevTooLarge = fmt.Errorf("models.dev payload is larger than %d bytes", maxModelsDevBody)

// FetchModelsDev downloads the models.dev payload and returns the filtered
// subset: only the named providers and, in each model, only the fields that
// MapModelsDev reads. The subset has the layout of catalog_snapshot.json
// without its fetchedAt and source fields; MapModelsDev ignores top-level keys
// that are not providers, so it reads both.
//
// A non-empty etag is sent as If-None-Match. On 304 the result is
// notModified, a nil payload, and the response ETag (or etag when the
// response has none). The caller sends an etag only when it was stored for the
// same provider list, because a 304 confirms the old filtered payload, not the
// new list.
//
// The request carries only Accept and User-Agent (plus If-None-Match and what
// the transport adds), never a credential. The caller selects the client, so
// a nil client is an error rather than a silent fallback to a default client.
// A status other than 2xx or 304 gives an error with the status code only.
func FetchModelsDev(ctx context.Context, client *http.Client, etag string, providers []string) (payload []byte, newETag string, notModified bool, err error) {
	if client == nil {
		return nil, "", false, errors.New("models.dev fetch: nil HTTP client")
	}
	if len(providers) == 0 {
		// An empty subset with a valid ETag would be cached as a good result.
		return nil, "", false, errors.New("models.dev fetch: no providers")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelsDevURL, nil)
	if err != nil {
		return nil, "", false, fmt.Errorf("models.dev fetch: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", modelsDevUserAgent)
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", false, fmt.Errorf("models.dev fetch: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch {
	case resp.StatusCode == http.StatusNotModified:
		newETag = resp.Header.Get("ETag")
		if newETag == "" {
			newETag = etag
		}
		return nil, newETag, true, nil
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		// Only the code: a body from an unknown server is not shown.
		return nil, "", false, fmt.Errorf("models.dev fetch: status %d", resp.StatusCode)
	}

	top, err := filterModelsDev(&cappedReader{r: resp.Body, left: maxModelsDevBody}, providers)
	if err != nil {
		return nil, "", false, err
	}
	payload, err = json.Marshal(top)
	if err != nil {
		return nil, "", false, fmt.Errorf("models.dev payload: %w", err)
	}
	return payload, resp.Header.Get("ETag"), false, nil
}

// filterModelsDev reads a models.dev payload as a stream and keeps the named
// providers and, in each model, only the fields that the mapper reads. It
// decodes one top-level value at a time, so a skipped provider is never held
// with the others. Each kept value is {"models": {...}}, the layout that
// MapModelsDev reads.
func filterModelsDev(r io.Reader, providers []string) (map[string]json.RawMessage, error) {
	dec := json.NewDecoder(r)
	if err := expectDelim(dec, '{'); err != nil {
		return nil, err
	}
	out := map[string]json.RawMessage{}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("models.dev payload: %w", err)
		}
		key, ok := tok.(string)
		if !ok {
			return nil, fmt.Errorf("models.dev payload: unexpected token %v", tok)
		}
		if !slices.Contains(providers, key) {
			// Any value type is skipped, also the snapshot header strings.
			var skip json.RawMessage
			if err := dec.Decode(&skip); err != nil {
				return nil, fmt.Errorf("models.dev payload: %w", err)
			}
			continue
		}
		var p struct {
			Models map[string]map[string]json.RawMessage `json:"models"`
		}
		if err := dec.Decode(&p); err != nil {
			return nil, fmt.Errorf("models.dev provider %s: %w", key, err)
		}
		models := make(map[string]map[string]json.RawMessage, len(p.Models))
		for id, fields := range p.Models {
			kept := map[string]json.RawMessage{}
			for _, f := range modelsDevFields {
				if v, ok := fields[f]; ok {
					kept[f] = v
				}
			}
			models[id] = kept
		}
		b, err := json.Marshal(map[string]any{"models": models})
		if err != nil {
			return nil, fmt.Errorf("models.dev provider %s: %w", key, err)
		}
		out[key] = b
	}
	if err := expectDelim(dec, '}'); err != nil {
		return nil, err
	}
	// Data after the object means the body is not one JSON document.
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("data after the top-level object")
		}
		return nil, fmt.Errorf("models.dev payload: %w", err)
	}
	return out, nil
}

func expectDelim(dec *json.Decoder, want json.Delim) error {
	tok, err := dec.Token()
	if err != nil {
		return fmt.Errorf("models.dev payload: %w", err)
	}
	if d, ok := tok.(json.Delim); !ok || d != want {
		return fmt.Errorf("models.dev payload: want %q, got %v", want, tok)
	}
	return nil
}

// cappedReader returns errModelsDevTooLarge when more than left bytes come
// from r. Unlike io.LimitReader alone, it tells an overflow apart from a body
// that ends exactly at the cap, and the decoder passes the error through.
type cappedReader struct {
	r    io.Reader
	left int64
}

func (c *cappedReader) Read(p []byte) (int, error) {
	if c.left < 0 {
		return 0, errModelsDevTooLarge
	}
	// One byte past the cap is enough to see an overflow.
	if int64(len(p)) > c.left+1 {
		p = p[:c.left+1]
	}
	n, err := c.r.Read(p)
	c.left -= int64(n)
	if c.left < 0 {
		return 0, errModelsDevTooLarge
	}
	return n, err
}
