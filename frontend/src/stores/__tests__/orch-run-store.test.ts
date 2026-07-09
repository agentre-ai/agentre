import { describe, it, expect, vi, beforeEach } from "vitest";
vi.mock("../../../wailsjs/go/app/App", () => ({
  RunLoad: vi.fn().mockResolvedValue({
    run: { id: 1, status: "running" },
    dispatches: [{ id: 9, runId: 1, status: "running", parentDispatchId: 0 }],
  }),
}));
import { useOrchRunStore } from "../orch-run-store";

describe("orch-run-store", () => {
  beforeEach(() => useOrchRunStore.getState().__reset());
  it("loadRun 填充详情", async () => {
    await useOrchRunStore.getState().loadRun(1);
    expect(useOrchRunStore.getState().details.get(1)?.dispatches).toHaveLength(
      1,
    );
  });
  it("deadlock 事件记录环", () => {
    useOrchRunStore
      .getState()
      .onRunEvent("orch:run:deadlock", { runId: 1, cycle: [700, 800] });
    expect(useOrchRunStore.getState().deadlocks.get(1)).toEqual([700, 800]);
  });
  it("onRunEvent ask → activeAsks + askLog 增; reply → activeAsks 清、askLog 加 reply", () => {
    const s = useOrchRunStore.getState();
    s.onRunEvent("orch:run:ask", {
      runId: 1,
      askId: "k1",
      askerAgentId: 2,
      askerSessionId: 50,
      targetAgentId: 3,
      targetSessionId: 70,
      question: "鉴权?",
    } as never);
    expect(useOrchRunStore.getState().activeAsks.get(1)).toHaveLength(1);
    expect(useOrchRunStore.getState().askLog.get(1)).toHaveLength(1);
    s.onRunEvent("orch:run:reply", {
      runId: 1,
      askId: "k1",
      answer: "ok",
      timedOut: false,
    } as never);
    expect(useOrchRunStore.getState().activeAsks.get(1) ?? []).toHaveLength(0);
    expect(useOrchRunStore.getState().askLog.get(1)).toHaveLength(2);
  });

  it("reply log item 记录 targetAgentId = 原始 asker (askerAgentId)", () => {
    const s = useOrchRunStore.getState();
    // ask: askerAgentId=2, targetAgentId=3
    s.onRunEvent("orch:run:ask", {
      runId: 2,
      askId: "q1",
      askerAgentId: 2,
      targetAgentId: 3,
      question: "测试?",
    } as never);
    // reply: replier=targetAgentId(3), should record targetAgentId=askerAgentId(2)
    s.onRunEvent("orch:run:reply", {
      runId: 2,
      askId: "q1",
      answer: "回答",
      timedOut: false,
    } as never);
    const log = useOrchRunStore.getState().askLog.get(2) ?? [];
    const replyItem = log.find((i) => i.kind === "reply");
    expect(replyItem).toBeDefined();
    expect(replyItem!.targetAgentId).toBe(2); // original asker
  });
});
