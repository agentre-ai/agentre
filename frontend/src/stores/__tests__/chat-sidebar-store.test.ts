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
      gitBaselineBySession: {},
      previewBySession: {},
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

  it("stores the git baseline per session and persists it", () => {
    useChatSidebarStore.getState().setGitBaseline(7, "origin/main");
    useChatSidebarStore.getState().setGitBaseline(8, "develop/wyz");

    expect(useChatSidebarStore.getState().gitBaselineBySession).toEqual({
      7: "origin/main",
      8: "develop/wyz",
    });
    expect(localStorage.getItem("chat-sidebar-state")).toContain(
      '"7":"origin/main"',
    );
  });

  it("clears one session's baseline without touching the others", () => {
    useChatSidebarStore.getState().setGitBaseline(7, "origin/main");
    useChatSidebarStore.getState().setGitBaseline(8, "develop/wyz");

    useChatSidebarStore.getState().clearGitBaseline(7);

    expect(useChatSidebarStore.getState().gitBaselineBySession).toEqual({
      8: "develop/wyz",
    });
  });

  it("ignores a baseline write for a non-session id or an empty ref", () => {
    useChatSidebarStore.getState().setGitBaseline(0, "origin/main");
    useChatSidebarStore.getState().setGitBaseline(7, "");

    expect(useChatSidebarStore.getState().gitBaselineBySession).toEqual({});
  });

  it("drops persisted baselines that are not a session id to ref map", async () => {
    localStorage.setItem(
      "chat-sidebar-state",
      JSON.stringify({
        state: {
          open: true,
          activeTab: "files",
          gitBaselineBySession: {
            7: "origin/main",
            8: 42,
            notAnId: "main",
          },
        },
        version: 0,
      }),
    );
    await useChatSidebarStore.persist.rehydrate();

    expect(useChatSidebarStore.getState().gitBaselineBySession).toEqual({
      7: "origin/main",
    });
  });

  it("falls back to an empty baseline map when the persisted value is not an object", async () => {
    localStorage.setItem(
      "chat-sidebar-state",
      JSON.stringify({
        state: { open: true, gitBaselineBySession: "origin/main" },
        version: 0,
      }),
    );
    await useChatSidebarStore.persist.rehydrate();

    expect(useChatSidebarStore.getState().gitBaselineBySession).toEqual({});
  });

  describe("preview selection", () => {
    it("opens a file with a null segment (default) when nothing was selected", () => {
      useChatSidebarStore.getState().openPreview(7, "README.md");
      expect(useChatSidebarStore.getState().previewBySession[7]).toEqual({
        path: "README.md",
        segment: null,
      });
    });

    it("keeps the segment when switching files while the panel is open", () => {
      useChatSidebarStore.getState().openPreview(7, "README.md");
      useChatSidebarStore.getState().setPreviewSegment(7, "split");
      useChatSidebarStore.getState().openPreview(7, "guide.md");

      expect(useChatSidebarStore.getState().previewBySession[7]).toEqual({
        path: "guide.md",
        segment: "split",
      });
    });

    it("updates the segment for an existing selection only", () => {
      useChatSidebarStore.getState().openPreview(7, "a.go");
      useChatSidebarStore.getState().setPreviewSegment(7, "diff");
      expect(useChatSidebarStore.getState().previewBySession[7].segment).toBe(
        "diff",
      );

      // 没有选中文件时设置档位是 no-op。
      useChatSidebarStore.getState().setPreviewSegment(8, "text");
      expect(
        useChatSidebarStore.getState().previewBySession[8],
      ).toBeUndefined();
    });

    it("rejects an unknown segment at runtime by no-op", () => {
      useChatSidebarStore.getState().openPreview(7, "a.go");
      useChatSidebarStore.getState().setPreviewSegment(7, "bogus" as never);
      expect(
        useChatSidebarStore.getState().previewBySession[7].segment,
      ).toBeNull();
    });

    it("clears one session's preview without touching the others", () => {
      useChatSidebarStore.getState().openPreview(7, "a.md");
      useChatSidebarStore.getState().openPreview(8, "b.go");

      useChatSidebarStore.getState().clearPreview(7);

      expect(
        useChatSidebarStore.getState().previewBySession[7],
      ).toBeUndefined();
      expect(useChatSidebarStore.getState().previewBySession[8]).toEqual({
        path: "b.go",
        segment: null,
      });
    });

    it("ignores openPreview for a non-session id or an empty path", () => {
      useChatSidebarStore.getState().openPreview(0, "a.md");
      useChatSidebarStore.getState().openPreview(7, "");
      expect(useChatSidebarStore.getState().previewBySession).toEqual({});
    });

    it("drops persisted previews that are not a session id to selection map", async () => {
      localStorage.setItem(
        "chat-sidebar-state",
        JSON.stringify({
          state: {
            open: true,
            activeTab: "files",
            previewBySession: {
              7: { path: "README.md", segment: "split" },
              8: { path: "a.go" },
              9: { path: "" },
              notAnId: { path: "x.md" },
              10: { path: "y.md", segment: "bogus" },
            },
          },
          version: 0,
        }),
      );
      await useChatSidebarStore.persist.rehydrate();

      expect(useChatSidebarStore.getState().previewBySession).toEqual({
        7: { path: "README.md", segment: "split" },
        8: { path: "a.go", segment: null },
      });
    });
  });
});
