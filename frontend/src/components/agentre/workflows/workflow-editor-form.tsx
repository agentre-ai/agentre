import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";

export type WorkflowEditorFormProps = {
  name: string;
  content: string;
  error: string | null;
  onNameChange: (v: string) => void;
  onContentChange: (v: string) => void;
};

// 受控编辑表单:名称 + 正文(Markdown) + 「插入骨架模板」。
// 不含提交按钮/弹窗壳 —— 提交与开关由宿主(管理弹窗右栏)统一管理。
export function WorkflowEditorForm({
  name,
  content,
  error,
  onNameChange,
  onContentChange,
}: WorkflowEditorFormProps) {
  const { t } = useTranslation();

  // 骨架模板:正文非空时追加到末尾不覆盖。
  const insertTemplate = () => {
    const tpl = t("workflows.editor.template");
    onContentChange(content.trim() ? `${content.trimEnd()}\n\n${tpl}` : tpl);
  };

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-3.5">
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
