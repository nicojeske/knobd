package spotify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newTestTokenManager(t *testing.T, tokenSrv *httptest.Server) (*TokenManager, *FakeSecretStore) {
	t.Helper()
	auth := NewAuth(nil)
	if tokenSrv != nil {
		auth.TokenURL = tokenSrv.URL
	}
	store := NewFakeSecretStore()
	return NewTokenManager(auth, store, nil), store
}

func TestTokenManagerAccessTokenNotAuthorized(t *testing.T) {
	tm, _ := newTestTokenManager(t, nil)
	_, err := tm.AccessToken(context.Background(), "client-1")
	if err != ErrNotAuthorized {
		t.Fatalf("err = %v, want ErrNotAuthorized", err)
	}
}

func TestTokenManagerRefreshesWhenExpired(t *testing.T) {
	calls := 0
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": "at-fresh",
			"expires_in":   3600,
		})
	}))
	defer tokenSrv.Close()

	tm, store := newTestTokenManager(t, tokenSrv)
	store.Set("client-1", "refresh-1")

	tok, err := tm.AccessToken(context.Background(), "client-1")
	if err != nil {
		t.Fatalf("AccessToken: %v", err)
	}
	if tok != "at-fresh" {
		t.Errorf("token = %q, want at-fresh", tok)
	}
	if calls != 1 {
		t.Errorf("token endpoint called %d times, want 1", calls)
	}

	// A second call within expiry should use the cached token, not
	// refresh again.
	if _, err := tm.AccessToken(context.Background(), "client-1"); err != nil {
		t.Fatalf("AccessToken (cached): %v", err)
	}
	if calls != 1 {
		t.Errorf("token endpoint called %d times after cached call, want 1", calls)
	}
}

func TestTokenManagerInvalidateForcesRefresh(t *testing.T) {
	calls := 0
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": "at-fresh",
			"expires_in":   3600,
		})
	}))
	defer tokenSrv.Close()

	tm, store := newTestTokenManager(t, tokenSrv)
	store.Set("client-1", "refresh-1")

	if _, err := tm.AccessToken(context.Background(), "client-1"); err != nil {
		t.Fatalf("AccessToken: %v", err)
	}
	tm.Invalidate("client-1")
	if _, err := tm.AccessToken(context.Background(), "client-1"); err != nil {
		t.Fatalf("AccessToken after invalidate: %v", err)
	}
	if calls != 2 {
		t.Errorf("token endpoint called %d times, want 2", calls)
	}
}

func TestTokenManagerInvalidGrantUnauthorizesAndDeletes(t *testing.T) {
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]any{"error": "invalid_grant"})
	}))
	defer tokenSrv.Close()

	tm, store := newTestTokenManager(t, tokenSrv)
	store.Set("client-1", "revoked-refresh")

	_, err := tm.AccessToken(context.Background(), "client-1")
	if err != ErrNotAuthorized {
		t.Fatalf("err = %v, want ErrNotAuthorized", err)
	}
	if _, ok, _ := store.Get("client-1"); ok {
		t.Error("revoked refresh token was not deleted from the store")
	}
}

func TestTokenManagerPersistsRotatedRefreshToken(t *testing.T) {
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "at1",
			"refresh_token": "rotated-refresh",
			"expires_in":    3600,
		})
	}))
	defer tokenSrv.Close()

	tm, store := newTestTokenManager(t, tokenSrv)
	store.Set("client-1", "original-refresh")

	if _, err := tm.AccessToken(context.Background(), "client-1"); err != nil {
		t.Fatalf("AccessToken: %v", err)
	}
	got, ok, _ := store.Get("client-1")
	if !ok || got != "rotated-refresh" {
		t.Errorf("stored refresh token = %q, ok=%v, want rotated-refresh", got, ok)
	}
}

func TestTokenManagerSetTokensPrimesCache(t *testing.T) {
	tm, _ := newTestTokenManager(t, nil)
	if err := tm.SetTokens("client-1", Tokens{
		AccessToken:  "at1",
		RefreshToken: "rt1",
		ExpiresAt:    time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("SetTokens: %v", err)
	}
	tok, err := tm.AccessToken(context.Background(), "client-1")
	if err != nil {
		t.Fatalf("AccessToken: %v", err)
	}
	if tok != "at1" {
		t.Errorf("token = %q, want at1", tok)
	}
}

func TestTokenManagerLogout(t *testing.T) {
	tm, store := newTestTokenManager(t, nil)
	store.Set("client-1", "rt1")
	if err := tm.Logout("client-1"); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if tm.Authorized("client-1") {
		t.Error("Authorized should be false after Logout")
	}
}
