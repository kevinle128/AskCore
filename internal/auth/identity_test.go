package auth

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestChatGPTIdentityVerifier(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	other, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	var jwksReads int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jwksReads++
		if r.URL.Path != "/keys" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{map[string]any{
			"kty": "RSA", "kid": "known", "alg": "RS256", "use": "sig",
			"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
		}}})
	}))
	defer server.Close()
	issuer := server.URL + "/issuer"
	verifier, err := NewChatGPTIdentityVerifier(issuer, server.URL+"/keys", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	claims := map[string]any{"iss": issuer, "aud": "issued-client", "sub": "account-1", "nonce": "attempt-nonce", "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix()}
	sign := func(values map[string]any, signingKey *rsa.PrivateKey, alg string) string {
		t.Helper()
		encode := func(v any) string {
			b, e := json.Marshal(v)
			if e != nil {
				t.Fatal(e)
			}
			return base64.RawURLEncoding.EncodeToString(b)
		}
		body := encode(map[string]any{"alg": alg, "kid": "known", "typ": "JWT"}) + "." + encode(values)
		digest := sha256.Sum256([]byte(body))
		sig, e := rsa.SignPKCS1v15(rand.Reader, signingKey, crypto.SHA256, digest[:])
		if e != nil {
			t.Fatal(e)
		}
		return body + "." + base64.RawURLEncoding.EncodeToString(sig)
	}
	valid := sign(claims, key, "RS256")
	subject, err := verifier.Verify(context.Background(), valid, "issued-client", "attempt-nonce")
	if err != nil || subject != "account-1" {
		t.Fatalf("valid token: subject=%q err=%v", subject, err)
	}
	if jwksReads == 0 {
		t.Fatal("verifier did not fetch configured JWKS")
	}
	for _, tc := range []struct {
		name   string
		change func(map[string]any)
		key    *rsa.PrivateKey
		alg    string
		client string
		nonce  string
	}{
		{"signature", nil, other, "RS256", "issued-client", "attempt-nonce"},
		{"issuer", func(c map[string]any) { c["iss"] = "https://other.example" }, key, "RS256", "issued-client", "attempt-nonce"},
		{"audience", func(c map[string]any) { c["aud"] = "other-client" }, key, "RS256", "issued-client", "attempt-nonce"},
		{"multiple audiences without party", func(c map[string]any) { c["aud"] = []string{"issued-client", "other-client"} }, key, "RS256", "issued-client", "attempt-nonce"},
		{"wrong authorized party", func(c map[string]any) { c["azp"] = "other-client" }, key, "RS256", "issued-client", "attempt-nonce"},
		{"expiry", func(c map[string]any) { c["exp"] = time.Now().Add(-time.Hour).Unix() }, key, "RS256", "issued-client", "attempt-nonce"},
		{"nonce", func(c map[string]any) { c["nonce"] = "other-nonce" }, key, "RS256", "issued-client", "attempt-nonce"},
		{"subject", func(c map[string]any) { delete(c, "sub") }, key, "RS256", "issued-client", "attempt-nonce"},
		{"algorithm", nil, key, "RS384", "issued-client", "attempt-nonce"},
		{"issued client", nil, key, "RS256", "different-client", "attempt-nonce"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			copy := make(map[string]any, len(claims))
			for k, v := range claims {
				copy[k] = v
			}
			if tc.change != nil {
				tc.change(copy)
			}
			if _, err := verifier.Verify(context.Background(), sign(copy, tc.key, tc.alg), tc.client, tc.nonce); err == nil {
				t.Fatal("accepted invalid token")
			}
		})
	}
	t.Run("refresh without login nonce", func(t *testing.T) {
		refreshClaims := map[string]any{"iss": issuer, "aud": "issued-client", "sub": "account-1", "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix()}
		subject, err := verifier.VerifyRefresh(context.Background(), sign(refreshClaims, key, "RS256"), "issued-client")
		if err != nil || subject != "account-1" {
			t.Fatalf("refresh identity: %v", err)
		}
		if _, err = verifier.VerifyRefresh(context.Background(), sign(refreshClaims, other, "RS256"), "issued-client"); err == nil {
			t.Fatal("refresh accepted bad signature")
		}
	})

}
