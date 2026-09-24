package media

import (
	"context"
	"testing"
	"time"
)

func newTestTracker(t *testing.T) (*Tracker, *FakeBackend) {
	t.Helper()
	fb := NewFakeBackend()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	tr, err := NewTracker(ctx, fb, TrackerOptions{})
	if err != nil {
		t.Fatalf("NewTracker: %v", err)
	}
	return tr, fb
}

// settle gives the Tracker's background consume goroutine a chance to
// process an emitted event before the test asserts on it -- Emit only
// pushes onto FakeBackend's buffered channel, it doesn't block for
// delivery.
func settle() { time.Sleep(20 * time.Millisecond) }

func player(busName string, status PlaybackStatus) PlayerInfo {
	return PlayerInfo{BusName: busName, Identity: busName, Status: status, CanControl: true}
}

func TestTrackerNoPlayersSelectedIsFalse(t *testing.T) {
	tr, _ := newTestTracker(t)
	if _, ok := tr.Selected(nil); ok {
		t.Error("Selected() ok = true with no players known")
	}
}

func TestTrackerSelectedMostRecentlyPlayingWins(t *testing.T) {
	tr, fb := newTestTracker(t)
	fb.Emit(Event{Kind: PlayerAppeared, Player: player("org.mpris.MediaPlayer2.b", StatusPaused)})
	settle()
	fb.Emit(Event{Kind: PlayerAppeared, Player: player("org.mpris.MediaPlayer2.a", StatusPaused)})
	settle()

	// Neither is playing yet, and mere appearance never steals the
	// selection: the tie-break among never-activated players is by
	// name, so a wins regardless of discovery order.
	got, ok := tr.Selected(nil)
	if !ok || got.BusName != "org.mpris.MediaPlayer2.a" {
		t.Fatalf("Selected() = %#v, %v, want a", got, ok)
	}

	// a starts playing: a should now be most recently active.
	fb.Emit(Event{Kind: PlayerChanged, Player: player("org.mpris.MediaPlayer2.a", StatusPlaying)})
	settle()
	got, ok = tr.Selected(nil)
	if !ok || got.BusName != "org.mpris.MediaPlayer2.a" {
		t.Fatalf("Selected() after a plays = %#v, %v, want a", got, ok)
	}
}

func TestTrackerIgnoreListExcludesFromSelection(t *testing.T) {
	tr, fb := newTestTracker(t)
	fb.Emit(Event{Kind: PlayerAppeared, Player: player("org.mpris.MediaPlayer2.brave.instance1", StatusPlaying)})
	settle()
	fb.Emit(Event{Kind: PlayerAppeared, Player: player("org.mpris.MediaPlayer2.plasma-browser-integration", StatusPaused)})
	settle()

	got, ok := tr.Selected([]string{"brave"})
	if !ok || got.BusName != "org.mpris.MediaPlayer2.plasma-browser-integration" {
		t.Fatalf("Selected(ignore brave) = %#v, %v, want plasma-browser-integration", got, ok)
	}
}

func TestTrackerResolveByRefMatchesInstanceSuffix(t *testing.T) {
	tr, fb := newTestTracker(t)
	fb.Emit(Event{Kind: PlayerAppeared, Player: player("org.mpris.MediaPlayer2.brave.instance1918", StatusPaused)})
	settle()

	got, ok := tr.Resolve("brave", nil)
	if !ok || got.BusName != "org.mpris.MediaPlayer2.brave.instance1918" {
		t.Fatalf("Resolve(brave) = %#v, %v", got, ok)
	}
	if _, ok := tr.Resolve("spotify", nil); ok {
		t.Error("Resolve(spotify) ok = true, want false (no such player)")
	}
}

func TestTrackerCycleAdvancesAndWraps(t *testing.T) {
	tr, fb := newTestTracker(t)
	fb.Emit(Event{Kind: PlayerAppeared, Player: player("org.mpris.MediaPlayer2.a", StatusPlaying)})
	settle()
	fb.Emit(Event{Kind: PlayerAppeared, Player: player("org.mpris.MediaPlayer2.b", StatusPaused)})
	settle()

	// a is selected (playing; b never activated). Cycle should move to
	// b.
	if sel, ok := tr.Selected(nil); !ok || sel.BusName != "org.mpris.MediaPlayer2.a" {
		t.Fatalf("Selected() before Cycle = %#v, %v, want a", sel, ok)
	}
	got, ok := tr.Cycle(nil)
	if !ok || got.BusName != "org.mpris.MediaPlayer2.b" {
		t.Fatalf("first Cycle() = %#v, %v, want b", got, ok)
	}
	// Selected now reflects the cycle, since Cycle bumps b's seq.
	if sel, ok := tr.Selected(nil); !ok || sel.BusName != "org.mpris.MediaPlayer2.b" {
		t.Fatalf("Selected() after Cycle = %#v, %v, want b", sel, ok)
	}
	// Cycle again should wrap back to a.
	got, ok = tr.Cycle(nil)
	if !ok || got.BusName != "org.mpris.MediaPlayer2.a" {
		t.Fatalf("second Cycle() = %#v, %v, want a", got, ok)
	}
}

func TestTrackerCycleWithNoPlayersIsFalse(t *testing.T) {
	tr, _ := newTestTracker(t)
	if _, ok := tr.Cycle(nil); ok {
		t.Error("Cycle() ok = true with no players known")
	}
}

func TestTrackerVanishFallsBackToNextMostRecentlyActive(t *testing.T) {
	tr, fb := newTestTracker(t)
	fb.Emit(Event{Kind: PlayerAppeared, Player: player("org.mpris.MediaPlayer2.a", StatusPaused)})
	settle()
	fb.Emit(Event{Kind: PlayerAppeared, Player: player("org.mpris.MediaPlayer2.b", StatusPlaying)})
	settle()

	if sel, ok := tr.Selected(nil); !ok || sel.BusName != "org.mpris.MediaPlayer2.b" {
		t.Fatalf("Selected() = %#v, %v, want b", sel, ok)
	}

	fb.Emit(Event{Kind: PlayerVanished, Player: PlayerInfo{BusName: "org.mpris.MediaPlayer2.b"}})
	settle()

	got, ok := tr.Selected(nil)
	if !ok || got.BusName != "org.mpris.MediaPlayer2.a" {
		t.Fatalf("Selected() after b vanished = %#v, %v, want a", got, ok)
	}
}

func TestTrackerSnapshotListsVisiblePlayersAndSelected(t *testing.T) {
	tr, fb := newTestTracker(t)
	fb.Emit(Event{Kind: PlayerAppeared, Player: player("org.mpris.MediaPlayer2.a", StatusPlaying)})
	settle()
	fb.Emit(Event{Kind: PlayerAppeared, Player: player("org.mpris.MediaPlayer2.brave.instance1", StatusPaused)})
	settle()

	players, selected := tr.Snapshot([]string{"brave"})
	if len(players) != 1 || players[0].BusName != "org.mpris.MediaPlayer2.a" {
		t.Fatalf("Snapshot players = %#v, want just a", players)
	}
	if selected != "org.mpris.MediaPlayer2.a" {
		t.Fatalf("Snapshot selected = %q, want a", selected)
	}
}
