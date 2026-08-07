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
const gitChangesMock = vi.fn();
const gitBranchesMock = vi.fn();
vi.mock("@/../wailsjs/go/app/App", () => ({
  OpenPath: (p: string) => openPathMock(p),
  WorkspaceFsListDir: (sessionId: number, relPath: string, ignored: boolean) =>
    listDirMock(sessionId, relPath, ignored),
  WorkspaceFsGitChanges: (sessionId: number, scope: string, baseRef: string) =>
    gitChangesMock(sessionId, scope, baseRef),
  WorkspaceFsGitBranches: (sessionId: number) => gitBranchesMock(sessionId),
}));

import { useChatSidebarStore } from "@/stores/chat-sidebar-store";
import { useSessionStatusStore } from "@/stores/session-status-store";

import { FilesPanel } from "../views/files-panel";

import type { FileEntry } from "../derive";

const CWD = "/Users/me/proj";

const files: FileEntry[] = [
  { path: "internal/service/chat_svc/chat.go", plus: 5, minus: 2, lastTurn: 3 },
];

type ChangeSeed = {
  path: string;
  status?: string;
  added?: number;
  deleted?: number;
  binary?: boolean;
  oldPath?: string;
};

function change(seed: ChangeSeed) {
  return {
    oldPath: "",
    status: "modified",
    added: 0,
    deleted: 0,
    binary: false,
    ...seed,
  };
}

function changesView(
  seeds: ChangeSeed[],
  extra: { notARepo?: boolean; baseRef?: string; truncated?: boolean } = {},
) {
  return {
    notARepo: false,
    baseRef: "",
    truncated: false,
    ...extra,
    changes: seeds.map(change),
  };
}

function branchesView(
  names: string[],
  extra: { notARepo?: boolean; defaultBaseline?: string } = {},
) {
  return {
    notARepo: false,
    currentBranch: "feat/session-files",
    defaultBaseline: "origin/main",
    ...extra,
    branches: names.map((name) => ({
      name,
      remote: name.startsWith("origin/"),
    })),
  };
}

function setupUser() {
  // Radix DropdownMenu 在 happy-dom 里要关掉 pointerEvents 检查。
  return userEvent.setup({ pointerEventsCheck: 0 });
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

function gitRow(path: string): HTMLElement {
  const found = screen
    .getAllByTestId("git-row")
    .find((el) => el.getAttribute("data-path") === path);
  if (!found) throw new Error(`no git row for ${path}`);
  return found;
}

async function switchScope(name: RegExp) {
  await userEvent.click(screen.getByRole("tab", { name }));
}

beforeEach(() => {
  localStorage.clear();
  useChatSidebarStore.setState({
    open: true,
    activeTab: "files",
    filesMode: "git",
    showIgnored: false,
    gitBaselineBySession: {},
    previewBySession: {},
  });
  useSessionStatusStore.getState().__reset();
  openPathMock.mockReset();
  openPathMock.mockResolvedValue(undefined);
  listDirMock.mockReset();
  listDirMock.mockResolvedValue({ path: CWD, entries: [], truncated: false });
  gitChangesMock.mockReset();
  gitChangesMock.mockResolvedValue(changesView([]));
  gitBranchesMock.mockReset();
  gitBranchesMock.mockResolvedValue(branchesView(["main"]));
  sonnerMocks.toast.error.mockReset();
});

describe("FilesPanel git mode · 未提交档", () => {
  it("asks for the uncommitted scope on entering git mode and renders flat rows", async () => {
    gitChangesMock.mockResolvedValue(
      changesView([
        {
          path: "internal/service/chat_svc/turn.go",
          status: "modified",
          added: 42,
          deleted: 7,
        },
        { path: "go.mod", status: "modified", added: 2 },
      ]),
    );
    renderPanel({});

    await screen.findByText("turn.go");
    expect(gitChangesMock).toHaveBeenCalledWith(7, "uncommitted", "");

    // 扁平：basename 主显 + 灰色目录后缀（根目录下的文件后缀为空）。
    const turn = gitRow("internal/service/chat_svc/turn.go");
    expect(within(turn).getByText("turn.go")).toBeInTheDocument();
    expect(
      within(turn).getByText("internal/service/chat_svc"),
    ).toBeInTheDocument();
    expect(within(turn).getByText("+42")).toBeInTheDocument();
    expect(within(turn).getByText("−7")).toBeInTheDocument();

    const goMod = gitRow("go.mod");
    expect(within(goMod).queryByText("/")).toBeNull();
    expect(within(goMod).queryByText("−0")).toBeNull();
  });

  it("gives every status a readable label and hides the letter from screen readers", async () => {
    gitChangesMock.mockResolvedValue(
      changesView([
        { path: "a/m.go", status: "modified" },
        { path: "b/a.go", status: "added" },
        { path: "c/d.go", status: "deleted" },
        { path: "d/r.go", status: "renamed", oldPath: "d/old.go" },
        { path: "e/u.go", status: "untracked" },
      ]),
    );
    renderPanel({});

    await screen.findByText("m.go");
    for (const [path, label] of [
      ["a/m.go", "Modified"],
      ["b/a.go", "Added"],
      ["c/d.go", "Deleted"],
      ["d/r.go", "Renamed"],
      ["e/u.go", "Untracked"],
    ]) {
      expect(within(gitRow(path)).getByText(label)).toHaveClass("sr-only");
    }
    expect(
      gitRow("a/m.go").querySelector("[data-status-letter]"),
    ).toHaveTextContent("M");
    expect(
      gitRow("a/m.go").querySelector("[data-status-letter]"),
    ).toHaveAttribute("aria-hidden", "true");
  });

  it("shows the changed-file count on the Git segment", async () => {
    gitChangesMock.mockResolvedValue(
      changesView([{ path: "a.go" }, { path: "b.go" }]),
    );
    renderPanel({});

    await waitFor(() =>
      expect(screen.getByRole("tab", { name: /^git/i })).toHaveTextContent(
        "Git2",
      ),
    );
  });

  it("drops the previous session's count when the session changes behind another mode", async () => {
    gitChangesMock.mockResolvedValue(
      changesView([{ path: "a.go" }, { path: "b.go" }]),
    );
    const { rerender } = renderPanel({});

    const gitTab = () => screen.getByRole("tab", { name: /^git/i });
    await waitFor(() => expect(gitTab().textContent).toBe("Git2"));

    await userEvent.click(screen.getByRole("tab", { name: /^changes/i }));
    rerender(
      <FilesPanel
        sessionId={8}
        files={files}
        cwd={CWD}
        remote={false}
        onJumpToTurn={() => {}}
      />,
    );

    await waitFor(() => expect(gitTab().textContent).toBe("Git"));
  });

  it("makes no git call at all while the 变动 mode is selected", async () => {
    useChatSidebarStore.setState({ filesMode: "changes" });
    renderPanel({});

    await screen.findByRole("button", { name: /chat\.go/ });
    expect(gitChangesMock).not.toHaveBeenCalled();
    expect(gitBranchesMock).not.toHaveBeenCalled();
  });

  it("notes truncation with the actual number of listed files", async () => {
    gitChangesMock.mockResolvedValue(
      changesView([{ path: "a.go" }, { path: "b.go" }], { truncated: true }),
    );
    renderPanel({});

    expect(
      await screen.findByText(/showing the first 2 changed files/i),
    ).toBeInTheDocument();
  });

  it("opens a file on row click for a local session and stays inert for a remote one", async () => {
    gitChangesMock.mockResolvedValue(changesView([{ path: "internal/a.go" }]));
    const { unmount } = renderPanel({});

    await userEvent.click(await screen.findByRole("button", { name: /a\.go/ }));
    expect(openPathMock).toHaveBeenCalledWith(`${CWD}/internal/a.go`);

    unmount();
    renderPanel({ remote: true });
    await screen.findByText("a.go");
    expect(screen.queryByRole("button", { name: /a\.go/ })).toBeNull();
  });

  it("shows a preview button beside the row and opens the selection by path", async () => {
    gitChangesMock.mockResolvedValue(
      changesView([
        { path: "internal/a.go", status: "modified" },
        { path: "asset.zip", status: "added" },
      ]),
    );
    renderPanel({});
    await screen.findByText("a.go");

    await userEvent.click(
      within(gitRow("internal/a.go")).getByRole("button", {
        name: /preview/i,
      }),
    );
    expect(useChatSidebarStore.getState().previewBySession[7]).toEqual({
      path: "internal/a.go",
      segment: null,
    });
    // 行点击仍是打开,预览按钮点击不打开文件。
    expect(openPathMock).not.toHaveBeenCalled();
    // 不可预览文件行不出预览按钮。
    expect(
      within(gitRow("asset.zip")).queryByRole("button", {
        name: /preview/i,
      }),
    ).toBeNull();
  });

  it("still shows a preview button for a remote git session", async () => {
    gitChangesMock.mockResolvedValue(
      changesView([{ path: "internal/a.go", status: "modified" }]),
    );
    renderPanel({ remote: true });
    await screen.findByText("a.go");

    // 远端整行不可点(无 open 按钮),但预览按钮仍在。
    const rowEl = gitRow("internal/a.go");
    expect(within(rowEl).queryByRole("button", { name: /a\.go/ })).toBeNull();
    expect(
      within(rowEl).getByRole("button", { name: /preview/i }),
    ).toBeInTheDocument();
  });
});

describe("FilesPanel git mode · 本分支档与基线", () => {
  it("switches to the branch scope and shows the effective baseline in the context bar", async () => {
    gitChangesMock.mockImplementation((_id: number, scope: string) =>
      Promise.resolve(
        scope === "branch"
          ? changesView([{ path: "internal/a.go", added: 210 }], {
              baseRef: "origin/main",
            })
          : changesView([]),
      ),
    );
    renderPanel({});

    await switchScope(/this branch/i);

    await waitFor(() =>
      expect(gitChangesMock).toHaveBeenCalledWith(7, "branch", ""),
    );
    const baseline = await screen.findByRole("button", {
      name: "Compare against: origin/main",
    });
    expect(baseline).toHaveTextContent("origin/main");
    expect(gitBranchesMock).toHaveBeenCalledWith(7);
  });

  it("picks a baseline from the branch dropdown, persists it per session and refetches", async () => {
    const user = setupUser();
    gitBranchesMock.mockResolvedValue(
      branchesView(["origin/main", "main", "develop/wyz"]),
    );
    gitChangesMock.mockImplementation(
      (_id: number, scope: string, baseRef: string) =>
        Promise.resolve(
          changesView([], {
            baseRef: scope === "branch" ? baseRef || "origin/main" : "",
          }),
        ),
    );
    renderPanel({});

    await switchScope(/this branch/i);
    await user.click(
      await screen.findByRole("button", {
        name: "Compare against: origin/main",
      }),
    );
    await user.click(
      await screen.findByRole("menuitemradio", { name: "develop/wyz" }),
    );

    await waitFor(() =>
      expect(gitChangesMock).toHaveBeenCalledWith(7, "branch", "develop/wyz"),
    );
    expect(useChatSidebarStore.getState().gitBaselineBySession[7]).toBe(
      "develop/wyz",
    );
  });

  it("drops a persisted baseline that no longer exists and falls back to the inferred default", async () => {
    useChatSidebarStore.setState({
      gitBaselineBySession: { 7: "gone/branch" },
    });
    gitChangesMock.mockImplementation(
      (_id: number, _scope: string, _baseRef: string) =>
        // 后端在基线失效时回落到推断值，返回的 baseRef 与请求的不同。
        Promise.resolve(changesView([], { baseRef: "origin/main" })),
    );
    renderPanel({});

    await switchScope(/this branch/i);

    await waitFor(() =>
      expect(gitChangesMock).toHaveBeenCalledWith(7, "branch", "gone/branch"),
    );
    await waitFor(() =>
      expect(
        useChatSidebarStore.getState().gitBaselineBySession[7],
      ).toBeUndefined(),
    );
    await waitFor(() =>
      expect(gitChangesMock).toHaveBeenCalledWith(7, "branch", ""),
    );
  });

  it("keeps each session's baseline apart", async () => {
    const user = setupUser();
    gitBranchesMock.mockResolvedValue(branchesView(["origin/main", "main"]));
    gitChangesMock.mockImplementation(
      (_id: number, scope: string, baseRef: string) =>
        Promise.resolve(
          changesView([], {
            baseRef: scope === "branch" ? baseRef || "origin/main" : "",
          }),
        ),
    );
    renderPanel({});

    await switchScope(/this branch/i);
    await user.click(
      await screen.findByRole("button", {
        name: "Compare against: origin/main",
      }),
    );
    await user.click(
      await screen.findByRole("menuitemradio", { name: "main" }),
    );

    await waitFor(() =>
      expect(useChatSidebarStore.getState().gitBaselineBySession[7]).toBe(
        "main",
      ),
    );
    expect(
      useChatSidebarStore.getState().gitBaselineBySession[8],
    ).toBeUndefined();
  });

  it("shows the no-baseline empty state with a pick-a-baseline button", async () => {
    gitChangesMock.mockResolvedValue(changesView([], { baseRef: "" }));
    gitBranchesMock.mockResolvedValue(
      branchesView(["feat/x"], { defaultBaseline: "" }),
    );
    renderPanel({});

    await switchScope(/this branch/i);

    expect(
      await screen.findByText(/can't infer a default branch/i),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /pick a baseline/i }),
    ).toBeInTheDocument();
  });

  it("uses a different clean-state copy for each scope", async () => {
    gitChangesMock.mockImplementation((_id: number, scope: string) =>
      Promise.resolve(
        changesView([], { baseRef: scope === "branch" ? "origin/main" : "" }),
      ),
    );
    renderPanel({});

    expect(
      await screen.findByText(/working tree is clean/i),
    ).toBeInTheDocument();

    await switchScope(/this branch/i);
    expect(
      await screen.findByText(/no changes compared with origin\/main/i),
    ).toBeInTheDocument();
  });
});

describe("FilesPanel git mode · 空态、错误与自动重拉", () => {
  it("shows the not-a-repo state with the guidance and hides the scope switch", async () => {
    gitChangesMock.mockResolvedValue(changesView([], { notARepo: true }));
    renderPanel({});

    expect(
      await screen.findByText(/not a git repository/i),
    ).toBeInTheDocument();
    expect(screen.getByText(/switch to changes/i)).toBeInTheDocument();
    expect(screen.queryByRole("tab", { name: /uncommitted/i })).toBeNull();
  });

  it("shows the no-working-directory state without calling the backend", async () => {
    renderPanel({ cwd: "" });

    expect(
      await screen.findByText(/this session has no working directory/i),
    ).toBeInTheDocument();
    expect(gitChangesMock).not.toHaveBeenCalled();
  });

  it("surfaces the backend failure verbatim with a retry that refetches", async () => {
    gitChangesMock.mockRejectedValueOnce(
      new Error("Remote agentred is too old; please upgrade to use this view"),
    );
    gitChangesMock.mockResolvedValue(changesView([{ path: "a.go" }]));
    renderPanel({});

    expect(
      await screen.findByText(
        "Remote agentred is too old; please upgrade to use this view",
      ),
    ).toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: /retry/i }));
    expect(await screen.findByText("a.go")).toBeInTheDocument();
    expect(gitChangesMock).toHaveBeenCalledTimes(2);
  });

  it("falls back to a generic failure when the rejection carries no message", async () => {
    gitChangesMock.mockRejectedValue("");
    renderPanel({});

    expect(
      await screen.findByText(/failed to read git changes/i),
    ).toBeInTheDocument();
  });

  it("refetches when the current session's turn ends", async () => {
    renderPanel({});

    await waitFor(() => expect(gitChangesMock).toHaveBeenCalledTimes(1));

    useSessionStatusStore.getState().bumpDone(7, { kind: "done" });
    await waitFor(() => expect(gitChangesMock).toHaveBeenCalledTimes(2));

    // 别的会话结束不该惊动本面板。
    useSessionStatusStore.getState().bumpDone(9, { kind: "done" });
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(gitChangesMock).toHaveBeenCalledTimes(2);
  });
});
