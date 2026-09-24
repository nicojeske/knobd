package actions

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/njeske/knobd/internal/media"
	"github.com/njeske/knobd/internal/model"
)

// fakeMediaPlayers is a MediaPlayers test double, simpler than a real
// media.Tracker: Resolve/Cycle both just return whatever's configured,
// ignoring the ignore list (the handler tests exercise ignore-list
// plumbing via MediaOptions.IgnorePlayers/its own resolve wrapper, not
// via this fake's internals).
type fakeMediaPlayers struct {
	byRef      map[string]media.PlayerInfo
	selected   media.PlayerInfo
	hasDefault bool
	cycleTo    media.PlayerInfo
	hasCycle   bool
}

func (f *fakeMediaPlayers) Resolve(ref string, ignore []string) (media.PlayerInfo, bool) {
	if ref == "" {
		return f.selected, f.hasDefault
	}
	p, ok := f.byRef[ref]
	return p, ok
}

func (f *fakeMediaPlayers) Cycle(ignore []string) (media.PlayerInfo, bool) {
	return f.cycleTo, f.hasCycle
}

func TestMediaTransportPlayPauseRequiresCanControl(t *testing.T) {
	p := media.PlayerInfo{BusName: "org.mpris.MediaPlayer2.vlc", Identity: "VLC", CanControl: false}
	players := &fakeMediaPlayers{selected: p, hasDefault: true}
	backend := media.NewFakeBackend()
	h := NewMediaHandlers(players, backend, &media.FakeNotifier{}, MediaOptions{})

	err := h.executeTransport(context.Background(), Invocation{
		Action: model.MediaTransportAction{Command: model.MediaPlayPause},
	})
	if !errors.Is(err, media.ErrUnsupported) {
		t.Fatalf("err = %v, want ErrUnsupported", err)
	}
	if len(backend.Calls) != 0 {
		t.Errorf("backend.Calls = %v, want none", backend.Calls)
	}
}

func TestMediaTransportPlayPauseSendsCommand(t *testing.T) {
	p := media.PlayerInfo{BusName: "org.mpris.MediaPlayer2.vlc", Identity: "VLC", CanControl: true}
	players := &fakeMediaPlayers{selected: p, hasDefault: true}
	backend := media.NewFakeBackend()
	h := NewMediaHandlers(players, backend, &media.FakeNotifier{}, MediaOptions{})

	if err := h.executeTransport(context.Background(), Invocation{
		Action: model.MediaTransportAction{Command: model.MediaPlayPause},
	}); err != nil {
		t.Fatalf("executeTransport: %v", err)
	}
	want := []string{"PlayPause(org.mpris.MediaPlayer2.vlc)"}
	if len(backend.Calls) != 1 || backend.Calls[0] != want[0] {
		t.Errorf("backend.Calls = %v, want %v", backend.Calls, want)
	}
}

func TestMediaTransportNoPlayerErrors(t *testing.T) {
	players := &fakeMediaPlayers{}
	backend := media.NewFakeBackend()
	h := NewMediaHandlers(players, backend, &media.FakeNotifier{}, MediaOptions{})

	err := h.executeTransport(context.Background(), Invocation{
		Action: model.MediaTransportAction{Command: model.MediaNext},
	})
	if !errors.Is(err, media.ErrNoPlayer) {
		t.Fatalf("err = %v, want ErrNoPlayer", err)
	}
}

func TestMediaTransportPlayerRefResolvesSpecificPlayer(t *testing.T) {
	spotify := media.PlayerInfo{BusName: "org.mpris.MediaPlayer2.spotify", CanControl: true}
	players := &fakeMediaPlayers{byRef: map[string]media.PlayerInfo{"spotify": spotify}}
	backend := media.NewFakeBackend()
	h := NewMediaHandlers(players, backend, &media.FakeNotifier{}, MediaOptions{})

	if err := h.executeTransport(context.Background(), Invocation{
		Action: model.MediaTransportAction{Command: model.MediaNext, PlayerRef: "spotify"},
	}); err != nil {
		t.Fatalf("executeTransport: %v", err)
	}
	if len(backend.Calls) != 1 || backend.Calls[0] != "Next(org.mpris.MediaPlayer2.spotify)" {
		t.Errorf("backend.Calls = %v", backend.Calls)
	}
}

func TestMediaTransportShuffleToggleFlipsCachedValue(t *testing.T) {
	on := true
	p := media.PlayerInfo{BusName: "org.mpris.MediaPlayer2.vlc", Shuffle: &on}
	players := &fakeMediaPlayers{selected: p, hasDefault: true}
	backend := media.NewFakeBackend()
	h := NewMediaHandlers(players, backend, &media.FakeNotifier{}, MediaOptions{})

	if err := h.executeTransport(context.Background(), Invocation{
		Action: model.MediaTransportAction{Command: model.MediaShuffleToggle},
	}); err != nil {
		t.Fatalf("executeTransport: %v", err)
	}
	if len(backend.Calls) != 1 || backend.Calls[0] != "SetShuffle(org.mpris.MediaPlayer2.vlc,false)" {
		t.Errorf("backend.Calls = %v, want SetShuffle(...,false)", backend.Calls)
	}
}

func TestMediaTransportShuffleUnsupportedWhenNil(t *testing.T) {
	p := media.PlayerInfo{BusName: "org.mpris.MediaPlayer2.brave", Shuffle: nil}
	players := &fakeMediaPlayers{selected: p, hasDefault: true}
	backend := media.NewFakeBackend()
	h := NewMediaHandlers(players, backend, &media.FakeNotifier{}, MediaOptions{})

	err := h.executeTransport(context.Background(), Invocation{
		Action: model.MediaTransportAction{Command: model.MediaShuffleToggle},
	})
	if !errors.Is(err, media.ErrUnsupported) {
		t.Fatalf("err = %v, want ErrUnsupported", err)
	}
}

func TestMediaTransportRepeatCycleAdvancesThroughStates(t *testing.T) {
	cases := []struct{ from, want string }{
		{"None", "Track"},
		{"Track", "Playlist"},
		{"Playlist", "None"},
		{"SomeUnknownValue", "None"},
	}
	for _, tc := range cases {
		p := media.PlayerInfo{BusName: "org.mpris.MediaPlayer2.vlc", LoopStatus: tc.from}
		players := &fakeMediaPlayers{selected: p, hasDefault: true}
		backend := media.NewFakeBackend()
		h := NewMediaHandlers(players, backend, &media.FakeNotifier{}, MediaOptions{})

		if err := h.executeTransport(context.Background(), Invocation{
			Action: model.MediaTransportAction{Command: model.MediaRepeatCycle},
		}); err != nil {
			t.Fatalf("from %q: executeTransport: %v", tc.from, err)
		}
		want := "SetLoopStatus(org.mpris.MediaPlayer2.vlc," + tc.want + ")"
		if len(backend.Calls) != 1 || backend.Calls[0] != want {
			t.Errorf("from %q: backend.Calls = %v, want %v", tc.from, backend.Calls, want)
		}
	}
}

func TestMediaTransportRepeatUnsupportedWhenEmpty(t *testing.T) {
	p := media.PlayerInfo{BusName: "org.mpris.MediaPlayer2.brave", LoopStatus: ""}
	players := &fakeMediaPlayers{selected: p, hasDefault: true}
	backend := media.NewFakeBackend()
	h := NewMediaHandlers(players, backend, &media.FakeNotifier{}, MediaOptions{})

	err := h.executeTransport(context.Background(), Invocation{
		Action: model.MediaTransportAction{Command: model.MediaRepeatCycle},
	})
	if !errors.Is(err, media.ErrUnsupported) {
		t.Fatalf("err = %v, want ErrUnsupported", err)
	}
}

func TestMediaTransportWrongActionTypeErrors(t *testing.T) {
	h := NewMediaHandlers(&fakeMediaPlayers{}, media.NewFakeBackend(), &media.FakeNotifier{}, MediaOptions{})
	if err := h.executeTransport(context.Background(), Invocation{Action: model.MediaSeekAction{}}); err == nil {
		t.Fatal("expected an error for a mismatched action type")
	}
}

func TestMediaSeekScalesByDelta(t *testing.T) {
	p := media.PlayerInfo{BusName: "org.mpris.MediaPlayer2.vlc", CanSeek: true}
	players := &fakeMediaPlayers{selected: p, hasDefault: true}
	backend := media.NewFakeBackend()
	h := NewMediaHandlers(players, backend, &media.FakeNotifier{}, MediaOptions{})

	if err := h.executeSeek(context.Background(), Invocation{
		Action: model.MediaSeekAction{SeekMs: 2000},
		Delta:  3,
	}); err != nil {
		t.Fatalf("executeSeek: %v", err)
	}
	want := "Seek(org.mpris.MediaPlayer2.vlc," + (6 * time.Second).String() + ")"
	if len(backend.Calls) != 1 || backend.Calls[0] != want {
		t.Errorf("backend.Calls = %v, want %v", backend.Calls, want)
	}
}

func TestMediaSeekNegativeDeltaSeeksBackward(t *testing.T) {
	p := media.PlayerInfo{BusName: "org.mpris.MediaPlayer2.vlc", CanSeek: true}
	players := &fakeMediaPlayers{selected: p, hasDefault: true}
	backend := media.NewFakeBackend()
	h := NewMediaHandlers(players, backend, &media.FakeNotifier{}, MediaOptions{})

	if err := h.executeSeek(context.Background(), Invocation{
		Action: model.MediaSeekAction{SeekMs: 2000},
		Delta:  -1,
	}); err != nil {
		t.Fatalf("executeSeek: %v", err)
	}
	want := "Seek(org.mpris.MediaPlayer2.vlc," + (-2 * time.Second).String() + ")"
	if len(backend.Calls) != 1 || backend.Calls[0] != want {
		t.Errorf("backend.Calls = %v, want %v", backend.Calls, want)
	}
}

func TestMediaSeekUnsupportedWhenCanSeekFalse(t *testing.T) {
	// The Brave-native MPRIS implementation observed during M09
	// planning: CanSeek false.
	p := media.PlayerInfo{BusName: "org.mpris.MediaPlayer2.brave.instance1918", CanSeek: false}
	players := &fakeMediaPlayers{selected: p, hasDefault: true}
	backend := media.NewFakeBackend()
	h := NewMediaHandlers(players, backend, &media.FakeNotifier{}, MediaOptions{})

	err := h.executeSeek(context.Background(), Invocation{Action: model.MediaSeekAction{SeekMs: 2000}, Delta: 1})
	if !errors.Is(err, media.ErrUnsupported) {
		t.Fatalf("err = %v, want ErrUnsupported", err)
	}
}

func TestMediaTargetCycleSelectsNextPlayer(t *testing.T) {
	cycled := media.PlayerInfo{BusName: "org.mpris.MediaPlayer2.spotify", Identity: "Spotify"}
	players := &fakeMediaPlayers{cycleTo: cycled, hasCycle: true}
	h := NewMediaHandlers(players, media.NewFakeBackend(), &media.FakeNotifier{}, MediaOptions{})

	if err := h.executeTargetCycle(context.Background(), Invocation{Action: model.MediaTargetCycleAction{}}); err != nil {
		t.Fatalf("executeTargetCycle: %v", err)
	}
}

func TestMediaTargetCycleNoPlayerErrors(t *testing.T) {
	players := &fakeMediaPlayers{hasCycle: false}
	h := NewMediaHandlers(players, media.NewFakeBackend(), &media.FakeNotifier{}, MediaOptions{})

	err := h.executeTargetCycle(context.Background(), Invocation{Action: model.MediaTargetCycleAction{}})
	if !errors.Is(err, media.ErrNoPlayer) {
		t.Fatalf("err = %v, want ErrNoPlayer", err)
	}
}

func TestMediaNowPlayingNotifiesWithTrackInfo(t *testing.T) {
	p := media.PlayerInfo{
		BusName:  "org.mpris.MediaPlayer2.spotify",
		Identity: "Spotify",
		Track: media.Track{
			Title:   "Song Title",
			Artists: []string{"Artist"},
			Album:   "Album",
		},
	}
	players := &fakeMediaPlayers{selected: p, hasDefault: true}
	notifier := &media.FakeNotifier{}
	h := NewMediaHandlers(players, media.NewFakeBackend(), notifier, MediaOptions{})

	if err := h.executeNowPlaying(context.Background(), Invocation{Action: model.MediaNowPlayingAction{}}); err != nil {
		t.Fatalf("executeNowPlaying: %v", err)
	}
	if len(notifier.Sent) != 1 {
		t.Fatalf("notifier.Sent = %v, want 1 entry", notifier.Sent)
	}
	if notifier.Sent[0].Summary != "Song Title" {
		t.Errorf("Summary = %q, want %q", notifier.Sent[0].Summary, "Song Title")
	}
}

func TestMediaNowPlayingNoTrackShowsNothingPlaying(t *testing.T) {
	p := media.PlayerInfo{BusName: "org.mpris.MediaPlayer2.vlc", Identity: "VLC"}
	players := &fakeMediaPlayers{selected: p, hasDefault: true}
	notifier := &media.FakeNotifier{}
	h := NewMediaHandlers(players, media.NewFakeBackend(), notifier, MediaOptions{})

	if err := h.executeNowPlaying(context.Background(), Invocation{Action: model.MediaNowPlayingAction{}}); err != nil {
		t.Fatalf("executeNowPlaying: %v", err)
	}
	if len(notifier.Sent) != 1 || notifier.Sent[0].Summary != "Nothing playing" {
		t.Fatalf("notifier.Sent = %v, want Summary=Nothing playing", notifier.Sent)
	}
}

func TestMediaIgnorePlayersReadFreshEveryDispatch(t *testing.T) {
	ignore := []string{"brave"}
	players := &fakeMediaPlayers{} // Resolve ignores the arg itself; this test only proves it's plumbed through
	backend := media.NewFakeBackend()
	h := NewMediaHandlers(players, backend, &media.FakeNotifier{}, MediaOptions{
		IgnorePlayers: func() []string { return ignore },
	})
	if got := h.opts.ignorePlayers(); len(got) != 1 || got[0] != "brave" {
		t.Fatalf("ignorePlayers() = %v, want [brave]", got)
	}
	ignore = append(ignore, "chromium")
	if got := h.opts.ignorePlayers(); len(got) != 2 {
		t.Fatalf("ignorePlayers() = %v, want a live read reflecting the update", got)
	}
}
