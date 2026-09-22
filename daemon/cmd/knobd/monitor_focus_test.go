package main

import (
	"strings"
	"testing"

	"github.com/njeske/knobd/internal/focus"
)

func TestFormatFocusInfo(t *testing.T) {
	cases := []struct {
		name string
		info focus.AppInfo
		want []string // substrings that must all be present
	}{
		{
			name: "brave with binary annotation",
			info: focus.AppInfo{ResourceClass: "brave-browser", PID: 3172, Binary: "brave"},
			want: []string{`resourceClass="brave-browser"`, "pid=3172", `binary="brave"`},
		},
		{
			name: "no binary, no caption",
			info: focus.AppInfo{ResourceClass: "konsole", PID: 100},
			want: []string{`resourceClass="konsole"`, "pid=100"},
		},
		{
			name: "with caption",
			info: focus.AppInfo{ResourceClass: "konsole", Caption: "a shell"},
			want: []string{`caption="a shell"`},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := formatFocusInfo(tc.info)
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Errorf("formatFocusInfo(%+v) = %q, want it to contain %q", tc.info, got, want)
				}
			}
		})
	}
}

func TestFormatFocusInfoOmitsEmptyBinaryAndCaption(t *testing.T) {
	got := formatFocusInfo(focus.AppInfo{ResourceClass: "konsole"})
	if strings.Contains(got, "binary=") || strings.Contains(got, "caption=") {
		t.Errorf("formatFocusInfo with no Binary/Caption = %q, want neither field present", got)
	}
}
