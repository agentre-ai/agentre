import * as React from "react";
import { Braces, Plus } from "lucide-react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { cn } from "@/lib/utils";

import type { FlowGraph, FlowKind } from "../orchestration/flow-graph";
import { FlowGraphView } from "../orchestration/flow-graph-view";
import { MarkdownText } from "../markdown-text";
import { WorkflowPreviewGraph } from "../../../../wailsjs/go/app/App";
import {
  addNode,
  earlierNodeIds,
  graphToJSON,
  moveNode,
  nodeBounce,
  nodeDependsOn,
  removeNode,
  setBounce,
  setDependsOn,
  updateNode,
} from "./flow-graph-draft";
import { WorkflowNodeForm } from "./workflow-node-form";

const DAG_TOKEN = "{{ DAGPrompt }}";

export function WorkflowDagDesigner({
  name,
  graph,
  template,
  error,
  onNameChange,
  onGraphChange,
  onTemplateChange,
  onTemplateError,
}: {
  name: string;
  graph: FlowGraph;
  template: string;
  error: string | null;
  onNameChange: (v: string) => void;
  onGraphChange: (g: FlowGraph) => void;
  onTemplateChange: (v: string) => void;
  onTemplateError: (hasError: boolean) => void;
}) {
  const { t } = useTranslation();
  const [preview, setPreview] = React.useState("");
  const [previewError, setPreviewError] = React.useState("");
  const [tab, setTab] = React.useState<"edit" | "preview">("edit");
  const taRef = React.useRef<HTMLTextAreaElement>(null);

  // graphJSON 作为依赖: graph 变化才重新预览(结构比较), 250ms 防抖。
  // useMemo 避免无关 re-render(如 preview state 更新)时反复 stringify 整图。
  const graphJSON = React.useMemo(() => graphToJSON(graph), [graph]);
  React.useEffect(() => {
    let alive = true;
    const timer = setTimeout(() => {
      WorkflowPreviewGraph({ name, graph: graphJSON, template })
        .then((resp) => {
          if (!alive) return;
          const err = resp?.error ?? "";
          setPreviewError(err);
          setPreview(err ? "" : (resp?.content ?? ""));
          onTemplateError(!!err);
        })
        .catch(() => {
          if (!alive) return;
          setPreviewError("");
          setPreview("");
          onTemplateError(false);
        });
    }, 250);
    return () => {
      alive = false;
      clearTimeout(timer);
    };
  }, [name, graphJSON, template, onTemplateError]);

  const insertToken = () => {
    const el = taRef.current;
    if (!el) {
      onTemplateChange(template + DAG_TOKEN);
      return;
    }
    const s = el.selectionStart ?? template.length;
    const e = el.selectionEnd ?? template.length;
    onTemplateChange(template.slice(0, s) + DAG_TOKEN + template.slice(e));
  };

  const nodeById = React.useMemo(
    () => new Map(graph.nodes.map((n) => [n.id, n])),
    [graph],
  );
  const earlierNodes = (id: string) =>
    earlierNodeIds(graph, id)
      .map((eid) => nodeById.get(eid))
      .filter((n): n is NonNullable<typeof n> => n != null);
  const otherNodes = (id: string) => graph.nodes.filter((n) => n.id !== id);

  const toggleDep = (id: string, depId: string) => {
    const cur = nodeDependsOn(graph, id);
    const next = cur.includes(depId)
      ? cur.filter((d) => d !== depId)
      : [...cur, depId];
    onGraphChange(setDependsOn(graph, id, next));
  };

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-3">
      <div className="flex min-h-0 flex-1 gap-4">
        {/* 左栏: 名称 + 节点表单 + 添加 */}
        <div className="flex w-[360px] shrink-0 flex-col gap-3 overflow-y-auto pr-1">
          <label className="flex flex-col gap-1.5 text-xs">
            <span className="font-medium text-foreground">
              {t("workflows.editor.name")}
              <span className="ml-0.5 text-destructive">*</span>
            </span>
            <Input
              data-testid="workflow-name-input"
              aria-label={t("workflows.editor.name")}
              value={name}
              onChange={(e) => onNameChange(e.target.value)}
              placeholder={t("workflows.editor.namePlaceholder")}
              className="h-9 text-xs"
            />
          </label>

          <div className="flex flex-col gap-2.5">
            {graph.nodes.map((node, i) => (
              <WorkflowNodeForm
                key={node.id}
                node={node}
                index={i}
                earlier={earlierNodes(node.id)}
                others={otherNodes(node.id)}
                dependsOn={nodeDependsOn(graph, node.id)}
                bounce={nodeBounce(graph, node.id)}
                canRemove={graph.nodes.length > 1}
                onLabelChange={(v) =>
                  onGraphChange(updateNode(graph, node.id, { label: v }))
                }
                onKindChange={(v: FlowKind) =>
                  onGraphChange(updateNode(graph, node.id, { kind: v }))
                }
                onBriefChange={(v) =>
                  onGraphChange(updateNode(graph, node.id, { brief: v }))
                }
                onToggleDependsOn={(depId) => toggleDep(node.id, depId)}
                onBounceChange={(target) =>
                  onGraphChange(setBounce(graph, node.id, target))
                }
                onMoveUp={() => onGraphChange(moveNode(graph, node.id, -1))}
                onMoveDown={() => onGraphChange(moveNode(graph, node.id, 1))}
                onRemove={() => onGraphChange(removeNode(graph, node.id))}
              />
            ))}
          </div>

          <Button
            type="button"
            variant="outline"
            size="sm"
            data-testid="designer-add-node"
            onClick={() => onGraphChange(addNode(graph))}
          >
            <Plus className="size-3.5" aria-hidden="true" />
            {t("workflows.designer.addNode")}
          </Button>
        </div>

        {/* 右栏: 上 DAG + 下模板 pane */}
        <div className="flex min-w-0 flex-1 flex-col gap-3">
          <div className="flex flex-col gap-1.5">
            <span className="text-2xs text-subtle-foreground">
              {t("workflows.designer.graphTitle")}
            </span>
            <FlowGraphView graph={graph} />
          </div>
          <div className="flex min-h-0 flex-1 flex-col gap-1.5">
            <div className="flex items-center gap-2">
              <span className="text-2xs font-medium text-foreground">
                {t("workflows.designer.templateTitle")}
              </span>
              <span className="rounded bg-primary-soft px-1.5 py-0.5 text-2xs font-medium text-primary-text">
                {t("workflows.designer.editableBadge")}
              </span>
              <div className="flex-1" />
              <div className="flex items-center gap-0.5 rounded-md bg-secondary p-0.5">
                <button
                  type="button"
                  data-testid="designer-tab-edit"
                  onClick={() => setTab("edit")}
                  className={cn(
                    "rounded px-2.5 py-0.5 text-2xs",
                    tab === "edit"
                      ? "bg-card font-medium text-foreground shadow-sm"
                      : "text-muted-foreground",
                  )}
                >
                  {t("workflows.designer.tabEdit")}
                </button>
                <button
                  type="button"
                  data-testid="designer-tab-preview"
                  onClick={() => setTab("preview")}
                  className={cn(
                    "rounded px-2.5 py-0.5 text-2xs",
                    tab === "preview"
                      ? "bg-card font-medium text-foreground shadow-sm"
                      : "text-muted-foreground",
                  )}
                >
                  {t("workflows.designer.tabPreview")}
                </button>
              </div>
            </div>

            <div className="flex items-center gap-2">
              <Button
                type="button"
                variant="secondary"
                size="sm"
                data-testid="designer-insert-token"
                onClick={insertToken}
              >
                <Braces className="size-3.5" aria-hidden="true" />
                {t("workflows.designer.insertToken")}{" "}
                <code className="ml-1 font-mono">{DAG_TOKEN}</code>
              </Button>
              <span className="truncate text-2xs text-muted-foreground">
                {t("workflows.designer.tokenHint", { token: DAG_TOKEN })}
              </span>
            </div>

            {tab === "edit" ? (
              <Textarea
                ref={taRef}
                data-testid="designer-template-input"
                aria-label={t("workflows.designer.templateTitle")}
                value={template}
                onChange={(e) => onTemplateChange(e.target.value)}
                placeholder={t("workflows.designer.templatePlaceholder", {
                  token: DAG_TOKEN,
                })}
                className="min-h-0 flex-1 resize-none font-mono text-xs"
              />
            ) : previewError ? (
              <div
                data-testid="designer-template-error"
                data-selectable-text="true"
                className="min-h-0 flex-1 overflow-y-auto rounded-lg border border-destructive bg-destructive-soft px-3 py-2 text-2xs text-destructive"
              >
                {t("workflows.designer.templateErrorLabel")}
                {previewError}
              </div>
            ) : (
              <div
                data-testid="designer-prompt-preview"
                data-selectable-text="true"
                className="min-h-0 flex-1 overflow-y-auto rounded-lg border border-border bg-card/40 px-3 py-2"
              >
                {preview.trim() ? (
                  <MarkdownText text={preview} />
                ) : (
                  <span className="text-2xs text-muted-foreground">
                    {t("workflows.designer.previewEmpty")}
                  </span>
                )}
              </div>
            )}

            <span className="text-2xs text-muted-foreground">
              {t("workflows.designer.templateHint", { token: DAG_TOKEN })}
            </span>
          </div>
        </div>
      </div>

      {error ? (
        <div className="rounded-md border border-destructive bg-destructive-soft px-3 py-2 text-2xs text-destructive">
          {error}
        </div>
      ) : null}
    </div>
  );
}
