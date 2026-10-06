package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

var errAuthorizationDenied = errors.New("auth: authorization denied")

type authorization struct{ code, client string }

func randomValue() (string, error) {
	var b [32]byte
	if _, e := rand.Read(b[:]); e != nil {
		return "", e
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}
func pkce() (string, string, error) {
	v, e := randomValue()
	if e != nil {
		return "", "", e
	}
	h := sha256.Sum256([]byte(v))
	return v, base64.RawURLEncoding.EncodeToString(h[:]), nil
}
func parseCallback(input, redirect, state string, bare, client bool) (authorization, error) {
	bad := errors.New("auth: invalid callback or state")
	if len(input) > privateInputLimit {
		return authorization{}, bad
	}
	input = strings.TrimSpace(input)
	var q url.Values
	if u, e := url.Parse(input); e == nil && u.IsAbs() {
		expected, _ := url.Parse(redirect)
		if u.Scheme != expected.Scheme || u.Host != expected.Host || u.Path != expected.Path || u.User != nil || u.Fragment != "" {
			return authorization{}, bad
		}
		q, e = url.ParseQuery(u.RawQuery)
		if e != nil {
			return authorization{}, bad
		}
	} else if bare {
		parts := strings.Split(input, "#")
		if len(parts) > 2 || parts[0] == "" {
			return authorization{}, bad
		}
		q = url.Values{"code": {parts[0]}, "state": {state}}
		if len(parts) == 2 {
			q.Set("state", parts[1])
		}
	} else {
		return authorization{}, bad
	}
	if len(q["state"]) != 1 || q.Get("state") != state {
		return authorization{}, bad
	}
	if q.Get("error") != "" {
		return authorization{}, errAuthorizationDenied
	}
	if len(q["code"]) != 1 || strings.TrimSpace(q.Get("code")) == "" {
		return authorization{}, bad
	}
	a := authorization{code: q.Get("code"), client: q.Get("client_id")}
	if client && (len(q["client_id"]) != 1 || strings.TrimSpace(a.client) == "") {
		return authorization{}, bad
	}
	return a, nil
}

func awaitAuthorization(ctx context.Context, r LoginRequest, authorize, redirect, state string, browser, issued bool) (authorization, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(chan struct {
		a authorization
		e error
	}, 2)
	var server *http.Server
	if browser {
		u, _ := url.Parse(redirect)
		listener, e := net.Listen("tcp", "127.0.0.1:"+u.Port())
		if e != nil {
			return authorization{}, errors.New("auth: callback port is unavailable")
		}
		server = &http.Server{ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, MaxHeaderBytes: 32 * 1024}
		server.Handler = http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if req.Method != http.MethodGet || len(req.RequestURI) > privateInputLimit || req.URL.Path != u.Path || req.Host != u.Host {
				http.Error(w, "Invalid callback", http.StatusBadRequest)
				return
			}
			a, e := parseCallback(redirect+"?"+req.URL.RawQuery, redirect, state, false, issued)
			if e != nil {
				if errors.Is(e, errAuthorizationDenied) {
					select {
					case results <- struct {
						a authorization
						e error
					}{e: e}:
					default:
					}
				}
				http.Error(w, "Invalid callback", http.StatusBadRequest)
				return
			}
			select {
			case results <- struct {
				a authorization
				e error
			}{a: a}:
			default:
			}
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = w.Write([]byte("Callback received. Return to the command to confirm that credentials were saved."))
		})
		serveDone := make(chan struct{})
		defer func() { _ = server.Close(); <-serveDone }()
		go func() { defer close(serveDone); _ = server.Serve(listener) }()
	}
	if e := notify(ctx, r, LoginNotice{URL: authorize, Message: "Complete sign-in, then paste the callback or code if required."}); e != nil {
		return authorization{}, e
	}
	var wg sync.WaitGroup
	if r.Input != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, e := privateInput(ctx, r)
			if browser && errors.Is(e, io.EOF) {
				return
			}
			var a authorization
			if e == nil {
				a, e = parseCallback(v, redirect, state, !browser && !issued, issued)
			}
			select {
			case results <- struct {
				a authorization
				e error
			}{a, e}:
			case <-ctx.Done():
			}
		}()
		defer func() { cancel(); wg.Wait() }()
	}
	if !browser && r.Input == nil {
		return authorization{}, errors.New("auth: private input required")
	}
	select {
	case <-ctx.Done():
		return authorization{}, ctx.Err()
	case result := <-results:
		return result.a, result.e
	}
}
