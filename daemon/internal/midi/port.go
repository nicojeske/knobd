// Package midi defines the transport-level interface between knobd and
// a MIDI controller: reading/writing raw 3-byte messages and discovering
// devices. It knows nothing about what the bytes mean for any specific
// controller — that translation is daemon/internal/device's job.
//
// The real backend (discover.go, rawmidi.go, watcher.go, supervisor.go)
// targets findings from live testing, captured in
// specs/reference/xtouch-mini-midi-map.md and specs/reference/environment.md:
//   - The X-Touch Mini shows up as a plain ALSA rawmidi character
//     device, readable/writable without CGo: read(2)/write(2) on
//     /dev/snd/midiC<card>D0 works, confirmed with a 20-line Python
//     script during planning.
//   - /dev/snd/by-id/usb-Behringer_X-TOUCH_MINI_* is the stable thing to
//     *discover* through (card numbers are not stable across reboots or
//     other USB audio devices being plugged in) — but it resolves to
//     /dev/snd/controlC<N>, the ALSA *control* device, not the rawmidi
//     node. The path actually opened for I/O must be the corresponding
//     /dev/snd/midiC<N>D0 (see discover.go); opening the control device
//     as if it were rawmidi doesn't error, it just blocks forever on the
//     first Read.
//   - On Linux, os.OpenFile on a character device is registered with
//     Go's runtime netpoller (the non-pollable-fd exclusion in
//     os.newFile is BSD-only), so a blocked Read unblocks promptly and
//     with a clean error when the file is Closed — no epoll plumbing,
//     no golang.org/x/sys, no CGo needed for that or for hotplug
//     watching (see watcher.go, built on the syscall package's stdlib
//     inotify wrappers).
//   - The device can disappear (unplugged, or the kernel briefly drops
//     it around a suspend/resume); Supervisor (supervisor.go) is the
//     hotplug + reconnect-with-backoff story built on top of Port.
//   - Messages need running-status handling (parser.go): a real capture
//     showed the fader's pitch-bend stream can arrive without a
//     repeated status byte between consecutive updates.
package midi

import (
	"context"
	"time"
)

// Message is one raw MIDI message as received from or sent to a Port.
// knobd only deals with 3-byte channel messages (note on/off, control
// change, pitch bend) — the X-Touch Mini in Mackie Control mode never
// sends anything else, per the live capture this was designed against.
type Message struct {
	// Status is the MIDI status byte, e.g. 0x90 for note-on channel 1,
	// 0xB0 for control-change channel 1, 0xE8 for pitch-bend channel 9.
	Status byte
	Data1  byte
	Data2  byte
	// Time is when the Port received (or is about to send) the message.
	// The real backend stamps this the instant read(2) returns, since
	// that's the closest available to the hardware event — the rawmidi
	// character device carries no hardware timestamp of its own (that's
	// a sequencer-API feature) — and gesture timing (press vs. hold, see
	// model.GestureHold) is measured from it.
	Time time.Time
}

// Port is a bidirectional, single-device MIDI connection. A Port
// implementation owns exactly one physical or virtual device; knobd
// currently only ever opens one Port (the X-Touch Mini), but the
// interface does not assume that.
type Port interface {
	// Read blocks until a message arrives, ctx is canceled, or the port
	// fails (e.g. the device was unplugged), whichever comes first. A
	// failed Read should return an error that unwraps to
	// context.Canceled only when ctx was actually the cause.
	Read(ctx context.Context) (Message, error)

	// Write sends a message to the device (e.g. an LED update — see
	// specs/milestones/M05-led-feedback.md). Write may be called
	// concurrently with Read.
	Write(ctx context.Context, msg Message) error

	// Close releases the underlying device. After Close, Read must
	// return promptly with an error rather than blocking forever.
	Close() error
}

// DeviceInfo describes one discovered MIDI device.
type DeviceInfo struct {
	// Name is a human-readable identification, e.g. "X-TOUCH MINI".
	Name string
	// Path is the rawmidi character device to pass to Open, e.g.
	// "/dev/snd/midiC3D0". This is deliberately not a /dev/snd/by-id
	// path — see the package doc comment for why that symlink points at
	// the wrong device for I/O.
	Path string
	// StableID is the /dev/snd/by-id path Path was resolved from, kept
	// around for logging and for re-resolving after a reconnect (Path's
	// card number is not stable across replugs). Empty when discovery
	// fell back to scanning for rawmidi nodes directly because by-id
	// wasn't available.
	StableID string
}
