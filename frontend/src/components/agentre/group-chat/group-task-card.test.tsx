import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { app } from "../../../../wailsjs/go/models";

import { GroupTaskCard } from "./group-task-card";

const NAMES: Record<number, string> = { 1: "主持人", 2: "前端" };
const memberName = (id: number) => NAMES[id] ?? `#${id}`;

function task(overrides: Partial<app.GroupTaskItem> = {}): app.GroupTaskItem {
  return {
    id: 9,
    taskNo: 3,
    title: "重构设置页",
    brief: "按设计稿重构,验收:vitest 全绿",
    creatorMemberID: 1,
    assigneeMemberID: 2,
    status: "open",
    result: "",
    parentTaskNo: 0,
    createtime: 1000,
    updatetime: 1000,
    ...overrides,
  } as app.GroupTaskItem;
}

describe("GroupTaskCard", () => {
  it("created 卡:序号/标题/brief/进行中 pill/指派给 @assignee", () => {
    const onJumpMember = vi.fn();
    render(
      <GroupTaskCard
        task={task()}
        taskEvent="created"
        messageId={42}
        memberName={memberName}
        onJumpMember={onJumpMember}
      />,
    );
    expect(screen.getByText("#3")).toBeInTheDocument();
    expect(screen.getByText("重构设置页")).toBeInTheDocument();
    expect(screen.getByText(/按设计稿重构/)).toBeInTheDocument();
    expect(screen.getByText(/In progress|进行中/)).toBeInTheDocument();
    expect(screen.getByText(/Assigned to|指派给/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "@前端" }));
    expect(onJumpMember).toHaveBeenCalledWith(2);
  });

  it("completed 卡:展示 result(非 brief)/已完成 pill/交付给 @creator", () => {
    const onJumpMember = vi.fn();
    render(
      <GroupTaskCard
        task={task({ status: "done", result: "改了 settings.tsx,自测通过" })}
        taskEvent="completed"
        messageId={43}
        memberName={memberName}
        onJumpMember={onJumpMember}
      />,
    );
    expect(screen.getByText(/改了 settings\.tsx/)).toBeInTheDocument();
    expect(screen.queryByText(/按设计稿重构/)).toBeNull();
    expect(screen.getByText(/Done|已完成/)).toBeInTheDocument();
    expect(screen.getByText(/Delivered to|交付给/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "@主持人" }));
    expect(onJumpMember).toHaveBeenCalledWith(1);
  });

  it("canceled 卡:已取消 pill,仍指派给 @assignee", () => {
    const onJumpMember = vi.fn();
    render(
      <GroupTaskCard
        task={task({ status: "canceled" })}
        taskEvent="canceled"
        messageId={44}
        memberName={memberName}
        onJumpMember={onJumpMember}
      />,
    );
    expect(screen.getByText(/Canceled|已取消/)).toBeInTheDocument();
    expect(screen.getByText(/Assigned to|指派给/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "@前端" }));
    expect(onJumpMember).toHaveBeenCalledWith(2);
  });

  it("parentTaskNo>0 渲染「↳ 验证 #N」回指,点击回调父任务编号", () => {
    const onJumpTaskNo = vi.fn();
    render(
      <GroupTaskCard
        task={task({ parentTaskNo: 1 })}
        taskEvent="created"
        messageId={45}
        memberName={memberName}
        onJumpMember={vi.fn()}
        onJumpTaskNo={onJumpTaskNo}
      />,
    );
    fireEvent.click(
      screen.getByRole("button", { name: /Verifies #1|验证 #1/ }),
    );
    expect(onJumpTaskNo).toHaveBeenCalledWith(1);
  });

  it("状态 pill 跟实时任务实体走:rerender 翻转 status 后 pill 变化", () => {
    const { rerender } = render(
      <GroupTaskCard
        task={task()}
        taskEvent="created"
        messageId={46}
        memberName={memberName}
        onJumpMember={vi.fn()}
      />,
    );
    expect(screen.getByText(/In progress|进行中/)).toBeInTheDocument();
    rerender(
      <GroupTaskCard
        task={task({ status: "done", result: "ok" })}
        taskEvent="created"
        messageId={46}
        memberName={memberName}
        onJumpMember={vi.fn()}
      />,
    );
    expect(screen.getByText(/Done|已完成/)).toBeInTheDocument();
    // created 卡体仍是 brief(交付物在 completed 卡上)。
    expect(screen.getByText(/按设计稿重构/)).toBeInTheDocument();
  });
});
