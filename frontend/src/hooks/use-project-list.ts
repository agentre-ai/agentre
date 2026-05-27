import { useCallback, useEffect, useState } from "react";

import { ProjectListTree } from "../../wailsjs/go/app/App";
import type { app } from "../../wailsjs/go/models";

// 平铺投影：命令面板项目下拉只需要 id/name，不需要 tree 结构。
// 父子关系命令面板里不显示。
export type ProjectFlat = {
  id: number;
  name: string;
};

function flatten(nodes: app.ProjectTreeNode[]): ProjectFlat[] {
  const out: ProjectFlat[] = [];
  const walk = (ns: app.ProjectTreeNode[]) => {
    for (const n of ns) {
      if (n.project) {
        out.push({
          id: n.project.id,
          name: n.project.name,
        });
      }
      if (n.children) walk(n.children);
    }
  };
  walk(nodes);
  return out;
}

export function useProjectList() {
  const [projects, setProjects] = useState<ProjectFlat[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const reload = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const tree = (await ProjectListTree()) ?? [];
      setProjects(flatten(tree));
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setLoading(false);
    }
  }, []);

  // load-on-mount pattern: reload() drives setLoading/setProjects/setError.
  // The React 19 lint rule discourages setState in effects, but there is no
  // Suspense-driven loader here; firing reload from an effect is the simplest
  // way to kick off the initial fetch.

  useEffect(() => {
    void reload();
  }, [reload]);

  return { projects, loading, error, reload };
}

export { flatten as flattenProjects };
