// Package midi defines the transport-level interface between knobd and
// a MIDI controller: reading/writing raw 3-byte messages and discovering
// devices. It knows nothing about what the bytes mean for any specific
// controller — that translation is daemon/internal/device's job.
//
// TODO(M02): implement the real backend. Findings from live testing this
// package's implementation should target, captured in
// specs/reference/xtouch-mini-midi-map.md and specs/reference/environment.md:
//   - The X-Touch Mini shows up as a plain ALSA rawmidi character
//     device, readable/writable without CGo: read(2)/write(2) on
//     /dev/snd/midiC<card>D0 works, confirmed with a 20-line Python
//     script during planning.
//   - Prefer /dev/snd/by-id/usb-Behringer_X-TOUCH_MINI_1.0.1-00 (or a
//     glob over /dev/snd/by-id/usb-Behringer_X-TOUCH_MINI_*) over a
//     card index — card numbers are not stable across reboots or other
//     USB audio devices being plugged in.
//   - The device can disappear (unplugged, or the kernel briefly drops
//     it around a suspend/resume); Discover and the real Port
//     implementation need a hotplug story (inotify on /dev/snd, or
//     periodic re-scan) and reconnect backoff, not just a one-shot open.
//   - Messages need running-status handling: a real capture showed the
//     fader's pitch-bend stream can arrive without a repeated status
//     byte between consecutive updates.
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
	// A real backend should use the timestamp closest to the hardware
	// event, not just time.Now() at the point the message is decoded,
	// since gesture timing (press vs. hold, see model.GestureHold) is
	// measured from it.
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
	// Path is the stable device path to open, preferring
	// /dev/snd/by-id/* over /dev/snd/midiC<n>D0 (see the package doc
	// comment for why).
	Path string
}

// Discover lists connected MIDI devices. TODO(M02): implement by
// scanning /dev/snd/by-id for entries matching known controller vendor
// strings (starting with "usb-Behringer_X-TOUCH_MINI_"), falling back to
// /dev/snd/by-path or a full /proc/asound scan if by-id is unavailable
// (e.g. udev not running).
func Discover() ([]DeviceInfo, error) {
	return nil, errNotImplemented("midi.Discover")
}

// Open opens the device at path as a Port. TODO(M02): implement using
// os.OpenFile on the rawmidi character device in read-write mode, and a
// background goroutine parsing the byte stream (including running
// status) into Messages for Read to consume.
func Open(path string) (Port, error) {
	return nil, errNotImplemented("midi.Open")
}
