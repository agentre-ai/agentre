import * as React from "react";
import {
  Folder,
  MoreHorizontal,
  PanelRight,
  PanelRightClose,
  Square,
  TriangleAlert,
  X,
} from "lucide-react";

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
import { useChatSession } from "@/hooks/use-chat-session";
import type { ChatStreamEvent } from "@/hooks/use-chat-stream";
import { useProjectTree } from "@/hooks/use-project-tree";
import { useVisibleMessageId } from "@/hooks/use-visible-message-id";
import { reasonToDisplayStatus } from "@/lib/attention-display";
import { copyTextWithToast } from "@/lib/clipboard-toast";
import { projectChain } from "@/lib/project-chain";
import { relativeTime } from "@/lib/relative-time";
import { cn } from "@/lib/utils";
import { useSessionAttention } from "@/stores/attention-store";
import { useChatStreamsStore } from "@/stores/chat-streams-store";
import { useQueuedMessagesStore } from "@/stores/queued-messages-store";
import { useSessionReadStore } from "@/stores/session-read-store";
import { useSessionStatusStore } from "@/stores/session-status-store";

import { useBackendCapabilities } from "./capability/use-backend-capabilities";
import { useSessionCapabilities } from "./capability/use-session-capabilities";
import type { PlanActionStream } from "./canonical-tool/props";
import { ChatComposer, ChatTranscript } from "./chat";
import { ChatContextSidebar } from "./chat-context-sidebar";
import { computeComposerContextUsage } from "./chat-panel-context-usage";
import { PermissionModePill, usePermissionMode } from "./permission-mode";
import { useChatSidebarStore } from "@/stores/chat-sidebar-store";
import { AgentAvatar, DeviceTag, StatusDot } from "./primitives";
import { QueuedMessagesBar } from "./queued-messages-bar";
import { deriveTaskProgress } from "./task-progress/derive";
import { TaskProgressBar } from "./task-progress/task-progress-bar";
import type { AgentColor, AgentStatus } from "./types";
import { statusConfig } from "./types";

import {
  CancelQueuedChatMessage,
  CompactChatSession,
  DeleteChatSession,
  EditChatMessage,
  EnqueueChatMessage,
  GetChatLaunchCommand,
  MarkChatSessionRead,
  RegenerateChatMessage,
  RenameChatSession,
  SendChatMessage,
  StopChatMessage,
} from "../../../wailsjs/go/app/App";
import type { chat_svc } from "../../../wailsjs/go/models";

type SvcChatMessage = chat_svc.ChatMessage;
type ChatAgentItem = chat_svc.ChatAgentItem;

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
    msg.includes("没有进行中的对话") ||
    msg.includes("No in-flight conversation")
  );
}

// isChatStopNoActiveError 同上：后端 ChatStopNoActive 错误码的中英文文案。
// Stop 与 turn 自然完成发生 race 时（用户点击之后、后端已自清 activeCancels）
// 会返这条；属于无害的「太晚了」，UI 不弹错。
function isChatStopNoActiveError(msg: string): boolean {
  return (
    msg.includes("没有正在进行的对话可停止") ||
    msg.includes("No in-flight turn to stop")
  );
}

function isExactCompactCommand(text: string): boolean {
  return text.trim() === "/compact";
}

function optimisticUser(id: number, sid: number, text: string): SvcChatMessage {
  return {
    id,
    sessionId: sid,
    role: "user",
    blocks: [{ type: "text", text }],
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
  /** sessionId=0 时若提供，则渲染"和 X 开始对话"占位 + Composer，首发 RPC 后建立新会话。*/
  newSessionAgent?: ChatAgentItem | null;
  /** 新建会话时附加的项目上下文。仅 sessionId=0 路径生效。*/
  newSessionContext?: NewSessionContext;
  /** 新会话首发成功后回调，父级用来同步 selectedSessionId/agentId。*/
  onSessionCreated?: (sessionId: number, agentId: number) => void;
  /** 会话被删除或加载失败时回调，父级清掉选中状态。*/
  onSessionDeleted?: () => void;
  /** 任何会让父级列表（Agent / 项目）需要刷新的 RPC 成功后调一次。*/
  onSidebarShouldReload?: () => void;
  /** 标题上方的小字 breadcrumb，比如 "📂 Agentre / backend / sess-142"。*/
  headerTopline?: React.ReactNode;
  /** sessionId=0 且未提供 newSessionAgent 时的空态。*/
  emptyState?: React.ReactNode;
  /** 该 ChatPanel 当前是否是可见的 tab；用于在切回时补一次"跟随到底"。默认 true。*/
  active?: boolean;
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
}: ChatPanelProps) {
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

  // ── 内部派生 breadcrumb（从 session.projectId + useProjectTree）──
  const { tree } = useProjectTree();
  const sessionProjectId = session?.projectId ?? 0;
  const currentSessionId = session?.id ?? 0;
  const derivedBreadcrumb = React.useMemo(() => {
    if (sessionProjectId <= 0) return null;
    const chain = projectChain(tree, sessionProjectId);
    if (chain.length === 0) return null;
    return (
      <span className="inline-flex items-center gap-1.5">
        <Folder className="size-3" aria-hidden="true" />
        <span>{chain.join(" / ")}</span>
        <span className="text-muted-foreground">·</span>
        <span className="text-muted-foreground">sess-{currentSessionId}</span>
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
  const atBottomRef = React.useRef(true);
  const sidebarOpen = useChatSidebarStore((s) => s.open);
  const setSidebarOpen = useChatSidebarStore((s) => s.setOpen);
  // 右侧 outline 高亮联动：跟踪 transcript 当前视野焦点对应的 message id。
  const activeMessageId = useVisibleMessageId(transcriptRef);

  const handleTranscriptScroll = React.useCallback(() => {
    const el = transcriptRef.current;
    if (!el) return;
    atBottomRef.current =
      el.scrollHeight - el.scrollTop - el.clientHeight <= 32;
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
  const effectiveTopline = headerTopline ?? derivedBreadcrumb;

  React.useLayoutEffect(() => {
    if (!atBottomRef.current) return;
    const el = transcriptRef.current;
    if (!el) return;
    // tab 被 display:none 隐藏时 clientHeight=0，scrollHeight 也是 0；
    // 此时设 scrollTop=0 会让切回来时停在顶部。跳过，等切回 tab 后
    // 的 ResizeObserver 兜底滚到底部。
    if (el.clientHeight === 0) return;
    el.scrollTop = el.scrollHeight;
  }, [messages, liveDelta, liveThinking, liveBlocks, liveRetry]);

  // tab 从隐藏(display:none)切回可见时，上面的 useLayoutEffect 在隐藏期间
  // 全部被 clientHeight===0 跳过，scrollTop 也被流式新增的内容挤离底部。
  // 这里由父层 HostedPanel 传入的 active 信号驱动：false → true 那一帧，
  // 若用户切走前停在底部 (atBottomRef.current)，补一次 scrollTop=scrollHeight。
  // 不用 ResizeObserver 是因为它依赖 WebKit 对祖先 display 切换触发回调，
  // 在新建会话型 tab 上 transcriptRef 首次 mount 时还为 null，effect 早返回后
  // 永远不再装上 observer。
  const prevActiveRef = React.useRef(active);
  React.useLayoutEffect(() => {
    const prev = prevActiveRef.current;
    prevActiveRef.current = active;
    if (!active || prev) return;
    if (!atBottomRef.current) return;
    const el = transcriptRef.current;
    if (!el) return;
    el.scrollTop = el.scrollHeight;
  }, [active]);

  // 切换会话时回到底部
  React.useEffect(() => {
    atBottomRef.current = true;
  }, [sessionId]);

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
    text: string,
    permissionModeOverride?: string,
  ) {
    // 发送消息时强制跟随到底部，无论用户当前在哪里
    atBottomRef.current = true;
    // 调用点都是 void doSend(...) fire-and-forget；这里必须自吞错误成 notice，
    // 否则 RPC 失败时 UI 完全无声（用户在 composer 干瞪眼）。doEnqueue 的 fallback
    // 也走这里，set notice 后不 rethrow，正好顶替 doEnqueue 原本的 setNotice。
    try {
      // 新建会话路径：把项目上下文带上（仅 targetSessionId=0 时生效）；
      // 已存在会话续发：projectId 在 Send 端被忽略，传 0 也无害。
      const resp = await SendChatMessage({
        sessionId: targetSessionId,
        agentId,
        text,
        projectId:
          targetSessionId === 0 ? (newSessionContext?.projectId ?? 0) : 0,
        permissionMode:
          permissionModeOverride ??
          (isModeSwitchable ? permissionMode.mode : ""),
      });
      // 新建会话路径：通知父级把 selectedSessionId 切到新 id。
      if (targetSessionId === 0 && resp.sessionId) {
        onSessionCreated?.(resp.sessionId, agentId);
      }
      setMessages((prev) => [
        ...prev,
        optimisticUser(resp.userMessageId, resp.sessionId, text),
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
      setNotice({ kind: "error", text: `发送失败：${msg}` });
    }
  }

  async function doCompact(sid: number) {
    if (!sid) return;
    try {
      atBottomRef.current = true;
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
      setNotice({ kind: "error", text: `压缩上下文失败：${msg}` });
    }
  }

  function notifyCompactNeedsSession() {
    setNotice({
      kind: "info",
      text: "请先发送一条消息让 Codex 会话启动后再压缩",
    });
  }

  function notifyCompactWaitForTurn() {
    setNotice({ kind: "info", text: "当前回复结束后再压缩" });
  }

  const handlePlanActionStarted = React.useCallback(
    (resp: PlanActionStream, userText: string) => {
      if (!resp.stream || !resp.sessionId || !resp.assistantMessageId) return;
      atBottomRef.current = true;
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
    [onSidebarShouldReload, openStream],
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
        await doSend(sid, agentId, text);
        return;
      }
      console.error("[chat] enqueue failed", e);
      setNotice({ kind: "error", text: `插入消息失败：${msg}` });
    }
  }

  async function doCancelQueued(sid: number, queuedId: string) {
    try {
      const resp = await CancelQueuedChatMessage({ sessionId: sid, queuedId });
      useQueuedMessagesStore.getState().consume(sid, resp.removed);
    } catch (e: unknown) {
      const msg = e instanceof Error ? e.message : String(e);
      console.error("[chat] cancel queued failed", e);
      setNotice({ kind: "error", text: `撤回失败：${msg}` });
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
      setNotice({ kind: "error", text: `停止失败：${msg}` });
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
      setNotice({ kind: "error", text: `重新生成失败：${msg}` });
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
      setNotice({ kind: "error", text: `重命名失败：${msg}` });
    }
  }

  function handleDelete(id: number) {
    setPendingDeleteId(id);
  }

  async function handleCopyLaunchCommand(sid: number) {
    try {
      const resp = await GetChatLaunchCommand({ sessionId: sid });
      await copyTextWithToast(resp.command, {
        errorTitle: "复制启动命令失败",
        successTitle: "已复制启动命令",
        successDescription:
          "含 token，直接粘贴到终端即可运行；本地网关重启后 token 失效需重新复制",
      });
    } catch (e: unknown) {
      const msg = e instanceof Error ? e.message : String(e);
      console.error("[chat] copy launch command failed", e);
      setNotice({ kind: "error", text: `复制启动命令失败：${msg}` });
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
      setNotice({ kind: "error", text: `编辑失败：${msg}` });
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
              aria-label="会话工具栏"
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
                            title={session.title || "(未命名)"}
                          >
                            {session.title || "(未命名)"}
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
                                  ? "停止当前对话"
                                  : "当前没有进行中的对话"
                              }
                            >
                              <Square
                                data-icon="inline-start"
                                aria-hidden="true"
                              />
                              停止
                            </Button>
                          );
                        })()}
                        <Button
                          type="button"
                          variant="outline"
                          size="icon-sm"
                          aria-label="上下文侧栏"
                          onClick={() => setSidebarOpen(!sidebarOpen)}
                          title={
                            sidebarOpen ? "隐藏上下文侧栏" : "显示上下文侧栏"
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
                              aria-label="更多操作"
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
                              重命名
                            </DropdownMenuItem>
                            {(session.backendType === "claudecode" ||
                              session.backendType === "codex") && (
                              <DropdownMenuItem
                                onClick={() =>
                                  void handleCopyLaunchCommand(session.id)
                                }
                              >
                                复制启动命令
                              </DropdownMenuItem>
                            )}
                            <DropdownMenuItem
                              className="text-destructive focus:text-destructive"
                              onClick={() => void handleDelete(session.id)}
                            >
                              删除
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
            <div className="flex min-h-0 min-w-0 flex-1 flex-col">
              {showNewSessionPrompt ? (
                <div className="flex flex-1 items-center justify-center">
                  <div className="flex flex-col items-center gap-2 text-center">
                    <div className="text-sm font-semibold">
                      和 {newSessionAgent.name} 开始对话
                    </div>
                    <div className="text-xs text-muted-foreground">
                      在下方输入框输入消息，按 Enter 发送，Shift+Enter 换行
                    </div>
                  </div>
                </div>
              ) : (
                <section
                  ref={transcriptRef}
                  onScroll={handleTranscriptScroll}
                  className="min-h-0 flex-1 overflow-auto px-7 py-5"
                >
                  <ChatTranscript
                    agentName={session?.agentName ?? "Agent"}
                    agentColor={
                      (session?.agentColor as AgentColor) || "agent-1"
                    }
                    sessionId={session?.id ?? 0}
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
                  />
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
                        aria-label="关闭提示"
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
                onSubmit={(text) => {
                  if (activeEditing) {
                    void confirmEdit(text);
                    return;
                  }
                  if (
                    activeBackendType === "codex" &&
                    isExactCompactCommand(text)
                  ) {
                    if (!sessionId) {
                      notifyCompactNeedsSession();
                      return;
                    }
                    if (streaming) {
                      notifyCompactWaitForTurn();
                      return;
                    }
                    void doCompact(sessionId);
                    return;
                  }
                  // 新建会话首发：targetSessionId=0，由 doSend 内的 RPC 返回真实 sessionId
                  // 并通过 onSessionCreated 回填到父 store；此时 composer 不会卸载（结构稳定）。
                  if (!sessionId && newSessionAgent) {
                    void doSend(0, newSessionAgent.id, text);
                    return;
                  }
                  if (streaming && sessionId > 0) {
                    // streaming 中：按回车走 Enqueue，把消息排队等下一个安全点注入。
                    void doEnqueue(sessionId, session?.agentId ?? 0, text);
                    return;
                  }
                  void doSend(sessionId, session?.agentId ?? 0, text);
                }}
                backendType={activeBackendType}
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
                  const el = document.querySelector(
                    `[data-message-id="${mid}"]`,
                  );
                  if (el && el instanceof HTMLElement) {
                    el.scrollIntoView({ behavior: "smooth", block: "start" });
                  }
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
            <DialogTitle>重新生成回复</DialogTitle>
          </DialogHeader>
          <DialogBody>
            <p className="text-sm text-muted-foreground">
              这条回复及它之后的所有消息会被丢弃，agent 会基于上一条 user
              消息重新生成新的回复。
            </p>
          </DialogBody>
          <DialogFooter>
            <Button
              variant="outline"
              size="sm"
              onClick={() => setPendingRegenId(null)}
            >
              取消
            </Button>
            <Button size="sm" onClick={() => void confirmRegenerate()}>
              确认重新生成
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
            <DialogTitle>删除会话</DialogTitle>
          </DialogHeader>
          <DialogBody>
            <p className="text-sm text-muted-foreground">
              删除后此会话的所有消息会被一并清除，且无法恢复。
            </p>
          </DialogBody>
          <DialogFooter>
            <Button
              variant="outline"
              size="sm"
              onClick={() => setPendingDeleteId(null)}
            >
              取消
            </Button>
            <Button
              size="sm"
              variant="destructive"
              onClick={() => void confirmDelete()}
            >
              删除
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
            <DialogTitle>重命名会话</DialogTitle>
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
                placeholder="输入新名称"
                aria-label="会话名称"
              />
            </form>
          </DialogBody>
          <DialogFooter>
            <Button
              variant="outline"
              size="sm"
              onClick={() => setPendingRename(null)}
            >
              取消
            </Button>
            <Button
              type="submit"
              form="rename-session-form"
              size="sm"
              disabled={
                !pendingRename || pendingRename.draft.trim().length === 0
              }
            >
              保存
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </TooltipProvider>
  );
}

export { ChatPanel };
export type { ChatPanelProps };
