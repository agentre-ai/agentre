import { create } from "zustand";

interface ChatTerminalState {
  /**
   * Session IDs that currently have a terminal panel open. Each entry
   * corresponds to one live PTY routed via terminal_svc.Service. Multiple
   * tabs can have terminals open simultaneously — they are independent.
   */
  openSessionIDs: ReadonlySet<number>;
  /** True iff the given sessionID has a terminal open right now. */
  isOpen: (sessionID: number) => boolean;
  /** Toggle the terminal for one session on or off (idempotent per session). */
  toggle: (sessionID: number) => void;
  /** Close every terminal — used by App shutdown / cleanup paths. */
  closeAll: () => void;
}

export const useChatTerminalStore = create<ChatTerminalState>((set, get) => ({
  openSessionIDs: new Set<number>(),
  isOpen: (sessionID) => get().openSessionIDs.has(sessionID),
  toggle: (sessionID) =>
    set((s) => {
      const next = new Set(s.openSessionIDs);
      if (next.has(sessionID)) {
        next.delete(sessionID);
      } else {
        next.add(sessionID);
      }
      return { openSessionIDs: next };
    }),
  closeAll: () => set({ openSessionIDs: new Set<number>() }),
}));
