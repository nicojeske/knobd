package model

import (
	"encoding/json"
	"testing"
)

func TestBindingJSONRoundTrip(t *testing.T) {
	want := Binding{
		Layer:   0,
		Control: Control{Kind: ControlEncoder, Index: 3},
		Gesture: GestureTurn,
		Action: VolumeAdjustAction{
			Target:      Target{Kind: TargetApp, Ref: "vesktop"},
			StepPercent: 2,
		},
	}

	data, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got Binding
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if got != want {
		t.Errorf("round trip mismatch: got %#v, want %#v (json: %s)", got, want, data)
	}
}

func TestBindingUnmarshalUnknownActionType(t *testing.T) {
	data := []byte(`{
		"layer": 0,
		"control": {"kind": "button", "index": 1},
		"gesture": "press",
		"action": {"type": "does.not.exist", "params": {}}
	}`)
	var b Binding
	if err := json.Unmarshal(data, &b); err == nil {
		t.Fatal("expected error unmarshaling binding with unknown action type")
	}
}

func TestBindingValidate(t *testing.T) {
	cases := []struct {
		name    string
		b       Binding
		wantErr bool
	}{
		{
			name: "valid encoder turn",
			b: Binding{
				Control: Control{Kind: ControlEncoder, Index: 1},
				Gesture: GestureTurn,
				Action:  VolumeAdjustAction{Target: Target{Kind: TargetFocused}, StepPercent: 2},
			},
			wantErr: false,
		},
		{
			name: "encoder cannot press",
			b: Binding{
				Control: Control{Kind: ControlEncoder, Index: 1},
				Gesture: GesturePress,
				Action:  VolumeMuteToggleAction{Target: Target{Kind: TargetFocused}},
			},
			wantErr: true,
		},
		{
			name: "invalid control index",
			b: Binding{
				Control: Control{Kind: ControlButton, Index: 99},
				Gesture: GesturePress,
				Action:  KnobClearAction{},
			},
			wantErr: true,
		},
		{
			name: "nil action",
			b: Binding{
				Control: Control{Kind: ControlButton, Index: 1},
				Gesture: GesturePress,
				Action:  nil,
			},
			wantErr: true,
		},
		{
			name: "layer.momentary on hold is valid",
			b: Binding{
				Control: Control{Kind: ControlSideButton, Index: 1},
				Gesture: GestureHold,
				Action:  LayerMomentaryAction{Layer: 1},
			},
			wantErr: false,
		},
		{
			name: "layer.momentary on press is invalid",
			b: Binding{
				Control: Control{Kind: ControlSideButton, Index: 1},
				Gesture: GesturePress,
				Action:  LayerMomentaryAction{Layer: 1},
			},
			wantErr: true,
		},
		{
			name: "audio.duck_hold on hold is valid",
			b: Binding{
				Control: Control{Kind: ControlButton, Index: 1},
				Gesture: GestureHold,
				Action:  AudioDuckHoldAction{Target: Target{Kind: TargetFocused}, DuckPercent: 20},
			},
			wantErr: false,
		},
		{
			name: "audio.duck_hold on press is invalid",
			b: Binding{
				Control: Control{Kind: ControlButton, Index: 1},
				Gesture: GesturePress,
				Action:  AudioDuckHoldAction{Target: Target{Kind: TargetFocused}, DuckPercent: 20},
			},
			wantErr: true,
		},
		{
			name: "layer.latch on press is valid (no gesture restriction)",
			b: Binding{
				Control: Control{Kind: ControlSideButton, Index: 1},
				Gesture: GesturePress,
				Action:  LayerLatchAction{Layer: 1},
			},
			wantErr: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.b.Validate()
			if (err != nil) != tc.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}
