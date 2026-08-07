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
    previewBySession: {},
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

  it("closes the panel and clears the selection when close is clicked", async () => {
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
        useChatSidebarStore.getState().previewBySession[7],
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
    expect(useChatSidebarStore.getState().previewBySession[7]).toEqual({
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
    expect(useChatSidebarStore.getState().previewBySession[7].segment).toBe(
      "text",
    );
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

  it("clears the selection when the session switches", async () => {
    readFileMock.mockResolvedValue(textView("# hi"));
    openPreview("README.md", 7, "directory");
    const { rerender } = renderPanel(7);

    await screen.findByRole("complementary", { name: "File preview" });

    rerender(<FilePreviewPanel sessionId={8} />);

    await waitFor(() => {
      expect(
        useChatSidebarStore.getState().previewBySession[7],
      ).toBeUndefined();
    });
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
