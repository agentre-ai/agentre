import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { app } from "../../../../wailsjs/go/models";

import { GroupTaskList } from "./group-task-list";

type Task = app.GroupTaskItem;

const NAMES: Record<number, string> = { 1: "主持人", 2: "前端" };
const memberName = (id: number) => NAMES[id] ?? `#${id}`;

function task(overrides: Partial<Task>): Task {
  return {
    id: 9,
    taskNo: 1,
    title: "重构设置页",
    brief: "按设计稿",
    creatorMemberID: 1,
    assigneeMemberID: 2,
    status: "open",
    result: "",
    parentTaskNo: 0,
    createtime: 0,
    updatetime: 0,
    ...overrides,
  } as unknown as Task;
}

describe("GroupTaskList", () => {
  it("进行中置顶、已结束(done/canceled)在下,组内按 #N 升序", () => {
    render(
      <GroupTaskList
        tasks={[
          task({ id: 12, taskNo: 4, status: "open", title: "代码审查" }),
          task({ id: 10, taskNo: 2, status: "done", title: "实现功能" }),
          task({ id: 11, taskNo: 3, status: "canceled", title: "废弃任务" }),
          task({ id: 9, taskNo: 1, status: "open", title: "重构设置页" }),
        ]}
        memberName={memberName}
        onAnchorTask={vi.fn()}
        onOpenMember={vi.fn()}
      />,
    );
    const nos = screen.getAllByText(/^#\d+$/).map((el) => el.textContent);
    expect(nos).toEqual(["#1", "#4", "#2", "#3"]);
  });

  it("空列表渲染空态文案", () => {
    render(
      <GroupTaskList
        tasks={[]}
        memberName={memberName}
        onAnchorTask={vi.fn()}
        onOpenMember={vi.fn()}
      />,
    );
    expect(screen.getByText(/No tasks yet|暂无任务/)).toBeInTheDocument();
  });

  it("副行展示 assignee 与回指编号", () => {
    render(
      <GroupTaskList
        tasks={[task({ taskNo: 2, parentTaskNo: 1 })]}
        memberName={memberName}
        onAnchorTask={vi.fn()}
        onOpenMember={vi.fn()}
      />,
    );
    // 副行是单个文本块「前端 · ↳#1 · now」,合并断言避免命中头像等其他节点。
    expect(screen.getByText(/前端 · ↳#1/)).toBeInTheDocument();
  });

  it("点行回调 onAnchorTask,点行尾 › 回调 onOpenMember(assignee) 且不触发锚定", () => {
    const onAnchorTask = vi.fn();
    const onOpenMember = vi.fn();
    render(
      <GroupTaskList
        tasks={[task({})]}
        memberName={memberName}
        onAnchorTask={onAnchorTask}
        onOpenMember={onOpenMember}
      />,
    );
    fireEvent.click(screen.getByText("重构设置页"));
    expect(onAnchorTask).toHaveBeenCalledWith(
      expect.objectContaining({ id: 9 }),
    );

    fireEvent.click(
      screen.getByRole("button", {
        name: /Open assignee 前端|打开执行成员 前端/,
      }),
    );
    expect(onOpenMember).toHaveBeenCalledWith(2);
    expect(onAnchorTask).toHaveBeenCalledTimes(1);
  });
});
