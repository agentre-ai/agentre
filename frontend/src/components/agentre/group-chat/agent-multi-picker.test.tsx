import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { AgentMultiPicker, type PickableAgent } from "./agent-multi-picker";

const agents: PickableAgent[] = [
  {
    id: 1,
    name: "云溪",
    avatarColor: "agent-1",
    avatarIcon: "",
    avatarDataUrl: "",
  },
  {
    id: 2,
    name: "影狼",
    avatarColor: "agent-2",
    avatarIcon: "",
    avatarDataUrl: "",
  },
  {
    id: 3,
    name: "石川",
    avatarColor: "agent-3",
    avatarIcon: "",
    avatarDataUrl: "",
  },
];

describe("AgentMultiPicker", () => {
  it("点候选把 agent 加入 value（onChange 收到新 id 列表）", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    const onChange = vi.fn();
    render(<AgentMultiPicker agents={agents} value={[]} onChange={onChange} />);
    // 测试 harness 跑 en，触发器渲染英文文案。
    await user.click(screen.getByRole("button", { name: "Add member" }));
    await user.click(screen.getByText("影狼"));
    expect(onChange).toHaveBeenCalledWith([2]);
  });

  it("exclude 的 agent 不出现在候选里", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    render(
      <AgentMultiPicker
        agents={agents}
        value={[]}
        onChange={() => {}}
        exclude={[1]}
      />,
    );
    await user.click(screen.getByRole("button", { name: "Add member" }));
    expect(screen.queryByText("云溪")).toBeNull();
    expect(screen.getByText("影狼")).toBeTruthy();
  });
});
