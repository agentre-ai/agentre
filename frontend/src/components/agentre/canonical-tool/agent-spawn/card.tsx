import * as React from "react";
import { useTranslation } from "react-i18next";
import type { TFunction } from "i18next";
import {
  Check,
  ChevronRight,
  CircleSlash,
  Clock,
  FileText,
  LoaderCircle,
  Search,
  Square,
  Terminal,
  TriangleAlert,
  Users,
  Wrench,
} from "lucide-react";

import { cn } from "@/lib/utils";
import type { ChatBlockData } from "@/stores/chat-streams-store";

import { shouldIgnoreClickForSelection } from "../../copyable-text";
import {
  TranscriptCard,
  TranscriptCardHeader,
  TranscriptPill,
} from "../../transcript-card";
import { statusConfig, type AgentStatus } from "../../types";
import { useTranscriptBooleanState } from "../../transcript-ui-state";
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
    // 子代理首帧的实际模型覆盖派遣瞬间的入参别名(R2);两者都缺时 model 仍是 undefined。
    model: meta.model || base.model,
  };
}

// shortenModelName 归一化模型短名供头部徽标显示(R7):只剥 "claude-" 前缀与结尾
// 8 位日期段,其余字符原样保留。第三方模型名(网关接入的任意串,如 glm-4.6)不含
// 这两种模式,原样返回——裁剪规则不做进一步"美化",避免把不认识的名字改坏。
// 展开区 meta 行使用未经裁剪的原值,不调用这个函数。
export function shortenModelName(model: string): string {
  const withoutPrefix = model.startsWith("claude-")
    ? model.slice("claude-".length)
    : model;
  return withoutPrefix.replace(/-\d{8}$/, "");
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

// formatTokenCount 是纯数字形式(无 "tok" 单位),供头部极简进度(R9)使用;
// formatTokens 在其后拼接单位,供展开区 meta 行(R8)使用。
function formatTokenCount(n: number): string {
  if (n < 1000) return `${n}`;
  if (n < 1_000_000) return `${(n / 1000).toFixed(1)}K`;
  return `${(n / 1_000_000).toFixed(2)}M`;
}

function formatTokens(n: number): string {
  return `${formatTokenCount(n)} tok`;
}

// buildProgressTick 拼出头部极简进度(R9):纯数字、无「个工具」「tok」字样,
// 用 " · " 连接;任一侧为零则只留另一侧;两侧都为零时返回空串(不渲染)。
function buildProgressTick(toolUses: number, totalTokens: number): string {
  const parts: string[] = [];
  if (toolUses > 0) parts.push(`${toolUses}`);
  if (totalTokens > 0) parts.push(formatTokenCount(totalTokens));
  return parts.join(" · ");
}

// buildProgressAriaLabel 给极简进度(R9)补一个无障碍标签:可见文案仍是
// 无单位的 "3 · 14.5K"(不把「个工具」「tok」文案加回可见位置,否则推翻 R9),
// 但 aria-label 让折叠态下的读屏用户也能知道这两个数字分别是什么,不必强制
// 展开卡片才能获得语义。只用 aria-label,不用 title——title 会在鼠标悬停时
// 可见地弹出同样的文案,等于把 R9 刚去掉的文案又从后门带回来。与
// buildProgressTick 同样的"任一侧为零则只留另一侧"规则。
function buildProgressAriaLabel(
  t: TFunction,
  toolUses: number,
  totalTokens: number,
): string {
  const parts: string[] = [];
  if (toolUses > 0) {
    parts.push(
      t("canonical.agentSpawn.progressAria.tools", { count: toolUses }),
    );
  }
  if (totalTokens > 0) {
    // count 是 i18next 的保留选项,会被送进 Intl.PluralRules.select() 做单
    // 复数判定;formatTokenCount 产出的是展示用字符串(如 "14.5K"),不是可
    // 数的语法数量,不应该借用这个保留名字,改用普通具名占位符 value。
    parts.push(
      t("canonical.agentSpawn.progressAria.tokens", {
        value: formatTokenCount(totalTokens),
      }),
    );
  }
  return parts.join(", ");
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
  uiStateKey,
  onStopSubagent,
}) => {
  const { t } = useTranslation();
  const spawn = readSpawn(toolBlock);
  const [expanded, setExpanded] = useTranscriptBooleanState(uiStateKey, false);

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
  const modelBadge = spawn.model ? shortenModelName(spawn.model) : "";
  const prompt = spawn.prompt || "";
  const steps = pairChildBlocks(childBlocks);
  const toolUses = spawn.toolUses ?? steps.length;
  const totalTokens = spawn.totalTokens ?? 0;
  const durationMs = spawn.durationMs ?? 0;
  // R9:极简进度只在运行态渲染,完成/取消/出错都不再显示。
  const isRunningState = status === "running" || status === "waiting";
  const progressTick = isRunningState
    ? buildProgressTick(toolUses, totalTokens)
    : "";
  const progressAriaLabel = isRunningState
    ? buildProgressAriaLabel(t, toolUses, totalTokens)
    : "";
  // R8:展开区 meta 行独立于运行状态存在,只要有值就列出;模型用未裁剪原值。
  const hasMetaRow =
    !!spawn.model || toolUses > 0 || totalTokens > 0 || durationMs > 0;

  return (
    <TranscriptCard
      data-testid="agent-spawn-card"
      aria-label={t("canonical.agentSpawn.aria", {
        name:
          description || subagentType || t("canonical.agentSpawn.defaultName"),
      })}
      data-selectable-text="true"
      tone={status === "error" ? "error" : "default"}
      className="font-mono text-aux"
    >
      <TranscriptCardHeader
        onClick={(event) => {
          if (shouldIgnoreClickForSelection(event)) return;
          setExpanded((v) => !v);
        }}
        aria-expanded={expanded}
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
              // R11:让位次序第一名——flex-shrink 权重远大于极简进度(见下方
              // agent-spawn-progress 的注释),这样无论两者各自的内容宽度比例
              // 如何,横向空间不足时都是描述先被压缩到裁切,而非按同一收缩
              // 因子与进度同时按比例挤压。
              className="min-w-0 shrink-[20] truncate text-muted-foreground"
            >
              {description}
            </span>
          </>
        ) : null}
        {subagentType ? (
          <span
            data-testid="agent-spawn-role-badge"
            data-copyable-control-text="true"
            className="ml-1 inline-flex shrink-0 items-center rounded-sm bg-secondary px-1.5 py-0.5 text-meta font-medium text-foreground"
          >
            {subagentType}
          </span>
        ) : null}
        {modelBadge ? (
          <span
            data-testid="agent-spawn-model-badge"
            data-copyable-control-text="true"
            // R7(修订)/R11:不再用与宽度无关的固定 max-w 裁剪——横向空间充足
            // 时徽标必须完整显示裁剪后的全文。空间不足时改为参与 R11 的
            // flex-shrink 让位次序:排在描述(shrink-[20])与极简进度(默认
            // shrink=1)都让位之后,shrink-[0.2] 是比两者都小的正因子,
            // min-w-0 允许它在挤压下真正收缩到 0 而不是撑破容器。truncate
            // 挪到内层 <span>、overflow-hidden 留在这个 flex 容器上——与下方
            // agent-spawn-progress 同一写法:text-overflow:ellipsis 只在块级
            // 容器生效,加在 inline-flex 容器本身,文本子节点会变成匿名 flex
            // item,省略号永远不会绘制,只会齐字硬切。完整原值仍在展开区
            // meta 行可见,截断不丢信息。
            className="ml-1 inline-flex min-w-0 shrink-[0.2] items-center overflow-hidden rounded-sm border border-border-strong bg-transparent px-1.5 py-0.5 text-meta font-normal text-muted-foreground"
          >
            <span className="truncate">{modelBadge}</span>
          </span>
        ) : null}
        <span className="min-w-0 flex-1" />
        {progressTick ? (
          // R9/R11:纯数字极简进度,次级前景色、无文案。让位次序第二名——
          // 保留默认 flex-shrink:1(通过 `shrink`),远小于描述的 `shrink-[20]`,
          // 所以横向空间不足时几乎全部收缩量先记到描述头上,直到描述被压到
          // min-w-0 的下限,余量才轮到这里继续收缩/裁切;状态胶囊(shrink-0)
          // 完全不参与收缩,任何宽度下都保持完整。
          <span
            data-testid="agent-spawn-progress"
            data-copyable-control-text="true"
            aria-label={progressAriaLabel}
            className="inline-flex min-w-0 shrink items-center gap-1 overflow-hidden text-meta text-subtle-foreground"
          >
            <Wrench className="size-2.5 shrink-0" aria-hidden="true" />
            <span className="truncate">{progressTick}</span>
          </span>
        ) : null}
        {(status === "running" || status === "waiting") &&
          spawn.taskId &&
          onStopSubagent &&
          toolBlock.toolUseId && (
            <button
              type="button"
              onClick={(event) => {
                event.stopPropagation();
                onStopSubagent(toolBlock.toolUseId!);
              }}
              aria-label={t("canonical.agentSpawn.stop")}
              title={t("canonical.agentSpawn.stop")}
              className="inline-flex size-5 shrink-0 items-center justify-center rounded text-muted-foreground transition-colors hover:bg-destructive/10 hover:text-destructive"
            >
              <Square className="size-2.5 fill-current" />
            </button>
          )}
        <TranscriptPill
          data-copyable-control-text="true"
          className={pillClassName}
        >
          <StatusIcon
            className={cn(
              "size-2.5",
              (status === "running" || status === "waiting") && "animate-spin",
            )}
            aria-hidden="true"
          />
          {statusLabel}
        </TranscriptPill>
      </TranscriptCardHeader>
      <div
        data-slot="agent-spawn-details"
        aria-hidden={!expanded}
        className="grid transition-[grid-template-rows] duration-200 ease-out motion-reduce:transition-none"
        style={{ gridTemplateRows: expanded ? "1fr" : "0fr" }}
      >
        <div className="min-h-0 overflow-hidden">
          <div className="flex flex-col gap-3 border-t border-border px-3 py-3">
            {hasMetaRow ? (
              <div
                data-testid="agent-spawn-meta-row"
                className="flex flex-wrap items-center gap-x-3.5 gap-y-1 text-meta"
              >
                {spawn.model ? (
                  <AgentSpawnMetaItem
                    testId="agent-spawn-meta-model"
                    label={t("canonical.agentSpawn.meta.model")}
                    value={spawn.model}
                  />
                ) : null}
                {toolUses > 0 ? (
                  <AgentSpawnMetaItem
                    testId="agent-spawn-meta-tools"
                    label={t("canonical.agentSpawn.meta.tools")}
                    value={`${toolUses}`}
                  />
                ) : null}
                {totalTokens > 0 ? (
                  <AgentSpawnMetaItem
                    testId="agent-spawn-meta-tokens"
                    label={t("canonical.agentSpawn.meta.tokens")}
                    value={formatTokens(totalTokens)}
                  />
                ) : null}
                {durationMs > 0 ? (
                  <AgentSpawnMetaItem
                    testId="agent-spawn-meta-duration"
                    label={t("canonical.agentSpawn.meta.duration")}
                    value={formatDuration(durationMs)}
                  />
                ) : null}
              </div>
            ) : null}
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
                  {steps.map((s, idx) => (
                    <AgentSpawnStepCard
                      key={s.tool.toolUseId || s.tool.text || ""}
                      step={s}
                      cwd={cwd}
                      uiStateKey={
                        uiStateKey
                          ? `${uiStateKey}:step:${s.tool.toolUseId || idx}`
                          : undefined
                      }
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
    </TranscriptCard>
  );
};

// AgentSpawnMetaItem 渲染展开区 meta 行的单个「标签 值」项(R8)。value 只放在
// 独立的 testId 节点上,不与标签共用一个 textContent——下沉数字与调用方(测试/
// 未来其它读者)需要精确取到裸值,不掺文案。
function AgentSpawnMetaItem({
  testId,
  label,
  value,
}: {
  testId: string;
  label: string;
  value: string;
}) {
  return (
    <span className="inline-flex items-center gap-1">
      <span className="text-subtle-foreground">{label}</span>
      <span data-testid={testId} className="text-foreground">
        {value}
      </span>
    </span>
  );
}

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
        <span className="font-sans text-meta font-semibold uppercase tracking-[0.06em] text-muted-foreground">
          {label}
        </span>
        {meta ? (
          <span className="font-mono text-meta text-subtle-foreground">
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
  uiStateKey,
}: {
  cwd?: string;
  step: StepRow;
  uiStateKey?: string;
}): React.ReactElement {
  const { t } = useTranslation();
  const [expanded, setExpanded] = useTranscriptBooleanState(uiStateKey, false);
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
        "overflow-hidden rounded-lg border bg-background",
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
        <TranscriptPill
          data-copyable-control-text="true"
          className={statusConfig[stepStatus].pillClassName}
        >
          <StepStatusIcon
            className={cn("size-2.5", isRunning && "animate-spin")}
            aria-hidden="true"
          />
          {stepLabel}
        </TranscriptPill>
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
