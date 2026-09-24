package spotify

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// tokenSkew is how far ahead of an access token's real expiry
// TokenManager treats it as already expired, so a call that starts
// mid-flight doesn't race the token dying underneath it.
const tokenSkew = 30 * time.Second

// TokenManager keeps one account's access token in memory (never
// persisted -- see the package doc comment) and refreshes it from the
// SecretStore-backed refresh token, transparently, ahead of expiry or
// on demand after a live call's 401. account is always the current
// Spotify Client ID (Service.clientID()) -- switching Client ID in
// config means a different SecretStore entry and a distinct in-memory
// cache slot, so a stale token under the old identity is never reused.
type TokenManager struct {
	auth   *Auth
	store  SecretStore
	logger *slog.Logger

	mu          sync.Mutex
	account     string
	accessToken string
	expiresAt   time.Time
}

// NewTokenManager builds a TokenManager. logger may be nil (slog.Default()).
func NewTokenManager(auth *Auth, store SecretStore, logger *slog.Logger) *TokenManager {
	if logger == nil {
		logger = slog.Default()
	}
	return &TokenManager{auth: auth, store: store, logger: logger}
}

// Authorized reports whether a refresh token is currently stored for
// account, without making a network call.
func (t *TokenManager) Authorized(account string) bool {
	if account == "" {
		return false
	}
	_, ok, err := t.store.Get(account)
	if err != nil {
		t.logger.Warn("spotify: check stored refresh token", "err", err)
		return false
	}
	return ok
}

// SetTokens stores tokens.RefreshToken for account (the result of a
// completed StartLogin) and primes the in-memory access token so the
// very first API call after login doesn't need an extra refresh
// round-trip.
func (t *TokenManager) SetTokens(account string, tokens Tokens) error {
	if err := t.store.Set(account, tokens.RefreshToken); err != nil {
		return fmt.Errorf("spotify: store refresh token: %w", err)
	}
	t.mu.Lock()
	t.account = account
	t.accessToken = tokens.AccessToken
	t.expiresAt = tokens.ExpiresAt
	t.mu.Unlock()
	return nil
}

// AccessToken returns a currently-valid access token for account,
// refreshing via the stored refresh token if the cached one is
// missing, near expiry, or cached for a different account. Returns
// ErrNotAuthorized if no refresh token is stored, or if Spotify
// reports the stored one was revoked (invalid_grant).
func (t *TokenManager) AccessToken(ctx context.Context, account string) (string, error) {
	t.mu.Lock()
	if t.account == account && t.accessToken != "" && time.Until(t.expiresAt) > tokenSkew {
		tok := t.accessToken
		t.mu.Unlock()
		return tok, nil
	}
	t.mu.Unlock()
	return t.refresh(ctx, account)
}

// Invalidate drops the cached access token for account, forcing the
// next AccessToken call to refresh -- used after a live API call gets a
// 401 despite the cached token appearing unexpired (clock skew, or
// Spotify revoking early).
func (t *TokenManager) Invalidate(account string) {
	t.mu.Lock()
	if t.account == account {
		t.accessToken = ""
	}
	t.mu.Unlock()
}

// Logout deletes the stored refresh token and clears the in-memory
// access token for account.
func (t *TokenManager) Logout(account string) error {
	t.mu.Lock()
	if t.account == account {
		t.accessToken, t.expiresAt = "", time.Time{}
	}
	t.mu.Unlock()
	return t.store.Delete(account)
}

func (t *TokenManager) refresh(ctx context.Context, account string) (string, error) {
	refreshToken, ok, err := t.store.Get(account)
	if err != nil {
		return "", fmt.Errorf("spotify: read stored refresh token: %w", err)
	}
	if !ok {
		return "", ErrNotAuthorized
	}

	tokens, err := t.auth.RefreshTokens(ctx, account, refreshToken)
	if err != nil {
		var terr *TokenError
		if errors.As(err, &terr) && terr.Code == "invalid_grant" {
			// The refresh token was revoked (user disconnected the app
			// in their Spotify account, or it simply expired from
			// disuse) -- there is nothing left to retry with. Drop it
			// so the UI shows "Connect" again instead of a repeating
			// error.
			if delErr := t.store.Delete(account); delErr != nil {
				t.logger.Warn("spotify: delete revoked refresh token", "err", delErr)
			}
			t.mu.Lock()
			if t.account == account {
				t.accessToken, t.expiresAt = "", time.Time{}
			}
			t.mu.Unlock()
			return "", ErrNotAuthorized
		}
		return "", fmt.Errorf("spotify: refresh access token: %w", err)
	}

	if tokens.RefreshToken != "" {
		// Spotify rotated the refresh token; persist the new one or a
		// future refresh will fail against the now-stale stored value.
		if err := t.store.Set(account, tokens.RefreshToken); err != nil {
			t.logger.Warn("spotify: persist rotated refresh token", "err", err)
		}
	}

	t.mu.Lock()
	t.account = account
	t.accessToken = tokens.AccessToken
	t.expiresAt = tokens.ExpiresAt
	t.mu.Unlock()
	return tokens.AccessToken, nil
}
