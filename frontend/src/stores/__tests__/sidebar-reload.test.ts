import { beforeEach, describe, expect, it, vi } from "vitest";

// 只 mock 到 reload 的依赖边界: chat-agents-store / project-sessions-store 在 import
// 时会拉 App binding 与 project-tree hook, 这里替身掉, 让 sidebar-reload 的纯逻辑
// (是否已知 + 是否触发 reload) 可以脱离 Wails 单测。
vi.mock("../../../wailsjs/go/app/App", () => ({
  ListChatAgents: vi.fn(),
  ProjectListSessions: vi.fn(),
}));
vi.mock("@/hooks/use-project-tree", () => ({
  ensureProjectTreeLoaded: vi.fn(),
  isProjectTreeCacheLoaded: () => false,
}));

import { useChatAgentsStore, type AgentSlim } from "../chat-agents-store";
import { useProjectSessionsStore } from "../project-sessions-store";
import {
  ensureSessionInSidebar,
  isSessionKnownToSidebar,
} from "../sidebar-reload";

function seedAgents(...sessionIds: number[]) {
  useChatAgentsStore.setState({
    agents: [
      { id: 1, name: "Eng", sessions: [], sessionIds } as unknown as AgentSlim,
    ],
    loading: false,
    error: null,
  });
}

describe("sidebar-reload helpers", () => {
  beforeEach(() => {
    // spyOn 在「已被 spy 的方法」上会复用同一个 spy(连带其调用计数),
    // 不 restore 的话上一例的 reload 计数会漏到下一例 —— 每例先还原再重新 spy。
    vi.restoreAllMocks();
    useChatAgentsStore.getState().__reset();
    useProjectSessionsStore.getState().__reset();
  });

  describe("isSessionKnownToSidebar", () => {
    it("returns true when an agent already lists the session id", () => {
      seedAgents(99);
      expect(isSessionKnownToSidebar(99)).toBe(true);
    });

    it("returns false for a session id no agent lists", () => {
      seedAgents(99);
      expect(isSessionKnownToSidebar(11)).toBe(false);
    });

    it("returns false for non-positive ids", () => {
      seedAgents(99);
      expect(isSessionKnownToSidebar(0)).toBe(false);
      expect(isSessionKnownToSidebar(-1)).toBe(false);
    });
  });

  describe("ensureSessionInSidebar", () => {
    it("reloads both sidebar sources when the session is unknown", () => {
      seedAgents(99);
      const chatReload = vi
        .spyOn(useChatAgentsStore.getState(), "reload")
        .mockResolvedValue();
      const projReload = vi
        .spyOn(useProjectSessionsStore.getState(), "reload")
        .mockResolvedValue();

      ensureSessionInSidebar(11);

      expect(chatReload).toHaveBeenCalledTimes(1);
      expect(projReload).toHaveBeenCalledTimes(1);
    });

    it("does not reload when the session is already in the sidebar", () => {
      seedAgents(11);
      const chatReload = vi
        .spyOn(useChatAgentsStore.getState(), "reload")
        .mockResolvedValue();
      const projReload = vi
        .spyOn(useProjectSessionsStore.getState(), "reload")
        .mockResolvedValue();

      ensureSessionInSidebar(11);

      expect(chatReload).not.toHaveBeenCalled();
      expect(projReload).not.toHaveBeenCalled();
    });

    it("ignores non-positive session ids", () => {
      seedAgents(99);
      const chatReload = vi
        .spyOn(useChatAgentsStore.getState(), "reload")
        .mockResolvedValue();

      ensureSessionInSidebar(0);

      expect(chatReload).not.toHaveBeenCalled();
    });
  });
});
