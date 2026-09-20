package model

import "testing"

func TestTargetValidate(t *testing.T) {
	cases := []struct {
		name    string
		target  Target
		wantErr bool
	}{
		{"default sink no ref", Target{Kind: TargetDefaultSink}, false},
		{"default sink with ref", Target{Kind: TargetDefaultSink, Ref: "x"}, true},
		{"sink requires ref", Target{Kind: TargetSink}, true},
		{"sink with ref", Target{Kind: TargetSink, Ref: "alsa_output.x"}, false},
		{"app requires ref", Target{Kind: TargetApp}, true},
		{"group requires ref", Target{Kind: TargetGroup}, true},
		{"focused no ref", Target{Kind: TargetFocused}, false},
		{"all streams no ref", Target{Kind: TargetAllStreams}, false},
		{"unknown kind", Target{Kind: TargetKind("bogus")}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.target.Validate()
			if (err != nil) != tc.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

// TestTargetKindsComplete guards against TargetKinds() silently falling
// out of sync with the kinds Target.Validate actually recognizes (schema
// generation trusts TargetKinds() as the full enum).
func TestTargetKindsComplete(t *testing.T) {
	needsRef := map[TargetKind]bool{
		TargetDefaultSink:   false,
		TargetSink:          true,
		TargetDefaultSource: false,
		TargetSource:        true,
		TargetApp:           true,
		TargetGroup:         true,
		TargetFocused:       false,
		TargetAllStreams:    false,
	}
	kinds := TargetKinds()
	if len(kinds) != len(needsRef) {
		t.Fatalf("TargetKinds() has %d entries, Target.Validate recognizes %d; keep both lists in sync",
			len(kinds), len(needsRef))
	}
	for _, k := range kinds {
		if _, ok := needsRef[k]; !ok {
			t.Errorf("TargetKinds() includes %q, which Target.Validate does not recognize", k)
		}
	}
	if err := (Target{Kind: TargetKind("bogus")}).Validate(); err == nil {
		t.Error("a kind absent from TargetKinds() should fail Validate")
	}
}
