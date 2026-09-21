package model

import "fmt"

// TargetKind identifies what kind of thing an action acts on.
type TargetKind string

const (
	// TargetDefaultSink is whatever PipeWire currently considers the
	// default audio output.
	TargetDefaultSink TargetKind = "default_sink"
	// TargetSink is one specific output device, identified by Ref
	// (the PipeWire node.name).
	TargetSink TargetKind = "sink"
	// TargetDefaultSource is whatever PipeWire currently considers the
	// default input (microphone).
	TargetDefaultSource TargetKind = "default_source"
	// TargetSource is one specific input device, identified by Ref.
	TargetSource TargetKind = "source"
	// TargetApp resolves at event time to every stream matching the
	// AppMatcher whose ID is Ref. See AppMatcher for why this is a
	// matcher and not a stream ID: streams come and go, and some
	// applications (see testdata/pipewire/README.md) publish more than
	// one stream at once.
	TargetApp TargetKind = "app"
	// TargetGroup resolves to the union of every AppMatcher belonging to
	// the named AppGroup identified by Ref.
	TargetGroup TargetKind = "group"
	// TargetFocused resolves at event time to whatever application owns
	// the currently focused window, via the focus package (see
	// specs/adr/0003-focus-tracking-via-kwin-script.md). Never persists
	// as a fixed app — if you focus a different window, the same
	// binding now points somewhere else.
	TargetFocused TargetKind = "focused"
	// TargetAllStreams resolves to every audio stream currently playing,
	// i.e. a "master volume for applications" control distinct from the
	// sink's own hardware volume.
	TargetAllStreams TargetKind = "all_streams"
)

// TargetKinds returns every valid TargetKind, for validation and for
// daemon/internal/schema's enum generation.
func TargetKinds() []TargetKind {
	return []TargetKind{
		TargetDefaultSink, TargetSink, TargetDefaultSource, TargetSource,
		TargetApp, TargetGroup, TargetFocused, TargetAllStreams,
	}
}

// Target names what an Action operates on. Ref's meaning depends on
// Kind: empty for DefaultSink/DefaultSource/Focused/AllStreams, a
// PipeWire node.name for Sink/Source, and an AppMatcher.ID or
// AppGroup.ID for App/Group respectively.
type Target struct {
	Kind TargetKind `json:"kind"`
	Ref  string     `json:"ref,omitempty"`
}

// Validate reports whether Ref is present/absent as required by Kind.
func (t Target) Validate() error {
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
	need, known := needsRef[t.Kind]
	if !known {
		return fmt.Errorf("model: unknown target kind %q", t.Kind)
	}
	if need && t.Ref == "" {
		return fmt.Errorf("model: target kind %q requires a ref", t.Kind)
	}
	if !need && t.Ref != "" {
		return fmt.Errorf("model: target kind %q must not set a ref", t.Kind)
	}
	return nil
}

// AppMatcher identifies an application by whatever properties its audio
// streams actually expose — which, per testdata/pipewire/pw-dump-sample.json,
// is not consistently the same set of properties. A binding never points
// at a stream ID directly; it points at an AppMatcher.ID, and the audio
// package resolves that to zero or more live streams at dispatch time.
//
// At least one of the slice/regex fields should be non-empty, and a
// stream matches if it matches ANY of them (logical OR both across and
// within fields).
type AppMatcher struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`

	// Binaries matches application.process.binary case-insensitively,
	// e.g. "vesktop".
	Binaries []string `json:"binaries,omitempty"`
	// AppNames matches application.name case-insensitively, e.g. "Pal".
	AppNames []string `json:"appNames,omitempty"`
	// NodeNames matches node.name case-insensitively. Required as a
	// first-class key (rather than a fallback) because some streams —
	// observed live, see testdata/pipewire/pw-dump-sample.json id 112 —
	// expose no application.* properties at all, only node.name.
	NodeNames []string `json:"nodeNames,omitempty"`
	// DesktopIDs matches application.id (PipeWire) or a window's
	// resourceClass (from the focus package) case-insensitively, e.g.
	// "discord".
	DesktopIDs []string `json:"desktopIds,omitempty"`
	// MediaNameRx, if set, is a regular expression matched against
	// media.name. Unlike the other fields, this one is case-sensitive
	// (prefix the pattern with "(?i)" for case-insensitive matching) and
	// unanchored — "Pal" also matches "Palworld".
	//
	// Every other field above matches case-insensitively: the same
	// application can publish properties in inconsistent casing across
	// its streams (testdata/pipewire/pw-dump-sample.json alone has
	// "Brave"/"brave", "vesktop", and "Pal"), and matchers are hand-
	// written against whatever a user sees in pactl/pw-dump.
	MediaNameRx string `json:"mediaNameRx,omitempty"`
}

// Validate reports whether the matcher has an ID and at least one
// matching criterion.
func (m AppMatcher) Validate() error {
	if m.ID == "" {
		return fmt.Errorf("model: app matcher missing id")
	}
	if len(m.Binaries) == 0 && len(m.AppNames) == 0 && len(m.NodeNames) == 0 &&
		len(m.DesktopIDs) == 0 && m.MediaNameRx == "" {
		return fmt.Errorf("model: app matcher %q has no matching criteria", m.ID)
	}
	return nil
}

// AppGroup names a set of AppMatchers (by ID) that should be controlled
// together, e.g. "Voice Chat" = {vesktop, discord}.
type AppGroup struct {
	ID          string   `json:"id"`
	DisplayName string   `json:"displayName"`
	MatcherIDs  []string `json:"matcherIds"`
}
