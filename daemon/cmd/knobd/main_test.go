package main

import (
	"reflect"
	"testing"
)

func TestParseArgs(t *testing.T) {
	cases := []struct {
		name     string
		argv     []string
		wantCmd  string
		wantRest []string
	}{
		{"no args runs the daemon", nil, "", nil},
		{"a flag with no subcommand still runs the daemon", []string{"--log-level", "debug"}, "", []string{"--log-level", "debug"}},
		{"single-dash flag also runs the daemon", []string{"-log-level", "debug"}, "", []string{"-log-level", "debug"}},
		{"monitor dispatches", []string{"monitor"}, "monitor", []string{}},
		{"monitor with flags dispatches and keeps them", []string{"monitor", "--raw"}, "monitor", []string{"--raw"}},
		{"monitor-audio dispatches", []string{"monitor-audio"}, "monitor-audio", []string{}},
		{"monitor-audio with flags dispatches and keeps them", []string{"monitor-audio", "--once"}, "monitor-audio", []string{"--once"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd, rest := parseArgs(tc.argv)
			if cmd != tc.wantCmd {
				t.Errorf("cmd = %q, want %q", cmd, tc.wantCmd)
			}
			if !reflect.DeepEqual(rest, tc.wantRest) && !(len(rest) == 0 && len(tc.wantRest) == 0) {
				t.Errorf("rest = %#v, want %#v", rest, tc.wantRest)
			}
		})
	}
}
