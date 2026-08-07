import "@testing-library/jest-dom/vitest";

import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

const sonnerMocks = vi.hoisted(() => ({
  toast: { error: vi.fn(), success: vi.fn() },
}));
vi.mock("sonner", () => sonnerMocks);

const openPathMock = vi.fn();
const listDirMock = vi.fn();
// Git 模式的两个绑定这里只需要能被安全调用（本文件不断言它们），断言在
// files-panel-git.test.tsx。
const gitMocks = vi.hoisted(() => ({
  changes: async () => ({
    notARepo: false,
    baseRef: "",
    changes: [],
    truncated: false,
  }),
  branches: async () => ({
    notARepo: false,
    currentBranch: "",
    defaultBaseline: "",
    branches: [],
  }),
}));
vi.mock("@/../wailsjs/go/app/App", () => ({
  OpenPath: (p: string) => openPathMock(p),
  WorkspaceFsListDir: (sessionId: number, relPath: string, ignored: boolean) =>
    listDirMock(sessionId, relPath, ignored),
  WorkspaceFsGitChanges: gitMocks.changes,
  WorkspaceFsGitBranches: gitMocks.branches,
}));

import { useChatSidebarStore } from "@/stores/chat-sidebar-store";
import { useSessionStatusStore } from "@/stores/session-status-store";

import { FilesPanel } from "../views/files-panel";

import type { FileEntry } from "../derive";

const CWD = "/Users/me/proj";

const files: FileEntry[] = [
  { path: "internal/service/chat_svc/chat.go", plus: 5, minus: 2, lastTurn: 3 },
  { path: "README.md", plus: 0, minus: 0, lastTurn: 1 },
];

function entry(name: string, isDir = false, gitIgnored = false) {
  return { name, isDir, size: 12, mtime: 0, symlink: false, gitIgnored };
}

function listing(entries: ReturnType<typeof entry>[], truncated = false) {
  return { path: CWD, entries, truncated };
}

function renderPanel(props: Partial<React.ComponentProps<typeof FilesPanel>>) {
  return render(
    <FilesPanel
      sessionId={7}
      files={files}
      cwd={CWD}
      remote={false}
      onJumpToTurn={() => {}}
      {...props}
    />,
  );
}

async function switchTo(name: RegExp) {
  await userEvent.click(screen.getByRole("tab", { name }));
}

function row(name: string): HTMLElement {
  const found = screen
    .getAllByTestId("directory-row")
    .find((el) => el.getAttribute("data-name") === name);
  if (!found) throw new Error(`no directory row named ${name}`);
  return found;
}

beforeEach(() => {
  localStorage.clear();
  useChatSidebarStore.setState({
    open: true,
    activeTab: "files",
    filesMode: "changes",
    showIgnored: false,
  });
  useSessionStatusStore.getState().__reset();
  openPathMock.mockReset();
  openPathMock.mockResolvedValue(undefined);
  listDirMock.mockReset();
  listDirMock.mockResolvedValue(listing([]));
  sonnerMocks.toast.error.mockReset();
});

describe("FilesPanel mode switcher", () => {
  it("renders three mode segments with 变动 selected by default and no backend call", () => {
    renderPanel({});

    const tabs = screen.getAllByRole("tab");
    expect(tabs.map((t) => t.textContent)).toEqual([
      "Changes2",
      "Directory",
      "Git",
    ]);
    expect(screen.getByRole("tab", { name: /changes/i })).toHaveAttribute(
      "aria-selected",
      "true",
    );
    expect(screen.getByRole("tab", { name: /directory/i })).toHaveAttribute(
      "aria-selected",
      "false",
    );
    // 「变动」模式零后端调用（硬不变量 1）。
    expect(listDirMock).not.toHaveBeenCalled();
  });

  it("keeps the changes tree rendering and jumping to lastTurn", async () => {
    const onJump = vi.fn();
    renderPanel({ onJumpToTurn: onJump });

    await userEvent.click(screen.getByRole("button", { name: /chat\.go/ }));
    expect(onJump).toHaveBeenCalledWith(3);
  });

  it("persists the selected mode in the sidebar store and restores the changes tree on return", async () => {
    renderPanel({});

    await switchTo(/directory/i);
    expect(useChatSidebarStore.getState().filesMode).toBe("directory");
    expect(screen.queryByRole("button", { name: /chat\.go/ })).toBeNull();

    await switchTo(/changes/i);
    expect(useChatSidebarStore.getState().filesMode).toBe("changes");
    expect(
      screen.getByRole("button", { name: /chat\.go/ }),
    ).toBeInTheDocument();
  });

  it("selects the git mode without touching the directory listing", async () => {
    renderPanel({});

    await switchTo(/^git$/i);
    expect(useChatSidebarStore.getState().filesMode).toBe("git");
    expect(listDirMock).not.toHaveBeenCalled();
  });

  it("shows the 显示忽略项 toggle only in directory mode", async () => {
    renderPanel({});
    expect(screen.queryByRole("button", { name: /show ignored/i })).toBeNull();

    await switchTo(/directory/i);
    expect(
      await screen.findByRole("button", { name: /show ignored/i }),
    ).toBeInTheDocument();

    await switchTo(/changes/i);
    expect(screen.queryByRole("button", { name: /show ignored/i })).toBeNull();
  });
});

describe("FilesPanel directory mode", () => {
  beforeEach(() => {
    useChatSidebarStore.setState({ filesMode: "directory" });
  });

  it("loads the root level once with includeIgnored=false and sorts dirs before files", async () => {
    listDirMock.mockResolvedValue(
      listing([
        entry("main.go"),
        entry("internal", true),
        entry("README.md"),
        entry("frontend", true),
      ]),
    );
    renderPanel({});

    await screen.findByRole("button", { name: /expand internal/i });
    expect(listDirMock).toHaveBeenCalledTimes(1);
    expect(listDirMock).toHaveBeenCalledWith(7, "", false);

    const names = screen
      .getAllByTestId("directory-row")
      .map((row) => row.getAttribute("data-name"));
    // 目录在前、各自 localeCompare 字母序（与 deriveFileTree 同一套排序约定）。
    expect(names).toEqual(["frontend", "internal", "main.go", "README.md"]);
  });

  it("shows a loading state while the root level is in flight", async () => {
    listDirMock.mockReturnValue(new Promise(() => {}));
    renderPanel({});

    expect(screen.getByText(/loading directory/i)).toBeInTheDocument();
  });

  it("lazily loads a folder on first expand, spins that row, and does not refetch on re-expand", async () => {
    let resolveChild: ((v: unknown) => void) | null = null;
    listDirMock.mockImplementation((_id: number, relPath: string) => {
      if (relPath === "") return Promise.resolve(listing([entry("app", true)]));
      return new Promise((resolve) => {
        resolveChild = resolve;
      });
    });
    renderPanel({});

    const appRow = await screen.findByRole("button", { name: /expand app/i });
    await userEvent.click(appRow);

    expect(listDirMock).toHaveBeenCalledWith(7, "app", false);
    expect(
      within(appRow).getByRole("status", { name: /loading directory/i }),
    ).toBeInTheDocument();

    resolveChild!(listing([entry("app.go")]));
    expect(await screen.findByText("app.go")).toBeInTheDocument();

    // 收起再展开只读缓存，不再打后端。
    await userEvent.click(
      screen.getByRole("button", { name: /collapse app/i }),
    );
    expect(screen.queryByText("app.go")).toBeNull();
    await userEvent.click(screen.getByRole("button", { name: /expand app/i }));
    expect(screen.getByText("app.go")).toBeInTheDocument();
    expect(listDirMock).toHaveBeenCalledTimes(2);
  });

  it("re-requests with includeIgnored=true when 显示忽略项 is switched on, persists it, and dims ignored rows", async () => {
    listDirMock.mockImplementation(
      (_id: number, _relPath: string, ignored: boolean) =>
        Promise.resolve(
          listing(
            ignored
              ? [entry("node_modules", true, true), entry("main.go")]
              : [entry("main.go")],
          ),
        ),
    );
    renderPanel({});

    await screen.findByText("main.go");
    await userEvent.click(
      screen.getByRole("button", { name: /show ignored/i }),
    );

    await waitFor(() => expect(listDirMock).toHaveBeenCalledWith(7, "", true));
    expect(useChatSidebarStore.getState().showIgnored).toBe(true);

    await screen.findByRole("button", { name: /expand node_modules/i });
    expect(row("node_modules")).toHaveAttribute("data-git-ignored", "true");
    expect(row("main.go")).not.toHaveAttribute("data-git-ignored");
  });

  it("notes the per-level truncation with the actual cap", async () => {
    listDirMock.mockResolvedValue(
      listing([entry("a.go"), entry("b.go"), entry("c.go")], true),
    );
    renderPanel({});

    expect(
      await screen.findByText(/showing the first 3 entries/i),
    ).toBeInTheDocument();
  });

  it("shows the empty copy for an empty directory", async () => {
    listDirMock.mockResolvedValue(listing([]));
    renderPanel({});

    expect(
      await screen.findByText(/this directory is empty/i),
    ).toBeInTheDocument();
  });

  it("shows the no-working-directory state without calling the backend", async () => {
    renderPanel({ cwd: "" });

    expect(
      await screen.findByText(/this session has no working directory/i),
    ).toBeInTheDocument();
    expect(listDirMock).not.toHaveBeenCalled();
  });

  it("surfaces a remote-offline failure with a retry that re-requests", async () => {
    listDirMock.mockRejectedValueOnce("Remote device offline");
    listDirMock.mockResolvedValue(listing([entry("main.go")]));
    renderPanel({});

    expect(
      await screen.findByText("Remote device offline"),
    ).toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: /retry/i }));
    expect(await screen.findByText("main.go")).toBeInTheDocument();
    expect(listDirMock).toHaveBeenCalledTimes(2);
  });

  it("surfaces the daemon-outdated message verbatim instead of a generic failure", async () => {
    listDirMock.mockRejectedValue(
      new Error("Remote agentred is too old; please upgrade to use this view"),
    );
    renderPanel({});

    expect(
      await screen.findByText(
        "Remote agentred is too old; please upgrade to use this view",
      ),
    ).toBeInTheDocument();
    expect(screen.queryByText(/failed to read the directory/i)).toBeNull();
  });

  it("falls back to a generic read failure when the rejection carries no message", async () => {
    listDirMock.mockRejectedValue("");
    renderPanel({});

    expect(
      await screen.findByText(/failed to read the directory/i),
    ).toBeInTheDocument();
  });

  it("keeps the rest of the tree usable when one sub-directory fails to read, and retries it on re-expand", async () => {
    let subFails = true;
    listDirMock.mockImplementation((_id: number, relPath: string) => {
      if (relPath === "")
        return Promise.resolve(listing([entry("secret", true), entry("a.go")]));
      if (subFails) return Promise.reject(new Error("permission denied"));
      return Promise.resolve(listing([entry("key.txt")]));
    });
    renderPanel({});

    await userEvent.click(
      await screen.findByRole("button", { name: /expand secret/i }),
    );

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "permission denied",
    );
    expect(screen.getByText("a.go")).toBeInTheDocument();

    // 失败的那一层没有缓存可读，收起再展开就是它的重试。
    subFails = false;
    await userEvent.click(
      screen.getByRole("button", { name: /collapse secret/i }),
    );
    await userEvent.click(
      screen.getByRole("button", { name: /expand secret/i }),
    );
    expect(await screen.findByText("key.txt")).toBeInTheDocument();
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("resets expansion and reloads the root when the session changes", async () => {
    listDirMock.mockImplementation((_id: number, relPath: string) =>
      Promise.resolve(
        relPath === ""
          ? listing([entry("app", true)])
          : listing([entry("app.go")]),
      ),
    );
    const { rerender } = renderPanel({});

    await userEvent.click(
      await screen.findByRole("button", { name: /expand app/i }),
    );
    expect(await screen.findByText("app.go")).toBeInTheDocument();

    rerender(
      <FilesPanel
        sessionId={8}
        files={files}
        cwd={CWD}
        remote={false}
        onJumpToTurn={() => {}}
      />,
    );

    await waitFor(() => expect(listDirMock).toHaveBeenCalledWith(8, "", false));
    expect(
      await screen.findByRole("button", { name: /expand app/i }),
    ).toHaveAttribute("aria-expanded", "false");
    expect(screen.queryByText("app.go")).toBeNull();
  });

  // 决策 13：目录与 Git 两个模式的数据都是快照，「当前会话轮次结束」是「文件可能
  // 变了」的唯一强信号，两个模式都要在那时自动重拉（没有手动刷新按钮可以补救）。
  it("refetches the loaded levels when the current session's turn ends", async () => {
    listDirMock.mockImplementation((_id: number, relPath: string) =>
      Promise.resolve(
        relPath === ""
          ? listing([entry("app", true)])
          : listing([entry("app.go")]),
      ),
    );
    renderPanel({});

    await userEvent.click(
      await screen.findByRole("button", { name: /expand app/i }),
    );
    expect(await screen.findByText("app.go")).toBeInTheDocument();
    expect(listDirMock).toHaveBeenCalledTimes(2);

    useSessionStatusStore.getState().bumpDone(7, { kind: "done" });

    // 根与已展开的那一层各重拉一遍，展开态保留。
    await waitFor(() => expect(listDirMock).toHaveBeenCalledTimes(4));
    expect(listDirMock).toHaveBeenCalledWith(7, "app", false);
    expect(
      await screen.findByRole("button", { name: /collapse app/i }),
    ).toHaveAttribute("aria-expanded", "true");

    // 别的会话结束不该惊动本面板。
    useSessionStatusStore.getState().bumpDone(9, { kind: "done" });
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(listDirMock).toHaveBeenCalledTimes(4);
  });

  it("opens a file with the cwd-joined path, and only for local sessions", async () => {
    listDirMock.mockImplementation((_id: number, relPath: string) =>
      Promise.resolve(
        relPath === ""
          ? listing([entry("internal", true)])
          : listing([entry("chat.go")]),
      ),
    );
    renderPanel({});

    await userEvent.click(
      await screen.findByRole("button", { name: /expand internal/i }),
    );
    await userEvent.click(
      await screen.findByRole("button", { name: /open file/i }),
    );
    expect(openPathMock).toHaveBeenCalledWith(`${CWD}/internal/chat.go`);
  });

  it("hides the open button for a remote session", async () => {
    listDirMock.mockResolvedValue(listing([entry("main.go")]));
    renderPanel({ remote: true });

    await screen.findByText("main.go");
    expect(screen.queryByRole("button", { name: /open file/i })).toBeNull();
  });
});
