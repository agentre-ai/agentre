import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const appMocks = vi.hoisted(() => ({
  WorkspaceFsReadFile: vi.fn(),
  OpenPath: vi.fn(),
}));

vi.mock("@/../wailsjs/go/app/App", () => ({
  WorkspaceFsReadFile: appMocks.WorkspaceFsReadFile,
  OpenPath: appMocks.OpenPath,
}));

import { classifyMarkdownImage, MarkdownImage } from "./markdown-image";

beforeEach(() => {
  appMocks.WorkspaceFsReadFile.mockReset();
  appMocks.OpenPath.mockReset();
  appMocks.OpenPath.mockResolvedValue(undefined);
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

  it("keeps .. traversal out of the clickable absolutePath (backend rejects the read)", () => {
    expect(
      classifyMarkdownImage("../secret.png", { cwd: "/proj", sessionId: 7 }),
    ).toEqual({
      kind: "fetch",
      relPath: "../secret.png",
      absolutePath: null,
      basename: "secret.png",
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

  it("renders an outside-cwd fallback chip as inert text (no button, no open)", () => {
    const { container } = render(
      <MarkdownImage src="/etc/passwd" cwd="/proj" sessionId={7} alt="A" />,
    );
    expect(container.textContent).toContain("passwd");
    expect(container.textContent).toContain("Cannot preview");
    expect(screen.queryByRole("button")).toBeNull();
    expect(appMocks.OpenPath).not.toHaveBeenCalled();
  });
});
