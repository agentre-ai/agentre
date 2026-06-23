import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { Tab } from "../tab";

describe("Tab 组件", () => {
  const baseProps = {
    title: "处理周一例会纪要",
    avatar: { letter: "C", color: "#3b82f6" },
    active: false,
    isPreview: false,
    isPinned: false,
    status: "idle" as const,
    projectColor: null as string | null,
    worktree: false,
    pillText: null as string | null,
    onActivate: vi.fn(),
    onClose: vi.fn(),
    onDoublePromote: vi.fn(),
  };

  it("渲染 title", () => {
    render(<Tab {...baseProps} />);
    expect(screen.getByText("处理周一例会纪要")).toBeInTheDocument();
  });

  it("限制最大宽度，长标题交给 CSS truncate", () => {
    render(<Tab {...baseProps} />);
    expect(screen.getByRole("tab")).toHaveClass("max-w-[240px]");
    expect(screen.getByText("处理周一例会纪要")).toHaveClass("truncate");
  });

  it("active=true 时加 data-active=true", () => {
    render(<Tab {...baseProps} active />);
    expect(screen.getByRole("tab")).toHaveAttribute("data-active", "true");
  });

  it("status='running' 时 close X 被替换成 spinner", () => {
    render(<Tab {...baseProps} status="running" />);
    expect(screen.getByTestId("tab-spinner")).toBeInTheDocument();
    expect(screen.queryByLabelText("Close Tab")).not.toBeInTheDocument();
  });

  it("pillText='审批' 渲染 pill", () => {
    render(<Tab {...baseProps} status="waiting" pillText="审批" />);
    expect(screen.getByText("审批")).toBeInTheDocument();
  });

  it("isPinned=true 显示 pin 图标 + 仍有 close X", () => {
    render(<Tab {...baseProps} isPinned />);
    expect(screen.getByTestId("tab-pin-icon")).toBeInTheDocument();
    expect(screen.getByLabelText("Close Tab")).toBeInTheDocument();
  });

  it("projectColor 设置 data-project-color", () => {
    render(<Tab {...baseProps} projectColor="#5b8def" />);
    expect(screen.getByRole("tab")).toHaveAttribute(
      "data-project-color",
      "#5b8def",
    );
  });

  it("kind='terminal' 显示终端图标(替代头像) + title 用传入的 title", () => {
    const { container } = render(
      <Tab {...baseProps} kind="terminal" title="终端 · MacMini" />,
    );
    expect(
      container.querySelector(".lucide-square-terminal"),
    ).toBeInTheDocument();
    expect(screen.getByText("终端 · MacMini")).toBeInTheDocument();
  });

  it("kind='group' 显示群组图标(users-round)替代字母头像,与普通 agent tab 区分", () => {
    const { container } = render(
      <Tab {...baseProps} kind="group" title="发布前回归小组" />,
    );
    expect(container.querySelector(".lucide-users-round")).toBeInTheDocument();
    // 群聊不展示 agent 风格的字母头像
    expect(screen.queryByText("C")).not.toBeInTheDocument();
    expect(screen.getByText("发布前回归小组")).toBeInTheDocument();
  });

  it("单击触发 onActivate", async () => {
    const user = userEvent.setup();
    const onActivate = vi.fn();
    render(<Tab {...baseProps} onActivate={onActivate} />);
    await user.click(screen.getByRole("tab"));
    expect(onActivate).toHaveBeenCalled();
  });

  it("Cmd+Click 不触发 onActivate (由父级 TabStrip 处理 sidebar 点击, 本地此事件交给父级)", async () => {
    const user = userEvent.setup();
    const onActivate = vi.fn();
    render(<Tab {...baseProps} onActivate={onActivate} />);
    await user.keyboard("{Meta>}");
    await user.click(screen.getByRole("tab"));
    await user.keyboard("{/Meta}");
    expect(onActivate).toHaveBeenCalled();
  });

  it("双击触发 onDoublePromote", async () => {
    const user = userEvent.setup();
    const onDoublePromote = vi.fn();
    render(<Tab {...baseProps} isPreview onDoublePromote={onDoublePromote} />);
    await user.dblClick(screen.getByRole("tab"));
    expect(onDoublePromote).toHaveBeenCalled();
  });

  it("点 close X 触发 onClose(并 stopPropagation)", async () => {
    const user = userEvent.setup();
    const onActivate = vi.fn();
    const onClose = vi.fn();
    render(<Tab {...baseProps} onActivate={onActivate} onClose={onClose} />);
    await user.click(screen.getByLabelText("Close Tab"));
    expect(onClose).toHaveBeenCalled();
    expect(onActivate).not.toHaveBeenCalled();
  });
});
