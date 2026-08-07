import { beforeEach, describe, expect, it } from "vitest";

import {
  useChatSidebarStore,
  type ChatFilesMode,
  type ChatSidebarTab,
} from "../chat-sidebar-store";

describe("chat-sidebar-store", () => {
  beforeEach(() => {
    localStorage.clear();
    useChatSidebarStore.setState({
      open: true,
      activeTab: "outline",
      filesMode: "changes",
      showIgnored: false,
    });
  });

  it("toggles open and persists to localStorage", () => {
    useChatSidebarStore.getState().setOpen(false);
    expect(useChatSidebarStore.getState().open).toBe(false);
    const raw = localStorage.getItem("chat-sidebar-state");
    expect(raw).toContain('"open":false');
  });

  it("switches activeTab between outline and files", () => {
    useChatSidebarStore.getState().setActiveTab("files");
    expect(useChatSidebarStore.getState().activeTab).toBe("files");
  });

  it("rejects unknown tab values at runtime by no-op", () => {
    useChatSidebarStore.getState().setActiveTab("bogus" as ChatSidebarTab);
    expect(useChatSidebarStore.getState().activeTab).toBe("outline");
  });

  it("defaults the files mode to changes and persists a switch", () => {
    expect(useChatSidebarStore.getState().filesMode).toBe("changes");
    useChatSidebarStore.getState().setFilesMode("directory");
    expect(useChatSidebarStore.getState().filesMode).toBe("directory");
    expect(localStorage.getItem("chat-sidebar-state")).toContain(
      '"filesMode":"directory"',
    );
  });

  it("rejects unknown files mode values at runtime by no-op", () => {
    useChatSidebarStore.getState().setFilesMode("bogus" as ChatFilesMode);
    expect(useChatSidebarStore.getState().filesMode).toBe("changes");
  });

  it("defaults showIgnored to false and persists a toggle", () => {
    expect(useChatSidebarStore.getState().showIgnored).toBe(false);
    useChatSidebarStore.getState().setShowIgnored(true);
    expect(useChatSidebarStore.getState().showIgnored).toBe(true);
    expect(localStorage.getItem("chat-sidebar-state")).toContain(
      '"showIgnored":true',
    );
  });

  it("falls back to changes when the persisted files mode is invalid", async () => {
    localStorage.setItem(
      "chat-sidebar-state",
      JSON.stringify({
        state: { open: true, activeTab: "files", filesMode: "bogus" },
        version: 0,
      }),
    );
    await useChatSidebarStore.persist.rehydrate();
    expect(useChatSidebarStore.getState().filesMode).toBe("changes");
  });

  it("falls back to changes when the persisted state has no files mode", async () => {
    localStorage.setItem(
      "chat-sidebar-state",
      JSON.stringify({ state: { open: true, activeTab: "files" }, version: 0 }),
    );
    await useChatSidebarStore.persist.rehydrate();
    expect(useChatSidebarStore.getState().filesMode).toBe("changes");
    expect(useChatSidebarStore.getState().showIgnored).toBe(false);
  });

  it("falls back to hiding ignored entries when the persisted flag is not a boolean", async () => {
    localStorage.setItem(
      "chat-sidebar-state",
      JSON.stringify({
        state: { open: true, activeTab: "files", showIgnored: "yes" },
        version: 0,
      }),
    );
    await useChatSidebarStore.persist.rehydrate();
    expect(useChatSidebarStore.getState().showIgnored).toBe(false);
  });
});
