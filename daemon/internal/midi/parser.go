package midi

import "time"

// parser turns a raw byte stream from the rawmidi device into complete
// Messages, handling MIDI running status: a channel-voice status byte
// may be omitted on subsequent messages of the same type until a
// different status byte appears (a live capture showed the X-Touch
// Mini's fader does exactly this during a fast sweep — see
// specs/milestones/M02-midi-transport.md's Design section).
//
// It also has to stay correct in the presence of bytes the X-Touch Mini
// has never been observed to send but that are legal MIDI and would
// otherwise desynchronize the parser if they ever appeared (e.g. Active
// Sensing, which some controllers emit as a keep-alive):
//
//   - System Realtime bytes (0xF8-0xFF) may appear *interleaved inside*
//     another message's bytes; they carry no data and must be skipped
//     without disturbing runningStatus or a message already in progress.
//   - System Common bytes (0xF1-0xF7) cancel running status, per spec.
//   - A System Exclusive block (0xF0 ... 0xF7) is skipped as an opaque
//     span rather than misinterpreted as channel-voice bytes.
//
// A parser is not safe for concurrent use; each Port owns exactly one.
type parser struct {
	runningStatus byte // 0 = none
	pending       []byte
	inSysEx       bool
}

// statusDataLen returns how many data bytes follow a channel-voice
// status byte, or -1 if status is not a channel-voice status this
// parser recognizes (the X-Touch Mini's MC-mode protocol never sends
// System Exclusive-adjacent channel modes beyond these, but an unknown
// status byte is treated the same as "no running status" rather than
// desynchronizing the stream).
func statusDataLen(status byte) int {
	switch status & 0xF0 {
	case 0xC0, 0xD0: // program change, channel pressure
		return 1
	case 0x80, 0x90, 0xA0, 0xB0, 0xE0: // note off/on, poly pressure, CC, pitch bend
		return 2
	default:
		return -1
	}
}

// push feeds b (one read(2) result) through the parser and returns every
// complete Message it produced, stamped with now. now should be the
// instant the read syscall returned — see Message.Time's doc comment for
// why gesture timing depends on that being close to the hardware event.
func (p *parser) push(b []byte, now time.Time) []Message {
	var out []Message
	for _, c := range b {
		switch {
		case c >= 0xF8:
			// System Realtime: transparent, never touches parser state.

		case p.inSysEx:
			if c == 0xF7 {
				p.inSysEx = false
			}

		case c == 0xF0:
			p.inSysEx = true
			p.runningStatus = 0
			p.pending = p.pending[:0]

		case c >= 0xF1:
			// System Common (0xF1-0xF7): cancels running status.
			p.runningStatus = 0
			p.pending = p.pending[:0]

		case c >= 0x80:
			// New status byte.
			p.pending = p.pending[:0]
			if statusDataLen(c) < 0 {
				p.runningStatus = 0
			} else {
				p.runningStatus = c
			}

		default:
			// Data byte.
			if p.runningStatus == 0 {
				continue // no (recognized) status yet; discard
			}
			need := statusDataLen(p.runningStatus)
			p.pending = append(p.pending, c)
			if len(p.pending) < need {
				continue
			}
			msg := Message{Status: p.runningStatus, Time: now}
			if need >= 1 {
				msg.Data1 = p.pending[0]
			}
			if need >= 2 {
				msg.Data2 = p.pending[1]
			}
			out = append(out, msg)
			p.pending = p.pending[:0]
		}
	}
	return out
}
