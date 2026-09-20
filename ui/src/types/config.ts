// Hand-written mirror of daemon/internal/model's JSON shape, kept only
// until M07 wires up real codegen (see the Makefile's `schema` target
// and daemon/internal/api's doc comment) from
// docs/config.schema.json/docs/openapi.json. Do not let this drift by
// hand for long — if you add a field here, add it in the Go struct
// first and copy the shape exactly, including field names.

export type ControlKind =
  | "encoder"
  | "encoder_push"
  | "button"
  | "side_button"
  | "fader";

export interface Control {
  kind: ControlKind;
  index: number;
}

export type Gesture = "turn" | "press" | "hold" | "release" | "double_press";

export type TargetKind =
  | "default_sink"
  | "sink"
  | "default_source"
  | "source"
  | "app"
  | "group"
  | "focused"
  | "all_streams";

export interface Target {
  kind: TargetKind;
  ref?: string;
}

export interface AppMatcher {
  id: string;
  displayName: string;
  binaries?: string[];
  appNames?: string[];
  nodeNames?: string[];
  desktopIds?: string[];
  mediaNameRx?: string;
}

export interface AppGroup {
  id: string;
  displayName: string;
  matcherIds: string[];
}

// Action is intentionally `unknown` params for now — see
// daemon/internal/model/action.go's actionRegistry for the full set of
// {type, params} shapes. Typing this precisely as a discriminated union
// is exactly what M07's schema-driven codegen should produce instead of
// hand-maintaining it here.
export interface Action {
  type: string;
  params: unknown;
}

export interface Binding {
  layer: number;
  control: Control;
  gesture: Gesture;
  action: Action;
}

export interface Profile {
  id: string;
  displayName: string;
  bindings: Binding[];
}

export interface SceneEntry {
  target: Target;
  volumePercent: number;
  muted: boolean;
}

export interface Scene {
  id: string;
  displayName: string;
  entries: SceneEntry[];
}

export interface Config {
  schemaVersion: number;
  activeProfileId: string;
  profiles: Profile[];
  appMatchers: AppMatcher[];
  appGroups: AppGroup[];
  scenes: Scene[];
}
