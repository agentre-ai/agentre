import { useEffect } from "react";

import {
  useProjectListStore,
  type ProjectFlat,
  flattenProjects,
} from "@/stores/project-list-store";

export type { ProjectFlat };
export { flattenProjects };

// useProjectList 是 project-list-store 的薄包装: 订阅 store 字段 + 首次 mount
// 触发 reload。所有调用方 (ChatComposer mention 菜单 / issues-page / 命令面板)
// 共享同一份 projects 数据, reload 在 store 内做并发去重, 多个组件同时 mount
// (例如 chat-panel-host 把每个已打开 tab 都常驻挂载) 也只跑一次 ProjectListTree。
export function useProjectList() {
  const projects = useProjectListStore((s) => s.projects);
  const loading = useProjectListStore((s) => s.loading);
  const error = useProjectListStore((s) => s.error);
  const reload = useProjectListStore((s) => s.reload);

  useEffect(() => {
    void reload();
  }, [reload]);

  return { projects, loading, error, reload };
}
