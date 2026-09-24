// Package spotify implements the Spotify Web API actions from
// specs/milestones/M10-spotify-web-api.md: OAuth 2.0 PKCE over a
// loopback redirect, refresh-token storage via the Secret Service
// D-Bus API, and thin REST client methods for the handful of endpoints
// actions.SpotifyHandlers needs.
//
// The refresh token is the only long-lived secret this package handles;
// the access token Token derives from it lives in memory only (see
// token.go). SecretStore is a small interface at the point of use (this
// file) over org.freedesktop.Secret.Service, the standard Secret
// Service D-Bus API -- KWallet (via ksecretd's
// org.kde.secretservicecompat) and gnome-keyring-daemon both implement
// it, so no vendor-specific code is needed. See fake.go for the
// FakeSecretStore used by tests.
package spotify

import (
	"context"
	"fmt"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	secretsBusName  = "org.freedesktop.secrets"
	secretsBasePath = dbus.ObjectPath("/org/freedesktop/secrets")
	serviceIface    = "org.freedesktop.Secret.Service"
	collectionIface = "org.freedesktop.Secret.Collection"
	itemIface       = "org.freedesktop.Secret.Item"
	promptIface     = "org.freedesktop.Secret.Prompt"

	// secretApplication/secretService tag every item this package
	// creates, so SearchItems only ever matches knobd's own Spotify
	// refresh tokens -- never another application's secrets that
	// happen to share a collection.
	secretApplication = "knobd"
	secretService     = "spotify"

	promptTimeout = 30 * time.Second
)

// SecretStore persists one string secret per account (here: per Spotify
// Client ID, so switching Client ID in config cleanly starts a fresh
// login rather than reusing a stale token under a different app
// identity). account is opaque to the store.
type SecretStore interface {
	Get(account string) (token string, ok bool, err error)
	Set(account, token string) error
	Delete(account string) error
}

// secretValue is the (session, parameters, value, content_type) struct
// org.freedesktop.Secret.Service.GetSecrets/Item.GetSecret use, decoded
// via dbus.Store's struct support.
type secretValue struct {
	Session     dbus.ObjectPath
	Parameters  []byte
	Value       []byte
	ContentType string
}

// dbusSecretStore is the real Secret Service-backed SecretStore.
type dbusSecretStore struct {
	conn    *dbus.Conn
	session dbus.ObjectPath
}

// NewSecretService opens a "plain" Secret Service session over conn.
// Plain (unencrypted) transport is acceptable here: the session bus
// itself is already restricted to the user's own processes by the
// kernel (AF_UNIX with peer credential checks) the same way every other
// local D-Bus call this daemon makes is, so the extra Diffie-Hellman
// negotiation the "dh-ietf1024-sha256-aes128-cbc-pkcs7" algorithm adds
// buys nothing over an already-private transport, at the cost of
// needing a crypto dependency this project's no-CGo, minimal-deps
// posture (ADR 0001, go.mod) doesn't otherwise need.
func NewSecretService(conn *dbus.Conn) (SecretStore, error) {
	svc := conn.Object(secretsBusName, secretsBasePath)
	var (
		output  dbus.Variant
		session dbus.ObjectPath
	)
	if err := svc.Call(serviceIface+".OpenSession", 0, "plain", dbus.MakeVariant("")).Store(&output, &session); err != nil {
		return nil, fmt.Errorf("spotify: open secret service session: %w", err)
	}
	return &dbusSecretStore{conn: conn, session: session}, nil
}

func (s *dbusSecretStore) attributes(account string) map[string]string {
	return map[string]string{
		"application": secretApplication,
		"service":     secretService,
		"account":     account,
	}
}

// findItem returns the object path of account's item, if any exists
// (locked or not).
func (s *dbusSecretStore) findItem(account string) (dbus.ObjectPath, bool, error) {
	svc := s.conn.Object(secretsBusName, secretsBasePath)
	var unlocked, locked []dbus.ObjectPath
	if err := svc.Call(serviceIface+".SearchItems", 0, s.attributes(account)).Store(&unlocked, &locked); err != nil {
		return "", false, fmt.Errorf("spotify: search secret items: %w", err)
	}
	all := append(unlocked, locked...)
	if len(all) == 0 {
		return "", false, nil
	}
	return all[0], true, nil
}

// unlock unlocks path if it's locked, prompting the user (KWallet's or
// gnome-keyring's own unlock dialog, out of this process's control) if
// needed, and blocks until that prompt completes or promptTimeout
// elapses.
func (s *dbusSecretStore) unlock(path dbus.ObjectPath) error {
	svc := s.conn.Object(secretsBusName, secretsBasePath)
	var unlocked []dbus.ObjectPath
	var prompt dbus.ObjectPath
	if err := svc.Call(serviceIface+".Unlock", 0, []dbus.ObjectPath{path}).Store(&unlocked, &prompt); err != nil {
		return fmt.Errorf("spotify: unlock secret: %w", err)
	}
	if prompt == "" || prompt == "/" {
		return nil // already unlocked, no prompt needed
	}
	return s.runPrompt(prompt)
}

// runPrompt drives a Secret Service Prompt object to completion: calls
// Prompt() then waits for the Completed signal (or promptTimeout).
func (s *dbusSecretStore) runPrompt(path dbus.ObjectPath) error {
	ctx, cancel := context.WithTimeout(context.Background(), promptTimeout)
	defer cancel()

	if err := s.conn.AddMatchSignalContext(ctx,
		dbus.WithMatchObjectPath(path),
		dbus.WithMatchInterface(promptIface),
		dbus.WithMatchMember("Completed"),
	); err != nil {
		return fmt.Errorf("spotify: watch prompt: %w", err)
	}
	defer s.conn.RemoveMatchSignalContext(context.Background(),
		dbus.WithMatchObjectPath(path),
		dbus.WithMatchInterface(promptIface),
		dbus.WithMatchMember("Completed"),
	)

	signals := make(chan *dbus.Signal, 1)
	s.conn.Signal(signals)
	defer s.conn.RemoveSignal(signals)

	obj := s.conn.Object(secretsBusName, path)
	if call := obj.Call(promptIface+".Prompt", 0, ""); call.Err != nil {
		return fmt.Errorf("spotify: start prompt: %w", call.Err)
	}

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("spotify: prompt timed out waiting for the user")
		case sig := <-signals:
			if sig.Path != path || sig.Name != promptIface+".Completed" {
				continue
			}
			var dismissed bool
			if len(sig.Body) > 0 {
				dismissed, _ = sig.Body[0].(bool)
			}
			if dismissed {
				return fmt.Errorf("spotify: unlock prompt dismissed")
			}
			return nil
		}
	}
}

func (s *dbusSecretStore) Get(account string) (string, bool, error) {
	path, ok, err := s.findItem(account)
	if err != nil || !ok {
		return "", false, err
	}
	obj := s.conn.Object(secretsBusName, path)

	var lockedVariant dbus.Variant
	if err := obj.Call("org.freedesktop.DBus.Properties.Get", 0, itemIface, "Locked").Store(&lockedVariant); err == nil {
		if locked, ok := lockedVariant.Value().(bool); ok && locked {
			if err := s.unlock(path); err != nil {
				return "", false, err
			}
		}
	}

	var sv secretValue
	if err := obj.Call(itemIface+".GetSecret", 0, s.session).Store(&sv); err != nil {
		return "", false, fmt.Errorf("spotify: read secret: %w", err)
	}
	return string(sv.Value), true, nil
}

func (s *dbusSecretStore) Set(account, token string) error {
	collection, err := s.defaultCollection()
	if err != nil {
		return err
	}

	// Replace any existing item for this account in one CreateItem call
	// (replace=true matches on the same attributes) rather than
	// delete-then-create, so a crash between the two can't leave the
	// account with no stored token at all.
	props := map[string]dbus.Variant{
		"org.freedesktop.Secret.Item.Label":      dbus.MakeVariant("knobd Spotify refresh token"),
		"org.freedesktop.Secret.Item.Attributes": dbus.MakeVariant(s.attributes(account)),
	}
	sv := secretValue{Session: s.session, Value: []byte(token), ContentType: "text/plain"}

	obj := s.conn.Object(secretsBusName, collection)
	var item dbus.ObjectPath
	var prompt dbus.ObjectPath
	if err := obj.Call(collectionIface+".CreateItem", 0, props, sv, true).Store(&item, &prompt); err != nil {
		return fmt.Errorf("spotify: store secret: %w", err)
	}
	if prompt != "" && prompt != "/" {
		return s.runPrompt(prompt)
	}
	return nil
}

func (s *dbusSecretStore) Delete(account string) error {
	path, ok, err := s.findItem(account)
	if err != nil || !ok {
		return err
	}
	obj := s.conn.Object(secretsBusName, path)
	var prompt dbus.ObjectPath
	if err := obj.Call(itemIface+".Delete", 0).Store(&prompt); err != nil {
		return fmt.Errorf("spotify: delete secret: %w", err)
	}
	if prompt != "" && prompt != "/" {
		return s.runPrompt(prompt)
	}
	return nil
}

// unavailableSecretStore is used when no D-Bus session bus was
// available at startup, mirroring media.Unavailable()'s posture: every
// spotify.* action reports ErrUnavailable rather than the daemon
// failing to start.
type unavailableSecretStore struct{}

func (unavailableSecretStore) Get(account string) (string, bool, error) {
	return "", false, ErrUnavailable
}
func (unavailableSecretStore) Set(account, token string) error { return ErrUnavailable }
func (unavailableSecretStore) Delete(account string) error     { return ErrUnavailable }

// UnavailableSecretStore returns a SecretStore that reports every
// operation as unavailable -- for cmd/knobd's wiring when no D-Bus
// session bus could be dialed at startup.
func UnavailableSecretStore() SecretStore { return unavailableSecretStore{} }

// defaultCollection resolves the collection new items are created in:
// the "default" alias if the backend defines one (both KWallet's and
// gnome-keyring's Secret Service implementations do), falling back to
// the well-known "/org/freedesktop/secrets/collection/login" path.
func (s *dbusSecretStore) defaultCollection() (dbus.ObjectPath, error) {
	svc := s.conn.Object(secretsBusName, secretsBasePath)
	var path dbus.ObjectPath
	if err := svc.Call(serviceIface+".ReadAlias", 0, "default").Store(&path); err == nil && path != "" && path != "/" {
		return path, nil
	}
	return secretsBasePath + "/collection/login", nil
}
