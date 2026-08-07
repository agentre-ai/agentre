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
  /** Git 模式「本分支」档的对比基线，按会话记住（设计决策 9）。 */
  gitBaselineBySession: Record<number, string>;
  setOpen: (open: boolean) => void;
  setActiveTab: (tab: ChatSidebarTab) => void;
  setFilesMode: (mode: ChatFilesMode) => void;
  setShowIgnored: (showIgnored: boolean) => void;
  setGitBaseline: (sessionId: number, ref: string) => void;
  clearGitBaseline: (sessionId: number) => void;
};

const VALID_TABS: ReadonlySet<ChatSidebarTab> = new Set(["outline", "files"]);

const VALID_FILES_MODES: ReadonlySet<ChatFilesMode> = new Set([
  "changes",
  "directory",
  "git",
]);

// sanitizeBaselines 只保留「正整数会话 id → 非空 ref」的条目：这张表会被
// JSON 往返，键必然是字符串，值来自更早的版本或被手改过的 localStorage。
// 全部条目都合法时原样返回，让 sanitize 的短路判断仍然成立。
function sanitizeBaselines(value: unknown): Record<number, string> {
  if (!value || typeof value !== "object" || Array.isArray(value)) return {};
  const entries = Object.entries(value as Record<string, unknown>);
  const kept = entries.filter(
    ([key, ref]) =>
      Number.isInteger(Number(key)) &&
      Number(key) > 0 &&
      typeof ref === "string" &&
      ref !== "",
  );
  if (kept.length === entries.length) return value as Record<number, string>;
  return Object.fromEntries(kept) as Record<number, string>;
}

// 模式与「显示忽略项」是用户偏好，随侧栏状态一起持久化；持久化值可能来自更早
// 的版本或被手改过的 localStorage，非法或缺失时一律回落到默认（模式回落到
// 「变动」，忽略项回落到隐藏，基线表回落到空表）。
function sanitize(state: ChatSidebarState): ChatSidebarState {
  const filesMode = VALID_FILES_MODES.has(state.filesMode)
    ? state.filesMode
    : "changes";
  const showIgnored =
    typeof state.showIgnored === "boolean" ? state.showIgnored : false;
  const gitBaselineBySession = sanitizeBaselines(state.gitBaselineBySession);
  if (
    filesMode === state.filesMode &&
    showIgnored === state.showIgnored &&
    gitBaselineBySession === state.gitBaselineBySession
  ) {
    return state;
  }
  return { ...state, filesMode, showIgnored, gitBaselineBySession };
}

export const useChatSidebarStore = create<ChatSidebarState>()(
  persist(
    (set) => ({
      open: true,
      activeTab: "outline",
      filesMode: "changes",
      showIgnored: false,
      gitBaselineBySession: {},
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
      setGitBaseline: (sessionId, ref) => {
        if (!Number.isInteger(sessionId) || sessionId <= 0 || ref === "")
          return;
        set((state) => ({
          gitBaselineBySession: {
            ...state.gitBaselineBySession,
            [sessionId]: ref,
          },
        }));
      },
      clearGitBaseline: (sessionId) =>
        set((state) => {
          if (!(sessionId in state.gitBaselineBySession)) return state;
          const next = { ...state.gitBaselineBySession };
          delete next[sessionId];
          return { gitBaselineBySession: next };
        }),
    }),
    {
      name: "chat-sidebar-state",
      merge: (persisted, current) =>
        sanitize({ ...current, ...(persisted as Partial<ChatSidebarState>) }),
    },
  ),
);
