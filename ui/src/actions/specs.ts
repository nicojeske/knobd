// One descriptor per model.ActionType, driving the binding editor's
// action picker and per-action params form without hand-writing 21
// forms. The mapped type over Action["type"] is what makes "you forgot
// an action" a compile error: omit an arm here and ACTION_SPECS fails
// to satisfy `{ readonly [T in ActionType]: ActionSpec<T> }`, so adding
// a 22nd action to the Go model (and regenerating config.ts) breaks the
// build until someone writes its descriptor.
//
// ACTION_SPECS is deliberately typed as a mapped type over the finite
// ActionType union, not `Record<string, ActionSpec>` -- a Record's
// index signature would reintroduce `| undefined` at every lookup under
// noUncheckedIndexedAccess, exactly the problem a mapped type over a
// literal union doesn't have. Do not "simplify" this to a Record.
import type { Action } from "../types/config";

export type ActionType = Action["type"];
export type ParamsOf<T extends ActionType> = Extract<Action, { type: T }>["params"];

export type ActionGroup = "audio" | "knob" | "layer" | "media" | "mic" | "scene" | "shell" | "sink" | "volume";

interface FieldBase<P, K extends Extract<keyof P, string>> {
  key: K;
  label: string;
}

/** FieldSpec is a distributive union over P's own keys, which is what
 * keeps each field's extra options (min/max, options, ...) tied to the
 * actual value type at that key rather than a generic blob. */
export type FieldSpec<P> = {
  [K in Extract<keyof P, string>]:
    | (FieldBase<P, K> & { kind: "target" })
    | (FieldBase<P, K> & { kind: "number"; min?: number; max?: number; step?: number; unit?: string })
    | (FieldBase<P, K> & { kind: "text"; placeholder?: string })
    | (FieldBase<P, K> & { kind: "enum"; options: readonly string[] })
    | (FieldBase<P, K> & { kind: "stringList" })
    | (FieldBase<P, K> & { kind: "numberList" });
}[Extract<keyof P, string>];

export interface ActionSpec<T extends ActionType> {
  label: string;
  group: ActionGroup;
  /** note is shown in the picker for an action with a caveat worth
   * surfacing up front (e.g. "no handler yet" is sourced from
   * GET /capabilities at render time, not hard-coded here -- this field
   * is for caveats that are true regardless of daemon capabilities). */
  note?: string;
  /** defaults must return a COMPLETE params object with every optional
   * field simply omitted (never set to `undefined`) -- see
   * config/fields.ts's setField/clearField for why an explicit
   * `undefined` is never the right way to represent "not set" under
   * exactOptionalPropertyTypes. */
  defaults: () => ParamsOf<T>;
  fields: readonly FieldSpec<ParamsOf<T>>[];
}

export const ACTION_SPECS: { readonly [T in ActionType]: ActionSpec<T> } = {
  "audio.duck_hold": {
    label: "Duck while held",
    group: "audio",
    defaults: () => ({ target: { kind: "default_sink" }, duckPercent: 50 }),
    fields: [
      { kind: "target", key: "target", label: "Target" },
      { kind: "number", key: "duckPercent", label: "Duck by", min: 0, max: 100, unit: "%" },
    ],
  },
  "audio.solo_toggle": {
    label: "Solo toggle",
    group: "audio",
    defaults: () => ({ target: { kind: "default_sink" } }),
    fields: [{ kind: "target", key: "target", label: "Target" }],
  },
  "knob.assign_focused_app": {
    label: "Assign focused app",
    group: "knob",
    note: "Normally set by holding the physical encoder-push, not from this editor -- rewrites the paired encoder's turn binding.",
    defaults: () => ({}),
    fields: [{ kind: "number", key: "stepPercent", label: "Step", min: 1, max: 50, unit: "%" }],
  },
  "knob.clear": {
    label: "Clear binding",
    group: "knob",
    defaults: () => ({}),
    fields: [],
  },
  "knob.lock_toggle": {
    label: "Lock toggle",
    group: "knob",
    defaults: () => ({}),
    fields: [],
  },
  "layer.cycle": {
    label: "Cycle layers",
    group: "layer",
    defaults: () => ({}),
    fields: [{ kind: "numberList", key: "layerOrder", label: "Layer order" }],
  },
  "layer.latch": {
    label: "Latch layer",
    group: "layer",
    defaults: () => ({ layer: 1 }),
    fields: [{ kind: "number", key: "layer", label: "Layer", min: 0, step: 1 }],
  },
  "layer.momentary": {
    label: "Momentary layer",
    group: "layer",
    defaults: () => ({ layer: 1 }),
    fields: [{ kind: "number", key: "layer", label: "Layer", min: 0, step: 1 }],
  },
  "media.seek": {
    label: "Seek",
    group: "media",
    defaults: () => ({ seekMs: 5000 }),
    fields: [
      { kind: "number", key: "seekMs", label: "Seek amount", unit: "ms" },
      { kind: "text", key: "playerRef", label: "Player (optional)" },
    ],
  },
  "media.transport": {
    label: "Transport control",
    group: "media",
    defaults: () => ({ command: "play_pause" }),
    fields: [
      {
        kind: "enum",
        key: "command",
        label: "Command",
        options: ["play_pause", "next", "previous", "shuffle_toggle", "repeat_cycle"],
      },
      { kind: "text", key: "playerRef", label: "Player (optional)" },
    ],
  },
  "mic.push_to_mute": {
    label: "Push to mute",
    group: "mic",
    defaults: () => ({}),
    fields: [],
  },
  "mic.push_to_talk": {
    label: "Push to talk",
    group: "mic",
    defaults: () => ({}),
    fields: [],
  },
  "scene.apply": {
    label: "Apply scene",
    group: "scene",
    defaults: () => ({ sceneId: "" }),
    fields: [{ kind: "text", key: "sceneId", label: "Scene" }],
  },
  "scene.save": {
    label: "Save scene",
    group: "scene",
    defaults: () => ({ sceneId: "" }),
    fields: [{ kind: "text", key: "sceneId", label: "Scene" }],
  },
  "shell.run": {
    label: "Run a command",
    group: "shell",
    note: "The command is always shown verbatim wherever this binding appears -- see model.ShellRunAction's own doc comment.",
    defaults: () => ({ command: "" }),
    fields: [{ kind: "text", key: "command", label: "Command", placeholder: "/path/to/script.sh" }],
  },
  "sink.cycle_default": {
    label: "Cycle default sink",
    group: "sink",
    defaults: () => ({ sinkNames: [] }),
    fields: [{ kind: "stringList", key: "sinkNames", label: "Sinks (in cycle order)" }],
  },
  "volume.adjust": {
    label: "Adjust volume",
    group: "volume",
    defaults: () => ({ target: { kind: "default_sink" }, stepPercent: 2 }),
    fields: [
      { kind: "target", key: "target", label: "Target" },
      { kind: "number", key: "stepPercent", label: "Step", min: 0, unit: "%" },
      { kind: "number", key: "curveExponent", label: "Curve exponent (optional)", min: 0.1, step: 0.1 },
    ],
  },
  "volume.balance": {
    label: "Balance",
    group: "volume",
    defaults: () => ({ target: { kind: "default_sink" }, step: 2 }),
    fields: [
      { kind: "target", key: "target", label: "Target" },
      { kind: "number", key: "step", label: "Step", unit: "%" },
    ],
  },
  "volume.follow": {
    label: "Follow (absolute)",
    group: "volume",
    defaults: () => ({ target: { kind: "default_sink" } }),
    fields: [
      { kind: "target", key: "target", label: "Target" },
      { kind: "number", key: "minPercent", label: "Min (optional)", min: 0, max: 100, unit: "%" },
      { kind: "number", key: "maxPercent", label: "Max (optional)", min: 0, max: 100, unit: "%" },
    ],
  },
  "volume.mute_toggle": {
    label: "Mute toggle",
    group: "volume",
    defaults: () => ({ target: { kind: "default_sink" } }),
    fields: [{ kind: "target", key: "target", label: "Target" }],
  },
  "volume.set": {
    label: "Set volume",
    group: "volume",
    defaults: () => ({ target: { kind: "default_sink" }, percent: 50 }),
    fields: [
      { kind: "target", key: "target", label: "Target" },
      { kind: "number", key: "percent", label: "Percent", min: 0, max: 100, unit: "%" },
    ],
  },
};

export function actionTypes(): readonly ActionType[] {
  return Object.keys(ACTION_SPECS) as ActionType[];
}
