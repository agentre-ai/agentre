import * as React from "react";
import {
  ArrowDown,
  ArrowUp,
  Check,
  Gauge,
  LoaderCircle,
  Pencil,
  RefreshCw,
  SendHorizontal,
  TriangleAlert,
  Wrench,
  X,
} from "lucide-react";

import { Button } from "@/components/ui/button";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";

import { PlanApproveCard } from "./canonical-tool/plan-approve-request/card";
import type { PlanActionStream } from "./canonical-tool/props";
import { CanonicalToolRouter } from "./canonical-tool/registry";
import { AIChatInput, type AIChatInputHandle } from "./chat-input";
import { CodeBlock } from "./code-block";
import { CompactBoundaryDivider } from "./compact-boundary-divider";
import { CompactHistoryFold } from "./compact-history-fold";
import { MarkdownText } from "./markdown-text";
import { AgentAvatar } from "./primitives";
import { ThinkingBlock } from "./thinking-block";

// isSubagentCanonical 替代旧 isSubagentTool(name) — name-based 检测改为读
// canonical.kind。translator 在 emit 时已经把 Task/Agent/collabAgent 工具识别成
// canonical.agentSpawn,这里直接 dispatch。
function isSubagentCanonical(block: {
  canonical?: { kind?: string };
}): boolean {
  return block.canonical?.kind === "agent.spawn";
}
import type { AgentColor, AgentStatus } from "./types";

// isAskUserQuestionToolName 旧 tool-summary.ts 同名;此处过滤掉 AskUserQuestion 类工具的
// tool_use 块,避免与 ask_user_question 独立 block 渲染的 UserAskCard 重复出卡。
function isAskUserQuestionToolName(toolName: string | undefined): boolean {
  if (!toolName) return false;
  const name = toolName.toLowerCase();
  return name === "askuserquestion" || name === "ask_user_question";
}
import { statusConfig } from "./types";
import type { ChatBlockData, RetryNotice } from "@/stores/chat-streams-store";
import type { chat_svc } from "../../../wailsjs/go/models";

function isRenderablePlanBlock(block: ChatBlockData): boolean {
  const canonical = block.canonical;
  if (canonical?.kind !== "plan.update" || !canonical.planUpdate) return false;
  const actions = canonical.planUpdate.actions ?? [];
  if (actions.length > 0) return true;
  const text = canonical.planUpdate.text ?? block.text ?? "";
  const steps = canonical.planUpdate.steps ?? [];
  return text.trim().length > 0 && steps.length === 0;
}

type ChatMessageProps = React.ComponentProps<"article"> & {
  author: string;
  avatarColor?: AgentColor;
  children: React.ReactNode;
  initials?: string;
  meta?: React.ReactNode;
  time: string;
  /** "assistant" (默认): 渲染 agent avatar + 名字。
   *  "user": 渲染中性的「我」头像 —— 与 agent 头像视觉对称，但走 muted
   *  色阶不抢焦点，把主视觉留给 agent。 */
  variant?: "user" | "assistant";
};

function ChatMessage({
  author,
  avatarColor = "agent-1",
  children,
  className,
  initials,
  meta,
  time,
  variant = "assistant",
  ...props
}: ChatMessageProps) {
  const isUser = variant === "user";
  return (
    <article className={cn("flex gap-3 text-sm", className)} {...props}>
      {isUser ? (
        <span
          aria-label="我"
          role="img"
          className="inline-flex size-7 shrink-0 items-center justify-center rounded-lg bg-muted text-[11px] font-semibold text-muted-foreground"
        >
          我
        </span>
      ) : (
        <AgentAvatar
          name={author}
          initials={initials}
          color={avatarColor}
          size="md"
          className="size-7 text-[11px]"
        />
      )}
      <div className="flex min-w-0 max-w-[720px] flex-1 flex-col gap-1">
        <div className="flex items-center gap-2">
          {/* user 行: 不渲染名字（前缀 "›" 已是充足的「这是我说的」信号）。
              assistant 行：保留 agent 名字 + 时间。 */}
          {isUser ? null : <span className="font-semibold">{author}</span>}
          <span className="font-mono text-[10px] text-muted-foreground">
            {time}
          </span>
        </div>
        <div
          data-selectable-text="true"
          className="flex flex-col gap-2 leading-[1.55]"
        >
          {children}
        </div>
        {meta ? (
          <div className="mt-1 font-mono text-[10px] text-subtle-foreground">
            {meta}
          </div>
        ) : null}
      </div>
    </article>
  );
}

type ToolCallProps = React.ComponentProps<"div"> & {
  path?: string;
  status?: AgentStatus;
  statusLabel: string;
  toolName: string;
};

function ToolCall({
  className,
  path,
  status = "running",
  statusLabel,
  toolName,
  ...props
}: ToolCallProps) {
  const config = statusConfig[status];
  const StatusIcon = status === "waiting" ? LoaderCircle : Check;

  return (
    <div
      data-selectable-text="true"
      className={cn(
        "flex w-full max-w-[720px] flex-col gap-1.5 rounded-md border border-border bg-card px-3 py-2.5",
        className,
      )}
      {...props}
    >
      <div className="flex min-w-0 items-center gap-1.5 font-mono text-xs">
        <Wrench className="size-3.5 shrink-0 text-primary-text" />
        <span className="font-semibold text-primary-text">{toolName}</span>
        {path ? (
          <>
            <span className="text-muted-foreground">·</span>
            <span className="min-w-0 truncate text-muted-foreground">
              {path}
            </span>
          </>
        ) : null}
      </div>
      <div className="flex items-center gap-1.5 font-mono text-[11px]">
        <StatusIcon className={cn("size-3", config.textClassName)} />
        <span className={status === "running" ? config.textClassName : ""}>
          {statusLabel}
        </span>
      </div>
    </div>
  );
}

type MessageMetaProps = {
  cacheCreationTokens?: number;
  cachedTokens?: number;
  completionTokens: number;
  durationMs: number;
  model: string;
  onRerun?: () => void;
  promptTokens: number;
  reasoningTokens?: number;
};

function MessageMeta({
  cacheCreationTokens = 0,
  cachedTokens = 0,
  completionTokens,
  durationMs,
  model,
  onRerun,
  promptTokens,
  reasoningTokens = 0,
}: MessageMetaProps) {
  const durationLabel = `${(durationMs / 1000).toFixed(1)}s`;

  // tooltip 里需要拆分展示，所以这里给一个稳定的 row 渲染器避免重复。
  const rows: { label: string; value: string }[] = [
    { label: "模型", value: model || "—" },
    { label: "发送 (prompt)", value: promptTokens.toLocaleString() },
    { label: "接收 (completion)", value: completionTokens.toLocaleString() },
  ];
  if (cachedTokens > 0) {
    rows.push({ label: "缓存命中", value: cachedTokens.toLocaleString() });
  }
  if (cacheCreationTokens > 0) {
    rows.push({
      label: "缓存写入",
      value: cacheCreationTokens.toLocaleString(),
    });
  }
  if (reasoningTokens > 0) {
    rows.push({ label: "思考", value: reasoningTokens.toLocaleString() });
  }
  rows.push({ label: "耗时", value: durationLabel });

  return (
    <div className="flex flex-wrap items-center gap-1.5">
      <Tooltip>
        <TooltipTrigger asChild>
          <button
            type="button"
            className="inline-flex items-center gap-1.5 rounded px-1 py-0.5 hover:bg-muted/60"
            aria-label="token 用量明细"
          >
            {model ? (
              <>
                <span>{model}</span>
                <span className="text-border-strong">·</span>
              </>
            ) : null}
            <span className="inline-flex items-center gap-0.5">
              <ArrowUp className="size-2.5" aria-hidden="true" />
              {promptTokens.toLocaleString()}
            </span>
            <span className="inline-flex items-center gap-0.5">
              <ArrowDown className="size-2.5" aria-hidden="true" />
              {completionTokens.toLocaleString()}
            </span>
            <span className="text-border-strong">·</span>
            <span>{durationLabel}</span>
          </button>
        </TooltipTrigger>
        <TooltipContent className="font-mono text-[11px]">
          <table className="border-separate border-spacing-x-3 border-spacing-y-0.5">
            <tbody>
              {rows.map((row) => (
                <tr key={row.label}>
                  <td className="text-left text-muted-foreground">
                    {row.label}
                  </td>
                  <td className="text-right tabular-nums">{row.value}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </TooltipContent>
      </Tooltip>
      {onRerun ? (
        <Button
          type="button"
          variant="ghost"
          size="xs"
          className="ml-1 h-5 gap-1 px-1.5 text-[10px] text-muted-foreground"
          onClick={onRerun}
        >
          <RefreshCw data-icon="inline-start" aria-hidden="true" />
          重新生成
        </Button>
      ) : null}
    </div>
  );
}

// UserMessageActions 渲染 user 气泡的 action 行：目前只有「编辑」。
// 作为 `meta` prop 传入 ChatMessage，常驻显示在消息下方。
function UserMessageActions({ onEdit }: { onEdit: () => void }) {
  return (
    <div className="flex items-center gap-1.5">
      <Button
        type="button"
        variant="ghost"
        size="xs"
        className="h-5 gap-1 px-1.5 text-[10px] text-muted-foreground"
        onClick={onEdit}
      >
        <Pencil data-icon="inline-start" aria-hidden="true" />
        编辑
      </Button>
    </div>
  );
}

type ApprovalGateProps = React.ComponentProps<"section"> & {
  description: string;
  onApprove?: () => void;
  onReject?: () => void;
  title: string;
};

function ApprovalGate({
  className,
  description,
  onApprove,
  onReject,
  title,
  ...props
}: ApprovalGateProps) {
  return (
    <section
      className={cn(
        "flex w-full max-w-[720px] items-center gap-3 rounded-lg border border-status-waiting bg-status-waiting-bg px-4 py-3",
        className,
      )}
      {...props}
    >
      <TriangleAlert
        className="size-5 shrink-0 text-status-waiting"
        aria-hidden="true"
      />
      <div className="min-w-0 flex-1">
        <div className="text-xs font-semibold text-status-waiting">{title}</div>
        <div className="mt-0.5 text-xs leading-snug">{description}</div>
      </div>
      <Button
        type="button"
        variant="outline"
        size="sm"
        className="h-8"
        onClick={onReject}
      >
        拒绝
      </Button>
      <Button
        type="button"
        size="sm"
        className="h-8 bg-status-running text-status-running-foreground hover:bg-status-running/90"
        onClick={onApprove}
      >
        批准
      </Button>
    </section>
  );
}

type ChatComposerProps = Omit<React.ComponentProps<"form">, "onSubmit"> & {
  onSubmit?: (message: string) => void;
  placeholder?: string;
  /** 历史消息文本，按时间倒序排列（最新在前），方向键 ↑↓ 浏览。 */
  userMessageHistory?: string[];
  /** 编辑模式：true 时输入框上方挂"正在编辑"提示条，发送按钮文案改为"保存"。 */
  editing?: boolean;
  /** 进入编辑模式时载入到输入框的初始草稿。每次切换编辑目标都会重新载入。 */
  editDraft?: string;
  /** 用户点击提示条上的"取消编辑"。父组件应清掉编辑状态并恢复正常输入。 */
  onCancelEdit?: () => void;
  /** 在输入框上方插入的内容（例：QueuedMessagesBar）。Composer 不关心内容，
   *  只负责把节点放进 card 顶部，跟 editing banner 同位。空节点正常渲染（组件自决可见性）。 */
  topSlot?: React.ReactNode;
  /** 上下文用量。max <= 0 时整块不渲染（未知模型 / 后端未配置）。 */
  contextUsage?: { used: number; max: number };
  /** Permission mode 控件，仅在 claudecode 后端时由 chat-panel 注入。null 时整块不渲染。 */
  permissionModeSlot?: React.ReactNode;
  /** 焦点在 composer 内时按下 Shift+Tab 的钩子（用于循环切换 permission mode）。 */
  onShiftTab?: () => void;
  /** 挂载时自动 focus 输入框。新建会话场景下由 chat-panel 传 true，让用户一打开
   *  就能直接打字、不用再点输入框一次。 */
  autoFocusOnMount?: boolean;
  /** 当前会话 backend 类型;让 AIChatInput 启用 slash menu 并按 backend 过滤候选命令。
   *  空串/省略 → 不启用 slash menu。 */
  backendType?: string;
  /** slash menu rpc 类命令的回调(literal_text 类由 AIChatInput 内部直接填回编辑器,
   *  不自动发送,也不会冒泡到这里)。省略则 slash menu 不启用。 */
  onSlashRpc?: (
    cmd: import("./slash-commands").SlashCommand,
    exec: Extract<import("./slash-commands").SlashExec, { kind: "rpc" }>,
  ) => void;
};

// 把 token 数显示成 "42.3k / 200k" 这种紧凑形式，跟 inline 底栏的 10px 字号匹配。
// >= 1000 时按 k 缩写并保留 1 位小数；< 1000 时直接显示。
function formatTokens(n: number): string {
  if (n < 1000) return String(n);
  const v = n / 1000;
  return v >= 100 ? `${Math.round(v)}k` : `${v.toFixed(1)}k`;
}

function ContextMeter({ used, max }: { used: number; max: number }) {
  const safeUsed = Math.max(0, used);
  const ratio = max > 0 ? Math.min(1, safeUsed / max) : 0;
  const pct = Math.round(ratio * 100);
  // 阈值色：>90% 触红，>75% 触黄，其余 primary。沿用 status-* / primary token，
  // 暗色模式下走 @theme 的 mapping，不用手 toggle。
  const warn = ratio >= 0.9 ? "error" : ratio >= 0.75 ? "warning" : "ok";
  const tone =
    warn === "error"
      ? "text-status-error"
      : warn === "warning"
        ? "text-status-warning"
        : "text-primary-text";
  const fill =
    warn === "error"
      ? "bg-status-error"
      : warn === "warning"
        ? "bg-status-warning"
        : "bg-primary";
  return (
    <div
      className="flex items-center gap-2 font-mono text-[10px] text-muted-foreground"
      aria-label={`上下文用量 ${safeUsed} / ${max}`}
    >
      <Gauge className="size-2.5" aria-hidden="true" />
      <span className="font-sans">上下文</span>
      <span className="inline-flex items-center gap-0.5 tabular-nums">
        <span className="font-medium text-foreground">
          {formatTokens(safeUsed)}
        </span>
        <span className="text-subtle-foreground"> / </span>
        <span>{formatTokens(max)}</span>
      </span>
      <span
        className="h-1 w-24 overflow-hidden rounded-sm bg-border"
        role="progressbar"
        aria-valuemin={0}
        aria-valuemax={max}
        aria-valuenow={Math.min(safeUsed, max)}
      >
        <span
          className={cn("block h-1 rounded-sm transition-[width]", fill)}
          style={{ width: `${pct}%` }}
        />
      </span>
      <span className={cn("font-medium tabular-nums", tone)}>{pct}%</span>
    </div>
  );
}

const SEND_SHORTCUT_HINT = "↵ 发送 · ⇧↵ 换行";
const EDIT_SHORTCUT_HINT = "↵ 保存 · Esc 取消";

function ChatComposer({
  className,
  onSubmit,
  placeholder = "输入消息或 / 触发命令",
  userMessageHistory,
  editing = false,
  editDraft,
  onCancelEdit,
  topSlot,
  contextUsage,
  permissionModeSlot,
  onShiftTab,
  autoFocusOnMount = false,
  backendType,
  onSlashRpc,
  ...props
}: ChatComposerProps) {
  const inputRef = React.useRef<AIChatInputHandle>(null);
  const [isEmpty, setIsEmpty] = React.useState(true);

  // 切换到编辑模式（或换了编辑目标）时把目标文本载进 TipTap，并把光标抓回输入框；
  // 退出编辑模式时清空输入，免得上一次的编辑残留干扰下一条新消息。
  const wasEditingRef = React.useRef(false);
  React.useEffect(() => {
    if (editing) {
      if (editDraft !== undefined) {
        inputRef.current?.loadDraft(editDraft);
      }
      inputRef.current?.focus();
    } else if (wasEditingRef.current) {
      inputRef.current?.clear();
    }
    wasEditingRef.current = editing;
  }, [editing, editDraft]);

  const wasAutoFocusOnMountRef = React.useRef(autoFocusOnMount);
  React.useEffect(() => {
    const wasAutoFocusOnMount = wasAutoFocusOnMountRef.current;
    wasAutoFocusOnMountRef.current = autoFocusOnMount;
    if (!autoFocusOnMount || wasAutoFocusOnMount) return;
    inputRef.current?.focus();
  }, [autoFocusOnMount]);

  function handleSend(text: string) {
    const trimmed = text.trim();
    if (!trimmed) return;
    onSubmit?.(trimmed);
  }

  function handleFormSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    inputRef.current?.submit();
  }

  // Esc 取消编辑。TipTap 的 handleKeyDown 不处理 Esc，所以这里在 form 层捕获；
  // 非编辑态下不消费，让默认行为走。
  //
  // Shift+Tab 循环切换 permission mode —— 对齐 Claude TUI；focus 在 composer 内
  // （TipTap editor / 按钮）时都会冒泡到 form，preventDefault 拦掉默认 tab 切换。
  // 编辑模式下不消费 Shift+Tab，让无障碍 tab 反向焦点正常工作。
  function handleFormKeyDown(event: React.KeyboardEvent<HTMLFormElement>) {
    if (editing && event.key === "Escape") {
      event.preventDefault();
      onCancelEdit?.();
      return;
    }
    if (
      !editing &&
      onShiftTab &&
      event.key === "Tab" &&
      event.shiftKey &&
      !event.metaKey &&
      !event.ctrlKey &&
      !event.altKey
    ) {
      event.preventDefault();
      onShiftTab();
    }
  }

  return (
    <form
      className={cn(
        "w-full border-t border-border bg-background px-5 py-3.5",
        className,
      )}
      onSubmit={handleFormSubmit}
      onKeyDown={handleFormKeyDown}
      {...props}
    >
      <div
        className={cn(
          "flex w-full flex-col overflow-hidden rounded-md border bg-card shadow-xs transition-colors",
          "focus-within:ring-[3px] focus-within:ring-ring/50",
          editing
            ? "border-primary-text/45 focus-within:border-primary-text/70"
            : "border-border focus-within:border-ring",
        )}
      >
        {topSlot}
        {editing ? (
          <div
            role="status"
            aria-label="正在编辑消息"
            className="flex items-center gap-2 border-b border-primary-text/20 bg-primary-soft px-3 py-1.5 text-[11px]"
          >
            <Pencil
              className="size-3 shrink-0 text-primary-text"
              aria-hidden="true"
            />
            <span className="font-semibold text-primary-text">编辑消息</span>
            <span className="text-muted-foreground">·</span>
            <span className="min-w-0 flex-1 truncate text-muted-foreground">
              保存后此条之后的历史会被丢弃并重新生成
            </span>
            <button
              type="button"
              aria-label="取消编辑"
              title="取消编辑 (Esc)"
              className="inline-flex size-5 shrink-0 cursor-pointer items-center justify-center rounded-sm text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
              onClick={() => onCancelEdit?.()}
            >
              <X className="size-3" aria-hidden="true" />
            </button>
          </div>
        ) : null}
        <div className="flex flex-col gap-1 px-3.5 pt-2.5 pb-1">
          <AIChatInput
            ref={inputRef}
            onSubmit={handleSend}
            onEmptyChange={setIsEmpty}
            sendOnEnter
            userMessageHistory={userMessageHistory}
            placeholder={placeholder}
            autoFocus={autoFocusOnMount}
            backendType={backendType}
            onSlashSelect={(cmd, exec) => {
              // literal_text 由 AIChatInput 内部直接填回编辑器(不自动发送),
              // 这里只接 rpc 类命令转交给父组件。
              if (exec.kind === "rpc") onSlashRpc?.(cmd, exec);
            }}
          />
          <div className="flex items-center gap-2">
            <span className="font-mono text-[10px] leading-none text-subtle-foreground">
              {editing ? EDIT_SHORTCUT_HINT : SEND_SHORTCUT_HINT}
            </span>
            {!editing && permissionModeSlot ? (
              <div className="flex items-center">{permissionModeSlot}</div>
            ) : null}
            <div className="min-w-0 flex-1" />
            {contextUsage && contextUsage.max > 0 && !editing ? (
              <ContextMeter used={contextUsage.used} max={contextUsage.max} />
            ) : null}
            {editing ? (
              <Button
                type="submit"
                disabled={isEmpty}
                size="xs"
                aria-label="保存编辑"
              >
                <Check data-icon="inline-start" aria-hidden="true" />
                保存
              </Button>
            ) : (
              <Button
                type="submit"
                disabled={isEmpty}
                size="icon-sm"
                aria-label="发送"
                title="发送 (Enter)"
              >
                <SendHorizontal data-icon="only" aria-hidden="true" />
              </Button>
            )}
          </div>
        </div>
      </div>
    </form>
  );
}

// ─── Helpers ────────────────────────────────────────────────────────────────

function formatHHmm(ms: number): string {
  if (!ms) return "";
  const d = new Date(ms);
  return `${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;
}

function formatHHmmss(ms: number): string {
  if (!ms) return "";
  const d = new Date(ms);
  return `${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}:${String(d.getSeconds()).padStart(2, "0")}`;
}

// ─── ErrorCard ───────────────────────────────────────────────────────────────

function ErrorCard({ text, onRerun }: { text: string; onRerun?: () => void }) {
  return (
    <section
      data-selectable-text="true"
      className="flex w-full max-w-[720px] items-center gap-3 rounded-md border border-status-error/40 bg-destructive-soft px-4 py-2.5"
    >
      <TriangleAlert
        className="size-4 shrink-0 text-status-error"
        aria-hidden="true"
      />
      <span className="min-w-0 flex-1 text-xs text-status-error">
        Agent 调用失败：{text}
      </span>
      {onRerun ? (
        <Button type="button" size="xs" variant="outline" onClick={onRerun}>
          ↻ 重新生成
        </Button>
      ) : null}
    </section>
  );
}

function RetryNoticeCard({ retry }: { retry: RetryNotice }) {
  const hasCount = retry.attempt > 0 && retry.maxAttempts > 0;
  const title = hasCount
    ? `正在重试 ${retry.attempt}/${retry.maxAttempts}`
    : retry.attempt > 0
      ? `正在重试 ${retry.attempt}`
      : "正在重试";
  const message = retry.message || "上游连接暂时中断";
  const at = formatHHmmss(retry.at);

  return (
    <section
      data-selectable-text="true"
      role="status"
      aria-label="正在重试"
      className="flex w-full max-w-[720px] items-start gap-3 rounded-md border border-status-warning/45 bg-status-warning/10 px-4 py-2.5"
    >
      <RefreshCw
        className="mt-0.5 size-4 shrink-0 animate-spin text-status-warning motion-reduce:animate-none"
        aria-hidden="true"
      />
      <div className="min-w-0 flex-1">
        <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
          <span className="text-xs font-semibold text-status-warning">
            {title}
          </span>
          {at ? (
            <span className="font-mono text-[10px] text-muted-foreground">
              {at}
            </span>
          ) : null}
        </div>
        <div className="mt-1 min-w-0 break-words font-mono text-[11px] text-foreground">
          {message}
        </div>
        {retry.details ? (
          <div className="mt-1 min-w-0 break-words text-[11px] leading-snug text-muted-foreground">
            {retry.details}
          </div>
        ) : null}
      </div>
    </section>
  );
}

// Generic tool card extension point: canonical-tool/raw/card.tsx handles
// non-canonical tools; canonical-tool/<kind>/card.tsx handles canonical kinds.

// ─── ChatTranscript ──────────────────────────────────────────────────────────

type ChatTranscriptProps = {
  agentName: string;
  agentColor: AgentColor;
  /** 会话的工作目录，用于工具卡片把 cwd 内路径展示为相对路径。 */
  cwd?: string;
  /** 当前 chat session id —— AskUserQuestionCard 提交答案时要带它去 Wails 绑定。 */
  sessionId?: number;
  messages: chat_svc.ChatMessage[];
  liveDelta?: string;
  /** 流式中累积的 thinking 增量。挂在 liveTargetId 对应的 assistant 上作为一张 streaming thinking 卡。 */
  liveThinking?: string;
  liveTargetId?: number | null;
  /**
   * 本轮 turn 已冻结但还没持久化的 blocks（text/tool_use/tool_result），跨路由
   * 由 chat-streams-store 维护。摆在 liveTargetId 对应的 assistant 的 persisted
   * blocks 之后、liveDelta 之前，整体顺序与真实流入顺序一致。
   */
  liveBlocks?: ChatBlockData[];
  liveRetry?: RetryNotice | null;
  /** 当前 stream 的开始时间，供合成 thinking 块计时使用。 */
  liveStreamStartedAt?: number | null;
  /** 用户点某条 assistant 上的「重新生成」时回调，参数是目标 assistant 的消息 id。 */
  onRerun?: (messageId: number) => void;
  /** 用户点某条 user 消息上的「编辑」时回调，参数是 user 消息 id。 */
  onEdit?: (messageId: number) => void;
  /** stream 是否进行中。true 时在末尾 assistant 内挂 typing 指示器，覆盖首 chunk 前 / 工具返回后的空窗期。 */
  streaming?: boolean;
  /** claudecode CLI 正在跑 /compact 时为 true;末尾 assistant 的 typing indicator 替换为
   *  "正在压缩上下文…" chip,让用户知道这段时间在做什么。compact_boundary 到达自动清空。*/
  liveCompacting?: boolean;
  onPlanActionStarted?: (stream: PlanActionStream, userText: string) => void;
};

// findLastCompactBoundary 顺序扫所有 messages.blocks 找最后一条 type=compact_boundary
// 的位置;没找到返回 null。返回 (messageIdx, blockIdx) 让 ChatTranscript 知道从哪里
// 起算"压缩后"显示段:messages[messageIdx].blocks[blockIdx] 即 boundary 块本身。
function findLastCompactBoundary(
  messages: chat_svc.ChatMessage[],
): { messageIdx: number; blockIdx: number } | null {
  let found: { messageIdx: number; blockIdx: number } | null = null;
  messages.forEach((m, i) => {
    (m.blocks ?? []).forEach((b, j) => {
      if (b.type === "compact_boundary") {
        found = { messageIdx: i, blockIdx: j };
      }
    });
  });
  return found;
}

function ChatTranscript({
  agentName,
  agentColor,
  cwd,
  sessionId,
  messages,
  liveDelta,
  liveThinking,
  liveTargetId,
  liveBlocks,
  liveRetry,
  liveStreamStartedAt,
  onRerun,
  onEdit,
  onPlanActionStarted,
  streaming = false,
  liveCompacting = false,
}: ChatTranscriptProps) {
  // 只有当整个序列的「最后一条」就是 assistant 时才挂指示器：
  // 正常 stream 流程下 doSend 会同时插入 user + assistant 占位，所以末尾一定是 assistant；
  // 如果末尾是 user（异常态或还没插占位），不应该在更早的 assistant 上挂指示器误导用户。
  const tail = messages.at(-1);
  const lastAssistantId =
    tail && tail.role === "assistant" ? tail.id : undefined;

  // 折叠"压缩前"的旧消息:扫所有 messages.blocks,找最后一条 compact_boundary 所在的
  // (messageIdx, blockIdx);该位置之前的所有消息默认隐藏,该消息自己的更早 blocks 也
  // 一并裁掉。expanded=true 时退化为原始 messages 渲染。
  const [expanded, setExpanded] = React.useState(false);
  const fold = React.useMemo(
    () => findLastCompactBoundary(messages),
    [messages],
  );
  const folding = !expanded && fold !== null;
  const foldedCount = folding ? fold.messageIdx : 0;
  const displayMessages = React.useMemo<chat_svc.ChatMessage[]>(() => {
    if (!folding) return messages;
    // 保留 boundary 所在消息及之后;boundary 消息的更早 blocks 裁掉。spread 出来的对象
    // 失去了 wails class 的 convertValues 方法,但下游 MessageItem 只读字段,as 强转
    // 即可,不需要走 Object.assign(Object.create(proto)) 这种重活。
    const out: chat_svc.ChatMessage[] = [];
    for (let i = fold.messageIdx; i < messages.length; i++) {
      if (i === fold.messageIdx && fold.blockIdx > 0) {
        const m = messages[i];
        out.push({
          ...m,
          blocks: (m.blocks ?? []).slice(fold.blockIdx),
        } as chat_svc.ChatMessage);
      } else {
        out.push(messages[i]);
      }
    }
    return out;
  }, [folding, messages, fold]);

  // useEvent 模式：把 onRerun/onEdit 包成稳定引用,让 MessageItem 的 React.memo
  // 不会被 ChatPanel 传入的 inline lambda 击穿。父侧每次重渲都换新函数,但 ref
  // 内部更新后稳定代理捕获最新值,语义不变。
  const onRerunRef = React.useRef(onRerun);
  const onEditRef = React.useRef(onEdit);
  React.useEffect(() => {
    onRerunRef.current = onRerun;
    onEditRef.current = onEdit;
  });
  const stableOnRerun = React.useCallback((id: number) => {
    onRerunRef.current?.(id);
  }, []);
  const stableOnEdit = React.useCallback((id: number) => {
    onEditRef.current?.(id);
  }, []);

  return (
    <TooltipProvider delayDuration={200}>
      {/* 不再加 max-w-4xl —— 内部 ChatMessage 已经 cap 在 720px,
          这里再叠一层外层 max-w 没有任何收紧效果,只会留出 phantom 空白。 */}
      <div className="flex flex-col gap-5">
        {folding && foldedCount > 0 ? (
          <CompactHistoryFold
            count={foldedCount}
            onExpand={() => setExpanded(true)}
          />
        ) : null}
        {displayMessages.map((m) => {
          const isLive = m.id === liveTargetId;
          return (
            <MessageItem
              key={m.id}
              m={m}
              agentName={agentName}
              agentColor={agentColor}
              cwd={cwd}
              sessionId={sessionId}
              // 关键: 非 live 消息的所有 live* prop 都收敛到稳定的"空值",
              // 让 React.memo 的 shallow 比较恒命中。
              liveTail={isLive ? (liveDelta ?? "") : ""}
              liveThinking={isLive ? (liveThinking ?? "") : ""}
              liveBlocks={isLive ? liveBlocks : undefined}
              liveRetry={isLive ? (liveRetry ?? null) : null}
              liveStreamStartedAt={
                isLive ? (liveStreamStartedAt ?? null) : null
              }
              showIndicator={
                streaming && m.role === "assistant" && m.id === lastAssistantId
              }
              compacting={
                isLive &&
                streaming &&
                m.role === "assistant" &&
                m.id === lastAssistantId &&
                liveCompacting
              }
              onRerun={stableOnRerun}
              onEdit={stableOnEdit}
              onPlanActionStarted={onPlanActionStarted}
            />
          );
        })}
      </div>
    </TooltipProvider>
  );
}

type MessageItemProps = {
  m: chat_svc.ChatMessage;
  agentName: string;
  agentColor: AgentColor;
  cwd?: string;
  sessionId?: number;
  liveTail: string;
  liveThinking: string;
  liveBlocks: ChatBlockData[] | undefined;
  liveRetry: RetryNotice | null;
  liveStreamStartedAt: number | null;
  showIndicator: boolean;
  /** showIndicator && compacting → 渲染 CompactingIndicator 替代 TypingIndicator。*/
  compacting: boolean;
  onRerun: (id: number) => void;
  onEdit: (id: number) => void;
  onPlanActionStarted?: (stream: PlanActionStream, userText: string) => void;
};

// MessageItem 是 transcript 里单条消息的渲染单元。memo 后,只要传入 props
// (m 引用 / live* 值) 没变,流式 chunk 不会再让历史消息进 render。配合上面
// 稳定的 onRerun/onEdit + 上游 immutable 消息更新,历史段在每次 chunk 期间
// 都能命中 memo,跳过 react-markdown / rehype-highlight 的重渲染。
const MessageItem = React.memo(function MessageItem({
  m,
  agentName,
  agentColor,
  cwd,
  sessionId,
  liveTail,
  liveThinking,
  liveBlocks,
  liveRetry,
  liveStreamStartedAt,
  showIndicator,
  compacting,
  onRerun,
  onEdit,
  onPlanActionStarted,
}: MessageItemProps) {
  const isAssistant = m.role === "assistant";
  // 每条 assistant 都允许重新生成；后端按消息 id 截断后重跑。
  const rerunHandler = isAssistant ? () => onRerun(m.id) : undefined;
  const editHandler = !isAssistant ? () => onEdit(m.id) : undefined;

  return (
    <ChatMessage
      data-message-id={m.id}
      author={isAssistant ? agentName : ""}
      avatarColor={isAssistant ? agentColor : "neutral"}
      initials={isAssistant ? agentName.charAt(0) : undefined}
      variant={isAssistant ? "assistant" : "user"}
      time={formatHHmm(m.createtime)}
      meta={
        // claude/codex 后端走 CLI 自身 login 时 m.model 落库是空串 —
        // 用 durationMs > 0 作为「turn 已结束」的可靠信号，让这些会话也
        // 能看到耗时/重新生成。streaming 占位（durationMs=0）仍然不出 meta。
        isAssistant && m.durationMs > 0 ? (
          <MessageMeta
            model={m.model}
            promptTokens={m.promptTokens}
            completionTokens={m.completionTokens}
            cachedTokens={m.cachedTokens}
            cacheCreationTokens={m.cacheCreationTokens}
            reasoningTokens={m.reasoningTokens}
            durationMs={m.durationMs}
            onRerun={rerunHandler}
          />
        ) : !isAssistant && editHandler ? (
          <UserMessageActions onEdit={editHandler} />
        ) : undefined
      }
    >
      {renderMessageBlocks(
        m.blocks,
        liveTail,
        cwd,
        liveThinking,
        liveStreamStartedAt,
        liveBlocks,
        sessionId,
        onPlanActionStarted,
      )}
      {liveRetry ? <RetryNoticeCard retry={liveRetry} /> : null}
      {showIndicator ? (
        compacting ? (
          <CompactingIndicator />
        ) : (
          <TypingIndicator />
        )
      ) : null}
      {isAssistant && m.errorText ? (
        <ErrorCard text={m.errorText} onRerun={rerunHandler} />
      ) : null}
    </ChatMessage>
  );
});

function TypingIndicator() {
  // keyframe 自己控制 opacity (0.2 ↔ 1)，dot 颜色不再叠 /60，避免叠加后整体太淡看不见。
  // 6px 三点 + 1.5 gap 是「克制但可感知」的尺寸；动画通过 @theme 的 --animate-typing-dot 注册，
  // class 名 animate-typing-dot 由 Tailwind v4 解析为 animation: typing-dot 1.2s ease-in-out infinite。
  const dotClass =
    "size-1.5 rounded-full bg-muted-foreground animate-typing-dot motion-reduce:animate-none";
  return (
    <div
      aria-label="正在生成"
      role="status"
      aria-live="polite"
      className="flex items-center gap-1.5 py-1"
    >
      <span className={dotClass} />
      <span className={cn(dotClass, "[animation-delay:0.15s]")} />
      <span className={cn(dotClass, "[animation-delay:0.3s]")} />
    </div>
  );
}

// CompactingIndicator 在 claudecode CLI 跑 /compact 期间替代 TypingIndicator,
// 让用户知道这段时间不是普通回答,而是在压缩上下文 (manual 或 auto)。文案旁
// 复用 TypingIndicator 的 dot 动画做"还在跑"的视觉信号。
function CompactingIndicator() {
  const dotClass =
    "size-1.5 rounded-full bg-muted-foreground animate-typing-dot motion-reduce:animate-none";
  return (
    <div
      aria-label="正在压缩上下文"
      role="status"
      aria-live="polite"
      className="flex items-center gap-2 py-1 text-xs text-muted-foreground"
    >
      <div className="flex items-center gap-1">
        <span className={dotClass} />
        <span className={cn(dotClass, "[animation-delay:0.15s]")} />
        <span className={cn(dotClass, "[animation-delay:0.3s]")} />
      </div>
      <span>正在压缩上下文…</span>
    </div>
  );
}

// renderMessageBlocks 把后端 ChatBlock 数组转成 JSX。
// 多个 text block 合并为一个 <p> + 末尾追加流式增量（liveTail），
// 其它类型逐个独立渲染。
// liveThinking 非空时在末尾追加一张「streaming thinking」卡片；当 liveTail 已有内容（文字已开始流式）
// 时该卡片显示为已完成态（思考结束、文字接力中），符合 spec 中「文字 chunk 一来思考即折叠」规则。
function renderMessageBlocks(
  blocks: ChatBlockData[] = [],
  liveTail: string,
  cwd?: string,
  liveThinking: string = "",
  liveThinkingStartedAt?: number | null,
  // 本轮 turn 已"冻结但还没持久化"的块（text / tool_use / tool_result），由
  // chat-streams-store 维护。和 persisted blocks 拼成一个完整顺序 —— 关键:
  // 流式途中遇到 tool_use 时,store 会把当下的 liveDelta 先冻成 text block 推
  // 到 liveBlocks 尾,所以真实顺序就是 [persisted..., ...liveBlocks, liveDelta]。
  liveBlocks: ChatBlockData[] = [],
  // AskUserQuestionCard 提交答案时要带它去 Wails 绑定。
  sessionId: number = 0,
  onPlanActionStarted?: (stream: PlanActionStream, userText: string) => void,
): React.ReactNode {
  type RenderItem =
    | { text: string; type: "text" }
    | { block: ChatBlockData; type: "plan" }
    | {
        block: ChatBlockData;
        startedAt?: number;
        streaming: boolean;
        type: "thinking";
      }
    | {
        // permissionBlock 仅在审批通过后由配对逻辑挂上,渲染时透传给 ToolInvocationCard。
        permissionBlock?: ChatBlockData;
        resultBlock?: ChatBlockData;
        toolBlock?: ChatBlockData;
        // childBlocks 仅 canonical.agent.spawn 需要(parent-child 归集),其它工具留空。
        childBlocks?: ChatBlockData[];
        type: "tool";
      }
    | {
        block: ChatBlockData;
        // _consumed 标记此条审批已被 merge 到某条 tool_use 卡上,渲染前会被过滤掉。
        // 未 resolved / resolved-denied 的审批不会被标记,保留为独立卡。
        _consumed?: boolean;
        type: "tool_permission_request";
      }
    | { block: ChatBlockData; type: "unknown" }
    | { block: ChatBlockData; type: "compact_boundary" };

  // 预扫一遍把 subagent 内部 block 归集到外层 Agent.tool_use_id；
  // 主流程遇到 parentToolUseId 非空就 skip，避免被同级渲染。
  const childrenByParent = new Map<string, ChatBlockData[]>();
  const collectChildren = (b: ChatBlockData) => {
    if (!b.parentToolUseId) return;
    const arr = childrenByParent.get(b.parentToolUseId) ?? [];
    arr.push(b);
    childrenByParent.set(b.parentToolUseId, arr);
  };
  blocks.forEach(collectChildren);
  liveBlocks.forEach(collectChildren);

  const items: RenderItem[] = [];
  const pendingToolIndexes = new Map<string, number>();
  const pendingAnonymousToolIndexes: number[] = [];
  // SKIPPED_TOOL_INDEX 给 AskUserQuestion 的 tool_use 占位用:tool_use 本身不入 items,
  // 但要让后续的 tool_result 在 pendingToolIndexes 里查到这个哨兵,从而一同 skip。
  const SKIPPED_TOOL_INDEX = -1;
  // pendingPermsByTool 按 toolName 维护"已审批通过、还在等匹配 tool_use"的 perm RenderItem
  // 下标 (FIFO)。匹配到 tool_use 时把 perm 标记 _consumed,merge 到那条 tool item。
  // 这是协议上唯一可行的关联方式 —— ChatBlockToolPermission 没有 toolUseId 字段,
  // can_use_tool control_request 也不携带未来的 tool_use_id。
  const pendingPermsByTool = new Map<string, number[]>();

  function appendText(text: string) {
    if (!text) return;
    const last = items.at(-1);
    if (last?.type === "text") {
      last.text += text;
      return;
    }
    items.push({ text, type: "text" });
  }

  const consumeBlock = (b: ChatBlockData) => {
    // subagent 内部 block 已经被归集到父 AgentSpawnCard 的 childBlocks，不再同级渲染。
    if (b.parentToolUseId) return;
    switch (b.type) {
      case "text":
        appendText(b.text ?? "");
        break;
      case "thinking":
        items.push({ block: b, streaming: false, type: "thinking" });
        break;
      case "plan":
        // Most plan.update blocks are progress data for TaskProgressBar only.
        // Actionable plan blocks carry canonical.actions and need the shared
        // PlanCard in the transcript.
        if (isRenderablePlanBlock(b)) {
          items.push({ block: b, type: "plan" });
        }
        break;
      case "tool_use": {
        // AskUserQuestion 类工具的 tool_use 不渲染独立卡 —— ask_user_question block
        // 已经把交互界面接管掉。占位 SKIPPED_TOOL_INDEX 让后续 tool_result 也 skip。
        if (isAskUserQuestionToolName(b.toolName)) {
          if (b.toolUseId)
            pendingToolIndexes.set(b.toolUseId, SKIPPED_TOOL_INDEX);
          break;
        }
        // ExitPlanMode 同理 —— PlanApproveCard(plan.approve_request canonical)已经
        // 承担"批准执行计划"的完整渲染,后续 CLI 真正调用 ExitPlanMode 冒出的 tool_use
        // 是协议余响,再渲染一张卡只会和 PlanApproveCard 视觉重复。break 前不入
        // pendingPermsByTool 队列也意味着 PlanApproveCard 不会被 merge 隐藏。
        if (b.toolName === "ExitPlanMode") {
          if (b.toolUseId)
            pendingToolIndexes.set(b.toolUseId, SKIPPED_TOOL_INDEX);
          break;
        }
        if (isSubagentCanonical(b)) {
          // canonical.agent.spawn — 走 CanonicalToolRouter → AgentSpawnCard,childBlocks
          // 由 chat.tsx 的 parent-child 归集传过去(AgentSpawnCard 内部渲染 STEPS 段)。
          const item: RenderItem = {
            childBlocks: b.toolUseId
              ? (childrenByParent.get(b.toolUseId) ?? [])
              : [],
            toolBlock: b,
            type: "tool",
          };
          items.push(item);
          if (b.toolUseId) {
            pendingToolIndexes.set(b.toolUseId, items.length - 1);
          }
          break;
        }
        const item: RenderItem = { toolBlock: b, type: "tool" };
        // 配对消费一条审批 RenderItem —— 找最早未消费且同 toolName 的 allowed 审批。
        if (b.toolName) {
          const queue = pendingPermsByTool.get(b.toolName);
          if (queue && queue.length > 0) {
            const permIdx = queue.shift()!;
            const permItem = items[permIdx];
            if (permItem?.type === "tool_permission_request") {
              permItem._consumed = true;
              item.permissionBlock = permItem.block;
            }
          }
        }
        items.push(item);
        if (b.toolUseId) {
          pendingToolIndexes.set(b.toolUseId, items.length - 1);
        } else {
          pendingAnonymousToolIndexes.push(items.length - 1);
        }
        break;
      }
      case "tool_result": {
        const toolIndex = b.toolUseId
          ? pendingToolIndexes.get(b.toolUseId)
          : pendingAnonymousToolIndexes.pop();
        // AskUserQuestion 的 tool_result 命中 SKIPPED_TOOL_INDEX 哨兵,直接丢弃。
        if (toolIndex === SKIPPED_TOOL_INDEX) {
          if (b.toolUseId) pendingToolIndexes.delete(b.toolUseId);
          break;
        }
        const item =
          typeof toolIndex === "number" ? items[toolIndex] : undefined;

        if (item?.type === "tool") {
          item.resultBlock = b;
          if (b.toolUseId) pendingToolIndexes.delete(b.toolUseId);
        }
        // 孤儿 tool_result：没有配对 tool_use（AskUserQuestion 历史数据 / 后端漏过滤
        // 的 PostToolUse 等都会走到这里），直接丢，不要 push 一条没有 toolBlock 的
        // 幽灵 tool 卡（toolName 会回退到默认 "tool" 把答案文本暴露出来）。
        break;
      }
      case "ask_user_question":
        // ask_user_question 走 CanonicalToolRouter — block.canonical (UserAsk)
        // 已由后端 live + replay 双路径填好,UserAskCard 直接消费。
        items.push({ toolBlock: b, type: "tool" });
        break;
      case "tool_permission_request": {
        // tool_permission_request 渲染走 CanonicalToolRouter —— ExitPlanMode
        // → canonical.plan.approve_request → PlanApproveCard;其它工具
        // → canonical.tool.permission → ToolPermissionCard。两条 canonical 都由后端
        // dispatcher_emitter + replay 填好。RenderItem.type 保留 "tool_permission_request"
        // 让 merge 到下方同 toolName tool_use 卡的逻辑可识别。
        items.push({ block: b, type: "tool_permission_request" });
        const idx = items.length - 1;
        const perm = b.toolPermission;
        // 只有 resolved + allowed 才参与 merge:未决态用户还要操作、denied 没有下游 tool_use。
        if (perm?.resolved && perm.allowed && perm.toolName) {
          const queue = pendingPermsByTool.get(perm.toolName) ?? [];
          queue.push(idx);
          pendingPermsByTool.set(perm.toolName, queue);
        }
        break;
      }
      case "compact_boundary":
        // CLI 通报上下文已压缩 (manual /compact 或 auto)。在 transcript 中嵌一条
        // 分隔卡片;最后一条 compact_boundary 之前的所有内容会被 ChatTranscript 顶层
        // 折叠成"查看历史"按钮。
        items.push({ block: b, type: "compact_boundary" });
        break;
      default:
        items.push({ block: b, type: "unknown" });
        break;
    }
  };
  blocks.forEach(consumeBlock);

  // 合成 thinking 必须排在本轮 liveBlocks（tool_use/tool_result/已冻结 text）之前 —
  // Anthropic 协议里 thinking 永远在 turn 开头，store 也是单一 liveThinking 字段不穿插。
  // 摆错位置会出现「思考 14s 还在转，但工具卡已经在它上方」的视觉错乱。
  // streaming 判定：本轮一旦冒出任何非思考的输出（tool_use 进 liveBlocks 或文本开始流到
  // liveTail），思考阶段就结束；只看 liveTail 会漏掉「思考完→直接发 tool」那一帧，徽标
  // 一直 pulse、计时定格。
  if (liveThinking) {
    items.push({
      block: { text: liveThinking, type: "thinking" } as ChatBlockData,
      startedAt: liveThinkingStartedAt ?? undefined,
      streaming: !liveTail && liveBlocks.length === 0,
      type: "thinking",
    });
  }
  liveBlocks.forEach(consumeBlock);
  appendText(liveTail);

  // 被 merge 到下方 tool_use 卡的审批 RenderItem 不再独立渲染。
  const visibleItems = items.filter(
    (item) => !(item.type === "tool_permission_request" && item._consumed),
  );

  return visibleItems.map((item, idx) => {
    switch (item.type) {
      case "text":
        return <MarkdownText key={`text-${idx}`} text={item.text} />;
      case "plan":
        return (
          <PlanApproveCard
            key={`plan-${idx}`}
            cwd={cwd}
            sessionId={sessionId}
            toolBlock={item.block}
            onPlanActionStarted={onPlanActionStarted}
          />
        );
      case "thinking":
        return (
          <ThinkingBlock
            key={`thinking-${idx}`}
            startedAt={item.startedAt}
            streaming={item.streaming}
            text={item.block.text ?? ""}
          />
        );
      case "tool": {
        // 工具卡统一走 CanonicalToolRouter:
        //   - block.canonical 非空且 kind 已注册 → 分发到 canonical-tool/<kind>/card.tsx
        //     (file.write / file.edit / agent.spawn ... )
        //   - tool_use 形态的 plan.update 刻意不注册,这里仍显示普通工具卡。
        //     type="plan" 且带 actions 的 plan.update 已在上方复用 PlanCard。
        //   - 否则 fallback 到 RawToolCard(Bash/Read/MCP 等通用工具)。
        // item.permissionBlock 由 RawToolCard 自行从 toolBlock.toolPermission 读。
        return (
          <CanonicalToolRouter
            key={`tool-${idx}`}
            cwd={cwd}
            sessionId={sessionId}
            resultBlock={item.resultBlock}
            toolBlock={item.toolBlock ?? { type: "tool_use" }}
            childBlocks={item.childBlocks}
            onPlanActionStarted={onPlanActionStarted}
          />
        );
      }
      case "tool_permission_request":
        // 两种 canonical(plan.approve_request / tool.permission)由后端在
        // ExitPlanMode 与其它工具之间分流。这里统一走 CanonicalToolRouter。
        return (
          <CanonicalToolRouter
            key={`perm-${idx}`}
            cwd={cwd}
            sessionId={sessionId}
            toolBlock={item.block}
            onPlanActionStarted={onPlanActionStarted}
          />
        );
      case "compact_boundary": {
        const trig = item.block.compact?.trigger;
        const trigger: "auto" | "manual" | undefined =
          trig === "auto" || trig === "manual" ? trig : undefined;
        return (
          <CompactBoundaryDivider
            key={`compact-${idx}`}
            preTokens={item.block.compact?.preTokens}
            trigger={trigger}
            at={item.block.compact?.at ?? 0}
          />
        );
      }
      case "unknown":
        return (
          <div
            key={`unknown-${idx}`}
            className="rounded-md border border-dashed border-border px-3 py-2 font-mono text-xs text-muted-foreground"
          >
            (debug) 未实现 block 类型：{item.block.type}
          </div>
        );
      default:
        return null;
    }
  });
}

export {
  ApprovalGate,
  ChatComposer,
  ChatMessage,
  ChatTranscript,
  CodeBlock,
  ErrorCard,
  MessageMeta,
  ToolCall,
};
