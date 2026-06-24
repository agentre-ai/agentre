import { create } from "zustand";
import { RunList } from "../../wailsjs/go/app/App";
import type { app } from "../../wailsjs/go/models";

interface OrchRunListState {
  runs: app.RunItemDTO[];
  loading: boolean;
  load: () => Promise<void>;
  upsert: (run: app.RunItemDTO) => void;
  __reset: () => void;
}

export const useOrchRunListStore = create<OrchRunListState>((set, get) => ({
  runs: [],
  loading: false,
  async load() {
    set({ loading: true });
    try {
      set({ runs: (await RunList()) ?? [], loading: false });
    } catch {
      set({ loading: false });
    }
  },
  upsert(run) {
    const rest = get().runs.filter((r) => r.id !== run.id);
    set({ runs: [run, ...rest] });
  },
  __reset: () => set({ runs: [], loading: false }),
}));
