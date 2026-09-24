package spotify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// TestChallengeFromVerifierKnownVector checks challengeFromVerifier
// against a verifier/challenge pair independently computed as
// base64url(sha256(verifier)) per RFC 7636 §4.2's S256 transform.
func TestChallengeFromVerifierKnownVector(t *testing.T) {
	const verifier = "dbjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	const want = "eINzvQ3Z8aARYw9pLv0ISwvsVZ3cecpv476AyyP_wEo"
	if got := challengeFromVerifier(verifier); got != want {
		t.Errorf("challengeFromVerifier(%q) = %q, want %q", verifier, got, want)
	}
}

func TestGenerateVerifierLengthAndUniqueness(t *testing.T) {
	a, err := generateVerifier()
	if err != nil {
		t.Fatalf("generateVerifier: %v", err)
	}
	b, err := generateVerifier()
	if err != nil {
		t.Fatalf("generateVerifier: %v", err)
	}
	if len(a) < 43 || len(a) > 128 {
		t.Errorf("verifier length %d outside RFC 7636's [43,128]", len(a))
	}
	if a == b {
		t.Error("two generateVerifier calls returned the same value")
	}
}

// fakeTokenServer stands in for accounts.spotify.com/api/token.
func fakeTokenServer(t *testing.T, handle func(w http.ResponseWriter, form url.Values)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse token request form: %v", err)
		}
		handle(w, r.Form)
	}))
}

func TestStartLoginFullFlow(t *testing.T) {
	tokenSrv := fakeTokenServer(t, func(w http.ResponseWriter, form url.Values) {
		if form.Get("grant_type") != "authorization_code" {
			t.Errorf("grant_type = %q, want authorization_code", form.Get("grant_type"))
		}
		if form.Get("code") != "test-code" {
			t.Errorf("code = %q, want test-code", form.Get("code"))
		}
		if form.Get("code_verifier") == "" {
			t.Error("code_verifier missing from token exchange")
		}
		json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "at1",
			"refresh_token": "rt1",
			"expires_in":    3600,
		})
	})
	defer tokenSrv.Close()

	auth := NewAuth(nil)
	auth.TokenURL = tokenSrv.URL

	resultCh := make(chan struct {
		tokens Tokens
		err    error
	}, 1)
	authorizeURL, err := auth.StartLogin(context.Background(), "client-1", func(tokens Tokens, err error) {
		resultCh <- struct {
			tokens Tokens
			err    error
		}{tokens, err}
	})
	if err != nil {
		t.Fatalf("StartLogin: %v", err)
	}

	u, err := url.Parse(authorizeURL)
	if err != nil {
		t.Fatalf("parse authorize URL: %v", err)
	}
	q := u.Query()
	if q.Get("client_id") != "client-1" {
		t.Errorf("client_id = %q, want client-1", q.Get("client_id"))
	}
	if q.Get("code_challenge_method") != "S256" {
		t.Errorf("code_challenge_method = %q, want S256", q.Get("code_challenge_method"))
	}
	redirectURI := q.Get("redirect_uri")
	if !strings.HasPrefix(redirectURI, "http://127.0.0.1:") || !strings.HasSuffix(redirectURI, "/callback") {
		t.Errorf("redirect_uri = %q, want http://127.0.0.1:<port>/callback", redirectURI)
	}

	// Simulate the browser hitting the callback.
	callbackURL := redirectURI + "?" + url.Values{
		"code":  {"test-code"},
		"state": {q.Get("state")},
	}.Encode()
	resp, err := http.Get(callbackURL)
	if err != nil {
		t.Fatalf("GET callback: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("callback status = %d, want 200", resp.StatusCode)
	}

	select {
	case result := <-resultCh:
		if result.err != nil {
			t.Fatalf("onComplete err = %v", result.err)
		}
		if result.tokens.AccessToken != "at1" || result.tokens.RefreshToken != "rt1" {
			t.Errorf("tokens = %+v, want access=at1 refresh=rt1", result.tokens)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for onComplete")
	}
}

func TestStartLoginStateMismatch(t *testing.T) {
	auth := NewAuth(nil)
	resultCh := make(chan error, 1)
	authorizeURL, err := auth.StartLogin(context.Background(), "client-1", func(_ Tokens, err error) {
		resultCh <- err
	})
	if err != nil {
		t.Fatalf("StartLogin: %v", err)
	}
	u, _ := url.Parse(authorizeURL)
	redirectURI := u.Query().Get("redirect_uri")

	callbackURL := redirectURI + "?" + url.Values{"code": {"x"}, "state": {"wrong-state"}}.Encode()
	resp, err := http.Get(callbackURL)
	if err != nil {
		t.Fatalf("GET callback: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}

	select {
	case err := <-resultCh:
		if err == nil {
			t.Fatal("expected a state-mismatch error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for onComplete")
	}
}

func TestStartLoginErrorCallback(t *testing.T) {
	auth := NewAuth(nil)
	resultCh := make(chan error, 1)
	authorizeURL, err := auth.StartLogin(context.Background(), "client-1", func(_ Tokens, err error) {
		resultCh <- err
	})
	if err != nil {
		t.Fatalf("StartLogin: %v", err)
	}
	u, _ := url.Parse(authorizeURL)
	redirectURI := u.Query().Get("redirect_uri")

	callbackURL := redirectURI + "?" + url.Values{"error": {"access_denied"}, "state": {u.Query().Get("state")}}.Encode()
	resp, err := http.Get(callbackURL)
	if err != nil {
		t.Fatalf("GET callback: %v", err)
	}
	resp.Body.Close()

	select {
	case err := <-resultCh:
		if err == nil {
			t.Fatal("expected an error for error= callback")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for onComplete")
	}
}

func TestStartLoginSecondCallCancelsFirst(t *testing.T) {
	auth := NewAuth(nil)
	firstResult := make(chan error, 1)
	_, err := auth.StartLogin(context.Background(), "client-1", func(_ Tokens, err error) {
		firstResult <- err
	})
	if err != nil {
		t.Fatalf("StartLogin (first): %v", err)
	}

	secondResult := make(chan error, 1)
	if _, err := auth.StartLogin(context.Background(), "client-1", func(_ Tokens, err error) {
		secondResult <- err
	}); err != nil {
		t.Fatalf("StartLogin (second): %v", err)
	}

	select {
	case err := <-firstResult:
		if err == nil {
			t.Fatal("expected the first flow's onComplete to receive an error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for first onComplete")
	}
	_ = secondResult
}

func TestRefreshTokensInvalidGrant(t *testing.T) {
	tokenSrv := fakeTokenServer(t, func(w http.ResponseWriter, form url.Values) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]any{
			"error":             "invalid_grant",
			"error_description": "Refresh token revoked",
		})
	})
	defer tokenSrv.Close()

	auth := NewAuth(nil)
	auth.TokenURL = tokenSrv.URL

	_, err := auth.RefreshTokens(context.Background(), "client-1", "stale-refresh")
	if err == nil {
		t.Fatal("expected an error")
	}
	terr, ok := err.(*TokenError)
	if !ok {
		t.Fatalf("err = %#v, want *TokenError", err)
	}
	if terr.Code != "invalid_grant" {
		t.Errorf("Code = %q, want invalid_grant", terr.Code)
	}
}
