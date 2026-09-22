package schema

import (
	"encoding/json"
	"fmt"

	"github.com/njeske/knobd/internal/device"
	"github.com/njeske/knobd/internal/model"
)

// DeviceLayout is docs/device-layout.json's shape: the hardware facts
// ui/'s visual panel and binding editor need that model.Control.Validate
// and model.ControlKind.SupportsGesture/HasLED otherwise only express as
// Go code -- so the UI doesn't hand-duplicate the index ranges and
// gesture-validity matrix (config.schema.json can't express either; see
// specs/milestones/M07-config-ui.md). Plain data, not a JSON Schema, so
// it's marshaled directly rather than reflected.
type DeviceLayout struct {
	Kinds []ControlKindLayout `json:"kinds"`
	// RingPositions is the highest addressable encoder-ring LED position
	// (see device.MaxRingPosition); the ring supports positions 0..N
	// inclusive.
	RingPositions int `json:"ringPositions"`
}

// ControlKindLayout is one model.ControlKind's hardware facts.
type ControlKindLayout struct {
	Kind     model.ControlKind `json:"kind"`
	MinIndex int               `json:"minIndex"`
	MaxIndex int               `json:"maxIndex"`
	HasLED   bool              `json:"hasLed"`
	// Gestures is every model.Gesture this kind supports, in
	// model.Gestures() order.
	Gestures []model.Gesture `json:"gestures"`
}

// GenerateDeviceLayout returns docs/device-layout.json as indented JSON
// with a trailing newline, matching Generate()'s style. Deterministic:
// model.ControlKinds() and model.Gestures() both return fixed slices.
func GenerateDeviceLayout() ([]byte, error) {
	layout := DeviceLayout{RingPositions: device.MaxRingPosition}

	for _, kind := range model.ControlKinds() {
		min, max, ok := model.ControlIndexRange(kind)
		if !ok {
			// Unreachable: kind came from model.ControlKinds(), which
			// ControlIndexRange's switch covers exhaustively.
			panic(fmt.Sprintf("schema: device-layout: ControlIndexRange(%q) missing despite ControlKinds() listing it", kind))
		}

		var gestures []model.Gesture
		for _, g := range model.Gestures() {
			if kind.SupportsGesture(g) {
				gestures = append(gestures, g)
			}
		}

		layout.Kinds = append(layout.Kinds, ControlKindLayout{
			Kind:     kind,
			MinIndex: min,
			MaxIndex: max,
			HasLED:   kind.HasLED(),
			Gestures: gestures,
		})
	}

	data, err := json.MarshalIndent(layout, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("schema: device-layout: marshal: %w", err)
	}
	return append(data, '\n'), nil
}
