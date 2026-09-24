package spotify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestServiceStatusUnconfiguredWithoutClientID(t *testing.T) {
	s := NewService(context.Background(), func() string { return "" }, NewFakeSecretStore(), ServiceOptions{})
	status := s.Status()
	if status.Configured {
		t.Error("Configured should be false with no Client ID")
	}
}

func TestServiceLoginFlow(t *testing.T) {
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/me" {
			json.NewEncoder(w).Encode(map[string]any{"id": "u1", "display_name": "Nico"})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer apiSrv.Close()

	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "at1",
			"refresh_token": "rt1",
			"expires_in":    3600,
		})
	}))
	defer tokenSrv.Close()

	// A canceled-on-cleanup context, not context.Background(): NewService
	// binds this to Login's OAuth flow (Auth.LoopbackPort is a fixed
	// port -- see its doc comment), which must be freed before another
	// test in this package tries to bind it too.
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	store := NewFakeSecretStore()
	changed := make(chan struct{}, 16)
	s := NewService(ctx, func() string { return "client-1" }, store, ServiceOptions{
		OnChange:   func() { changed <- struct{}{} },
		TokenURL:   tokenSrv.URL,
		APIBaseURL: apiSrv.URL,
	})

	authorizeURL, err := s.Login()
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	status := s.Status()
	if !status.LoginInProgress || status.LoginURL != authorizeURL {
		t.Errorf("status after Login = %+v", status)
	}

	u, _ := url.Parse(authorizeURL)
	redirectURI := u.Query().Get("redirect_uri")
	callbackURL := redirectURI + "?" + url.Values{
		"code": {"test-code"}, "state": {u.Query().Get("state")},
	}.Encode()

	resp, err := http.Get(callbackURL)
	if err != nil {
		t.Fatalf("GET callback: %v", err)
	}
	resp.Body.Close()

	waitForChange(t, changed)

	status = s.Status()
	if !status.Authorized {
		t.Errorf("status.Authorized = false after login, want true (status=%+v)", status)
	}
	if status.User != "Nico" {
		t.Errorf("status.User = %q, want Nico", status.User)
	}
	if status.LoginInProgress {
		t.Error("status.LoginInProgress should be false after completion")
	}

	if !store.tokenExists("client-1") {
		t.Error("refresh token not stored after login")
	}
}

func (f *FakeSecretStore) tokenExists(account string) bool {
	_, ok, _ := f.Get(account)
	return ok
}

func TestServiceLogout(t *testing.T) {
	store := NewFakeSecretStore()
	store.Set("client-1", "rt1")
	s := NewService(context.Background(), func() string { return "client-1" }, store, ServiceOptions{})

	if err := s.Logout(); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if store.tokenExists("client-1") {
		t.Error("refresh token still stored after Logout")
	}
	if s.Status().Authorized {
		t.Error("Authorized should be false after Logout")
	}
}

func TestServiceValidateStoredTokenSuccess(t *testing.T) {
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"id": "u1", "display_name": "Nico"})
	}))
	defer apiSrv.Close()

	store := NewFakeSecretStore()
	store.Set("client-1", "rt1")
	// Prime the in-memory access token directly (bypassing a live
	// token endpoint) so ValidateStoredToken's Me() call succeeds
	// without needing a token server too.
	changed := make(chan struct{}, 4)
	s := NewService(context.Background(), func() string { return "client-1" }, store, ServiceOptions{
		OnChange:   func() { changed <- struct{}{} },
		APIBaseURL: apiSrv.URL,
	})
	s.tokens.SetTokens("client-1", Tokens{AccessToken: "at1", ExpiresAt: time.Now().Add(time.Hour)})

	s.ValidateStoredToken()
	waitForChange(t, changed)

	status := s.Status()
	if !status.Authorized || status.User != "Nico" {
		t.Errorf("status = %+v", status)
	}
}

func waitForChange(t *testing.T, ch chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for OnChange")
	}
}
