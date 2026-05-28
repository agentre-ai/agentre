import "@testing-library/jest-dom/vitest";

import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { FilesView } from "../views/files-view";

import type { FileEntry } from "../derive";

const files: FileEntry[] = [
  {
    path: "internal/service/chat_svc/chat.go",
    edits: 5,
    reads: 1,
    lastTurn: 3,
  },
  {
    path: "frontend/src/components/chat-panel.tsx",
    edits: 2,
    reads: 0,
    lastTurn: 2,
  },
];

describe("FilesView", () => {
  it("renders each file with edits count", () => {
    render(<FilesView files={files} onJumpToTurn={() => {}} />);
    expect(screen.getByText(/chat\.go/)).toBeInTheDocument();
    expect(screen.getByText("5")).toBeInTheDocument();
  });

  it("calls onJumpToTurn with lastTurn when row clicked", async () => {
    const onJump = vi.fn();
    render(<FilesView files={files} onJumpToTurn={onJump} />);
    await userEvent.click(screen.getByText(/chat\.go/));
    expect(onJump).toHaveBeenCalledWith(3);
  });

  it("shows empty state when files is empty", () => {
    render(<FilesView files={[]} onJumpToTurn={() => {}} />);
    expect(screen.getByText(/没有文件|没有改过任何文件/)).toBeInTheDocument();
  });
});
