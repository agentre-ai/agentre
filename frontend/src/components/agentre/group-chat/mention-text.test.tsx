import { render, screen, fireEvent } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";

import { MentionText } from "./mention-text";

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
