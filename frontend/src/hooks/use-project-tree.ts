// frontend/src/hooks/use-project-tree.ts
import * as React from "react";

import { ProjectListTree } from "../../wailsjs/go/app/App";
import type { app } from "../../wailsjs/go/models";

type ProjectTreeNode = app.ProjectTreeNode;

type Cache = {
  tree: ProjectTreeNode[];
  promise: Promise<ProjectTreeNode[]> | null;
  loaded: boolean;
};

let cache: Cache = { tree: [], promise: null, loaded: false };
let listeners: Array<() => void> = [];

export function __resetProjectTreeForTesting() {
  cache = { tree: [], promise: null, loaded: false };
  listeners = [];
}

function notify() {
  for (const l of listeners) l();
}

export function invalidateProjectTreeCache() {
  void reloadProjectTreeCache();
}

export function reloadProjectTreeCache(): Promise<ProjectTreeNode[]> {
  cache = { tree: [], promise: null, loaded: false };
  return loadOnce();
}

// isProjectTreeCacheLoaded 给非 React 处用 (project-sessions-store): 没人订阅过
// 项目树时不必为 chat 侧的 sidebar 刷新顺带拉一遍项目数据。
export function isProjectTreeCacheLoaded(): boolean {
  return cache.loaded;
}

// ensureProjectTreeLoaded 给非 React 处复用 loadOnce 语义: 已加载 → 返回缓存,
// 否则触发并 await 加载, 中途有别人发起加载就复用 in-flight promise。
export function ensureProjectTreeLoaded(): Promise<ProjectTreeNode[]> {
  return loadOnce();
}

async function loadOnce(): Promise<ProjectTreeNode[]> {
  if (cache.loaded) return cache.tree;
  if (cache.promise) return cache.promise;
  cache.promise = (async () => {
    try {
      const tree = (await ProjectListTree()) ?? [];
      cache = { tree, promise: null, loaded: true };
      notify();
      return tree;
    } catch {
      cache = { tree: [], promise: null, loaded: true };
      notify();
      return [];
    }
  })();
  return cache.promise;
}

export function useProjectTree() {
  const [, force] = React.useReducer((n: number) => n + 1, 0);
  React.useEffect(() => {
    const l = () => force();
    listeners.push(l);
    if (!cache.loaded) void loadOnce();
    return () => {
      listeners = listeners.filter((x) => x !== l);
    };
  }, []);
  const invalidate = React.useCallback(() => {
    invalidateProjectTreeCache();
  }, []);
  return { tree: cache.tree, invalidate, loaded: cache.loaded };
}
