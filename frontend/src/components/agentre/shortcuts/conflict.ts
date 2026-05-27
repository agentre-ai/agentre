import { SYSTEM_RESERVED } from "./registry";
import type { KeyChord } from "./types";

export function sameChord(a: KeyChord, b: KeyChord): boolean {
  return a.mod === b.mod && a.key.toUpperCase() === b.key.toUpperCase();
}

export function isSystemReserved(chord: KeyChord): boolean {
  return SYSTEM_RESERVED.some((reserved) => sameChord(reserved, chord));
}

export type ConflictResult =
  | { type: "system" }
  | { type: "binding"; id: string };

export function findConflict(
  bindings: Map<string, KeyChord>,
  chord: KeyChord,
  excludeId: string,
): ConflictResult | null {
  if (isSystemReserved(chord)) {
    return { type: "system" };
  }
  for (const [id, c] of bindings) {
    if (id === excludeId) continue;
    if (sameChord(c, chord)) {
      return { type: "binding", id };
    }
  }
  return null;
}
