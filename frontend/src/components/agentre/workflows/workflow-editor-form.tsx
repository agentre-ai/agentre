import * as React from "react";
import { X } from "lucide-react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";

export type WorkflowEditorFormProps = {
  name: string;
  content: string;
  /** Display-only tags (chips). */
  tags: string[];
  error: string | null;
  onNameChange: (v: string) => void;
  onContentChange: (v: string) => void;
  /** Called with the new tags array when the user adds or removes a tag. */
  onTagsChange: (v: string[]) => void;
};

// 受控编辑表单:名称 + 标签(chips) + 正文(Markdown)。
// 标签是给人看的展示层,绝不注入 AI(见 hint)。提交由宿主统一管理。
export function WorkflowEditorForm({
  name,
  content,
  tags,
  error,
  onNameChange,
  onContentChange,
  onTagsChange,
}: WorkflowEditorFormProps) {
  const { t } = useTranslation();
  const [tagDraft, setTagDraft] = React.useState("");

  // 骨架模板:正文非空时追加到末尾不覆盖。
  const insertTemplate = () => {
    const tpl = t("workflows.editor.template");
    onContentChange(content.trim() ? `${content.trimEnd()}\n\n${tpl}` : tpl);
  };

  const addTag = () => {
    const v = tagDraft.trim();
    if (!v || tags.includes(v)) {
      setTagDraft("");
      return;
    }
    onTagsChange([...tags, v]);
    setTagDraft("");
  };

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-3.5">
      {/* 名称 */}
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

      {/* 标签(chips) */}
      <div className="flex flex-col gap-1.5 text-xs">
        <span className="font-medium text-foreground">
          {t("workflows.editor.tags")}
        </span>
        <div className="flex flex-wrap items-center gap-1.5">
          {tags.map((tag, i) => (
            <span
              key={`${tag}-${i}`}
              className="flex items-center gap-1 rounded bg-accent px-1.5 py-0.5 text-foreground"
            >
              {tag}
              <button
                type="button"
                data-testid={`workflow-tag-remove-${i}`}
                aria-label={t("workflows.editor.removeItem")}
                onClick={() => onTagsChange(tags.filter((_, k) => k !== i))}
              >
                <X
                  className="size-3 text-muted-foreground"
                  aria-hidden="true"
                />
              </button>
            </span>
          ))}
          <Input
            data-testid="workflow-tags-input"
            aria-label={t("workflows.editor.tags")}
            value={tagDraft}
            onChange={(e) => setTagDraft(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                addTag();
              }
            }}
            placeholder={t("workflows.editor.tagsPlaceholder")}
            className="h-7 w-40 text-2xs"
          />
        </div>
        <span
          data-testid="workflow-tags-hint"
          className="text-2xs text-muted-foreground"
        >
          {t("workflows.editor.tagsHint")}
        </span>
      </div>

      {/* 正文(Markdown) */}
      <div className="flex min-h-0 flex-1 flex-col gap-1.5 text-xs">
        <span className="flex items-center justify-between font-medium text-foreground">
          <span>{t("workflows.editor.content")}</span>
          <Button
            type="button"
            variant="link"
            size="sm"
            data-testid="workflow-insert-template-button"
            className="h-auto p-0 text-2xs"
            onClick={insertTemplate}
          >
            {t("workflows.editor.insertTemplate")}
          </Button>
        </span>
        <Textarea
          data-testid="workflow-content-input"
          aria-label={t("workflows.editor.content")}
          value={content}
          onChange={(e) => onContentChange(e.target.value)}
          className="min-h-0 flex-1 resize-none font-mono text-xs"
        />
      </div>

      {error ? (
        <div className="rounded-md border border-destructive bg-destructive-soft px-3 py-2 text-2xs text-destructive">
          {error}
        </div>
      ) : null}
    </div>
  );
}
