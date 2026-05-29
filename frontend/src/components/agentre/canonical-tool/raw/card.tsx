import * as React from "react";
import { useTranslation } from "react-i18next";
import {
  Check,
  ChevronRight,
  LoaderCircle,
  Terminal,
  TriangleAlert,
  Wrench,
} from "lucide-react";

import { cn } from "@/lib/utils";
import type { ChatBlockData } from "@/stores/chat-streams-store";

import { statusConfig, type AgentStatus } from "../../types";
import type { CanonicalCardProps } from "../props";

import { summarizeRawTool } from "./summary";
import { ToolPermissionOverlay } from "./tool-permission-overlay";

// RawToolCard 是不进 canonical 集合的工具(Bash/Read/Glob/MCP 等)的兜底卡。
// 视觉等价于旧 ToolInvocationCard:折叠/展开 + 状态 pill + 参数表 + 错误染色 +
// command_execution 结果解析,但识别都走 input shape (input.command 存在 ⇒
// shell-shape),**不复活 name 硬集合** —— 那是 backend-specific 知识泄漏。
export const RawToolCard: React.FC<CanonicalCardProps> = ({
  toolBlock,
  resultBlock,
  cwd,
  sessionId,
}) => {
  const { t } = useTranslation();
  const [expanded, setExpanded] = React.useState(false);

  const toolName = toolBlock.toolName ?? "tool";
  const input = toolBlock.toolInput as Record<string, unknown> | undefined;
  const isShellShape = typeof input?.command === "string";
  const toolLabel = isShellShape ? "Bash" : toolName;
  const ToolIcon = isShellShape ? Terminal : Wrench;

  const summary = React.useMemo(
    () => summarizeRawTool(toolName, input, { cwd }),
    [toolName, input, cwd],
  );

  const commandResult = React.useMemo(
    () => parseCommandExecutionResult(resultBlock?.text),
    [resultBlock?.text],
  );
  const commandFailed =
    commandResult !== null &&
    ((typeof commandResult.exitCode === "number" &&
      commandResult.exitCode !== 0) ||
      commandResult.status === "failed" ||
      commandResult.status === "error" ||
      commandResult.status === "interrupted");

  const hasResult = !!resultBlock;
  const isError = !!resultBlock?.isError || commandFailed;
  const status: AgentStatus = isError
    ? "error"
    : hasResult
      ? "running"
      : "waiting";
  const pillConfig = statusConfig[status];

  const statusLabel =
    typeof commandResult?.exitCode === "number"
      ? `EXIT ${commandResult.exitCode}`
      : isError
        ? t("canonical.status.error")
        : hasResult
          ? t("canonical.status.done")
          : t("canonical.status.running");
  const StatusIcon = isError ? TriangleAlert : hasResult ? Check : LoaderCircle;

  const perm = (toolBlock as ChatBlockData).toolPermission;
  const showOverlay = perm && !perm.resolved;
  const allowedBadge =
    perm?.resolved && perm.allowed
      ? perm.alwaysAllow
        ? t("canonical.raw.allowedSession")
        : t("canonical.raw.allowed")
      : null;

  const params = React.useMemo(() => toolInputEntries(input), [input]);
  const resultMeta = commandResult
    ? formatCommandMeta(commandResult)
    : hasResult
      ? null
      : t("canonical.raw.waitingResult");

  return (
    <section
      data-testid="raw-tool-card"
      aria-label={t("canonical.raw.aria", { tool: toolName })}
      className={cn(
        "w-full max-w-[720px] overflow-hidden rounded-md border bg-card font-mono text-xs",
        isError ? "border-status-error/40" : "border-border",
      )}
    >
      <button
        type="button"
        onClick={() => setExpanded((v) => !v)}
        aria-expanded={expanded}
        className="flex w-full min-w-0 cursor-pointer items-center gap-2 px-3 py-2 text-left transition-colors hover:bg-muted/40"
      >
        <ChevronRight
          className={cn(
            "size-3 shrink-0 text-muted-foreground transition-transform",
            expanded && "rotate-90",
          )}
          aria-hidden="true"
        />
        <ToolIcon
          className="size-3.5 shrink-0 text-primary-text"
          aria-hidden="true"
        />
        <span className="shrink-0 font-semibold text-primary-text">
          {toolLabel}
        </span>
        {summary && (
          <>
            <span className="text-muted-foreground">·</span>
            <span className="min-w-0 truncate text-muted-foreground">
              {summary}
            </span>
          </>
        )}
        <span className="min-w-0 flex-1" />
        {allowedBadge && (
          <span
            className="inline-flex shrink-0 items-center gap-1 rounded bg-emerald-500/15 px-1.5 py-0.5 text-[9px] font-semibold tracking-[0.04em] text-emerald-600 dark:text-emerald-400"
            title={t("canonical.raw.approvedTitle")}
          >
            <Check className="size-2.5" aria-hidden="true" />
            {allowedBadge}
          </span>
        )}
        <span
          className={cn(
            "inline-flex shrink-0 items-center gap-1 rounded px-1.5 py-0.5 text-[9px] font-semibold tracking-[0.04em]",
            pillConfig.pillClassName,
          )}
        >
          <StatusIcon
            className={cn(
              "size-2.5",
              !hasResult && !isError ? "animate-spin" : "",
            )}
            aria-hidden="true"
          />
          {statusLabel}
        </span>
      </button>
      {expanded && (
        <div className="flex flex-col gap-3 border-t border-border px-3 py-3">
          <Section label={t("canonical.raw.sections.params")}>
            {params.length === 0 ? (
              <div className="text-muted-foreground">
                {t("canonical.raw.emptyParams")}
              </div>
            ) : (
              <dl className="grid max-h-[120px] grid-cols-[minmax(80px,auto)_1fr] gap-x-3 gap-y-1 overflow-y-auto overscroll-contain px-1">
                {params.map(([key, value]) => (
                  <React.Fragment key={key}>
                    <dt className="text-muted-foreground">{key}</dt>
                    <dd className="min-w-0 whitespace-pre-wrap break-words text-foreground">
                      {value}
                    </dd>
                  </React.Fragment>
                ))}
              </dl>
            )}
          </Section>
          <Section
            label={t("canonical.raw.sections.result")}
            meta={
              resultMeta ? (
                <span
                  className={!hasResult ? pillConfig.textClassName : undefined}
                >
                  {resultMeta}
                </span>
              ) : null
            }
          >
            <div
              className={cn(
                "min-w-0 max-h-[200px] overflow-y-auto overscroll-contain whitespace-pre-wrap break-words rounded-sm px-2.5 py-2",
                isError
                  ? "bg-destructive-soft text-status-error"
                  : "bg-muted/40 text-foreground",
              )}
            >
              {commandResult ? (
                commandResult.output ? (
                  <span>{commandResult.output}</span>
                ) : (
                  t("canonical.raw.emptyOutput")
                )
              ) : hasResult ? (
                resultBlock?.text ? (
                  <span>{resultBlock.text}</span>
                ) : (
                  t("canonical.raw.emptyResult")
                )
              ) : (
                "—"
              )}
            </div>
          </Section>
        </div>
      )}
      {showOverlay && perm && (
        <ToolPermissionOverlay
          payload={{ requestId: perm.requestId, toolName: perm.toolName }}
          sessionId={sessionId}
        />
      )}
    </section>
  );
};

function Section({
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
        <span className="font-sans text-[11px] font-semibold tracking-wide text-muted-foreground">
          {label}
        </span>
        {meta ? (
          <span className="font-mono text-[10px] text-subtle-foreground">
            {meta}
          </span>
        ) : null}
        <span className="h-px min-w-0 flex-1 bg-border" />
      </div>
      {children}
    </div>
  );
}

function toolInputEntries(input?: Record<string, unknown>): [string, string][] {
  if (!input) return [];
  return Object.entries(input)
    .filter(([, value]) => typeof value !== "undefined")
    .map(([key, value]) => [key, stringifyValue(value)]);
}

function stringifyValue(value: unknown): string {
  if (value == null) return "";
  if (typeof value === "string") return value;
  if (typeof value === "number" || typeof value === "boolean") {
    return String(value);
  }
  try {
    return JSON.stringify(value);
  } catch {
    return String(value);
  }
}

type CommandExecutionResult = {
  exitCode?: number;
  output: string;
  status?: string;
};

// command_execution 工具返回 JSON {exitCode, output, status};其它工具的 result
// 直接是 plain text。**靠 result shape 判定**,不靠 toolName。
function parseCommandExecutionResult(
  text?: string,
): CommandExecutionResult | null {
  if (typeof text !== "string") return null;
  let parsed: unknown;
  try {
    parsed = JSON.parse(text);
  } catch {
    return null;
  }
  if (!parsed || typeof parsed !== "object") return null;
  const data = parsed as Record<string, unknown>;
  if (!("output" in data) && !("exitCode" in data) && !("status" in data)) {
    return null;
  }
  return {
    exitCode: typeof data.exitCode === "number" ? data.exitCode : undefined,
    output: commandOutput(data.output),
    status: typeof data.status === "string" ? data.status : undefined,
  };
}

function commandOutput(output: unknown): string {
  if (output == null) return "";
  if (typeof output === "string") return output;
  try {
    return JSON.stringify(output, null, 2);
  } catch {
    return String(output);
  }
}

function formatCommandMeta(result: CommandExecutionResult): string {
  const parts: string[] = [];
  if (typeof result.exitCode === "number")
    parts.push(`exit ${result.exitCode}`);
  if (result.status) parts.push(result.status);
  return parts.join(" · ");
}
