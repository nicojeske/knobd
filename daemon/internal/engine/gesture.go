package engine

import (
	"time"

	"github.com/njeske/knobd/internal/device"
	"github.com/njeske/knobd/internal/model"
)

// gesture is one resolved (Control, Gesture) firing, ready to be looked
// up against a bindingIndex.
type gesture struct {
	Control model.Control
	Gesture model.Gesture
	// Delta is the signed detent count for model.GestureTurn; 0
	// otherwise.
	Delta int
	// Value is the absolute 0-127 position for model.GestureMove; 0
	// otherwise.
	Value int
	// At is the hardware timestamp of the event that produced (or, for
	// GestureHold/a deferred GesturePress, completed) this gesture.
	At time.Time
}

// pressState tracks one control currently down.
type pressState struct {
	downAt        time.Time
	holdDelivered bool
}

// pendingPress is a GesturePress deferred past its own release, waiting
// to see whether a second press arrives within doubleWindow and turns it
// into a GestureDoublePress instead.
type pendingPress struct {
	g        gesture
	deadline time.Time
}

// gestureMachine turns a stream of device.Event values into
// model.Gesture firings: it is the press-vs-hold-vs-double-press state
// machine specs/milestones/M02-midi-transport.md left for M04, since
// device.Codec.Decode ships stateless (see its doc comment).
//
// The machine itself has no clock. Handle makes every timing decision
// against the device.Event's own Time field — the hardware timestamp
// midi.Message carries from read(2) — so it is a pure function of its
// input and can be driven entirely by fabricated timestamps in tests,
// with zero elapsed real time. Only the caller (Engine.Run) needs a real
// clock, and only to know when to call Tick with no further input; see
// NextDeadline.
type gestureMachine struct {
	hold         time.Duration
	doubleWindow time.Duration
	// deferPress reports whether c has a double_press binding on any
	// layer, and therefore must have its press deferred until
	// doubleWindow has passed with no second press. A control with no
	// double_press binding fires GesturePress the instant its up event
	// arrives — zero added latency on the common path — at the cost of
	// never producing GestureDoublePress for that control (which is
	// harmless: nothing binds it). nil means never defer.
	deferPress func(model.Control) bool
	// detectHold reports whether c has a hold or release binding on any
	// layer, and therefore must have a long press actually turned into
	// GestureHold at the hold threshold. A control with neither instead
	// keeps accumulating toward GesturePress/GestureDoublePress no
	// matter how long it's held, so a long press on a press-only control
	// still fires the press on release rather than being silently
	// dropped. nil means always detect (the opposite default from
	// deferPress, since detecting a hold that's never bound is harmless
	// — it's exactly today's behavior — while deferring a press that's
	// never bound to double_press is the one that must default off).
	detectHold func(model.Control) bool

	down      map[model.Control]*pressState
	lastPress map[model.Control]time.Time
	pending   map[model.Control]pendingPress
}

// newGestureMachine returns a gestureMachine ready to receive events. A
// nil deferPress means every press fires immediately and
// GestureDoublePress is never produced. A nil detectHold means every
// control held past hold fires GestureHold (today's behavior, before
// per-control hold detection existed).
func newGestureMachine(hold, doubleWindow time.Duration, deferPress, detectHold func(model.Control) bool) *gestureMachine {
	m := &gestureMachine{hold: hold, doubleWindow: doubleWindow, deferPress: deferPress, detectHold: detectHold}
	m.Reset()
	return m
}

// Handle folds one decoded device.Event into zero or more gestures.
func (m *gestureMachine) Handle(ev device.Event) []gesture {
	switch ev.Kind {
	case device.EventTurn:
		return []gesture{{Control: ev.Control, Gesture: model.GestureTurn, Delta: ev.Delta, At: ev.Time}}
	case device.EventFaderMove:
		return []gesture{{Control: ev.Control, Gesture: model.GestureMove, Value: ev.Value, At: ev.Time}}
	case device.EventButtonDown:
		// Overwrite any stale state unconditionally: Decode is stateless
		// and permits an unbalanced sequence, so a second down with no
		// intervening up must not panic or get stuck mid-hold forever.
		m.down[ev.Control] = &pressState{downAt: ev.Time}
		return nil
	case device.EventButtonUp:
		return m.handleUp(ev.Control, ev.Time)
	default:
		return nil
	}
}

func (m *gestureMachine) handleUp(c model.Control, at time.Time) []gesture {
	ps, ok := m.down[c]
	if !ok {
		// A note-off with no corresponding down. Codec.Decode explicitly
		// permits this (its own doc comment); it decodes to a plain
		// EventButtonUp regardless of prior state, and the gesture
		// machine must likewise not synthesize a gesture for it.
		return nil
	}
	delete(m.down, c)

	if ps.holdDelivered {
		// A hold already fired for this press; the up is its release,
		// never a press of any kind. A held-then-released control also
		// can never be half of a double press.
		delete(m.lastPress, c)
		return []gesture{{Control: c, Gesture: model.GestureRelease, At: at}}
	}

	if m.deferPress == nil || !m.deferPress(c) {
		return []gesture{{Control: c, Gesture: model.GesturePress, At: at}}
	}

	if last, ok := m.lastPress[c]; ok && !at.After(last.Add(m.doubleWindow)) {
		// The second press of a pair: the first press's deferred
		// GesturePress must never fire on its own.
		delete(m.pending, c)
		delete(m.lastPress, c)
		return []gesture{{Control: c, Gesture: model.GestureDoublePress, At: at}}
	}

	m.pending[c] = pendingPress{
		g:        gesture{Control: c, Gesture: model.GesturePress, At: at},
		deadline: at.Add(m.doubleWindow),
	}
	m.lastPress[c] = at
	return nil
}

// Tick emits the gestures that become due purely by the passage of time
// with no further input: GestureHold for a control still held past
// hold, and a deferred GesturePress whose doubleWindow has expired with
// no second press arriving.
func (m *gestureMachine) Tick(now time.Time) []gesture {
	var out []gesture

	for c, ps := range m.down {
		if ps.holdDelivered || now.Before(ps.downAt.Add(m.hold)) {
			continue
		}
		if m.detectHold != nil && !m.detectHold(c) {
			// Nothing is bound to this control's hold/release: leave it
			// accumulating in m.down rather than marking holdDelivered,
			// so handleUp still sees an ordinary (possibly long) press
			// on release instead of dropping it.
			continue
		}
		ps.holdDelivered = true
		// A control that reaches the hold threshold can never turn out
		// to have been the first half of a double press.
		delete(m.lastPress, c)
		out = append(out, gesture{Control: c, Gesture: model.GestureHold, At: now})
	}

	for c, pp := range m.pending {
		if now.Before(pp.deadline) {
			continue
		}
		delete(m.pending, c)
		delete(m.lastPress, c)
		out = append(out, pp.g)
	}

	return out
}

// NextDeadline reports the earliest time at which Tick would produce a
// gesture if given no further input before then. ok is false when
// nothing is pending.
func (m *gestureMachine) NextDeadline() (deadline time.Time, ok bool) {
	consider := func(t time.Time) {
		if !ok || t.Before(deadline) {
			deadline, ok = t, true
		}
	}
	for c, ps := range m.down {
		if !ps.holdDelivered && (m.detectHold == nil || m.detectHold(c)) {
			consider(ps.downAt.Add(m.hold))
		}
	}
	for _, pp := range m.pending {
		consider(pp.deadline)
	}
	return deadline, ok
}

// Reset drops all in-flight press state — for a device replug or codec
// error that leaves the machine's idea of "what's down" untrustworthy.
func (m *gestureMachine) Reset() {
	m.down = make(map[model.Control]*pressState)
	m.lastPress = make(map[model.Control]time.Time)
	m.pending = make(map[model.Control]pendingPress)
}
