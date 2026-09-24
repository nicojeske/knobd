package spotify

import (
	"fmt"
	"regexp"
)

// idPattern matches a bare Spotify base62 ID -- what every endpoint's
// path segment actually wants.
var idPattern = regexp.MustCompile(`^[A-Za-z0-9]{15,30}$`)

// uriPattern extracts the ID from a spotify:<kind>:<id> URI.
var uriPattern = regexp.MustCompile(`^spotify:([a-z]+):([A-Za-z0-9]{15,30})$`)

// urlPattern extracts the kind and ID from an open.spotify.com URL,
// with or without a query string (e.g. a shared link's "?si=...").
var urlPattern = regexp.MustCompile(`open\.spotify\.com/(?:[a-z-]+/)?([a-z]+)/([A-Za-z0-9]{15,30})`)

// ParseID normalizes s -- a bare ID, a spotify:<kind>:<id> URI, or an
// open.spotify.com/<kind>/<id> URL (optionally with a locale segment or
// query string) -- into a bare ID, so config.json can hold whichever
// form a user pasted (model.SpotifyAddToPlaylistAction.PlaylistID etc.)
// and the client always sends what the REST API's paths expect.
//
// wantKind ("playlist", "track", ...) is checked when s carries an
// explicit kind (a URI or URL); a bare ID has no kind to check and is
// accepted as-is. An empty s is always rejected.
func ParseID(wantKind, s string) (string, error) {
	if s == "" {
		return "", fmt.Errorf("spotify: empty %s id", wantKind)
	}
	if m := uriPattern.FindStringSubmatch(s); m != nil {
		if m[1] != wantKind {
			return "", fmt.Errorf("spotify: %q is a %s uri, want %s", s, m[1], wantKind)
		}
		return m[2], nil
	}
	if m := urlPattern.FindStringSubmatch(s); m != nil {
		if m[1] != wantKind {
			return "", fmt.Errorf("spotify: %q is a %s url, want %s", s, m[1], wantKind)
		}
		return m[2], nil
	}
	if idPattern.MatchString(s) {
		return s, nil
	}
	return "", fmt.Errorf("spotify: %q is not a recognizable %s id/uri/url", s, wantKind)
}

// URI builds a spotify:<kind>:<id> URI from a bare ID, the form several
// endpoints (PUT/DELETE /me/library, POST /playlists/{id}/items) take
// per the February 2026 Web API changes.
func URI(kind, id string) string {
	return fmt.Sprintf("spotify:%s:%s", kind, id)
}
