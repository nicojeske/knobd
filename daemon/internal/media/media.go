// Package media discovers and controls MPRIS media players over D-Bus
// (org.mpris.MediaPlayer2.* bus names, https://specifications.freedesktop.org/mpris-spec/latest/),
// for model.ActionMediaTransport/ActionMediaSeek/ActionMediaTargetCycle/
// ActionMediaNowPlaying. See specs/milestones/M09-media-transport-mpris.md.
//
// Confirmed live during M09 planning: a single physical browser tab can
// appear under two different bus names at once --
// "org.mpris.MediaPlayer2.brave.instance1918" (the browser's own,
// limited MPRIS implementation: no LoopStatus, no Shuffle, CanSeek
// false) and "org.mpris.MediaPlayer2.plasma-browser-integration" (KDE's
// richer proxy for the same tab: CanSeek true, LoopStatus present).
// Config.Media.IgnorePlayers exists specifically to let a user exclude
// the worse of a duplicate pair from discovery/selection/target_cycle.
//
// Every write (PlayPause/Next/Previous/Seek/SetShuffle/SetLoopStatus) is
// sent with dbus.FlagNoReplyExpected: these run on the same dispatcher
// goroutine that serializes every audio.Backend write (see
// engine/dispatch.go's runDispatcher), and a wedged or slow-to-answer
// player must never be able to block that goroutine the way a blocking
// call could. Capability checks (CanControl/CanSeek/Shuffle != nil/
// LoopStatus != "") are made against Tracker's cached PlayerInfo, never
// by a live property Get, for the same reason.
package media

import (
	"context"
	"regexp"
	"strings"
	"time"
)

// PlaybackStatus mirrors MPRIS's Player.PlaybackStatus property.
type PlaybackStatus string

const (
	StatusPlaying PlaybackStatus = "Playing"
	StatusPaused  PlaybackStatus = "Paused"
	StatusStopped PlaybackStatus = "Stopped"
)

// Track is the subset of MPRIS Metadata (xesam: namespace plus
// mpris:trackid) knobd surfaces -- to the UI's media state and to
// media.now_playing's notification.
type Track struct {
	ID      string
	Title   string
	Artists []string
	Album   string
	URL     string
}

// PlayerInfo is one MPRIS player's currently-known state -- a bag of
// best-effort hints, the same posture as focus.AppInfo/audio.Stream:
// Shuffle/LoopStatus are pointers/empty-string exactly because not
// every player implements them (see the package doc comment's Brave/
// plasma-browser-integration example).
type PlayerInfo struct {
	// BusName is the full org.mpris.MediaPlayer2.* bus name, e.g.
	// "org.mpris.MediaPlayer2.brave.instance1918".
	BusName string
	// Identity is Root.Identity -- a human-readable name ("Brave",
	// "Spotify") for status display.
	Identity string
	Status   PlaybackStatus

	CanControl bool
	CanSeek    bool
	// Shuffle is nil when the player has no Shuffle property at all
	// (property Get/GetAll returned no value for it), as opposed to a
	// real false.
	Shuffle *bool
	// LoopStatus is "" when the player has no LoopStatus property at
	// all, as opposed to a real "None". One of "None"/"Track"/
	// "Playlist" otherwise, per the MPRIS spec.
	LoopStatus string

	Track Track
}

// Ref returns BusName with the "org.mpris.MediaPlayer2." prefix and any
// ".instanceNNN"-style D-Bus unique-connection suffix stripped, e.g.
// "org.mpris.MediaPlayer2.brave.instance1918" -> "brave". This is the
// form MediaTransportAction/MediaSeekAction/MediaNowPlayingAction's
// PlayerRef and Config.Media.IgnorePlayers both use.
func (p PlayerInfo) Ref() string { return RefOf(p.BusName) }

var instanceSuffixRx = regexp.MustCompile(`\.instance\d+$`)

// RefOf strips busName down to its player ref, per PlayerInfo.Ref's doc
// comment.
func RefOf(busName string) string {
	ref := strings.TrimPrefix(busName, "org.mpris.MediaPlayer2.")
	return instanceSuffixRx.ReplaceAllString(ref, "")
}

// MatchesRef reports whether busName's ref (see RefOf) is ref exactly,
// or ref plus a further dotted suffix of its own (e.g. ref "vlc"
// matching busName "org.mpris.MediaPlayer2.vlc.instance42" via RefOf
// stripping the instance suffix first, or a player that dots its own
// name further still). An empty ref never matches (callers use "" to
// mean "no specific player").
func MatchesRef(busName, ref string) bool {
	if ref == "" {
		return false
	}
	got := RefOf(busName)
	return got == ref || strings.HasPrefix(got, ref+".")
}

// EventKind identifies what changed in an Event.
type EventKind int

const (
	// PlayerAppeared: a new MPRIS bus name was seen; Event.Player is its
	// full initial state.
	PlayerAppeared EventKind = iota
	// PlayerVanished: BusName's owner disappeared from the bus.
	// Event.Player carries only BusName; every other field is zero.
	PlayerVanished
	// PlayerChanged: BusName reported a PropertiesChanged signal;
	// Event.Player is its full current state.
	PlayerChanged
)

// Event is one change Backend.Watch delivers.
type Event struct {
	Kind   EventKind
	Player PlayerInfo
}

// Backend talks to MPRIS players over D-Bus. See mpris.go for the real,
// session-bus-backed implementation and fake.go for FakeBackend.
type Backend interface {
	// Watch streams player discovery/state events until ctx is
	// canceled, self-healing any reconnect internally -- the same
	// contract as focus.Provider.Watch. Called exactly once, by
	// Tracker.
	Watch(ctx context.Context) (<-chan Event, error)

	PlayPause(busName string) error
	Next(busName string) error
	Previous(busName string) error
	// Seek moves the current track's position by offset (positive
	// forward, negative backward) -- MediaSeekAction.SeekMs converted
	// to a time.Duration by the caller. Per the MPRIS spec, seeking
	// past the end of the track is the player's own business (commonly:
	// skip to the next track); this package does not clamp it, since
	// doing so would need a blocking Position read.
	Seek(busName string, offset time.Duration) error
	SetShuffle(busName string, shuffle bool) error
	SetLoopStatus(busName string, status string) error

	Close() error
}

// Notifier shows a desktop notification for media.now_playing.
type Notifier interface {
	// Notify shows summary/body as one notification, replacing this
	// Notifier's own previous one if it's still visible (so repeated
	// now_playing presses update one bubble rather than piling up).
	Notify(summary, body string) error
}
