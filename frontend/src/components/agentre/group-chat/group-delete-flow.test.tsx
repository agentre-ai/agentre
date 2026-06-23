import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, it, expect, vi } from "vitest";

import { useChatTabsStore } from "@/stores/chat-tabs-store";

const h = vi.hoisted(() => ({
  groupDelete: vi.fn(() => Promise.resolve()),
  groupReload: vi.fn(() => Promise.resolve()),
  agentsReload: vi.fn(() => Promise.resolve()),
}));

vi.mock("../../../hooks/use-group", () => ({
  useGroup: () => ({
    detail: {
      group: { id: 5, title: "队", runStatus: "idle", roundCount: 0 },
      members: [
        {
          id: 1,
          agentID: 2,
          role: "host",
          status: "active",
          backingSessionID: 11,
        },
        {
          id: 2,
          agentID: 3,
          role: "member",
          status: "active",
          backingSessionID: 12,
        },
      ],
      messages: [],
    },
    loading: false,
    reload: vi.fn(),
  }),
}));
vi.mock("../../../../wailsjs/go/app/App", () => ({
  GroupSend: vi.fn(),
  GroupStop: vi.fn(),
  GroupPause: vi.fn(),
  GroupResume: vi.fn(),
  GroupAddMember: vi.fn(),
  GroupRemoveMember: vi.fn(),
  GroupRename: vi.fn(),
  GroupDelete: h.groupDelete,
}));
vi.mock("../chat-panel", () => ({ ChatPanel: () => null }));
vi.mock("../../../hooks/use-chat-agents", () => ({
  useChatAgents: () => ({
    agents: [
      { id: 2, name: "后端" },
      { id: 3, name: "前端" },
    ],
    loading: false,
    error: null,
    reload: vi.fn(),
  }),
}));
// 群列表 / 会话列表 store:删除后应各被 reload 一次,让群与(被删的)成员会话立刻从侧栏消失。
vi.mock("@/stores/group-list-store", () => ({
  useGroupListStore: Object.assign(() => ({ groups: [] }), {
    getState: () => ({ reload: h.groupReload }),
  }),
}));
vi.mock("@/stores/chat-agents-store", () => ({
  useChatAgentsStore: Object.assign(() => ({ agents: [] }), {
    getState: () => ({ reload: h.agentsReload }),
  }),
}));

import { GroupChat } from "./index";

describe("GroupChat delete flow", () => {
  beforeEach(() => {
    useChatTabsStore.setState({ tabs: [], activeTabId: null });
    h.groupDelete.mockClear();
    h.groupReload.mockClear();
    h.agentsReload.mockClear();
  });

  it("confirming delete calls GroupDelete, reloads group + agents stores, and closes the group tab", async () => {
    useChatTabsStore.getState().openGroup(5, "队");
    render(
      <MemoryRouter>
        <GroupChat groupId={5} />
      </MemoryRouter>,
    );

    fireEvent.click(screen.getByText(/Settings|设置/));
    fireEvent.click(
      screen.getByRole("button", { name: /Delete group|删除群/ }),
    );
    // 不勾选「同时删除关联会话」→ deleteSessions=false。
    fireEvent.click(screen.getByRole("button", { name: /^Delete$|^删除$/ }));

    await waitFor(() => expect(h.groupDelete).toHaveBeenCalledWith(5, false));
    await waitFor(() => expect(h.groupReload).toHaveBeenCalledTimes(1));
    // 关键回归:必须同时刷新会话列表,否则 deleteSessions=true 时被软删的成员 backing
    // session 仍残留侧栏(各 agent recent/attention/运行灯)直到下次手动 reload。
    await waitFor(() => expect(h.agentsReload).toHaveBeenCalledTimes(1));
    await waitFor(() =>
      expect(
        useChatTabsStore
          .getState()
          .tabs.some((t) => t.meta.kind === "group" && t.meta.groupId === 5),
      ).toBe(false),
    );
  });

  it("also closes the deleted group's member-session tabs, but leaves other groups' tabs open", async () => {
    const tabs = useChatTabsStore.getState();
    tabs.openGroup(5, "队");
    tabs.openGroupMemberSession(5, 12, "前端"); // 该群成员会话 → 应一起关闭
    tabs.openGroupMemberSession(9, 99, "他群成员"); // 另一个群 → 必须保留
    render(
      <MemoryRouter>
        <GroupChat groupId={5} />
      </MemoryRouter>,
    );

    fireEvent.click(screen.getByText(/Settings|设置/));
    fireEvent.click(
      screen.getByRole("button", { name: /Delete group|删除群/ }),
    );
    fireEvent.click(screen.getByRole("button", { name: /^Delete$|^删除$/ }));

    await waitFor(() => expect(h.groupDelete).toHaveBeenCalled());
    await waitFor(() => {
      const remaining = useChatTabsStore.getState().tabs;
      expect(
        remaining.some((t) => t.meta.kind === "group" && t.meta.groupId === 5),
      ).toBe(false);
      expect(
        remaining.some(
          (t) => t.meta.kind === "groupSession" && t.meta.groupId === 5,
        ),
      ).toBe(false);
      // 别的群(9)的成员会话 tab 不受影响。
      expect(
        remaining.some(
          (t) => t.meta.kind === "groupSession" && t.meta.groupId === 9,
        ),
      ).toBe(true);
    });
  });
});
