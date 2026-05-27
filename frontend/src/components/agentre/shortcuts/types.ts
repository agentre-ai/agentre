import type { NavigateFunction } from "react-router-dom";

export type ShortcutScope = "global" | "session" | "tabs";

export type ModifierKind = "primary" | "primary-shift";

export type KeyChord = {
  mod: ModifierKind;
  key: string;
};

export type ShortcutDef = {
  id: string;
  label: string;
  hint: string;
  scope: ShortcutScope;
  defaultBinding: KeyChord;
  rebindable: boolean;
};

export type AttentionEntry = {
  agentId: number;
  sessionId: number;
};

export type ShortcutCtx = {
  navigate: NavigateFunction;
  attentionEntries: AttentionEntry[];
  selectChatSession: (agentId: number, sessionId: number) => void;
};

// Bridge object injected by <PaletteScopeBridge/>. ShortcutsProvider keeps it
// behind a ref so it can fire palette.open / cmd.new-chat from its single window
// keydown handler without taking a direct dependency on the palette store.
export type PaletteScope = {
  toggle: () => void;
  openWith: (query: string) => void;
};
