import { toast } from "sonner";
import { create } from "zustand";
import { persist } from "zustand/middleware";

import i18n from "@/i18n";

/** 顶层三段：大纲 / 文件 / Git（Git 已从「文件」页内的一档提升为顶层 tab，决策 1）。 */
export type ChatSidebarTab = "outline" | "files" | "git";

/** 「文件」页内的两种视角：本次对话改过的文件 / 工作目录树。 */
export type ChatFilesMode = "changes" | "directory";

/** 预览面板按文件类型提供的视图档位（spec 决策 7）；diff 已随需求修订去掉，仅 markdown 有意义。 */
export type FilePreviewSegment = "render" | "text" | "split";

/** 打开预览的行来自哪个文件模式：目录 / Git / 变动。首视图由它决定（spec 决策 9）。 */
export type PreviewSourceMode = "directory" | "git" | "changes";

/**
 * 一个预览标签。path 是会话级 relPath，也是标签在会话内的唯一键（同一文件至多一个
 * 标签，重复打开只激活既有标签）；语义照抄 chat-tabs-store 的 ChatTab：isPreview =
 * 临时标签（任一时刻至多一个、被下一次单击原地替换），isPinned = 固定标签（不参与
 * 上限淘汰）。activatedAt 是最近一次被激活的时刻，上限淘汰与会话淘汰都按它排序。
 */
export type FilePreviewTab = {
  path: string;
  segment: FilePreviewSegment | null;
  sourceMode: PreviewSourceMode;
  isPreview: boolean;
  isPinned: boolean;
  activatedAt: number;
};

/** 一个会话打开的整组预览标签与当前活动标签。 */
export type SessionPreviewTabs = {
  tabs: FilePreviewTab[];
  activePath: string | null;
};

/**
 * openPreview 的返回值：行的双击手势靠它判断「这一次 click 有没有原地替换掉别的
 * 临时标签」（sidebar-row.tsx 的 onClick 记下它、onDoubleClick 据此调用
 * restoreClobberedPreviewTab 补回）。真实鼠标双击在派发 dblclick 之前会先各打一次
 * click：第一次 click 命中「原地替换」分支时 replaced 是被替换掉的那个标签；第二
 * 次 click（此时目标路径已经因为第一次 click 而在标签组里）命中的是「已打开，只
 * 是重新激活」分支，alreadyOpen=true、replaced 恒为 null——调用方必须据此**不要**
 * 用这次的 null 覆盖第一次 click 记下的值，否则双击要补回的那个标签就丢了。
 */
export type PreviewClickOutcome = {
  alreadyOpen: boolean;
  replaced: FilePreviewTab | null;
};

/** 每会话最多 8 个标签；再开就淘汰最久未被激活的未固定标签（spec「上限与淘汰」）。 */
export const MAX_PREVIEW_TABS = 8;
/** 全局只留最近 20 个会话的标签表，按「该会话标签最后一次被激活的时间」淘汰整条。 */
export const MAX_PREVIEW_SESSIONS = 20;

type ChatSidebarState = {
  open: boolean;
  activeTab: ChatSidebarTab;
  filesMode: ChatFilesMode;
  showIgnored: boolean;
  /** Git 模式「本分支」档的对比基线，按会话记住（设计决策 9）。 */
  gitBaselineBySession: Record<number, string>;
  /** 每个会话打开的预览标签组，跨重启存活（spec「多标签预览 · 持久化」）。 */
  previewTabsBySession: Record<number, SessionPreviewTabs>;
  setOpen: (open: boolean) => void;
  setActiveTab: (tab: ChatSidebarTab) => void;
  setFilesMode: (mode: ChatFilesMode) => void;
  setShowIgnored: (showIgnored: boolean) => void;
  setGitBaseline: (sessionId: number, ref: string) => void;
  clearGitBaseline: (sessionId: number) => void;
  /**
   * 单击语义：打开成临时标签，原地替换上一个临时标签；文件已打开则激活既有标签。
   * 返回值见 PreviewClickOutcome——行的双击手势要用它在原地替换发生时记下「刚刚
   * 替换掉了谁」，以便双击结束后补回来（否则双击会把它连同一起吞掉）。
   */
  openPreview: (
    sessionId: number,
    path: string,
    sourceMode: PreviewSourceMode,
  ) => PreviewClickOutcome;
  /** 双击 / 右键「在新标签页预览」：直接开常驻标签；文件已打开则激活并转常驻。 */
  openPreviewInNewTab: (
    sessionId: number,
    path: string,
    sourceMode: PreviewSourceMode,
  ) => void;
  /**
   * 补回一个被单击「原地替换」吞掉的临时标签；只供行的双击手势自我修复用，见
   * sidebar-row.tsx。已经存在同路径标签时是 no-op（不覆盖双击自己已经建立的状态）。
   */
  restoreClobberedPreviewTab: (sessionId: number, tab: FilePreviewTab) => void;
  /** 双击当前标签：把活动的临时标签转成常驻标签。 */
  promoteActivePreviewTab: (sessionId: number) => void;
  /** 切换到某个已打开的标签。 */
  activatePreviewTab: (sessionId: number, path: string) => void;
  /** 更新活动标签的视图档位；档位是标签自身的状态，切换标签时各自保留。 */
  setPreviewSegment: (sessionId: number, segment: FilePreviewSegment) => void;
  /** 固定 / 取消固定；固定同时把临时标签转常驻（与 chat-tabs 的 togglePin 一致）。 */
  togglePreviewTabPin: (sessionId: number, path: string) => void;
  /** 关闭一个标签：关掉活动标签后激活右邻居，没有右邻居则激活左邻居。 */
  closePreviewTab: (sessionId: number, path: string) => void;
  /** 关闭其他标签（固定的留下）。 */
  closeOtherPreviewTabs: (sessionId: number, path: string) => void;
  /**
   * 全部关闭：整组关掉，固定标签也不例外——「要留下某几个」的语义已经由「关闭
   * 其他」承担，菜单项就叫「全部关闭」。关光之后整条会话条目消失，预览面板收起。
   */
  closeAllPreviewTabs: (sessionId: number) => void;
};

/** 取某会话的活动标签；没有打开任何标签时返回 null（预览面板据此收起）。 */
export function selectActivePreviewTab(
  state: Pick<ChatSidebarState, "previewTabsBySession">,
  sessionId: number,
): FilePreviewTab | null {
  const entry = state.previewTabsBySession[sessionId];
  if (!entry) return null;
  return entry.tabs.find((tab) => tab.path === entry.activePath) ?? null;
}

const VALID_TABS: ReadonlySet<ChatSidebarTab> = new Set([
  "outline",
  "files",
  "git",
]);

const VALID_FILES_MODES: ReadonlySet<ChatFilesMode> = new Set([
  "changes",
  "directory",
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

// sanitizeTab 校验单个持久化标签：任一字段非法或缺失就返回 null，调用方据此丢弃它所在
// 的整个会话条目（spec「持久化值非法、缺失…时整条丢弃」）。segment 也必须显式存在
// （null 或合法档位）——本 store 写出去的每个标签都带这个字段，缺了就说明数据被改坏了。
function sanitizeTab(value: unknown): FilePreviewTab | null {
  if (!value || typeof value !== "object" || Array.isArray(value)) return null;
  const tab = value as Record<string, unknown>;
  if (typeof tab.path !== "string" || tab.path === "") return null;
  if (!VALID_SOURCE_MODES.has(tab.sourceMode as PreviewSourceMode)) return null;
  if (
    tab.segment !== null &&
    !VALID_PREVIEW_SEGMENTS.has(tab.segment as FilePreviewSegment)
  ) {
    return null;
  }
  if (typeof tab.isPreview !== "boolean" || typeof tab.isPinned !== "boolean") {
    return null;
  }
  if (
    typeof tab.activatedAt !== "number" ||
    !Number.isFinite(tab.activatedAt)
  ) {
    return null;
  }
  return {
    path: tab.path,
    segment: tab.segment as FilePreviewSegment | null,
    sourceMode: tab.sourceMode as PreviewSourceMode,
    isPreview: tab.isPreview,
    isPinned: tab.isPinned,
    activatedAt: tab.activatedAt,
  };
}

// sanitizeSessionTabs 校验一个会话条目：标签集合非空且不超过上限、路径不重复、至多一个
// 临时标签、活动标签必须在集合内。任一条不成立就整条丢弃。
function sanitizeSessionTabs(value: unknown): SessionPreviewTabs | null {
  if (!value || typeof value !== "object" || Array.isArray(value)) return null;
  const entry = value as Record<string, unknown>;
  if (!Array.isArray(entry.tabs)) return null;
  if (entry.tabs.length === 0 || entry.tabs.length > MAX_PREVIEW_TABS) {
    return null;
  }
  const tabs: FilePreviewTab[] = [];
  for (const raw of entry.tabs) {
    const tab = sanitizeTab(raw);
    if (!tab) return null;
    if (tabs.some((kept) => kept.path === tab.path)) return null;
    tabs.push(tab);
  }
  if (tabs.filter((tab) => tab.isPreview).length > 1) return null;
  if (typeof entry.activePath !== "string") return null;
  if (!tabs.some((tab) => tab.path === entry.activePath)) return null;
  return { tabs, activePath: entry.activePath };
}

// sanitizePreviewTabs 只保留「正整数会话 id → 合法标签组」的条目，并把会话数压到全局
// 上限（超出时先淘汰最久未被激活的会话）。JSON 往返后键必然是字符串。
function sanitizePreviewTabs(
  value: unknown,
): Record<number, SessionPreviewTabs> {
  if (!value || typeof value !== "object" || Array.isArray(value)) return {};
  const entries = Object.entries(value as Record<string, unknown>);
  const kept: [number, SessionPreviewTabs][] = [];
  let changed = false;
  for (const [key, raw] of entries) {
    const sessionId = Number(key);
    const entry =
      Number.isInteger(sessionId) && sessionId > 0
        ? sanitizeSessionTabs(raw)
        : null;
    if (!entry) {
      changed = true;
      continue;
    }
    kept.push([sessionId, entry]);
  }
  const capped = capSessions(kept);
  if (!changed && capped.length === entries.length) {
    return value as Record<number, SessionPreviewTabs>;
  }
  return Object.fromEntries(capped) as Record<number, SessionPreviewTabs>;
}

/** 一个会话条目的「最后一次被激活时间」= 组内标签 activatedAt 的最大值。 */
function lastActivatedAt(entry: SessionPreviewTabs): number {
  return entry.tabs.reduce((max, tab) => Math.max(max, tab.activatedAt), 0);
}

// capSessions 把会话数压到 MAX_PREVIEW_SESSIONS：超出时按 lastActivatedAt 从旧到新
// 整条淘汰。返回的顺序保持传入顺序，只是删掉了被淘汰的条目。
function capSessions(
  entries: [number, SessionPreviewTabs][],
): [number, SessionPreviewTabs][] {
  if (entries.length <= MAX_PREVIEW_SESSIONS) return entries;
  const doomed = new Set(
    [...entries]
      .sort((a, b) => lastActivatedAt(a[1]) - lastActivatedAt(b[1]))
      .slice(0, entries.length - MAX_PREVIEW_SESSIONS)
      .map(([sessionId]) => sessionId),
  );
  return entries.filter(([sessionId]) => !doomed.has(sessionId));
}

// activeTab、模式与「显示忽略项」都是用户偏好，随侧栏状态一起持久化；持久化值
// 可能来自更早的版本或被手改过的 localStorage，非法或缺失时一律回落到默认
// （顶层 tab 回落到「大纲」，模式回落到「变动」，忽略项回落到隐藏，基线表回落
// 到空表）。旧版本持久化下来的 filesMode "git" 在 VALID_FILES_MODES 收窄后
// 自然落入这条回落路径，不需要单独的迁移逻辑。
function sanitize(state: ChatSidebarState): ChatSidebarState {
  const activeTab = VALID_TABS.has(state.activeTab)
    ? state.activeTab
    : "outline";
  const filesMode = VALID_FILES_MODES.has(state.filesMode)
    ? state.filesMode
    : "changes";
  const showIgnored =
    typeof state.showIgnored === "boolean" ? state.showIgnored : false;
  const gitBaselineBySession = sanitizeBaselines(state.gitBaselineBySession);
  const previewTabsBySession = sanitizePreviewTabs(state.previewTabsBySession);
  if (
    activeTab === state.activeTab &&
    filesMode === state.filesMode &&
    showIgnored === state.showIgnored &&
    gitBaselineBySession === state.gitBaselineBySession &&
    previewTabsBySession === state.previewTabsBySession
  ) {
    return state;
  }
  return {
    ...state,
    activeTab,
    filesMode,
    showIgnored,
    gitBaselineBySession,
    previewTabsBySession,
  };
}

// stamp 是激活时刻的单调时钟：同一毫秒内连续激活多个标签时 Date.now() 会打平，
// 「最久未被激活」的排序就失去意义，这里保证严格递增。
let lastStamp = 0;
function stamp(): number {
  const nowMs = Date.now();
  lastStamp = nowMs > lastStamp ? nowMs : lastStamp + 1;
  return lastStamp;
}

/** 把一个会话条目写回整张表，并顺带压到全局会话上限；条目为空则整条删除。 */
function commitSession(
  table: Record<number, SessionPreviewTabs>,
  sessionId: number,
  entry: SessionPreviewTabs | null,
): Record<number, SessionPreviewTabs> {
  const next: [number, SessionPreviewTabs][] = [];
  for (const [key, value] of Object.entries(table)) {
    const id = Number(key);
    if (id === sessionId) continue;
    next.push([id, value]);
  }
  if (entry) next.push([sessionId, entry]);
  return Object.fromEntries(capSessions(next)) as Record<
    number,
    SessionPreviewTabs
  >;
}

// retarget 把既有标签的入口模式改成这次打开用的模式：首视图由入口模式决定（决策 9），
// 从另一个模式重新点开同一个文件要重设首视图与档位；同模式则原样保留。
function retarget(
  entry: SessionPreviewTabs,
  index: number,
  sourceMode: PreviewSourceMode,
): SessionPreviewTabs {
  if (entry.tabs[index].sourceMode === sourceMode) return entry;
  const tabs = [...entry.tabs];
  tabs[index] = { ...tabs[index], sourceMode, segment: null };
  return { ...entry, tabs };
}

/** 激活一个已存在的标签：刷新它的 activatedAt 并设为活动标签。 */
function activate(
  entry: SessionPreviewTabs,
  index: number,
): SessionPreviewTabs {
  const tabs = [...entry.tabs];
  tabs[index] = { ...tabs[index], activatedAt: stamp() };
  return { tabs, activePath: tabs[index].path };
}

// insertTab 在标签集合末尾追加一个新标签。已达上限时先淘汰最久未被激活的未固定
// 标签；全部已固定则不新开，返回 null 让调用方提示用户先关掉一个。
function insertTab(
  entry: SessionPreviewTabs,
  tab: FilePreviewTab,
): SessionPreviewTabs | null {
  let tabs = entry.tabs;
  if (tabs.length >= MAX_PREVIEW_TABS) {
    let victim = -1;
    for (let i = 0; i < tabs.length; i++) {
      if (tabs[i].isPinned) continue;
      if (victim < 0 || tabs[i].activatedAt < tabs[victim].activatedAt) {
        victim = i;
      }
    }
    if (victim < 0) return null;
    tabs = tabs.filter((_, i) => i !== victim);
  }
  return { tabs: [...tabs, tab], activePath: tab.path };
}

const EMPTY_ENTRY: SessionPreviewTabs = { tabs: [], activePath: null };

/** 打开预览的入参门槛：会话 id 必须是正整数、路径非空、入口模式合法。 */
function isOpenable(
  sessionId: number,
  path: string,
  sourceMode: PreviewSourceMode,
): boolean {
  if (!Number.isInteger(sessionId) || sessionId <= 0 || path === "") {
    return false;
  }
  return VALID_SOURCE_MODES.has(sourceMode);
}

function commit(
  state: ChatSidebarState,
  sessionId: number,
  entry: SessionPreviewTabs | null,
): Partial<ChatSidebarState> {
  return {
    previewTabsBySession: commitSession(
      state.previewTabsBySession,
      sessionId,
      entry,
    ),
  };
}

// commitOrWarn：entry 为 null 表示已达上限且 8 个标签全部固定 —— 此时不新开标签，
// 只提示用户先关掉一个（spec「上限与淘汰」）。
function commitOrWarn(
  state: ChatSidebarState,
  sessionId: number,
  entry: SessionPreviewTabs | null,
): Partial<ChatSidebarState> {
  if (!entry) {
    toast.warning(
      i18n.t("chatContext.filePreview.tabLimit", { limit: MAX_PREVIEW_TABS }),
    );
    return state;
  }
  return commit(state, sessionId, entry);
}

// closeMany 关掉一批标签：keep 为真的标签留下，固定标签一律留下（与 chat-tabs 的
// closeOthers 一致）。活动标签被关掉时落到操作目标身上。
function closeMany(
  state: ChatSidebarState,
  sessionId: number,
  path: string,
  keep: (tab: FilePreviewTab) => boolean,
): Partial<ChatSidebarState> {
  const entry = state.previewTabsBySession[sessionId];
  if (!entry) return state;
  if (!entry.tabs.some((tab) => tab.path === path)) return state;
  const tabs = entry.tabs.filter((tab) => keep(tab) || tab.isPinned);
  if (tabs.length === entry.tabs.length) return state;
  const activePath =
    entry.activePath !== null &&
    tabs.some((tab) => tab.path === entry.activePath)
      ? entry.activePath
      : path;
  return commit(state, sessionId, { tabs, activePath });
}

/** 关闭若干标签后重新挑活动标签：右邻居优先，没有右邻居取左邻居。 */
function reselectActive(
  tabs: FilePreviewTab[],
  previousTabs: FilePreviewTab[],
  activePath: string | null,
): string | null {
  if (tabs.length === 0) return null;
  if (activePath !== null && tabs.some((tab) => tab.path === activePath)) {
    return activePath;
  }
  const removedAt = previousTabs.findIndex((tab) => tab.path === activePath);
  if (removedAt < 0) return tabs[tabs.length - 1].path;
  return removedAt < tabs.length
    ? tabs[removedAt].path
    : tabs[tabs.length - 1].path;
}

export const useChatSidebarStore = create<ChatSidebarState>()(
  persist(
    (set) => ({
      open: true,
      activeTab: "outline",
      filesMode: "changes",
      showIgnored: false,
      gitBaselineBySession: {},
      previewTabsBySession: {},
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
        if (!isOpenable(sessionId, path, sourceMode)) {
          return { alreadyOpen: false, replaced: null };
        }
        let outcome: PreviewClickOutcome = {
          alreadyOpen: false,
          replaced: null,
        };
        set((state) => {
          const entry = state.previewTabsBySession[sessionId] ?? EMPTY_ENTRY;
          const existing = entry.tabs.findIndex((tab) => tab.path === path);
          if (existing >= 0) {
            // 已打开的文件被再次单击：激活既有标签，不新建、也不降级成临时标签；
            // 但入口模式仍决定首视图（决策 9），从另一个模式点进来要重设。这也是
            // 真实鼠标双击里第二次 click 命中的分支（第一次 click 已经把目标路径
            // 换进了标签组）——outcome.replaced 恒为 null，调用方不能拿它去覆盖
            // 第一次 click 记下的值。
            outcome = { alreadyOpen: true, replaced: null };
            return commit(
              state,
              sessionId,
              retarget(activate(entry, existing), existing, sourceMode),
            );
          }
          const previewIdx = entry.tabs.findIndex((tab) => tab.isPreview);
          const replaced = previewIdx >= 0 ? entry.tabs[previewIdx] : null;
          outcome = { alreadyOpen: false, replaced };
          const tab: FilePreviewTab = {
            path,
            // 入口模式决定首视图：临时标签被同模式的下一个文件原地替换时保留
            // markdown 档位，换模式则回默认档（spec 决策 9 / 12）。
            segment:
              replaced && replaced.sourceMode === sourceMode
                ? replaced.segment
                : null,
            sourceMode,
            isPreview: true,
            isPinned: false,
            activatedAt: stamp(),
          };
          if (replaced) {
            const tabs = [...entry.tabs];
            tabs[previewIdx] = tab;
            return commit(state, sessionId, { tabs, activePath: path });
          }
          return commitOrWarn(state, sessionId, insertTab(entry, tab));
        });
        return outcome;
      },
      openPreviewInNewTab: (sessionId, path, sourceMode) => {
        if (!isOpenable(sessionId, path, sourceMode)) return;
        set((state) => {
          const entry = state.previewTabsBySession[sessionId] ?? EMPTY_ENTRY;
          const existing = entry.tabs.findIndex((tab) => tab.path === path);
          if (existing >= 0) {
            // 已经开着的文件：激活它，并且转成常驻——否则「在新标签页预览」对当前
            // 临时标签等于什么都没做，下一次单击又会把它替换掉。
            const activated = retarget(
              activate(entry, existing),
              existing,
              sourceMode,
            );
            const tabs = [...activated.tabs];
            tabs[existing] = { ...tabs[existing], isPreview: false };
            return commit(state, sessionId, { ...activated, tabs });
          }
          const tab: FilePreviewTab = {
            path,
            segment: null,
            sourceMode,
            isPreview: false,
            isPinned: false,
            activatedAt: stamp(),
          };
          return commitOrWarn(state, sessionId, insertTab(entry, tab));
        });
      },
      restoreClobberedPreviewTab: (sessionId, tab) =>
        set((state) => {
          const entry = state.previewTabsBySession[sessionId];
          if (!entry) return state;
          // 双击自己已经把这个路径建立成了某种标签（比如恰好把它双击回来）：不
          // 覆盖双击刚建立的状态。
          if (entry.tabs.some((t) => t.path === tab.path)) return state;
          const inserted = insertTab(entry, tab);
          if (!inserted) return state;
          // insertTab 会把新标签设成活动标签，但这里活动标签应该仍是双击刚转常
          // 驻的那一个——补回来的只是「一个之前存在过的标签」，不该抢走焦点。
          return commit(state, sessionId, {
            ...inserted,
            activePath: entry.activePath,
          });
        }),
      promoteActivePreviewTab: (sessionId) =>
        set((state) => {
          const entry = state.previewTabsBySession[sessionId];
          if (!entry) return state;
          const idx = entry.tabs.findIndex(
            (tab) => tab.path === entry.activePath,
          );
          if (idx < 0 || !entry.tabs[idx].isPreview) return state;
          const tabs = [...entry.tabs];
          tabs[idx] = { ...tabs[idx], isPreview: false };
          return commit(state, sessionId, { ...entry, tabs });
        }),
      activatePreviewTab: (sessionId, path) =>
        set((state) => {
          const entry = state.previewTabsBySession[sessionId];
          if (!entry) return state;
          const idx = entry.tabs.findIndex((tab) => tab.path === path);
          if (idx < 0) return state;
          return commit(state, sessionId, activate(entry, idx));
        }),
      setPreviewSegment: (sessionId, segment) => {
        if (!VALID_PREVIEW_SEGMENTS.has(segment)) return;
        set((state) => {
          const entry = state.previewTabsBySession[sessionId];
          if (!entry) return state;
          const idx = entry.tabs.findIndex(
            (tab) => tab.path === entry.activePath,
          );
          if (idx < 0) return state;
          const tabs = [...entry.tabs];
          tabs[idx] = { ...tabs[idx], segment };
          return commit(state, sessionId, { ...entry, tabs });
        });
      },
      togglePreviewTabPin: (sessionId, path) =>
        set((state) => {
          const entry = state.previewTabsBySession[sessionId];
          if (!entry) return state;
          const idx = entry.tabs.findIndex((tab) => tab.path === path);
          if (idx < 0) return state;
          const cur = entry.tabs[idx];
          const tabs = [...entry.tabs];
          // 固定顺带转常驻（与 chat-tabs 的 togglePin 一致）；取消固定不动位置。
          tabs[idx] = cur.isPinned
            ? { ...cur, isPinned: false }
            : { ...cur, isPinned: true, isPreview: false };
          return commit(state, sessionId, { ...entry, tabs });
        }),
      closePreviewTab: (sessionId, path) =>
        set((state) => {
          const entry = state.previewTabsBySession[sessionId];
          if (!entry) return state;
          if (!entry.tabs.some((tab) => tab.path === path)) return state;
          const tabs = entry.tabs.filter((tab) => tab.path !== path);
          if (tabs.length === 0) return commit(state, sessionId, null);
          return commit(state, sessionId, {
            tabs,
            activePath: reselectActive(tabs, entry.tabs, entry.activePath),
          });
        }),
      closeOtherPreviewTabs: (sessionId, path) =>
        set((state) =>
          closeMany(state, sessionId, path, (tab) => tab.path === path),
        ),
      closeAllPreviewTabs: (sessionId) =>
        set((state) => {
          if (!state.previewTabsBySession[sessionId]) return state;
          return commit(state, sessionId, null);
        }),
    }),
    {
      name: "chat-sidebar-state",
      merge: (persisted, current) =>
        sanitize({ ...current, ...(persisted as Partial<ChatSidebarState>) }),
    },
  ),
);
