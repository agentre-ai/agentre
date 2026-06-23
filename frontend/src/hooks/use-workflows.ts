import { useCallback, useEffect, useState } from "react";

import {
  WorkflowCreate,
  WorkflowDelete,
  WorkflowList,
  WorkflowUpdate,
} from "../../wailsjs/go/app/App";

// 平铺投影:页面/弹窗只需要这些字段,避免直接耦合 wails models 类。
export type WorkflowItem = {
  id: number;
  name: string;
  content: string;
  groupCount: number;
  createtime: number;
  updatetime: number;
};

export function useWorkflows() {
  const [workflows, setWorkflows] = useState<WorkflowItem[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const reload = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const resp = await WorkflowList();
      setWorkflows(
        (resp?.items ?? []).map((i) => ({
          id: i.id,
          name: i.name,
          content: i.content,
          groupCount: i.groupCount,
          createtime: i.createtime,
          updatetime: i.updatetime,
        })),
      );
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setLoading(false);
    }
  }, []);

  // load-on-mount pattern: reload() drives setLoading/setWorkflows/setError.
  // The React 19 lint rule discourages setState in effects, but there is no
  // Suspense-driven loader here; firing reload from an effect is the simplest
  // way to kick off the initial fetch.

  useEffect(() => {
    void reload();
  }, [reload]);

  const create = useCallback(
    async (name: string, content: string) => {
      await WorkflowCreate({ name, content });
      await reload();
    },
    [reload],
  );

  const update = useCallback(
    async (id: number, name: string, content: string) => {
      await WorkflowUpdate({ id, name, content });
      await reload();
    },
    [reload],
  );

  // remove 与 create/update 不同:create/update 被编辑表单 catch 后内联展示,所以保持抛出;
  // remove 没有表单消费方,失败直接落 error 由列表区展示。返回 ok 布尔,让调用方据此
  // 决定是否清选中/回浏览态(失败时保留选中,避免"看着删了其实没删"的错位)。
  const remove = useCallback(
    async (id: number): Promise<boolean> => {
      try {
        await WorkflowDelete({ id });
      } catch (e: unknown) {
        setError(e instanceof Error ? e.message : String(e));
        return false;
      }
      await reload();
      return true;
    },
    [reload],
  );

  return { workflows, loading, error, reload, create, update, remove };
}
