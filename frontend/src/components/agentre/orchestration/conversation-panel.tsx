import * as React from "react";
import { useTranslation } from "react-i18next";
import { ArrowLeft, Clock } from "lucide-react";
import { Button } from "@/components/ui/button";
import { useLiveConversation } from "@/hooks/use-live-conversation";
import { ChatComposer, ChatTranscript, type ChatComposerSubmit } from "../chat";
import { PermissionModePill } from "../permission-mode";
import type { AgentColor, AgentStatus } from "../types";
import { AgentAvatar, StatusDot } from "../primitives";
import { useOrchRunStore } from "../../../stores/orch-run-store";
import type { ChatBlockData, RetryNotice } from "@/stores/chat-streams-store";

/**
 * Derive agent status from the tasks belonging to this agent in a run.
 * Rule: if any task is "running" → running; if all done → "done" (sentinel);
 * if no tasks → "idle" (not started); else → "idle".
 * Note: AgentStatus has no "done" value — we use "idle" for both; the
 * statusLabelKey distinguishes "pending" (0 tasks) from "done" (all tasks done).
 */
export type AgentTaskStatus = "idle" | "pending" | "done" | "running" | "error";

export function deriveAgentTaskStatus(
  tasks: Array<{ agentId: number; status: string }>,
  agentId: number,
): AgentTaskStatus {
  const agentTasks = tasks.filter((t) => t.agentId === agentId);
  if (agentTasks.length === 0) return "pending";
  if (agentTasks.some((t) => t.status === "running")) return "running";
  if (agentTasks.every((t) => t.status === "done")) return "done";
  return "idle";
}

// Keep backward-compat shim for existing callers that expect AgentStatus
function deriveAgentStatus(
  tasks: Array<{ agentId: number; status: string }>,
  agentId: number,
): AgentStatus {
  const ts = deriveAgentTaskStatus(tasks, agentId);
  if (ts === "running") return "running";
  if (ts === "error") return "error";
  return "idle";
}

export function ConversationPanel({
  sessionId,
  agentName,
  agentColor,
  onBack,
  runId,
  agentId,
}: {
  sessionId: number;
  agentName: string;
  agentColor: AgentColor;
  onBack: () => void;
  /** Optional: run id used to look up activeAsks for the waiting callout */
  runId?: number;
  /** Optional: agent id used to look up activeAsks for the waiting callout */
  agentId?: number;
}) {
  const { t } = useTranslation();
  const {
    messages,
    live: liveRaw,
    submit,
    isModeSwitchable,
    supportsImageInput,
    permissionMode,
    permissionModeMeta,
    backendType,
    contextUsage,
  } = useLiveConversation(sessionId, agentId ?? 0);
  // useLiveConversation's EMPTY_LIVE sentinel types liveBlocks/liveRetry as
  // unknown[]/unknown (rather than ChatBlockData[]/RetryNotice | null), which
  // widens `live`'s inferred union type. Normalize once here at the boundary
  // instead of casting at every ChatTranscript prop below.
  const live = liveRaw as {
    liveDelta: string;
    liveThinking: string;
    liveBlocks: ChatBlockData[];
    liveRetry: RetryNotice | null;
    liveStreamStartedAt: number | null;
    streaming: boolean;
    liveCompacting: boolean;
  };
  const [scrollEl, setScrollEl] = React.useState<HTMLDivElement | null>(null);

  // Derive awaiting state: is this agent currently waiting for a peer reply?
  const isAwaiting = useOrchRunStore((s) => {
    if (!runId || !agentId) return false;
    return (s.activeAsks.get(runId) ?? []).some(
      (ask) => ask.askerAgentId === agentId,
    );
  });

  // Derive agent task count from run detail (primitive — no re-render loop)
  const agentTaskCount = useOrchRunStore((s) => {
    if (!runId || !agentId) return 0;
    const detail = s.details.get(runId);
    if (!detail?.tasks) return 0;
    return detail.tasks.filter((t) => t.agentId === agentId).length;
  });

  // Derive agent status from run detail (primitive — no re-render loop)
  const agentStatus = useOrchRunStore((s): AgentStatus => {
    if (!runId || !agentId) return "idle";
    const detail = s.details.get(runId);
    if (!detail?.tasks) return "idle";
    return deriveAgentStatus(detail.tasks, agentId);
  });

  // Richer task-status for the who-row subtitle label
  const agentTaskStatus = useOrchRunStore((s): AgentTaskStatus => {
    if (!runId || !agentId) return "pending";
    const detail = s.details.get(runId);
    if (!detail?.tasks) return "pending";
    return deriveAgentTaskStatus(detail.tasks, agentId);
  });

  // Status label: reuse board status keys
  // pending = no tasks yet (not started); done = all tasks done; running/error as-is;
  // idle = has tasks but not all done and none running (in progress / queued)
  const statusLabelKey =
    agentTaskStatus === "running"
      ? "orchestration.board.statusRunning"
      : agentTaskStatus === "error"
        ? "orchestration.board.statusError"
        : agentTaskStatus === "pending"
          ? "orchestration.board.statusPending"
          : agentTaskStatus === "idle"
            ? "orchestration.board.statusIdle"
            : "orchestration.board.statusDone";

  return (
    <div
      data-testid="conversation-panel"
      className="flex h-full flex-col bg-sidebar"
    >
      {/* cvHead: bg-card, padding [12,14], border-b, gap-2, vertical */}
      <div className="flex shrink-0 flex-col gap-2 border-b border-border bg-card px-3.5 py-3">
        {/* back row: arrow-left + label, gap=4px — shadcn Button */}
        <Button
          data-testid="conversation-back"
          variant="ghost"
          size="sm"
          className="h-auto gap-1 p-0 text-[11px] font-normal text-muted-foreground hover:bg-transparent hover:text-foreground"
          onClick={onBack}
          type="button"
        >
          <ArrowLeft className="size-3 shrink-0" aria-hidden="true" />
          {t("orchestration.conversation.backToBoard")}
        </Button>

        {/* who row: avatar(26px circle) + name-column fill + status dot */}
        <div className="flex items-center gap-2" data-testid="conversation-who">
          <AgentAvatar
            name={agentName}
            color={agentColor}
            size="sm"
            className="size-[26px] shrink-0 rounded-full"
          />
          <div className="flex min-w-0 flex-1 flex-col gap-px">
            {/* Line 1: agentName · 会话 (13px/600) */}
            <span
              data-testid="conversation-who-name"
              className="truncate text-[13px] font-semibold leading-none text-foreground"
            >
              {agentName} · {t("orchestration.conversation.sessionLabel")}
            </span>
            {/* Line 2: statusLabel · N 任务 (10px mono muted) */}
            <span
              data-testid="conversation-who-subtitle"
              className="truncate font-mono text-[10px] leading-none text-muted-foreground"
            >
              {t(statusLabelKey)} · {agentTaskCount}{" "}
              {t("orchestration.conversation.tasksUnit")}
            </span>
          </div>
          {/* Trailing 7px status dot */}
          <StatusDot
            data-testid="conversation-who-status-dot"
            status={isAwaiting ? "waiting" : agentStatus}
            size="xs"
            className="size-[7px] shrink-0"
          />
        </div>
      </div>

      {/* cvBody: bg-sidebar, padding 14, gap-3, vertical, overflow hidden */}
      <div className="flex min-h-0 flex-1 flex-col gap-3 overflow-hidden bg-sidebar p-3.5">
        {/* waiting callout — shown only when agent is awaiting a peer reply */}
        {isAwaiting && (
          <div
            data-testid="conversation-awaiting-callout"
            className="flex shrink-0 items-center gap-1.5 rounded-lg border-l-[3px] border-status-waiting bg-status-waiting-bg p-2.5"
          >
            <Clock
              className="size-3 shrink-0 text-status-waiting"
              aria-hidden="true"
            />
            <span className="text-[11px] font-semibold text-status-waiting">
              {t("orchestration.conversation.awaitingReply")}
            </span>
          </div>
        )}

        {/* read-only transcript: omit onRerun/onEdit props; live overlay wired
            from useLiveConversation so streaming deltas render live. */}
        {/* ref={setScrollEl} on the actual scrolling element */}
        <div ref={setScrollEl} className="min-h-0 flex-1 overflow-y-auto">
          <ChatTranscript
            agentName={agentName}
            agentColor={agentColor}
            sessionId={sessionId}
            messages={messages}
            scrollElement={scrollEl}
            virtualize
            active
            liveDelta={live.liveDelta}
            liveThinking={live.liveThinking}
            liveBlocks={live.liveBlocks}
            liveRetry={live.liveRetry}
            liveStreamStartedAt={live.liveStreamStartedAt}
            streaming={live.streaming}
            liveCompacting={live.liveCompacting}
          />
        </div>
      </div>

      {/* cvInput: ChatComposer replaces the bare textarea + send button so
          orchestration conversations get the same slash-menu/image/permission-mode
          affordances as the main chat panel. */}
      <div className="shrink-0 border-t border-border bg-card px-3 py-2.5">
        <ChatComposer
          placeholder={t("orchestration.conversation.speakPlaceholder")}
          backendType={backendType}
          supportsImageInput={supportsImageInput}
          contextUsage={contextUsage}
          permissionModeSlot={
            isModeSwitchable ? (
              <PermissionModePill
                mode={permissionMode.mode}
                modes={permissionModeMeta.order}
                onSelect={permissionMode.setMode}
                errorMessage={permissionMode.error}
                runtimeKey={backendType}
                permissionModeAtLaunch={permissionMode.permissionModeAtLaunch}
                hasActiveSession={permissionMode.hasActiveSession}
              />
            ) : null
          }
          onShiftTab={isModeSwitchable ? permissionMode.cycleMode : undefined}
          onSubmit={(m: ChatComposerSubmit | string) => {
            const msg = typeof m === "string" ? { text: m } : m;
            void submit(msg);
          }}
        />
      </div>
    </div>
  );
}
