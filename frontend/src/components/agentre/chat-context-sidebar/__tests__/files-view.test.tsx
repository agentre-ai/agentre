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

  it("renders a Windows path as one collapsed chain row down to its basename directory", () => {
    // src 只有一个子目录 components、components 直接包含文件 file.tsx——两段
    // 折成一行「src/components」，而不是各占一行。
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
      screen.getByRole("button", { name: "Collapse src/components" }),
    ).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Collapse src" })).toBeNull();
    expect(
      screen.queryByRole("button", { name: "Collapse components" }),
    ).toBeNull();
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
    // internal/frontend 各自的链已折叠成一行，用正则匹配折叠后的完整标签。
    for (const name of [/^Collapse internal/, /^Collapse frontend/]) {
      const btn = screen.getByRole("button", { name });
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

describe("FilesView 链压缩", () => {
  function chainRow(): HTMLElement {
    const found = screen
      .getAllByTestId("changes-row")
      .find((el) => el.getAttribute("data-name") === "chat_svc");
    if (!found) throw new Error("no folded internal/service/chat_svc row");
    return found;
  }

  it("folds a single-subdirectory, file-free chain into one row, dimming every prefix segment but the last", () => {
    render(
      <FilesView
        sessionId={1}
        files={files}
        cwd={CWD}
        remote={false}
        onJumpToTurn={() => {}}
      />,
    );

    // internal 与 service 各自单独一行的旧形态不再存在；三段折成一行。
    expect(
      screen.queryByRole("button", { name: "Collapse internal" }),
    ).toBeNull();
    expect(
      screen.queryByRole("button", { name: "Collapse service" }),
    ).toBeNull();
    const row = chainRow();
    expect(
      within(row).getByRole("button", {
        name: "Collapse internal/service/chat_svc",
      }),
    ).toBeInTheDocument();

    // 前缀（除末段外）降低不透明度，末段本身不带这个类——身份一眼可辨。
    expect(within(row).getByTestId("chain-prefix")).toHaveTextContent(
      "internal/service/",
    );
    expect(within(row).getByTestId("chain-prefix").className).toContain(
      "opacity-55",
    );
    const nameCell = within(row).getByTestId("chain-name");
    expect(nameCell).toHaveTextContent("internal/service/chat_svc");
    // 末段紧跟在 dimmed 前缀 span 后面，作为一段不带额外透明度类的纯文本。
    expect(nameCell.lastChild?.textContent).toBe("chat_svc");
  });

  it("does not fold a directory that directly contains a file", () => {
    render(
      <FilesView
        sessionId={1}
        files={[
          { path: "a/b/c.go", plus: 0, minus: 0, lastTurn: 1 },
          { path: "a/direct.go", plus: 0, minus: 0, lastTurn: 1 },
        ]}
        cwd={CWD}
        remote={false}
        onJumpToTurn={() => {}}
      />,
    );

    // a 直接包含 direct.go，链在 a 处即终止；b 是 a 的单独子行。
    expect(
      screen.getByRole("button", { name: "Collapse a" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Collapse b" }),
    ).toBeInTheDocument();
  });

  it("does not fold a directory with multiple subdirectories", () => {
    render(
      <FilesView
        sessionId={1}
        files={[
          { path: "a/b/x.go", plus: 0, minus: 0, lastTurn: 1 },
          { path: "a/c/y.go", plus: 0, minus: 0, lastTurn: 1 },
        ]}
        cwd={CWD}
        remote={false}
        onJumpToTurn={() => {}}
      />,
    );

    // a 有两个子目录，链在 a 处即终止；b、c 各自独立一行。
    expect(
      screen.getByRole("button", { name: "Collapse a" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Collapse b" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Collapse c" }),
    ).toBeInTheDocument();
  });

  it("expands/collapses the folded row as one unit; its children indent exactly one level deeper", async () => {
    render(
      <FilesView
        sessionId={1}
        files={files}
        cwd={CWD}
        remote={false}
        onJumpToTurn={() => {}}
      />,
    );

    const row = chainRow();
    // 折叠行本身在树的第 0 层（internal 是根级链），子项 chat.go 只多缩进一
    // 级，无论链吸收了几段（internal/service/chat_svc 三段仍是 +1 级）。
    expect(row.style.paddingLeft).toBe("8px");
    const chatGoRow = screen
      .getAllByTestId("changes-row")
      .find((el) => el.getAttribute("data-name") === "chat.go")!;
    expect(chatGoRow.style.paddingLeft).toBe("22px");

    const toggle = within(row).getByRole("button", {
      name: "Collapse internal/service/chat_svc",
    });
    await userEvent.click(toggle);
    expect(toggle).toHaveAttribute("aria-expanded", "false");
    expect(
      screen.queryByRole("button", { name: /chat\.go/ }),
    ).not.toBeInTheDocument();

    await userEvent.click(toggle);
    expect(toggle).toHaveAttribute("aria-expanded", "true");
    expect(
      screen.getByRole("button", { name: /chat\.go/ }),
    ).toBeInTheDocument();
  });

  it("marks the folded chain label to truncate from the head, keeping the last segment", () => {
    render(
      <FilesView
        sessionId={1}
        files={files}
        cwd={CWD}
        remote={false}
        onJumpToTurn={() => {}}
      />,
    );

    // dir="rtl" + text-left 是本仓「从头截断」的既有实现方式（与 Git 页目录
    // 后缀一致）：省略号落在开头，末段（身份）留在可见的一端。
    const nameCell = within(chainRow()).getByTestId("chain-name");
    expect(nameCell).toHaveAttribute("dir", "rtl");
    expect(nameCell.className).toContain("text-left");
    expect(nameCell.className).toContain("truncate");
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
