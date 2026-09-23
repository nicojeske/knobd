// bindingKey mirrors engine/bindings.go's bindingIndex key exactly:
// (layer, control.kind, control.index, gesture). Using the identical
// key means "is this the same binding" agrees between the UI and the
// daemon, including the last-one-wins resolution a duplicate gets.
import type { Binding, Control } from "../types/config";

export function bindingKey(layer: number, control: Control, gesture: Binding["gesture"]): string {
  return `${layer}|${control.kind}|${control.index}|${gesture}`;
}

export function bindingKeyOf(b: Binding): string {
  return bindingKey(b.layer, b.control, b.gesture);
}

/** findBinding mirrors bindingIndex's own lookup: the LAST matching
 * entry in array order wins, exactly like the daemon's Warn-and-take-
 * the-last-one behavior for a duplicate (see findDuplicateKeys). */
export function findBinding(
  bindings: readonly Binding[],
  layer: number,
  control: Control,
  gesture: Binding["gesture"],
): Binding | undefined {
  const key = bindingKey(layer, control, gesture);
  let found: Binding | undefined;
  for (const b of bindings) {
    if (bindingKeyOf(b) === key) found = b;
  }
  return found;
}

/** upsertBinding replaces the binding matching next's key if one
 * exists, or appends it otherwise -- immutable, never mutates
 * `bindings`. Callers decide separately whether "a binding already
 * exists here" should open it for editing instead of calling this; this
 * is the write side once that decision is made. */
export function upsertBinding(bindings: readonly Binding[], next: Binding): Binding[] {
  const key = bindingKeyOf(next);
  const idx = bindings.findIndex((b) => bindingKeyOf(b) === key);
  if (idx === -1) return [...bindings, next];
  const copy = bindings.slice();
  copy[idx] = next;
  return copy;
}

export function removeBinding(
  bindings: readonly Binding[],
  layer: number,
  control: Control,
  gesture: Binding["gesture"],
): Binding[] {
  const key = bindingKey(layer, control, gesture);
  return bindings.filter((b) => bindingKeyOf(b) !== key);
}

/** findDuplicateKeys returns every binding key that appears more than
 * once. model.Config.Validate does not reject this; bindingIndex
 * resolves it last-one-wins with a logged Warn. The UI surfaces it as a
 * lint (naming which one the daemon actually uses) instead of silently
 * hiding a config that came from a hand-edited file or a SIGHUP reload. */
export function findDuplicateKeys(bindings: readonly Binding[]): string[] {
  const counts = new Map<string, number>();
  for (const b of bindings) {
    const key = bindingKeyOf(b);
    counts.set(key, (counts.get(key) ?? 0) + 1);
  }
  return [...counts.entries()].filter(([, count]) => count > 1).map(([key]) => key);
}
