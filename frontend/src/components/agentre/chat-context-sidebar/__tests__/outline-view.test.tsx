import "@testing-library/jest-dom/vitest";

import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { OutlineView } from "../views/outline-view";

import type { OutlineItem } from "../derive";

const items: OutlineItem[] = [
  { messageId: 11, turn: 1, text: "do thing one", time: 1700000000000, edits: 2, err: false },
  { messageId: 22, turn: 2, text: "do thing two", time: 1700000060000, edits: 0, err: true },
];

describe("OutlineView", () => {
  it("renders each outline item", () => {
    render(<OutlineView items={items} activeMessageId={null} onSelect={() => {}} />);
    expect(screen.getByText("do thing one")).toBeInTheDocument();
    expect(screen.getByText("do thing two")).toBeInTheDocument();
  });

  it("renders edits badge and error badge", () => {
    render(<OutlineView items={items} activeMessageId={null} onSelect={() => {}} />);
    expect(screen.getByText(/2 edits/)).toBeInTheDocument();
    expect(screen.getByText(/error/i)).toBeInTheDocument();
  });

  it("highlights active row", () => {
    render(<OutlineView items={items} activeMessageId={22} onSelect={() => {}} />);
    const active = screen.getByText("do thing two").closest("[data-active]")!;
    expect(active.getAttribute("data-active")).toBe("true");
  });

  it("calls onSelect with messageId when row clicked", async () => {
    const onSelect = vi.fn();
    render(<OutlineView items={items} activeMessageId={null} onSelect={onSelect} />);
    await userEvent.click(screen.getByText("do thing one"));
    expect(onSelect).toHaveBeenCalledWith(11);
  });

  it("renders empty state when items is empty", () => {
    render(<OutlineView items={[]} activeMessageId={null} onSelect={() => {}} />);
    expect(screen.getByText(/还没有消息/)).toBeInTheDocument();
  });
});
