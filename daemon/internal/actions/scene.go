package actions

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/njeske/knobd/internal/model"
)

// SceneOptions configures SceneHandlers. The zero value is sane
// defaults.
type SceneOptions struct {
	// Logger receives per-entry resolution/clamp diagnostics. Nil means
	// slog.Default().
	Logger *slog.Logger
}

func (o SceneOptions) logger() *slog.Logger {
	if o.Logger != nil {
		return o.Logger
	}
	return slog.Default()
}

// SceneHandlers implements model.ActionSceneApply/ActionSceneSave.
// engine resolves each entry's Target at dispatch time (see
// Invocation.Scene/SceneRefs' doc comment) since Target resolution
// needs the config's AppMatchers/AppGroups and the live stream graph,
// both of which live in engine, not here.
type SceneHandlers struct {
	vol   *VolumeHandlers
	store ConfigMutator
	opts  SceneOptions
}

// NewSceneHandlers returns handlers that read/write volume levels
// through vol's shared cache (so a scene apply/save observes and
// commits through the same OnApplied/LED-feedback seam as every other
// volume write) and persist scene edits through store.
func NewSceneHandlers(vol *VolumeHandlers, store ConfigMutator, opts SceneOptions) *SceneHandlers {
	return &SceneHandlers{vol: vol, store: store, opts: opts}
}

// Register wires model.ActionSceneApply/ActionSceneSave into r.
func (h *SceneHandlers) Register(r *Registry) {
	r.Register(model.ActionSceneApply, HandlerFunc(h.executeApply))
	r.Register(model.ActionSceneSave, HandlerFunc(h.executeSave))
}

// executeApply restores every entry's saved VolumePercent/Muted to its
// resolved refs, in the scene's own entry order. An entry whose target
// didn't resolve to anything at dispatch time (engine.dispatchScene
// left it a nil slice) is skipped, not an error -- the same "an app
// that isn't running is a no-op" rule every other target-bearing action
// in this package follows.
func (h *SceneHandlers) executeApply(ctx context.Context, inv Invocation) error {
	if _, ok := inv.Action.(model.SceneApplyAction); !ok {
		return fmt.Errorf("actions: scene.apply got %T", inv.Action)
	}
	if inv.Scene == nil {
		return fmt.Errorf("actions: scene.apply: no scene resolved for this invocation")
	}

	var errs []error
	for i, entry := range inv.Scene.Entries {
		if i >= len(inv.SceneRefs) || len(inv.SceneRefs[i]) == 0 {
			h.opts.logger().Debug("actions: scene.apply: entry target resolved to nothing", "sceneId", inv.Scene.ID, "target", entry.Target)
			continue
		}
		refs := inv.SceneRefs[i]
		percent := h.vol.clamp(entry.VolumePercent, "scene.apply")
		for _, ref := range refs {
			// Volume first, then mute -- writeMute (see its doc comment)
			// re-reads the cache itself, so it always picks up the
			// percent write just below rather than a stale pre-write
			// snapshot.
			if err := h.vol.writeVolume(ctx, ref, percent); err != nil {
				errs = append(errs, err)
				continue
			}
			if err := h.vol.writeMute(ctx, ref, entry.Muted); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

// executeSave overwrites the scene's existing entries with the current
// live level/mute of whatever each entry's target resolves to right
// now -- it never adds, removes, or reorders entries (see model.Scene's
// doc comment and specs/milestones/M08-layers-groups-scenes.md's Design
// section: a scene's target list is fixed at creation/edit time, not
// implicitly grown by saving over it). An entry whose target resolved
// to nothing right now (engine.dispatchScene left it nil) is left
// untouched in the saved config, not zeroed out.
//
// Reads the live config from store.Config() rather than trusting
// inv.Scene to still be current: another PUT /config could have
// changed this same scene between dispatch and this handler running on
// the (single, serializing) dispatcher goroutine -- unlikely, but cheap
// to get right by re-reading rather than assuming.
func (h *SceneHandlers) executeSave(ctx context.Context, inv Invocation) error {
	a, ok := inv.Action.(model.SceneSaveAction)
	if !ok {
		return fmt.Errorf("actions: scene.save got %T", inv.Action)
	}
	if inv.Scene == nil {
		return fmt.Errorf("actions: scene.save: no scene resolved for this invocation")
	}

	cfg := h.store.Config()
	sceneIdx := -1
	for i, s := range cfg.Scenes {
		if s.ID == a.SceneID {
			sceneIdx = i
			break
		}
	}
	if sceneIdx == -1 {
		return fmt.Errorf("actions: scene.save: scene %q no longer exists", a.SceneID)
	}

	scenes := append([]model.Scene(nil), cfg.Scenes...)
	scene := scenes[sceneIdx]
	entries := append([]model.SceneEntry(nil), scene.Entries...)

	var errs []error
	for i := range entries {
		if i >= len(inv.SceneRefs) || len(inv.SceneRefs[i]) == 0 {
			// Not currently resolvable (see the doc comment above) --
			// leave this entry exactly as it was.
			h.opts.logger().Debug("actions: scene.save: entry target resolved to nothing, leaving it untouched", "sceneId", a.SceneID, "target", entries[i].Target)
			continue
		}
		ref := inv.SceneRefs[i][0]
		st, err := h.vol.getCached(ctx, ref)
		if err != nil {
			errs = append(errs, fmt.Errorf("actions: scene.save: get level for %+v: %w", ref, err))
			continue
		}
		entries[i].VolumePercent = st.Percent
		entries[i].Muted = st.Muted
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	scene.Entries = entries
	scenes[sceneIdx] = scene
	cfg.Scenes = scenes

	persistCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := h.store.SetConfig(persistCtx, cfg); err != nil {
		return fmt.Errorf("actions: scene.save: persist: %w", err)
	}
	return nil
}
