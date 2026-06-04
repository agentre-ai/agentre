import { render, screen, fireEvent } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";

// mock useGroup to return a fixed detail (so the panel renders deterministically)
vi.mock("../../../hooks/use-group", () => ({
  useGroup: () => ({
    detail: {
      group: { id: 5, title: "队", runStatus: "running", roundCount: 3 },
      members: [
        {
          id: 1,
          agentID: 2,
          role: "coordinator",
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
      messages: [
        {
          id: 1,
          seq: 1,
          senderKind: "user",
          senderMemberID: 0,
          recipientMemberIDs: [1],
          toUser: false,
          content: "开工",
          createtime: 0,
        },
      ],
    },
    loading: false,
    reload: vi.fn(),
  }),
}));
// mock the Wails bindings + ChatPanel embed (ChatPanel is heavy; stub it)
vi.mock("../../../../wailsjs/go/app/App", () => ({
  GroupSend: vi.fn(),
  GroupStop: vi.fn(),
  GroupPause: vi.fn(),
  GroupResume: vi.fn(),
  GroupAddMember: vi.fn(),
  GroupRemoveMember: vi.fn(),
  GroupRename: vi.fn(),
  GroupArchive: vi.fn(),
}));
vi.mock("../chat-panel", () => ({ ChatPanel: () => null }));
// mock useChatAgents so the panel resolves real agent names deterministically
// (agentID 2 → "后端", 3 → "前端") without hitting the ListChatAgents binding.
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

import { GroupChat } from "./index";

describe("GroupChat", () => {
  it("renders room title, run status pill and member roster", () => {
    render(<GroupChat groupId={5} />);
    expect(screen.getByText("队")).toBeInTheDocument(); // dynamic title
    expect(screen.getByText(/Running|运行中/)).toBeInTheDocument(); // run_status pill (en default in tests)
    expect(screen.getByText(/Coordinator|协调者/)).toBeInTheDocument(); // members tab default, coordinator section
  });
  it("switches right panel to settings tab", () => {
    render(<GroupChat groupId={5} />);
    fireEvent.click(screen.getByText(/Settings|设置/));
    expect(screen.getByText(/Working directory|工作目录/)).toBeInTheDocument();
  });
});
