import "@testing-library/jest-dom/vitest";

import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const gitChangesMock = vi.fn();
const gitBranchesMock = vi.fn();
vi.mock("@/../wailsjs/go/app/App", () => ({
  OpenPath: vi.fn(),
  WorkspaceFsListDir: vi.fn(),
  WorkspaceFsGitChanges: (sessionId: number, scope: string, baseRef: string) =>
    gitChangesMock(sessionId, scope, baseRef),
  WorkspaceFsGitBranches: (sessionId: number) => gitBranchesMock(sessionId),
}));

import { useChatSidebarStore } from "@/stores/chat-sidebar-store";

import { ChatContextSidebar } from "../index";

import type { chat_svc } from "../../../../../wailsjs/go/models";

type Msg = chat_svc.ChatMessage;

function gitChangesView(
  seeds: { path: string }[],
  extra: { notARepo?: boolean; baseRef?: string } = {},
) {
  return {
    notARepo: false,
    baseRef: "",
    truncated: false,
    ...extra,
    changes: seeds.map((seed) => ({
      path: seed.path,
      oldPath: "",
      status: "modified",
      added: 0,
      deleted: 0,
      binary: false,
    })),
  };
}

const userM = (id: number, text: string): Msg =>
  ({
    id,
    role: "user",
    sessionId: 1,
    blocks: [{ type: "text", text }],
    model: "",
    promptTokens: 0,
    completionTokens: 0,
    durationMs: 0,
    errorText: "",
    seq: 0,
    createtime: 0,
  }) as unknown as Msg;

const assistantM = (id: number, text: string): Msg =>
  ({
    id,
    role: "assistant",
    sessionId: 1,
    blocks: [{ type: "text", text }],
    model: "",
    promptTokens: 0,
    completionTokens: 0,
    durationMs: 0,
    errorText: "",
    seq: 0,
    createtime: 0,
  }) as unknown as Msg;

describe("ChatContextSidebar", () => {
  beforeEach(() => {
    localStorage.clear();
    useChatSidebarStore.setState({ open: true, activeTab: "outline" });
    gitChangesMock.mockReset();
    gitChangesMock.mockResolvedValue(gitChangesView([]));
    gitBranchesMock.mockReset();
    gitBranchesMock.mockResolvedValue({
      notARepo: false,
      currentBranch: "",
      defaultBaseline: "",
      branches: [],
    });
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("shows OutlineView by default", () => {
    render(
      <ChatContextSidebar
        sessionId={1}
        messages={[userM(1, "hello world")]}
        activeMessageId={null}
        onJumpToMessage={() => {}}
      />,
    );
    expect(screen.getByText("hello world")).toBeInTheDocument();
  });

  it("does not render the legacy git Context block at the top", () => {
    render(
      <ChatContextSidebar
        sessionId={1}
        messages={[userM(1, "hello world")]}
        activeMessageId={null}
        onJumpToMessage={() => {}}
      />,
    );
    expect(screen.queryByText("Context")).not.toBeInTheDocument();
    expect(screen.queryByTestId("branch-chip")).not.toBeInTheDocument();
  });

  it("exposes a resize separator on the left edge", () => {
    render(
      <ChatContextSidebar
        sessionId={1}
        messages={[userM(1, "hello world")]}
        activeMessageId={null}
        onJumpToMessage={() => {}}
      />,
    );
    const sep = screen.getByRole("separator");
    expect(sep).toHaveAttribute("aria-orientation", "vertical");
  });

  it("switches to FilesView when Files tab clicked", async () => {
    render(
      <ChatContextSidebar
        sessionId={1}
        messages={[userM(1, "hello world")]}
        activeMessageId={null}
        onJumpToMessage={() => {}}
      />,
    );
    await userEvent.click(screen.getByRole("tab", { name: /files/i }));
    expect(
      screen.getByText(/No files have been changed in this session/),
    ).toBeInTheDocument();
    expect(useChatSidebarStore.getState().activeTab).toBe("files");
  });

  it("calls onJumpToMessage when outline row clicked", async () => {
    const onJump = vi.fn();
    render(
      <ChatContextSidebar
        sessionId={1}
        messages={[userM(99, "hello world")]}
        activeMessageId={null}
        onJumpToMessage={onJump}
      />,
    );
    await userEvent.click(screen.getByText("hello world"));
    expect(onJump).toHaveBeenCalledWith(99);
  });

  it("highlights the outline row matching activeMessageId", () => {
    render(
      <ChatContextSidebar
        sessionId={1}
        messages={[userM(11, "first"), userM(22, "second")]}
        activeMessageId={22}
        onJumpToMessage={() => {}}
      />,
    );
    const active = document.querySelector('[data-outline-message-id="22"]');
    const inactive = document.querySelector('[data-outline-message-id="11"]');
    expect(active).toHaveAttribute("data-active", "true");
    expect(inactive).toHaveAttribute("data-active", "false");
  });

  it("highlights the turn's user row when activeMessageId points to an assistant reply in that turn", () => {
    render(
      <ChatContextSidebar
        sessionId={1}
        messages={[
          userM(11, "first"),
          assistantM(12, "reply 1"),
          userM(22, "second"),
          assistantM(23, "reply 2"),
        ]}
        activeMessageId={12}
        onJumpToMessage={() => {}}
      />,
    );
    expect(
      document.querySelector('[data-outline-message-id="11"]'),
    ).toHaveAttribute("data-active", "true");
    expect(
      document.querySelector('[data-outline-message-id="22"]'),
    ).toHaveAttribute("data-active", "false");
  });

  it("switches the highlight to the next turn's user row once a later turn's assistant becomes active", () => {
    render(
      <ChatContextSidebar
        sessionId={1}
        messages={[
          userM(11, "first"),
          assistantM(12, "reply 1"),
          userM(22, "second"),
          assistantM(23, "reply 2"),
        ]}
        activeMessageId={23}
        onJumpToMessage={() => {}}
      />,
    );
    expect(
      document.querySelector('[data-outline-message-id="11"]'),
    ).toHaveAttribute("data-active", "false");
    expect(
      document.querySelector('[data-outline-message-id="22"]'),
    ).toHaveAttribute("data-active", "true");
  });

  it("Given the transcript focus reaches the last turn, When the active row changes, Then the outline scrolls that row to the bottom edge", () => {
    const scrollIntoView = vi
      .spyOn(HTMLElement.prototype, "scrollIntoView")
      .mockImplementation(() => {});

    render(
      <ChatContextSidebar
        sessionId={1}
        messages={[userM(11, "first"), userM(22, "second")]}
        activeMessageId={22}
        onJumpToMessage={() => {}}
      />,
    );

    expect(scrollIntoView).toHaveBeenCalledWith({
      block: "end",
      inline: "nearest",
    });
  });

  it("Given no transcript message is active, When the outline renders, Then it does not adjust scroll position", () => {
    const scrollIntoView = vi
      .spyOn(HTMLElement.prototype, "scrollIntoView")
      .mockImplementation(() => {});

    render(
      <ChatContextSidebar
        sessionId={1}
        messages={[userM(11, "first"), userM(22, "second")]}
        activeMessageId={null}
        onJumpToMessage={() => {}}
      />,
    );

    expect(scrollIntoView).not.toHaveBeenCalled();
  });
});

describe("ChatContextSidebar top-level tabs", () => {
  beforeEach(() => {
    localStorage.clear();
    useChatSidebarStore.setState({ open: true, activeTab: "outline" });
    gitChangesMock.mockReset();
    gitChangesMock.mockResolvedValue(gitChangesView([]));
    gitBranchesMock.mockReset();
    gitBranchesMock.mockResolvedValue({
      notARepo: false,
      currentBranch: "",
      defaultBaseline: "",
      branches: [],
    });
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("renders three equal-width, iconless tabs showing 大纲/文件/Git counts, exactly one selected", () => {
    render(
      <ChatContextSidebar
        sessionId={1}
        messages={[userM(1, "first"), userM(2, "second")]}
        activeMessageId={null}
        onJumpToMessage={() => {}}
      />,
    );

    const tabs = screen.getAllByRole("tab");
    expect(tabs).toHaveLength(3);
    expect(tabs.map((tab) => tab.textContent)).toEqual([
      "Outline2",
      "Files0",
      "Git",
    ]);
    // 不带图标。
    for (const tab of tabs) {
      expect(tab.querySelector("svg")).toBeNull();
    }
    // 恰有一段选中（大纲），其余未选中。
    expect(tabs.map((tab) => tab.getAttribute("aria-selected"))).toEqual([
      "true",
      "false",
      "false",
    ]);
    expect(screen.getByRole("tablist")).toContainElement(tabs[0]);
  });

  it("shows no count on the Git tab until its data has loaded", () => {
    render(
      <ChatContextSidebar
        sessionId={1}
        messages={[]}
        activeMessageId={null}
        onJumpToMessage={() => {}}
        cwd="/repo"
      />,
    );

    const gitTab = screen.getByRole("tab", { name: /^git/i });
    expect(gitTab).toHaveTextContent("Git");
    expect(gitTab.textContent).toBe("Git");
    expect(gitChangesMock).not.toHaveBeenCalled();
  });

  it("switches the selected tab on click and persists it to the sidebar store", async () => {
    render(
      <ChatContextSidebar
        sessionId={1}
        messages={[]}
        activeMessageId={null}
        onJumpToMessage={() => {}}
        cwd="/repo"
      />,
    );

    await userEvent.click(screen.getByRole("tab", { name: /^git/i }));
    expect(useChatSidebarStore.getState().activeTab).toBe("git");
    expect(screen.getByRole("tab", { name: /^git/i })).toHaveAttribute(
      "aria-selected",
      "true",
    );
    expect(screen.getByRole("tab", { name: /^outline/i })).toHaveAttribute(
      "aria-selected",
      "false",
    );
  });

  it("keeps the outline page's chrome to a single row (only the top tab bar)", () => {
    render(
      <ChatContextSidebar
        sessionId={1}
        messages={[]}
        activeMessageId={null}
        onJumpToMessage={() => {}}
      />,
    );

    expect(screen.getAllByRole("tablist")).toHaveLength(1);
  });

  it("keeps the files page's chrome to two rows: top tab bar + changes/directory pill group", async () => {
    render(
      <ChatContextSidebar
        sessionId={1}
        messages={[]}
        activeMessageId={null}
        onJumpToMessage={() => {}}
        cwd="/repo"
      />,
    );

    await userEvent.click(screen.getByRole("tab", { name: /^files/i }));
    expect(screen.getAllByRole("tablist")).toHaveLength(2);
  });

  it("keeps the git page's chrome to two rows for a repo and one row when there is no working directory", async () => {
    const { rerender } = render(
      <ChatContextSidebar
        sessionId={1}
        messages={[]}
        activeMessageId={null}
        onJumpToMessage={() => {}}
        cwd="/repo"
      />,
    );

    await userEvent.click(screen.getByRole("tab", { name: /^git/i }));
    await waitFor(() => expect(gitChangesMock).toHaveBeenCalled());
    await waitFor(() => expect(screen.getAllByRole("tablist")).toHaveLength(2));

    rerender(
      <ChatContextSidebar
        sessionId={1}
        messages={[]}
        activeMessageId={null}
        onJumpToMessage={() => {}}
        cwd=""
      />,
    );
    expect(screen.getAllByRole("tablist")).toHaveLength(1);
  });

  it("shows the git page's context bar as one row and the file list as content when it is a repo, matching the two-row chrome budget", async () => {
    gitChangesMock.mockResolvedValue(
      gitChangesView([{ path: "a.go" }, { path: "b.go" }]),
    );
    render(
      <ChatContextSidebar
        sessionId={1}
        messages={[]}
        activeMessageId={null}
        onJumpToMessage={() => {}}
        cwd="/repo"
      />,
    );

    await userEvent.click(screen.getByRole("tab", { name: /^git/i }));
    await waitFor(() =>
      expect(screen.getByRole("tab", { name: /^git/i })).toHaveTextContent(
        "Git2",
      ),
    );
    const scopeBar = screen.getByRole("tablist", {
      name: /comparison scope/i,
    });
    expect(
      within(scopeBar).getByRole("tab", { name: /uncommitted/i }),
    ).toBeInTheDocument();
  });
});
