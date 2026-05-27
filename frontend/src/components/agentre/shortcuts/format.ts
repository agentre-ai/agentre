import type { DesktopPlatform } from "../chrome";

import type { KeyChord, ModifierKind } from "./types";

const ACCEPTABLE_KEY_RE = /^[A-Z0-9,]$/;

export function isPrimaryModifier(
  event: KeyboardEvent,
  platform: DesktopPlatform,
): boolean {
  return platform === "darwin" ? event.metaKey : event.ctrlKey;
}

export function formatPrimaryModifier(platform: DesktopPlatform): string {
  return platform === "darwin" ? "⌘" : "Ctrl";
}

function isOtherModifier(
  event: KeyboardEvent,
  platform: DesktopPlatform,
): boolean {
  if (event.altKey) return true;
  // Forbid mixing ctrl+meta — we only accept the platform's primary key.
  return platform === "darwin" ? event.ctrlKey : event.metaKey;
}

export function isPrimaryModifierKey(
  event: KeyboardEvent,
  platform: DesktopPlatform,
): boolean {
  if (event.altKey) return false;
  if (platform === "darwin") {
    return event.key === "Meta" && event.metaKey && !event.ctrlKey;
  }
  return event.key === "Control" && event.ctrlKey && !event.metaKey;
}

export function isPrimaryShortcut(
  event: KeyboardEvent,
  platform: DesktopPlatform,
): boolean {
  return (
    isPrimaryModifier(event, platform) && !isOtherModifier(event, platform)
  );
}

export function chordFromEvent(
  event: KeyboardEvent,
  platform: DesktopPlatform,
): KeyChord | null {
  if (!isPrimaryModifier(event, platform)) return null;
  if (isOtherModifier(event, platform)) return null;

  const raw = event.key;
  if (raw === "Meta" || raw === "Control" || raw === "Shift" || raw === "Alt") {
    return null;
  }

  const key = raw.length === 1 ? raw.toUpperCase() : raw;
  if (!ACCEPTABLE_KEY_RE.test(key)) return null;

  const mod: ModifierKind = event.shiftKey ? "primary-shift" : "primary";
  return { mod, key };
}

export function formatChord(
  chord: KeyChord,
  platform: DesktopPlatform,
): string {
  const isMac = platform === "darwin";
  if (isMac) {
    const shift = chord.mod === "primary-shift" ? "⇧" : "";
    return `⌘${shift}${chord.key}`;
  }
  const shift = chord.mod === "primary-shift" ? "Shift+" : "";
  // Keep the comma literal so "Ctrl+," reads as a key, not a separator.
  return `Ctrl+${shift}${chord.key}`;
}
