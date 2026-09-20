package main

import (
	"testing"
	"time"

	"github.com/njeske/knobd/internal/device"
	"github.com/njeske/knobd/internal/midi"
	"github.com/njeske/knobd/internal/model"
)

func TestFormatEvent(t *testing.T) {
	cases := []struct {
		name string
		ev   device.Event
		want string
	}{
		{
			"turn",
			device.Event{Control: model.Control{Kind: model.ControlEncoder, Index: 3}, Kind: device.EventTurn, Delta: -2},
			"encoder 3        turn    -2",
		},
		{
			"button down",
			device.Event{Control: model.Control{Kind: model.ControlButton, Index: 11}, Kind: device.EventButtonDown},
			"button 11        down",
		},
		{
			"button up",
			device.Event{Control: model.Control{Kind: model.ControlButton, Index: 11}, Kind: device.EventButtonUp},
			"button 11        up",
		},
		{
			"fader",
			device.Event{Control: model.Control{Kind: model.ControlFader, Index: 1}, Kind: device.EventFaderMove, Value: 97},
			"fader 1          move    97",
		},
		{
			"encoder push down",
			device.Event{Control: model.Control{Kind: model.ControlEncoderPush, Index: 2}, Kind: device.EventButtonDown},
			"encoder push 2   down",
		},
		{
			"side button down",
			device.Event{Control: model.Control{Kind: model.ControlSideButton, Index: 1}, Kind: device.EventButtonDown},
			"side button 1    down",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatEvent(tc.ev); got != tc.want {
				t.Errorf("formatEvent(%+v) = %q, want %q", tc.ev, got, tc.want)
			}
		})
	}
}

func TestPrintEventTracksHeldDuration(t *testing.T) {
	codec := device.NewXTouchMiniCodec()
	downAt := make(map[model.Control]time.Time)

	t0 := time.Now()
	printEvent(codec, midi.Message{Status: 0x90, Data1: 89, Data2: 127, Time: t0}, downAt, false)

	ctrl := model.Control{Kind: model.ControlButton, Index: 1}
	if _, ok := downAt[ctrl]; !ok {
		t.Fatal("expected downAt to record the press")
	}

	printEvent(codec, midi.Message{Status: 0x90, Data1: 89, Data2: 0, Time: t0.Add(250 * time.Millisecond)}, downAt, false)
	if _, ok := downAt[ctrl]; ok {
		t.Error("expected downAt to be cleared after the release")
	}
}
