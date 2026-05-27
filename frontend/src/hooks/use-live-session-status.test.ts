import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";

import { useSessionStatusStore } from "@/stores/session-status-store";

import {
  useEffectiveSessionStatus,
  useSessionStatusOverlay,
} from "./use-live-session-status";

describe("useEffectiveSessionStatus", () => {
  beforeEach(() => {
    useSessionStatusStore.getState().__reset();
  });

  // store 里没该 sid 时返回 fallback。会话列表里大多数 session 没有运行时态,
  // 必须保持 DB 快照不变。
  it("returns fallback when store has no entry", () => {
    const { result } = renderHook(() =>
      useEffectiveSessionStatus(42, {
        agentStatus: "idle",
        needsAttention: false,
      }),
    );
    expect(result.current).toEqual({
      agentStatus: "idle",
      needsAttention: false,
    });
  });

  // store 有运行时态时覆盖 fallback —— 不论该值是不是真比 fallback 新, 默认
  // store 是事实, 调用方再决定怎么合并。
  it("overlays session-status-store when entry exists", () => {
    const { result, rerender } = renderHook(() =>
      useEffectiveSessionStatus(7, {
        agentStatus: "running",
        needsAttention: false,
      }),
    );
    expect(result.current.agentStatus).toBe("running");

    act(() => {
      useSessionStatusStore.getState().upsert(7, {
        agentStatus: "waiting",
        needsAttention: true,
      });
    });
    rerender();

    expect(result.current).toEqual({
      agentStatus: "waiting",
      needsAttention: true,
    });
  });

  // 列表 overlay 模式：project-page / agent-list 把 ProjectSessionItem[] 喂进来，
  // 一次性把所有命中 store 的项替换 agentStatus / needsAttention,
  // 没命中的保持原引用 —— 整数组只在真有变化时产生新引用避免反复 re-render。
  describe("useSessionStatusOverlay", () => {
    it("returns same reference when store is empty", () => {
      const sessions = [
        { id: 1, agentStatus: "idle", needsAttention: false },
        { id: 2, agentStatus: "running", needsAttention: false },
      ];
      const { result } = renderHook(() => useSessionStatusOverlay(sessions));
      expect(result.current).toBe(sessions);
    });

    it("overlays matching ids and keeps others unchanged", () => {
      act(() => {
        useSessionStatusStore.getState().upsert(2, {
          agentStatus: "waiting",
          needsAttention: true,
        });
      });
      const sessions = [
        { id: 1, agentStatus: "idle", needsAttention: false },
        { id: 2, agentStatus: "running", needsAttention: false },
        { id: 3, agentStatus: "idle", needsAttention: false },
      ];
      const { result } = renderHook(() => useSessionStatusOverlay(sessions));
      expect(result.current[0]).toBe(sessions[0]);
      expect(result.current[1]).toEqual({
        id: 2,
        agentStatus: "waiting",
        needsAttention: true,
      });
      expect(result.current[2]).toBe(sessions[2]);
    });
  });

  // store 里手动 remove 后 hook 必须 fallback 回快照，避免一直挂在 waiting。
  it("falls back when entry is removed", () => {
    act(() => {
      useSessionStatusStore.getState().upsert(8, {
        agentStatus: "waiting",
        needsAttention: true,
      });
    });

    const { result, rerender } = renderHook(() =>
      useEffectiveSessionStatus(8, {
        agentStatus: "idle",
        needsAttention: false,
      }),
    );
    expect(result.current.agentStatus).toBe("waiting");

    act(() => {
      useSessionStatusStore.getState().remove(8);
    });
    rerender();

    expect(result.current).toEqual({
      agentStatus: "idle",
      needsAttention: false,
    });
  });
});
