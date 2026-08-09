import "@testing-library/jest-dom/vitest";

import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

const sonnerMocks = vi.hoisted(() => ({
  toast: { error: vi.fn(), success: vi.fn() },
}));
vi.mock("sonner", () => sonnerMocks);

const openPathMock = vi.fn();
const revealPathMock = vi.fn();
vi.mock("@/../wailsjs/go/app/App", () => ({
  OpenPath: (p: string) => openPathMock(p),
  RevealPath: (p: string) => revealPathMock(p),
}));

import {
  selectActivePreviewTab,
  useChatSidebarStore,
} from "@/stores/chat-sidebar-store";

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

/** 「用默认应用打开」现在只在行的 ⋯ 菜单里（行内不再有常驻图标按钮）。 */
async function openWithDefaultApp(name: string) {
  const user = userEvent.setup({ pointerEventsCheck: 0 });
  const row = screen
    .getAllByTestId("changes-row")
    .find((el) => el.getAttribute("data-name") === name);
  if (!row) throw new Error(`no changes row named ${name}`);
  await user.click(within(row).getByRole("button", { name: /more actions/i }));
  const menu = await screen.findByRole("menu");
  await user.click(
    within(menu).getByRole("menuitem", { name: "Open with default app" }),
  );
}

beforeEach(() => {
  openPathMock.mockReset();
  openPathMock.mockResolvedValue(undefined);
  revealPathMock.mockReset();
  revealPathMock.mockResolvedValue(undefined);
  sonnerMocks.toast.error.mockReset();
  localStorage.clear();
  useChatSidebarStore.setState({ previewTabsBySession: {} });
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

    await openWithDefaultApp("chat.go");

    expect(openPathMock).toHaveBeenCalledWith(
      "/Users/me/proj/internal/service/chat_svc/chat.go",
    );
  });

  it("joins the cwd for a relative tool path", async () => {
    render(
      <FilesView
        sessionId={1}
        files={files}
        cwd={CWD}
        remote={false}
        onJumpToTurn={() => {}}
      />,
    );

    await openWithDefaultApp("chat.go");

    expect(openPathMock).toHaveBeenCalledWith(
      "/Users/me/proj/internal/service/chat_svc/chat.go",
    );
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
    await openWithDefaultApp("chat.go");
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

describe("FilesView 单击预览", () => {
  const clickRow = (rowName: RegExp) =>
    userEvent.click(screen.getByRole("button", { name: rowName }));

  it("markdown / 代码 / 图片行单击都开出预览标签", async () => {
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
    for (const [rowName, path] of [
      [/chat\.go/, "internal/service/chat_svc/chat.go"],
      [/README\.md/, "README.md"],
      [/logo\.png/, "assets/logo.png"],
    ] as const) {
      await clickRow(rowName);
      expect(
        selectActivePreviewTab(useChatSidebarStore.getState(), 1),
      ).toMatchObject({ path, sourceMode: "changes" });
    }
  });

  it("不可预览的文件行不是按钮，单击没有任何反应", async () => {
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
    expect(screen.queryByRole("button", { name: /archive\.zip/ })).toBeNull();
    await userEvent.click(screen.getByText("doc.pdf"));
    expect(
      selectActivePreviewTab(useChatSidebarStore.getState(), 1),
    ).toBeNull();
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
    await clickRow(/chat\.go/);

    expect(
      selectActivePreviewTab(useChatSidebarStore.getState(), 1),
    ).toMatchObject({
      path: "internal/service/chat_svc/chat.go",
      segment: null,
      sourceMode: "changes",
    });
  });

  it("绝对路径落在 cwd 之外的行不可点", () => {
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
    expect(screen.queryByRole("button", { name: /secret\.go/ })).toBeNull();
  });

  it("会话没有工作目录时所有文件行都不可点", () => {
    render(
      <FilesView
        sessionId={1}
        files={files}
        cwd=""
        remote={false}
        onJumpToTurn={() => {}}
      />,
    );
    expect(screen.queryByRole("button", { name: /chat\.go/ })).toBeNull();
    expect(screen.queryByRole("button", { name: /README\.md/ })).toBeNull();
  });

  it("远端会话的行照样可以单击预览（内容经 RPC 读取）", async () => {
    render(
      <FilesView
        sessionId={1}
        files={files}
        cwd={CWD}
        remote={true}
        onJumpToTurn={() => {}}
      />,
    );
    await clickRow(/README\.md/);
    expect(
      selectActivePreviewTab(useChatSidebarStore.getState(), 1),
    ).toMatchObject({ path: "README.md", sourceMode: "changes" });
  });
});
