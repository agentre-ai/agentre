// frontend/src/hooks/__tests__/use-session-with-overlays.test.tsx
import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";

import { useSessionConnStore } from "@/stores/session-conn-store";
import { useSessionMetaStore } from "@/stores/session-meta-store";
import { useSessionReadStore } from "@/stores/session-read-store";
import { useSessionStatusStore } from "@/stores/session-status-store";
import { useSessionWithOverlays } from "../use-session-with-overlays";

import type { AgentStatus } from "@/stores/types";

describe("useSessionWithOverlays", () => {
  beforeEach(() => {
    useSessionMetaStore.getState().__reset();
    useSessionStatusStore.getState().__reset();
    // session-read-store 没有 __reset，跳过；按 sid 隔离测试用例
  });

  it("无任何 store 数据 → 返回 null", () => {
    const { result } = renderHook(() => useSessionWithOverlays(999));
    expect(result.current).toBeNull();
  });

  it("meta + status 都在 → 合并为 SessionView", () => {
    useSessionMetaStore.getState().setMeta(1, {
      agentId: 10,
      agentName: "A",
      agentColor: "agent-1",
      projectId: 0,
      title: "t",
      lastMessageAt: 100,
    });
    useSessionStatusStore.getState().upsert(1, {
      agentStatus: "running",
      needsAttention: false,
    });
    const { result } = renderHook(() => useSessionWithOverlays(1));
    expect(result.current).toMatchObject({
      id: 1,
      agentStatus: "running",
      needsAttention: false,
      title: "t",
      lastMessageAt: 100,
      lastReadAt: 0,
    });
  });

  it("read overlay 高于 server lastReadAt", () => {
    useSessionMetaStore.getState().setMeta(101, {
      agentId: 10,
      agentName: "A",
      agentColor: "agent-1",
      projectId: 0,
      title: "t",
      lastMessageAt: 100,
    });
    useSessionStatusStore.getState().upsert(101, {
      agentStatus: "idle",
      needsAttention: false,
    });
    act(() => useSessionReadStore.getState().markRead(101, 500));
    const { result } = renderHook(() => useSessionWithOverlays(101));
    expect(result.current?.lastReadAt).toBe(500);
  });

  it("同值再调用返回同引用（referential equality）", () => {
    useSessionMetaStore.getState().setMeta(102, {
      agentId: 10,
      agentName: "A",
      agentColor: "agent-1",
      projectId: 0,
      title: "t",
      lastMessageAt: 100,
    });
    useSessionStatusStore.getState().upsert(102, {
      agentStatus: "idle",
      needsAttention: false,
    });
    const { result, rerender } = renderHook(() => useSessionWithOverlays(102));
    const first = result.current;
    rerender();
    expect(result.current).toBe(first);
  });

  it("meta.lastReadAt 与 read overlay 取 max", () => {
    useSessionMetaStore.getState().setMeta(103, {
      agentId: 10,
      agentName: "A",
      agentColor: "agent-1",
      projectId: 0,
      title: "t",
      lastMessageAt: 100,
      lastReadAt: 50,
    });
    useSessionStatusStore.getState().upsert(103, {
      agentStatus: "idle",
      needsAttention: false,
    });
    act(() => useSessionReadStore.getState().markRead(103, 30)); // override 比 server 小
    const { result } = renderHook(() => useSessionWithOverlays(103));
    expect(result.current?.lastReadAt).toBe(50); // server wins
  });
});

// ─── R15:会话状态沿用既有四态 ───────────────────────────────────────────────
//
// 连接态是运行态之上的**修饰**,不是第五种运行状态 —— 远端此刻可能正在全速跑,
// 断的只是本机与远端之间的通道。把它做成第五个取值会让所有既有的状态判定点
// (退出确认的活跃态集合、侧边栏计数、工具栏禁用逻辑)全部错误归类。
describe("会话状态沿用既有四态 (R15)", () => {
  beforeEach(() => {
    useSessionMetaStore.getState().__reset();
    useSessionStatusStore.getState().__reset();
    useSessionConnStore.getState().__reset();
  });

  function seedRunningRemoteSession(sid: number): void {
    useSessionMetaStore.getState().setMeta(sid, {
      agentId: 10,
      agentName: "A",
      agentColor: "agent-1",
      projectId: 0,
      title: "t",
      lastMessageAt: 100,
    });
    useSessionStatusStore.getState().upsert(sid, {
      agentStatus: "running",
      needsAttention: false,
    });
  }

  it("守卫:AgentStatus 仍只有四个取值", () => {
    // Record<AgentStatus, true> 让编译期漏掉新取值直接报错(tsc -b --noEmit),
    // 下面的键集合断言让运行期多出的取值也拦得住。
    const all: Record<AgentStatus, true> = {
      idle: true,
      running: true,
      waiting: true,
      error: true,
    };
    expect(Object.keys(all).sort()).toEqual([
      "error",
      "idle",
      "running",
      "waiting",
    ]);
  });

  it("等待用户输入落在既有的 waiting 态", () => {
    seedRunningRemoteSession(200);
    act(() =>
      useSessionStatusStore.getState().upsert(200, {
        agentStatus: "waiting",
        needsAttention: true,
      }),
    );
    const { result } = renderHook(() => useSessionWithOverlays(200));
    expect(result.current?.agentStatus).toBe("waiting");
    expect(result.current?.needsAttention).toBe(true);
  });

  it("daemon 重启导致的中断落在既有的 error 态,靠文案区分而非新状态", () => {
    seedRunningRemoteSession(201);
    act(() => useSessionConnStore.getState().setConnState(201, "lost"));
    act(() =>
      useSessionStatusStore.getState().upsert(201, {
        agentStatus: "error",
        needsAttention: true,
      }),
    );
    const { result } = renderHook(() => useSessionWithOverlays(201));
    expect(result.current?.agentStatus).toBe("error");
  });

  it("转入重连态不改运行态:agentStatus 仍是 running,运行态 store 一个字节没动", () => {
    seedRunningRemoteSession(202);
    const { result } = renderHook(() => useSessionWithOverlays(202));
    const statusBefore = useSessionStatusStore.getState().statuses.get(202);

    act(() => useSessionConnStore.getState().setConnState(202, "reconnecting"));

    expect(useSessionConnStore.getState().stateOf(202)).toBe("reconnecting");
    expect(result.current?.agentStatus).toBe("running");
    expect(result.current?.needsAttention).toBe(false);
    // 同一引用 = 连接态没有经由运行态 store 落地。
    expect(useSessionStatusStore.getState().statuses.get(202)).toBe(
      statusBefore,
    );
  });

  it("连接恢复后连接态回到 connected,运行态自始至终没被碰过", () => {
    seedRunningRemoteSession(203);
    const { result } = renderHook(() => useSessionWithOverlays(203));
    const viewBefore = result.current;

    act(() => useSessionConnStore.getState().setConnState(203, "reconnecting"));
    act(() => useSessionConnStore.getState().setConnState(203, "connected"));

    expect(useSessionConnStore.getState().stateOf(203)).toBe("connected");
    expect(result.current).toBe(viewBefore);
  });

  it("没有连接态记录的会话默认按已连接看待(本地会话不受影响)", () => {
    expect(useSessionConnStore.getState().stateOf(9999)).toBe("connected");
  });
});
