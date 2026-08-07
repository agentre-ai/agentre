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

import { useChatSidebarStore } from "@/stores/chat-sidebar-store";

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
  openPathMock.mockResolvedValue(undefined);
  sonnerMocks.toast.error.mockReset();
  localStorage.clear();
  useChatSidebarStore.setState({ previewBySession: {} });
});

describe("FilesView", () => {
  it("renders the directory tree with folder rows and file basenames", () => {
    render(
      <FilesView
        sessionId={1}
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

  it("reserves a chevron-width slot on file rows so dir/file icon and name columns align", () => {
    render(
      <FilesView
        sessionId={1}
        files={files}
        cwd={CWD}
        remote={false}
        onJumpToTurn={() => {}}
      />,
    );
    // 文件行跳转按钮的第一个子元素是与目录 chevron 同宽(size-3.5)的空槽位，
    // 使同级目录文件夹图标/文件名与文件图标/文件名在水平上对齐。
    const fileRow = screen.getByRole("button", { name: /chat\.go/ });
    const firstChild = fileRow.firstElementChild;
    expect(firstChild).not.toBeNull();
    expect(firstChild!.className).toContain("size-3.5");
    expect(firstChild!.textContent).toBe("");
    expect(firstChild!.getAttribute("aria-hidden")).toBe("true");
    // 空槽位之后紧跟文件图标（FileCode svg），再是文件名。
    const icon = fileRow.querySelector("svg");
    expect(icon).not.toBeNull();
    expect(icon!.nextElementSibling?.textContent).toBe("chat.go");
  });

  it("renders a Windows path as folders with its basename on the file row", () => {
    const windowsFiles: FileEntry[] = [
      {
        path: "src\\components\\file.tsx",
        plus: 0,
        minus: 0,
        lastTurn: 1,
      },
    ];
    render(
      <FilesView
        sessionId={1}
        files={windowsFiles}
        cwd="C:\\proj"
        remote={false}
        onJumpToTurn={() => {}}
      />,
    );

    expect(
      screen.getByRole("button", { name: "Collapse src" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Collapse components" }),
    ).toBeInTheDocument();
    expect(screen.getByText("file.tsx")).toBeInTheDocument();
  });

  it("left-aligns folder names with explicit text-left (matches the file-row pattern)", () => {
    render(
      <FilesView
        sessionId={1}
        files={files}
        cwd={CWD}
        remote={false}
        onJumpToTurn={() => {}}
      />,
    );
    // 目录按钮必须显式 text-left：app 存在把 button 文字居中的全局规则，
    // 文件行按钮原本就带 text-left 绕开；目录按钮漏掉会导致文件夹名与图标拉开大间隔。
    for (const name of ["internal", "frontend"]) {
      const btn = screen.getByRole("button", { name: `Collapse ${name}` });
      expect(btn.className).toContain("text-left");
    }
  });

  it("collapses and expands a folder on row click, aria-expanded reflects state", async () => {
    render(
      <FilesView
        sessionId={1}
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

  it("keeps a collapsed folder closed when an equivalent files array rerenders", async () => {
    const { rerender } = render(
      <FilesView
        sessionId={1}
        files={files}
        cwd={CWD}
        remote={false}
        onJumpToTurn={() => {}}
      />,
    );
    await userEvent.click(screen.getByRole("button", { name: /internal/ }));

    rerender(
      <FilesView
        sessionId={1}
        files={[...files]}
        cwd={CWD}
        remote={false}
        onJumpToTurn={() => {}}
      />,
    );

    expect(
      screen.queryByRole("button", { name: /chat\.go/ }),
    ).not.toBeInTheDocument();
  });

  it("resets a collapsed folder when the ordered file paths change", async () => {
    const { rerender } = render(
      <FilesView
        sessionId={1}
        files={files}
        cwd={CWD}
        remote={false}
        onJumpToTurn={() => {}}
      />,
    );
    await userEvent.click(screen.getByRole("button", { name: /internal/ }));

    rerender(
      <FilesView
        sessionId={1}
        files={[...files].reverse()}
        cwd={CWD}
        remote={false}
        onJumpToTurn={() => {}}
      />,
    );

    expect(
      screen.getByRole("button", { name: /chat\.go/ }),
    ).toBeInTheDocument();
  });

  it("file row click still jumps to lastTurn", async () => {
    const onJump = vi.fn();
    render(
      <FilesView
        sessionId={1}
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
        sessionId={1}
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
        sessionId={1}
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
        sessionId={1}
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

  it("opens an absolute tool path directly instead of prefixing cwd", async () => {
    const absoluteFiles: FileEntry[] = [
      {
        path: "/Users/me/proj/internal/service/chat_svc/chat.go",
        plus: 0,
        minus: 0,
        lastTurn: 1,
      },
    ];
    render(
      <FilesView
        sessionId={1}
        files={absoluteFiles}
        cwd={CWD}
        remote={false}
        onJumpToTurn={() => {}}
      />,
    );

    const chatGoRow = screen.getByRole("button", { name: /chat\.go/ });
    await userEvent.click(
      within(chatGoRow.parentElement!).getByRole("button", {
        name: /Open file/i,
      }),
    );

    expect(openPathMock).toHaveBeenCalledWith(
      "/Users/me/proj/internal/service/chat_svc/chat.go",
    );
  });

  it("hides open buttons when remote is true", () => {
    render(
      <FilesView
        sessionId={1}
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
      <FilesView
        sessionId={1}
        files={files}
        cwd=""
        remote={false}
        onJumpToTurn={() => {}}
      />,
    );
    expect(
      screen.queryByRole("button", { name: /Open file/i }),
    ).not.toBeInTheDocument();
  });

  it("toasts openFailed with the error when OpenPath rejects", async () => {
    openPathMock.mockRejectedValue(new Error("boom"));
    render(
      <FilesView
        sessionId={1}
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
      <FilesView
        sessionId={1}
        files={[]}
        cwd={CWD}
        remote={false}
        onJumpToTurn={() => {}}
      />,
    );
    expect(
      screen.getByText(/No files have been changed in this session/),
    ).toBeInTheDocument();
  });
});

describe("FilesView preview button", () => {
  const previewBtn = (rowName: RegExp) => {
    const row = screen.getByRole("button", { name: rowName });
    return within(row.parentElement!).queryByRole("button", {
      name: /preview/i,
    });
  };

  it("renders a preview button for markdown / code / image files", () => {
    render(
      <FilesView
        sessionId={1}
        files={[
          ...files,
          { path: "assets/logo.png", plus: 0, minus: 0, lastTurn: 1 },
        ]}
        cwd={CWD}
        remote={false}
        onJumpToTurn={() => {}}
      />,
    );
    expect(previewBtn(/chat\.go/)).not.toBeNull();
    expect(previewBtn(/README\.md/)).not.toBeNull();
    expect(previewBtn(/logo\.png/)).not.toBeNull();
  });

  it("does not render a preview button for non-previewable file types", () => {
    render(
      <FilesView
        sessionId={1}
        files={[
          { path: "archive.zip", plus: 0, minus: 0, lastTurn: 1 },
          { path: "doc.pdf", plus: 0, minus: 0, lastTurn: 1 },
        ]}
        cwd={CWD}
        remote={false}
        onJumpToTurn={() => {}}
      />,
    );
    expect(screen.queryByRole("button", { name: /preview/i })).toBeNull();
  });

  it("opens the panel with the relative path and does not jump on click", async () => {
    const onJump = vi.fn();
    render(
      <FilesView
        sessionId={1}
        files={files}
        cwd={CWD}
        remote={false}
        onJumpToTurn={onJump}
      />,
    );
    await userEvent.click(previewBtn(/README\.md/)!);

    expect(useChatSidebarStore.getState().previewBySession[1]).toEqual({
      path: "README.md",
      segment: null,
    });
    expect(onJump).not.toHaveBeenCalled();
  });

  it("strips the cwd prefix for an absolute path inside the cwd", async () => {
    render(
      <FilesView
        sessionId={1}
        files={[
          {
            path: "/Users/me/proj/internal/service/chat_svc/chat.go",
            plus: 0,
            minus: 0,
            lastTurn: 1,
          },
        ]}
        cwd={CWD}
        remote={false}
        onJumpToTurn={() => {}}
      />,
    );
    const row = screen.getByRole("button", { name: /chat\.go/ });
    await userEvent.click(
      within(row.parentElement!).getByRole("button", { name: /preview/i }),
    );

    expect(useChatSidebarStore.getState().previewBySession[1]).toEqual({
      path: "internal/service/chat_svc/chat.go",
      segment: null,
    });
  });

  it("hides the preview button for an absolute path outside the cwd", () => {
    render(
      <FilesView
        sessionId={1}
        files={[
          {
            path: "/Users/other/proj/secret.go",
            plus: 0,
            minus: 0,
            lastTurn: 1,
          },
        ]}
        cwd={CWD}
        remote={false}
        onJumpToTurn={() => {}}
      />,
    );
    expect(screen.queryByRole("button", { name: /preview/i })).toBeNull();
  });

  it("still shows the preview button for remote sessions (unlike open)", () => {
    render(
      <FilesView
        sessionId={1}
        files={files}
        cwd={CWD}
        remote={true}
        onJumpToTurn={() => {}}
      />,
    );
    expect(screen.queryByRole("button", { name: /open file/i })).toBeNull();
    // 三行都是可预览文件 → 三个预览按钮(远端也出,内容经 RPC 读取)。
    expect(screen.getAllByRole("button", { name: /preview/i }).length).toBe(3);
  });

  it("highlights the currently previewed file's button", () => {
    useChatSidebarStore.getState().openPreview(1, "README.md");
    render(
      <FilesView
        sessionId={1}
        files={files}
        cwd={CWD}
        remote={false}
        onJumpToTurn={() => {}}
      />,
    );
    expect(previewBtn(/README\.md/)!.className).toContain("text-primary");
    expect(previewBtn(/chat\.go/)!.className).not.toContain("text-primary");
  });
});
