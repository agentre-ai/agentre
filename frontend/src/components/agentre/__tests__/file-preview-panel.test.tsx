import "@testing-library/jest-dom/vitest";

import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

const readFileMock = vi.fn();
const gitFileContentMock = vi.fn();
vi.mock("@/../wailsjs/go/app/App", () => ({
  WorkspaceFsReadFile: (sessionId: number, relPath: string) =>
    readFileMock(sessionId, relPath),
  WorkspaceFsGitFileContent: (sessionId: number, relPath: string) =>
    gitFileContentMock(sessionId, relPath),
}));

// Monaco 接缝:面板内的 CodePreview / DiffPreview / MarkdownSourceView 在 happy-dom
// 里跑不了真实 Monaco,按 task 5 定好的 seam 把 loadMonaco 换成 fake 命名空间。
const loaderMocks = vi.hoisted(() => ({ loadMonaco: vi.fn() }));
vi.mock("@/lib/file-preview/monaco-loader", () => loaderMocks);

type FakeEditor = {
  options: Record<string, unknown>;
  setValue: ReturnType<typeof vi.fn>;
  dispose: ReturnType<typeof vi.fn>;
};
type FakeDiff = {
  options: Record<string, unknown>;
  setModel: ReturnType<typeof vi.fn>;
  dispose: ReturnType<typeof vi.fn>;
};

type FakeMonaco = {
  editor: {
    create: ReturnType<typeof vi.fn>;
    createDiffEditor: ReturnType<typeof vi.fn>;
    createModel: ReturnType<typeof vi.fn>;
  };
};

function createFakeMonaco(): FakeMonaco {
  const editor = {
    setTheme: vi.fn(),
    create: vi.fn(
      (_container: HTMLElement, options: Record<string, unknown>) => {
        const e: FakeEditor = {
          options,
          setValue: vi.fn(),
          dispose: vi.fn(),
        };
        return e;
      },
    ),
    createDiffEditor: vi.fn(
      (_container: HTMLElement, options: Record<string, unknown>) => {
        const d: FakeDiff = {
          options,
          setModel: vi.fn(),
          dispose: vi.fn(),
        };
        return d;
      },
    ),
    createModel: vi.fn((value: string, lang: string) => ({
      value,
      lang,
      dispose: vi.fn(),
    })),
  };
  return { editor };
}

let fakeMonaco: FakeMonaco;

import {
  selectActivePreviewTab,
  useChatSidebarStore,
  type PreviewSourceMode,
} from "@/stores/chat-sidebar-store";
import { useSessionStatusStore } from "@/stores/session-status-store";

import { FilePreviewPanel } from "../file-preview/file-preview-panel";

function textView(content: string) {
  return { content, contentType: "", binary: false, tooLarge: false };
}
function imageView(content: string, contentType: string) {
  return { content, contentType, binary: false, tooLarge: false };
}

beforeEach(() => {
  localStorage.clear();
  useChatSidebarStore.setState({
    open: true,
    activeTab: "files",
    filesMode: "changes",
    showIgnored: false,
    previewTabsBySession: {},
  });
  useSessionStatusStore.getState().__reset();
  readFileMock.mockReset();
  gitFileContentMock.mockReset();
  loaderMocks.loadMonaco.mockReset();
  fakeMonaco = createFakeMonaco();
  loaderMocks.loadMonaco.mockResolvedValue(fakeMonaco as never);
});

function renderPanel(sessionId = 7) {
  return render(<FilePreviewPanel sessionId={sessionId} />);
}

function openPreview(
  path: string,
  sessionId = 7,
  sourceMode: PreviewSourceMode = "directory",
) {
  useChatSidebarStore.getState().openPreview(sessionId, path, sourceMode);
}

describe("FilePreviewPanel", () => {
  it("renders nothing when no file is selected", () => {
    const { container } = renderPanel();
    expect(container).toBeEmptyDOMElement();
    expect(readFileMock).not.toHaveBeenCalled();
  });

  it("reads the selected file on open and renders the header", async () => {
    readFileMock.mockResolvedValue(textView("# Hi"));
    openPreview("docs/guide.md", 7, "directory");

    renderPanel();

    await screen.findByRole("complementary", { name: "File preview" });
    expect(readFileMock).toHaveBeenCalledWith(7, "docs/guide.md");

    // header: 文件名(含目录前缀) + markdown 三档分段 + 关闭按钮。
    const panel = screen.getByRole("complementary", { name: "File preview" });
    expect(within(panel).getByText("guide.md")).toBeInTheDocument();
    expect(within(panel).getByText("docs/")).toBeInTheDocument();
    for (const seg of ["Render", "Text", "Split"]) {
      expect(
        within(panel).getByRole("button", { name: seg }),
      ).toBeInTheDocument();
    }
    expect(
      within(panel).getByRole("button", { name: "Close preview" }),
    ).toBeInTheDocument();
  });

  it("renders markdown in render mode by default (GFM MarkdownText)", async () => {
    readFileMock.mockResolvedValue(textView("**bold**"));
    openPreview("README.md", 7, "directory");
    renderPanel();

    const panel = await screen.findByRole("complementary", {
      name: "File preview",
    });
    // MarkdownText 渲染出 <strong>。
    await within(panel).findByText("bold");
  });

  it("switches markdown to text mode showing raw source via Monaco", async () => {
    readFileMock.mockResolvedValue(textView("# raw"));
    openPreview("README.md", 7, "directory");
    renderPanel();

    const panel = await screen.findByRole("complementary", {
      name: "File preview",
    });
    await userEvent.click(within(panel).getByRole("button", { name: "Text" }));

    // MarkdownSourceView → CodePreview 以 markdown 语言建只读编辑器。
    await waitFor(() =>
      expect(fakeMonaco.editor.create).toHaveBeenCalledWith(
        expect.any(HTMLElement),
        expect.objectContaining({ language: "markdown", readOnly: true }),
      ),
    );
  });

  it("switches markdown to split mode showing source and render side by side", async () => {
    readFileMock.mockResolvedValue(textView("# Title\n\nSome **bold** body."));
    openPreview("README.md", 7, "directory");
    renderPanel();

    const panel = await screen.findByRole("complementary", {
      name: "File preview",
    });
    await userEvent.click(within(panel).getByRole("button", { name: "Split" }));

    // 渲染侧出现 GFM 的 <strong>;源码侧 Monaco 编辑器建了 markdown 语言。
    await within(panel).findByText("bold");
    await waitFor(() =>
      expect(fakeMonaco.editor.create).toHaveBeenCalledWith(
        expect.any(HTMLElement),
        expect.objectContaining({ language: "markdown", readOnly: true }),
      ),
    );
  });

  it("shows a code file opened from directory mode as content with no segment control", async () => {
    readFileMock.mockResolvedValue(textView("package main"));
    openPreview("main.go", 7, "directory");
    renderPanel();

    const panel = await screen.findByRole("complementary", {
      name: "File preview",
    });
    // 代码 / 文本没有分段控件（不再有「文本 / 对比」两档）。
    expect(within(panel).queryByRole("button", { name: "Render" })).toBeNull();
    expect(within(panel).queryByRole("button", { name: "Text" })).toBeNull();
    expect(within(panel).queryByRole("button", { name: "Diff" })).toBeNull();
    // 目录打开 → 只读内容，不调 GitFileContent。
    expect(gitFileContentMock).not.toHaveBeenCalled();
    await waitFor(() =>
      expect(fakeMonaco.editor.create).toHaveBeenCalledWith(
        expect.any(HTMLElement),
        expect.objectContaining({ language: "go", readOnly: true }),
      ),
    );
  });

  it("shows a git-opened code file as a diff with no segment control", async () => {
    readFileMock.mockResolvedValue(textView("package main\n"));
    gitFileContentMock.mockResolvedValue({
      content: "package main\n// old\n",
      notARepo: false,
      hasHead: true,
    });
    openPreview("main.go", 7, "git");
    renderPanel();

    const panel = await screen.findByRole("complementary", {
      name: "File preview",
    });
    // 首视图直接是对比：调 GitFileContent、出对比头部条 + 增删图例、建 diff editor。
    await waitFor(() =>
      expect(gitFileContentMock).toHaveBeenCalledWith(7, "main.go"),
    );
    expect(
      within(panel).getByText("Diff · HEAD → working tree"),
    ).toBeInTheDocument();
    expect(within(panel).getByText("Added")).toBeInTheDocument();
    expect(within(panel).getByText("Deleted")).toBeInTheDocument();
    await waitFor(() =>
      expect(fakeMonaco.editor.createDiffEditor).toHaveBeenCalledWith(
        expect.any(HTMLElement),
        expect.objectContaining({ readOnly: true }),
      ),
    );
    // 代码 / 文本没有分段控件。
    expect(within(panel).queryByRole("button", { name: "Text" })).toBeNull();
    expect(within(panel).queryByRole("button", { name: "Diff" })).toBeNull();
  });

  it("shows a changes-mode code file as a diff too", async () => {
    readFileMock.mockResolvedValue(textView("package main\n"));
    gitFileContentMock.mockResolvedValue({
      content: "package main\n// old\n",
      notARepo: false,
      hasHead: true,
    });
    openPreview("main.go", 7, "changes");
    renderPanel();

    const panel = await screen.findByRole("complementary", {
      name: "File preview",
    });
    await waitFor(() =>
      expect(gitFileContentMock).toHaveBeenCalledWith(7, "main.go"),
    );
    expect(
      within(panel).getByText("Diff · HEAD → working tree"),
    ).toBeInTheDocument();
  });

  it("renders the no-git-baseline empty state for a git-opened code file outside a repo", async () => {
    readFileMock.mockResolvedValue(textView("hello"));
    gitFileContentMock.mockResolvedValue({
      content: "",
      notARepo: true,
      hasHead: false,
    });
    openPreview("a.txt", 7, "git");
    renderPanel();

    const panel = await screen.findByRole("complementary", {
      name: "File preview",
    });
    expect(
      await within(panel).findByText("No git baseline to compare"),
    ).toBeInTheDocument();
  });

  it("shows markdown content segments even when opened from git mode (never a diff)", async () => {
    readFileMock.mockResolvedValue(textView("# Title"));
    openPreview("README.md", 7, "git");
    renderPanel();

    const panel = await screen.findByRole("complementary", {
      name: "File preview",
    });
    // markdown 三档仍在、默认渲染档；从任何模式都不调 GitFileContent。
    for (const seg of ["Render", "Text", "Split"]) {
      expect(
        within(panel).getByRole("button", { name: seg }),
      ).toBeInTheDocument();
    }
    expect(gitFileContentMock).not.toHaveBeenCalled();
    await within(panel).findByRole("heading", { level: 1, name: "Title" });
  });

  it("renders an image with a data URL and alt text", async () => {
    readFileMock.mockResolvedValue(imageView("aGVsbG8=", "image/png"));
    openPreview("assets/logo.png", 7, "directory");
    renderPanel();

    const panel = await screen.findByRole("complementary", {
      name: "File preview",
    });
    const img = await within(panel).findByAltText("logo.png");
    expect(img).toHaveAttribute("src", "data:image/png;base64,aGVsbG8=");
    // 图片没有视图分段。
    expect(within(panel).queryByRole("button", { name: "Render" })).toBeNull();
    expect(within(panel).queryByRole("button", { name: "Text" })).toBeNull();
  });

  it("shows the binary state and a retry for a binary file", async () => {
    readFileMock.mockResolvedValue({
      content: "",
      contentType: "",
      binary: true,
      tooLarge: false,
    });
    openPreview("archive.bin", 7, "directory");
    renderPanel();

    const panel = await screen.findByRole("complementary", {
      name: "File preview",
    });
    expect(
      await within(panel).findByText(/Binary file, cannot preview/),
    ).toBeInTheDocument();
    expect(
      within(panel).getByRole("button", { name: /Retry/i }),
    ).toBeInTheDocument();
  });

  it("shows the too-large state with the image threshold hint for images", async () => {
    readFileMock.mockResolvedValue({
      content: "",
      contentType: "",
      binary: false,
      tooLarge: true,
    });
    openPreview("photo.jpg", 7, "directory");
    renderPanel();

    const panel = await screen.findByRole("complementary", {
      name: "File preview",
    });
    expect(
      await within(panel).findByText(/File too large to preview/),
    ).toBeInTheDocument();
    expect(
      within(panel).getByText(/Image larger than 10 MiB/),
    ).toBeInTheDocument();
  });

  it("shows the read error text with a retry that re-reads", async () => {
    readFileMock.mockRejectedValueOnce(
      new Error("Path is outside the session working directory"),
    );
    readFileMock.mockResolvedValue(textView("# ok"));
    openPreview("README.md", 7, "directory");
    renderPanel();

    const panel = await screen.findByRole("complementary", {
      name: "File preview",
    });
    expect(
      await within(panel).findByText(
        "Path is outside the session working directory",
      ),
    ).toBeInTheDocument();

    await userEvent.click(
      within(panel).getByRole("button", { name: /Retry/i }),
    );
    expect(readFileMock).toHaveBeenCalledTimes(2);
    await within(panel).findByRole("heading", { level: 1, name: "ok" });
  });

  it("closes the panel and drops the last tab when close is clicked", async () => {
    readFileMock.mockResolvedValue(textView("# hi"));
    openPreview("README.md", 7, "directory");
    renderPanel();

    const panel = await screen.findByRole("complementary", {
      name: "File preview",
    });
    await userEvent.click(
      within(panel).getByRole("button", { name: "Close preview" }),
    );

    await waitFor(() => {
      expect(
        useChatSidebarStore.getState().previewTabsBySession[7],
      ).toBeUndefined();
    });
  });

  it("keeps a newly opened file when a close is still animating", async () => {
    readFileMock.mockResolvedValue(textView("# a"));
    openPreview("a.md", 7, "directory");
    renderPanel();

    const panel = await screen.findByRole("complementary", {
      name: "File preview",
    });
    await userEvent.click(
      within(panel).getByRole("button", { name: "Close preview" }),
    );
    // 200ms 出场动画期间打开另一个文件:旧 timer 不能把新选择清掉。
    useChatSidebarStore.getState().openPreview(7, "b.md", "directory");

    // 等关闭动画的 timer 跑完,断言 b.md 仍然选中、面板仍然开着。
    await new Promise((resolve) => setTimeout(resolve, 300));
    expect(
      selectActivePreviewTab(useChatSidebarStore.getState(), 7),
    ).toMatchObject({
      path: "b.md",
      segment: null,
      sourceMode: "directory",
    });
  });

  it("switching files re-reads the new file and keeps the segment in the same mode", async () => {
    readFileMock.mockResolvedValue(textView("# a"));
    openPreview("a.md", 7, "directory");
    const { rerender } = renderPanel();

    await screen.findByRole("complementary", { name: "File preview" });
    // 打开第二个文件(同模式,面板保持打开,markdown 档位保留)。
    useChatSidebarStore.getState().setPreviewSegment(7, "text");
    useChatSidebarStore.getState().openPreview(7, "b.md", "directory");
    rerender(<FilePreviewPanel sessionId={7} />);

    await waitFor(() => {
      expect(readFileMock).toHaveBeenLastCalledWith(7, "b.md");
    });
    expect(
      selectActivePreviewTab(useChatSidebarStore.getState(), 7)?.segment,
    ).toBe("text");
  });

  it("re-sets the first view when the same file is opened from a different mode", async () => {
    readFileMock.mockResolvedValue(textView("hello"));
    gitFileContentMock.mockResolvedValue({
      content: "",
      notARepo: false,
      hasHead: true,
    });
    openPreview("a.go", 7, "directory");
    const { rerender } = renderPanel();

    const panel = await screen.findByRole("complementary", {
      name: "File preview",
    });
    await waitFor(() =>
      expect(fakeMonaco.editor.create).toHaveBeenCalledWith(
        expect.any(HTMLElement),
        expect.objectContaining({ language: "go", readOnly: true }),
      ),
    );
    expect(gitFileContentMock).not.toHaveBeenCalled();

    // 换到 Git 模式重开同一文件 → 首视图变对比。
    useChatSidebarStore.getState().openPreview(7, "a.go", "git");
    rerender(<FilePreviewPanel sessionId={7} />);

    await waitFor(() =>
      expect(gitFileContentMock).toHaveBeenCalledWith(7, "a.go"),
    );
    expect(
      within(panel).getByText("Diff · HEAD → working tree"),
    ).toBeInTheDocument();
  });

  it("swaps the whole tab table when the session switches and keeps each session's own", async () => {
    readFileMock.mockResolvedValue(textView("# hi"));
    openPreview("README.md", 7, "directory");
    const { rerender } = renderPanel(7);

    await screen.findByRole("complementary", { name: "File preview" });

    // 会话 8 没开过任何预览 → 面板收起，但会话 7 的标签表照旧留着。
    rerender(<FilePreviewPanel sessionId={8} />);
    await waitFor(() => {
      expect(
        screen.queryByRole("complementary", { name: "File preview" }),
      ).toBeNull();
    });
    expect(
      selectActivePreviewTab(useChatSidebarStore.getState(), 7)?.path,
    ).toBe("README.md");

    // 切回来还是原来那个标签。
    rerender(<FilePreviewPanel sessionId={7} />);
    const panel = await screen.findByRole("complementary", {
      name: "File preview",
    });
    expect(within(panel).getByText("README.md")).toBeInTheDocument();
  });

  it("re-reads the open file when the session turn ends (doneTick)", async () => {
    readFileMock.mockResolvedValue(textView("# v1"));
    openPreview("README.md", 7, "directory");
    renderPanel();

    const panel = await screen.findByRole("complementary", {
      name: "File preview",
    });
    await within(panel).findByRole("heading", { level: 1, name: "v1" });
    expect(readFileMock).toHaveBeenCalledTimes(1);

    readFileMock.mockResolvedValue(textView("# v2"));
    useSessionStatusStore.getState().bumpDone(7, { kind: "done" });

    await within(panel).findByRole("heading", { level: 1, name: "v2" });
    expect(readFileMock).toHaveBeenCalledTimes(2);
  });

  it("re-reads and re-computes the diff for a git-opened code file on doneTick", async () => {
    readFileMock.mockResolvedValue(textView("v1\n"));
    gitFileContentMock.mockResolvedValue({
      content: "old\n",
      notARepo: false,
      hasHead: true,
    });
    openPreview("a.go", 7, "git");
    renderPanel();

    await screen.findByRole("complementary", { name: "File preview" });
    await waitFor(() =>
      expect(gitFileContentMock).toHaveBeenCalledWith(7, "a.go"),
    );
    expect(readFileMock).toHaveBeenCalledTimes(1);
    expect(gitFileContentMock).toHaveBeenCalledTimes(1);

    readFileMock.mockResolvedValue(textView("v2\n"));
    useSessionStatusStore.getState().bumpDone(7, { kind: "done" });

    await waitFor(() => expect(readFileMock).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(gitFileContentMock).toHaveBeenCalledTimes(2));
    await waitFor(() =>
      expect(fakeMonaco.editor.createDiffEditor).toHaveBeenCalledWith(
        expect.any(HTMLElement),
        expect.objectContaining({ readOnly: true }),
      ),
    );
  });

  describe("tab strip", () => {
    const store = () => useChatSidebarStore.getState();

    async function openTwoTabs() {
      readFileMock.mockResolvedValue(textView("# a"));
      store().openPreviewInNewTab(7, "docs/a.md", "directory");
      store().openPreview(7, "b.md", "directory");
      renderPanel();
      return screen.findByRole("complementary", { name: "File preview" });
    }

    it("renders no tab strip while a single file is open", async () => {
      readFileMock.mockResolvedValue(textView("# a"));
      openPreview("a.md", 7, "directory");
      renderPanel();

      await screen.findByRole("complementary", { name: "File preview" });
      expect(screen.queryByRole("tablist")).toBeNull();
    });

    it("renders one tab per open file, the temporary one in italic and the active one marked", async () => {
      const panel = await openTwoTabs();

      const strip = within(panel).getByRole("tablist", {
        name: "Preview tabs",
      });
      const tabs = within(strip).getAllByRole("tab");
      expect(tabs).toHaveLength(2);
      // 标签显示文件名(不是整条路径);常驻标签在前、临时标签在后。
      expect(tabs[0]).toHaveTextContent("a.md");
      expect(tabs[1]).toHaveTextContent("b.md");
      expect(tabs[0]).toHaveAttribute("data-active", "false");
      expect(tabs[1]).toHaveAttribute("data-active", "true");
      // 临时标签的文件名以斜体区分。
      expect(within(tabs[1]).getByText("b.md")).toHaveClass("italic");
      expect(within(tabs[0]).getByText("a.md")).not.toHaveClass("italic");
      // header 仍然只讲当前活动文件。
      expect(
        within(screen.getByTestId("file-preview-header")).getByText("b.md"),
      ).toBeInTheDocument();
    });

    it("activates another tab on click and keeps each tab's markdown segment", async () => {
      const panel = await openTwoTabs();

      await userEvent.click(
        within(panel).getByRole("button", { name: "Split" }),
      );
      const tabs = within(panel).getAllByRole("tab");
      await userEvent.click(tabs[0]);

      await waitFor(() =>
        expect(readFileMock).toHaveBeenLastCalledWith(7, "docs/a.md"),
      );
      expect(store().previewTabsBySession[7].activePath).toBe("docs/a.md");
      // 刚切过去的标签用自己的默认档，b.md 的「双栏」不跟着跑。
      expect(
        within(panel).getByRole("button", { name: "Render" }),
      ).toHaveAttribute("aria-pressed", "true");

      await userEvent.click(within(panel).getAllByRole("tab")[1]);
      expect(
        within(panel).getByRole("button", { name: "Split" }),
      ).toHaveAttribute("aria-pressed", "true");
    });

    it("promotes the temporary tab on double click", async () => {
      const panel = await openTwoTabs();

      await userEvent.dblClick(within(panel).getAllByRole("tab")[1]);

      expect(store().previewTabsBySession[7].tabs[1].isPreview).toBe(false);
    });

    it("closes the active tab from the header and activates the neighbour", async () => {
      const panel = await openTwoTabs();

      await userEvent.click(
        within(panel).getByRole("button", { name: "Close preview" }),
      );

      // 没有右邻居 → 激活左邻居，面板不收起。
      await waitFor(() =>
        expect(store().previewTabsBySession[7].activePath).toBe("docs/a.md"),
      );
      expect(screen.queryByRole("tablist")).toBeNull();
      expect(
        screen.getByRole("complementary", { name: "File preview" }),
      ).toBeInTheDocument();
    });

    it("closes a tab from its own close button and collapses the panel at zero", async () => {
      const panel = await openTwoTabs();

      const tabs = within(panel).getAllByRole("tab");
      await userEvent.click(
        within(tabs[0]).getByRole("button", { name: "Close Tab" }),
      );
      expect(store().previewTabsBySession[7].tabs).toHaveLength(1);

      await userEvent.click(
        within(panel).getByRole("button", { name: "Close preview" }),
      );
      await waitFor(() =>
        expect(store().previewTabsBySession[7]).toBeUndefined(),
      );
      await waitFor(() =>
        expect(
          screen.queryByRole("complementary", { name: "File preview" }),
        ).toBeNull(),
      );
    });

    it("lists every tab in the overflow menu and switches to the picked one", async () => {
      const panel = await openTwoTabs();

      await userEvent.click(
        within(panel).getByRole("button", { name: "Open Tab menu" }),
      );
      const items = await screen.findAllByRole("menuitem");
      expect(items).toHaveLength(2);
      expect(items[0]).toHaveTextContent("a.md");

      await userEvent.click(items[0]);
      expect(store().previewTabsBySession[7].activePath).toBe("docs/a.md");
    });

    it("pins a tab from its context menu, which also makes it permanent", async () => {
      const panel = await openTwoTabs();

      const tab = within(panel).getAllByRole("tab")[1];
      await userEvent.pointer({ keys: "[MouseRight]", target: tab });
      expect(
        screen.getByRole("menuitem", { name: "Close Others" }),
      ).toBeInTheDocument();
      expect(
        screen.getByRole("menuitem", { name: "Close Tabs to the Right" }),
      ).toBeInTheDocument();
      await userEvent.click(screen.getByRole("menuitem", { name: /Pin/ }));

      expect(store().previewTabsBySession[7].tabs[1]).toMatchObject({
        isPinned: true,
        isPreview: false,
      });
    });

    it("keeps the tab of a file that has been deleted and shows the read failure", async () => {
      readFileMock.mockResolvedValue(textView("# a"));
      store().openPreviewInNewTab(7, "docs/a.md", "directory");
      readFileMock.mockRejectedValue(new Error("no such file or directory"));
      store().openPreviewInNewTab(7, "gone.md", "directory");
      renderPanel();

      const panel = await screen.findByRole("complementary", {
        name: "File preview",
      });
      expect(
        await within(panel).findByText("no such file or directory"),
      ).toBeInTheDocument();
      expect(
        within(panel).getByRole("button", { name: /Retry/i }),
      ).toBeInTheDocument();
      // 标签照常留着,不自动关闭。
      expect(within(panel).getAllByRole("tab")).toHaveLength(2);
      expect(store().previewTabsBySession[7].activePath).toBe("gone.md");
    });

    it("moves between tabs with the arrow keys, activating the one focused", async () => {
      const panel = await openTwoTabs();
      const tabs = within(panel).getAllByRole("tab");

      // roving tabindex：标签条整体只有活动标签一个 Tab 停靠点。
      expect(tabs.map((el) => el.getAttribute("tabindex"))).toEqual([
        "-1",
        "0",
      ]);

      await userEvent.click(tabs[0]);
      expect(tabs[0]).toHaveFocus();
      expect(tabs.map((el) => el.getAttribute("tabindex"))).toEqual([
        "0",
        "-1",
      ]);

      await userEvent.keyboard("{ArrowRight}");
      expect(tabs[1]).toHaveFocus();
      expect(store().previewTabsBySession[7].activePath).toBe("b.md");
      // 末尾不回绕。
      await userEvent.keyboard("{ArrowRight}");
      expect(tabs[1]).toHaveFocus();

      await userEvent.keyboard("{ArrowLeft}");
      expect(tabs[0]).toHaveFocus();
      expect(store().previewTabsBySession[7].activePath).toBe("docs/a.md");
      await userEvent.keyboard("{ArrowLeft}");
      expect(tabs[0]).toHaveFocus();
    });

    it("closes the active tab on Esc from anywhere in the panel", async () => {
      const panel = await openTwoTabs();

      await userEvent.click(within(panel).getAllByRole("tab")[0]);
      await userEvent.keyboard("{Escape}");

      expect(store().previewTabsBySession[7].tabs).toHaveLength(1);
      expect(store().previewTabsBySession[7].activePath).toBe("b.md");

      // 最后一个标签也能用 Esc 关掉（此时标签条已收起，焦点在 header 上），
      // 面板随之收起。
      within(panel).getByRole("button", { name: "Close preview" }).focus();
      await userEvent.keyboard("{Escape}");
      await waitFor(() =>
        expect(store().previewTabsBySession[7]).toBeUndefined(),
      );
    });

    it("scrolls the active tab into view", async () => {
      const scrollIntoView = vi.fn();
      const original = HTMLElement.prototype.scrollIntoView;
      HTMLElement.prototype.scrollIntoView = scrollIntoView;
      try {
        const panel = await openTwoTabs();
        scrollIntoView.mockClear();
        await userEvent.click(within(panel).getAllByRole("tab")[0]);
        expect(scrollIntoView).toHaveBeenCalled();
      } finally {
        HTMLElement.prototype.scrollIntoView = original;
      }
    });
  });

  it("re-reads only content on doneTick for a directory-opened code file", async () => {
    readFileMock.mockResolvedValue(textView("v1"));
    openPreview("a.go", 7, "directory");
    renderPanel();

    await screen.findByRole("complementary", { name: "File preview" });
    await waitFor(() =>
      expect(fakeMonaco.editor.create).toHaveBeenCalledWith(
        expect.any(HTMLElement),
        expect.objectContaining({ language: "go", readOnly: true }),
      ),
    );
    expect(gitFileContentMock).not.toHaveBeenCalled();
    expect(readFileMock).toHaveBeenCalledTimes(1);

    readFileMock.mockResolvedValue(textView("v2"));
    useSessionStatusStore.getState().bumpDone(7, { kind: "done" });

    await waitFor(() => expect(readFileMock).toHaveBeenCalledTimes(2));
    expect(gitFileContentMock).not.toHaveBeenCalled();
  });
});
