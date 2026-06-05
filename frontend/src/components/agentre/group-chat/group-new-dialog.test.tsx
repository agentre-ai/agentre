import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { GroupNewDialog } from "./group-new-dialog";

const groupCreate = vi.fn();
const groupListReload = vi.fn();
const openGroup = vi.fn();

vi.mock("../../../../wailsjs/go/app/App", () => ({
  GroupCreate: (...a: unknown[]) => groupCreate(...a),
}));
vi.mock("@/hooks/use-chat-agents", () => ({
  useChatAgents: () => ({
    agents: [
      {
        id: 1,
        name: "云溪",
        avatarColor: "agent-1",
        avatarIcon: "",
        avatarDataUrl: "",
        chattable: true,
        supportsGroup: true,
      },
      {
        id: 9,
        name: "Codex君",
        avatarColor: "agent-2",
        avatarIcon: "",
        avatarDataUrl: "",
        chattable: true,
        supportsGroup: false,
      },
    ],
    loading: false,
  }),
}));
vi.mock("@/hooks/use-project-list", () => ({
  useProjectList: () => ({
    projects: [{ id: 3, name: "Agentre" }],
    reload: vi.fn(),
  }),
}));
vi.mock("@/stores/group-list-store", () => ({
  useGroupListStore: { getState: () => ({ reload: groupListReload }) },
}));
vi.mock("@/stores/chat-tabs-store", () => ({
  useChatTabsStore: { getState: () => ({ openGroup }) },
}));
vi.mock("@/stores/new-chat-context-store", () => ({
  useNewChatContextStore: (
    selector: (s: { projectContext: null }) => unknown,
  ) => selector({ projectContext: null }),
}));

describe("GroupNewDialog", () => {
  beforeEach(() => {
    groupCreate
      .mockReset()
      .mockResolvedValue({ group: { id: 5, title: "新群" } });
    groupListReload.mockReset();
    openGroup.mockReset();
  });

  it("不支持群聊的 agent 不在协调者候选里", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    render(<GroupNewDialog open onOpenChange={() => {}} />);
    // 测试 harness 跑 en：触发器 aria-label = "Coordinator"。
    await user.click(screen.getByRole("combobox", { name: "Coordinator" }));
    expect(screen.queryByRole("option", { name: "Codex君" })).toBeNull();
    expect(screen.getByRole("option", { name: "云溪" })).toBeTruthy();
  });

  it("填标题 + 选协调者 → 提交调 GroupCreate 并打开群 tab", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    render(<GroupNewDialog open onOpenChange={() => {}} />);
    fireEvent.change(screen.getByRole("textbox", { name: "Group title" }), {
      target: { value: "支付小队" },
    });
    await user.click(screen.getByRole("combobox", { name: "Coordinator" }));
    await user.click(screen.getByRole("option", { name: "云溪" }));
    await user.click(screen.getByRole("button", { name: "Create group" }));
    await waitFor(() => expect(groupCreate).toHaveBeenCalled());
    expect(groupCreate.mock.calls[0][0]).toMatchObject({
      title: "支付小队",
      coordinatorAgentID: 1,
    });
    await waitFor(() => expect(openGroup).toHaveBeenCalledWith(5, "新群"));
    expect(groupListReload).toHaveBeenCalled();
  });
});
