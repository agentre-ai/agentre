import * as React from "react";
import { useNavigate } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

import { useOrchRunStore } from "../../stores/orch-run-store";

// OrchNotifier 常驻 App 根、不渲染任何 UI；检测编排 Run 完成和等待你状态并弹 toast。
export function OrchNotifier(): null {
  const { t } = useTranslation();
  const navigate = useNavigate();

  React.useEffect(() => {
    // 初始化 prev-status 映射，避免首次加载时对已有 done/awaiting 误报。
    const prevStatus = new Map<number, string>();
    const notifiedAwaitingTaskIds = new Set<number>();

    for (const [runId, detail] of useOrchRunStore.getState().details) {
      if (detail.run) {
        prevStatus.set(runId, detail.run.status);
      }
      for (const task of detail.tasks ?? []) {
        if (task.status === "awaiting-user") {
          notifiedAwaitingTaskIds.add(task.id);
        }
      }
    }

    const unsub = useOrchRunStore.subscribe((state) => {
      for (const [runId, detail] of state.details) {
        const currentStatus = detail.run?.status;
        const prevSt = prevStatus.get(runId);

        // Run 完成通知：从非 done 转为 done 时触发。
        if (currentStatus === "done" && prevSt !== "done") {
          const goal = detail.run?.goal ?? "";
          toast.success(t("orchestration.notify.completed", { goal }), {
            action: {
              label: t("orchestration.notify.view"),
              onClick: () => navigate(`/orchestration/${runId}`),
            },
          });
        }

        if (currentStatus !== undefined) {
          prevStatus.set(runId, currentStatus);
        }

        // 等待你通知：任务首次进入 awaiting-user 时触发，去重。
        for (const task of detail.tasks ?? []) {
          if (
            task.status === "awaiting-user" &&
            !notifiedAwaitingTaskIds.has(task.id)
          ) {
            notifiedAwaitingTaskIds.add(task.id);
            const goal = detail.run?.goal ?? "";
            toast(t("orchestration.notify.waitingYou", { goal }), {
              action: {
                label: t("orchestration.notify.view"),
                onClick: () => navigate(`/orchestration/${runId}`),
              },
            });
          }
        }
      }
    });

    return unsub;
  }, [t, navigate]);

  return null;
}
