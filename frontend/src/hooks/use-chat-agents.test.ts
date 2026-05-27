import { act, renderHook, waitFor } from "@testing-library/react";
import { describe, expect, it, vi, beforeEach } from "vitest";

vi.mock("../../wailsjs/go/app/App", () => ({
  ListChatAgents: vi.fn(),
}));

import { ListChatAgents } from "../../wailsjs/go/app/App";
import { useChatAgentsStore } from "../stores/chat-agents-store";
import { useChatAgents } from "./use-chat-agents";

const listChatAgents = ListChatAgents as ReturnType<typeof vi.fn>;

describe("useChatAgents", () => {
  beforeEach(() => {
    listChatAgents.mockReset();
    useChatAgentsStore.getState().__reset();
  });

  it("loads agents on mount", async () => {
    listChatAgents.mockResolvedValueOnce({
      agents: [
        { id: 1, name: "Eng", pinned: false, chattable: true, sessions: [] },
      ],
    });
    const { result } = renderHook(() => useChatAgents());
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.agents).toHaveLength(1);
    expect(result.current.error).toBeNull();
  });

  it("captures error", async () => {
    listChatAgents.mockRejectedValueOnce(new Error("boom"));
    const { result } = renderHook(() => useChatAgents());
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.error).toBe("boom");
  });

  it("reload re-fetches", async () => {
    listChatAgents.mockResolvedValueOnce({ agents: [] });
    const { result } = renderHook(() => useChatAgents());
    await waitFor(() => expect(result.current.loading).toBe(false));

    listChatAgents.mockResolvedValueOnce({
      agents: [
        { id: 2, name: "X", pinned: false, chattable: true, sessions: [] },
      ],
    });
    await act(async () => {
      await result.current.reload();
    });
    expect(result.current.agents).toHaveLength(1);
  });

  it("两个 hook 实例共享同一份 store, 只触发一次 ListChatAgents", async () => {
    listChatAgents.mockResolvedValue({ agents: [] });
    const { result: a } = renderHook(() => useChatAgents());
    const { result: b } = renderHook(() => useChatAgents());
    await waitFor(() => expect(a.current.loading).toBe(false));
    await waitFor(() => expect(b.current.loading).toBe(false));
    expect(listChatAgents).toHaveBeenCalledTimes(1);
  });
});
