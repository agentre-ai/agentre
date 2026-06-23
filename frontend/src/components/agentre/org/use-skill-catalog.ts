import * as React from "react";
import { useTranslation } from "react-i18next";

import { ListAgentSkillPacks } from "@/../wailsjs/go/app/App";

import type { CatalogItem } from "../capability/catalog";
import { skillPacksToCatalog } from "./skill-catalog";

// useSkillCatalog 加载某 agent 的技能目录。默认懒加载——组件在「打开添加弹窗」
// 时调一次 load(false)，「重新扫描」时 load(true)。
// 传入 autoLoad=true 时在 mount 时自动拉一次（技能区可见时用），默认 false 保持懒加载。
export function useSkillCatalog(agentId: number, autoLoad = false) {
  const { t } = useTranslation();
  const [items, setItems] = React.useState<CatalogItem[]>([]);
  const [loading, setLoading] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);
  const [fetched, setFetched] = React.useState(false);

  const load = React.useCallback(
    async (refresh: boolean) => {
      setLoading(true);
      setError(null);
      try {
        const resp = await ListAgentSkillPacks(agentId, refresh);
        setItems(skillPacksToCatalog(resp?.packs ?? [], t));
        setFetched(true);
      } catch (e) {
        setError(e instanceof Error ? e.message : String(e));
      } finally {
        setLoading(false);
      }
    },
    [agentId, t],
  );

  React.useEffect(() => {
    if (autoLoad) void load(false);
  }, [autoLoad, load]);

  return { items, loading, error, fetched, load, reload: () => load(false) };
}
