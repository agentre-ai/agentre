import { render, screen, fireEvent } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { app } from "../../../../wailsjs/go/models";

import { GroupTranscript } from "./group-transcript";

const copySpy = vi.fn();
vi.mock("@/lib/clipboard-toast", () => ({
  copyTextWithToast: (text: string, opts: unknown) => {
    copySpy(text, opts);
    return Promise.resolve(true);
  },
}));

const roster = [
  { id: 1, agentID: 2, role: "host", status: "active", backingSessionID: 11 },
] as unknown as app.GroupMemberItem[];

function msg(over: Partial<app.GroupMessageItem>): app.GroupMessageItem {
  return {
    id: 1,
    seq: 1,
    senderKind: "agent",
    senderMemberID: 1,
    recipientMemberIDs: [],
    toUser: false,
    content: "hello from agent",
    createtime: 0,
    ...over,
  } as app.GroupMessageItem;
}

const memberName = (id: number) => (id === 1 ? "后端" : `#${id}`);

describe("GroupTranscript", () => {
  it("agent 消息渲染规范尺寸头像(size-7)", () => {
    render(
      <GroupTranscript
        messages={[msg({})]}
        roster={roster}
        memberName={memberName}
      />,
    );
    const avatar = screen.getByLabelText("后端");
    expect(avatar.className).toContain("size-7");
  });

  it("agent 消息有复制按钮，点击以正文调用 copyTextWithToast", () => {
    copySpy.mockClear();
    render(
      <GroupTranscript
        messages={[msg({ content: "hello from agent" })]}
        roster={roster}
        memberName={memberName}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: /Copy|复制/ }));
    expect(copySpy).toHaveBeenCalledWith(
      "hello from agent",
      expect.any(Object),
    );
  });

  it("默认正文渲染保持 pre-wrap 纯文本", () => {
    const { container } = render(
      <GroupTranscript
        messages={[msg({ content: "line1\nline2" })]}
        roster={roster}
        memberName={memberName}
      />,
    );
    expect(
      container.querySelector(".whitespace-pre-wrap")?.textContent,
    ).toContain("line1\nline2");
  });

  it("renderBody 拥有整块正文：transcript 不再包 pre-wrap 外壳", () => {
    const { container } = render(
      <GroupTranscript
        messages={[msg({ content: "**bold**" })]}
        roster={roster}
        memberName={memberName}
        renderBody={(content) => <div data-testid="custom-body">{content}</div>}
      />,
    );
    expect(screen.getByTestId("custom-body")).toHaveTextContent("**bold**");
    expect(container.querySelector(".whitespace-pre-wrap")).toBeNull();
  });

  it("system 行走 renderSystemBody,不进 renderBody", () => {
    const renderBody = vi.fn((content: string) => <div>{content}</div>);
    render(
      <GroupTranscript
        messages={[msg({ senderKind: "system", content: "X 加入了群聊" })]}
        roster={roster}
        memberName={memberName}
        renderBody={renderBody}
        renderSystemBody={(content) => (
          <span data-testid="system-body">{content}</span>
        )}
      />,
    );
    expect(screen.getByTestId("system-body")).toHaveTextContent("X 加入了群聊");
    expect(renderBody).not.toHaveBeenCalled();
  });

  it("system 行不渲染复制按钮", () => {
    render(
      <GroupTranscript
        messages={[msg({ senderKind: "system", content: "X 加入了群聊" })]}
        roster={roster}
        memberName={memberName}
      />,
    );
    expect(screen.queryByRole("button", { name: /Copy|复制/ })).toBeNull();
  });
});

// ── 任务卡渲染专项测试 ────────────────────────────────────────────────────────

type Msg = app.GroupMessageItem;
type Task = app.GroupTaskItem;

const NAMES: Record<number, string> = { 1: "主持人", 2: "前端" };
const memberName2 = (id: number) =>
  id === 0 ? "用户" : (NAMES[id] ?? `#${id}`);

const roster2 = [
  { id: 1, agentID: 2, role: "host", status: "active" },
  { id: 2, agentID: 3, role: "member", status: "active" },
] as unknown as app.GroupMemberItem[];

function msg2(overrides: Partial<Msg>): Msg {
  return {
    id: 1,
    seq: 1,
    senderKind: "agent",
    senderMemberID: 1,
    recipientMemberIDs: [],
    toUser: false,
    content: "",
    taskID: 0,
    taskEvent: "",
    createtime: 0,
    ...overrides,
  } as unknown as Msg;
}

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

function renderTranscript(messages: Msg[], tasks: Task[]) {
  const taskById = new Map(tasks.map((tk) => [tk.id, tk]));
  return render(
    <GroupTranscript
      messages={messages}
      roster={roster2}
      memberName={memberName2}
      taskById={taskById}
      onJumpMember={vi.fn()}
    />,
  );
}

describe("GroupTranscript 任务卡", () => {
  it("taskEvent != '' 的消息渲染任务卡而非正文", () => {
    renderTranscript(
      [
        msg2({
          id: 11,
          taskEvent: "created",
          taskID: 9,
          content: "(来自 主持人 的任务 #1) 重构设置页:按设计稿",
        }),
      ],
      [task({})],
    );
    expect(screen.getByTestId("group-task-card")).toBeInTheDocument();
    expect(screen.getByText("重构设置页")).toBeInTheDocument();
    // 原始消息正文(任务抬头)不再直出。
    expect(screen.queryByText(/来自 主持人 的任务/)).toBeNull();
  });

  it("同发送者连续两条 created 聚合为一行(一个头像行,两张卡并排)", () => {
    const { container } = renderTranscript(
      [
        msg2({ id: 11, taskEvent: "created", taskID: 9 }),
        msg2({ id: 12, taskEvent: "created", taskID: 10 }),
      ],
      [
        task({}),
        task({ id: 10, taskNo: 2, title: "e2e 验证", parentTaskNo: 1 }),
      ],
    );
    const cards = screen.getAllByTestId("group-task-card");
    expect(cards).toHaveLength(2);
    // 两张卡同处一个 MessageRow(article)。
    expect(cards[0].closest("article")).toBe(cards[1].closest("article"));
    // 发送者名字行只出现一次。
    expect(container.querySelectorAll("article")).toHaveLength(1);
  });

  it("发送者不同的任务消息不聚合", () => {
    const { container } = renderTranscript(
      [
        msg2({ id: 11, taskEvent: "created", taskID: 9, senderMemberID: 1 }),
        msg2({ id: 12, taskEvent: "completed", taskID: 9, senderMemberID: 2 }),
      ],
      [task({ status: "done", result: "改完了" })],
    );
    expect(container.querySelectorAll("article")).toHaveLength(2);
  });

  it("任务实体缺失时兜底渲染原文", () => {
    renderTranscript(
      [
        msg2({
          id: 11,
          taskEvent: "created",
          taskID: 404,
          content: "(来自 主持人 的任务 #7) 神秘任务",
        }),
      ],
      [],
    );
    expect(screen.queryByTestId("group-task-card")).toBeNull();
    expect(screen.getByText(/神秘任务/)).toBeInTheDocument();
  });

  it("历史 created 卡的状态 pill 随实时任务实体翻转", () => {
    const messages = [msg2({ id: 11, taskEvent: "created", taskID: 9 })];
    const { rerender } = renderTranscript(messages, [task({})]);
    expect(screen.getByText(/In progress|进行中/)).toBeInTheDocument();

    const flipped = new Map([[9, task({ status: "done", result: "ok" })]]);
    rerender(
      <GroupTranscript
        messages={messages}
        roster={roster2}
        memberName={memberName2}
        taskById={flipped}
        onJumpMember={vi.fn()}
      />,
    );
    expect(screen.getByText(/Done|已完成/)).toBeInTheDocument();
  });

  it("普通消息行携带 data-message-id(锚定用)", () => {
    const { container } = renderTranscript(
      [msg2({ id: 21, content: "普通发言" })],
      [],
    );
    expect(
      container.querySelector('article[data-message-id="21"]'),
    ).not.toBeNull();
  });

  it("任务卡行与卡片均携带 data-message-id(锚定用,含兜底行)", () => {
    const { container } = renderTranscript(
      [msg2({ id: 11, taskEvent: "created", taskID: 404, content: "兜底" })],
      [],
    );
    // 实体缺失的兜底行也能被锚定(article 上的 data-message-id)。
    expect(
      container.querySelector('article[data-message-id="11"]'),
    ).not.toBeNull();
  });
});
