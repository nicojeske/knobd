package schema

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/njeske/knobd/internal/device"
	"github.com/njeske/knobd/internal/model"
)

// TestGenerateDeviceLayoutDeterministic mirrors TestGenerateDeterministic.
func TestGenerateDeviceLayoutDeterministic(t *testing.T) {
	a, err := GenerateDeviceLayout()
	if err != nil {
		t.Fatalf("GenerateDeviceLayout: %v", err)
	}
	b, err := GenerateDeviceLayout()
	if err != nil {
		t.Fatalf("GenerateDeviceLayout: %v", err)
	}
	if !bytes.Equal(a, b) {
		t.Error("GenerateDeviceLayout() produced different output across two calls in the same process")
	}
}

// TestGenerateDeviceLayoutMatchesCommitted is the drift gate: CI runs
// `make schema` and diffs docs/device-layout.json.
func TestGenerateDeviceLayoutMatchesCommitted(t *testing.T) {
	got, err := GenerateDeviceLayout()
	if err != nil {
		t.Fatalf("GenerateDeviceLayout: %v", err)
	}
	want, err := os.ReadFile("../../../docs/device-layout.json")
	if err != nil {
		t.Fatalf("read docs/device-layout.json (run `make schema` from the repo root after a model change): %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Error("docs/device-layout.json is out of date; run `make schema` from the repo root to regenerate it")
	}
}

// TestDeviceLayoutMatchesModel checks the generated document agrees with
// model.Control.Validate, model.ControlKind.SupportsGesture, and
// model.ControlKind.HasLED for every kind -- the source of truth this
// document is derived from.
func TestDeviceLayoutMatchesModel(t *testing.T) {
	layout, err := generateDeviceLayoutStruct()
	if err != nil {
		t.Fatalf("generateDeviceLayoutStruct: %v", err)
	}

	if layout.RingPositions != device.MaxRingPosition {
		t.Errorf("RingPositions = %d, want %d (device.MaxRingPosition)", layout.RingPositions, device.MaxRingPosition)
	}

	kinds := model.ControlKinds()
	if len(layout.Kinds) != len(kinds) {
		t.Fatalf("Kinds has %d entries, model.ControlKinds() has %d", len(layout.Kinds), len(kinds))
	}

	byKind := make(map[model.ControlKind]ControlKindLayout, len(layout.Kinds))
	for _, k := range layout.Kinds {
		byKind[k.Kind] = k
	}

	for _, kind := range kinds {
		entry, ok := byKind[kind]
		if !ok {
			t.Errorf("no device-layout entry for kind %q", kind)
			continue
		}

		wantMin, wantMax, ok := model.ControlIndexRange(kind)
		if !ok {
			t.Fatalf("model.ControlIndexRange(%q) missing despite ControlKinds() listing it", kind)
		}
		if entry.MinIndex != wantMin || entry.MaxIndex != wantMax {
			t.Errorf("%s: index range [%d,%d], want [%d,%d]", kind, entry.MinIndex, entry.MaxIndex, wantMin, wantMax)
		}

		if entry.HasLED != kind.HasLED() {
			t.Errorf("%s: hasLed = %v, want %v", kind, entry.HasLED, kind.HasLED())
		}

		for _, g := range model.Gestures() {
			want := kind.SupportsGesture(g)
			got := false
			for _, eg := range entry.Gestures {
				if eg == g {
					got = true
					break
				}
			}
			if got != want {
				t.Errorf("%s: gesture %q listed = %v, want %v (SupportsGesture)", kind, g, got, want)
			}
		}
	}
}

// generateDeviceLayoutStruct is GenerateDeviceLayout, unmarshaled back
// into DeviceLayout, so tests can assert on the struct rather than
// re-parsing raw JSON.
func generateDeviceLayoutStruct() (DeviceLayout, error) {
	data, err := GenerateDeviceLayout()
	if err != nil {
		return DeviceLayout{}, err
	}
	var layout DeviceLayout
	if err := json.Unmarshal(data, &layout); err != nil {
		return DeviceLayout{}, err
	}
	return layout, nil
}
