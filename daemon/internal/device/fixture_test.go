package device

import (
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/njeske/knobd/internal/midi"
	"github.com/njeske/knobd/internal/model"
)

// readGuidedCapture parses testdata/midi/guided-capture.txt's format:
// "seconds status data1 data2" where, despite the fixture directory's
// own README, the status column is hex and the two data columns are
// decimal (e.g. "1.912 B0  23  65" is CC 23 = 65, not CC 0x23). The
// timestamp column is unused here — Decode doesn't need it.
func readGuidedCapture(t *testing.T, path string) []midi.Message {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var msgs []midi.Message
	for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		f := strings.Fields(line)
		if len(f) != 4 {
			t.Fatalf("%s: unexpected line format %q (want 4 fields)", path, line)
		}
		status, err := strconv.ParseUint(f[1], 16, 8)
		if err != nil {
			t.Fatalf("%s: parse status in %q: %v", path, line, err)
		}
		data1, err := strconv.ParseUint(f[2], 10, 8)
		if err != nil {
			t.Fatalf("%s: parse data1 in %q: %v", path, line, err)
		}
		data2, err := strconv.ParseUint(f[3], 10, 8)
		if err != nil {
			t.Fatalf("%s: parse data2 in %q: %v", path, line, err)
		}
		msgs = append(msgs, midi.Message{Status: byte(status), Data1: byte(data1), Data2: byte(data2)})
	}
	return msgs
}

// readFreePlayCapture parses testdata/midi/free-play-capture.txt's
// format: plain `amidi -d` output, all-hex "status data1 data2", one
// leading blank line, no trailing newline on the last line — a
// different format from guided-capture.txt, not the same parser reused.
func readFreePlayCapture(t *testing.T, path string) []midi.Message {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var msgs []midi.Message
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		f := strings.Fields(line)
		if len(f) != 3 {
			t.Fatalf("%s: unexpected line format %q (want 3 fields)", path, line)
		}
		status, err := strconv.ParseUint(f[0], 16, 8)
		if err != nil {
			t.Fatalf("%s: parse status in %q: %v", path, line, err)
		}
		data1, err := strconv.ParseUint(f[1], 16, 8)
		if err != nil {
			t.Fatalf("%s: parse data1 in %q: %v", path, line, err)
		}
		data2, err := strconv.ParseUint(f[2], 16, 8)
		if err != nil {
			t.Fatalf("%s: parse data2 in %q: %v", path, line, err)
		}
		msgs = append(msgs, midi.Message{Status: byte(status), Data1: byte(data1), Data2: byte(data2)})
	}
	return msgs
}

// TestFixtureGuidedCapture replays the real guided capture and checks
// the exact decoded sequence — the concrete form of acceptance criteria
// #2/#4 in specs/milestones/M02-midi-transport.md. The fixture itself
// has known gaps (see testdata/midi/README.md and the doc comments
// below): no bottom-row notes, no fader, and only encoder 8 — this test
// documents that rather than pretending otherwise.
func TestFixtureGuidedCapture(t *testing.T) {
	msgs := readGuidedCapture(t, "../../../testdata/midi/guided-capture.txt")
	c := NewXTouchMiniCodec()

	var turns, buttons []Event
	for _, msg := range msgs {
		ev, ok, err := c.Decode(msg)
		if err != nil {
			t.Fatalf("Decode(%+v): unexpected error: %v", msg, err)
		}
		if !ok {
			continue
		}
		if ev.Kind == EventTurn {
			turns = append(turns, ev)
		} else {
			buttons = append(buttons, ev)
		}
	}

	// Every turn in this capture is encoder 8 (CC 23) — the fixture has
	// no CC 16 (encoder 1) traffic at all, contrary to
	// testdata/midi/README.md's description of the capture sequence.
	if len(turns) == 0 {
		t.Fatal("expected at least one turn event")
	}
	for _, ev := range turns {
		if ev.Control != (model.Control{Kind: model.ControlEncoder, Index: 8}) {
			t.Errorf("turn event on unexpected control %+v", ev.Control)
		}
	}
	// First block (t=1.912-2.811) is counter-clockwise, second block
	// (t=3.666-5.199) is clockwise — see the map's sign convention.
	if turns[0].Delta >= 0 {
		t.Errorf("first turn Delta = %d, want negative (counter-clockwise)", turns[0].Delta)
	}
	if turns[len(turns)-1].Delta <= 0 {
		t.Errorf("last turn Delta = %d, want positive (clockwise)", turns[len(turns)-1].Delta)
	}

	type ctrlEvent struct {
		Control model.Control
		Kind    EventKind
	}
	want := []ctrlEvent{
		// The capture's very first line is an unpaired note-off (top
		// row button 2) — Decode is stateless, so this must still come
		// through as a plain release, not be dropped or rejected.
		{model.Control{Kind: model.ControlButton, Index: 2}, EventButtonUp},

		{model.Control{Kind: model.ControlEncoderPush, Index: 1}, EventButtonDown},
		{model.Control{Kind: model.ControlEncoderPush, Index: 1}, EventButtonUp},
		{model.Control{Kind: model.ControlEncoderPush, Index: 8}, EventButtonDown},
		{model.Control{Kind: model.ControlEncoderPush, Index: 8}, EventButtonUp},

		{model.Control{Kind: model.ControlButton, Index: 1}, EventButtonDown},
		{model.Control{Kind: model.ControlButton, Index: 1}, EventButtonUp},
		{model.Control{Kind: model.ControlButton, Index: 2}, EventButtonDown},
		{model.Control{Kind: model.ControlButton, Index: 2}, EventButtonUp},
		{model.Control{Kind: model.ControlButton, Index: 3}, EventButtonDown},
		{model.Control{Kind: model.ControlButton, Index: 3}, EventButtonUp},
		{model.Control{Kind: model.ControlButton, Index: 4}, EventButtonDown},
		{model.Control{Kind: model.ControlButton, Index: 4}, EventButtonUp},
		{model.Control{Kind: model.ControlButton, Index: 5}, EventButtonDown},
		{model.Control{Kind: model.ControlButton, Index: 5}, EventButtonUp},
		{model.Control{Kind: model.ControlButton, Index: 6}, EventButtonDown},
		{model.Control{Kind: model.ControlButton, Index: 6}, EventButtonUp},
		{model.Control{Kind: model.ControlButton, Index: 7}, EventButtonDown},
		{model.Control{Kind: model.ControlButton, Index: 7}, EventButtonUp},
		{model.Control{Kind: model.ControlButton, Index: 8}, EventButtonDown},
		{model.Control{Kind: model.ControlButton, Index: 8}, EventButtonUp},

		{model.Control{Kind: model.ControlSideButton, Index: 1}, EventButtonDown},
		{model.Control{Kind: model.ControlSideButton, Index: 1}, EventButtonUp},
		{model.Control{Kind: model.ControlSideButton, Index: 2}, EventButtonDown},
		{model.Control{Kind: model.ControlSideButton, Index: 2}, EventButtonUp},
	}

	if len(buttons) != len(want) {
		t.Fatalf("got %d button/push/side events, want %d\n got: %+v\nwant: %+v", len(buttons), len(want), buttons, want)
	}
	for i := range want {
		if buttons[i].Control != want[i].Control || buttons[i].Kind != want[i].Kind {
			t.Errorf("event %d = {%+v %v}, want {%+v %v}", i, buttons[i].Control, buttons[i].Kind, want[i].Control, want[i].Kind)
		}
	}
}

// TestFixtureFreePlayCaptureDecodesCleanly is the "does the codec choke
// on anything" smoke test testdata/midi/README.md describes this
// fixture for: every message must decode without error (ok may be false
// for e.g. duplicate/no-op values, but never err != nil), and the
// message-type histogram is pinned to the values verified during
// planning so a change in this fixture's content doesn't go unnoticed.
func TestFixtureFreePlayCaptureDecodesCleanly(t *testing.T) {
	msgs := readFreePlayCapture(t, "../../../testdata/midi/free-play-capture.txt")
	if len(msgs) != 569 {
		t.Fatalf("parsed %d messages, want 569", len(msgs))
	}

	var notes, ccs, pitchBends int
	c := NewXTouchMiniCodec()
	for _, msg := range msgs {
		switch msg.Status & 0xF0 {
		case 0x90, 0x80:
			notes++
		case 0xB0:
			ccs++
		case 0xE0:
			pitchBends++
		}
		if _, _, err := c.Decode(msg); err != nil {
			t.Errorf("Decode(%+v): unexpected error: %v", msg, err)
		}
	}
	if notes != 125 {
		t.Errorf("note messages = %d, want 125", notes)
	}
	if ccs != 40 {
		t.Errorf("CC messages = %d, want 40", ccs)
	}
	if pitchBends != 404 {
		t.Errorf("pitch bend messages = %d, want 404", pitchBends)
	}
}

// TestFixtureLiveVerificationCapture replays the live capture taken
// specifically to re-verify the bottom row's previously-inferred notes
// (see specs/reference/xtouch-mini-midi-map.md) against the physical
// unit. It decodes cleanly end to end and, in particular, confirms
// bottom-row positions 2-5 (notes 88, 91, 92, 86) actually decode to
// buttons 10-13 as the map claims, closing out that risk.
func TestFixtureLiveVerificationCapture(t *testing.T) {
	msgs := readFreePlayCapture(t, "../../../testdata/midi/live-verification-capture.txt")
	c := NewXTouchMiniCodec()

	seenBottomRow := make(map[int]bool)
	var faderMoves int
	for _, msg := range msgs {
		ev, ok, err := c.Decode(msg)
		if err != nil {
			t.Fatalf("Decode(%+v): unexpected error: %v", msg, err)
		}
		if !ok {
			continue
		}
		if ev.Control.Kind == model.ControlButton && ev.Control.Index >= 9 {
			seenBottomRow[ev.Control.Index] = true
		}
		if ev.Kind == EventFaderMove {
			faderMoves++
		}
	}

	// Positions 2-5 (global indices 10-13) were the ones only inferred
	// from the published Mackie Control map, not previously captured on
	// this unit.
	for _, idx := range []int{10, 11, 12, 13} {
		if !seenBottomRow[idx] {
			t.Errorf("expected bottom-row button %d to appear in the live-verification capture", idx)
		}
	}
	if faderMoves == 0 {
		t.Error("expected at least one fader move in the live-verification capture")
	}
}
