package spotify

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Status is a point-in-time snapshot of Service's connection state, for
// api.State.Spotify (see cmd/knobd's adapter).
type Status struct {
	// Configured is true once Config.Spotify.ClientID is non-empty.
	Configured bool
	// Authorized is true once a refresh token is stored and was last
	// confirmed good (or has never been checked and failed).
	Authorized bool
	// LoginInProgress is true between Login() returning an authorize
	// URL and its callback completing.
	LoginInProgress bool
	// LoginURL is the most recent authorize URL from an in-progress
	// login, for the UI's copyable fallback if the daemon's best-effort
	// xdg-open didn't work.
	LoginURL string
	// User is the authorized account's display name (or id, if no
	// display name is set), empty until confirmed.
	User string
	// LastError is the most recent login/refresh/validation failure's
	// message, cleared on the next successful one. Empty means no
	// error to report.
	LastError string
}

// ServiceOptions configures Service. The zero value is sane defaults.
type ServiceOptions struct {
	Logger *slog.Logger
	// OnChange is called (never with Service's own lock held) whenever
	// Status() would return something new -- wired to hub.NotifyStateDirty
	// in cmd/knobd, the same pattern media.TrackerOptions.OnChange uses.
	OnChange func()
	// AuthorizeURL/TokenURL/APIBaseURL override the real Spotify
	// endpoints, for tests.
	AuthorizeURL string
	TokenURL     string
	APIBaseURL   string
}

// Service ties together Auth, TokenManager, and Client behind the
// current Config.Spotify.ClientID (read fresh via clientID on every
// call, the same live-config-read pattern actions.MediaOptions.
// IgnorePlayers uses -- so an edit through the UI's Spotify tab applies
// immediately with no separate push path).
type Service struct {
	clientID func() string
	auth     *Auth
	tokens   *TokenManager
	client   *Client
	logger   *slog.Logger
	onChange func()

	mu     sync.Mutex
	status Status
}

// NewService builds a Service. clientID is read fresh on every call
// (Config.Spotify.ClientID, via cmd/knobd's configStore, the same shape
// media's ignorePlayers uses). store is where the refresh token lives
// -- a *dbusSecretStore (NewSecretService) in production, a
// FakeSecretStore in tests.
func NewService(clientID func() string, store SecretStore, opts ServiceOptions) *Service {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	auth := NewAuth(logger)
	if opts.AuthorizeURL != "" {
		auth.AuthorizeURL = opts.AuthorizeURL
	}
	if opts.TokenURL != "" {
		auth.TokenURL = opts.TokenURL
	}
	tokens := NewTokenManager(auth, store, logger)
	client := NewClient(tokens, clientID)
	if opts.APIBaseURL != "" {
		client.BaseURL = opts.APIBaseURL
	}

	s := &Service{
		clientID: clientID,
		auth:     auth,
		tokens:   tokens,
		client:   client,
		logger:   logger,
		onChange: opts.OnChange,
	}
	return s
}

// Client returns the underlying *Client for actions.SpotifyHandlers
// (via the actions.SpotifyAPI point-of-use interface) to issue API
// calls through.
func (s *Service) Client() *Client { return s.client }

// ValidateStoredToken checks, in the background, whether a refresh
// token stored under the current Client ID is still good, filling in
// Status.User/Authorized once it answers. Called once at daemon
// startup (cmd/knobd's wiring) so a restart doesn't show "Connect"
// while a perfectly good token is sitting in the Secret Service --
// mirrors media.NewTracker's best-effort, never-blocks-startup
// posture. A missing Client ID or refresh token is not an error, just
// "not connected yet."
func (s *Service) ValidateStoredToken(ctx context.Context) {
	account := s.clientID()
	if account == "" || !s.tokens.Authorized(account) {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		user, err := s.client.Me(ctx)
		s.mu.Lock()
		if err != nil {
			s.status.Authorized = false
			s.status.LastError = err.Error()
		} else {
			s.status.Authorized = true
			s.status.User = displayName(user)
			s.status.LastError = ""
		}
		s.mu.Unlock()
		s.notify()
	}()
}

// Status returns a snapshot of the current connection state.
func (s *Service) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	status := s.status
	status.Configured = s.clientID() != ""
	return status
}

// Login starts a fresh OAuth flow for the current Client ID, returning
// the authorize URL for cmd/knobd's API handler to both try to
// xdg-open and hand back to the UI as a fallback. Returns
// ErrUnavailable if no Client ID is configured.
func (s *Service) Login(ctx context.Context) (string, error) {
	account := s.clientID()
	if account == "" {
		return "", ErrUnavailable
	}

	authorizeURL, err := s.auth.StartLogin(ctx, account, func(tokens Tokens, err error) {
		s.mu.Lock()
		s.status.LoginInProgress = false
		s.status.LoginURL = ""
		if err != nil {
			s.status.LastError = err.Error()
			s.mu.Unlock()
			s.notify()
			return
		}
		s.mu.Unlock()

		if setErr := s.tokens.SetTokens(account, tokens); setErr != nil {
			s.mu.Lock()
			s.status.LastError = setErr.Error()
			s.mu.Unlock()
			s.notify()
			return
		}

		user, meErr := s.client.Me(context.Background())
		s.mu.Lock()
		s.status.Authorized = true
		s.status.LastError = ""
		if meErr == nil {
			s.status.User = displayName(user)
		}
		s.mu.Unlock()
		s.notify()
	})
	if err != nil {
		return "", err
	}

	s.mu.Lock()
	s.status.LoginInProgress = true
	s.status.LoginURL = authorizeURL
	s.status.LastError = ""
	s.mu.Unlock()
	s.notify()

	return authorizeURL, nil
}

// Logout deletes the stored refresh token for the current Client ID and
// clears Status.
func (s *Service) Logout() error {
	account := s.clientID()
	if account == "" {
		return ErrUnavailable
	}
	err := s.tokens.Logout(account)
	s.mu.Lock()
	s.status = Status{}
	s.mu.Unlock()
	s.notify()
	if err != nil {
		return err
	}
	return nil
}

func (s *Service) notify() {
	if s.onChange != nil {
		s.onChange()
	}
}

func displayName(u User) string {
	if u.DisplayName != "" {
		return u.DisplayName
	}
	return u.ID
}
