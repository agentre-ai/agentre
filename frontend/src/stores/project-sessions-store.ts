// frontend/src/stores/project-sessions-store.ts
//
// project-sessions-store 是「项目侧栏 sessions 列表」的唯一前端 source of truth。
// 设计与 chat-agents-store 镜像 —— ChatPanelHost 在 turn 起手 / 落定时调一次
// 统一的 reloadSidebarSources(), 两个 store 各自做 inflight dedup + 增量刷新,
// /chat 与 /projects 侧栏一起同步。
//
// 历史背景: ProjectsPage 此前自己维护 useState<Map<projectID, sessions>>,
// 只在初次 mount 和项目 CRUD 时刷; 新建会话后 sidebar 不刷, 切走再回来才出来。
// 把它提到 store 后, 跟 chat-agents-store 一起被 onSidebarShouldReload 触发。
//
// reload 行为:
//   - tree cache 未 loaded → 直接 resolve, 不发任何 RPC (用户没用过项目侧栏时
//     完全静默, 不为 chat 侧拉项目数据)。
//   - tree cache loaded → ensureProjectTreeLoaded 拿当前 tree (复用缓存, 不重拉),
//     并发 ProjectListSessions 每个项目, 单个项目失败兜底成 [], 不阻断整体。
//   - inflight dedup: 同一时刻只跑一次, 后续调用复用 promise。

import { create } from "zustand";

import {
  ensureProjectTreeLoaded,
  isProjectTreeCacheLoaded,
} from "@/hooks/use-project-tree";

import { ProjectListSessions } from "../../wailsjs/go/app/App";
import type { app } from "../../wailsjs/go/models";

export type ProjectSessionItem = app.ProjectSessionItem;
type ProjectTreeNode = app.ProjectTreeNode;

type State = {
  sessionsByProject: Map<number, ProjectSessionItem[]>;
  loading: boolean;
  error: string | null;
};

type Actions = {
  reload: () => Promise<void>;
  // 测试隔离用, 生产代码不该调。
  __reset: () => void;
};

let inflight: Promise<void> | null = null;

function collectProjectIDs(nodes: ProjectTreeNode[]): number[] {
  const ids: number[] = [];
  const stack = [...nodes];
  while (stack.length > 0) {
    const n = stack.shift();
    if (!n) continue;
    const pid = n.project?.id ?? 0;
    if (pid > 0) ids.push(pid);
    if (n.children) stack.push(...n.children);
  }
  return ids;
}

export const useProjectSessionsStore = create<State & Actions>((set) => ({
  sessionsByProject: new Map(),
  loading: false,
  error: null,
  reload: () => {
    if (inflight) return inflight;
    // 项目树没人订阅过 → 用户没打开过 /projects 也没用项目侧栏, 这次 reload 跳过。
    // 当用户切到 /projects 时 ProjectsPage 会主动 reloadProjectTreeCache + reload,
    // 那时再做正经的 sessions 拉取。
    if (!isProjectTreeCacheLoaded()) {
      return Promise.resolve();
    }
    set({ loading: true, error: null });
    inflight = (async () => {
      try {
        const tree = await ensureProjectTreeLoaded();
        const ids = collectProjectIDs(tree);
        const map = new Map<number, ProjectSessionItem[]>();
        await Promise.all(
          ids.map(async (pid) => {
            try {
              const sessions = await ProjectListSessions(pid);
              map.set(pid, sessions ?? []);
            } catch {
              // 单项目失败不阻断整体: 兜底成 []。整体 RPC 失败才走外层 catch。
              map.set(pid, []);
            }
          }),
        );
        set({ sessionsByProject: map, loading: false, error: null });
      } catch (e: unknown) {
        const msg = e instanceof Error ? e.message : String(e);
        set({ loading: false, error: msg });
      } finally {
        inflight = null;
      }
    })();
    return inflight;
  },
  __reset: () => {
    inflight = null;
    set({ sessionsByProject: new Map(), loading: false, error: null });
  },
}));
