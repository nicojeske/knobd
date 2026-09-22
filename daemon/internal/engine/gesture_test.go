package engine

import (
	"testing"
	"time"

	"github.com/njeske/knobd/internal/device"
	"github.com/njeske/knobd/internal/model"
)

const (
	testHold   = 600 * time.Millisecond
	testWindow = 350 * time.Millisecond
)

var push1 = model.Control{Kind: model.ControlEncoderPush, Index: 1}

// step is one input into the machine under test: either a device.Event
// (via Handle) or a bare tick at a point in time (via Tick).
type step struct {
	ev     *device.Event
	tickAt time.Time
	isTick bool
}

func down(c model.Control, at time.Time) step {
	return step{ev: &device.Event{Control: c, Kind: device.EventButtonDown, Time: at}}
}

func up(c model.Control, at time.Time) step {
	return step{ev: &device.Event{Control: c, Kind: device.EventButtonUp, Time: at}}
}

func tick(at time.Time) step {
	return step{isTick: true, tickAt: at}
}

func run(m *gestureMachine, steps []step) []gesture {
	var out []gesture
	for _, s := range steps {
		if s.isTick {
			out = append(out, m.Tick(s.tickAt)...)
		} else {
			out = append(out, m.Handle(*s.ev)...)
		}
	}
	return out
}

func gestureEqual(a, b []gesture) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		// At is deliberately excluded from comparison: it always equals
		// the triggering step's timestamp, which every test case already
		// controls explicitly by construction.
		if a[i].Control != b[i].Control || a[i].Gesture != b[i].Gesture ||
			a[i].Delta != b[i].Delta || a[i].Value != b[i].Value {
			return false
		}
	}
	return true
}

func TestGestureMachinePressVsHold(t *testing.T) {
	t0 := time.Unix(0, 0)

	cases := []struct {
		name  string
		steps []step
		want  []gesture
	}{
		{
			"599ms release is a press",
			[]step{down(push1, t0), up(push1, t0.Add(599*time.Millisecond))},
			[]gesture{{Control: push1, Gesture: model.GesturePress}},
		},
		{
			"exactly 600ms held is a hold, then release on the up",
			[]step{down(push1, t0), tick(t0.Add(testHold)), up(push1, t0.Add(700*time.Millisecond))},
			[]gesture{{Control: push1, Gesture: model.GestureHold}, {Control: push1, Gesture: model.GestureRelease}},
		},
		{
			"601ms held is a hold",
			[]step{down(push1, t0), tick(t0.Add(601 * time.Millisecond))},
			[]gesture{{Control: push1, Gesture: model.GestureHold}},
		},
		{
			"up with no prior down is ignored",
			[]step{up(push1, t0)},
			nil,
		},
		{
			"repeated ticks past the threshold fire the hold exactly once",
			[]step{down(push1, t0), tick(t0.Add(testHold)), tick(t0.Add(800 * time.Millisecond)), tick(t0.Add(900 * time.Millisecond))},
			[]gesture{{Control: push1, Gesture: model.GestureHold}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newGestureMachine(testHold, testWindow, nil)
			got := run(m, tc.steps)
			if !gestureEqual(got, tc.want) {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestGestureMachineDoublePress(t *testing.T) {
	t0 := time.Unix(0, 0)
	alwaysDefer := func(model.Control) bool { return true }

	cases := []struct {
		name       string
		deferPress func(model.Control) bool
		steps      []step
		want       []gesture
	}{
		{
			"two presses 300ms apart with deferral produce only a double press",
			alwaysDefer,
			[]step{
				down(push1, t0), up(push1, t0.Add(10*time.Millisecond)),
				tick(t0.Add(200 * time.Millisecond)), // window not yet up; nothing fires
				down(push1, t0.Add(300*time.Millisecond)), up(push1, t0.Add(310*time.Millisecond)),
			},
			[]gesture{{Control: push1, Gesture: model.GestureDoublePress}},
		},
		{
			"without deferral, two rapid presses each fire immediately",
			nil,
			[]step{
				down(push1, t0), up(push1, t0.Add(10*time.Millisecond)),
				down(push1, t0.Add(300*time.Millisecond)), up(push1, t0.Add(310*time.Millisecond)),
			},
			[]gesture{
				{Control: push1, Gesture: model.GesturePress},
				{Control: push1, Gesture: model.GesturePress},
			},
		},
		{
			"two presses 400ms apart (outside the window) are two presses",
			alwaysDefer,
			[]step{
				down(push1, t0), up(push1, t0.Add(10*time.Millisecond)),
				tick(t0.Add(10*time.Millisecond + testWindow)), // deferred press fires
				down(push1, t0.Add(400*time.Millisecond)), up(push1, t0.Add(410*time.Millisecond)),
				tick(t0.Add(410*time.Millisecond + testWindow)),
			},
			[]gesture{
				{Control: push1, Gesture: model.GesturePress},
				{Control: push1, Gesture: model.GesturePress},
			},
		},
		{
			// A real engine loop always ticks at NextDeadline(), so it
			// never skips the first press's own deferred-press deadline
			// (10ms+window) even though a second down happens first —
			// this scripts that same ordering explicitly.
			"a press followed by a hold within the window never doubles",
			alwaysDefer,
			[]step{
				down(push1, t0), up(push1, t0.Add(10*time.Millisecond)),
				down(push1, t0.Add(50*time.Millisecond)),
				tick(t0.Add(10*time.Millisecond + testWindow)), // press1's deferred press resolves
				tick(t0.Add(50*time.Millisecond + testHold)),   // press2 reaches the hold threshold
			},
			[]gesture{
				{Control: push1, Gesture: model.GesturePress},
				{Control: push1, Gesture: model.GestureHold},
			},
		},
		{
			"four rapid presses: double, double",
			alwaysDefer,
			[]step{
				down(push1, t0), up(push1, t0.Add(10*time.Millisecond)),
				down(push1, t0.Add(20*time.Millisecond)), up(push1, t0.Add(30*time.Millisecond)),
				down(push1, t0.Add(40*time.Millisecond)), up(push1, t0.Add(50*time.Millisecond)),
				down(push1, t0.Add(60*time.Millisecond)), up(push1, t0.Add(70*time.Millisecond)),
			},
			[]gesture{
				{Control: push1, Gesture: model.GestureDoublePress},
				{Control: push1, Gesture: model.GestureDoublePress},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newGestureMachine(testHold, testWindow, tc.deferPress)
			got := run(m, tc.steps)
			if !gestureEqual(got, tc.want) {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestGestureMachineTurnAndMove(t *testing.T) {
	enc1 := model.Control{Kind: model.ControlEncoder, Index: 1}
	fader := model.Control{Kind: model.ControlFader, Index: 1}
	m := newGestureMachine(testHold, testWindow, nil)

	got := m.Handle(device.Event{Control: enc1, Kind: device.EventTurn, Delta: 3, Time: time.Unix(0, 0)})
	want := []gesture{{Control: enc1, Gesture: model.GestureTurn, Delta: 3}}
	if !gestureEqual(got, want) {
		t.Errorf("turn: got %+v, want %+v", got, want)
	}

	got = m.Handle(device.Event{Control: fader, Kind: device.EventFaderMove, Value: 64, Time: time.Unix(0, 0)})
	want = []gesture{{Control: fader, Gesture: model.GestureMove, Value: 64}}
	if !gestureEqual(got, want) {
		t.Errorf("fader move: got %+v, want %+v", got, want)
	}
}

func TestGestureMachineNextDeadline(t *testing.T) {
	t0 := time.Unix(0, 0)
	push2 := model.Control{Kind: model.ControlEncoderPush, Index: 2}
	m := newGestureMachine(testHold, testWindow, nil)

	if _, ok := m.NextDeadline(); ok {
		t.Fatal("NextDeadline() ok = true with nothing in flight")
	}

	m.Handle(device.Event{Control: push1, Kind: device.EventButtonDown, Time: t0})
	m.Handle(device.Event{Control: push2, Kind: device.EventButtonDown, Time: t0.Add(100 * time.Millisecond)})

	deadline, ok := m.NextDeadline()
	if !ok {
		t.Fatal("NextDeadline() ok = false with two controls down")
	}
	want := t0.Add(testHold) // push1's hold deadline is earlier than push2's
	if !deadline.Equal(want) {
		t.Errorf("NextDeadline() = %v, want %v", deadline, want)
	}
}

func TestGestureMachineReset(t *testing.T) {
	m := newGestureMachine(testHold, testWindow, nil)
	m.Handle(device.Event{Control: push1, Kind: device.EventButtonDown, Time: time.Unix(0, 0)})
	if _, ok := m.NextDeadline(); !ok {
		t.Fatal("expected a pending deadline before Reset")
	}
	m.Reset()
	if _, ok := m.NextDeadline(); ok {
		t.Fatal("expected no pending deadline after Reset")
	}
}
