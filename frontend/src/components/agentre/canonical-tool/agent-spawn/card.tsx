import * as React from "react";
import { useTranslation } from "react-i18next";
import type { TFunction } from "i18next";
import {
  Check,
  ChevronRight,
  CircleSlash,
  Clock,
  Coins,
  FileText,
  LoaderCircle,
  Search,
  Terminal,
  TriangleAlert,
  Users,
  Wrench,
} from "lucide-react";

import { cn } from "@/lib/utils";
import type { ChatBlockData } from "@/stores/chat-streams-store";

import { shouldIgnoreClickForSelection } from "../../copyable-text";
import { statusConfig, type AgentStatus } from "../../types";
import { summarizeRawTool } from "../raw/summary";
import type { CanonicalCardProps } from "../props";
import type { AgentSpawnDTO, CanonicalDTO } from "../types";

// readSpawn 合并 canonical.agentSpawn(translator 算的静态字段:description/subagentType/
// prompt/taskId)与 toolBlock.subagent(SubagentStarted/Progress/Done 经 mergeSubagentMeta
// 累积的运行时字段:toolUses/totalTokens/durationMs/status/lastToolName)。零值不覆盖,避免
// 早到的空 meta 把已有进度抹掉。
function readSpawn(toolBlock: ChatBlockData): AgentSpawnDTO | undefined {
  const c = (toolBlock as { canonical?: CanonicalDTO }).canonical;
  if (!c || c.kind !== "agent.spawn") return undefined;
  const base = c.agentSpawn;
  if (!base) return undefined;
  const meta = toolBlock.subagent;
  if (!meta) return base;
  return {
    ...base,
    lastToolName: meta.lastToolName || base.lastToolName,
    toolUses: meta.toolUses ?? base.toolUses,
    totalTokens: meta.totalTokens ?? base.totalTokens,
    durationMs: meta.durationMs ?? base.durationMs,
    status: narrowSpawnStatus(meta.status) ?? base.status,
  };
}

function narrowSpawnStatus(
  s: string | undefined,
): AgentSpawnDTO["status"] | undefined {
  return s === "running" ||
    s === "completed" ||
    s === "failed" ||
    s === "canceled"
    ? s
    : undefined;
}

// isBashLikeTool 子调用 step 选 Terminal icon。
function isBashLikeTool(name: string): boolean {
  const n = name.toLowerCase();
  return (
    n === "bash" ||
    n === "shell" ||
    n === "run" ||
    n === "exec" ||
    n === "command_execution"
  );
}

type StepRow = { tool: ChatBlockData; result?: ChatBlockData };

function pairChildBlocks(blocks: ChatBlockData[]): StepRow[] {
  const steps: StepRow[] = [];
  const byId = new Map<string, number>();
  for (const b of blocks) {
    if (b.type === "tool_use" && b.toolUseId) {
      steps.push({ tool: b });
      byId.set(b.toolUseId, steps.length - 1);
    } else if (b.type === "tool_result" && b.toolUseId) {
      const idx = byId.get(b.toolUseId);
      if (idx !== undefined) {
        steps[idx].result = b;
      }
    }
  }
  return steps;
}

function statusFromSpawn(
  spawn: AgentSpawnDTO,
  resultBlock: ChatBlockData | undefined,
): AgentStatus {
  if (resultBlock?.isError || spawn.status === "failed") return "error";
  if (spawn.status === "completed" || spawn.status === "canceled")
    return "idle";
  if (spawn.status === "running") return "running";
  if (resultBlock) return "idle";
  return "running";
}

// buildStatusLabel: cancelled 走 idle 通道但 label 单独显示 STOPPED,避免和正常
// 完成的 DONE 混淆。
function buildStatusLabel(
  status: AgentStatus,
  spawnStatus: AgentSpawnDTO["status"] | undefined,
  t: TFunction,
  durationMs?: number,
): string {
  const dur = durationMs ? formatDuration(durationMs) : "";
  if (spawnStatus === "canceled") {
    return dur
      ? t("canonical.agentSpawn.status.withDuration", {
          status: t("canonical.agentSpawn.status.stopped"),
          duration: dur,
        })
      : t("canonical.agentSpawn.status.stopped");
  }
  const label =
    status === "error"
      ? t("canonical.agentSpawn.status.error")
      : status === "running" || status === "waiting"
        ? t("canonical.agentSpawn.status.running")
        : t("canonical.agentSpawn.status.done");
  switch (status) {
    case "error":
    case "running":
    case "waiting":
    default:
      return dur
        ? t("canonical.agentSpawn.status.withDuration", {
            status: label,
            duration: dur,
          })
        : label;
  }
}

function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms}ms`;
  const s = ms / 1000;
  if (s < 60) return `${s.toFixed(1)}s`;
  const m = Math.floor(s / 60);
  const rem = Math.round(s % 60);
  return `${m}m${rem.toString().padStart(2, "0")}s`;
}

function formatTokens(n: number): string {
  if (n < 1000) return `${n} tok`;
  if (n < 1_000_000) return `${(n / 1000).toFixed(1)}K tok`;
  return `${(n / 1_000_000).toFixed(2)}M tok`;
}

// AgentSpawnCard 渲染 canonical.agent.spawn — 替代 SubagentInvocationCard。
// 数据形态:toolBlock(外层 Agent.tool_use,canonical.agentSpawn 由 translator 算好);
// resultBlock(外层 Agent.tool_result,content 为 SUMMARY 文本);
// childBlocks(subagent 内部 tool_use/tool_result,parentToolUseId 归集后传入)。
// 展开后:TASK PROMPT / STEPS(子调用) / SUMMARY 三段。
export const AgentSpawnCard: React.FC<CanonicalCardProps> = ({
  toolBlock,
  resultBlock,
  cwd,
  childBlocks = [],
}) => {
  const { t } = useTranslation();
  const spawn = readSpawn(toolBlock);
  const [expanded, setExpanded] = React.useState(false);

  if (!spawn) return null;

  const status = statusFromSpawn(spawn, resultBlock);
  const { pillClassName } = statusConfig[status];
  const StatusIcon =
    status === "error"
      ? TriangleAlert
      : spawn.status === "canceled"
        ? CircleSlash
        : status === "running" || status === "waiting"
          ? LoaderCircle
          : Check;
  const statusLabel = buildStatusLabel(
    status,
    spawn.status,
    t,
    spawn.durationMs,
  );

  const description = spawn.taskDescription || "";
  const subagentType = spawn.subagentType || "";
  const prompt = spawn.prompt || "";
  const steps = pairChildBlocks(childBlocks);
  const toolUses = spawn.toolUses ?? steps.length;
  const tokens = spawn.totalTokens ? formatTokens(spawn.totalTokens) : "";

  return (
    <section
      data-testid="agent-spawn-card"
      aria-label={t("canonical.agentSpawn.aria", {
        name:
          description || subagentType || t("canonical.agentSpawn.defaultName"),
      })}
      data-selectable-text="true"
      className={cn(
        "w-full max-w-[720px] overflow-hidden rounded-md border bg-card font-mono text-xs",
        status === "error" ? "border-status-error/40" : "border-border",
      )}
    >
      <button
        type="button"
        onClick={(event) => {
          if (shouldIgnoreClickForSelection(event)) return;
          setExpanded((v) => !v);
        }}
        aria-expanded={expanded}
        className="flex w-full min-w-0 cursor-pointer items-center gap-2 px-3 py-2 text-left transition-colors hover:bg-muted/40"
      >
        <ChevronRight
          className={cn(
            "size-3 shrink-0 text-muted-foreground transition-transform duration-150 ease-out motion-reduce:transition-none",
            expanded && "rotate-90",
          )}
          aria-hidden="true"
        />
        <Users
          className="size-3.5 shrink-0 text-primary-text"
          aria-hidden="true"
        />
        <span
          data-copyable-control-text="true"
          className="shrink-0 font-semibold text-primary-text"
        >
          {t("canonical.agentSpawn.defaultName")}
        </span>
        {description ? (
          <>
            <span className="text-muted-foreground">·</span>
            <span
              data-copyable-control-text="true"
              className="min-w-0 truncate text-muted-foreground"
            >
              {description}
            </span>
          </>
        ) : null}
        {subagentType ? (
          <span
            data-copyable-control-text="true"
            className="ml-1 inline-flex shrink-0 items-center rounded-sm bg-secondary px-1.5 py-0.5 text-2xs font-medium text-foreground"
          >
            {subagentType}
          </span>
        ) : null}
        <span className="min-w-0 flex-1" />
        {toolUses > 0 ? (
          <span
            data-copyable-control-text="true"
            className="inline-flex shrink-0 items-center gap-1 text-2xs text-muted-foreground"
          >
            <Wrench className="size-2.5" aria-hidden="true" />
            <span>
              {t("canonical.agentSpawn.toolCount", { count: toolUses })}
            </span>
          </span>
        ) : null}
        {toolUses > 0 && tokens ? (
          <span className="text-muted-foreground">·</span>
        ) : null}
        {tokens ? (
          <span
            data-copyable-control-text="true"
            className="inline-flex shrink-0 items-center gap-1 text-2xs text-muted-foreground"
          >
            <Coins className="size-2.5" aria-hidden="true" />
            <span>{tokens}</span>
          </span>
        ) : null}
        <span
          data-copyable-control-text="true"
          className={cn(
            "inline-flex shrink-0 items-center gap-1 rounded px-1.5 py-0.5 text-2xs font-semibold tracking-[0.04em]",
            pillClassName,
          )}
        >
          <StatusIcon
            className={cn(
              "size-2.5",
              (status === "running" || status === "waiting") && "animate-spin",
            )}
            aria-hidden="true"
          />
          {statusLabel}
        </span>
      </button>
      <div
        data-slot="agent-spawn-details"
        aria-hidden={!expanded}
        className="grid transition-[grid-template-rows] duration-200 ease-out motion-reduce:transition-none"
        style={{ gridTemplateRows: expanded ? "1fr" : "0fr" }}
      >
        <div className="min-h-0 overflow-hidden">
          <div className="flex flex-col gap-3 border-t border-border px-3 py-3">
            <AgentSpawnSection
              label={t("canonical.agentSpawn.sections.prompt")}
            >
              <div className="max-h-[160px] overflow-y-auto overscroll-contain whitespace-pre-wrap break-words rounded-sm bg-muted/40 px-2.5 py-2 text-foreground">
                {prompt ? (
                  <span>{prompt}</span>
                ) : (
                  <span className="text-muted-foreground">
                    {t("canonical.agentSpawn.emptyPrompt")}
                  </span>
                )}
              </div>
            </AgentSpawnSection>
            <AgentSpawnSection
              label={t("canonical.agentSpawn.sections.steps")}
              meta={
                steps.length === 0 && status === "running"
                  ? t("canonical.agentSpawn.waitingFirstStep")
                  : null
              }
            >
              {steps.length === 0 ? (
                <div className="rounded-sm bg-muted/30 px-2.5 py-2 text-muted-foreground">
                  {status === "running"
                    ? t("canonical.agentSpawn.emptyStepsRunning")
                    : t("canonical.agentSpawn.emptySteps")}
                </div>
              ) : (
                <div className="flex flex-col gap-2">
                  {steps.map((s) => (
                    <AgentSpawnStepCard
                      key={s.tool.toolUseId || s.tool.text || ""}
                      step={s}
                      cwd={cwd}
                    />
                  ))}
                </div>
              )}
            </AgentSpawnSection>
            <AgentSpawnSection
              label={t("canonical.agentSpawn.sections.summary")}
            >
              {renderSummary(status, t, resultBlock)}
            </AgentSpawnSection>
          </div>
        </div>
      </div>
    </section>
  );
};

function AgentSpawnSection({
  children,
  label,
  meta,
}: {
  children: React.ReactNode;
  label: string;
  meta?: React.ReactNode;
}) {
  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex items-center gap-2">
        <span className="font-sans text-2xs font-semibold uppercase tracking-[0.06em] text-muted-foreground">
          {label}
        </span>
        {meta ? (
          <span className="font-mono text-2xs text-subtle-foreground">
            {meta}
          </span>
        ) : null}
        <span className="h-px min-w-0 flex-1 bg-border" />
      </div>
      {children}
    </div>
  );
}

function AgentSpawnStepCard({
  cwd,
  step,
}: {
  cwd?: string;
  step: StepRow;
}): React.ReactElement {
  const { t } = useTranslation();
  const [expanded, setExpanded] = React.useState(false);
  const tool = step.tool;
  const result = step.result;
  const toolName = tool.toolName || "tool";
  const summary = summarizeRawTool(toolName, tool.toolInput, { cwd });
  const isError = !!result?.isError;
  const isRunning = !result;
  const toolNameLower = toolName.toLowerCase();
  const StepIcon = isBashLikeTool(toolName)
    ? Terminal
    : toolNameLower === "grep" ||
        toolNameLower === "glob" ||
        toolNameLower === "search" ||
        toolNameLower === "ripgrep"
      ? Search
      : toolNameLower === "read" ||
          toolNameLower === "read_file" ||
          toolNameLower === "write" ||
          toolNameLower === "write_file" ||
          toolNameLower === "edit"
        ? FileText
        : Wrench;
  const stepStatus: AgentStatus = isError
    ? "error"
    : isRunning
      ? "running"
      : "idle";
  const StepStatusIcon = isError
    ? TriangleAlert
    : isRunning
      ? LoaderCircle
      : Check;
  const stepLabel = isError
    ? t("canonical.agentSpawn.status.fail")
    : isRunning
      ? t("canonical.agentSpawn.status.runningLower")
      : t("canonical.agentSpawn.status.done");

  return (
    <div
      className={cn(
        "overflow-hidden rounded-md border bg-background",
        isError
          ? "border-status-error/50 border-l-2 border-l-status-error"
          : isRunning
            ? "border-status-running/50 border-l-2 border-l-status-running"
            : "border-border border-l-2 border-l-border",
      )}
    >
      <button
        type="button"
        onClick={(event) => {
          if (shouldIgnoreClickForSelection(event)) return;
          setExpanded((v) => !v);
        }}
        aria-expanded={expanded}
        className="flex w-full min-w-0 cursor-pointer items-center gap-2 px-2.5 py-1.5 text-left transition-colors hover:bg-muted/30"
      >
        <ChevronRight
          className={cn(
            "size-2.5 shrink-0 text-muted-foreground transition-transform duration-150 ease-out motion-reduce:transition-none",
            expanded && "rotate-90",
          )}
          aria-hidden="true"
        />
        <StepIcon
          className="size-3 shrink-0 text-foreground"
          aria-hidden="true"
        />
        <span
          data-copyable-control-text="true"
          className="shrink-0 font-semibold text-foreground"
        >
          {toolName}
        </span>
        {summary ? (
          <>
            <span className="text-muted-foreground">·</span>
            <span
              data-copyable-control-text="true"
              className="min-w-0 truncate text-muted-foreground"
            >
              {summary}
            </span>
          </>
        ) : null}
        <span className="min-w-0 flex-1" />
        <span
          data-copyable-control-text="true"
          className={cn(
            "inline-flex shrink-0 items-center gap-1 rounded px-1.5 py-0.5 text-2xs font-semibold tracking-[0.04em]",
            statusConfig[stepStatus].pillClassName,
          )}
        >
          <StepStatusIcon
            className={cn("size-2.5", isRunning && "animate-spin")}
            aria-hidden="true"
          />
          {stepLabel}
        </span>
      </button>
      <div
        aria-hidden={!expanded}
        className="grid transition-[grid-template-rows] duration-200 ease-out motion-reduce:transition-none"
        style={{ gridTemplateRows: expanded ? "1fr" : "0fr" }}
      >
        <div className="min-h-0 overflow-hidden">
          <div className="border-t border-border px-2.5 py-1.5">
            <div
              className={cn(
                "max-h-[180px] overflow-y-auto overscroll-contain whitespace-pre-wrap break-words rounded-sm px-2 py-1.5 text-foreground",
                isError
                  ? "bg-destructive-soft text-status-error"
                  : "bg-muted/40",
              )}
            >
              {result?.text ? (
                <span>{result.text}</span>
              ) : isRunning ? (
                "—"
              ) : (
                t("canonical.agentSpawn.emptyResult")
              )}
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}

function renderSummary(
  status: AgentStatus,
  t: TFunction,
  resultBlock?: ChatBlockData,
): React.ReactElement {
  if (!resultBlock || !resultBlock.text) {
    if (status === "running" || status === "waiting") {
      return (
        <div className="inline-flex items-center gap-2 rounded-sm bg-secondary px-2.5 py-2 text-muted-foreground">
          <Clock className="size-3" aria-hidden="true" />
          {t("canonical.agentSpawn.summaryRunning")}
        </div>
      );
    }
    return (
      <div className="rounded-sm bg-muted/40 px-2.5 py-2 text-muted-foreground">
        {t("canonical.agentSpawn.emptySummary")}
      </div>
    );
  }
  return (
    <div
      className={cn(
        "max-h-[220px] overflow-y-auto overscroll-contain whitespace-pre-wrap break-words rounded-sm border-l-2 px-2.5 py-2",
        status === "error"
          ? "border-status-error bg-destructive-soft text-status-error"
          : "border-primary bg-muted/40 text-foreground",
      )}
    >
      {resultBlock.text}
    </div>
  );
}
