import { beforeEach, describe, expect, it, vi } from "vitest";

import { useNewChatContextStore } from "../new-chat-context-store";

describe("useNewChatContextStore", () => {
  beforeEach(() => {
    useNewChatContextStore.getState().clear();
  });

  it("defaults to null context and no handler", () => {
    const s = useNewChatContextStore.getState();
    expect(s.projectContext).toBeNull();
    expect(s.newSelectionHandler).toBeNull();
  });

  it("setContext stores project info", () => {
    useNewChatContextStore.getState().setContext({
      projectID: 7,
      projectName: "后端重构",
    });
    expect(useNewChatContextStore.getState().projectContext).toEqual({
      projectID: 7,
      projectName: "后端重构",
    });
  });

  it("setContext(null) clears the context", () => {
    useNewChatContextStore.getState().setContext({
      projectID: 7,
      projectName: "X",
    });
    useNewChatContextStore.getState().setContext(null);
    expect(useNewChatContextStore.getState().projectContext).toBeNull();
  });

  it("setNewSelectionHandler accepts and clears", () => {
    const fn = vi.fn();
    useNewChatContextStore.getState().setNewSelectionHandler(fn);
    expect(useNewChatContextStore.getState().newSelectionHandler).toBe(fn);
    useNewChatContextStore.getState().setNewSelectionHandler(null);
    expect(useNewChatContextStore.getState().newSelectionHandler).toBeNull();
  });

  it("clear resets context and handler", () => {
    useNewChatContextStore.getState().setContext({
      projectID: 1,
      projectName: "x",
    });
    useNewChatContextStore.getState().setNewSelectionHandler(vi.fn());
    useNewChatContextStore.getState().clear();
    const s = useNewChatContextStore.getState();
    expect(s.projectContext).toBeNull();
    expect(s.newSelectionHandler).toBeNull();
  });
});
