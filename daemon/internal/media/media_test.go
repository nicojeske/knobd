package media

import (
	"testing"

	"github.com/godbus/dbus/v5"
)

func TestRefOf(t *testing.T) {
	cases := map[string]string{
		"org.mpris.MediaPlayer2.spotify":                    "spotify",
		"org.mpris.MediaPlayer2.brave.instance1918":         "brave",
		"org.mpris.MediaPlayer2.vlc.instance99999":          "vlc",
		"org.mpris.MediaPlayer2.plasma-browser-integration": "plasma-browser-integration",
		"org.mpris.MediaPlayer2.chromium.instance_1_2":      "chromium.instance_1_2", // not a bare digit suffix
	}
	for busName, want := range cases {
		if got := RefOf(busName); got != want {
			t.Errorf("RefOf(%q) = %q, want %q", busName, got, want)
		}
	}
}

func TestMatchesRef(t *testing.T) {
	cases := []struct {
		busName string
		ref     string
		want    bool
	}{
		{"org.mpris.MediaPlayer2.spotify", "spotify", true},
		{"org.mpris.MediaPlayer2.brave.instance1918", "brave", true},
		{"org.mpris.MediaPlayer2.brave.instance1918", "bra", false},
		{"org.mpris.MediaPlayer2.spotify", "", false},
		{"org.mpris.MediaPlayer2.vlc", "spotify", false},
	}
	for _, tc := range cases {
		if got := MatchesRef(tc.busName, tc.ref); got != tc.want {
			t.Errorf("MatchesRef(%q, %q) = %v, want %v", tc.busName, tc.ref, got, tc.want)
		}
	}
}

func TestDecodeMetadata(t *testing.T) {
	m := map[string]dbus.Variant{
		"mpris:trackid": dbus.MakeVariant(dbus.ObjectPath("/org/mpris/MediaPlayer2/Track/1")),
		"xesam:title":   dbus.MakeVariant("Song Title"),
		"xesam:artist":  dbus.MakeVariant([]string{"Artist One", "Artist Two"}),
		"xesam:album":   dbus.MakeVariant("Album Name"),
		"xesam:url":     dbus.MakeVariant("file:///song.flac"),
	}
	got := decodeMetadata(m)
	want := Track{
		ID:      "/org/mpris/MediaPlayer2/Track/1",
		Title:   "Song Title",
		Artists: []string{"Artist One", "Artist Two"},
		Album:   "Album Name",
		URL:     "file:///song.flac",
	}
	if got.ID != want.ID || got.Title != want.Title || got.Album != want.Album || got.URL != want.URL {
		t.Fatalf("decodeMetadata = %#v, want %#v", got, want)
	}
	if len(got.Artists) != 2 || got.Artists[0] != want.Artists[0] || got.Artists[1] != want.Artists[1] {
		t.Fatalf("decodeMetadata.Artists = %#v, want %#v", got.Artists, want.Artists)
	}
}

func TestDecodeMetadataIgnoresWrongTypes(t *testing.T) {
	// A malformed/unexpected-type value must be left at the zero value,
	// not cause a panic -- Metadata is a best-effort bag of hints, same
	// posture as audio.Stream/focus.AppInfo.
	m := map[string]dbus.Variant{
		"xesam:title":  dbus.MakeVariant(42),
		"xesam:artist": dbus.MakeVariant("not a list"),
	}
	got := decodeMetadata(m)
	if got.Title != "" || got.Artists != nil {
		t.Fatalf("decodeMetadata with wrong types = %#v, want zero value", got)
	}
}

func TestApplyPlayerPropertiesShuffleLoopStatusUnsupportedByDefault(t *testing.T) {
	var info PlayerInfo
	applyPlayerProperties(&info, map[string]dbus.Variant{
		"PlaybackStatus": dbus.MakeVariant("Playing"),
		"CanControl":     dbus.MakeVariant(true),
		"CanSeek":        dbus.MakeVariant(false),
	}, nil)
	if info.Shuffle != nil {
		t.Errorf("Shuffle = %v, want nil (unsupported)", *info.Shuffle)
	}
	if info.LoopStatus != "" {
		t.Errorf("LoopStatus = %q, want empty (unsupported)", info.LoopStatus)
	}
	if info.Status != StatusPlaying || !info.CanControl || info.CanSeek {
		t.Errorf("info = %#v", info)
	}
}

func TestApplyPlayerPropertiesInvalidatedClearsShuffleAndLoopStatus(t *testing.T) {
	f := true
	info := PlayerInfo{Shuffle: &f, LoopStatus: "Track"}
	applyPlayerProperties(&info, nil, []string{"Shuffle", "LoopStatus"})
	if info.Shuffle != nil {
		t.Errorf("Shuffle = %v, want nil after invalidation", *info.Shuffle)
	}
	if info.LoopStatus != "" {
		t.Errorf("LoopStatus = %q, want empty after invalidation", info.LoopStatus)
	}
}
