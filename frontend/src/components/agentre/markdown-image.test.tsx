import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const appMocks = vi.hoisted(() => ({
  WorkspaceFsReadFile: vi.fn(),
  OpenPath: vi.fn(),
}));

const sonnerMocks = vi.hoisted(() => ({
  toast: { error: vi.fn() },
}));

vi.mock("sonner", () => sonnerMocks);

vi.mock("@/../wailsjs/go/app/App", () => ({
  WorkspaceFsReadFile: appMocks.WorkspaceFsReadFile,
  OpenPath: appMocks.OpenPath,
}));

import { classifyMarkdownImage, MarkdownImage } from "./markdown-image";

beforeEach(() => {
  appMocks.WorkspaceFsReadFile.mockReset();
  appMocks.OpenPath.mockReset();
  appMocks.OpenPath.mockResolvedValue(undefined);
  sonnerMocks.toast.error.mockReset();
});

describe("classifyMarkdownImage", () => {
  it("classifies http/https/www as remote", () => {
    expect(classifyMarkdownImage("https://x/a.png", {})).toEqual({
      kind: "remote",
      src: "https://x/a.png",
    });
    expect(classifyMarkdownImage("http://x/a.png", {})).toEqual({
      kind: "remote",
      src: "http://x/a.png",
    });
    expect(classifyMarkdownImage("www.x.com/a.png", {})).toEqual({
      kind: "remote",
      src: "www.x.com/a.png",
    });
  });

  it("classifies empty, data: and javascript: as plain with no src", () => {
    expect(classifyMarkdownImage("", {})).toEqual({
      kind: "plain",
      src: undefined,
    });
    expect(classifyMarkdownImage("data:image/png;base64,xx", {})).toEqual({
      kind: "plain",
      src: undefined,
    });
    expect(classifyMarkdownImage("javascript:alert(1)", {})).toEqual({
      kind: "plain",
      src: undefined,
    });
  });

  it("classifies a local path without sessionId as plain with stripped src", () => {
    expect(classifyMarkdownImage("a.png", { cwd: "/proj" })).toEqual({
      kind: "plain",
      src: undefined,
    });
  });

  it("keeps an absolute local path without sessionId as plain with its src (today's broken-image behaviour)", () => {
    expect(classifyMarkdownImage("/abs/a.png", {})).toEqual({
      kind: "plain",
      src: "/abs/a.png",
    });
  });

  it("treats a non-positive sessionId as absent instead of calling the workspace API", () => {
    expect(
      classifyMarkdownImage("a.png", { cwd: "/proj", sessionId: 0 }),
    ).toEqual({
      kind: "plain",
      src: undefined,
    });
  });

  it("classifies relative path + cwd + image extension + sessionId as fetch", () => {
    expect(
      classifyMarkdownImage("a.png", { cwd: "/proj", sessionId: 7 }),
    ).toEqual({
      kind: "fetch",
      relPath: "a.png",
      absolutePath: "/proj/a.png",
      basename: "a.png",
    });
  });

  it("decodes percent-encoded non-ASCII relative paths before fetch", () => {
    expect(
      classifyMarkdownImage("docs/%E6%9C%AC%E5%9C%B0.png", {
        cwd: "/proj",
        sessionId: 7,
      }),
    ).toEqual({
      kind: "fetch",
      relPath: "docs/本地.png",
      absolutePath: "/proj/docs/本地.png",
      basename: "本地.png",
    });
  });

  it("resolves file:// to a local path", () => {
    expect(
      classifyMarkdownImage("file:///proj/a.png", {
        cwd: "/proj",
        sessionId: 7,
      }),
    ).toEqual({
      kind: "fetch",
      relPath: "a.png",
      absolutePath: "/proj/a.png",
      basename: "a.png",
    });
  });

  it("resolves file://localhost to a local path", () => {
    expect(
      classifyMarkdownImage("file://localhost/proj/a.png", {
        cwd: "/proj",
        sessionId: 7,
      }),
    ).toEqual({
      kind: "fetch",
      relPath: "a.png",
      absolutePath: "/proj/a.png",
      basename: "a.png",
    });
  });

  it("strips a file:// URL with a remote host instead of auto-loading a network share", () => {
    expect(
      classifyMarkdownImage("file://server/share/a.png", {
        cwd: "/proj",
        sessionId: 7,
      }),
    ).toEqual({ kind: "plain", src: undefined });
  });

  it("resolves a localhost file URL with a Windows drive path", () => {
    expect(
      classifyMarkdownImage("file://localhost/C:/Users/x/a.png", {
        cwd: "C:\\Users\\x",
        sessionId: 7,
      }),
    ).toEqual({
      kind: "fetch",
      relPath: "a.png",
      absolutePath: "C:/Users/x/a.png",
      basename: "a.png",
    });
  });

  it("matches Windows absolute paths case-insensitively and across slash styles", () => {
    expect(
      classifyMarkdownImage("c:/USERS/x/a.png", {
        cwd: "C:\\Users\\x",
        sessionId: 7,
      }),
    ).toEqual({
      kind: "fetch",
      relPath: "a.png",
      absolutePath: "c:/USERS/x/a.png",
      basename: "a.png",
    });
  });

  it("classifies a protocol-relative image URL as remote", () => {
    expect(
      classifyMarkdownImage("//cdn.example.com/a.png", {
        cwd: "/proj",
        sessionId: 7,
      }),
    ).toEqual({ kind: "remote", src: "//cdn.example.com/a.png" });
  });

  it("classifies a non-image extension as fallback (clickable inside cwd)", () => {
    expect(
      classifyMarkdownImage("notes.txt", { cwd: "/proj", sessionId: 7 }),
    ).toEqual({
      kind: "fallback",
      absolutePath: "/proj/notes.txt",
      basename: "notes.txt",
    });
  });

  it("classifies an absolute path outside cwd as fallback (not clickable)", () => {
    expect(
      classifyMarkdownImage("/etc/passwd", { cwd: "/proj", sessionId: 7 }),
    ).toEqual({
      kind: "fallback",
      absolutePath: null,
      basename: "passwd",
    });
  });

  it("classifies a relative path without cwd as fallback (not clickable)", () => {
    expect(classifyMarkdownImage("a.png", { sessionId: 7 })).toEqual({
      kind: "fallback",
      absolutePath: null,
      basename: "a.png",
    });
  });

  it("classifies .. traversal that escapes cwd as fallback (not read, not clickable)", () => {
    expect(
      classifyMarkdownImage("../secret.png", { cwd: "/proj", sessionId: 7 }),
    ).toEqual({
      kind: "fallback",
      absolutePath: null,
      basename: "secret.png",
    });
  });

  it("classifies an absolute path with .. escaping cwd as fallback (not read, not clickable)", () => {
    expect(
      classifyMarkdownImage("/proj/../secret.png", {
        cwd: "/proj",
        sessionId: 7,
      }),
    ).toEqual({
      kind: "fallback",
      absolutePath: null,
      basename: "secret.png",
    });
  });

  it("classifies a file:// path with .. escaping cwd as fallback (not read, not clickable)", () => {
    expect(
      classifyMarkdownImage("file:///proj/../secret.png", {
        cwd: "/proj",
        sessionId: 7,
      }),
    ).toEqual({
      kind: "fallback",
      absolutePath: null,
      basename: "secret.png",
    });
  });

  it("classifies a Windows rooted path (\\foo) as fallback (not read, not clickable)", () => {
    expect(
      classifyMarkdownImage("\\Windows\\a.png", {
        cwd: "C:\\Users\\x",
        sessionId: 7,
      }),
    ).toEqual({
      kind: "fallback",
      absolutePath: null,
      basename: "a.png",
    });
  });

  it("classifies a UNC path (\\\\server\\share) as fallback (not read, not clickable)", () => {
    expect(
      classifyMarkdownImage("\\\\server\\share\\a.png", {
        cwd: "C:\\Users\\x",
        sessionId: 7,
      }),
    ).toEqual({
      kind: "fallback",
      absolutePath: null,
      basename: "a.png",
    });
  });
});

describe("MarkdownImage", () => {
  it("renders a remote image unchanged", () => {
    const { container } = render(
      <MarkdownImage src="https://x/a.png" alt="A" />,
    );
    const img = container.querySelector("img");
    expect(img?.getAttribute("src")).toBe("https://x/a.png");
    expect(img?.getAttribute("alt")).toBe("A");
  });

  it("renders a local path without sessionId as a plain img with stripped src", () => {
    const { container } = render(
      <MarkdownImage src="a.png" cwd="/proj" alt="A" />,
    );
    const img = container.querySelector("img");
    expect(img).toBeTruthy();
    expect(img?.getAttribute("src")).toBeNull();
    expect(appMocks.WorkspaceFsReadFile).not.toHaveBeenCalled();
  });

  it("fetches a local image via WorkspaceFsReadFile and renders a data URL", async () => {
    appMocks.WorkspaceFsReadFile.mockResolvedValue({
      content: "aGVsbG8=",
      contentType: "image/png",
    });
    render(<MarkdownImage src="a.png" cwd="/proj" sessionId={7} alt="A" />);

    await waitFor(() =>
      expect(appMocks.WorkspaceFsReadFile).toHaveBeenCalledWith(7, "a.png"),
    );
    const img = await screen.findByRole("img");
    expect(img.getAttribute("src")).toBe("data:image/png;base64,aGVsbG8=");
  });

  it("resets to loading when the image relPath changes", async () => {
    appMocks.WorkspaceFsReadFile.mockResolvedValueOnce({
      content: "aGVsbG8=",
      contentType: "image/png",
    });
    const { rerender } = render(
      <MarkdownImage src="a.png" cwd="/proj" sessionId={7} alt="A" />,
    );
    await screen.findByRole("img");

    appMocks.WorkspaceFsReadFile.mockResolvedValue({
      content: "d29ybGQ=",
      contentType: "image/png",
    });
    rerender(<MarkdownImage src="b.png" cwd="/proj" sessionId={7} alt="A" />);
    expect(screen.queryByRole("img")).toBeNull();
    await screen.findByRole("img");
    expect(screen.getByRole("img").getAttribute("src")).toBe(
      "data:image/png;base64,d29ybGQ=",
    );
  });

  it("resets to loading when the session changes for the same relPath", async () => {
    appMocks.WorkspaceFsReadFile.mockResolvedValueOnce({
      content: "c2Vzc2lvbi03",
      contentType: "image/png",
    });
    const { rerender } = render(
      <MarkdownImage src="a.png" cwd="/proj" sessionId={7} alt="A" />,
    );
    await screen.findByRole("img");

    let resolveSecond:
      | ((value: { content: string; contentType: string }) => void)
      | undefined;
    appMocks.WorkspaceFsReadFile.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          resolveSecond = resolve;
        }),
    );
    rerender(<MarkdownImage src="a.png" cwd="/proj" sessionId={8} alt="A" />);

    expect(appMocks.WorkspaceFsReadFile).toHaveBeenLastCalledWith(8, "a.png");
    expect(screen.queryByRole("img")).toBeNull();
    resolveSecond?.({ content: "c2Vzc2lvbi04", contentType: "image/png" });
    await screen.findByRole("img");
    expect(screen.getByRole("img").getAttribute("src")).toBe(
      "data:image/png;base64,c2Vzc2lvbi04",
    );
  });

  it("refetches when cwd changes for the same session and relPath", async () => {
    appMocks.WorkspaceFsReadFile.mockResolvedValueOnce({
      content: "cHJvai0x",
      contentType: "image/png",
    });
    const { rerender } = render(
      <MarkdownImage src="a.png" cwd="/proj-1" sessionId={7} alt="A" />,
    );
    await screen.findByRole("img");

    appMocks.WorkspaceFsReadFile.mockResolvedValueOnce({
      content: "cHJvai0y",
      contentType: "image/png",
    });
    rerender(<MarkdownImage src="a.png" cwd="/proj-2" sessionId={7} alt="A" />);

    await waitFor(() =>
      expect(appMocks.WorkspaceFsReadFile).toHaveBeenCalledTimes(2),
    );
    await waitFor(() =>
      expect(screen.getByRole("img").getAttribute("src")).toBe(
        "data:image/png;base64,cHJvai0y",
      ),
    );
  });

  it("renders a tooLarge result as the tooLarge chip", async () => {
    appMocks.WorkspaceFsReadFile.mockResolvedValue({
      content: "",
      contentType: "image/png",
      tooLarge: true,
    });
    render(<MarkdownImage src="big.png" cwd="/proj" sessionId={7} alt="A" />);

    expect(await screen.findByText("big.png")).toBeInTheDocument();
    expect(screen.getByText("Image too large")).toBeInTheDocument();
    expect(screen.queryByRole("img")).toBeNull();
  });

  it("renders a binary result as the cannot-preview chip", async () => {
    appMocks.WorkspaceFsReadFile.mockResolvedValue({
      content: "",
      contentType: "image/png",
      binary: true,
    });
    render(<MarkdownImage src="b.png" cwd="/proj" sessionId={7} alt="A" />);

    expect(await screen.findByText("b.png")).toBeInTheDocument();
    expect(screen.getByText("Cannot preview")).toBeInTheDocument();
  });

  it("renders a read error as the cannot-preview chip", async () => {
    appMocks.WorkspaceFsReadFile.mockRejectedValue(new Error("nope"));
    render(<MarkdownImage src="c.png" cwd="/proj" sessionId={7} alt="A" />);

    expect(await screen.findByText("c.png")).toBeInTheDocument();
    expect(screen.getByText("Cannot preview")).toBeInTheDocument();
  });

  it("renders an inside-cwd fallback chip as a clickable button that opens the path", () => {
    render(<MarkdownImage src="notes.txt" cwd="/proj" sessionId={7} alt="A" />);

    const button = screen.getByRole("button");
    expect(button).toHaveTextContent("notes.txt");
    expect(button).toHaveTextContent("Cannot preview");
    fireEvent.click(button);
    expect(appMocks.OpenPath).toHaveBeenCalledWith("/proj/notes.txt");
  });

  it("reports an OpenPath rejection instead of silently swallowing it", async () => {
    appMocks.OpenPath.mockRejectedValueOnce(new Error("denied"));
    render(<MarkdownImage src="notes.txt" cwd="/proj" sessionId={7} alt="A" />);

    fireEvent.click(screen.getByRole("button"));

    await waitFor(() =>
      expect(sonnerMocks.toast.error).toHaveBeenCalledWith(
        "Open failed: denied",
      ),
    );
  });

  it("renders an outside-cwd fallback chip as inert text (no button, no open)", () => {
    const { container } = render(
      <MarkdownImage src="/etc/passwd" cwd="/proj" sessionId={7} alt="A" />,
    );
    expect(container.textContent).toContain("passwd");
    expect(container.textContent).toContain("Cannot preview");
    expect(screen.queryByRole("button")).toBeNull();
    expect(appMocks.OpenPath).not.toHaveBeenCalled();
  });

  it("does not read a .. traversal that escapes cwd (inert chip)", () => {
    const { container } = render(
      <MarkdownImage src="../secret.png" cwd="/proj" sessionId={7} alt="A" />,
    );
    expect(container.textContent).toContain("secret.png");
    expect(container.textContent).toContain("Cannot preview");
    expect(screen.queryByRole("button")).toBeNull();
    expect(appMocks.WorkspaceFsReadFile).not.toHaveBeenCalled();
  });

  it("does not read an absolute path with .. escaping cwd (inert chip)", () => {
    const { container } = render(
      <MarkdownImage
        src="/proj/../secret.png"
        cwd="/proj"
        sessionId={7}
        alt="A"
      />,
    );
    expect(container.textContent).toContain("secret.png");
    expect(container.textContent).toContain("Cannot preview");
    expect(screen.queryByRole("button")).toBeNull();
    expect(appMocks.WorkspaceFsReadFile).not.toHaveBeenCalled();
  });
});
