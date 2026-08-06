import "@testing-library/jest-dom/vitest";

import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

const sonnerMocks = vi.hoisted(() => ({
  toast: { error: vi.fn(), success: vi.fn() },
}));
vi.mock("sonner", () => sonnerMocks);

const openPathMock = vi.fn();
vi.mock("@/../wailsjs/go/app/App", () => ({
  OpenPath: (p: string) => openPathMock(p),
}));

import { FilesView } from "../views/files-view";

import type { FileEntry } from "../derive";

const files: FileEntry[] = [
  {
    path: "internal/service/chat_svc/chat.go",
    plus: 5,
    minus: 2,
    lastTurn: 3,
  },
  {
    path: "frontend/src/components/chat-panel.tsx",
    plus: 0,
    minus: 3,
    lastTurn: 2,
  },
  {
    path: "README.md",
    plus: 0,
    minus: 0,
    lastTurn: 1,
  },
];

const CWD = "/Users/me/proj";

beforeEach(() => {
  openPathMock.mockReset();
  sonnerMocks.toast.error.mockReset();
});

describe("FilesView", () => {
  it("renders the directory tree with folder rows and file basenames", () => {
    render(
      <FilesView
        files={files}
        cwd={CWD}
        remote={false}
        onJumpToTurn={() => {}}
      />,
    );
    expect(
      screen.getByRole("button", { name: /internal/ }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /frontend/ }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /chat\.go/ }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /chat-panel\.tsx/ }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /README\.md/ }),
    ).toBeInTheDocument();
  });

  it("collapses and expands a folder on row click, aria-expanded reflects state", async () => {
    render(
      <FilesView
        files={files}
        cwd={CWD}
        remote={false}
        onJumpToTurn={() => {}}
      />,
    );
    const internalRow = screen.getByRole("button", { name: /internal/ });
    expect(internalRow).toHaveAttribute("aria-expanded", "true");
    expect(
      screen.getByRole("button", { name: /chat\.go/ }),
    ).toBeInTheDocument();

    await userEvent.click(internalRow);
    expect(internalRow).toHaveAttribute("aria-expanded", "false");
    expect(
      screen.queryByRole("button", { name: /chat\.go/ }),
    ).not.toBeInTheDocument();

    await userEvent.click(internalRow);
    expect(internalRow).toHaveAttribute("aria-expanded", "true");
    expect(
      screen.getByRole("button", { name: /chat\.go/ }),
    ).toBeInTheDocument();
  });

  it("file row click still jumps to lastTurn", async () => {
    const onJump = vi.fn();
    render(
      <FilesView
        files={files}
        cwd={CWD}
        remote={false}
        onJumpToTurn={onJump}
      />,
    );
    await userEvent.click(screen.getByRole("button", { name: /chat\.go/ }));
    expect(onJump).toHaveBeenCalledWith(3);
  });

  it("renders +N in text-status-running and −N in text-destructive badges", () => {
    render(
      <FilesView
        files={files}
        cwd={CWD}
        remote={false}
        onJumpToTurn={() => {}}
      />,
    );
    const chatGo = screen.getByRole("button", { name: /chat\.go/ });
    expect(within(chatGo).getByText("+5")).toHaveClass("text-status-running");
    expect(within(chatGo).getByText("−2")).toHaveClass("text-destructive");

    const panel = screen.getByRole("button", { name: /chat-panel\.tsx/ });
    expect(within(panel).getByText("−3")).toHaveClass("text-destructive");
    expect(within(panel).queryByText(/\+/)).not.toBeInTheDocument();
  });

  it("omits diff badge when plus and minus are both 0", () => {
    render(
      <FilesView
        files={files}
        cwd={CWD}
        remote={false}
        onJumpToTurn={() => {}}
      />,
    );
    const readme = screen.getByRole("button", { name: /README\.md/ });
    expect(within(readme).queryByText(/[+\u2212]\d/)).not.toBeInTheDocument();
  });

  it("renders open button and calls OpenPath(cwd + path), without jumping", async () => {
    const onJump = vi.fn();
    render(
      <FilesView
        files={files}
        cwd={CWD}
        remote={false}
        onJumpToTurn={onJump}
      />,
    );
    const chatGoRow = screen.getByRole("button", { name: /chat\.go/ });
    const openBtn = within(chatGoRow.parentElement!).getByRole("button", {
      name: /Open file/i,
    });
    await userEvent.click(openBtn);
    expect(openPathMock).toHaveBeenCalledWith(
      "/Users/me/proj/internal/service/chat_svc/chat.go",
    );
    expect(onJump).not.toHaveBeenCalled();
  });

  it("hides open buttons when remote is true", () => {
    render(
      <FilesView
        files={files}
        cwd={CWD}
        remote={true}
        onJumpToTurn={() => {}}
      />,
    );
    expect(
      screen.queryByRole("button", { name: /Open file/i }),
    ).not.toBeInTheDocument();
  });

  it("hides open buttons when cwd is empty", () => {
    render(
      <FilesView files={files} cwd="" remote={false} onJumpToTurn={() => {}} />,
    );
    expect(
      screen.queryByRole("button", { name: /Open file/i }),
    ).not.toBeInTheDocument();
  });

  it("toasts openFailed with the error when OpenPath rejects", async () => {
    openPathMock.mockRejectedValue(new Error("boom"));
    render(
      <FilesView
        files={files}
        cwd={CWD}
        remote={false}
        onJumpToTurn={() => {}}
      />,
    );
    const chatGoRow = screen.getByRole("button", { name: /chat\.go/ });
    const openBtn = within(chatGoRow.parentElement!).getByRole("button", {
      name: /Open file/i,
    });
    await userEvent.click(openBtn);
    await vi.waitFor(() => {
      expect(sonnerMocks.toast.error).toHaveBeenCalledWith(
        "Failed to open file: boom",
      );
    });
  });

  it("shows empty state when files is empty", () => {
    render(
      <FilesView files={[]} cwd={CWD} remote={false} onJumpToTurn={() => {}} />,
    );
    expect(
      screen.getByText(/No files have been changed in this session/),
    ).toBeInTheDocument();
  });
});
