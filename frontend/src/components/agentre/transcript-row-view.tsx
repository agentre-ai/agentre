// transcript-row-view: 行级虚拟化的渲染层 —— 一个 TranscriptRow 渲染成一个虚拟行。
// 消息 chrome 按分片标志挂载:首行走 ChatMessage(article + 头像 + 名字 + 时间戳,
// 纯文本消息恰好一行,DOM 与 message 级虚拟化完全一致);后续行用幽灵 gutter 对齐
// 头像列;末行追加 footer(meta/copy/edit)+ RetryNotice + TypingIndicator + ErrorCard。
// 静态依赖走 TranscriptRenderContext,行组件的 memo 比较只剩 row 引用 + 少量 live 标量。
import * as React from "react";
import {
  ArrowDown,
  ArrowUp,
  Pencil,
  RefreshCw,
  TriangleAlert,
} from "lucide-react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";

import { AutoTriggerBanner } from "./auto-trigger-banner";
import { PlanApproveCard } from "./canonical-tool/plan-approve-request/card";
import type { PlanActionStream } from "./canonical-tool/props";
import { CanonicalToolRouter } from "./canonical-tool/registry";
import { CompactBoundaryDivider } from "./compact-boundary-divider";
import { MarkdownText, StreamingMarkdown } from "./markdown-text";
import { MessageRow, MessageCopyButton } from "./message-row";
import { ToolApprovalCard } from "./tool-approval/card";
import { ThinkingBlock } from "./thinking-block";
import type { TranscriptRow, TranscriptRowItem } from "./transcript-rows";
import type { AgentColor } from "./types";
import type { ChatBlockData, RetryNotice } from "@/stores/chat-streams-store";

// ─── 会话级静态渲染依赖 ───────────────────────────────────────────────────────

export type TranscriptRenderContextValue = {
  agentName: string;
  agentColor: AgentColor;
  cwd?: string;
  sessionId: number;
  tabStateKey?: string;
  onPlanActionStarted?: (stream: PlanActionStream, userText: string) => void;
  onRerun: (messageId: number) => void;
  onEdit: (messageId: number) => void;
};

export const TranscriptRenderContext =
  React.createContext<TranscriptRenderContextValue | null>(null);

// ─── 消息 chrome 组件(自 chat.tsx 平移)──────────────────────────────────────

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
  const { t } = useTranslation();
  const isUser = variant === "user";
  return (
    <MessageRow
      className={className}
      avatar={
        isUser ? (
          <span
            aria-label={t("chat.message.me")}
            role="img"
            className="inline-flex size-7 shrink-0 items-center justify-center rounded-lg bg-muted text-[11px] font-semibold text-muted-foreground"
          >
            {t("chat.message.me")}
          </span>
        ) : undefined
      }
      avatarName={author}
      avatarInitials={initials}
      avatarColor={avatarColor}
      name={isUser ? null : author}
      headerExtra={
        <span className="font-mono text-[10px] text-muted-foreground">
          {time}
        </span>
      }
      footer={meta}
      {...props}
    >
      {children}
    </MessageRow>
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
  const { t } = useTranslation();
  const durationLabel = `${(durationMs / 1000).toFixed(1)}s`;

  // tooltip 里需要拆分展示，所以这里给一个稳定的 row 渲染器避免重复。
  const rows: { label: string; value: string }[] = [
    { label: t("chat.meta.model"), value: model || "—" },
    {
      label: t("chat.meta.prompt"),
      value: promptTokens.toLocaleString(),
    },
    {
      label: t("chat.meta.completion"),
      value: completionTokens.toLocaleString(),
    },
  ];
  if (cachedTokens > 0) {
    rows.push({
      label: t("chat.meta.cacheHit"),
      value: cachedTokens.toLocaleString(),
    });
  }
  if (cacheCreationTokens > 0) {
    rows.push({
      label: t("chat.meta.cacheWrite"),
      value: cacheCreationTokens.toLocaleString(),
    });
  }
  if (reasoningTokens > 0) {
    rows.push({
      label: t("chat.meta.reasoning"),
      value: reasoningTokens.toLocaleString(),
    });
  }
  rows.push({ label: t("chat.meta.duration"), value: durationLabel });

  return (
    <div className="flex flex-wrap items-center gap-1.5">
      <Tooltip>
        <TooltipTrigger asChild>
          <button
            type="button"
            className="inline-flex items-center gap-1.5 rounded px-1 py-0.5 hover:bg-muted/60"
            aria-label={t("chat.meta.tokenDetails")}
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
          {t("chat.actions.regenerate")}
        </Button>
      ) : null}
    </div>
  );
}

type AssistantMessageActionsProps = {
  cacheCreationTokens?: number;
  cachedTokens?: number;
  completionTokens: number;
  copyText: string;
  durationMs: number;
  model: string;
  onRerun?: () => void;
  promptTokens: number;
  reasoningTokens?: number;
};

function AssistantMessageActions({
  cacheCreationTokens,
  cachedTokens,
  completionTokens,
  copyText,
  durationMs,
  model,
  onRerun,
  promptTokens,
  reasoningTokens,
}: AssistantMessageActionsProps) {
  const { t } = useTranslation();

  return (
    <>
      {durationMs > 0 ? (
        <MessageMeta
          model={model}
          promptTokens={promptTokens}
          completionTokens={completionTokens}
          cachedTokens={cachedTokens}
          cacheCreationTokens={cacheCreationTokens}
          reasoningTokens={reasoningTokens}
          durationMs={durationMs}
          onRerun={onRerun}
        />
      ) : null}
      <MessageCopyButton
        text={copyText}
        label={t("common.copy")}
        ariaLabel={t("chat.actions.copyOutput")}
        successTitle={t("chat.actions.copyOutputDone")}
        errorTitle={t("chat.actions.copyOutputFailed")}
      />
    </>
  );
}

// UserMessageActions 渲染 user 气泡的 action 行：目前只有「编辑」。
// 作为 `meta` prop 传入 ChatMessage，常驻显示在消息下方。
function UserMessageActions({ onEdit }: { onEdit: () => void }) {
  const { t } = useTranslation();
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
        {t("common.edit")}
      </Button>
    </div>
  );
}

function ErrorCard({ text, onRerun }: { text: string; onRerun?: () => void }) {
  const { t } = useTranslation();
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
        {t("chat.errorCard.message", { text })}
      </span>
      {onRerun ? (
        <Button type="button" size="xs" variant="outline" onClick={onRerun}>
          {t("chat.errorCard.regenerate")}
        </Button>
      ) : null}
    </section>
  );
}

function RetryNoticeCard({ retry }: { retry: RetryNotice }) {
  const { t } = useTranslation();
  const hasCount = retry.attempt > 0 && retry.maxAttempts > 0;
  const title = hasCount
    ? t("chat.retry.titleWithMax", {
        attempt: retry.attempt,
        max: retry.maxAttempts,
      })
    : retry.attempt > 0
      ? t("chat.retry.titleWithAttempt", { attempt: retry.attempt })
      : t("chat.retry.title");
  const message = retry.message || t("chat.retry.defaultMessage");
  const at = formatHHmmss(retry.at);

  return (
    <section
      data-selectable-text="true"
      role="status"
      aria-label={t("chat.retry.aria")}
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

function TypingIndicator() {
  const { t } = useTranslation();
  // keyframe 自己控制 opacity (0.2 ↔ 1)，dot 颜色不再叠 /60，避免叠加后整体太淡看不见。
  // 6px 三点 + 1.5 gap 是「克制但可感知」的尺寸；动画通过 @theme 的 --animate-typing-dot 注册，
  // class 名 animate-typing-dot 由 Tailwind v4 解析为 animation: typing-dot 1.2s ease-in-out infinite。
  const dotClass =
    "size-1.5 rounded-full bg-muted-foreground animate-typing-dot motion-reduce:animate-none";
  return (
    <div
      aria-label={t("chat.typing.aria")}
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
  const { t } = useTranslation();
  const dotClass =
    "size-1.5 rounded-full bg-muted-foreground animate-typing-dot motion-reduce:animate-none";
  return (
    <div
      aria-label={t("chat.compacting.aria")}
      role="status"
      aria-live="polite"
      className="flex items-center gap-2 py-1 text-xs text-muted-foreground"
    >
      <div className="flex items-center gap-1">
        <span className={dotClass} />
        <span className={cn(dotClass, "[animation-delay:0.15s]")} />
        <span className={cn(dotClass, "[animation-delay:0.3s]")} />
      </div>
      <span>{t("chat.compacting.label")}</span>
    </div>
  );
}

function ImageBlockView({ block }: { block: ChatBlockData }) {
  const { t } = useTranslation();
  const image = (
    block as ChatBlockData & {
      image?: { dataUrl?: string; mediaType?: string; name?: string };
    }
  ).image;
  if (!image?.dataUrl) return null;
  return (
    <a
      href={image.dataUrl}
      target="_blank"
      rel="noreferrer"
      className="block w-fit overflow-hidden rounded-md border border-border bg-muted"
    >
      <img
        src={image.dataUrl}
        alt={image.name || image.mediaType || t("chat.image.alt")}
        className="max-h-72 max-w-full object-contain"
      />
    </a>
  );
}

function extractAssistantOutputText(
  blocks: ChatBlockData[] = [],
  liveBlocks: ChatBlockData[] = [],
  liveTail: string = "",
): string {
  const text = [...blocks, ...liveBlocks]
    .filter((block) => block.type === "text")
    .map((block) => block.text ?? "")
    .join("");
  return text + liveTail;
}

// ─── RenderItem → JSX ────────────────────────────────────────────────────────

function RenderItemView({ item }: { item: TranscriptRowItem }) {
  const { t } = useTranslation();
  const ctx = React.useContext(TranscriptRenderContext);
  switch (item.type) {
    case "placeholder":
      return null;
    case "text":
      return item.streaming ? (
        <StreamingMarkdown cwd={ctx?.cwd} text={item.text} />
      ) : (
        <MarkdownText cwd={ctx?.cwd} text={item.text} />
      );
    case "plan":
      return (
        <PlanApproveCard
          cwd={ctx?.cwd}
          sessionId={ctx?.sessionId ?? 0}
          toolBlock={item.block}
          onPlanActionStarted={ctx?.onPlanActionStarted}
          uiStateKey={item.uiStateKey}
          tabStateKey={ctx?.tabStateKey}
        />
      );
    case "thinking":
      return (
        <ThinkingBlock
          startedAt={item.startedAt}
          streaming={item.streaming}
          text={item.block.text ?? ""}
          uiStateKey={item.uiStateKey}
        />
      );
    case "image":
      return <ImageBlockView block={item.block} />;
    case "tool":
      // 工具卡统一走 CanonicalToolRouter:
      //   - block.canonical 非空且 kind 已注册 → 分发到 canonical-tool/<kind>/card.tsx
      //     (file.write / file.edit / agent.spawn ... )
      //   - tool_use 形态的 plan.update 刻意不注册,这里仍显示普通工具卡。
      //     type="plan" 且带 actions 的 plan.update 已在上方复用 PlanCard。
      //   - 否则 fallback 到 RawToolCard(Bash/Read/MCP 等通用工具)。
      // item.permissionBlock 由 RawToolCard 自行从 toolBlock.toolPermission 读。
      return (
        <CanonicalToolRouter
          cwd={ctx?.cwd}
          sessionId={ctx?.sessionId ?? 0}
          resultBlock={item.resultBlock}
          toolBlock={item.toolBlock ?? { type: "tool_use" }}
          childBlocks={item.childBlocks}
          onPlanActionStarted={ctx?.onPlanActionStarted}
          uiStateKey={item.uiStateKey}
          tabStateKey={ctx?.tabStateKey}
        />
      );
    case "tool_permission_request":
      // 两种 canonical(plan.approve_request / tool.permission)由后端在
      // ExitPlanMode 与其它工具之间分流。这里统一走 CanonicalToolRouter。
      return (
        <CanonicalToolRouter
          cwd={ctx?.cwd}
          sessionId={ctx?.sessionId ?? 0}
          toolBlock={item.block}
          onPlanActionStarted={ctx?.onPlanActionStarted}
          uiStateKey={item.uiStateKey}
          tabStateKey={ctx?.tabStateKey}
        />
      );
    case "tool_approval":
      // 内置写工具审批卡:不走 CanonicalToolRouter,按 block.type 直接路由。
      // approval 从 block.toolApproval 取,sessionId 从渲染上下文取(同 tool 卡)。
      return item.block.toolApproval ? (
        <ToolApprovalCard
          approval={item.block.toolApproval}
          sessionId={ctx?.sessionId ?? 0}
        />
      ) : null;
    case "compact_boundary": {
      const trig = item.block.compact?.trigger;
      const trigger: "auto" | "manual" | undefined =
        trig === "auto" || trig === "manual" ? trig : undefined;
      return (
        <CompactBoundaryDivider
          preTokens={item.block.compact?.preTokens}
          trigger={trigger}
          at={item.block.compact?.at ?? 0}
        />
      );
    }
    case "unknown":
      return (
        <div className="rounded-md border border-dashed border-border px-3 py-2 font-mono text-xs text-muted-foreground">
          {t("chat.debug.unknownBlock", { type: item.block.type })}
        </div>
      );
    default:
      return null;
  }
}

// ─── 行渲染 ──────────────────────────────────────────────────────────────────

export type TranscriptRowViewProps = {
  row: TranscriptRow;
  /** 仅 live 消息的末行收到非空值 —— footer copyText 需要拼上未持久化的输出。 */
  liveTail: string;
  liveBlocks: ChatBlockData[] | undefined;
  /** 仅 live 消息的末行非空。 */
  liveRetry: RetryNotice | null;
  /** 末行 && 该消息是 streaming 指示器宿主(最后一条 assistant)。 */
  showIndicator: boolean;
  /** showIndicator && compacting → 渲染 CompactingIndicator 替代 TypingIndicator。*/
  compacting: boolean;
};

// 行级 memo 是流式期间的重渲边界:persisted 消息的行对象来自 WeakMap 缓存(引用
// 稳定),live 标量 props 对非 live 行恒为空值 → 每个 chunk 只有 live 消息的行重渲。
export const TranscriptRowView = React.memo(function TranscriptRowView({
  row,
  liveTail,
  liveBlocks,
  liveRetry,
  showIndicator,
  compacting,
}: TranscriptRowViewProps) {
  const ctx = React.useContext(TranscriptRenderContext);
  const m = row.message;
  const isAssistant = m.role === "assistant";
  // 每条 assistant 都允许重新生成；后端按消息 id 截断后重跑。
  const rerunHandler = isAssistant ? () => ctx?.onRerun(m.id) : undefined;
  const copyText =
    isAssistant && row.isLastOfMessage
      ? extractAssistantOutputText(m.blocks ?? [], liveBlocks ?? [], liveTail)
      : "";

  const tailAttachments = row.isLastOfMessage ? (
    <>
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
    </>
  ) : null;

  const meta = !row.isLastOfMessage ? undefined : isAssistant &&
    (m.durationMs > 0 || copyText) ? (
    <AssistantMessageActions
      model={m.model}
      promptTokens={m.promptTokens}
      completionTokens={m.completionTokens}
      cachedTokens={m.cachedTokens}
      cacheCreationTokens={m.cacheCreationTokens}
      reasoningTokens={m.reasoningTokens}
      durationMs={m.durationMs}
      onRerun={rerunHandler}
      copyText={copyText}
    />
  ) : !isAssistant ? (
    <UserMessageActions onEdit={() => ctx?.onEdit(m.id)} />
  ) : undefined;

  if (row.isFirstOfMessage) {
    return (
      <>
        {row.autonomous ? <AutoTriggerBanner /> : null}
        <ChatMessage
          author={isAssistant ? (ctx?.agentName ?? "") : ""}
          avatarColor={isAssistant ? (ctx?.agentColor ?? "agent-1") : "neutral"}
          initials={isAssistant ? ctx?.agentName.charAt(0) : undefined}
          variant={isAssistant ? "assistant" : "user"}
          time={formatHHmm(m.createtime)}
          meta={meta}
        >
          <RenderItemView item={row.item} />
          {tailAttachments}
        </ChatMessage>
      </>
    );
  }

  // 消息的后续分片行:左侧 w-7 幽灵 gutter 对齐头像列,内容列与 MessageRow
  // 完全同构(max-w-[720px] / data-selectable-text / footer 槽样式)。
  return (
    <div className="flex gap-3 text-sm">
      <div aria-hidden className="w-7 shrink-0" />
      <div className="flex min-w-0 max-w-[720px] flex-1 flex-col gap-1">
        <div
          data-selectable-text="true"
          className="flex flex-col gap-2 leading-[1.55]"
        >
          <RenderItemView item={row.item} />
          {tailAttachments}
        </div>
        {meta ? (
          <div className="mt-1 flex flex-wrap items-center gap-1.5 font-mono text-[10px] text-subtle-foreground">
            {meta}
          </div>
        ) : null}
      </div>
    </div>
  );
});

export { ChatMessage, ErrorCard, MessageMeta };
