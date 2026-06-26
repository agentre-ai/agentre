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

  it("ensureLoaded 同时缓存原始 messages, messagesFor 可取", async () => {
    loadMock.mockResolvedValue({
      messages: [{ id: 1, blocks: [{ type: "text", text: "hi" }] }],
    });
    useOrchSubagentsStore.getState().ensureLoaded(801);
    await vi.waitFor(() =>
      expect(useOrchSubagentsStore.getState().messagesFor(801)).toHaveLength(1),
    );
  });

  it("未加载的 session messagesFor 返回空数组", () => {
    expect(useOrchSubagentsStore.getState().messagesFor(999)).toEqual([]);
  });

  it("reload 即使 session 已缓存也重新拉取 LoadChatSession", async () => {
    // 先 ensureLoaded 缓存 1 条消息
    loadMock.mockResolvedValueOnce({
      messages: [{ id: 1, blocks: [] }],
    });
    useOrchSubagentsStore.getState().ensureLoaded(901);
    await vi.waitFor(() =>
      expect(useOrchSubagentsStore.getState().messagesFor(901)).toHaveLength(1),
    );
    expect(loadMock).toHaveBeenCalledTimes(1);

    // reload 绕过缓存，重新拉取（返回 2 条消息）
    loadMock.mockResolvedValueOnce({
      messages: [
        { id: 1, blocks: [] },
        { id: 2, blocks: [] },
      ],
    });
    useOrchSubagentsStore.getState().reload(901);
    await vi.waitFor(() =>
      expect(useOrchSubagentsStore.getState().messagesFor(901)).toHaveLength(2),
    );
    expect(loadMock).toHaveBeenCalledTimes(2);
  });

  it("reload 对 sessionId=0 是 no-op", () => {
    useOrchSubagentsStore.getState().reload(0);
    expect(loadMock).not.toHaveBeenCalled();
  });
});
