import { create } from "zustand";

interface ChatTerminalState {
  openSessionID: number | null;
  toggle: (sessionID: number) => void;
  closeAll: () => void;
}

export const useChatTerminalStore = create<ChatTerminalState>((set, get) => ({
  openSessionID: null,
  toggle: (sessionID) => {
    const current = get().openSessionID;
    set({ openSessionID: current === sessionID ? null : sessionID });
  },
  closeAll: () => set({ openSessionID: null }),
}));
