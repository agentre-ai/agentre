// frontend/src/stores/chat-agents-store.ts
//
// chat-agents-store 是「左侧 sidebar 的 agents 列表」唯一数据源。所有消费方
// (ChatPage / 命令面板 / App / 顶层 indicator) 共享同一份 state, 任何一处触发
// reload 都让其他订阅者立刻看到最新数据。
//
// 这个 store 取代了原先 hook 内的 useState —— hook 之间各自维护一份会造成
// 「sidebar 拉过、命令面板没拉」的不一致, 且 ChatPanelHost 这种「不直接调
// hook 的组件」根本没办法触发刷新 (这是新建会话不进左栏的根因)。改成 store
// 之后 ChatPanelHost 直接 useChatAgentsStore.getState().reload() 即可。
//
// reload 并发去重: 同一时刻只跑一个 ListChatAgents in-flight; 后续 reload 调用
// 复用同一个 promise, 避免 sidebar 拉一遍、命令面板紧接着又拉一遍。

import { create } from "zustand";

import { ListChatAgents } from "../../wailsjs/go/app/App";
import type { chat_svc } from "../../wailsjs/go/models";

import {
  useSessionStatusStore,
  type SessionStatusPatch,
} from "./session-status-store";
import { useSessionMetaStore, type SessionMeta } from "./session-meta-store";
import type { AgentStatus } from "./types";

// AgentSlim: chat-agents-store 对外暴露的 agent 行形态 —— 在 Wails 原生 ChatAgentItem
// 基础上附加 sessionIds（去重的全量 id 列表）。
//
// 实现细节: reload 时直接 Object.assign 把 sessionIds 挂到 Wails class 实例上(原地修改),
// 不用 spread。这样既保留 class 方法 (convertValues 等), 也让既有 ChatAgentItem[] 消费方
// 不用改类型 —— AgentSlim is-a ChatAgentItem。
//
// 后端 ChatAgentItem 已经带上, 但 wailsjs codegen 只有 make dev / wails build 时才刷新,
// 这里手工 mirror 让 TS 在 codegen 未运行的 worktree 也能编译通过 (运行时仍走 Wails
// 序列化的真实 JSON, 不影响数据流)。
export type AgentSlim = chat_svc.ChatAgentItem & {
  sessionIds: number[];
  deviceID?: string;
  deviceName?: string;
  online?: boolean;
};

// 兼容: 历史代码用 "ChatAgentItem" 这个名字指代「chat-agents-store 提供的 agent 行」。
// 别名指向 AgentSlim, 让 useChatAgents() 的消费方拿到 sessionIds (App.tsx reconcile 用),
// 同时由于 AgentSlim is-a chat_svc.ChatAgentItem, 旧的字段访问代码也都通过。
export type ChatAgentItem = AgentSlim;

type State = {
  agents: AgentSlim[];
  loading: boolean;
  error: string | null;
};

type Actions = {
  reload: () => Promise<void>;
  // 测试隔离用, 生产代码不该调。
  __reset: () => void;
};

// in-flight reload promise: 并发调用 reload() 时复用, 避免重复 RPC。
let inflight: Promise<void> | null = null;

// 初始 loading=true: 反映「还没拉过, 别把空 agents 当成最终态」, 与原 hook 行为对齐
// (原 useState(true))。这样命令面板在 useChatAgents 首次 mount 的那一帧不会闪「无结果」。
export const useChatAgentsStore = create<State & Actions>((set) => ({
  agents: [],
  loading: true,
  error: null,
  reload: () => {
    if (inflight) return inflight;
    set({ loading: true, error: null });
    inflight = (async () => {
      try {
        const resp = await ListChatAgents();
        const agents = resp.agents ?? [];
        // 把快照里所有 session 的 status 灌进 session-status-store, 让 tab /
        // sidebar / toolbar 通过同一个 store 读到「running / waiting / idle」。
        // bulkUpsert 内部逐条同值短路, 一刷只在真有差异时才换 Map 引用。
        const entries: [number, SessionStatusPatch][] = [];
        for (const a of agents) {
          for (const s of a.sessions ?? []) {
            entries.push([
              s.id,
              {
                // Wails boundary: ChatSessionLite.status is string; cast to AgentStatus.
                agentStatus: (s.status as AgentStatus) || "idle",
                needsAttention: s.needsAttention ?? false,
              },
            ]);
          }
        }
        useSessionStatusStore.getState().bulkUpsert(entries);

        // 把 sessions 的静态字段分发到 session-meta-store。
        // ChatSessionLite 没有 projectId —— 它只能从 ChatSessionDetail (LoadChatSession) 拿,
        // 所以这里 patch 不带 projectId, 由 useChatSession 在加载详情后通过 setMeta 补全。
        // bulkUpsert 走 merge 语义, 不会清掉既有 projectId。
        const metaEntries: [number, Partial<SessionMeta>][] = [];
        for (const a of agents) {
          for (const s of a.sessions ?? []) {
            metaEntries.push([
              s.id,
              {
                agentId: a.id,
                agentName: a.name,
                agentColor: a.avatarColor || "agent-1",
                title: s.title || "",
                lastMessageAt: s.lastMessageAt ?? 0,
                lastReadAt: s.lastReadAt ?? 0,
              },
            ]);
          }
        }
        useSessionMetaStore.getState().bulkUpsert(metaEntries);

        // 构造 AgentSlim: 原地 Object.assign 给 Wails class 实例挂 sessionIds 字段,
        // 不用 spread —— spread 会丢失 class 方法 (convertValues), 触发 TS 错误。
        const slimAgents = agents.map((a) => {
          const ids = new Set<number>();
          for (const s of a.sessions ?? []) ids.add(s.id);
          return Object.assign(a, { sessionIds: Array.from(ids) }) as AgentSlim;
        });

        set({ agents: slimAgents, loading: false, error: null });
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
    set({ agents: [], loading: true, error: null });
  },
}));
