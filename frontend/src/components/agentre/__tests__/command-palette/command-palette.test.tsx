import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { ChatAgentItem } from "@/hooks/use-chat-agents";
import { useChatTabsStore } from "@/stores/chat-tabs-store";
import { useCommandPaletteStore } from "@/stores/command-palette-store";

// 直接 mock 整个 useChatAgents hook —— 避开 Wails 模块多层 mock 的解析路径风险。
const hookMocks = vi.hoisted(() => ({
  useChatAgents: vi.fn(),
}));

vi.mock("@/hooks/use-chat-agents", () => ({
  useChatAgents: hookMocks.useChatAgents,
}));

import { CommandPalette } from "../../command-palette";
import { ShortcutsProvider } from "../../shortcuts/shortcuts-provider";

const MOCK_AGENTS = [
  {
    id: 1,
    name: "CEO 助手",
    avatarColor: "agent-1",
    avatarIcon: "",
    avatarDataUrl: "",
    backendType: "",
    chattable: true,
    pinned: true,
    chattableHint: "",
    activeCount: 1,
    recentCount: 2,
    totalSessions: 2,
    sessions: [
      {
        id: 101,
        title: "年度报告 v2",
        status: "running",
        needsAttention: false,
        lastMessageAt: Date.now(),
      },
      {
        id: 102,
        title: "周报草稿",
        status: "idle",
        needsAttention: false,
        lastMessageAt: Date.now() - 10000,
      },
    ],
    attentionSessions: [],
  },
] as unknown as ChatAgentItem[];

beforeEach(() => {
  useCommandPaletteStore.setState({ open: false });
  useChatTabsStore.setState({ tabs: [], activeTabId: null });
  hookMocks.useChatAgents.mockReturnValue({
    agents: MOCK_AGENTS,
    loading: false,
    error: null,
    reload: vi.fn(),
  });
});

function renderPalette() {
  return render(
    <MemoryRouter initialEntries={["/projects"]}>
      <ShortcutsProvider platform="darwin">
        <CommandPalette />
      </ShortcutsProvider>
    </MemoryRouter>,
  );
}

describe("CommandPalette", () => {
  it("renders nothing when palette is closed", () => {
    renderPalette();
    expect(screen.queryByPlaceholderText("Search sessions...")).toBeNull();
  });

  it("opens via store and lists active-first sessions", async () => {
    renderPalette();
    act(() => useCommandPaletteStore.getState().setOpen(true));
    await waitFor(() =>
      expect(
        screen.getByPlaceholderText("Search sessions..."),
      ).toBeInTheDocument(),
    );
    expect(await screen.findByText("年度报告 v2")).toBeInTheDocument();
    expect(screen.getByText("周报草稿")).toBeInTheDocument();
  });

  it("filters by pinyin (ndbg → 年度报告)", async () => {
    renderPalette();
    act(() => useCommandPaletteStore.getState().setOpen(true));
    const input = await screen.findByPlaceholderText("Search sessions...");
    await screen.findByText("年度报告 v2");
    await userEvent.type(input, "ndbg");
    await waitFor(() => {
      expect(screen.queryByText("周报草稿")).toBeNull();
    });
    expect(screen.getByText("年度报告 v2")).toBeInTheDocument();
  });

  it("clicking a session opens a session tab and closes palette", async () => {
    renderPalette();
    act(() => useCommandPaletteStore.getState().setOpen(true));
    const row = await screen.findByText("年度报告 v2");
    await userEvent.click(row);
    await waitFor(() => {
      const { tabs, activeTabId } = useChatTabsStore.getState();
      expect(tabs).toHaveLength(1);
      expect(tabs[0].id).toBe(activeTabId);
      expect(tabs[0].meta).toEqual({ kind: "session", sessionId: 101 });
      expect(useCommandPaletteStore.getState().open).toBe(false);
    });
  });
});
