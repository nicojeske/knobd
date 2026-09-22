package actions

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/njeske/knobd/internal/focus"
	"github.com/njeske/knobd/internal/model"
)

// defaultAssignStepPercent matches model.Default()'s starter encoder
// bindings, so a freshly-assigned encoder behaves the same as the
// hand-picked defaults unless the triggering action says otherwise.
const defaultAssignStepPercent = 2

// maxAssignStepPercent bounds AssignOptions.DefaultStepPercent and a
// KnobAssignFocusedAppAction's own StepPercent alike -- a runaway value
// (a config typo, or a future UI bug) shouldn't be able to make a single
// detent swing volume by more than half in one step.
const maxAssignStepPercent = 50

// ConfigMutator is the slice of cmd/knobd's configStore this handler
// needs -- a point-of-use interface, so actions never imports cmd/knobd.
// *configStore already satisfies it as written (see
// daemon/cmd/knobd/configstore.go).
type ConfigMutator interface {
	Config() model.Config
	SetConfig(ctx context.Context, cfg model.Config) error
}

// FocusReporter is the slice of focus.Provider this handler needs --
// just enough to read the currently focused app, never Watch or Close.
type FocusReporter interface {
	Current(ctx context.Context) (focus.AppInfo, error)
}

// AssignOptions configures AssignHandlers.
type AssignOptions struct {
	// DefaultStepPercent is used when the triggering action carries no
	// StepPercent of its own (<= 0). <= 0 here means
	// defaultAssignStepPercent.
	DefaultStepPercent float64
	Logger             *slog.Logger
	// OnAssigned fires only after the new config is durably persisted
	// via ConfigMutator.SetConfig -- M05's deferred LED-confirmation
	// seam (see engine.Engine.FlashControl). Control is the encoder
	// that was rebound (ControlEncoder), not the encoder_push that
	// triggered the assignment.
	OnAssigned func(control model.Control, matcher model.AppMatcher)
}

func (o AssignOptions) defaultStepPercent() float64 {
	if o.DefaultStepPercent <= 0 {
		return defaultAssignStepPercent
	}
	return o.DefaultStepPercent
}

func (o AssignOptions) logger() *slog.Logger {
	if o.Logger == nil {
		return slog.Default()
	}
	return o.Logger
}

// AssignHandlers implements model.ActionKnobAssignFocusedApp: on a
// GestureHold of an encoder-push, it reads the currently focused
// application, creates or reuses a model.AppMatcher for it, rewrites
// the corresponding encoder's turn binding to a fresh VolumeAdjustAction
// targeting that matcher, and persists the result.
//
// This handler authors config; it does not act on audio and never
// re-resolves a Target of its own -- KnobAssignFocusedAppAction carries
// no Target at all (model.TargetOf returns false for it), so
// Invocation's "a Handler must use Refs, never re-resolve Target" rule
// (see registry.go's doc comment) simply doesn't apply here.
type AssignHandlers struct {
	store ConfigMutator
	focus FocusReporter
	opts  AssignOptions
}

// NewAssignHandlers returns handlers that read the focused app through
// f and read/write configuration through store.
func NewAssignHandlers(store ConfigMutator, f FocusReporter, opts AssignOptions) *AssignHandlers {
	return &AssignHandlers{store: store, focus: f, opts: opts}
}

// Register wires this handler into r. Must be called before the
// engine's Run goroutine starts dispatching -- Registry.Register is not
// safe to call concurrently with Registry.Execute (see registry.go),
// and every handler in this codebase is registered once at startup
// before Engine.Run, mirroring VolumeHandlers.Register.
func (h *AssignHandlers) Register(r *Registry) {
	r.Register(model.ActionKnobAssignFocusedApp, HandlerFunc(h.execute))
}

func (h *AssignHandlers) execute(ctx context.Context, inv Invocation) error {
	action, ok := inv.Action.(model.KnobAssignFocusedAppAction)
	if !ok {
		return fmt.Errorf("actions: knob.assign_focused_app got %T", inv.Action)
	}

	if inv.Control.Kind != model.ControlEncoderPush {
		return fmt.Errorf("actions: knob.assign_focused_app: %s cannot be assigned (only an encoder_push's hold can)", inv.Control.Kind)
	}

	info, err := h.focus.Current(ctx)
	if err != nil {
		return fmt.Errorf("actions: knob.assign_focused_app: get focused app: %w", err)
	}

	step := action.StepPercent
	if step <= 0 {
		step = h.opts.defaultStepPercent()
	}
	if step > maxAssignStepPercent {
		step = maxAssignStepPercent
	}

	cfg := h.store.Config()
	next, encoder, matcher, err := applyAssignment(cfg, inv.Layer, inv.Control, info, step)
	if err != nil {
		return fmt.Errorf("actions: knob.assign_focused_app: %w", err)
	}

	// Bounds only the engine round trip inside SetConfig
	// (engine.Engine.SetConfig is a channel round trip); config.Save's
	// disk write is plain file I/O and does not observe ctx at all.
	// Synchronous on this call (the dispatcher goroutine), deliberately
	// not backgrounded -- see this file's package-level doc note below
	// for why: read-modify-write atomicity across two rapid assignments
	// depends on it, the same argument dispatch.go's serialization
	// already rests on for every audio.Backend call.
	persistCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := h.store.SetConfig(persistCtx, next); err != nil {
		return fmt.Errorf("actions: knob.assign_focused_app: persist: %w", err)
	}

	h.opts.logger().Info("assigned focused app to a knob", "control", encoder, "matcher", matcher.ID)
	if h.opts.OnAssigned != nil {
		h.opts.OnAssigned(encoder, matcher)
	}
	return nil
}

// applyAssignment is the whole decision as a pure function: no I/O, and
// -- critically -- no mutation of cfg's own backing slices. cfg's
// Profiles/Bindings/AppMatchers slices alias whatever configStore is
// currently holding (and the engine's bindingIndex holds model.Action
// values out of that same backing array), so mutating in place would
// edit the live running config before validation and before persist,
// and leave it edited even if SetConfig then failed. Every slice this
// touches is cloned before it's touched; see assign_test.go's aliasing
// test.
//
// push must be a ControlEncoderPush; the binding actually rewritten is
// the GestureTurn binding on the ControlEncoder of the same Index, on
// layer -- the encoder's ring/turn is what a knob "being assigned to an
// app" means, not the push itself (which stays bound to this same
// assign action, so holding it again reassigns).
func applyAssignment(cfg model.Config, layer int, push model.Control, info focus.AppInfo, step float64) (model.Config, model.Control, model.AppMatcher, error) {
	if push.Kind != model.ControlEncoderPush {
		return model.Config{}, model.Control{}, model.AppMatcher{}, fmt.Errorf("assign target must be an encoder_push, got %s", push.Kind)
	}
	encoder := model.Control{Kind: model.ControlEncoder, Index: push.Index}

	tokens := info.NameCandidates()
	if len(tokens) == 0 {
		return model.Config{}, model.Control{}, model.AppMatcher{}, fmt.Errorf(
			"no identifying information for the focused app (resourceClass/desktopFileId/binary all empty)")
	}

	matcherID, matcher, matchers := findOrCreateMatcher(cfg.AppMatchers, tokens, info.DisplayName())

	profiles := append([]model.Profile(nil), cfg.Profiles...)
	profileIdx := -1
	for i, p := range profiles {
		if p.ID == cfg.ActiveProfileID {
			profileIdx = i
			break
		}
	}
	if profileIdx == -1 {
		return model.Config{}, model.Control{}, model.AppMatcher{}, fmt.Errorf("no active profile %q to rebind", cfg.ActiveProfileID)
	}
	active := profiles[profileIdx]
	bindings := append([]model.Binding(nil), active.Bindings...)

	newBinding := model.Binding{
		Layer:   layer,
		Control: encoder,
		Gesture: model.GestureTurn,
		Action: model.VolumeAdjustAction{
			Target:      model.Target{Kind: model.TargetApp, Ref: matcherID},
			StepPercent: step,
		},
	}

	replaced := false
	for i, b := range bindings {
		if b.Layer == layer && b.Control == encoder && b.Gesture == model.GestureTurn {
			bindings[i] = newBinding
			replaced = true
			break
		}
	}
	if !replaced {
		bindings = append(bindings, newBinding)
	}

	active.Bindings = bindings
	profiles[profileIdx] = active

	out := cfg
	out.Profiles = profiles
	out.AppMatchers = matchers

	if err := out.Validate(); err != nil {
		return model.Config{}, model.Control{}, model.AppMatcher{}, fmt.Errorf("resulting config invalid: %w", err)
	}

	return out, encoder, matcher, nil
}

// findOrCreateMatcher decides which model.AppMatcher tokens should bind
// to: reuse the first existing matcher whose criteria already overlap
// one of tokens (without modifying it -- never silently rewrite a
// hand-tuned matcher), which is what makes reassigning the same app a
// no-op for AppMatchers: a matcher this function created previously has
// its own Binaries/AppNames/NodeNames/DesktopIDs set to that same token
// list, so it trivially overlaps a fresh computation for the same app.
// Only once nothing overlaps does ID even enter the decision, and then
// only to avoid colliding with an unrelated matcher that happens to use
// the same slug -- an ID match with NO criteria overlap is a collision
// to disambiguate, not a reuse signal (see assign_test.go's ID-collision
// case).
func findOrCreateMatcher(matchers []model.AppMatcher, tokens []string, displayName string) (id string, matcher model.AppMatcher, out []model.AppMatcher) {
	for _, m := range matchers {
		if matcherOverlapsTokens(m, tokens) {
			return m.ID, m, matchers
		}
	}

	base := displayName
	if base == "" {
		base = tokens[0]
	}
	candidateID := slugify(base)
	if candidateID == "" {
		candidateID = "app"
	}

	id = candidateID
	for i := 2; matcherIDExists(matchers, id); i++ {
		id = fmt.Sprintf("%s-%d", candidateID, i)
	}
	dn := displayName
	if dn == "" {
		dn = id
	}
	m := model.AppMatcher{
		ID:          id,
		DisplayName: dn,
		Binaries:    append([]string(nil), tokens...),
		AppNames:    append([]string(nil), tokens...),
		NodeNames:   append([]string(nil), tokens...),
		DesktopIDs:  append([]string(nil), tokens...),
	}
	return id, m, append(append([]model.AppMatcher(nil), matchers...), m)
}

func matcherOverlapsTokens(m model.AppMatcher, tokens []string) bool {
	for _, field := range [][]string{m.Binaries, m.AppNames, m.NodeNames, m.DesktopIDs} {
		for _, existing := range field {
			for _, tok := range tokens {
				if strings.EqualFold(existing, tok) {
					return true
				}
			}
		}
	}
	return false
}

func matcherIDExists(matchers []model.AppMatcher, id string) bool {
	for _, m := range matchers {
		if m.ID == id {
			return true
		}
	}
	return false
}

// slugify lowercases s and collapses every run of characters outside
// [a-z0-9] to a single '-', trimming any leading/trailing dashes --
// "Brave" -> "brave", "brave-browser" already comes in normalized via
// AppInfo.NameCandidates but this also handles a raw DisplayName like
// "Org.kde.dolphin".
func slugify(s string) string {
	var b strings.Builder
	prevDash := true // avoid ever writing a leading dash
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		case !prevDash:
			b.WriteByte('-')
			prevDash = true
		}
	}
	return strings.TrimSuffix(b.String(), "-")
}
