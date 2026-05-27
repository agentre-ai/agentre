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
    expect(screen.queryByLabelText("关闭 Tab")).not.toBeInTheDocument();
  });

  it("pillText='审批' 渲染 pill", () => {
    render(<Tab {...baseProps} status="waiting" pillText="审批" />);
    expect(screen.getByText("审批")).toBeInTheDocument();
  });

  it("isPinned=true 显示 pin 图标 + 仍有 close X", () => {
    render(<Tab {...baseProps} isPinned />);
    expect(screen.getByTestId("tab-pin-icon")).toBeInTheDocument();
    expect(screen.getByLabelText("关闭 Tab")).toBeInTheDocument();
  });

  it("projectColor 设置 data-project-color", () => {
    render(<Tab {...baseProps} projectColor="#5b8def" />);
    expect(screen.getByRole("tab")).toHaveAttribute(
      "data-project-color",
      "#5b8def",
    );
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
    await user.click(screen.getByLabelText("关闭 Tab"));
    expect(onClose).toHaveBeenCalled();
    expect(onActivate).not.toHaveBeenCalled();
  });
});
