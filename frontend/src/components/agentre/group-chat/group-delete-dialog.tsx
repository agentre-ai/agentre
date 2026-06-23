import * as React from "react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";

import { AgentreDialog } from "../app-dialog";

export type GroupDeleteDialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** 确认删除时回调,deleteSessions 表示是否一并删除关联的成员会话。 */
  onConfirm: (deleteSessions: boolean) => void;
};

// 群删除确认弹层:复选框默认不勾选(保留会话),确认时把勾选值回传给 onConfirm。
function GroupDeleteDialog({
  open,
  onOpenChange,
  onConfirm,
}: GroupDeleteDialogProps) {
  const { t } = useTranslation();
  const [deleteSessions, setDeleteSessions] = React.useState(false);

  // 每次打开都重置为默认(不勾选),避免上次的勾选状态残留。
  React.useEffect(() => {
    if (open) setDeleteSessions(false);
  }, [open]);

  return (
    <AgentreDialog
      open={open}
      onOpenChange={onOpenChange}
      title={t("group.delete.title")}
      description={t("group.delete.description")}
      footer={
        <>
          <Button
            type="button"
            variant="ghost"
            onClick={() => onOpenChange(false)}
          >
            {t("common.cancel")}
          </Button>
          <Button
            type="button"
            variant="destructive"
            onClick={() => {
              onConfirm(deleteSessions);
              onOpenChange(false);
            }}
          >
            {t("group.delete.confirm")}
          </Button>
        </>
      }
    >
      <label className="flex items-center gap-2 text-sm text-foreground">
        <Checkbox
          checked={deleteSessions}
          onCheckedChange={(v) => setDeleteSessions(v === true)}
        />
        {t("group.delete.alsoSessions")}
      </label>
    </AgentreDialog>
  );
}

export { GroupDeleteDialog };
