// Derives the panel's three visual tiers from the live GET /state
// snapshot: "unbound" (nothing in state.controls for this (kind,
// index)), "bound" (present, but Resolved is absent or empty --
// e.g. the target app isn't running), and "live" (resolved to at
// least one ref, with a real volume/mute reading).
//
// This intentionally reads bound-ness from live state, not from a local
// config draft: no config editor exists yet (that lands with the
// binding editor), so GET /state -- which already reflects the actual
// running config -- is the honest source for now. Once editing exists,
// "unbound vs bound" should switch to reading the in-progress draft (so
// a just-created binding lights up before it round-trips through the
// daemon), while "bound vs live" stays driven by GET /state either way.
import type { State } from "../../api/client";
import type { ControlKind, Gesture } from "../../device/layout";
import { gestureLabel } from "../../device/layout";

export type ControlTier = "unbound" | "bound" | "live";

export interface ControlLiveInfo {
  tier: ControlTier;
  volumePercent: number | undefined;
  muted: boolean | undefined;
  actionType: string | undefined;
}

const UNBOUND: ControlLiveInfo = { tier: "unbound", volumePercent: undefined, muted: undefined, actionType: undefined };

export function controlLiveInfo(state: State | undefined, kind: ControlKind, index: number): ControlLiveInfo {
  if (!state) return UNBOUND;

  const entries = state.controls.filter((c) => c.control.kind === kind && c.control.index === index);
  if (entries.length === 0) return UNBOUND;

  const resolved = entries.find((e) => e.resolved && e.resolved.refs.length > 0);
  const primary = resolved ?? entries[0];
  if (!primary) return UNBOUND;

  if (primary.resolved && primary.resolved.refs.length > 0) {
    return {
      tier: "live",
      volumePercent: primary.resolved.volumePercent,
      muted: primary.resolved.muted,
      actionType: primary.actionType,
    };
  }
  return { tier: "bound", volumePercent: undefined, muted: undefined, actionType: primary.actionType };
}

export interface BoundGesture {
  gesture: Gesture;
  actionType: string;
}

/** boundGestures lists every gesture this (kind, index) control has an
 * action on right now, for a tooltip/summary that shows all of them at
 * once -- not just controlLiveInfo's single "primary" entry -- now that
 * one control commonly has a different action per gesture (press, long
 * press, double press, ...). */
export function boundGestures(state: State | undefined, kind: ControlKind, index: number): BoundGesture[] {
  if (!state) return [];
  return state.controls
    .filter((c) => c.control.kind === kind && c.control.index === index)
    .map((c) => ({ gesture: c.gesture, actionType: c.actionType }));
}

/** boundGesturesSummary renders boundGestures as one line for a tooltip,
 * e.g. "Press: media.transport · Long press: spotify.like_toggle". */
export function boundGesturesSummary(bound: readonly BoundGesture[]): string | undefined {
  if (bound.length === 0) return undefined;
  return bound.map((b) => `${gestureLabel(b.gesture)}: ${b.actionType}`).join(" · ");
}
