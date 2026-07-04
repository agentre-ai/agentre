import { ArrowDown, ArrowUp, X } from "lucide-react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { cn } from "@/lib/utils";

import type { FlowKind, FlowNode } from "../orchestration/flow-graph";

// bounce「无」的哨兵值:shadcn SelectItem 不允许空串 value。
const BOUNCE_NONE = "__none__";

export function WorkflowNodeForm({
  node,
  index,
  earlier,
  others,
  dependsOn,
  bounce,
  canRemove,
  onLabelChange,
  onKindChange,
  onBriefChange,
  onToggleDependsOn,
  onBounceChange,
  onMoveUp,
  onMoveDown,
  onRemove,
}: {
  node: FlowNode;
  index: number;
  earlier: FlowNode[];
  others: FlowNode[];
  dependsOn: string[];
  bounce: string | null;
  canRemove: boolean;
  onLabelChange: (v: string) => void;
  onKindChange: (v: FlowKind) => void;
  onBriefChange: (v: string) => void;
  onToggleDependsOn: (depId: string) => void;
  onBounceChange: (target: string | null) => void;
  onMoveUp: () => void;
  onMoveDown: () => void;
  onRemove: () => void;
}) {
  const { t } = useTranslation();
  return (
    <div className="flex flex-col gap-2 rounded-md border border-border bg-card px-3 py-2.5">
      <div className="flex items-center gap-2">
        <span className="flex size-5 shrink-0 items-center justify-center rounded bg-accent text-2xs text-muted-foreground">
          {index + 1}
        </span>
        <Input
          data-testid={`node-${node.id}-label`}
          aria-label={t("workflows.designer.nodeLabel")}
          value={node.label}
          onChange={(e) => onLabelChange(e.target.value)}
          placeholder={t("workflows.designer.nodeLabelPlaceholder")}
          className="h-8 flex-1 text-xs"
        />
        <Button
          type="button"
          variant="ghost"
          size="icon-xs"
          data-testid={`node-${node.id}-up`}
          aria-label={t("workflows.editor.moveUp")}
          onClick={onMoveUp}
        >
          <ArrowUp className="size-3" aria-hidden="true" />
        </Button>
        <Button
          type="button"
          variant="ghost"
          size="icon-xs"
          data-testid={`node-${node.id}-down`}
          aria-label={t("workflows.editor.moveDown")}
          onClick={onMoveDown}
        >
          <ArrowDown className="size-3" aria-hidden="true" />
        </Button>
        <Button
          type="button"
          variant="ghost"
          size="icon-xs"
          disabled={!canRemove}
          data-testid={`node-${node.id}-remove`}
          aria-label={t("workflows.editor.removeItem")}
          onClick={onRemove}
        >
          <X className="size-3" aria-hidden="true" />
        </Button>
      </div>

      <div className="flex items-center gap-2">
        <span className="w-16 shrink-0 text-2xs text-muted-foreground">
          {t("workflows.designer.nodeKind")}
        </span>
        <Select value={node.kind} onValueChange={(v) => onKindChange(v as FlowKind)}>
          <SelectTrigger
            data-testid={`node-${node.id}-kind`}
            className="h-7 flex-1 text-2xs"
          >
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="task">{t("workflows.designer.kindTask")}</SelectItem>
            <SelectItem value="leader">{t("workflows.designer.kindLeader")}</SelectItem>
          </SelectContent>
        </Select>
      </div>

      {node.kind === "task" ? (
        <Textarea
          data-testid={`node-${node.id}-brief`}
          aria-label={t("workflows.designer.nodeBrief")}
          value={node.brief ?? ""}
          onChange={(e) => onBriefChange(e.target.value)}
          placeholder={t("workflows.designer.nodeBriefPlaceholder")}
          className="min-h-16 resize-none text-2xs"
        />
      ) : null}

      {earlier.length > 0 ? (
        <div className="flex flex-col gap-1">
          <span className="text-2xs text-muted-foreground">
            {t("workflows.designer.dependsOn")}
          </span>
          <div className="flex flex-wrap gap-1.5">
            {earlier.map((dep) => {
              const on = dependsOn.includes(dep.id);
              return (
                <button
                  key={dep.id}
                  type="button"
                  data-testid={`node-${node.id}-dep-${dep.id}`}
                  aria-pressed={on}
                  onClick={() => onToggleDependsOn(dep.id)}
                  className={cn(
                    "rounded border px-1.5 py-0.5 text-2xs",
                    on
                      ? "border-primary bg-primary-soft text-primary-text"
                      : "border-border bg-background text-muted-foreground hover:bg-accent/50",
                  )}
                >
                  {dep.label || dep.id}
                </button>
              );
            })}
          </div>
          <span className="text-2xs text-subtle-foreground">
            {t("workflows.designer.dependsOnHint")}
          </span>
        </div>
      ) : null}

      {others.length > 0 ? (
        <div className="flex items-center gap-2">
          <span className="w-16 shrink-0 text-2xs text-muted-foreground">
            {t("workflows.designer.bounce")}
          </span>
          <Select
            value={bounce ?? BOUNCE_NONE}
            onValueChange={(v) => onBounceChange(v === BOUNCE_NONE ? null : v)}
          >
            <SelectTrigger
              data-testid={`node-${node.id}-bounce`}
              className="h-7 flex-1 text-2xs"
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={BOUNCE_NONE}>
                {t("workflows.designer.bounceNone")}
              </SelectItem>
              {others.map((o) => (
                <SelectItem key={o.id} value={o.id}>
                  {o.label || o.id}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      ) : null}
    </div>
  );
}
