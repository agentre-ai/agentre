import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";

import { useSessionMetaStore } from "../session-meta-store";
import { useSessionReadStore } from "../session-read-store";
import { useSessionStatusStore } from "../session-status-store";
import {
  useSessionAttention,
  useSessionAttentionList,
} from "../attention-store";

import { computeAttention } from "../attention-store";

describe("computeAttention", () => {
  const base = {
    agentStatus: "idle" as const,
    needsAttention: false,
    lastMessageAt: 0,
    lastReadAt: 0,
  };

  it("needsAttention 优先于其它", () => {
    expect(
      computeAttention({
        ...base,
        needsAttention: true,
        agentStatus: "running",
      }),
    ).toBe("needs_attention");
    expect(
      computeAttention({
        ...base,
        needsAttention: true,
        agentStatus: "error",
        lastMessageAt: 100,
      }),
    ).toBe("needs_attention");
  });

  it("running", () => {
    expect(computeAttention({ ...base, agentStatus: "running" })).toBe(
      "running",
    );
  });

  it("error + unread → error", () => {
    expect(
      computeAttention({
        ...base,
        agentStatus: "error",
        lastMessageAt: 100,
        lastReadAt: 50,
      }),
    ).toBe("error");
  });

  it("error + 已读 → null（不骚扰）", () => {
    expect(
      computeAttention({
        ...base,
        agentStatus: "error",
        lastMessageAt: 50,
        lastReadAt: 100,
      }),
    ).toBeNull();
  });

  it("idle + unread → unread", () => {
    expect(
      computeAttention({
        ...base,
        agentStatus: "idle",
        lastMessageAt: 100,
        lastReadAt: 50,
      }),
    ).toBe("unread");
  });

  it("idle + 已读 → null", () => {
    expect(
      computeAttention({
        ...base,
        agentStatus: "idle",
        lastMessageAt: 50,
        lastReadAt: 100,
      }),
    ).toBeNull();
  });

  it("waiting 永远伴随 needsAttention=true（后端契约），实际表现为 needs_attention", () => {
    // 假设 backend 失误送来 waiting + !needsAttention，函数仍返回 null（不冒泡）
    expect(
      computeAttention({
        ...base,
        agentStatus: "waiting",
        needsAttention: false,
      }),
    ).toBeNull();
  });

  it("空输入 → null", () => {
    expect(computeAttention(base)).toBeNull();
  });

  // ── 8 个边界回归用例（spec §8 要求 16 个表驱动用例）────────────────────

  it("idle: lastMessageAt === lastReadAt → null（> 不成立，不是 unread）", () => {
    expect(
      computeAttention({
        ...base,
        agentStatus: "idle",
        lastMessageAt: 100,
        lastReadAt: 100,
      }),
    ).toBeNull();
  });

  it("error: lastMessageAt === lastReadAt → null（= 不触发 unread 分支）", () => {
    expect(
      computeAttention({
        ...base,
        agentStatus: "error",
        lastMessageAt: 100,
        lastReadAt: 100,
      }),
    ).toBeNull();
  });

  it("全零边界：lastMessageAt=0 + lastReadAt=0 → null（0 > 0 不成立）", () => {
    expect(
      computeAttention({
        ...base,
        agentStatus: "idle",
        lastMessageAt: 0,
        lastReadAt: 0,
      }),
    ).toBeNull();
    expect(
      computeAttention({
        ...base,
        agentStatus: "error",
        lastMessageAt: 0,
        lastReadAt: 0,
      }),
    ).toBeNull();
    expect(
      computeAttention({
        ...base,
        agentStatus: "running",
        lastMessageAt: 0,
        lastReadAt: 0,
      }),
    ).toBe("running");
  });

  it("消息时间早于已读（lastMessageAt=0, lastReadAt=100）→ null（合法但不 unread）", () => {
    expect(
      computeAttention({
        ...base,
        agentStatus: "idle",
        lastMessageAt: 0,
        lastReadAt: 100,
      }),
    ).toBeNull();
    expect(
      computeAttention({
        ...base,
        agentStatus: "error",
        lastMessageAt: 0,
        lastReadAt: 100,
      }),
    ).toBeNull();
    expect(
      computeAttention({
        ...base,
        agentStatus: "running",
        lastMessageAt: 0,
        lastReadAt: 100,
      }),
    ).toBe("running");
  });

  it("needsAttention=true + lastReadAt > lastMessageAt → 仍是 needs_attention（needsAttention 优先级最高）", () => {
    expect(
      computeAttention({
        ...base,
        needsAttention: true,
        agentStatus: "idle",
        lastMessageAt: 50,
        lastReadAt: 200,
      }),
    ).toBe("needs_attention");
    expect(
      computeAttention({
        ...base,
        needsAttention: true,
        agentStatus: "error",
        lastMessageAt: 50,
        lastReadAt: 200,
      }),
    ).toBe("needs_attention");
  });

  it("running + unread（lastMessageAt > lastReadAt）→ running（running 优先于 unread）", () => {
    expect(
      computeAttention({
        ...base,
        agentStatus: "running",
        lastMessageAt: 100,
        lastReadAt: 0,
      }),
    ).toBe("running");
  });

  it("running + needsAttention=true → needs_attention（needsAttention 优先级高于 running）", () => {
    expect(
      computeAttention({
        ...base,
        agentStatus: "running",
        needsAttention: true,
        lastMessageAt: 100,
        lastReadAt: 0,
      }),
    ).toBe("needs_attention");
  });

  it("未知 agentStatus（如 'pending'）+ 无 needsAttention + unread → null（不在已知状态内，不冒泡）", () => {
    // AgentStatus 类型不含 'pending'；通过 cast 模拟 Wails 边界传入未知值。
    const unknown = "pending" as unknown as import("../types").AgentStatus;
    expect(
      computeAttention({
        ...base,
        agentStatus: unknown,
        lastMessageAt: 100,
        lastReadAt: 0,
      }),
    ).toBeNull();
  });
});

describe("useSessionAttention", () => {
  beforeEach(() => {
    useSessionMetaStore.getState().__reset();
    useSessionStatusStore.getState().__reset();
  });

  it("session 不存在 → reason=null, isAttention=false", () => {
    const { result } = renderHook(() => useSessionAttention(999));
    expect(result.current).toEqual({ reason: null, isAttention: false });
  });

  it("needsAttention=true → reason='needs_attention', isAttention=true", () => {
    useSessionMetaStore.getState().setMeta(1, {
      agentId: 1,
      agentName: "A",
      agentColor: "agent-1",
      projectId: 0,
      title: "t",
      lastMessageAt: 100,
      lastReadAt: 100,
    });
    useSessionStatusStore
      .getState()
      .upsert(1, { agentStatus: "waiting", needsAttention: true });
    const { result } = renderHook(() => useSessionAttention(1));
    expect(result.current).toEqual({
      reason: "needs_attention",
      isAttention: true,
    });
  });

  it("打开会话后 markRead → unread reason 消失", () => {
    useSessionMetaStore.getState().setMeta(1, {
      agentId: 1,
      agentName: "A",
      agentColor: "agent-1",
      projectId: 0,
      title: "t",
      lastMessageAt: 100,
      lastReadAt: 0,
    });
    useSessionStatusStore
      .getState()
      .upsert(1, { agentStatus: "idle", needsAttention: false });
    const { result } = renderHook(() => useSessionAttention(1));
    expect(result.current.reason).toBe("unread");
    act(() => useSessionReadStore.getState().markRead(1, 100));
    expect(result.current.reason).toBeNull();
  });
});

describe("useSessionAttentionList", () => {
  beforeEach(() => {
    useSessionMetaStore.getState().__reset();
    useSessionStatusStore.getState().__reset();
  });

  it("批量返回（无序，调用方自己排）", () => {
    // 使用不同 sid（10/11/12）避免 session-read-store 跨用例污染（无 __reset）
    useSessionMetaStore.getState().setMeta(10, {
      agentId: 1,
      agentName: "A",
      agentColor: "agent-1",
      projectId: 0,
      title: "t",
      lastMessageAt: 100,
      lastReadAt: 0,
    });
    useSessionMetaStore.getState().setMeta(11, {
      agentId: 1,
      agentName: "A",
      agentColor: "agent-1",
      projectId: 0,
      title: "t",
      lastMessageAt: 0,
      lastReadAt: 0,
    });
    useSessionStatusStore
      .getState()
      .upsert(10, { agentStatus: "idle", needsAttention: false });
    useSessionStatusStore
      .getState()
      .upsert(11, { agentStatus: "running", needsAttention: false });
    const { result } = renderHook(() => useSessionAttentionList([10, 11, 12]));
    expect(result.current).toEqual(
      expect.arrayContaining([
        { sessionId: 10, reason: "unread" },
        { sessionId: 11, reason: "running" },
      ]),
    );
    expect(result.current.length).toBe(2); // sid=12 没数据,跳过
  });

  it("内容相同的新数组引用不触发重算（referential stability）", () => {
    // sid=20 有 unread reason；sid=21 没数据
    useSessionMetaStore.getState().setMeta(20, {
      agentId: 1,
      agentName: "A",
      agentColor: "agent-1",
      projectId: 0,
      title: "t",
      lastMessageAt: 200,
      lastReadAt: 0,
    });
    useSessionStatusStore
      .getState()
      .upsert(20, { agentStatus: "idle", needsAttention: false });

    // 第一次 render：ids1 = [20, 21]
    let ids = [20, 21];
    const { result, rerender } = renderHook(() => useSessionAttentionList(ids));
    const first = result.current;
    expect(first.length).toBe(1);

    // 内容相同、但引用不同的新数组 → rerender
    ids = [20, 21];
    rerender();
    // 输出引用应与第一次相同（useMemo 未失效）
    expect(result.current).toBe(first);
  });
});
