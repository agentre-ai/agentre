import { describe, it, expect, vi, beforeEach } from "vitest";
vi.mock("../../../wailsjs/go/app/App", () => ({
  RunList: vi.fn().mockResolvedValue([{ id: 1, goal: "做登录页", status: "running" }]),
}));
import { useOrchRunListStore } from "../orch-run-list-store";

describe("orch-run-list-store", () => {
  beforeEach(() => useOrchRunListStore.getState().__reset());
  it("load 填充 runs", async () => {
    await useOrchRunListStore.getState().load();
    expect(useOrchRunListStore.getState().runs).toHaveLength(1);
    expect(useOrchRunListStore.getState().runs[0].goal).toBe("做登录页");
  });
});
