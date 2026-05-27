import { create } from "zustand";

type State = {
  open: boolean;
  // ⌘N 等场景写入：组件 mount/open=true 后把它拷到本地 query state。
  // close() 主动清零，避免下次 open 复读上一次的 seed。
  initialQuery: string;
};

type Actions = {
  setOpen: (open: boolean) => void;
  toggle: () => void;
  close: () => void;
  // 原子打开 + 预填 query。用于 ⌘N → "> New chat with " 这类入口。
  openWith: (query: string) => void;
};

// Zustand store for the ⌘P command palette dialog. UI state only — palette
// data sources own their own data. Kept in the store (not a React context)
// so that ⌘P / ⌘N dispatched from ShortcutsProvider can toggle without prop drilling.
export const useCommandPaletteStore = create<State & Actions>((set, get) => ({
  open: false,
  initialQuery: "",
  // 所有"翻成关"的路径都必须清 initialQuery，否则下次 ⌘P/toggle 会复读 stale seed：
  //   ⌘N (openWith) → 关弹窗 (Dialog.onOpenChange → setOpen(false))
  //   → ⌘P (toggle) → useEffect 读到残留 initialQuery="> " → 误入命令模式
  // 收口在 store：setOpen / toggle / close 关闭分支统一清。
  setOpen: (open) => set(open ? { open } : { open: false, initialQuery: "" }),
  toggle: () => {
    const willOpen = !get().open;
    set(willOpen ? { open: true } : { open: false, initialQuery: "" });
  },
  close: () => set({ open: false, initialQuery: "" }),
  openWith: (query) => set({ open: true, initialQuery: query }),
}));
