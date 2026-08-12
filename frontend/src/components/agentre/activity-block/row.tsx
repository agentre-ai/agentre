import * as React from "react";
import { useTranslation } from "react-i18next";
import {
  Brain,
  ChevronRight,
  FilePlus,
  Pencil,
  Search,
  Terminal,
  TriangleAlert,
  Wrench,
} from "lucide-react";

import { cn } from "@/lib/utils";

import {
  CollapsibleCode,
  CollapsibleCodeParams,
  toolInputEntries,
} from "../collapsible-code";
import { FileBlock } from "../canonical-tool/file-edit/hunk-renderer";
import { FileWriteContent } from "../canonical-tool/file-write/content-renderer";
import { summarizeRawTool } from "../canonical-tool/raw/summary";
import { displayName, toolCategory } from "../canonical-tool/tier";
import { shouldIgnoreClickForSelection } from "../copyable-text";
import type { ActivityStep } from "../transcript-rows";
import { useTranscriptBooleanState } from "../transcript-ui-state";

import { canonicalOf, stepFacts, stepWeight } from "./facts";

// ActivityRow —— 活动块展开后的一行。整行是一个原生 button:可 Tab 聚焦、带
// aria-expanded、focus-visible 走既有的 ring。展开指示(chevron)静息时不可见,
// hover / 聚焦 / 展开才显形 —— 它是视觉修饰,不影响可聚焦性与读屏播报。
//
// 三档权重(读 / 中性 / 写)只体现在字重与前景色上:行高、内边距、交互完全相同。
// 分层是为了扫视,不是为了制造第二种控件。

// 图标按 toolCategory 的类目取,不查工具名表。
const CATEGORY_ICON = {
  command: Terminal,
  edit: Pencil,
  other: Wrench,
  read: Search,
  write: FilePlus,
} as const;

const NAME_CLASS: Record<string, string> = {
  neutral: "text-foreground font-medium",
  read: "text-muted-foreground",
  write: "text-foreground font-semibold",
};

const ICON_CLASS: Record<string, string> = {
  neutral: "text-muted-foreground",
  read: "text-subtle-foreground",
  write: "text-foreground",
};

export function ActivityRow({
  step,
  cwd,
}: {
  step: ActivityStep;
  cwd?: string;
}) {
  const { t } = useTranslation();
  // 折叠态挂在这一步「单独成行时」的同一个 key 上 —— 组内组外字节级一致,
  // 用户展开过的那一步不会因为它被折进组而丢掉展开态。
  const [expanded, setExpanded] = useTranscriptBooleanState(
    step.uiStateKey,
    false,
  );

  const weight = stepWeight(step);
  const facts = stepFacts(step);
  const isThinking = step.type === "thinking";
  const toolBlock = isThinking ? undefined : step.toolBlock;
  const input = toolBlock?.toolInput as Record<string, unknown> | undefined;
  // RawToolCard 的同一条规矩:input 里有 command 就是 shell 形态,标签用 Bash,
  // 不认工具名(codex 的 command_execution 与 claudecode 的 Bash 同形)。
  const isShellShape = typeof input?.command === "string";
  const name = isThinking
    ? t("activity.row.thinking")
    : isShellShape
      ? "Bash"
      : displayName(toolBlock?.toolName ?? "tool");
  const summary = isThinking
    ? t("activity.row.thinkingMeta", { count: (step.block.text ?? "").length })
    : summarizeRawTool(toolBlock?.toolName ?? "", input, { cwd });
  const Icon = facts.failed
    ? TriangleAlert
    : isThinking
      ? Brain
      : CATEGORY_ICON[toolBlock ? toolCategory(toolBlock) : "other"];

  const handleToggle = (event: React.MouseEvent<HTMLButtonElement>) => {
    if (shouldIgnoreClickForSelection(event)) return;
    setExpanded((v) => !v);
  };

  return (
    <div>
      <button
        type="button"
        data-testid="activity-row"
        data-weight={weight}
        data-failed={facts.failed ? "true" : undefined}
        aria-expanded={expanded}
        aria-label={t("activity.row.toggle", { tool: name })}
        onClick={handleToggle}
        className="group flex w-full items-center gap-2 rounded px-1.5 py-0.5 text-left font-mono text-meta text-muted-foreground transition-colors duration-150 ease-out hover:bg-muted/55 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50 motion-reduce:transition-none"
      >
        <ChevronRight
          aria-hidden="true"
          className={cn(
            "size-3 shrink-0 text-subtle-foreground opacity-0 transition-[opacity,transform] duration-150 ease-out group-hover:opacity-100 group-focus-visible:opacity-100 motion-reduce:transition-none",
            expanded && "rotate-90 opacity-100",
          )}
        />
        <Icon
          aria-hidden="true"
          className={cn(
            "size-3 shrink-0",
            facts.failed ? "text-status-error" : ICON_CLASS[weight],
          )}
        />
        <span
          data-testid="activity-name"
          className={cn(
            "shrink-0",
            facts.failed ? "text-status-error" : NAME_CLASS[weight],
          )}
        >
          {name}
        </span>
        {summary ? (
          <span className="min-w-0 flex-1 truncate text-subtle-foreground">
            {summary}
          </span>
        ) : (
          <span className="min-w-0 flex-1" />
        )}
        <StepTrailing facts={facts} />
      </button>
      <div
        aria-hidden={!expanded}
        className="grid transition-[grid-template-rows] duration-200 ease-out motion-reduce:transition-none"
        style={{ gridTemplateRows: expanded ? "1fr" : "0fr" }}
      >
        <div className="min-h-0 overflow-hidden">
          {
            // 性能:结果文本懒挂载 —— 折叠态不 mount 这一步的结果(同
            // agent-spawn/card.tsx 已确立的约定:几十 KB 隐藏文本会让整个
            // transcript 陪跑 layout/paint)。
            expanded ? (
              <div
                data-testid="activity-row-body"
                data-selectable-text="true"
                className="ml-1.5 flex flex-col gap-1.5 border-l border-border py-1 pl-3"
              >
                <ActivityRowBody step={step} facts={facts} />
              </div>
            ) : null
          }
        </div>
      </div>
    </div>
  );
}

// StepTrailing:行尾的实义信息。写操作报增删行,失败报红色 exit N,其余报结果
// 规模(多行)或结果预览(单行)—— 成功态腾出来的位置放实义信息,不放绿胶囊。
// 失败行不报预览:它的右侧留给红色 exit N,失败详情点开就在展开体里。
function StepTrailing({ facts }: { facts: ReturnType<typeof stepFacts> }) {
  const { t } = useTranslation();
  return (
    <>
      {facts.plus !== undefined && facts.plus > 0 ? (
        <span className="shrink-0 font-semibold text-status-running">
          +{facts.plus}
        </span>
      ) : null}
      {facts.minus !== undefined && facts.minus > 0 ? (
        <span className="shrink-0 font-semibold text-destructive">
          −{facts.minus}
        </span>
      ) : null}
      {facts.exitCode !== undefined ? (
        <span
          className={cn(
            "shrink-0",
            facts.failed ? "text-status-error" : "text-subtle-foreground",
          )}
        >
          {t("activity.row.exit", { code: facts.exitCode })}
        </span>
      ) : facts.failed ? (
        <span className="shrink-0 text-status-error">
          {t("activity.row.failed")}
        </span>
      ) : null}
      {facts.failed ? null : facts.lines !== undefined ? (
        <span className="shrink-0 text-subtle-foreground">
          {t("activity.row.lines", { count: facts.lines })}
        </span>
      ) : facts.preview ? (
        <span className="max-w-56 shrink-0 truncate text-subtle-foreground">
          {facts.preview}
        </span>
      ) : null}
    </>
  );
}

// ActivityRowBody:展开体按该步的 canonical.kind 复用既有卡片正文 ——
// file.edit 走既有 diff hunk、file.write 给文件内容、其余是「参数 + 结果」两段。
// 折叠前能看到的东西,展开后必须一样能看到:把一次 Edit 的 diff 换成一段
// old_string/new_string 参数属于信息降级。
function ActivityRowBody({
  step,
  facts,
}: {
  step: ActivityStep;
  facts: ReturnType<typeof stepFacts>;
}) {
  const { t } = useTranslation();
  if (step.type === "thinking") {
    return (
      <div className="whitespace-pre-wrap break-words text-aux italic text-muted-foreground">
        {step.block.text ?? ""}
      </div>
    );
  }
  const toolBlock = step.toolBlock;
  const canonical = canonicalOf(toolBlock);
  if (canonical?.kind === "file.edit") {
    const files = canonical.fileEdit?.files ?? [];
    return (
      <div className="min-w-0">
        {files.map((file, fi) => (
          <FileBlock key={fi} file={file} showHeader={files.length > 1} />
        ))}
      </div>
    );
  }
  if (canonical?.kind === "file.write") {
    const write = canonical.fileWrite;
    return (
      <Section label={t("activity.row.sections.content")}>
        {/* 复用 FileWriteCard 今天的正文渲染器(行号 / 横向滚动 / 截断条),
            换成一段裸代码块就丢掉了「这段内容被截断过」和「复制完整内容」。 */}
        <div data-testid="activity-file-write" className="min-w-0">
          <FileWriteContent write={write} />
        </div>
      </Section>
    );
  }
  return <ParamsAndResult step={step} facts={facts} />;
}

function ParamsAndResult({
  step,
  facts,
}: {
  step: ActivityStep;
  facts: ReturnType<typeof stepFacts>;
}) {
  const { t } = useTranslation();
  const toolBlock = step.type === "tool" ? step.toolBlock : undefined;
  const params = React.useMemo(
    () =>
      toolInputEntries(
        toolBlock?.toolInput as Record<string, unknown> | undefined,
      ),
    [toolBlock?.toolInput],
  );
  const hasResult = step.type === "tool" && !!step.resultBlock;
  return (
    <>
      <Section label={t("activity.row.sections.params")}>
        {params.length === 0 ? (
          <div className="text-muted-foreground">
            {t("canonical.raw.emptyParams")}
          </div>
        ) : (
          <CollapsibleCodeParams
            entries={params}
            testIdPrefix={`${step.uiStateKey}:param`}
          />
        )}
      </Section>
      <Section label={t("activity.row.sections.result")}>
        {facts.resultText ? (
          <CollapsibleCode
            value={facts.resultText}
            surface={facts.failed ? "destructive" : "muted"}
            bodyClassName="rounded-sm px-2 py-1.5"
          />
        ) : (
          <div className="text-muted-foreground">
            {hasResult
              ? t("canonical.raw.emptyResult")
              : t("canonical.raw.waitingResult")}
          </div>
        )}
      </Section>
    </>
  );
}

function Section({
  children,
  label,
}: {
  children: React.ReactNode;
  label: string;
}) {
  return (
    <div className="flex min-w-0 flex-col gap-1">
      <div className="flex items-center gap-2">
        <span className="font-sans text-meta font-semibold text-muted-foreground">
          {label}
        </span>
        <span className="h-px min-w-0 flex-1 bg-border" />
      </div>
      <div className="min-w-0 font-mono text-meta">{children}</div>
    </div>
  );
}
