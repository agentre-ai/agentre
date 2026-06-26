import { describe, it, expect, vi, beforeEach } from "vitest";

const loadMock = vi.fn();
vi.mock("../../../wailsjs/go/app/App", () => ({
  LoadChatSession: (...a: unknown[]) => loadMock(...a),
}));

import { useOrchSubagentsStore } from "../orch-subagents-store";

beforeEach(() => {
  useOrchSubagentsStore.getState().__reset();
  loadMock.mockReset();
});

describe("orch-subagents-store", () => {
  it("ensureLoaded 调 LoadChatSession 并缓存 derive 结果", async () => {
    loadMock.mockResolvedValue({
      messages: [
        {
          blocks: [
            {
              type: "tool_use",
              toolUseId: "a",
              subagent: { kind: "local_agent", status: "completed" },
            },
          ],
        },
      ],
    });
    useOrchSubagentsStore.getState().ensureLoaded(501);
    // 等微任务队列 flush
    await vi.waitFor(() =>
      expect(useOrchSubagentsStore.getState().bySession.get(501)).toHaveLength(
        1,
      ),
    );
    expect(loadMock).toHaveBeenCalledTimes(1);
  });

  it("同 sessionId 不重复加载(已缓存或加载中)", async () => {
    loadMock.mockResolvedValue({ messages: [] });
    useOrchSubagentsStore.getState().ensureLoaded(7);
    useOrchSubagentsStore.getState().ensureLoaded(7);
    await vi.waitFor(() =>
      expect(useOrchSubagentsStore.getState().bySession.has(7)).toBe(true),
    );
    expect(loadMock).toHaveBeenCalledTimes(1);
  });

  it("sessionId=0 不加载", () => {
    useOrchSubagentsStore.getState().ensureLoaded(0);
    expect(loadMock).not.toHaveBeenCalled();
  });
});
