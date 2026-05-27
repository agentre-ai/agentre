import { beforeEach, describe, expect, it } from "vitest";

import { useSessionStatusStore } from "../session-status-store";

describe("session-status-store", () => {
  beforeEach(() => {
    useSessionStatusStore.getState().__reset();
  });

  it("upsert 写入新 sid", () => {
    useSessionStatusStore.getState().upsert(1, {
      agentStatus: "running",
      needsAttention: false,
    });
    expect(useSessionStatusStore.getState().statuses.get(1)).toMatchObject({
      agentStatus: "running",
      needsAttention: false,
      doneTick: 0,
      lastDoneEvent: null,
    });
  });

  it("upsert 同值短路: Map 引用稳定", () => {
    useSessionStatusStore.getState().upsert(1, {
      agentStatus: "running",
      needsAttention: false,
    });
    const before = useSessionStatusStore.getState().statuses;
    useSessionStatusStore.getState().upsert(1, {
      agentStatus: "running",
      needsAttention: false,
    });
    const after = useSessionStatusStore.getState().statuses;
    expect(after).toBe(before);
  });

  it("upsert 字段变化时换 Map 引用", () => {
    useSessionStatusStore.getState().upsert(1, {
      agentStatus: "running",
      needsAttention: false,
    });
    const before = useSessionStatusStore.getState().statuses;
    useSessionStatusStore.getState().upsert(1, {
      agentStatus: "waiting",
      needsAttention: true,
    });
    const after = useSessionStatusStore.getState().statuses;
    expect(after).not.toBe(before);
    expect(after.get(1)).toMatchObject({
      agentStatus: "waiting",
      needsAttention: true,
    });
  });

  it("upsert permissionMode undefined 与 '' 视为同值", () => {
    useSessionStatusStore.getState().upsert(1, {
      agentStatus: "running",
      needsAttention: false,
      permissionMode: "",
    });
    const before = useSessionStatusStore.getState().statuses;
    useSessionStatusStore.getState().upsert(1, {
      agentStatus: "running",
      needsAttention: false,
    });
    const after = useSessionStatusStore.getState().statuses;
    expect(after).toBe(before);
  });

  it("upsert permissionMode 改变时不视为同值", () => {
    useSessionStatusStore.getState().upsert(1, {
      agentStatus: "running",
      needsAttention: false,
      permissionMode: "plan",
    });
    useSessionStatusStore.getState().upsert(1, {
      agentStatus: "running",
      needsAttention: false,
      permissionMode: "default",
    });
    expect(useSessionStatusStore.getState().statuses.get(1)).toMatchObject({
      agentStatus: "running",
      needsAttention: false,
      permissionMode: "default",
    });
  });

  it("bulkUpsert 全部同值时 Map 引用不变", () => {
    useSessionStatusStore.getState().upsert(1, {
      agentStatus: "running",
      needsAttention: false,
    });
    useSessionStatusStore.getState().upsert(2, {
      agentStatus: "idle",
      needsAttention: false,
    });
    const before = useSessionStatusStore.getState().statuses;
    useSessionStatusStore.getState().bulkUpsert([
      [1, { agentStatus: "running", needsAttention: false }],
      [2, { agentStatus: "idle", needsAttention: false }],
    ]);
    const after = useSessionStatusStore.getState().statuses;
    expect(after).toBe(before);
  });

  it("bulkUpsert 只要有一条变化就换 Map 引用", () => {
    useSessionStatusStore.getState().upsert(1, {
      agentStatus: "running",
      needsAttention: false,
    });
    useSessionStatusStore.getState().upsert(2, {
      agentStatus: "idle",
      needsAttention: false,
    });
    const before = useSessionStatusStore.getState().statuses;
    useSessionStatusStore.getState().bulkUpsert([
      [1, { agentStatus: "running", needsAttention: false }], // 同值
      [2, { agentStatus: "running", needsAttention: false }], // 变化
    ]);
    const after = useSessionStatusStore.getState().statuses;
    expect(after).not.toBe(before);
    expect(after.get(2)?.agentStatus).toBe("running");
  });

  it("bulkUpsert 写入未存在的 sid", () => {
    useSessionStatusStore.getState().bulkUpsert([
      [10, { agentStatus: "running", needsAttention: false }],
      [11, { agentStatus: "waiting", needsAttention: true }],
    ]);
    const map = useSessionStatusStore.getState().statuses;
    expect(map.get(10)?.agentStatus).toBe("running");
    expect(map.get(11)?.agentStatus).toBe("waiting");
  });

  it("remove 删掉指定 sid, 其他保留", () => {
    useSessionStatusStore.getState().upsert(1, {
      agentStatus: "running",
      needsAttention: false,
    });
    useSessionStatusStore.getState().upsert(2, {
      agentStatus: "waiting",
      needsAttention: true,
    });
    useSessionStatusStore.getState().remove(1);
    const map = useSessionStatusStore.getState().statuses;
    expect(map.has(1)).toBe(false);
    expect(map.get(2)?.agentStatus).toBe("waiting");
  });

  it("remove 不存在的 sid 时 Map 引用稳定", () => {
    const before = useSessionStatusStore.getState().statuses;
    useSessionStatusStore.getState().remove(999);
    const after = useSessionStatusStore.getState().statuses;
    expect(after).toBe(before);
  });

  it("bumpDone 自增 doneTick 并记录最近事件", () => {
    const ev = { kind: "done" as const, sessionId: 1 };
    useSessionStatusStore.getState().bumpDone(1, ev);
    const v = useSessionStatusStore.getState().statuses.get(1);
    expect(v?.doneTick).toBe(1);
    expect(v?.lastDoneEvent?.kind).toBe("done");

    useSessionStatusStore
      .getState()
      .bumpDone(1, { kind: "error", sessionId: 1 });
    const v2 = useSessionStatusStore.getState().statuses.get(1);
    expect(v2?.doneTick).toBe(2);
    expect(v2?.lastDoneEvent?.kind).toBe("error");
  });

  it("bumpDone 保留已有 agentStatus/needsAttention", () => {
    useSessionStatusStore.getState().upsert(5, {
      agentStatus: "running",
      needsAttention: true,
    });
    useSessionStatusStore.getState().bumpDone(5, { kind: "done" });
    const v = useSessionStatusStore.getState().statuses.get(5);
    expect(v?.agentStatus).toBe("running");
    expect(v?.needsAttention).toBe(true);
    expect(v?.doneTick).toBe(1);
  });

  it("upsert 保留已有 doneTick/lastDoneEvent", () => {
    useSessionStatusStore.getState().bumpDone(3, { kind: "done" });
    expect(useSessionStatusStore.getState().statuses.get(3)?.doneTick).toBe(1);
    // 再 upsert 修改 agentStatus，doneTick 不应被清零
    useSessionStatusStore.getState().upsert(3, {
      agentStatus: "idle",
      needsAttention: false,
    });
    expect(useSessionStatusStore.getState().statuses.get(3)?.doneTick).toBe(1);
    expect(
      useSessionStatusStore.getState().statuses.get(3)?.lastDoneEvent?.kind,
    ).toBe("done");
  });
});
