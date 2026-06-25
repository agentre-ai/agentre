import { describe, it, expect, vi, beforeEach } from "vitest";
vi.mock("../../../wailsjs/go/app/App", () => ({
  RunLoad: vi.fn().mockResolvedValue({
    run: { id: 1, status: "running" },
    tasks: [{ id: 9, runId: 1, status: "running", parentTaskId: 0 }],
  }),
}));
import { useOrchRunStore } from "../orch-run-store";

describe("orch-run-store", () => {
  beforeEach(() => useOrchRunStore.getState().__reset());
  it("loadRun 填充详情", async () => {
    await useOrchRunStore.getState().loadRun(1);
    expect(useOrchRunStore.getState().details.get(1)?.tasks).toHaveLength(1);
  });
  it("deadlock 事件记录环", () => {
    useOrchRunStore
      .getState()
      .onRunEvent("orch:run:deadlock", { runId: 1, cycle: [700, 800] });
    expect(useOrchRunStore.getState().deadlocks.get(1)).toEqual([700, 800]);
  });
});
