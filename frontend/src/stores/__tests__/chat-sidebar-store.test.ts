import { beforeEach, describe, expect, it } from "vitest";

import {
  useChatSidebarStore,
  type ChatSidebarTab,
} from "../chat-sidebar-store";

describe("chat-sidebar-store", () => {
  beforeEach(() => {
    localStorage.clear();
    useChatSidebarStore.setState({ open: true, activeTab: "outline" });
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
});
