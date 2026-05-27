// frontend/src/hooks/__tests__/use-session-with-overlays.test.tsx
import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";

import { useSessionMetaStore } from "@/stores/session-meta-store";
import { useSessionReadStore } from "@/stores/session-read-store";
import { useSessionStatusStore } from "@/stores/session-status-store";
import { useSessionWithOverlays } from "../use-session-with-overlays";

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
