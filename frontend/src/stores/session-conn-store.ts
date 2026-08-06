// frontend/src/stores/session-conn-store.ts
//
// session-conn-store 持有「本机与执行该会话那台远端 daemon 之间的通道状态」。
//
// 为什么它自成一个 store,而不是塞进 session-status-store:
// 连接态是运行态之上的一层**修饰**,不是第五个 AgentStatus —— 断连期间远端可能
// 正在全速跑,会话仍然是「运行中」。两者混在一起会让退出确认的活跃态集合、侧边栏
// 计数、工具栏禁用逻辑这些既有判定点全部错误归类。分开放,运行态的判定点就一个
// 字节都不用改;连接态只被转录流里的活信号形态消费。
//
// 写入路径只有一条:ChatStreamsHost 订阅会话级流 "chat:conn:<sessionId>"
// (后端 chat_svc.ConnStateStreamName),把 connection_state 事件翻译成一次 set。
// 订阅挂在跨路由长存的 host 上而不是 ChatPanel —— 连接态每次变化只发一帧,挂在
// 会随路由/标签页销毁的组件上,重新挂载时就只剩打字指示器,而真实情况是网断了。

import { create } from "zustand";

import type { SessionConnectionState } from "./types";
export type { SessionConnectionState };

type State = {
  // 只装「非 connected」的会话也可以,但保留全量更直白:没有条目 = 按已连接看待
  // (本地会话、以及远端会话开跑前后的常态)。
  conns: Map<number, SessionConnectionState>;
};

type Actions = {
  setConnState: (sessionId: number, state: SessionConnectionState) => void;
  // seed 是 LoadSession 响应的播种入口 —— 整页重载后推送流那一帧早已发过,连接态
  // 只能随响应同步回来(补发会落在订阅建立之前而丢失,那是竞态)。
  // 与 setConnState 的两点差别都来自"它带的是快照,不是跃迁":
  //   1. 已有条目就不写 —— 有条目意味着 chat:conn:<sid> 的订阅者在位且写过,
  //      订阅期内不漏帧,那份必然比响应快照新,不许被回填的旧值倒回去;
  //   2. connected 不落条目 —— 无条目本就按已连接看待,写进去只是多一条要清的记录。
  seed: (sessionId: number, state: SessionConnectionState) => void;
  // clear 在该会话最后一条流结束、连接态订阅随之解绑时调用:留着旧的 reconnecting
  // 会泄漏到下一轮,让一个连接正常的新 turn 顶着断连指示器跑。
  clear: (sessionId: number) => void;
  // stateOf 读单条;无记录按 connected 看待。
  stateOf: (sessionId: number) => SessionConnectionState;
  // 测试隔离用,生产代码不该调。
  __reset: () => void;
};

export const useSessionConnStore = create<State & Actions>((set, get) => ({
  conns: new Map(),

  setConnState: (sessionId, state) =>
    set((prev) => {
      if (prev.conns.get(sessionId) === state) return prev;
      const conns = new Map(prev.conns);
      conns.set(sessionId, state);
      return { conns };
    }),

  seed: (sessionId, state) =>
    set((prev) => {
      if (state === "connected" || prev.conns.has(sessionId)) return prev;
      const conns = new Map(prev.conns);
      conns.set(sessionId, state);
      return { conns };
    }),

  clear: (sessionId) =>
    set((prev) => {
      if (!prev.conns.has(sessionId)) return prev;
      const conns = new Map(prev.conns);
      conns.delete(sessionId);
      return { conns };
    }),

  stateOf: (sessionId) => get().conns.get(sessionId) ?? "connected",

  __reset: () => set({ conns: new Map() }),
}));

// useSessionConnectionState 是组件侧的窄订阅:只在该会话的连接态真的变了时重渲。
export function useSessionConnectionState(
  sessionId: number,
): SessionConnectionState {
  return useSessionConnStore((s) => s.conns.get(sessionId) ?? "connected");
}
