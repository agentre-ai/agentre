import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { MentionPopover } from "../mention-popover";
import type { MentionItem, MentionMenuState } from "../types";

const items: MentionItem[] = [
  { kind: "agent", refId: 12, label: "Reviewer", color: "agent-3" },
  { kind: "project", refId: 3, label: "Web", path: "/w", color: "agent-5" },
];

const state: MentionMenuState = {
  open: true,
  anchorRect: { left: 10, top: 40, bottom: 60 },
  items,
  selectedIndex: 0,
  query: "",
};

describe("MentionPopover", () => {
  it("renders grouped agent + project items", () => {
    render(<MentionPopover state={state} onPick={vi.fn()} onHover={vi.fn()} />);
    expect(screen.getByRole("listbox")).toBeInTheDocument();
    expect(screen.getByText("Reviewer")).toBeInTheDocument();
    expect(screen.getByText("Web")).toBeInTheDocument();
    // section headers present
    expect(screen.getByText("Agents")).toBeInTheDocument();
    expect(screen.getByText("Projects")).toBeInTheDocument();
  });

  it("calls onPick on mousedown", () => {
    const onPick = vi.fn();
    render(<MentionPopover state={state} onPick={onPick} onHover={vi.fn()} />);
    screen
      .getByText("Reviewer")
      .closest("button")!
      .dispatchEvent(
        new MouseEvent("mousedown", { bubbles: true, cancelable: true }),
      );
    expect(onPick).toHaveBeenCalledWith(items[0]);
  });

  it("renders nothing when closed", () => {
    const { container } = render(
      <MentionPopover
        state={{ ...state, open: false }}
        onPick={vi.fn()}
        onHover={vi.fn()}
      />,
    );
    expect(container.firstChild).toBeNull();
  });
});
