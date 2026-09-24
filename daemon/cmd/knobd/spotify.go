package main

import (
	"context"
	"errors"
	"log/slog"
	"os/exec"

	"github.com/njeske/knobd/internal/api"
	"github.com/njeske/knobd/internal/spotify"
)

// spotifyService is the slice of *spotify.Service spotifyProvider needs
// -- point-of-use, matching every other adapter in this package.
type spotifyService interface {
	Login(ctx context.Context) (string, error)
	Logout() error
	Status() spotify.Status
	Client() *spotify.Client
}

// spotifyProvider is cmd/knobd's api.SpotifyProvider adapter: it
// translates spotify.ErrUnavailable/ErrNotAuthorized to the api package's
// own sentinels (api never imports daemon/internal/spotify -- see
// api.Server's package doc comment) and, on a successful Login, makes a
// best-effort attempt to open the authorize URL in a browser so the
// user isn't left to copy-paste it (the UI shows it too, as a
// fallback -- see api.SpotifyState.LoginURL).
type spotifyProvider struct {
	svc    spotifyService
	logger *slog.Logger
	// open is xdg-open by default; overridden in tests so they don't
	// actually spawn a browser.
	open func(url string)
}

func newSpotifyProvider(svc spotifyService, logger *slog.Logger) *spotifyProvider {
	if logger == nil {
		logger = slog.Default()
	}
	p := &spotifyProvider{svc: svc, logger: logger}
	p.open = p.xdgOpen
	return p
}

func (p *spotifyProvider) xdgOpen(url string) {
	if err := exec.Command("xdg-open", url).Start(); err != nil {
		p.logger.Warn("spotify: could not open a browser for the authorize URL; use the URL shown in the Spotify tab instead", "err", err)
	}
}

// Login implements api.SpotifyProvider.
func (p *spotifyProvider) Login(ctx context.Context) (string, error) {
	url, err := p.svc.Login(ctx)
	if err != nil {
		if errors.Is(err, spotify.ErrUnavailable) {
			return "", api.ErrSpotifyNotConfigured
		}
		return "", err
	}
	p.open(url)
	return url, nil
}

// Logout implements api.SpotifyProvider.
func (p *spotifyProvider) Logout() error {
	if err := p.svc.Logout(); err != nil {
		if errors.Is(err, spotify.ErrUnavailable) {
			return api.ErrSpotifyNotConfigured
		}
		return err
	}
	return nil
}

// Playlists implements api.SpotifyProvider.
func (p *spotifyProvider) Playlists(ctx context.Context) (api.SpotifyPlaylistList, error) {
	if !p.svc.Status().Authorized {
		return nil, api.ErrSpotifyNotAuthorized
	}
	playlists, err := p.svc.Client().Playlists(ctx)
	if err != nil {
		return nil, translateSpotifyClientErr(err)
	}
	out := make(api.SpotifyPlaylistList, len(playlists))
	for i, pl := range playlists {
		out[i] = api.SpotifyPlaylist{ID: pl.ID, Name: pl.Name, Owner: pl.Owner, Editable: pl.Editable}
	}
	return out, nil
}

// Devices implements api.SpotifyProvider.
func (p *spotifyProvider) Devices(ctx context.Context) (api.SpotifyDeviceList, error) {
	if !p.svc.Status().Authorized {
		return nil, api.ErrSpotifyNotAuthorized
	}
	devices, err := p.svc.Client().Devices(ctx)
	if err != nil {
		return nil, translateSpotifyClientErr(err)
	}
	out := make(api.SpotifyDeviceList, len(devices))
	for i, d := range devices {
		out[i] = api.SpotifyDevice{ID: d.ID, Name: d.Name, Type: d.Type, Active: d.IsActive}
	}
	return out, nil
}

func translateSpotifyClientErr(err error) error {
	if errors.Is(err, spotify.ErrNotAuthorized) {
		return api.ErrSpotifyNotAuthorized
	}
	return err
}

var _ api.SpotifyProvider = (*spotifyProvider)(nil)
