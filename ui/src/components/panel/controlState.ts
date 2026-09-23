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
import type { ControlKind } from "../../device/layout";

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
