import * as React from "react";
import { Loader2 } from "lucide-react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
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

import { toneClass } from "./issue-tones";
import type { ProjectFlat } from "@/hooks/use-project-list";
import { IssueCreate, IssueUpdate } from "../../../wailsjs/go/app/App";
import type { app } from "../../../wailsjs/go/models";

export type IssueNewDialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  projects: ProjectFlat[];
  labels: app.LabelItem[];
  editing?: app.IssueItem | null;
  onSaved: () => void;
};

function IssueNewDialog({
  open,
  onOpenChange,
  projects,
  labels,
  editing,
  onSaved,
}: IssueNewDialogProps) {
  const { t } = useTranslation();
  const [title, setTitle] = React.useState("");
  const [body, setBody] = React.useState("");
  const [projectID, setProjectID] = React.useState(0);
  const [labelIDs, setLabelIDs] = React.useState<number[]>([]);
  const [submitError, setSubmitError] = React.useState<string | null>(null);
  const [submitting, setSubmitting] = React.useState(false);

  // 每次重开弹窗时根据 editing 重置表单 —— 新建是空表单，编辑回填现有值。
  React.useEffect(() => {
    if (!open) {
      return;
    }
    setTitle(editing?.title ?? "");
    setBody(editing?.body ?? "");
    setProjectID(editing?.projectID ?? 0);
    setLabelIDs((editing?.labels ?? []).map((l) => l.id));
    setSubmitError(null);
  }, [open, editing]);

  const toggleLabel = (id: number) => {
    setLabelIDs((ids) =>
      ids.includes(id) ? ids.filter((x) => x !== id) : [...ids, id],
    );
  };

  const canSubmit = title.trim().length > 0 && !submitting;

  const handleSubmit = async () => {
    setSubmitError(null);
    setSubmitting(true);
    try {
      if (editing) {
        await IssueUpdate({
          id: editing.id,
          projectID,
          title: title.trim(),
          body: body.trim(),
          labelIDs,
        });
      } else {
        await IssueCreate({
          projectID,
          title: title.trim(),
          body: body.trim(),
          labelIDs,
        });
      }
      onSaved();
      onOpenChange(false);
    } catch (err) {
      setSubmitError(String(err));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-[520px]">
        <DialogHeader>
          <DialogTitle>
            {editing ? t("issues.dialog.editTitle") : t("issues.dialog.title")}
          </DialogTitle>
        </DialogHeader>
        <DialogBody className="flex flex-col gap-3.5">
          <label className="flex flex-col gap-1.5 text-xs">
            <span className="font-medium text-foreground">
              {t("issues.dialog.titleLabel")}
              <span className="ml-0.5 text-destructive">*</span>
            </span>
            <Input
              aria-label={t("issues.dialog.titleLabel")}
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              className="h-9 text-xs"
            />
          </label>
          <label className="flex flex-col gap-1.5 text-xs">
            <span className="font-medium text-foreground">
              {t("issues.dialog.bodyLabel")}
            </span>
            <Textarea
              value={body}
              onChange={(e) => setBody(e.target.value)}
              className="min-h-[88px] text-xs"
            />
          </label>
          <label className="flex flex-col gap-1.5 text-xs">
            <span className="font-medium text-foreground">
              {t("issues.dialog.projectLabel")}
            </span>
            <Select
              value={String(projectID)}
              onValueChange={(v) => setProjectID(Number(v))}
            >
              <SelectTrigger className="h-9 text-xs">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="0">
                  {t("issues.dialog.noProject")}
                </SelectItem>
                {projects.map((p) => (
                  <SelectItem key={p.id} value={String(p.id)}>
                    {p.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </label>
          <div className="flex flex-col gap-1.5 text-xs">
            <span className="font-medium text-foreground">
              {t("issues.dialog.labelsLabel")}
            </span>
            <div className="flex flex-wrap gap-1.5">
              {labels.map((l) => {
                const selected = labelIDs.includes(l.id);
                return (
                  <button
                    type="button"
                    key={l.id}
                    aria-pressed={selected}
                    onClick={() => toggleLabel(l.id)}
                    className={cn(
                      "cursor-pointer rounded-full border px-2 py-px font-mono text-2xs font-semibold transition-colors",
                      selected
                        ? cn(toneClass(l.tone), "border-transparent")
                        : "border-border text-muted-foreground hover:bg-accent",
                    )}
                  >
                    {l.name}
                  </button>
                );
              })}
            </div>
          </div>
          {submitError ? (
            <div className="rounded-md border border-destructive bg-destructive-soft px-3 py-2 text-2xs text-destructive">
              {submitError}
            </div>
          ) : null}
        </DialogBody>
        <DialogFooter>
          <Button
            type="button"
            variant="ghost"
            onClick={() => onOpenChange(false)}
            disabled={submitting}
          >
            {t("common.cancel")}
          </Button>
          <Button
            type="button"
            disabled={!canSubmit}
            onClick={() => void handleSubmit()}
          >
            {submitting ? (
              <Loader2 className="size-3.5 animate-spin" aria-hidden="true" />
            ) : null}
            {editing ? t("issues.dialog.save") : t("issues.dialog.create")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export { IssueNewDialog };
