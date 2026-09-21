package main

import (
	"testing"

	"github.com/njeske/knobd/internal/audio"
)

func TestFormatAudioEvent(t *testing.T) {
	cases := []struct {
		name string
		ev   audio.Event
		want string
	}{
		{
			"stream changed with state",
			audio.Event{
				Kind:   audio.EventStreamChanged,
				Stream: &audio.Stream{ID: "118", Props: map[string]string{"application.name": "vesktop"}},
				State:  &audio.VolumeState{Percent: 80, Muted: false},
			},
			"stream 118 changed  application.name=vesktop  vol=80% muted=false",
		},
		{
			"stream removed carries last-known props",
			audio.Event{
				Kind:   audio.EventStreamRemoved,
				Stream: &audio.Stream{ID: "118", Props: map[string]string{"node.name": "vesktop"}},
			},
			"stream 118 removed  node.name=vesktop",
		},
		{
			"device changed",
			audio.Event{
				Kind:   audio.EventDeviceChanged,
				Device: &audio.Device{ID: "alsa_output.x", Description: "Speakers", IsDefault: true},
			},
			"device changed  " + formatDevice(audio.Device{ID: "alsa_output.x", Description: "Speakers", IsDefault: true}),
		},
		{
			"device removed",
			audio.Event{Kind: audio.EventDeviceRemoved, Device: &audio.Device{ID: "57"}},
			"device removed  " + formatDevice(audio.Device{ID: "57"}),
		},
		{
			"default changed",
			audio.Event{Kind: audio.EventDefaultChanged},
			"default sink/source changed",
		},
		{
			"resync",
			audio.Event{Kind: audio.EventResync},
			"-- resync: some events may have been missed, re-enumerating recommended",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatAudioEvent(tc.ev); got != tc.want {
				t.Errorf("formatAudioEvent(%+v) = %q, want %q", tc.ev, got, tc.want)
			}
		})
	}
}

func TestFormatStreamPropsIsDeterministic(t *testing.T) {
	s := audio.Stream{Props: map[string]string{
		"node.name":         "vesktop",
		"application.name":  "vesktop",
		"media.name":        "Playback",
		"unrelated.ignored": "x",
	}}
	want := "application.name=vesktop node.name=vesktop media.name=Playback"
	for i := 0; i < 5; i++ {
		if got := formatStreamProps(&s); got != want {
			t.Fatalf("formatStreamProps() = %q, want %q (run %d)", got, want, i)
		}
	}
}

func TestFormatDeviceMarksDefault(t *testing.T) {
	d := audio.Device{ID: "sink1", Description: "Speakers", IsDefault: true}
	got := formatDevice(d)
	if got == formatDevice(audio.Device{ID: "sink1", Description: "Speakers"}) {
		t.Error("formatDevice should distinguish a default device from a non-default one")
	}
}
