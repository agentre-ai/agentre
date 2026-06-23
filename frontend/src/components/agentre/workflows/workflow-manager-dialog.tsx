import * as React from "react";
import { Check, Pencil, Plus, Route, Search, Trash2, X } from "lucide-react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { useWorkflows, type WorkflowItem } from "@/hooks/use-workflows";
import { cn } from "@/lib/utils";
import { useWorkflowManagerStore } from "@/stores/workflow-manager-store";

import { MarkdownText } from "../markdown-text";
import { WorkflowEditorForm } from "./workflow-editor-form";

// 摘要首行:跳过空行与 markdown 标题行,取第一行正文。
function firstSummaryLine(content: string): string {
  for (const raw of content.split("\n")) {
    const line = raw.trim();
    if (!line || line.startsWith("#")) continue;
    return line;
  }
  return "";
}

type DetailMode = "view" | "editor";

export function WorkflowManagerDialog() {
  const open = useWorkflowManagerStore((s) => s.open);
  const intent = useWorkflowManagerStore((s) => s.intent);
  const close = useWorkflowManagerStore((s) => s.close);
  if (!open) return null;
  return <WorkflowManagerBody intent={intent} onClose={close} />;
}

function WorkflowManagerBody({
  intent,
  onClose,
}: {
  intent: "browse" | "create";
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const { workflows, loading, error, create, update, remove } = useWorkflows();

  const [selectedId, setSelectedId] = React.useState(0);
  const [mode, setMode] = React.useState<DetailMode>("view");
  const [editingId, setEditingId] = React.useState(0);
  const [query, setQuery] = React.useState("");
  const [confirmingDelete, setConfirmingDelete] = React.useState(false);
  const [draftName, setDraftName] = React.useState("");
  const [draftContent, setDraftContent] = React.useState("");
  const [formError, setFormError] = React.useState<string | null>(null);
  const [submitting, setSubmitting] = React.useState(false);

  const started = React.useRef(false);
  React.useEffect(() => {
    if (started.current) return;
    started.current = true;
    if (intent === "create") {
      setMode("editor");
      setEditingId(0);
      setDraftName("");
      setDraftContent("");
      setFormError(null);
    }
  }, [intent]);

  const selected = workflows.find((w) => w.id === selectedId) ?? null;

  const filtered = React.useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return workflows;
    return workflows.filter(
      (w) =>
        w.name.toLowerCase().includes(q) || w.content.toLowerCase().includes(q),
    );
  }, [workflows, query]);

  const openCreate = () => {
    setMode("editor");
    setEditingId(0);
    setDraftName("");
    setDraftContent("");
    setFormError(null);
    setConfirmingDelete(false);
  };
  const openEdit = (w: WorkflowItem) => {
    setMode("editor");
    setEditingId(w.id);
    setDraftName(w.name);
    setDraftContent(w.content);
    setFormError(null);
    setConfirmingDelete(false);
  };
  const cancelEdit = () => {
    setMode("view");
    setFormError(null);
  };

  const canSave = draftName.trim().length > 0 && !submitting;
  const submit = async () => {
    if (!canSave) return;
    setFormError(null);
    setSubmitting(true);
    try {
      if (editingId > 0) {
        await update(editingId, draftName.trim(), draftContent);
        setSelectedId(editingId);
      } else {
        await create(draftName.trim(), draftContent);
        setSelectedId(0);
      }
      setMode("view");
    } catch (err) {
      setFormError(String(err));
    } finally {
      setSubmitting(false);
    }
  };

  const confirmDelete = async () => {
    if (!selected) return;
    // remove 失败不抛(落到 hook 的 error,在列表区展示);仅成功时才清选中/回浏览态,
    // 否则会出现"看着删掉了其实没删"的错位。
    const ok = await remove(selected.id);
    setConfirmingDelete(false);
    if (ok) {
      setSelectedId(0);
      setMode("view");
    }
  };

  const onEditorKeyDown = (e: React.KeyboardEvent) => {
    if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
      e.preventDefault();
      void submit();
    }
  };

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent
        showCloseButton={false}
        data-testid="workflow-manager"
        onEscapeKeyDown={(e) => {
          // 编辑态按 Esc 回到浏览态(取消编辑),不关整个弹窗;浏览态走默认(关弹窗)。
          if (mode === "editor") {
            e.preventDefault();
            cancelEdit();
          }
        }}
        className={cn(
          "flex h-[640px] max-h-[88vh] w-[920px] max-w-[94vw] flex-col gap-0 overflow-hidden p-0",
        )}
      >
        <DialogTitle className="sr-only">{t("workflows.title")}</DialogTitle>
        <DialogDescription className="sr-only">
          {t("workflows.subtitle")}
        </DialogDescription>

        <header className="flex shrink-0 items-center gap-3 border-b border-border bg-muted/40 px-5 py-3.5">
          <span className="flex size-7 shrink-0 items-center justify-center rounded-md bg-primary-soft">
            <Route className="size-4 text-primary-text" aria-hidden="true" />
          </span>
          <div className="flex min-w-0 flex-col">
            <h1 className="text-sm font-semibold text-foreground">
              {t("workflows.title")}
            </h1>
            <p className="truncate text-2xs text-muted-foreground">
              {t("workflows.subtitle")}
            </p>
          </div>
          <div className="flex-1" />
          <Button
            type="button"
            size="sm"
            data-testid="workflow-new-button"
            onClick={openCreate}
          >
            <Plus className="size-3.5" aria-hidden="true" />
            {t("workflows.new")}
          </Button>
          <Button
            type="button"
            variant="ghost"
            size="icon-xs"
            aria-label={t("common.close")}
            onClick={onClose}
          >
            <X className="size-3.5" aria-hidden="true" />
          </Button>
        </header>

        <div className="flex min-h-0 flex-1">
          <aside className="flex w-[300px] shrink-0 flex-col border-r border-border bg-muted/20">
            <div className="border-b border-border p-2.5">
              <div className="flex items-center gap-2 rounded-md border border-border bg-background px-2.5 py-1.5">
                <Search
                  className="size-3.5 shrink-0 text-muted-foreground"
                  aria-hidden="true"
                />
                <Input
                  aria-label={t("workflows.manager.searchPlaceholder")}
                  placeholder={t("workflows.manager.searchPlaceholder")}
                  value={query}
                  onChange={(e) => setQuery(e.target.value)}
                  className="h-auto border-0 bg-transparent p-0 text-xs shadow-none focus-visible:ring-0"
                />
              </div>
            </div>
            <div className="flex-1 overflow-y-auto">
              {error ? (
                <div className="px-4 py-2 text-2xs text-destructive">
                  {error}
                </div>
              ) : null}
              {filtered.length === 0 && !loading && !error ? (
                <div className="px-4 py-8 text-center text-xs text-muted-foreground">
                  {t("workflows.empty")}
                </div>
              ) : null}
              {filtered.map((w) => {
                const summary = firstSummaryLine(w.content);
                const active = selectedId === w.id;
                return (
                  <button
                    key={w.id}
                    type="button"
                    data-testid={`workflow-row-${w.id}`}
                    aria-current={active ? "true" : undefined}
                    onClick={() => {
                      setSelectedId(w.id);
                      setMode("view");
                      setConfirmingDelete(false);
                    }}
                    className={cn(
                      "flex w-full cursor-pointer flex-col gap-1 border-b border-border px-4 py-2.5 text-left",
                      active
                        ? "border-l-[3px] border-l-primary bg-primary-soft"
                        : "hover:bg-accent/50",
                    )}
                  >
                    <div className="flex w-full items-center gap-2">
                      <span className="min-w-0 flex-1 truncate text-xs font-medium text-foreground">
                        {w.name}
                      </span>
                      {w.groupCount > 0 ? (
                        <span className="shrink-0 rounded-full bg-accent px-1.5 py-0.5 text-2xs text-muted-foreground">
                          {t("workflows.groupCount", { count: w.groupCount })}
                        </span>
                      ) : null}
                    </div>
                    {summary ? (
                      <span className="w-full truncate text-2xs text-muted-foreground">
                        {summary}
                      </span>
                    ) : null}
                    <span className="text-2xs text-muted-foreground">
                      {t("workflows.updatedAt", {
                        time: new Date(w.updatetime).toLocaleDateString(),
                      })}
                    </span>
                  </button>
                );
              })}
            </div>
          </aside>

          <section className="flex min-w-0 flex-1 flex-col bg-muted/10">
            {mode === "editor" ? (
              <EditorPane
                editing={editingId > 0}
                name={draftName}
                content={draftContent}
                error={formError}
                canSave={canSave}
                onNameChange={setDraftName}
                onContentChange={setDraftContent}
                onCancel={cancelEdit}
                onSave={() => void submit()}
                onKeyDown={onEditorKeyDown}
              />
            ) : selected ? (
              <ViewPane
                workflow={selected}
                confirmingDelete={confirmingDelete}
                onEdit={() => openEdit(selected)}
                onAskDelete={() => setConfirmingDelete(true)}
                onCancelDelete={() => setConfirmingDelete(false)}
                onConfirmDelete={() => void confirmDelete()}
              />
            ) : (
              <div className="flex flex-1 items-center justify-center text-xs text-muted-foreground">
                {t("workflows.preview.empty")}
              </div>
            )}
          </section>
        </div>
      </DialogContent>
    </Dialog>
  );
}

function ViewPane({
  workflow,
  confirmingDelete,
  onEdit,
  onAskDelete,
  onCancelDelete,
  onConfirmDelete,
}: {
  workflow: WorkflowItem;
  confirmingDelete: boolean;
  onEdit: () => void;
  onAskDelete: () => void;
  onCancelDelete: () => void;
  onConfirmDelete: () => void;
}) {
  const { t } = useTranslation();
  const meta =
    workflow.groupCount > 0
      ? t("workflows.manager.metaLive", {
          count: workflow.groupCount,
          time: new Date(workflow.updatetime).toLocaleDateString(),
        })
      : t("workflows.manager.metaUnused", {
          time: new Date(workflow.updatetime).toLocaleDateString(),
        });
  return (
    <>
      <header className="flex flex-col gap-1 border-b border-border px-5 py-3">
        <div className="flex items-center gap-2.5">
          <span className="flex size-7 shrink-0 items-center justify-center rounded-md bg-primary-soft">
            <Route className="size-4 text-primary-text" aria-hidden="true" />
          </span>
          <h2 className="text-sm font-semibold text-foreground">
            {workflow.name}
          </h2>
        </div>
        <p className="text-2xs text-muted-foreground">{meta}</p>
      </header>
      <div className="flex-1 overflow-y-auto px-5 py-4">
        <MarkdownText text={workflow.content} />
      </div>
      {confirmingDelete ? (
        <DeleteConfirmBar
          workflow={workflow}
          onCancel={onCancelDelete}
          onConfirm={onConfirmDelete}
        />
      ) : (
        <footer className="flex items-center gap-2 border-t border-border px-5 py-3">
          <Button
            type="button"
            size="sm"
            className="flex-1"
            data-testid="workflow-edit-button"
            onClick={onEdit}
          >
            <Pencil className="size-3.5" aria-hidden="true" />
            {t("workflows.edit")}
          </Button>
          <Button
            type="button"
            variant="outline"
            size="icon-sm"
            data-testid="workflow-delete-button"
            aria-label={t("workflows.delete")}
            onClick={onAskDelete}
          >
            <Trash2 className="size-3.5" aria-hidden="true" />
          </Button>
        </footer>
      )}
    </>
  );
}

function DeleteConfirmBar({
  workflow,
  onCancel,
  onConfirm,
}: {
  workflow: WorkflowItem;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  const { t } = useTranslation();
  const desc =
    workflow.groupCount > 0
      ? t("workflows.deleteConfirm.desc", {
          name: workflow.name,
          count: workflow.groupCount,
        })
      : t("workflows.deleteConfirm.descUnused", { name: workflow.name });
  return (
    <div
      data-testid="workflow-delete-confirm"
      className="flex flex-col gap-3 border-t border-destructive/40 bg-destructive-soft px-5 py-3.5"
    >
      <div className="flex items-start gap-2.5">
        <Trash2
          className="mt-0.5 size-4 shrink-0 text-destructive"
          aria-hidden="true"
        />
        <div className="flex flex-col gap-0.5">
          <span className="text-xs font-semibold text-destructive">
            {t("workflows.deleteConfirm.title")}
          </span>
          <span className="text-2xs leading-relaxed text-muted-foreground">
            {desc}
          </span>
        </div>
      </div>
      <div className="flex items-center justify-end gap-2">
        <Button type="button" variant="outline" size="sm" onClick={onCancel}>
          {t("common.cancel")}
        </Button>
        <Button
          type="button"
          variant="destructive"
          size="sm"
          data-testid="workflow-delete-confirm-button"
          onClick={onConfirm}
        >
          {t("workflows.deleteConfirm.confirm")}
        </Button>
      </div>
    </div>
  );
}

function EditorPane({
  editing,
  name,
  content,
  error,
  canSave,
  onNameChange,
  onContentChange,
  onCancel,
  onSave,
  onKeyDown,
}: {
  editing: boolean;
  name: string;
  content: string;
  error: string | null;
  canSave: boolean;
  onNameChange: (v: string) => void;
  onContentChange: (v: string) => void;
  onCancel: () => void;
  onSave: () => void;
  onKeyDown: (e: React.KeyboardEvent) => void;
}) {
  const { t } = useTranslation();
  return (
    <div className="flex min-h-0 flex-1 flex-col" onKeyDown={onKeyDown}>
      <header className="flex items-center gap-2.5 border-b border-border px-5 py-3">
        <span className="flex size-7 shrink-0 items-center justify-center rounded-md bg-primary-soft">
          <Pencil className="size-4 text-primary-text" aria-hidden="true" />
        </span>
        <h2 className="text-sm font-semibold text-foreground">
          {editing
            ? t("workflows.editor.editTitle")
            : t("workflows.editor.createTitle")}
        </h2>
      </header>
      <div className="flex min-h-0 flex-1 flex-col overflow-y-auto px-5 py-4">
        <WorkflowEditorForm
          name={name}
          content={content}
          error={error}
          onNameChange={onNameChange}
          onContentChange={onContentChange}
        />
      </div>
      <footer className="flex items-center gap-2 border-t border-border px-5 py-3">
        <span className="text-2xs text-muted-foreground">
          {t("workflows.manager.saveHint")}
        </span>
        <div className="flex-1" />
        <Button type="button" variant="outline" size="sm" onClick={onCancel}>
          {t("common.cancel")}
        </Button>
        <Button
          type="button"
          size="sm"
          disabled={!canSave}
          data-testid="workflow-save-button"
          onClick={onSave}
        >
          <Check className="size-3.5" aria-hidden="true" />
          {t("workflows.editor.save")}
        </Button>
      </footer>
    </div>
  );
}
