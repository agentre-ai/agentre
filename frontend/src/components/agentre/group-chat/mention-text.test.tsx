import { render, screen, fireEvent } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";

import { MarkdownText } from "../markdown-text";

import { MentionText, mentionMarkdownDecorator } from "./mention-text";

describe("MentionText", () => {
  const roster = [
    { memberId: 2, name: "后端" },
    { memberId: 3, name: "前端" },
  ];
  it("renders matched @name as a clickable chip, plain text otherwise", () => {
    const onJump = vi.fn();
    render(
      <MentionText
        text="麻烦 @后端 看下 @陌生人"
        roster={roster}
        onJump={onJump}
      />,
    );
    fireEvent.click(screen.getByText("@后端"));
    expect(onJump).toHaveBeenCalledWith(2);
    expect(screen.getByText(/陌生人/)).toBeInTheDocument(); // stranger stays plain text
  });
  it("also recognizes <mention>name</mention> markup", () => {
    const onJump = vi.fn();
    render(
      <MentionText
        text="好的 <mention>前端</mention>"
        roster={roster}
        onJump={onJump}
      />,
    );
    fireEvent.click(screen.getByText("@前端"));
    expect(onJump).toHaveBeenCalledWith(3);
  });
  it("renders multi-word names and does not chip @Bob inside @Bobby", () => {
    const onJump = vi.fn();
    render(
      <MentionText
        text="hi @Bobby ping @Code Reviewer"
        roster={[
          { memberId: 7, name: "Bob" },
          { memberId: 8, name: "Bobby" },
          { memberId: 4, name: "Code Reviewer" },
        ]}
        onJump={onJump}
      />,
    );
    fireEvent.click(screen.getByText("@Bobby"));
    expect(onJump).toHaveBeenCalledWith(8); // Bobby, not Bob(7)
    fireEvent.click(screen.getByText("@Code Reviewer"));
    expect(onJump).toHaveBeenCalledWith(4); // full multi-word name
    expect(screen.queryByText("@Bob")).not.toBeInTheDocument();
  });
});

describe("mentionMarkdownDecorator + MarkdownText", () => {
  const roster = [
    { memberId: 2, name: "后端" },
    { memberId: 3, name: "前端" },
  ];

  it("renders markdown formatting and @mention chips together", () => {
    const onJump = vi.fn();
    const { container } = render(
      <MarkdownText
        text="请看 **重点**,麻烦 @后端 跟进 @陌生人"
        decorator={mentionMarkdownDecorator(roster, onJump)}
      />,
    );
    expect(container.querySelector("strong")?.textContent).toBe("重点");
    fireEvent.click(screen.getByRole("button", { name: "@后端" }));
    expect(onJump).toHaveBeenCalledWith(2);
    expect(screen.getByText(/陌生人/)).toBeInTheDocument(); // 未命中 roster 保持纯文本
  });

  it("does not chip @name inside code blocks", () => {
    const onJump = vi.fn();
    render(
      <MarkdownText
        text={"运行 `@后端` 字面量:\n\n```\n@前端\n```\n"}
        decorator={mentionMarkdownDecorator(roster, onJump)}
      />,
    );
    expect(screen.queryByRole("button", { name: /@后端|@前端/ })).toBeNull();
  });
});
