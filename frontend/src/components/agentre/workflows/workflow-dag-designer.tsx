import * as React from "react";
import { Plus } from "lucide-react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";

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

export function WorkflowDagDesigner({
  name,
  graph,
  error,
  onNameChange,
  onGraphChange,
}: {
  name: string;
  graph: FlowGraph;
  error: string | null;
  onNameChange: (v: string) => void;
  onGraphChange: (g: FlowGraph) => void;
}) {
  const { t } = useTranslation();
  const [preview, setPreview] = React.useState("");

  // graphJSON 作为依赖: graph 变化才重新预览(结构比较), 250ms 防抖。
  // useMemo 避免无关 re-render(如 preview state 更新)时反复 stringify 整图。
  const graphJSON = React.useMemo(() => graphToJSON(graph), [graph]);
  React.useEffect(() => {
    let alive = true;
    const timer = setTimeout(() => {
      WorkflowPreviewGraph({ name, graph: graphJSON })
        .then((resp) => {
          if (alive) setPreview(resp?.content ?? "");
        })
        .catch(() => {
          if (alive) setPreview("");
        });
    }, 250);
    return () => {
      alive = false;
      clearTimeout(timer);
    };
  }, [name, graphJSON]);

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

        {/* 右栏: 上 DAG + 下实时提示词 */}
        <div className="flex min-w-0 flex-1 flex-col gap-3">
          <div className="flex flex-col gap-1.5">
            <span className="text-2xs text-subtle-foreground">
              {t("workflows.designer.graphTitle")}
            </span>
            <FlowGraphView graph={graph} />
          </div>
          <div className="flex min-h-0 flex-1 flex-col gap-1.5">
            <span className="text-2xs text-subtle-foreground">
              {t("workflows.designer.previewTitle")}
            </span>
            <div
              data-testid="designer-prompt-preview"
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
