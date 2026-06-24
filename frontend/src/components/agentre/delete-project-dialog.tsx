import * as React from "react";
import { Loader2, Trash2 } from "lucide-react";
import { useTranslation } from "react-i18next";

import { ProjectDelete } from "../../../wailsjs/go/app/App";
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

export type DeleteProjectTarget = { id: number; name: string };

/**
 * DeleteProjectDialog —— 项目删除二次确认弹窗。
 * 与设置抽屉 Danger 页同样要求输入完整项目名才放行（防误删），
 * 但以独立 Dialog 形式直接挂在项目列表上，供卡片 ⋮ 菜单一键唤起。
 */
export function DeleteProjectDialog({
  target,
  onClose,
  onDeleted,
}: {
  target: DeleteProjectTarget | null;
  onClose: () => void;
  onDeleted: () => void;
}) {
  const { t } = useTranslation();
  const [confirm, setConfirm] = React.useState("");
  const [err, setErr] = React.useState<string | null>(null);
  const [busy, setBusy] = React.useState(false);

  // 每次切换目标项目时重置确认输入 / 错误 / 忙碌态。
  React.useEffect(() => {
    setConfirm("");
    setErr(null);
    setBusy(false);
  }, [target?.id]);

  const canDelete = target !== null && confirm.trim() === target.name && !busy;

  const handleDelete = async () => {
    if (target === null) return;
    setErr(null);
    setBusy(true);
    try {
      await ProjectDelete(target.id);
      onDeleted();
    } catch (e) {
      setErr(String(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog
      open={target !== null}
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t("projectSettings.danger.deleteProject")}</DialogTitle>
        </DialogHeader>
        <DialogBody className="flex flex-col gap-3">
          <p className="text-2xs text-muted-foreground">
            {t("projectSettings.danger.deleteDescription")}
          </p>
          {target !== null ? (
            <label className="flex flex-col gap-1.5 text-xs">
              <span className="font-medium text-foreground">
                {t("projectSettings.danger.confirmName", { name: target.name })}
              </span>
              <Input
                autoFocus
                value={confirm}
                onChange={(e) => setConfirm(e.target.value)}
                className="h-9 font-mono text-xs"
                placeholder={target.name}
              />
            </label>
          ) : null}
          {err ? (
            <div className="rounded-md border border-destructive bg-destructive-soft px-3 py-2 text-2xs text-destructive">
              {err}
            </div>
          ) : null}
        </DialogBody>
        <DialogFooter>
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={busy}
            onClick={onClose}
          >
            {t("common.cancel")}
          </Button>
          <Button
            type="button"
            variant="destructive"
            size="sm"
            disabled={!canDelete}
            onClick={() => void handleDelete()}
          >
            {busy ? (
              <Loader2 className="size-3.5 animate-spin" aria-hidden="true" />
            ) : (
              <Trash2 className="size-3.5" aria-hidden="true" />
            )}
            {t("projectSettings.danger.deleteProject")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
