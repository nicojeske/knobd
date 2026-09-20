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
