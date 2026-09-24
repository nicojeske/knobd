// Pure helpers over Config.scenes, mirroring config/bindings.ts's shape
// for the same reason: immutable, no I/O, easy to unit test without a
// running daemon.
import type { Scene } from "../types/config";

/** upsertScene replaces the scene matching next.id if one exists, or
 * appends it otherwise -- never mutates `scenes`. */
export function upsertScene(scenes: readonly Scene[], next: Scene): Scene[] {
  const idx = scenes.findIndex((s) => s.id === next.id);
  if (idx === -1) return [...scenes, next];
  const copy = scenes.slice();
  copy[idx] = next;
  return copy;
}

export function removeScene(scenes: readonly Scene[], id: string): Scene[] {
  return scenes.filter((s) => s.id !== id);
}
