import { create } from "zustand";
import { persist } from "zustand/middleware";

export type ChatSidebarTab = "outline" | "files";

/** 「文件」页内的三种视角：本次对话改过的文件 / 工作目录树 / git 变动。 */
export type ChatFilesMode = "changes" | "directory" | "git";

/** 预览面板按文件类型提供的视图档位（spec 决策 7）；diff 已随需求修订去掉，仅 markdown 有意义。 */
export type FilePreviewSegment = "render" | "text" | "split";

/** 打开预览的行来自哪个文件模式：目录 / Git / 变动。首视图由它决定（spec 决策 9）。 */
export type PreviewSourceMode = "directory" | "git" | "changes";

/** 预览选中：path 是会话级 relPath；sourceMode 是入口模式；segment 为 null 表示用 markdown 默认档。 */
export type FilePreviewSelection = {
  path: string;
  segment: FilePreviewSegment | null;
  sourceMode: PreviewSourceMode;
};

type ChatSidebarState = {
  open: boolean;
  activeTab: ChatSidebarTab;
  filesMode: ChatFilesMode;
  showIgnored: boolean;
  /** Git 模式「本分支」档的对比基线，按会话记住（设计决策 9）。 */
  gitBaselineBySession: Record<number, string>;
  /** 预览选中的文件与视图档位，按会话记住（spec 决策 12）。 */
  previewBySession: Record<number, FilePreviewSelection>;
  setOpen: (open: boolean) => void;
  setActiveTab: (tab: ChatSidebarTab) => void;
  setFilesMode: (mode: ChatFilesMode) => void;
  setShowIgnored: (showIgnored: boolean) => void;
  setGitBaseline: (sessionId: number, ref: string) => void;
  clearGitBaseline: (sessionId: number) => void;
  /** 打开 / 切换到某文件并记录入口模式；面板开着且模式不变时保留 markdown 档位，换模式则回默认档。 */
  openPreview: (
    sessionId: number,
    path: string,
    sourceMode: PreviewSourceMode,
  ) => void;
  /** 更新当前选中文件的视图档位；无选中时为 no-op。 */
  setPreviewSegment: (sessionId: number, segment: FilePreviewSegment) => void;
  /** 关闭面板：清空该会话的预览选中。 */
  clearPreview: (sessionId: number) => void;
};

const VALID_TABS: ReadonlySet<ChatSidebarTab> = new Set(["outline", "files"]);

const VALID_FILES_MODES: ReadonlySet<ChatFilesMode> = new Set([
  "changes",
  "directory",
  "git",
]);

const VALID_PREVIEW_SEGMENTS: ReadonlySet<FilePreviewSegment> = new Set([
  "render",
  "text",
  "split",
]);

const VALID_SOURCE_MODES: ReadonlySet<PreviewSourceMode> = new Set([
  "directory",
  "git",
  "changes",
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

// sanitizePreview 只保留「正整数会话 id → { path, segment?, sourceMode }」的合法条目：路径必须
// 非空字符串,segment 缺失补 null、非法值丢弃整条；sourceMode 缺失 / 非法（旧版本持久化
// 无此字段）也丢弃整条。JSON 往返后键必然是字符串。
function sanitizePreview(value: unknown): Record<number, FilePreviewSelection> {
  if (!value || typeof value !== "object" || Array.isArray(value)) return {};
  const entries = Object.entries(value as Record<string, unknown>);
  const kept = entries.filter(([key, sel]) => {
    if (!(Number.isInteger(Number(key)) && Number(key) > 0)) return false;
    if (!sel || typeof sel !== "object" || Array.isArray(sel)) return false;
    const s = sel as Record<string, unknown>;
    if (typeof s.path !== "string" || s.path === "") return false;
    if (
      s.segment != null &&
      !VALID_PREVIEW_SEGMENTS.has(s.segment as FilePreviewSegment)
    ) {
      return false;
    }
    if (!VALID_SOURCE_MODES.has(s.sourceMode as PreviewSourceMode)) {
      return false;
    }
    return true;
  });
  const normalized: Record<number, FilePreviewSelection> = Object.fromEntries(
    kept.map(([key, sel]) => {
      const s = sel as Record<string, unknown>;
      return [
        Number(key),
        {
          path: s.path as string,
          segment: (s.segment as FilePreviewSegment) ?? null,
          sourceMode: s.sourceMode as PreviewSourceMode,
        },
      ];
    }),
  );
  if (
    kept.length === entries.length &&
    Object.keys(normalized).length === entries.length
  ) {
    return value as Record<number, FilePreviewSelection>;
  }
  return normalized;
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
  const previewBySession = sanitizePreview(state.previewBySession);
  if (
    filesMode === state.filesMode &&
    showIgnored === state.showIgnored &&
    gitBaselineBySession === state.gitBaselineBySession &&
    previewBySession === state.previewBySession
  ) {
    return state;
  }
  return {
    ...state,
    filesMode,
    showIgnored,
    gitBaselineBySession,
    previewBySession,
  };
}

export const useChatSidebarStore = create<ChatSidebarState>()(
  persist(
    (set) => ({
      open: true,
      activeTab: "outline",
      filesMode: "changes",
      showIgnored: false,
      gitBaselineBySession: {},
      previewBySession: {},
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
      openPreview: (sessionId, path, sourceMode) => {
        if (!Number.isInteger(sessionId) || sessionId <= 0 || path === "")
          return;
        if (!VALID_SOURCE_MODES.has(sourceMode)) return;
        set((state) => {
          const prev = state.previewBySession[sessionId];
          // 入口模式决定首视图：换模式打开重设首视图（markdown 档位回默认）；
          // 同一模式内切文件保留 markdown 档位（spec 决策 9 / 12）。
          const sameMode = prev?.sourceMode === sourceMode;
          return {
            previewBySession: {
              ...state.previewBySession,
              [sessionId]: {
                path,
                sourceMode,
                segment: sameMode ? (prev?.segment ?? null) : null,
              },
            },
          };
        });
      },
      setPreviewSegment: (sessionId, segment) => {
        if (!Number.isInteger(sessionId) || sessionId <= 0) return;
        if (!VALID_PREVIEW_SEGMENTS.has(segment)) return;
        set((state) => {
          const prev = state.previewBySession[sessionId];
          if (!prev) return state;
          return {
            previewBySession: {
              ...state.previewBySession,
              [sessionId]: { ...prev, segment },
            },
          };
        });
      },
      clearPreview: (sessionId) =>
        set((state) => {
          if (!(sessionId in state.previewBySession)) return state;
          const next = { ...state.previewBySession };
          delete next[sessionId];
          return { previewBySession: next };
        }),
    }),
    {
      name: "chat-sidebar-state",
      merge: (persisted, current) =>
        sanitize({ ...current, ...(persisted as Partial<ChatSidebarState>) }),
    },
  ),
);
