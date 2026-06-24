import * as React from "react";
import {
  ArrowDown,
  Folder,
  MoreHorizontal,
  PanelRight,
  PanelRightClose,
  Square,
  TriangleAlert,
  X,
} from "lucide-react";
import { useTranslation } from "react-i18next";

import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { useCCUsage } from "@/hooks/use-cc-usage";
import { useChatSession } from "@/hooks/use-chat-session";
import { useChatStream, type ChatStreamEvent } from "@/hooks/use-chat-stream";
import { useProjectTree } from "@/hooks/use-project-tree";
import { useVisibleMessageId } from "@/hooks/use-visible-message-id";
import i18n from "@/i18n";
import { reasonToDisplayStatus } from "@/lib/attention-display";
import { copyTextWithToast } from "@/lib/clipboard-toast";
import { findProjectColorToken, projectChain } from "@/lib/project-chain";
import { relativeTime } from "@/lib/relative-time";
import { cn } from "@/lib/utils";
import { useSessionAttention } from "@/stores/attention-store";
import { useClearedBackgroundTasksStore } from "@/stores/cleared-background-tasks-store";
import { useChatStreamsStore } from "@/stores/chat-streams-store";
import { useQueuedMessagesStore } from "@/stores/queued-messages-store";
import { useSessionReadStore } from "@/stores/session-read-store";
import { useSessionStatusStore } from "@/stores/session-status-store";

import { useBackendCapabilities } from "./capability/use-backend-capabilities";
import { useSessionCapabilities } from "./capability/use-session-capabilities";
import type { PlanActionStream } from "./canonical-tool/props";
import {
  ChatComposer,
  ChatTranscript,
  type ChatComposerSubmit,
  type ChatTranscriptHandle,
  type ChatImageAttachment,
} from "./chat";
import { ChatContextSidebar } from "./chat-context-sidebar";
import { computeComposerContextUsage } from "./chat-panel-context-usage";
import { PermissionModePill, usePermissionMode } from "./permission-mode";
import { useChatSidebarStore } from "@/stores/chat-sidebar-store";
import { AgentAvatar, DeviceTag, StatusDot } from "./primitives";
import { QueuedMessagesBar } from "./queued-messages-bar";
import { BackgroundTasksChip } from "./background-tasks/background-tasks-chip";
import {
  loadTranscriptScrollState,
  nextAutoFollow,
  saveTranscriptScrollState,
} from "./chat-panel-scroll-state";
import { deriveBackgroundTasks } from "./background-tasks/derive";
import { deriveTaskProgress } from "./task-progress/derive";
import { TaskProgressBar } from "./task-progress/task-progress-bar";
import type { AgentColor, AgentStatus } from "./types";
import { agentTextColorClassName, statusConfig } from "./types";

import {
  CancelQueuedChatMessage,
  ClearChatGoal,
  CompactChatSession,
  DeleteChatSession,
  EditChatMessage,
  EnqueueChatMessage,
  GetChatGoal,
  GetChatLaunchCommand,
  MarkChatSessionRead,
  RegenerateChatMessage,
  RenameChatSession,
  SendChatMessage,
  SetChatGoal,
  StartChatGoal,
  StopChatMessage,
} from "../../../wailsjs/go/app/App";
import { chat_svc } from "../../../wailsjs/go/models";

type SvcChatMessage = chat_svc.ChatMessage;
type ChatAgentItem = chat_svc.ChatAgentItem;

type TranscriptScrollSnapshot = {
  atBottom: boolean;
  scrollTop: number;
  // 非贴底时记的锚点:视口顶部那一行的消息 id + 行顶边在视口顶上方的 px +
  // 行身份(行级虚拟化下长消息拆多行,rowKey 才能钉回同一行)。
  // 见 computeTopVisibleAnchor / ChatTranscriptHandle.scrollToAnchor。
  anchorId?: number;
  anchorOffset?: number;
  anchorRowKey?: string;
};

type ScrollMetrics = {
  clientHeight: number;
  maxScrollTop: number;
  scrollHeight: number;
  scrollTop: number;
};

type CollapsedScrollRestoreGuard = TranscriptScrollSnapshot & {
  key: string;
  minMaxScrollTop: number;
  until: number;
};

const TRANSCRIPT_BOTTOM_THRESHOLD = 32;
const COLLAPSED_RESTORE_GUARD_MS = 3_000;

function readScrollMetrics(el: HTMLElement): ScrollMetrics {
  const { clientHeight, scrollHeight, scrollTop } = el;
  return {
    clientHeight,
    maxScrollTop: Math.max(0, scrollHeight - clientHeight),
    scrollHeight,
    scrollTop,
  };
}

function isTranscriptAtBottom(metrics: ScrollMetrics): boolean {
  return (
    metrics.scrollHeight - metrics.scrollTop - metrics.clientHeight <=
    TRANSCRIPT_BOTTOM_THRESHOLD
  );
}

function isCollapsedBelowGuard(
  metrics: ScrollMetrics,
  guard: CollapsedScrollRestoreGuard,
): boolean {
  return (
    Date.now() <= guard.until && metrics.maxScrollTop < guard.minMaxScrollTop
  );
}

// computeTopVisibleAnchor 找滚动容器内"视口顶部那条消息"——即第一条底边已越过视口顶
// 的 [data-message-id] 行(虚拟列表里 DOM 顺序≈消息顺序,故首条命中即视口顶那条),
// 返回其 id 与顶边在视口顶上方的 px。非贴底保存时记下它,路由重挂后据此 scrollToAnchor
// 钉回该消息,避免仅凭像素 scrollTop 在"整列还是 estimate 高度"时落到错消息的漂移。
// 找不到(无消息行 / 容器未布局)返回 null,调用方退回纯像素快照。
export function computeTopVisibleAnchor(el: HTMLElement): {
  anchorId: number;
  anchorOffset: number;
  anchorRowKey?: string;
} | null {
  const containerTop = el.getBoundingClientRect().top;
  const rows = el.querySelectorAll<HTMLElement>("[data-message-id]");
  for (const row of rows) {
    const rect = row.getBoundingClientRect();
    if (rect.bottom <= containerTop) continue; // 完全在视口上方,跳过
    const id = Number(row.getAttribute("data-message-id"));
    if (!Number.isFinite(id)) continue;
    // 行级虚拟化下一条消息拆成多行,offset 相对的是「这一行」的顶边 —— 把行身份
    // (data-row-key)一并记下,恢复端才能钉回同一行而不是塌到消息首行。
    const rowKey = row.getAttribute("data-row-key");
    return {
      anchorId: id,
      anchorOffset: Math.max(0, containerTop - rect.top),
      ...(rowKey ? { anchorRowKey: rowKey } : {}),
    };
  }
  return null;
}

// ─── Optimistic message helpers ─────────────────────────────────────────────

function textOfChatMessage(m: SvcChatMessage): string {
  for (const b of m.blocks ?? []) {
    if ((b as { type?: string }).type === "text") {
      return (b as { text?: string }).text ?? "";
    }
  }
  return "";
}

// isChatSteerNoActiveError 用 i18n 文案前缀匹配。Wails 把 service 端的
// i18n.NewError 透传成普通 Error，没有结构化 code，只能按字串识别。
function isChatSteerNoActiveError(msg: string): boolean {
  return (
    msg.includes(
      i18n.t("chatPanel.errors.noActiveConversation", { lng: "zh-CN" }),
    ) ||
    msg.includes(i18n.t("chatPanel.errors.noActiveConversation", { lng: "en" }))
  );
}

// isChatStopNoActiveError 同上：后端 ChatStopNoActive 错误码的中英文文案。
// Stop 与 turn 自然完成发生 race 时（用户点击之后、后端已自清 activeCancels）
// 会返这条；属于无害的「太晚了」，UI 不弹错。
function isChatStopNoActiveError(msg: string): boolean {
  return (
    msg.includes(
      i18n.t("chatPanel.errors.noActiveTurnToStop", { lng: "zh-CN" }),
    ) ||
    msg.includes(i18n.t("chatPanel.errors.noActiveTurnToStop", { lng: "en" }))
  );
}

function isExactCompactCommand(text: string): boolean {
  return text.trim() === "/compact";
}

type GoalCommand =
  | { kind: "get" }
  | { kind: "clear" }
  | { kind: "set"; objective: string }
  | { kind: "status"; status: "active" | "paused" | "complete" };

function parseGoalCommand(text: string): GoalCommand | null {
  const trimmed = text.trim();
  if (trimmed === "/goal") return { kind: "get" };
  if (!trimmed.startsWith("/goal ")) return null;
  const arg = trimmed.slice("/goal ".length).trim();
  if (!arg) return { kind: "get" };
  switch (arg) {
    case "clear":
      return { kind: "clear" };
    case "pause":
      return { kind: "status", status: "paused" };
    case "resume":
      return { kind: "status", status: "active" };
    case "complete":
      return { kind: "status", status: "complete" };
    default:
      return { kind: "set", objective: arg };
  }
}

const EMPTY_CLEARED: string[] = [];

function optimisticUser(
  id: number,
  sid: number,
  text: string,
  images: ChatImageAttachment[] = [],
): SvcChatMessage {
  const blocks: Array<Record<string, unknown>> = [];
  if (text) blocks.push({ type: "text", text });
  for (const image of images) {
    blocks.push({
      type: "image",
      image: {
        dataUrl: image.dataUrl,
        mediaType: image.mediaType,
        name: image.name,
      },
    });
  }
  return {
    id,
    sessionId: sid,
    role: "user",
    blocks,
    model: "",
    promptTokens: 0,
    completionTokens: 0,
    durationMs: 0,
    errorText: "",
    seq: 0,
    createtime: Date.now(),
  } as unknown as SvcChatMessage;
}

// markSessionRunning 在 Send / Regenerate / Edit 成功返回后乐观把 session 翻成
// running 态。后端落库已经是 running, 但 turn 起手没 emit session_status 事件,
// 不补一刀的话 tab / toolbar / sidebar 读 session-status-store 会停在 idle。
// permissionMode 取 store 当前值, 避免覆盖刚 set 的 plan/default 等。
function markSessionRunning(sessionId: number): void {
  if (!sessionId) return;
  const prev = useSessionStatusStore.getState().statuses.get(sessionId);
  useSessionStatusStore.getState().upsert(sessionId, {
    agentStatus: "running",
    needsAttention: false,
    permissionMode: prev?.permissionMode,
  });
}

function optimisticAssistantPlaceholder(
  id: number,
  sid: number,
): SvcChatMessage {
  return {
    id,
    sessionId: sid,
    role: "assistant",
    blocks: [],
    model: "",
    promptTokens: 0,
    completionTokens: 0,
    durationMs: 0,
    errorText: "",
    seq: 0,
    createtime: Date.now(),
  } as unknown as SvcChatMessage;
}

function upsertMessage(
  messages: SvcChatMessage[],
  next: SvcChatMessage,
): SvcChatMessage[] {
  const updated = [...messages];
  const idx = updated.findIndex((m) => m.id === next.id);
  if (idx >= 0) updated[idx] = next;
  else updated.push(next);
  return updated;
}

function applySteerConsumed(
  messages: SvcChatMessage[],
  event: ChatStreamEvent,
): SvcChatMessage[] {
  const additions = [
    ...(event.userMessages ?? []),
    ...(event.assistantMessage ? [event.assistantMessage] : []),
  ];
  const additionIDs = new Set(additions.map((m) => m.id));
  const next = messages.filter((m) => !additionIDs.has(m.id));

  let anchorIdx = -1;
  if (event.previousAssistantMessage) {
    anchorIdx = next.findIndex(
      (m) => m.id === event.previousAssistantMessage!.id,
    );
    if (anchorIdx >= 0) {
      next[anchorIdx] = event.previousAssistantMessage;
    } else {
      next.push(event.previousAssistantMessage);
      anchorIdx = next.length - 1;
    }
  }

  const insertAt = anchorIdx >= 0 ? anchorIdx + 1 : next.length;
  next.splice(insertAt, 0, ...additions);
  return next;
}

function applyStreamError(
  messages: SvcChatMessage[],
  message?: SvcChatMessage,
  error?: string,
): SvcChatMessage[] {
  if (message) return upsertMessage(messages, message);
  if (!error) return messages;

  let idx = -1;
  for (let i = messages.length - 1; i >= 0; i--) {
    if (messages[i].role === "assistant") {
      idx = i;
      break;
    }
  }
  if (idx < 0) return messages;

  const updated = [...messages];
  updated[idx] = { ...updated[idx], errorText: error } as SvcChatMessage;
  return updated;
}

// ─── ChatPanel ───────────────────────────────────────────────────────────────

type NewSessionContext = {
  /** 新建会话挂到此项目。0 = 自由会话。*/
  projectId?: number;
};

type ChatPanelProps = {
  /** 当前要渲染的会话；0 = 新建会话模式（需要配合 newSessionAgent）或空态。*/
  sessionId: number;
  /** sessionId=0 时若提供，则渲染新会话占位 + Composer，首发 RPC 后建立新会话。*/
  newSessionAgent?: ChatAgentItem | null;
  /** 新建会话时附加的项目上下文。仅 sessionId=0 路径生效。*/
  newSessionContext?: NewSessionContext;
  /** 新会话首发成功后回调，父级用来同步 selectedSessionId/agentId。*/
  onSessionCreated?: (sessionId: number, agentId: number) => void;
  /** 会话被删除或加载失败时回调，父级清掉选中状态。*/
  onSessionDeleted?: () => void;
  /** 任何会让父级列表（Agent / 项目）需要刷新的 RPC 成功后调一次。*/
  onSidebarShouldReload?: () => void;
  /** 标题上方的小字 meta，比如 "📂 Agentre / backend / sess-142"。*/
  headerTopline?: React.ReactNode;
  /** sessionId=0 且未提供 newSessionAgent 时的空态。*/
  emptyState?: React.ReactNode;
  /** 该 ChatPanel 当前是否是可见的 tab；用于在切回时补一次"跟随到底"。默认 true。*/
  active?: boolean;
  /** 当前 mounted tab 的稳定 id。用于跨路由 remount 恢复滚动；关闭 tab 后新 id 不复用。*/
  scrollStateKey?: string;
};

function ChatPanel({
  sessionId,
  newSessionAgent,
  newSessionContext,
  onSessionCreated,
  onSessionDeleted,
  onSidebarShouldReload,
  headerTopline,
  emptyState,
  active = true,
  scrollStateKey,
}: ChatPanelProps) {
  const { t } = useTranslation();
  // 流式状态(streams / queuedBySession / liveBlocks ...)全部托管在跨路由长存的
  // zustand store 里。ChatPanel 只做「读 + 派发」,不再持有状态副本,这样切到 /projects
  // 等其它路由再切回来时,store 里累积的 liveDelta / liveBlocks / queued 都能直接还原。
  const openStream = useChatStreamsStore((s) => s.openStream);
  const currentStream = useChatStreamsStore((s) =>
    sessionId ? (s.streams.get(sessionId) ?? null) : null,
  );
  const currentQueued = useQueuedMessagesStore(
    (s) => s.queuedBySession.get(sessionId) ?? null,
  );
  // doneTick / lastDoneEvent 从 session-status-store 读取。
  // 每次 turn 结束（done/error/aborted/closed/steer_consumed）bumpDone 自增 doneTick，
  // ChatPanel 的 lastSeenDoneTickRef effect 据此触发 reload + 副作用。
  const liveStatus = useSessionStatusStore((s) =>
    sessionId ? (s.statuses.get(sessionId) ?? null) : null,
  );
  const doneTick = liveStatus?.doneTick ?? 0;
  const lastDoneEvent = liveStatus?.lastDoneEvent ?? null;

  const [pendingRegenId, setPendingRegenId] = React.useState<number | null>(
    null,
  );
  const [pendingDeleteId, setPendingDeleteId] = React.useState<number | null>(
    null,
  );
  // pendingRename 取代旧 window.prompt：null = 未弹窗；非空 = 显示 Dialog + Input。
  // draft 跟随用户在 Input 里的输入；提交时调 RenameChatSession，关闭时清空。
  const [pendingRename, setPendingRename] = React.useState<{
    id: number;
    draft: string;
  } | null>(null);
  // notice 取代旧 window.alert：所有 RPC 失败 / 重要提示统一渲染成 composer 上方的内联 Alert。
  // kind=info 用于成功后的提醒（带 token 复制等），error 用于失败；用户点 × 关闭即可。
  const [notice, setNotice] = React.useState<{
    kind: "error" | "info";
    text: string;
  } | null>(null);
  // 「编辑用户消息」：点编辑后把目标消息文本直接载入 Composer。带 sessionId 在切换会话
  // 时自动失效，免得弄个 useEffect 在会话切换时手动 setState 一遍。
  const [editingMessage, setEditingMessage] = React.useState<{
    sessionId: number;
    messageId: number;
    text: string;
  } | null>(null);
  const activeEditing =
    editingMessage && editingMessage.sessionId === sessionId
      ? editingMessage
      : null;

  const {
    session,
    messages,
    setMessages,
    error: sessionError,
    reload: reloadSession,
  } = useChatSession(sessionId);

  const { reason: attentionReason } = useSessionAttention(sessionId);

  // ── 自主续轮(后台任务完成,CLI 自主跑的一轮)的会话级旁路订阅 ──
  // per-turn 流名只有用户 Send 才会从后端响应里拿到;自主轮没有这个入口,所以后端
  // 经会话级事件 "chat:autonomous:<sessionId>"(后端 AutonomousEventPrefix)推一帧
  // StreamAutonomousStarted。收到后:乐观翻 running + 插入新 assistant 行 + openStream
  // 订阅该自主轮的 per-turn 流,让它像普通 turn 一样实时渲染。后续 chunk/done 与
  // StreamDone→reloadSession 都复用既有路径,自主轮无需任何特殊渲染分支。
  // completedTask: 若事件携带后台任务身份,立即把对应 live tool_use block 的
  // subagent.status 翻成终态,刷新后台任务面板胶囊。
  // subagent_activity_started: 后台 subagent 开始产生内部活动(本地 claudecode 专有)。
  // 重开发起消息已有的 per-turn 流把嵌套块渲染回 AgentSpawnCard;不新建消息行,
  // session 保持 idle 态。remote claudecode 目前不发此事件。
  const onAutonomousEvent = React.useCallback(
    (ev: ChatStreamEvent) => {
      // subagent_activity_started: 后台 subagent 开始产生内部活动。
      // 只重开发起消息的流（openStream 绑到已存在的 launchMessageId）；
      // 不插入新消息行，不把 session 翻成 running（后台工作保持 idle 态）。
      if (ev.kind === "subagent_activity_started") {
        if (!ev.stream || !ev.launchMessageId) return;
        openStream({
          name: ev.stream,
          sessionId,
          assistantMessageId: ev.launchMessageId,
          streamStartedAt: Date.now(),
        });
        return;
      }
      if (ev.kind !== "autonomous_started") {
        return;
      }
      // 先翻转后台任务状态 (completedTask 可能在没有 assistantMessage 时也存在)
      if (ev.completedTask?.toolUseId) {
        useChatStreamsStore
          .getState()
          .mergeSubagentMeta(sessionId, ev.completedTask.toolUseId, {
            status: ev.completedTask.status,
            summary: ev.completedTask.summary,
          } as chat_svc.ChatBlockSubagent);
      }
      if (!ev.assistantMessage || !ev.stream) {
        return;
      }
      const amsg = ev.assistantMessage;
      markSessionRunning(sessionId);
      openStream({
        name: ev.stream,
        sessionId,
        assistantMessageId: amsg.id,
        streamStartedAt: Date.now(),
      });
      setMessages((prev) =>
        prev.some((m) => m.id === amsg.id) ? prev : [...prev, amsg],
      );
    },
    [sessionId, openStream, setMessages],
  );
  useChatStream(
    sessionId ? `chat:autonomous:${sessionId}` : null,
    onAutonomousEvent,
  );

  // ── 内部派生 breadcrumb（从 session.projectId + useProjectTree）──
  const { tree } = useProjectTree();
  const sessionProjectId = session?.projectId ?? 0;
  const currentSessionId = session?.id ?? 0;
  const newSessionProjectName = React.useMemo(() => {
    const projectId = newSessionContext?.projectId ?? 0;
    if (projectId <= 0) return "";
    return projectChain(tree, projectId).join(" / ");
  }, [newSessionContext?.projectId, tree]);
  const derivedTopline = React.useMemo(() => {
    if (currentSessionId <= 0) return null;
    const sessionIDNode = (
      <span className="text-muted-foreground">sess-{currentSessionId}</span>
    );
    if (sessionProjectId <= 0) return sessionIDNode;
    const chain = projectChain(tree, sessionProjectId);
    if (chain.length === 0) return sessionIDNode;
    const projectTextColorClass = agentTextColorClassName(
      findProjectColorToken(tree, sessionProjectId),
    );
    return (
      <span className="inline-flex items-center gap-1.5">
        <Folder
          className={cn("size-3", projectTextColorClass)}
          aria-hidden="true"
        />
        <span className={projectTextColorClass}>{chain.join(" / ")}</span>
        <span className="text-muted-foreground">·</span>
        {sessionIDNode}
      </span>
    );
  }, [tree, sessionProjectId, currentSessionId]);

  // 持久化的会话已被删（或加载失败），优雅通知父级回退
  React.useEffect(() => {
    if (sessionError && sessionId) {
      onSessionDeleted?.();
    }
  }, [sessionError, sessionId, onSessionDeleted]);

  // ── Transcript 滚动跟随 ──
  // atBottomRef = 用户上次滚动后是否停在底部附近（32px 容差）。
  // 新内容到达时只有"在底部"才自动跟随，否则保持当前位置不打扰用户阅读。
  const transcriptRef = React.useRef<HTMLElement>(null);
  const transcriptHandleRef = React.useRef<ChatTranscriptHandle>(null);
  const [transcriptElement, setTranscriptElement] =
    React.useState<HTMLElement | null>(null);
  const atBottomRef = React.useRef(true);
  // autoFollowRef = 「贴底跟随意图」,与 atBottomRef(纯位置:距底 ≤32px)不同 ——
  // 它对「内容增长把底部推远」免疫,只有用户主动上滚才置 false、滚回底部再置 true。
  // 流式逐 chunk 的贴底必须用它做闸:位置式 atBottomRef 在内容增长快过滚动时(正是
  // 流式)会被误判成"用户离开了底部"而永久关掉跟随,导致转录区冻结、输出沉到折叠线下。
  const autoFollowRef = React.useRef(true);
  // 记录上一次滚动后的 scrollTop,用来区分「用户上滚(scrollTop 变小)」与「内容增长/
  // 程序化贴底(scrollTop 不变或变大)」—— 只有前者才解除 autoFollow。
  const lastScrollTopRef = React.useRef(0);
  const [showBackToBottom, setShowBackToBottom] = React.useState(() => {
    const saved = loadTranscriptScrollState(scrollStateKey);
    return Boolean(saved && !saved.atBottom);
  });
  const restoredScrollStateKeyRef = React.useRef<string | null>(null);
  const pendingScrollRestoreRef = React.useRef<{
    key: string;
    scrollTop: number;
  } | null>(null);
  const collapsedScrollSaveGuardRef =
    React.useRef<CollapsedScrollRestoreGuard | null>(null);
  const collapsedRestoreFrameRef = React.useRef<number | null>(null);
  const sidebarOpen = useChatSidebarStore((s) => s.open);
  const setSidebarOpen = useChatSidebarStore((s) => s.setOpen);
  // 右侧 outline 高亮联动：跟踪 transcript 当前视野焦点对应的 message id。
  const activeMessageId = useVisibleMessageId(transcriptRef);

  const cancelCollapsedRestoreFrame = React.useCallback(() => {
    if (collapsedRestoreFrameRef.current == null) return;
    window.cancelAnimationFrame(collapsedRestoreFrameRef.current);
    collapsedRestoreFrameRef.current = null;
  }, []);

  const setTranscriptPaintSuppressed = React.useCallback(
    (suppressed: boolean) => {
      const el = transcriptRef.current;
      if (!el) return;
      el.style.visibility = suppressed ? "hidden" : "";
    },
    [],
  );

  const saveScrollSnapshot = React.useCallback(
    (snapshot: TranscriptScrollSnapshot) => {
      atBottomRef.current = snapshot.atBottom;
      setShowBackToBottom(!snapshot.atBottom);
      saveTranscriptScrollState(scrollStateKey, snapshot);
    },
    [scrollStateKey],
  );

  const restoreCollapsedScrollPosition = React.useCallback(() => {
    const guard = collapsedScrollSaveGuardRef.current;
    if (!guard || guard.key !== scrollStateKey) return false;
    const el = transcriptRef.current;
    if (!el) return false;
    const metrics = readScrollMetrics(el);
    if (metrics.maxScrollTop >= guard.minMaxScrollTop) {
      const nextScrollTop = guard.atBottom
        ? metrics.maxScrollTop
        : guard.scrollTop;
      el.scrollTop = nextScrollTop;
      saveScrollSnapshot({
        atBottom: guard.atBottom,
        scrollTop: nextScrollTop,
      });
      collapsedScrollSaveGuardRef.current = null;
      setTranscriptPaintSuppressed(false);
      cancelCollapsedRestoreFrame();
      return true;
    }
    if (Date.now() <= guard.until) return false;
    collapsedScrollSaveGuardRef.current = null;
    setTranscriptPaintSuppressed(false);
    cancelCollapsedRestoreFrame();
    return false;
  }, [
    cancelCollapsedRestoreFrame,
    scrollStateKey,
    saveScrollSnapshot,
    setTranscriptPaintSuppressed,
  ]);

  const startCollapsedRestoreLoop = React.useCallback(() => {
    cancelCollapsedRestoreFrame();
    const tick = () => {
      collapsedRestoreFrameRef.current = null;
      const guard = collapsedScrollSaveGuardRef.current;
      if (!guard || guard.key !== scrollStateKey) return;
      if (restoreCollapsedScrollPosition()) return;
      if (collapsedScrollSaveGuardRef.current?.key !== scrollStateKey) return;
      collapsedRestoreFrameRef.current = window.requestAnimationFrame(tick);
    };
    collapsedRestoreFrameRef.current = window.requestAnimationFrame(tick);
  }, [
    cancelCollapsedRestoreFrame,
    scrollStateKey,
    restoreCollapsedScrollPosition,
  ]);

  React.useEffect(
    () => () => {
      cancelCollapsedRestoreFrame();
    },
    [cancelCollapsedRestoreFrame],
  );

  const saveBottomScrollPosition = React.useCallback(
    (metrics: ScrollMetrics) => {
      const el = transcriptRef.current;
      if (!el) return;
      el.scrollTop = metrics.maxScrollTop;
      saveScrollSnapshot({ atBottom: true, scrollTop: el.scrollTop });
    },
    [saveScrollSnapshot],
  );

  const skipWhileCollapsedHeight = React.useCallback(
    (metrics: ScrollMetrics) => {
      const guard = collapsedScrollSaveGuardRef.current;
      if (!guard || guard.key !== scrollStateKey) return false;
      return isCollapsedBelowGuard(metrics, guard);
    },
    [scrollStateKey],
  );

  const armCollapsedScrollRestore = React.useCallback(
    (saved: TranscriptScrollSnapshot) => {
      collapsedScrollSaveGuardRef.current = {
        atBottom: saved.atBottom,
        key: scrollStateKey ?? "",
        minMaxScrollTop: Math.max(
          0,
          saved.scrollTop - TRANSCRIPT_BOTTOM_THRESHOLD,
        ),
        scrollTop: saved.scrollTop,
        until: Date.now() + COLLAPSED_RESTORE_GUARD_MS,
      };
      setTranscriptPaintSuppressed(true);
      startCollapsedRestoreLoop();
    },
    [scrollStateKey, setTranscriptPaintSuppressed, startCollapsedRestoreLoop],
  );

  const handleTranscriptScroll = React.useCallback(() => {
    const el = transcriptRef.current;
    if (!el) return;
    const metrics = readScrollMetrics(el);
    // prevScrollTop 留给 nextAutoFollow 区分「用户上滚」与「内容增长/程序化贴底」。
    const prevScrollTop = lastScrollTopRef.current;
    lastScrollTopRef.current = metrics.scrollTop;
    const guard = collapsedScrollSaveGuardRef.current;
    if (guard && guard.key === scrollStateKey) {
      if (restoreCollapsedScrollPosition()) return;
      if (skipWhileCollapsedHeight(metrics)) return;
    }
    const saved = loadTranscriptScrollState(scrollStateKey);
    if (
      saved?.atBottom &&
      metrics.maxScrollTop > saved.scrollTop + TRANSCRIPT_BOTTOM_THRESHOLD &&
      metrics.scrollTop <= saved.scrollTop + TRANSCRIPT_BOTTOM_THRESHOLD
    ) {
      el.scrollTop = metrics.maxScrollTop;
      saveScrollSnapshot({ atBottom: true, scrollTop: metrics.maxScrollTop });
      autoFollowRef.current = true;
      lastScrollTopRef.current = metrics.maxScrollTop;
      return;
    }
    const atBottom = isTranscriptAtBottom(metrics);
    autoFollowRef.current = nextAutoFollow({
      prev: autoFollowRef.current,
      prevScrollTop,
      scrollTop: metrics.scrollTop,
      atBottom,
    });
    // 非贴底才记锚点(贴底走 followOnAppend / 结构性 follow 还原,不需要)。
    const anchor = atBottom ? null : computeTopVisibleAnchor(el);
    saveScrollSnapshot({
      atBottom,
      scrollTop: metrics.scrollTop,
      ...(anchor ?? {}),
    });
  }, [
    restoreCollapsedScrollPosition,
    saveScrollSnapshot,
    scrollStateKey,
    skipWhileCollapsedHeight,
  ]);
  const setTranscriptNode = React.useCallback((node: HTMLElement | null) => {
    transcriptRef.current = node;
    setTranscriptElement(node);
  }, []);

  // ── 当前选中会话的派生视图 ──
  // 没有 LiveStream entry 表示该会话当前不在生成中；UI 的 typing indicator /
  // liveDelta / liveThinking / liveBlocks 全部来自 store,天然按 sessionId 隔离。
  const liveDelta = currentStream?.liveDelta ?? "";
  const liveThinking = currentStream?.liveThinking ?? "";
  const liveBlocks = currentStream?.liveBlocks ?? null;
  const liveRetry = currentStream?.liveRetry ?? null;
  const liveUsage = currentStream?.liveUsage ?? null;
  const liveContextWindow = currentStream?.liveContextWindow ?? 0;
  const liveStreamStartedAt = currentStream?.streamStartedAt ?? null;
  const liveTargetId = currentStream?.assistantMessageId ?? null;
  const liveCompacting = currentStream?.liveCompacting ?? false;
  const streaming = currentStream !== null;

  // CLI mode 控件：claudecode 使用 permission mode，codex 使用 collaboration
  // mode 的 default/plan 子集。DB 是 source-of-truth；新会话还没有 sessionId
  // 时先保存在本地 state，首发 Send payload 会把 mode 写入新 session 行。
  const activeBackendType =
    session?.backendType ?? newSessionAgent?.backendType ?? "";

  // Claude Code OAuth 配额 HUD:仅 claudecode backend 显示。device 维度优先 session
  // (已存在的会话),sessionId=0 新建态回退到 newSessionAgent —— 否则远端 agent 起的
  // 新会话还没发送时,quotaDeviceKey 会落到 "local" 把桌面本机配额错画上去。
  const activeDeviceID = session?.deviceID ?? newSessionAgent?.deviceID ?? "";
  const activeDeviceName =
    session?.deviceName ?? newSessionAgent?.deviceName ?? "";
  const quotaDeviceKey =
    activeBackendType === "claudecode"
      ? activeDeviceID
        ? `remote:${activeDeviceID}`
        : "local"
      : "";
  const quotaUsage = useCCUsage(quotaDeviceKey);
  const quotaDeviceLabel = activeDeviceID
    ? activeDeviceName || `device #${activeDeviceID}`
    : t("chatPanel.localDevice");
  // caps 来自后端 runtime 的 Capabilities — UI 不再按 backendType 硬分支。
  // 已有 session 走 GetSessionCapabilities;新对话(sessionId<=0)按
  // newSessionAgent.backendType 走 GetBackendCapabilities — 这样 PermissionModePill
  // 在新对话首发前就能正确渲染并落定 backend 预设的 defaultPermissionMode。
  const { caps: sessionCaps } = useSessionCapabilities(
    sessionId > 0 ? sessionId : undefined,
  );
  const { caps: backendCaps } = useBackendCapabilities(
    sessionId > 0 ? undefined : (newSessionAgent?.backendType ?? undefined),
  );
  const caps = sessionCaps ?? backendCaps;
  const isModeSwitchable = !!caps?.has("set_permission_mode");
  const supportsImageInput = !!caps?.has("image_input");
  const supportsCompactRPC = caps
    ? caps.has("compact")
    : activeBackendType === "codex" || activeBackendType === "piagent";

  // composerContextUsage：当前会话 inputBox 底栏的「上下文用量」数据。
  //   - max  = session.contextWindow（解析顺序见 chat_svc.resolveContextWindowWithRuntime；为 0 时整块隐藏）。
  //   - used 优先用 liveUsage（runtime translator 在每次 API call 边界后端推一条
  //     StreamUsage,TotalInputTokens 已 family-aware 聚合好），fallback 到最新
  //     一条 assistant message 的 totalInputTokens 列。
  const effectiveContextWindow =
    liveContextWindow > 0 ? liveContextWindow : (session?.contextWindow ?? 0);
  const composerContextUsage = React.useMemo(
    () =>
      computeComposerContextUsage(messages, effectiveContextWindow, liveUsage),
    [messages, effectiveContextWindow, liveUsage],
  );
  const taskProgress = React.useMemo(
    () => deriveTaskProgress(messages, currentStream?.liveBlocks ?? []),
    [messages, currentStream?.liveBlocks],
  );
  const clearedList = useClearedBackgroundTasksStore((s) =>
    sessionId > 0 ? (s.cleared[sessionId] ?? EMPTY_CLEARED) : EMPTY_CLEARED,
  );
  const clearedSet = React.useMemo(() => new Set(clearedList), [clearedList]);
  const backgroundTasks = React.useMemo(
    () =>
      deriveBackgroundTasks(
        messages,
        currentStream?.liveBlocks ?? [],
        clearedSet,
      ),
    [messages, currentStream?.liveBlocks, clearedSet],
  );
  const clearCompletedTasks = useClearedBackgroundTasksStore(
    (s) => s.clearCompleted,
  );
  const handleClearCompleted = React.useCallback(() => {
    if (sessionId <= 0) return;
    const doneIds = backgroundTasks
      .filter((tk) => tk.status === "completed" || tk.status === "failed")
      .map((tk) => tk.toolUseId);
    clearCompletedTasks(sessionId, doneIds);
  }, [sessionId, backgroundTasks, clearCompletedTasks]);
  // PermissionMode pill 数据从 caps.permissionModeMeta 拉;caps 未到位时
  // 用空 meta 做 placeholder(pill 整体被 isModeSwitchable 守护)。
  const permissionModeMeta = caps?.permissionModeMeta ?? {
    allowedModes: [],
    defaultMode: "",
    switchableDuringTurn: false,
    order: [],
  };
  const permissionMode = usePermissionMode({
    sessionId: isModeSwitchable && sessionId > 0 ? sessionId : undefined,
    permissionModeMeta,
    runtimeKey: activeBackendType,
    initialMode: session?.permissionMode,
    initialModeAtLaunch: session?.permissionModeAtLaunch,
    hasActiveSession: messages.length > 0,
    // 新会话场景下 session 还不存在 → initialMode 是 undefined；
    // 这里把 backend 管理员预设透下去，让 pill 起手值和 chat_svc.Send
    // spawn 时的 mode 一致（否则会被硬编码默认值覆盖）。
    backendDefaultMode: newSessionAgent?.defaultPermissionMode,
  });
  // switchableDuringTurn=false 的 runtime(典型 codex)在 turn 进行中不允许切 mode。
  const modeSwitchingDisabled =
    permissionModeMeta.switchableDuringTurn === false &&
    (streaming ||
      session?.agentStatus === "running" ||
      session?.agentStatus === "waiting");

  // prop 优先，无 prop 时降级到内部派生值。
  const effectiveTopline = headerTopline ?? derivedTopline;

  React.useLayoutEffect(() => {
    const el = transcriptRef.current;
    if (!el || !scrollStateKey) {
      return;
    }
    if (
      restoredScrollStateKeyRef.current === scrollStateKey &&
      pendingScrollRestoreRef.current?.key !== scrollStateKey
    ) {
      return;
    }
    const saved = loadTranscriptScrollState(scrollStateKey);
    if (!saved || saved.atBottom) {
      pendingScrollRestoreRef.current = null;
      restoredScrollStateKeyRef.current = scrollStateKey;
      return;
    }
    atBottomRef.current = false;
    // 优先锚点恢复:让虚拟器把保存时视口顶那条消息钉回原处,并随逐行复测收敛——
    // 不受"路由重挂时整列还是 estimate 高度→像素 scrollTop 落到错消息"的冷启动漂移。
    if (saved.anchorId != null) {
      if (
        transcriptHandleRef.current?.scrollToAnchor(
          saved.anchorId,
          saved.anchorOffset ?? 0,
          saved.anchorRowKey,
        )
      ) {
        pendingScrollRestoreRef.current = null;
        restoredScrollStateKeyRef.current = scrollStateKey;
        return;
      }
      // 锚点消息尚未加载进 displayMessages:先用 scrollTop 占位(避免顶部闪一下),
      // 留 pending 等下次 messages 变化(消息到位)再由 scrollToAnchor 精确钉回。
      pendingScrollRestoreRef.current = {
        key: scrollStateKey,
        scrollTop: saved.scrollTop,
      };
      el.scrollTop = saved.scrollTop;
      return;
    }
    // 回退:旧快照无锚点(贴底时存的 / 保存时无消息行)时,沿用像素恢复 + 逐渲染重试。
    pendingScrollRestoreRef.current = {
      key: scrollStateKey,
      scrollTop: saved.scrollTop,
    };
    el.scrollTop = saved.scrollTop;
    const metrics = readScrollMetrics(el);
    if (metrics.maxScrollTop < saved.scrollTop) {
      return;
    }
    pendingScrollRestoreRef.current = null;
    restoredScrollStateKeyRef.current = scrollStateKey;
  }, [
    messages,
    liveDelta,
    liveThinking,
    liveBlocks,
    liveRetry,
    scrollStateKey,
    sessionId,
    transcriptElement,
  ]);

  React.useLayoutEffect(() => {
    if (pendingScrollRestoreRef.current?.key === scrollStateKey) {
      return;
    }
    if (!atBottomRef.current) {
      return;
    }
    const el = transcriptRef.current;
    if (!el) {
      return;
    }
    // tab 被 display:none 隐藏时 clientHeight=0，scrollHeight 也是 0；
    // 此时设 scrollTop=0 会让切回来时停在顶部。跳过，等切回 tab 后
    // 由 active 切换恢复逻辑兜底滚到底部。
    if (el.clientHeight === 0) {
      return;
    }
    saveBottomScrollPosition(readScrollMetrics(el));
    // 依赖里只留 messages(结构性变化:首屏加载 / 发送乐观追加 / turn 落定 reload)。
    // 流式逐 chunk 的贴底由下面单独的 effect 接管(挂 liveDelta/liveThinking/...)。
  }, [messages, scrollStateKey, saveBottomScrollPosition]);

  // 流式逐 chunk 贴底。曾经把这件事完全交给虚拟器的 anchorTo:"end"(见 chat.tsx),
  // 但那条路只在「距底 ≤ 32px 钉底容差」时才钉:turn 开头结构性 follow 滚到的是
  // 占位行 estimate 高度的底部,真实流式文本测量出来更高 → 首帧就落后 >32px →
  // anchorTo:"end" 再也咬不回来,整轮转录区冻结、最新输出沉到折叠线下面(回归 bug)。
  // 这里改成「意图驱动」:只要 autoFollowRef(贴底跟随意图,对内容增长免疫、仅用户上滚
  // 才解除,见 nextAutoFollow)为真,就随每个流式增量把滚动钉到真实底部。读的是已提交 DOM
  // 的实时 scrollHeight(readScrollMetrics 同步触发 reflow),不是慢一帧的虚拟器 getTotalSize
  // 估值,故不掉队;钉到真实底部后虚拟器的 anchorTo:"end" 也回到容差内、自然协同而非互抢。
  React.useLayoutEffect(() => {
    if (pendingScrollRestoreRef.current?.key === scrollStateKey) {
      return;
    }
    if (!autoFollowRef.current) {
      return;
    }
    const el = transcriptRef.current;
    if (!el || el.clientHeight === 0) {
      return;
    }
    saveBottomScrollPosition(readScrollMetrics(el));
  }, [
    liveDelta,
    liveThinking,
    liveBlocks,
    liveRetry,
    scrollStateKey,
    saveBottomScrollPosition,
  ]);

  // tab 从隐藏(display:none)切回可见时，上面的 useLayoutEffect 在隐藏期间
  // 会被 clientHeight===0 跳过。这里由父层 HostedPanel 传入的 active 信号
  // 驱动恢复：若用户切走前停在底部，就补一次 scrollTop=scrollHeight。
  const prevActiveRef = React.useRef(active);
  React.useLayoutEffect(() => {
    const prev = prevActiveRef.current;
    prevActiveRef.current = active;
    if (!active || prev) return;
    const saved = loadTranscriptScrollState(scrollStateKey);
    if (saved && saved.scrollTop > 0) {
      armCollapsedScrollRestore(saved);
    }
    if (!atBottomRef.current) {
      return;
    }
    const el = transcriptRef.current;
    if (!el) {
      return;
    }
    const metrics = readScrollMetrics(el);
    if (skipWhileCollapsedHeight(metrics)) {
      return;
    }
    saveBottomScrollPosition(metrics);
  }, [
    active,
    armCollapsedScrollRestore,
    saveBottomScrollPosition,
    scrollStateKey,
    skipWhileCollapsedHeight,
  ]);

  // 切换会话时回到底部
  React.useEffect(() => {
    const saved = loadTranscriptScrollState(scrollStateKey);
    if (saved && !saved.atBottom) {
      atBottomRef.current = false;
      autoFollowRef.current = false;
      return;
    }
    atBottomRef.current = true;
    autoFollowRef.current = true;
    restoredScrollStateKeyRef.current = null;
    pendingScrollRestoreRef.current = null;
  }, [scrollStateKey, sessionId]);

  const handleBackToBottom = React.useCallback(() => {
    const el = transcriptRef.current;
    if (!el) return;
    // 用户主动点「回到底部」= 明确想跟随,恢复 autoFollow(上滚解除后由此重新咬合)。
    autoFollowRef.current = true;
    saveBottomScrollPosition(readScrollMetrics(el));
  }, [saveBottomScrollPosition]);

  // ── 跨路由 turn 落定后的善后 ──
  // store 在 done/error/closed 时给该 sessionId 自增 doneTick。我们只关心「当前正在
  // 显示」的会话:抓最新的 lastDoneEvent,reload 一次 useChatSession 把后端写好的
  // 最终 blocks(穿插顺序)拉回来,然后做 error 文案等副作用。
  // MarkChatSessionRead 不在这里调 —— 由下方 active-gated effect 在
  // reloadSession 拉到新的 session.lastMessageAt 后自动触发(隐藏 tab active=false
  // 不应被标已读)。
  // 第一次 mount 时 doneTick=0,什么都不做(用 ref 跳过首次)。
  const lastSeenDoneTickRef = React.useRef(doneTick);
  React.useEffect(() => {
    if (!sessionId) return;
    if (doneTick === lastSeenDoneTickRef.current) return;
    lastSeenDoneTickRef.current = doneTick;
    const ev = lastDoneEvent;
    if (!ev) return;
    if (ev.kind === "steer_consumed") {
      setMessages((prev) => applySteerConsumed(prev, ev));
      void reloadSession();
      onSidebarShouldReload?.();
    } else if (ev.kind === "done") {
      // 后端在发 done 前已经 chat_repo.Message().Update,reload 拿到最终顺序。
      void reloadSession();
      onSidebarShouldReload?.();
    } else if (ev.kind === "error") {
      // 错误路径:后端同样 Update 过 assistant.errorText,但有可能 final message 已附带
      // ev.message。两条路都靠 reload 把最新落库状态拿回来;再补 errorText 落到 UI。
      if (ev.message) {
        setMessages((prev) => upsertMessage(prev, ev.message!));
      } else if (ev.error) {
        setMessages((prev) => applyStreamError(prev, undefined, ev.error));
      }
      void reloadSession();
      onSidebarShouldReload?.();
    } else if (ev.kind === "aborted") {
      // 用户主动「停止」：后端已经把 partial 内容写入 DB 且 errorText 为空。
      // 走和 done 一样的 reload 路径即可，让 transcript 渲染 partial 结果；
      // 不调 MarkRead（abort 不是「用户已读完」语义）。
      void reloadSession();
      onSidebarShouldReload?.();
    } else if (ev.kind === "closed") {
      // closed 单独出现(没先来 done/error)通常意味着 wails 端被关掉,不算 turn 结束,
      // 不主动 reload 也不动 errorText —— 与旧版行为对齐。
    }
  }, [
    doneTick,
    lastDoneEvent,
    onSidebarShouldReload,
    reloadSession,
    sessionId,
    setMessages,
  ]);

  // ── Mark-read: 仅当当前 ChatPanel 是「可见 tab」时,把 lastMessageAt 推进到
  // 服务端 last_read_at 并同步到 read overlay。chat-panel-host 会把所有 tab
  // 都 mount(隐藏 tab 用 display:none),所以一定要 gate 在 active prop 上 ——
  // 否则后台 tab 在 turn 完成 / 启动恢复时会被错误地标记为已读,未读 indicator
  // 永远不出现。
  //
  // 触发时机:active 翻成 true、sessionId 变化、或 session.lastMessageAt 推进
  // (turn 落定 → reloadSession → meta/lastMessageAt 更新)。lastMessageAt=0
  // 是没有消息的新会话,无需 mark。
  const sessionLastMessageAt = session?.lastMessageAt ?? 0;
  React.useEffect(() => {
    if (!active || sessionId <= 0 || sessionLastMessageAt <= 0) return;
    void MarkChatSessionRead({
      sessionId,
      timestamp: sessionLastMessageAt,
    });
    useSessionReadStore.getState().markRead(sessionId, sessionLastMessageAt);
  }, [active, sessionId, sessionLastMessageAt]);

  async function doSend(
    targetSessionId: number,
    agentId: number,
    message: ChatComposerSubmit,
    permissionModeOverride?: string,
  ) {
    const text = message.text.trim();
    const images = message.images ?? [];
    // 发送消息时强制跟随到底部，无论用户当前在哪里
    atBottomRef.current = true;
    setShowBackToBottom(false);
    // 调用点都是 void doSend(...) fire-and-forget；这里必须自吞错误成 notice，
    // 否则 RPC 失败时 UI 完全无声（用户在 composer 干瞪眼）。doEnqueue 的 fallback
    // 也走这里，set notice 后不 rethrow，正好顶替 doEnqueue 原本的 setNotice。
    try {
      // 新建会话路径：把项目上下文带上（仅 targetSessionId=0 时生效）；
      // 已存在会话续发：projectId 在 Send 端被忽略，传 0 也无害。
      const sendPayload: Record<string, unknown> = {
        sessionId: targetSessionId,
        agentId,
        text,
        projectId:
          targetSessionId === 0 ? (newSessionContext?.projectId ?? 0) : 0,
        permissionMode:
          permissionModeOverride ??
          (isModeSwitchable ? permissionMode.mode : ""),
      };
      if (images.length > 0) {
        sendPayload.images = images.map((image) => ({
          name: image.name,
          dataUrl: image.dataUrl,
        }));
      }
      const resp = await SendChatMessage(
        chat_svc.SendRequest.createFrom(sendPayload),
      );
      // 新建会话路径：通知父级把 selectedSessionId 切到新 id。
      if (targetSessionId === 0 && resp.sessionId) {
        onSessionCreated?.(resp.sessionId, agentId);
      }
      setMessages((prev) => [
        ...prev,
        optimisticUser(resp.userMessageId, resp.sessionId, text, images),
        optimisticAssistantPlaceholder(resp.assistantMessageId, resp.sessionId),
      ]);
      // 乐观写 running: 后端 Send 已把 sess.AgentStatus="running" 落库, 但 turn
      // 起手没 emit session_status 事件, tab / 详情 toolbar 单纯读 store 会停在
      // 上一轮的 idle。这里同步翻成 running, 让所有订阅者一帧内看到运行态。
      markSessionRunning(resp.sessionId);
      openStream({
        name: resp.stream,
        sessionId: resp.sessionId,
        assistantMessageId: resp.assistantMessageId,
        streamStartedAt: Date.now(),
      });
      // 创建新会话时后端在 RPC 内已写入 AgentStatus="running" 并落库，
      // 立刻 reload 让左侧 sidebar 同步出现新会话 + running 状态，不用等 turn 结束。
      onSidebarShouldReload?.();
    } catch (e: unknown) {
      const msg = e instanceof Error ? e.message : String(e);
      console.error("[chat] send failed", e);
      setNotice({ kind: "error", text: t("chatPanel.errors.send", { msg }) });
    }
  }

  async function doCompact(sid: number) {
    if (!sid) return;
    try {
      atBottomRef.current = true;
      setShowBackToBottom(false);
      const resp = await CompactChatSession({ sessionId: sid });
      setMessages((prev) => {
        if (prev.some((m) => m.id === resp.assistantMessageId)) return prev;
        return [
          ...prev,
          optimisticAssistantPlaceholder(
            resp.assistantMessageId,
            resp.sessionId,
          ),
        ];
      });
      markSessionRunning(resp.sessionId);
      openStream({
        name: resp.stream,
        sessionId: resp.sessionId,
        assistantMessageId: resp.assistantMessageId,
        streamStartedAt: Date.now(),
      });
      onSidebarShouldReload?.();
    } catch (e: unknown) {
      const msg = e instanceof Error ? e.message : String(e);
      console.error("[chat] compact failed", e);
      setNotice({
        kind: "error",
        text: t("chatPanel.errors.compact", { msg }),
      });
    }
  }

  async function doGoal(sid: number, agentId: number, cmd: GoalCommand) {
    if (!sid) return;
    try {
      if (cmd.kind === "get") {
        const resp = await GetChatGoal({ sessionId: sid });
        const goal = resp.goal;
        setNotice({
          kind: "info",
          text: goal
            ? t("chatPanel.goal.current", {
                objective: goal.objective,
                status: goal.status,
                tokens: goal.tokensUsed ?? 0,
              })
            : t("chatPanel.goal.empty"),
        });
        return;
      }
      if (cmd.kind === "clear") {
        await ClearChatGoal({ sessionId: sid });
        setNotice({ kind: "info", text: t("chatPanel.goal.cleared") });
        return;
      }
      const payload =
        cmd.kind === "set"
          ? { sessionId: sid, objective: cmd.objective, status: "active" }
          : { sessionId: sid, status: cmd.status };
      const resp = await SetChatGoal(payload);
      setNotice({
        kind: "info",
        text: resp.goal
          ? t("chatPanel.goal.updatedWithObjective", {
              objective: resp.goal.objective,
            })
          : t("chatPanel.goal.updated"),
      });
      if (cmd.kind === "set") {
        await doSend(sid, agentId, { text: cmd.objective });
      }
    } catch (e: unknown) {
      const msg = e instanceof Error ? e.message : String(e);
      console.error("[chat] goal failed", e);
      setNotice({ kind: "error", text: t("chatPanel.errors.goal", { msg }) });
    }
  }

  async function doStartGoal(
    agentId: number,
    cmd: Extract<GoalCommand, { kind: "set" }>,
  ) {
    try {
      const resp = await StartChatGoal({
        agentId,
        projectId: newSessionContext?.projectId ?? 0,
        objective: cmd.objective,
        status: "active",
        permissionMode: isModeSwitchable ? permissionMode.mode : "",
      });
      if (resp.sessionId) {
        onSessionCreated?.(resp.sessionId, agentId);
      }
      onSidebarShouldReload?.();
      setNotice({
        kind: "info",
        text: resp.goal
          ? t("chatPanel.goal.updatedWithObjective", {
              objective: resp.goal.objective,
            })
          : t("chatPanel.goal.updated"),
      });
      if (resp.sessionId) {
        await doSend(resp.sessionId, agentId, { text: cmd.objective });
      }
    } catch (e: unknown) {
      const msg = e instanceof Error ? e.message : String(e);
      console.error("[chat] start goal failed", e);
      setNotice({ kind: "error", text: t("chatPanel.errors.goal", { msg }) });
    }
  }

  function notifyCompactNeedsSession() {
    setNotice({
      kind: "info",
      text: t("chatPanel.compact.needsSession"),
    });
  }

  function notifyCompactWaitForTurn() {
    setNotice({ kind: "info", text: t("chatPanel.compact.waitForTurn") });
  }

  const handlePlanActionStarted = React.useCallback(
    (resp: PlanActionStream, userText: string) => {
      if (!resp.stream || !resp.sessionId || !resp.assistantMessageId) return;
      atBottomRef.current = true;
      setShowBackToBottom(false);
      setMessages((prev) => {
        const next = [...prev];
        if (!next.some((m) => m.id === resp.userMessageId)) {
          next.push(
            optimisticUser(resp.userMessageId, resp.sessionId, userText),
          );
        }
        if (!next.some((m) => m.id === resp.assistantMessageId)) {
          next.push(
            optimisticAssistantPlaceholder(
              resp.assistantMessageId,
              resp.sessionId,
            ),
          );
        }
        return next;
      });
      markSessionRunning(resp.sessionId);
      openStream({
        name: resp.stream,
        sessionId: resp.sessionId,
        assistantMessageId: resp.assistantMessageId,
        streamStartedAt: Date.now(),
      });
      onSidebarShouldReload?.();
    },
    [onSidebarShouldReload, openStream, setMessages],
  );

  // doEnqueue：streaming 中按回车走这里。把新消息推到当前 turn 的排队队列，
  // 等 AI 跑到下一个安全点（claudecode PreToolUse hook / codex turn/steer RPC 即刻 /
  // builtin cago safe-point）才注入。
  async function doEnqueue(sid: number, agentId: number, text: string) {
    try {
      const resp = await EnqueueChatMessage({ sessionId: sid, text });
      useQueuedMessagesStore.getState().append(sid, {
        id: resp.queuedId,
        text,
        cancellable: resp.cancellable,
      });
    } catch (e: unknown) {
      const msg = e instanceof Error ? e.message : String(e);
      if (isChatSteerNoActiveError(msg)) {
        // turn 已结束（done/closed 事件即将到 / 已到），按普通 send 重新起一轮。
        await doSend(sid, agentId, { text });
        return;
      }
      console.error("[chat] enqueue failed", e);
      setNotice({
        kind: "error",
        text: t("chatPanel.errors.enqueue", { msg }),
      });
    }
  }

  async function doCancelQueued(sid: number, queuedId: string) {
    try {
      const resp = await CancelQueuedChatMessage({ sessionId: sid, queuedId });
      useQueuedMessagesStore.getState().consume(sid, resp.removed);
    } catch (e: unknown) {
      const msg = e instanceof Error ? e.message : String(e);
      console.error("[chat] cancel queued failed", e);
      setNotice({
        kind: "error",
        text: t("chatPanel.errors.cancelQueued", { msg }),
      });
    }
  }

  // doStop 软中断当前 turn。后端会按 backend 分别走 control_request{interrupt}
  // /turn/interrupt/ctx-cancel，子进程都保留，发个 StreamAborted 事件让 store
  // bump tick → reload 拿 partial 内容。这里不做乐观 UI，等 aborted 事件回来。
  async function doStop(sid: number) {
    try {
      await StopChatMessage({ sessionId: sid });
    } catch (e: unknown) {
      const msg = e instanceof Error ? e.message : String(e);
      // turn 已自然完成与点击 Stop 发生 race —— 后端 activeCancels 已经清掉了，
      // 不算错，静默即可（用户的意图是「让这轮停下」，结果已经停了）。
      if (isChatStopNoActiveError(msg)) {
        console.warn("[chat] stop race-lost (turn already finished)");
        return;
      }
      console.error("[chat] stop failed", e);
      setNotice({ kind: "error", text: t("chatPanel.errors.stop", { msg }) });
    }
  }

  function handleRegenerate(messageId: number) {
    if (!sessionId) return;
    setPendingRegenId(messageId);
  }

  async function confirmRegenerate() {
    const messageId = pendingRegenId;
    if (messageId == null || !sessionId) {
      setPendingRegenId(null);
      return;
    }
    setPendingRegenId(null);

    const snapshot = messages;
    const targetIdx = snapshot.findIndex((m) => m.id === messageId);
    if (targetIdx < 0) return;
    let userIdx = -1;
    for (let i = targetIdx - 1; i >= 0; i--) {
      if (snapshot[i].role === "user") {
        userIdx = i;
        break;
      }
    }
    if (userIdx < 0) return;
    const userText = textOfChatMessage(snapshot[userIdx]);

    try {
      atBottomRef.current = true;
      setShowBackToBottom(false);
      const resp = await RegenerateChatMessage({
        sessionId,
        messageId,
        permissionMode: isModeSwitchable ? permissionMode.mode : "",
      });
      markSessionRunning(resp.sessionId);
      openStream({
        name: resp.stream,
        sessionId: resp.sessionId,
        assistantMessageId: resp.assistantMessageId,
        streamStartedAt: Date.now(),
      });
      setMessages([
        ...snapshot.slice(0, userIdx),
        optimisticUser(resp.userMessageId, resp.sessionId, userText),
        optimisticAssistantPlaceholder(resp.assistantMessageId, resp.sessionId),
      ]);
      onSidebarShouldReload?.();
    } catch (e: unknown) {
      console.error("[chat] regenerate failed", e);
      const msg = e instanceof Error ? e.message : String(e);
      setNotice({
        kind: "error",
        text: t("chatPanel.errors.regenerate", { msg }),
      });
    }
  }

  async function confirmRename() {
    if (!pendingRename) return;
    const next = pendingRename.draft.trim();
    if (!next) {
      setPendingRename(null);
      return;
    }
    const id = pendingRename.id;
    setPendingRename(null);
    try {
      await RenameChatSession({ sessionId: id, title: next });
      onSidebarShouldReload?.();
    } catch (e: unknown) {
      const msg = e instanceof Error ? e.message : String(e);
      console.error("[chat] rename failed", e);
      setNotice({
        kind: "error",
        text: t("chatPanel.errors.rename", { msg }),
      });
    }
  }

  function handleDelete(id: number) {
    setPendingDeleteId(id);
  }

  async function handleCopyLaunchCommand(sid: number) {
    try {
      const resp = await GetChatLaunchCommand({ sessionId: sid });
      await copyTextWithToast(resp.command, {
        errorTitle: t("chatPanel.launchCommand.copyFailed"),
        successTitle: t("chatPanel.launchCommand.copyDone"),
        successDescription: t("chatPanel.launchCommand.copyDescription"),
      });
    } catch (e: unknown) {
      const msg = e instanceof Error ? e.message : String(e);
      console.error("[chat] copy launch command failed", e);
      setNotice({
        kind: "error",
        text: t("chatPanel.errors.copyLaunchCommand", { msg }),
      });
    }
  }

  async function confirmDelete() {
    const id = pendingDeleteId;
    if (id == null) return;
    setPendingDeleteId(null);
    await DeleteChatSession({ sessionId: id });
    onSessionDeleted?.();
    onSidebarShouldReload?.();
  }

  function handleEdit(messageId: number) {
    if (!sessionId) return;
    const target = messages.find((m) => m.id === messageId);
    if (!target || target.role !== "user") return;
    setEditingMessage({
      sessionId,
      messageId,
      text: textOfChatMessage(target),
    });
  }

  async function confirmEdit(newText: string) {
    const pending = activeEditing;
    if (pending == null || !sessionId) {
      setEditingMessage(null);
      return;
    }
    const trimmed = newText.trim();
    if (!trimmed) {
      setEditingMessage(null);
      return;
    }
    setEditingMessage(null);

    const snapshot = messages;
    const targetIdx = snapshot.findIndex((m) => m.id === pending.messageId);
    if (targetIdx < 0) return;

    try {
      atBottomRef.current = true;
      setShowBackToBottom(false);
      const resp = await EditChatMessage({
        sessionId,
        messageId: pending.messageId,
        text: trimmed,
        permissionMode: isModeSwitchable ? permissionMode.mode : "",
      });
      markSessionRunning(resp.sessionId);
      openStream({
        name: resp.stream,
        sessionId: resp.sessionId,
        assistantMessageId: resp.assistantMessageId,
        streamStartedAt: Date.now(),
      });
      setMessages([
        ...snapshot.slice(0, targetIdx),
        optimisticUser(resp.userMessageId, resp.sessionId, trimmed),
        optimisticAssistantPlaceholder(resp.assistantMessageId, resp.sessionId),
      ]);
      onSidebarShouldReload?.();
    } catch (e: unknown) {
      console.error("[chat] edit failed", e);
      const msg = e instanceof Error ? e.message : String(e);
      setNotice({ kind: "error", text: t("chatPanel.errors.edit", { msg }) });
    }
  }

  // ── render ──
  const showNewSessionPrompt = !sessionId && newSessionAgent;
  const showEmpty = !sessionId && !newSessionAgent;

  return (
    <TooltipProvider delayDuration={200}>
      {/* Wails 事件订阅器现在挂在 App 顶层的 <ChatStreamsHost />,跨路由长存,
          ChatPanel 这里只读 store 状态、不再自己订阅。 */}

      {showEmpty ? (
        (emptyState ?? null)
      ) : (
        // 关键：showNewSessionPrompt → 已有会话 切换时，<ChatComposer> 必须保持挂载，
        // 否则 TipTap editor 实例随子树卸载重建，用户刚发完首条消息焦点就跑了。
        // 布局：toolbar 整行铺顶（无 newSessionPrompt 时），下面 flex row 分两栏 ——
        //   左栏 flex-col：transcript（或新会话占位）+ notice + ChatComposer，
        //   右栏：ChatContextSidebar 占满整高（从 toolbar 下沿一直到底）。
        //   ChatComposer 固定在左栏的最后一个 child 位置，跨分支保持同一 React 实例。
        <main className="flex min-h-0 min-w-0 flex-1 flex-col bg-background">
          {/* ── Toolbar / Header ── */}
          {/* 单行 meta：breadcrumb · dot+agent · relativeTime · device(tooltip cwd)。
              原先三行 (breadcrumb / agent+RUNNING / device+cwd) 在 44-52px 内压缩到
              一行；状态文案 (RUNNING/WAITING) 用裸 dot 的颜色携带，去掉大写 mono 标签。
              cwd 太长不适合常驻一行，挪到 DeviceTag 的 tooltip。 */}
          {showNewSessionPrompt ? null : (
            <div
              role="toolbar"
              aria-label={t("chatPanel.toolbar.aria")}
              className="flex min-h-[44px] shrink-0 items-center gap-3 border-b border-border px-5 py-1.5"
            >
              {session
                ? (() => {
                    const rawStatus: AgentStatus =
                      (session.agentStatus as AgentStatus) || "idle";
                    const toolbarStatus = reasonToDisplayStatus(
                      attentionReason,
                      rawStatus,
                    );
                    const statusTone =
                      statusConfig[toolbarStatus].textClassName;
                    return (
                      <>
                        <AgentAvatar
                          name={session.agentName}
                          initials={session.agentName.charAt(0)}
                          color={
                            (session.agentColor as AgentColor) || "agent-1"
                          }
                          size="md"
                        />
                        <div className="min-w-0 flex-1">
                          <div
                            className="line-clamp-2 break-words text-sm font-semibold leading-snug"
                            title={session.title || t("chatPanel.untitled")}
                          >
                            {session.title || t("chatPanel.untitled")}
                          </div>
                          <div className="mt-0.5 flex min-w-0 flex-wrap items-center gap-x-1.5 gap-y-0 font-mono text-2xs text-muted-foreground">
                            {effectiveTopline ? (
                              <>
                                <span className="inline-flex min-w-0 items-center text-primary-text">
                                  {effectiveTopline}
                                </span>
                                <span className="text-border-strong">·</span>
                              </>
                            ) : null}
                            <span className="inline-flex shrink-0 items-center gap-1">
                              <StatusDot status={toolbarStatus} size="xs" />
                              <span className={cn(statusTone)}>
                                {session.agentName}
                              </span>
                            </span>
                            <span className="text-border-strong">·</span>
                            <span className="shrink-0">
                              {relativeTime(session.lastMessageAt)}
                            </span>
                            {session.deviceID ? (
                              <>
                                <span className="text-border-strong">·</span>
                                {session.cwd ? (
                                  <Tooltip>
                                    <TooltipTrigger asChild>
                                      <DeviceTag
                                        deviceId={session.deviceID}
                                        deviceName={
                                          session.deviceName || session.deviceID
                                        }
                                        online={session.online ?? true}
                                      />
                                    </TooltipTrigger>
                                    <TooltipContent
                                      side="bottom"
                                      className="max-w-[480px] break-all font-mono text-[11px]"
                                    >
                                      {session.cwd}
                                    </TooltipContent>
                                  </Tooltip>
                                ) : (
                                  <DeviceTag
                                    deviceId={session.deviceID}
                                    deviceName={
                                      session.deviceName || session.deviceID
                                    }
                                    online={session.online ?? true}
                                  />
                                )}
                              </>
                            ) : null}
                          </div>
                        </div>
                        {/* 后台任务胶囊：有运行中任务时显示，点击展开只读弹层 */}
                        <BackgroundTasksChip
                          tasks={backgroundTasks}
                          onClearCompleted={handleClearCompleted}
                        />
                        {(() => {
                          // canStop 双源：
                          //   1. currentStream !== null —— 本客户端刚起的 turn，
                          //      openStream 是同步 store 写，比 useChatSession.reload
                          //      早；解决 Regenerate / Edit / Send-existing 的「服务端
                          //      已 running 但前端 agentStatus 还是上轮 idle」窗口。
                          //   2. status === running/waiting —— 服务端权威态，覆盖
                          //      「app 重启时另一会话仍在跑」场景（store 里没 entry）。
                          const canStop =
                            currentStream !== null ||
                            toolbarStatus === "running" ||
                            toolbarStatus === "waiting";
                          return (
                            <Button
                              type="button"
                              variant="outline"
                              size="sm"
                              disabled={!canStop}
                              onClick={() => void doStop(session.id)}
                              title={
                                canStop
                                  ? t("chatPanel.toolbar.stopActiveTitle")
                                  : t("chatPanel.toolbar.stopInactiveTitle")
                              }
                            >
                              <Square
                                data-icon="inline-start"
                                aria-hidden="true"
                              />
                              {t("chatPanel.toolbar.stop")}
                            </Button>
                          );
                        })()}
                        <Button
                          type="button"
                          variant="outline"
                          size="icon-sm"
                          aria-label={t("chatPanel.toolbar.contextSidebar")}
                          onClick={() => setSidebarOpen(!sidebarOpen)}
                          title={
                            sidebarOpen
                              ? t("chatPanel.toolbar.hideContextSidebar")
                              : t("chatPanel.toolbar.showContextSidebar")
                          }
                        >
                          {sidebarOpen ? (
                            <PanelRightClose
                              data-icon="only"
                              aria-hidden="true"
                            />
                          ) : (
                            <PanelRight data-icon="only" aria-hidden="true" />
                          )}
                        </Button>
                        {/* Pencil Ozwj8 — More dropdown */}
                        <DropdownMenu>
                          <DropdownMenuTrigger asChild>
                            <Button
                              type="button"
                              variant="outline"
                              size="icon-sm"
                              aria-label={t("common.moreActions")}
                            >
                              <MoreHorizontal
                                data-icon="only"
                                aria-hidden="true"
                              />
                            </Button>
                          </DropdownMenuTrigger>
                          <DropdownMenuContent align="end">
                            <DropdownMenuItem
                              onClick={() =>
                                setPendingRename({
                                  id: session.id,
                                  draft: session.title,
                                })
                              }
                            >
                              {t("chatPanel.actions.rename")}
                            </DropdownMenuItem>
                            {(session.backendType === "claudecode" ||
                              session.backendType === "codex" ||
                              session.backendType === "piagent") && (
                              <DropdownMenuItem
                                onClick={() =>
                                  void handleCopyLaunchCommand(session.id)
                                }
                              >
                                {t("chatPanel.launchCommand.copy")}
                              </DropdownMenuItem>
                            )}
                            <DropdownMenuItem
                              className="text-destructive focus:text-destructive"
                              onClick={() => void handleDelete(session.id)}
                            >
                              {t("common.delete")}
                            </DropdownMenuItem>
                          </DropdownMenuContent>
                        </DropdownMenu>
                      </>
                    );
                  })()
                : null}
            </div>
          )}

          {/* ── Body row: 左栏 chat / 右栏 sidebar 占满整高 ──
              输入框宽度 = transcript 宽度,与对话流同列;sidebar 从 toolbar 下沿一路顶到底。 */}
          <div className="flex min-h-0 min-w-0 flex-1">
            {/* ── Body: chat ── */}
            <div className="flex min-h-0 min-w-0 flex-1 flex-col">
              {showNewSessionPrompt ? (
                <div className="flex flex-1 items-center justify-center">
                  <div className="flex flex-col items-center gap-2 text-center">
                    <div className="text-sm font-semibold">
                      {newSessionProjectName
                        ? t("chatPanel.newProjectSession.title", {
                            agentName: newSessionAgent.name,
                            projectName: newSessionProjectName,
                          })
                        : t("chatPanel.newSession.title", {
                            name: newSessionAgent.name,
                          })}
                    </div>
                    <div className="text-xs text-muted-foreground">
                      {newSessionProjectName
                        ? t("chatPanel.newProjectSession.description")
                        : t("chatPanel.newSession.description")}
                    </div>
                  </div>
                </div>
              ) : (
                <section
                  ref={setTranscriptNode}
                  data-testid="chat-transcript-scroll"
                  onScroll={handleTranscriptScroll}
                  className="min-h-0 flex-1 overflow-auto px-7 py-5"
                >
                  <ChatTranscript
                    ref={transcriptHandleRef}
                    agentName={session?.agentName ?? "Agent"}
                    agentColor={
                      (session?.agentColor as AgentColor) || "agent-1"
                    }
                    cwd={session?.cwd}
                    sessionId={session?.id ?? 0}
                    scrollElement={transcriptElement}
                    virtualize
                    active={active}
                    messages={messages}
                    liveDelta={liveDelta}
                    liveThinking={liveThinking}
                    liveBlocks={liveBlocks ?? undefined}
                    liveRetry={liveRetry}
                    liveStreamStartedAt={liveStreamStartedAt}
                    liveTargetId={liveTargetId}
                    streaming={streaming}
                    liveCompacting={liveCompacting}
                    onRerun={(messageId) => void handleRegenerate(messageId)}
                    onEdit={(messageId) => handleEdit(messageId)}
                    onPlanActionStarted={handlePlanActionStarted}
                    tabStateKey={scrollStateKey}
                  />
                  {showBackToBottom ? (
                    <Button
                      type="button"
                      data-testid="back-to-bottom-button"
                      variant="outline"
                      size="icon-sm"
                      aria-label={t("chatPanel.scroll.backToBottom")}
                      title={t("chatPanel.scroll.backToBottom")}
                      onClick={handleBackToBottom}
                      className="sticky bottom-4 z-20 ml-auto flex rounded-full bg-background shadow-md hover:shadow-lg dark:bg-background animate-in fade-in slide-in-from-bottom-1 duration-200 ease-out motion-reduce:animate-none"
                    >
                      <ArrowDown data-icon="only" aria-hidden="true" />
                    </Button>
                  ) : null}
                </section>
              )}

              {/* ── Inline notice (取代 window.alert)。
                    error / info 两种态共用一个 slot，最多挂一条；右侧 × 关闭。
                    info 用 default Alert 样式（中性），error 用 destructive。 */}
              {notice ? (
                <div className="border-t border-border bg-background px-5 pt-2">
                  <Alert
                    variant={
                      notice.kind === "error" ? "destructive" : "default"
                    }
                    className="py-2 pr-2"
                  >
                    <TriangleAlert aria-hidden="true" />
                    <AlertDescription className="flex min-w-0 items-start gap-2">
                      <span className="min-w-0 flex-1 break-words text-xs leading-snug">
                        {notice.text}
                      </span>
                      <button
                        type="button"
                        aria-label={t("chatPanel.notice.close")}
                        onClick={() => setNotice(null)}
                        className="-mr-1 inline-flex size-5 shrink-0 cursor-pointer items-center justify-center rounded-sm text-current opacity-70 transition-opacity hover:opacity-100"
                      >
                        <X className="size-3" aria-hidden="true" />
                      </button>
                    </AlertDescription>
                  </Alert>
                </div>
              ) : null}

              {/* ── Composer ── */}
              <ChatComposer
                editing={activeEditing !== null}
                editDraft={activeEditing?.text}
                onCancelEdit={() => setEditingMessage(null)}
                // 新建会话场景下，ChatPanel 的 key 变化让 Composer 重新挂载 → 自动抓焦点，
                // 用户一进来就能直接打字。续聊已有会话不抢焦点，避免打断侧栏切换的鼠标交互。
                autoFocusOnMount={!!newSessionAgent}
                contextUsage={composerContextUsage}
                quotaUsage={quotaUsage}
                quotaDeviceLabel={quotaDeviceLabel}
                permissionModeSlot={
                  isModeSwitchable ? (
                    <PermissionModePill
                      mode={permissionMode.mode}
                      modes={permissionModeMeta.order}
                      onSelect={permissionMode.setMode}
                      errorMessage={permissionMode.error}
                      disabled={modeSwitchingDisabled}
                      runtimeKey={activeBackendType}
                      permissionModeAtLaunch={
                        permissionMode.permissionModeAtLaunch
                      }
                      hasActiveSession={permissionMode.hasActiveSession}
                    />
                  ) : null
                }
                onShiftTab={
                  isModeSwitchable && !modeSwitchingDisabled
                    ? permissionMode.cycleMode
                    : undefined
                }
                topSlot={
                  <>
                    <TaskProgressBar progress={taskProgress} />
                    <QueuedMessagesBar
                      queued={currentQueued ?? []}
                      onCancel={(id) => void doCancelQueued(sessionId, id)}
                      onClearAll={() => void doCancelQueued(sessionId, "")}
                    />
                  </>
                }
                onSubmit={(message: ChatComposerSubmit | string) => {
                  message =
                    typeof message === "string" ? { text: message } : message;
                  const text = message.text.trim();
                  const images = message.images ?? [];
                  if (images.length > 0 && !supportsImageInput) {
                    setNotice({
                      kind: "error",
                      text: t("chatPanel.errors.imageUnsupported"),
                    });
                    return;
                  }
                  if (activeEditing) {
                    void confirmEdit(text);
                    return;
                  }
                  const goalCommand =
                    activeBackendType === "codex"
                      ? parseGoalCommand(text)
                      : null;
                  if (goalCommand) {
                    if (images.length > 0) {
                      setNotice({
                        kind: "error",
                        text: t("chatPanel.goal.imageUnsupported"),
                      });
                      return;
                    }
                    if (streaming) {
                      setNotice({
                        kind: "info",
                        text: t("chatPanel.goal.waitForTurn"),
                      });
                      return;
                    }
                    if (!sessionId) {
                      if (newSessionAgent && goalCommand.kind === "set") {
                        void doStartGoal(newSessionAgent.id, goalCommand);
                        return;
                      }
                      setNotice({
                        kind: "info",
                        text: t("chatPanel.goal.needsSession"),
                      });
                      return;
                    }
                    void doGoal(sessionId, session?.agentId ?? 0, goalCommand);
                    return;
                  }
                  if (supportsCompactRPC && isExactCompactCommand(text)) {
                    if (!sessionId) {
                      notifyCompactNeedsSession();
                      return;
                    }
                    if (streaming) {
                      notifyCompactWaitForTurn();
                      return;
                    }
                    if (images.length > 0) {
                      setNotice({
                        kind: "error",
                        text: t("chatPanel.compact.imageUnsupported"),
                      });
                      return;
                    }
                    void doCompact(sessionId);
                    return;
                  }
                  // 新建会话首发：targetSessionId=0，由 doSend 内的 RPC 返回真实 sessionId
                  // 并通过 onSessionCreated 回填到父 store；此时 composer 不会卸载（结构稳定）。
                  if (!sessionId && newSessionAgent) {
                    void doSend(0, newSessionAgent.id, message);
                    return;
                  }
                  if (streaming && sessionId > 0) {
                    if (images.length > 0) {
                      setNotice({
                        kind: "error",
                        text: t("chatPanel.errors.imageWhileStreaming"),
                      });
                      return;
                    }
                    // streaming 中：按回车走 Enqueue，把消息排队等下一个安全点注入。
                    void doEnqueue(sessionId, session?.agentId ?? 0, text);
                    return;
                  }
                  void doSend(sessionId, session?.agentId ?? 0, message);
                }}
                backendType={activeBackendType}
                supportsImageInput={supportsImageInput}
                onSlashRpc={(cmd) => {
                  console.warn(
                    `slash rpc not wired: cmd=${cmd.name} backend=${activeBackendType}`,
                  );
                }}
              />
            </div>
            {!showNewSessionPrompt && sidebarOpen ? (
              <ChatContextSidebar
                sessionId={session?.id ?? 0}
                messages={messages}
                activeMessageId={activeMessageId}
                onJumpToMessage={(mid) => {
                  transcriptHandleRef.current?.scrollToMessage(mid);
                }}
              />
            ) : null}
          </div>
        </main>
      )}
      <Dialog
        open={pendingRegenId !== null}
        onOpenChange={(open) => {
          if (!open) setPendingRegenId(null);
        }}
      >
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{t("chatPanel.regenerateDialog.title")}</DialogTitle>
          </DialogHeader>
          <DialogBody>
            <p className="text-sm text-muted-foreground">
              {t("chatPanel.regenerateDialog.description")}
            </p>
          </DialogBody>
          <DialogFooter>
            <Button
              variant="outline"
              size="sm"
              onClick={() => setPendingRegenId(null)}
            >
              {t("common.cancel")}
            </Button>
            <Button size="sm" onClick={() => void confirmRegenerate()}>
              {t("chatPanel.regenerateDialog.confirm")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      <Dialog
        open={pendingDeleteId !== null}
        onOpenChange={(open) => {
          if (!open) setPendingDeleteId(null);
        }}
      >
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{t("chatPanel.deleteDialog.title")}</DialogTitle>
          </DialogHeader>
          <DialogBody>
            <p className="text-sm text-muted-foreground">
              {t("chatPanel.deleteDialog.description")}
            </p>
          </DialogBody>
          <DialogFooter>
            <Button
              variant="outline"
              size="sm"
              onClick={() => setPendingDeleteId(null)}
            >
              {t("common.cancel")}
            </Button>
            <Button
              size="sm"
              variant="destructive"
              onClick={() => void confirmDelete()}
            >
              {t("common.delete")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      {/* Rename Dialog 取代旧 window.prompt：把 draft 提到 state 上，
          DialogClose 由 onOpenChange 统一管理（× / Esc / 取消 都走同一 setter）。 */}
      <Dialog
        open={pendingRename !== null}
        onOpenChange={(open) => {
          if (!open) setPendingRename(null);
        }}
      >
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{t("chatPanel.renameDialog.title")}</DialogTitle>
          </DialogHeader>
          <DialogBody>
            <form
              id="rename-session-form"
              onSubmit={(e) => {
                e.preventDefault();
                void confirmRename();
              }}
            >
              <Input
                autoFocus
                value={pendingRename?.draft ?? ""}
                onChange={(e) =>
                  setPendingRename((prev) =>
                    prev ? { ...prev, draft: e.target.value } : prev,
                  )
                }
                placeholder={t("chatPanel.renameDialog.placeholder")}
                aria-label={t("chatPanel.renameDialog.nameAria")}
              />
            </form>
          </DialogBody>
          <DialogFooter>
            <Button
              variant="outline"
              size="sm"
              onClick={() => setPendingRename(null)}
            >
              {t("common.cancel")}
            </Button>
            <Button
              type="submit"
              form="rename-session-form"
              size="sm"
              disabled={
                !pendingRename || pendingRename.draft.trim().length === 0
              }
            >
              {t("common.save")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </TooltipProvider>
  );
}

export { ChatPanel };
export type { ChatPanelProps };
