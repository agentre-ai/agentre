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
    createModel: vi.fn((value: string, lang: string) => ({ value, lang })),
  };
  return { editor };
}

let fakeMonaco: FakeMonaco;

import { useChatSidebarStore } from "@/stores/chat-sidebar-store";
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

function openPreview(path: string, sessionId = 7) {
  useChatSidebarStore.getState().openPreview(sessionId, path);
}

describe("FilePreviewPanel", () => {
  it("renders nothing when no file is selected", () => {
    const { container } = renderPanel();
    expect(container).toBeEmptyDOMElement();
    expect(readFileMock).not.toHaveBeenCalled();
  });

  it("reads the selected file on open and renders the header", async () => {
    readFileMock.mockResolvedValue(textView("# Hi"));
    openPreview("docs/guide.md");

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
    openPreview("README.md");
    renderPanel();

    const panel = await screen.findByRole("complementary", {
      name: "File preview",
    });
    // MarkdownText 渲染出 <strong>。
    await within(panel).findByText("bold");
  });

  it("switches markdown to text mode showing raw source via Monaco", async () => {
    readFileMock.mockResolvedValue(textView("# raw"));
    openPreview("README.md");
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
    openPreview("README.md");
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

  it("shows a code file in text mode by default and only two segments", async () => {
    readFileMock.mockResolvedValue(textView("package main"));
    openPreview("main.go");
    renderPanel();

    const panel = await screen.findByRole("complementary", {
      name: "File preview",
    });
    expect(within(panel).queryByRole("button", { name: "Render" })).toBeNull();
    expect(
      within(panel).getByRole("button", { name: "Diff" }),
    ).toBeInTheDocument();
  });

  it("fetches git HEAD content when switching a code file to diff mode", async () => {
    readFileMock.mockResolvedValue(textView("package main\n"));
    gitFileContentMock.mockResolvedValue({
      content: "package main\n// old\n",
      notARepo: false,
      hasHead: true,
    });
    openPreview("main.go");
    renderPanel();

    const panel = await screen.findByRole("complementary", {
      name: "File preview",
    });
    expect(gitFileContentMock).not.toHaveBeenCalled();
    await userEvent.click(within(panel).getByRole("button", { name: "Diff" }));

    await waitFor(() =>
      expect(gitFileContentMock).toHaveBeenCalledWith(7, "main.go"),
    );
    // 头部条:对比 · HEAD → 工作区 + 增删图例。
    expect(
      within(panel).getByText("Diff · HEAD → working tree"),
    ).toBeInTheDocument();
    expect(within(panel).getByText("Added")).toBeInTheDocument();
    expect(within(panel).getByText("Deleted")).toBeInTheDocument();
    // Monaco diff editor 被创建(只读, side-by-side)。
    await waitFor(() =>
      expect(fakeMonaco.editor.createDiffEditor).toHaveBeenCalledWith(
        expect.any(HTMLElement),
        expect.objectContaining({ readOnly: true }),
      ),
    );
  });

  it("renders the no-git-baseline empty state when the directory is not a repo", async () => {
    readFileMock.mockResolvedValue(textView("hello"));
    gitFileContentMock.mockResolvedValue({
      content: "",
      notARepo: true,
      hasHead: false,
    });
    openPreview("a.txt");
    renderPanel();

    const panel = await screen.findByRole("complementary", {
      name: "File preview",
    });
    await userEvent.click(within(panel).getByRole("button", { name: "Diff" }));

    expect(
      await within(panel).findByText("No git baseline to compare"),
    ).toBeInTheDocument();
  });

  it("renders an image with a data URL and alt text", async () => {
    readFileMock.mockResolvedValue(imageView("aGVsbG8=", "image/png"));
    openPreview("assets/logo.png");
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
    openPreview("archive.bin");
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
    openPreview("photo.jpg");
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
    openPreview("README.md");
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
    openPreview("README.md");
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

  it("switching files re-reads the new file and switches segment back on close", async () => {
    readFileMock.mockResolvedValue(textView("# a"));
    openPreview("a.md");
    const { rerender } = renderPanel();

    await screen.findByRole("complementary", { name: "File preview" });
    // 打开第二个文件(面板保持打开,档位保留)。
    useChatSidebarStore.getState().setPreviewSegment(7, "text");
    useChatSidebarStore.getState().openPreview(7, "b.md");
    rerender(<FilePreviewPanel sessionId={7} />);

    await waitFor(() => {
      expect(readFileMock).toHaveBeenLastCalledWith(7, "b.md");
    });
    expect(useChatSidebarStore.getState().previewBySession[7].segment).toBe(
      "text",
    );
  });

  it("clears the selection when the session switches", async () => {
    readFileMock.mockResolvedValue(textView("# hi"));
    openPreview("README.md");
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
    openPreview("README.md");
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
});
