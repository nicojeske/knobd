package spotify

import "testing"

func TestParseID(t *testing.T) {
	const bareID = "37i9dQZF1DXcBWIGoYBM5M"
	cases := []struct {
		name    string
		kind    string
		input   string
		want    string
		wantErr bool
	}{
		{"bare id", "playlist", bareID, bareID, false},
		{"uri", "playlist", "spotify:playlist:" + bareID, bareID, false},
		{"url", "playlist", "https://open.spotify.com/playlist/" + bareID + "?si=abc123", bareID, false},
		{"url with locale", "track", "https://open.spotify.com/intl-de/track/" + bareID, bareID, false},
		{"wrong kind uri", "track", "spotify:playlist:" + bareID, "", true},
		{"wrong kind url", "track", "https://open.spotify.com/playlist/" + bareID, "", true},
		{"empty", "playlist", "", "", true},
		{"garbage", "playlist", "not an id", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseID(tc.kind, tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseID(%q, %q) = %q, want error", tc.kind, tc.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseID(%q, %q): %v", tc.kind, tc.input, err)
			}
			if got != tc.want {
				t.Errorf("ParseID(%q, %q) = %q, want %q", tc.kind, tc.input, got, tc.want)
			}
		})
	}
}

func TestURI(t *testing.T) {
	if got, want := URI("track", "abc123"), "spotify:track:abc123"; got != want {
		t.Errorf("URI() = %q, want %q", got, want)
	}
}
