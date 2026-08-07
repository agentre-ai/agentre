import { create } from "zustand";
import { persist } from "zustand/middleware";

export type ChatSidebarTab = "outline" | "files";

/** 「文件」页内的三种视角：本次对话改过的文件 / 工作目录树 / git 变动。 */
export type ChatFilesMode = "changes" | "directory" | "git";

type ChatSidebarState = {
  open: boolean;
  activeTab: ChatSidebarTab;
  filesMode: ChatFilesMode;
  showIgnored: boolean;
  setOpen: (open: boolean) => void;
  setActiveTab: (tab: ChatSidebarTab) => void;
  setFilesMode: (mode: ChatFilesMode) => void;
  setShowIgnored: (showIgnored: boolean) => void;
};

const VALID_TABS: ReadonlySet<ChatSidebarTab> = new Set(["outline", "files"]);

const VALID_FILES_MODES: ReadonlySet<ChatFilesMode> = new Set([
  "changes",
  "directory",
  "git",
]);

// 模式与「显示忽略项」是用户偏好，随侧栏状态一起持久化；持久化值可能来自更早
// 的版本或被手改过的 localStorage，非法或缺失时一律回落到默认（模式回落到
// 「变动」，忽略项回落到隐藏）。
function sanitize(state: ChatSidebarState): ChatSidebarState {
  const filesMode = VALID_FILES_MODES.has(state.filesMode)
    ? state.filesMode
    : "changes";
  const showIgnored =
    typeof state.showIgnored === "boolean" ? state.showIgnored : false;
  if (filesMode === state.filesMode && showIgnored === state.showIgnored) {
    return state;
  }
  return { ...state, filesMode, showIgnored };
}

export const useChatSidebarStore = create<ChatSidebarState>()(
  persist(
    (set) => ({
      open: true,
      activeTab: "outline",
      filesMode: "changes",
      showIgnored: false,
      setOpen: (open) => set({ open }),
      setActiveTab: (tab) => {
        if (!VALID_TABS.has(tab)) return;
        set({ activeTab: tab });
      },
      setFilesMode: (mode) => {
        if (!VALID_FILES_MODES.has(mode)) return;
        set({ filesMode: mode });
      },
      setShowIgnored: (showIgnored) => set({ showIgnored }),
    }),
    {
      name: "chat-sidebar-state",
      merge: (persisted, current) =>
        sanitize({ ...current, ...(persisted as Partial<ChatSidebarState>) }),
    },
  ),
);
