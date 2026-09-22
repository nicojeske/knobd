package focus

import "testing"

func TestDecodeScriptEventFullPayload(t *testing.T) {
	payload := `{"v":1,"normal":true,"resourceClass":"brave-browser","desktopFileId":"brave-browser","caption":"some page","pid":3172}`
	ev, err := decodeScriptEvent(payload)
	if err != nil {
		t.Fatalf("decodeScriptEvent: %v", err)
	}
	want := scriptEvent{V: 1, Normal: true, ResourceClass: "brave-browser", DesktopFileID: "brave-browser", Caption: "some page", PID: 3172}
	if ev != want {
		t.Fatalf("decodeScriptEvent(%q) = %+v, want %+v", payload, ev, want)
	}
}

func TestDecodeScriptEventNullWindowPayload(t *testing.T) {
	// Exactly what describe(w) emits in the script for a null w.
	payload := `{"v":1,"normal":false,"resourceClass":"","desktopFileId":"","caption":"","pid":0}`
	ev, err := decodeScriptEvent(payload)
	if err != nil {
		t.Fatalf("decodeScriptEvent: %v", err)
	}
	if ev.Normal || ev.ResourceClass != "" || ev.PID != 0 {
		t.Fatalf("decodeScriptEvent(%q) = %+v, want all-zero", payload, ev)
	}
}

func TestDecodeScriptEventMissingKeysDecodeToZeroValues(t *testing.T) {
	ev, err := decodeScriptEvent(`{"resourceClass":"konsole"}`)
	if err != nil {
		t.Fatalf("decodeScriptEvent: %v", err)
	}
	if ev.ResourceClass != "konsole" || ev.Normal != false || ev.PID != 0 {
		t.Fatalf("decodeScriptEvent with missing keys = %+v, want zero values for the rest", ev)
	}
}

func TestDecodeScriptEventPIDAsStringIsRejected(t *testing.T) {
	// The script always emits pid as a JSON number; a string here would
	// mean something is very wrong (a hand-edited payload, or a script
	// bug) and should fail loudly rather than silently coerce.
	if _, err := decodeScriptEvent(`{"pid":"3172"}`); err == nil {
		t.Fatal("decodeScriptEvent with pid as a string should have errored")
	}
}

func TestDecodeScriptEventMalformedJSON(t *testing.T) {
	if _, err := decodeScriptEvent(`{not json`); err == nil {
		t.Fatal("decodeScriptEvent with malformed JSON should have errored")
	}
}

func TestAcceptEvent(t *testing.T) {
	cases := []struct {
		name string
		ev   scriptEvent
		want bool
	}{
		{"normal window with resourceClass", scriptEvent{Normal: true, ResourceClass: "konsole"}, true},
		{"normal window with only desktopFileId", scriptEvent{Normal: true, DesktopFileID: "org.kde.konsole"}, true},
		{"not normal (a panel/dock)", scriptEvent{Normal: false, ResourceClass: "plasmashell"}, false},
		{"normal but no identifying field at all", scriptEvent{Normal: true}, false},
		{"normal but an ignored shell resourceClass", scriptEvent{Normal: true, ResourceClass: "plasmashell"}, false},
		{"normal but krunner", scriptEvent{Normal: true, ResourceClass: "krunner"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := acceptEvent(tc.ev); got != tc.want {
				t.Errorf("acceptEvent(%+v) = %v, want %v", tc.ev, got, tc.want)
			}
		})
	}
}

func TestScriptEventAppInfo(t *testing.T) {
	ev := scriptEvent{ResourceClass: "brave-browser", DesktopFileID: "brave-browser", Caption: "a page", PID: 3172}
	got := ev.appInfo()
	want := AppInfo{PID: 3172, ResourceClass: "brave-browser", DesktopFileID: "brave-browser", Caption: "a page"}
	if got != want {
		t.Fatalf("appInfo() = %+v, want %+v", got, want)
	}
}
