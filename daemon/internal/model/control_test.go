package model

import "testing"

func TestControlValidate(t *testing.T) {
	cases := []struct {
		name    string
		c       Control
		wantErr bool
	}{
		{"encoder in range", Control{ControlEncoder, 1}, false},
		{"encoder upper bound", Control{ControlEncoder, 8}, false},
		{"encoder zero", Control{ControlEncoder, 0}, true},
		{"encoder too high", Control{ControlEncoder, 9}, true},
		{"button in range", Control{ControlButton, 16}, false},
		{"button too high", Control{ControlButton, 17}, true},
		{"side button in range", Control{ControlSideButton, 2}, false},
		{"side button too high", Control{ControlSideButton, 3}, true},
		{"fader must be 1", Control{ControlFader, 1}, false},
		{"fader index 2 invalid", Control{ControlFader, 2}, true},
		{"unknown kind", Control{ControlKind("bogus"), 1}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.c.Validate()
			if (err != nil) != tc.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestControlKindSupportsGesture(t *testing.T) {
	if !ControlEncoder.SupportsGesture(GestureTurn) {
		t.Error("encoder should support turn")
	}
	if ControlEncoder.SupportsGesture(GesturePress) {
		t.Error("encoder should not support press")
	}
	if !ControlButton.SupportsGesture(GesturePress) {
		t.Error("button should support press")
	}
	if !ControlButton.SupportsGesture(GestureHold) {
		t.Error("button should support hold")
	}
	if ControlButton.SupportsGesture(GestureTurn) {
		t.Error("button should not support turn")
	}
	if ControlFader.SupportsGesture(GesturePress) {
		t.Error("fader should not support any gesture yet")
	}
}

// TestControlKindsComplete guards against ControlKinds() silently
// falling out of sync with the kinds Control.Validate actually accepts
// (schema generation trusts ControlKinds() as the full enum).
func TestControlKindsComplete(t *testing.T) {
	for _, k := range ControlKinds() {
		if err := (Control{Kind: k, Index: 1}).Validate(); err != nil && k != ControlFader {
			t.Errorf("Control{Kind: %q, Index: 1}.Validate() = %v, want nil", k, err)
		}
	}
	if err := (Control{Kind: ControlKind("bogus"), Index: 1}).Validate(); err == nil {
		t.Error("a kind absent from ControlKinds() should fail Validate")
	}
}

// TestGesturesComplete guards ditto for Gestures() against
// ControlKind.SupportsGesture.
func TestGesturesComplete(t *testing.T) {
	gestures := Gestures()
	if len(gestures) != 5 {
		t.Fatalf("Gestures() returned %d gestures, want 5 (update this test if the set changed intentionally)", len(gestures))
	}
	if ControlButton.SupportsGesture(Gesture("bogus")) {
		t.Error("a gesture absent from Gestures() should not be supported by any control kind")
	}
}
