import { useEffect } from "react";

import {
  useGroupListStore,
  type GroupListItem,
} from "@/stores/group-list-store";

export type { GroupListItem };

// useGroupList 是 group-list-store 的薄包装:订阅 store 字段 + 首次 mount 触发
// reload。与 useChatAgents 同构,所有调用方共享同一份群列表,reload 在 store 内
// 做并发去重。MVP 只驱动 sidebar 群聊分区;需要实时刷新时调用方显式 reload()。
export function useGroupList() {
  const groups = useGroupListStore((s) => s.groups);
  const loading = useGroupListStore((s) => s.loading);
  const error = useGroupListStore((s) => s.error);
  const reload = useGroupListStore((s) => s.reload);

  useEffect(() => {
    void reload();
  }, [reload]);

  return { groups, loading, error, reload };
}
