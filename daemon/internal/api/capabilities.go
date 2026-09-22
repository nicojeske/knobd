package api

import "github.com/njeske/knobd/internal/model"

// Capabilities is the GET /capabilities response: what this daemon
// build actually supports, as opposed to what model.ActionTypes() and
// model.TargetKinds() merely declare exist. actions.Registry is
// assembled at daemon startup (cmd/knobd/main.go), so only the running
// daemon knows which action types have a Handler -- as of this writing,
// 5 of model.ActionTypes()'s 21. This is what lets the UI badge an
// unimplemented action, or a target kind that doesn't resolve yet,
// with a reason that disappears on its own as later milestones register
// more handlers, instead of a hand-kept TypeScript list going stale.
type Capabilities struct {
	ImplementedActions   []model.ActionType `json:"implementedActions"`
	SupportedTargetKinds []model.TargetKind `json:"supportedTargetKinds"`
	Features             Features           `json:"features"`
}

// Features are optional daemon-wide capabilities that aren't per-action
// or per-target-kind.
type Features struct {
	// Layers is true once model.Binding.Layer values other than 0 are
	// actually reachable at runtime (M08); until then every binding
	// effectively runs on layer 0.
	Layers bool `json:"layers"`
	// Scenes is true once scene.apply/scene.save have registered
	// handlers (M08).
	Scenes bool `json:"scenes"`
	// Learn is true once MIDI learn (POST/DELETE /learn) is wired up.
	Learn bool `json:"learn"`
}

// CapabilitiesProvider supplies GET /capabilities. Unlike ConfigStore/
// StateProvider/AudioProvider this never fails and never blocks: it
// reports on daemon-startup-time wiring (which handlers got
// registered), not on any external system.
type CapabilitiesProvider interface {
	Capabilities() Capabilities
}
