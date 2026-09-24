package spotify

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DefaultAuthorizeURL/DefaultTokenURL are Spotify's real OAuth
// endpoints. Auth.AuthorizeURL/TokenURL are overridable so tests can
// point them at an httptest.Server instead.
const (
	DefaultAuthorizeURL = "https://accounts.spotify.com/authorize"
	DefaultTokenURL     = "https://accounts.spotify.com/api/token"

	// scopes is the fixed set every login requests -- see
	// specs/milestones/M10-spotify-web-api.md's Design section for why
	// each one is needed (library read/modify for like_toggle,
	// playlist read/modify for add/remove/start_playlist, player
	// read/modify for queue_track/transfer_playback/like_toggle's
	// currently-playing lookup).
	scopes = "user-library-read user-library-modify " +
		"playlist-read-private playlist-read-collaborative playlist-modify-public playlist-modify-private " +
		"user-read-playback-state user-modify-playback-state user-read-currently-playing"

	loginTimeout = 5 * time.Minute
)

// Tokens is the result of a successful authorization-code or
// refresh-token exchange.
type Tokens struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
}

// Auth drives the PKCE authorization-code flow over a one-shot loopback
// HTTP listener (specs/adr's redirect-URI rules: 127.0.0.1, not
// "localhost", with no fixed port registered -- see
// specs/milestones/M10-spotify-web-api.md's Design refinements). Only
// one login flow runs at a time; starting a new one cancels whatever
// flow was in progress.
type Auth struct {
	AuthorizeURL string
	TokenURL     string
	HTTPClient   *http.Client
	Logger       *slog.Logger

	mu      sync.Mutex
	session *loginSession
}

type loginSession struct {
	listener net.Listener
	cancel   context.CancelFunc
}

// NewAuth returns an Auth pointed at the real Spotify endpoints with a
// default HTTP client. Override AuthorizeURL/TokenURL/HTTPClient after
// construction for tests.
func NewAuth(logger *slog.Logger) *Auth {
	if logger == nil {
		logger = slog.Default()
	}
	return &Auth{
		AuthorizeURL: DefaultAuthorizeURL,
		TokenURL:     DefaultTokenURL,
		HTTPClient:   &http.Client{Timeout: 15 * time.Second},
		Logger:       logger,
	}
}

// StartLogin binds a loopback listener, builds the authorize URL for
// clientID, and serves exactly one /callback request (or times out
// after loginTimeout). onComplete runs exactly once per StartLogin
// call, off the HTTP-handling goroutine, with either a populated Tokens
// or a non-nil err (context canceled/timed out, state mismatch, an
// error= callback param, or a failed code exchange). Calling StartLogin
// again before a prior flow's onComplete has fired cancels that flow --
// its onComplete still runs, with a context.Canceled err -- before
// starting the new one.
func (a *Auth) StartLogin(ctx context.Context, clientID string, onComplete func(Tokens, error)) (authorizeURL string, err error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("spotify: bind loopback listener: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/callback", port)

	verifier, err := generateVerifier()
	if err != nil {
		listener.Close()
		return "", fmt.Errorf("spotify: generate PKCE verifier: %w", err)
	}
	state, err := generateVerifier()
	if err != nil {
		listener.Close()
		return "", fmt.Errorf("spotify: generate state: %w", err)
	}

	loginCtx, cancel := context.WithTimeout(ctx, loginTimeout)

	a.mu.Lock()
	if a.session != nil {
		// cancel() alone is enough: it trips the prior StartLogin
		// call's own watcher goroutine, which shuts its server down
		// gracefully (see below) -- closing the listener again here
		// too would race that shutdown.
		a.session.cancel()
	}
	a.session = &loginSession{listener: listener, cancel: cancel}
	a.mu.Unlock()

	mux := http.NewServeMux()
	var once sync.Once
	finish := func(tokens Tokens, ferr error) {
		once.Do(func() {
			cancel()
			a.mu.Lock()
			if a.session != nil && a.session.listener == listener {
				a.session = nil
			}
			a.mu.Unlock()
			onComplete(tokens, ferr)
		})
	}

	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if errParam := q.Get("error"); errParam != "" {
			writeCallbackPage(w, false)
			finish(Tokens{}, fmt.Errorf("spotify: authorization denied: %s", errParam))
			return
		}
		if q.Get("state") != state {
			writeCallbackPage(w, false)
			finish(Tokens{}, fmt.Errorf("spotify: callback state mismatch"))
			return
		}
		code := q.Get("code")
		if code == "" {
			writeCallbackPage(w, false)
			finish(Tokens{}, fmt.Errorf("spotify: callback missing code"))
			return
		}
		tokens, err := a.exchangeCode(r.Context(), clientID, code, redirectURI, verifier)
		writeCallbackPage(w, err == nil)
		finish(tokens, err)
	})

	server := &http.Server{Handler: mux}
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			a.Logger.Warn("spotify: loopback callback server exited", "err", err)
		}
	}()
	go func() {
		<-loginCtx.Done()
		// Shutdown (not Close): the callback handler may still be
		// flushing its response to the browser when this fires --
		// finish() cancels loginCtx from inside that same handler on
		// the success path, so an abrupt Close() here would race a
		// still-in-flight write and the browser would see a reset
		// connection instead of the "you can close this tab" page.
		// Shutdown waits for the in-flight request to finish first.
		go server.Shutdown(context.Background())
		finish(Tokens{}, loginCtx.Err())
	}()

	authorizeURL = a.buildAuthorizeURL(clientID, redirectURI, state, verifier)
	return authorizeURL, nil
}

func (a *Auth) buildAuthorizeURL(clientID, redirectURI, state, verifier string) string {
	v := url.Values{}
	v.Set("client_id", clientID)
	v.Set("response_type", "code")
	v.Set("redirect_uri", redirectURI)
	v.Set("code_challenge_method", "S256")
	v.Set("code_challenge", challengeFromVerifier(verifier))
	v.Set("state", state)
	v.Set("scope", scopes)
	return a.AuthorizeURL + "?" + v.Encode()
}

// exchangeCode performs the authorization_code grant -- the same POST
// shape RefreshTokens (token.go) uses for the refresh_token grant.
func (a *Auth) exchangeCode(ctx context.Context, clientID, code, redirectURI, verifier string) (Tokens, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	form.Set("client_id", clientID)
	form.Set("code_verifier", verifier)
	return a.postToken(ctx, form)
}

// RefreshTokens exchanges refreshToken for a new access token (and,
// possibly, a rotated refresh token -- see Tokens.RefreshToken's use in
// token.go).
func (a *Auth) RefreshTokens(ctx context.Context, clientID, refreshToken string) (Tokens, error) {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	form.Set("client_id", clientID)
	return a.postToken(ctx, form)
}

// tokenErrorResponse is Spotify's token-endpoint error body shape,
// e.g. {"error":"invalid_grant","error_description":"..."}.
type tokenErrorResponse struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

// TokenError wraps a token-endpoint error response so callers (notably
// token.go, which treats "invalid_grant" specially) can check Code
// without string-matching Error().
type TokenError struct {
	Code        string
	Description string
}

func (e *TokenError) Error() string {
	if e.Description != "" {
		return fmt.Sprintf("spotify: token error %s: %s", e.Code, e.Description)
	}
	return fmt.Sprintf("spotify: token error %s", e.Code)
}

func (a *Auth) postToken(ctx context.Context, form url.Values) (Tokens, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return Tokens{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return Tokens{}, fmt.Errorf("spotify: token request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var terr tokenErrorResponse
		_ = json.NewDecoder(resp.Body).Decode(&terr)
		if terr.Error == "" {
			terr.Error = strconv.Itoa(resp.StatusCode)
		}
		return Tokens{}, &TokenError{Code: terr.Error, Description: terr.ErrorDescription}
	}

	var body struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return Tokens{}, fmt.Errorf("spotify: decode token response: %w", err)
	}
	return Tokens{
		AccessToken:  body.AccessToken,
		RefreshToken: body.RefreshToken, // empty unless Spotify rotated it
		ExpiresAt:    time.Now().Add(time.Duration(body.ExpiresIn) * time.Second),
	}, nil
}

// generateVerifier returns a cryptographically random PKCE code
// verifier: 64 random bytes, base64url-encoded without padding (86
// characters -- within RFC 7636's required 43-128 range).
func generateVerifier() (string, error) {
	b := make([]byte, 64)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// challengeFromVerifier computes the S256 code_challenge for verifier
// per RFC 7636 §4.2.
func challengeFromVerifier(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// writeCallbackPage renders the minimal page shown in the browser after
// the loopback callback -- ok controls whether it reports success.
func writeCallbackPage(w http.ResponseWriter, ok bool) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if ok {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `<!doctype html><html><body><p>knobd is connected to Spotify. You can close this tab.</p></body></html>`)
		return
	}
	w.WriteHeader(http.StatusBadRequest)
	fmt.Fprint(w, `<!doctype html><html><body><p>Connecting knobd to Spotify failed. You can close this tab and try again.</p></body></html>`)
}
