import * as React from "react";
import { Route } from "lucide-react";
import { useTranslation } from "react-i18next";

import { useWorkflows } from "@/hooks/use-workflows";

// Run 流程蓝图参考带:给人看的、与实时执行解耦的「这条 Run 套用的流程概览」。
// 读 flowId → 流程 outline,纯展示;不读注入、不读任务状态。flowId=0 或找不到 → null。
export function RunFlowBlueprint({ flowId }: { flowId: number }) {
  const { t } = useTranslation();
  const { workflows } = useWorkflows();
  const flow = flowId > 0 ? workflows.find((w) => w.id === flowId) : undefined;
  if (!flow) return null;

  return (
    <div
      data-testid="run-flow-blueprint"
      className="flex items-center gap-2.5 border-b border-border bg-muted/40 px-4 py-2"
    >
      <Route className="size-3.5 shrink-0 text-subtle-foreground" aria-hidden="true" />
      <span className="text-2xs text-subtle-foreground">
        {t("orchestration.run.blueprintTitle")}
      </span>
      <span className="text-xs font-medium text-foreground">{flow.name}</span>
      {flow.outline.length > 0 ? (
        <span className="flex flex-wrap items-center gap-1">
          {flow.outline.map((step, i) => (
            <React.Fragment key={`${step}-${i}`}>
              {i > 0 ? (
                <span className="text-2xs text-subtle-foreground">›</span>
              ) : null}
              <span className="rounded border border-border bg-card px-1.5 py-0.5 text-2xs text-muted-foreground">
                {step}
              </span>
            </React.Fragment>
          ))}
        </span>
      ) : null}
      <span className="ml-auto shrink-0 rounded-full bg-accent px-2 py-0.5 text-2xs text-muted-foreground">
        {t("orchestration.run.blueprintRef")}
      </span>
    </div>
  );
}
