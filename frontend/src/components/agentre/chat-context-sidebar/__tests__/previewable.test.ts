import { describe, expect, it } from "vitest";

import { previewKind, resolvePreviewRelPath } from "../previewable";

describe("previewKind", () => {
  it("classifies markdown files (.md / .markdown) as markdown, not .mdx", () => {
    expect(previewKind("README.md")).toBe("markdown");
    expect(previewKind("docs/guide.markdown")).toBe("markdown");
    // MDX 含 JSX,GFM 渲染会碎——明确不入档(spec 决策 3)。
    expect(previewKind("docs/page.mdx")).toBeNull();
  });

  it("classifies code / text extensions as code", () => {
    for (const p of [
      "main.go",
      "src/app.ts",
      "index.js",
      "page.tsx",
      "package.json",
      "Dockerfile.yaml",
      "style.css",
      "notes.txt",
      "debug.log",
      "a.py",
      "b.sh",
      "c.sql",
    ]) {
      expect(previewKind(p)).toBe("code");
    }
  });

  it("classifies image extensions as image", () => {
    for (const p of [
      "logo.png",
      "photo.jpg",
      "a.jpeg",
      "anim.gif",
      "pic.webp",
      "x.avif",
      "i.bmp",
      "favicon.ico",
      "diagram.svg",
    ]) {
      expect(previewKind(p)).toBe("image");
    }
  });

  it("is case-insensitive for extensions", () => {
    expect(previewKind("README.MD")).toBe("markdown");
    expect(previewKind("logo.PNG")).toBe("image");
    expect(previewKind("main.GO")).toBe("code");
  });

  it("returns null for non-previewable file types", () => {
    for (const p of [
      "archive.zip",
      "doc.pdf",
      "movie.mp4",
      "audio.mp3",
      "app.exe",
      "font.woff2",
      "secret.bin",
      "Dockerfile",
      "LICENSE",
      ".gitignore",
    ]) {
      expect(previewKind(p)).toBeNull();
    }
  });
});

describe("resolvePreviewRelPath", () => {
  const CWD = "/Users/me/proj";

  it("passes relative paths through as-is", () => {
    expect(resolvePreviewRelPath("README.md", CWD)).toBe("README.md");
    expect(resolvePreviewRelPath("internal/a.go", CWD)).toBe("internal/a.go");
  });

  it("strips the cwd prefix from an absolute path inside the cwd", () => {
    expect(resolvePreviewRelPath("/Users/me/proj/README.md", CWD)).toBe(
      "README.md",
    );
    expect(
      resolvePreviewRelPath("/Users/me/proj/internal/service/a.go", CWD),
    ).toBe("internal/service/a.go");
  });

  it("rejects an absolute path outside the cwd", () => {
    expect(resolvePreviewRelPath("/Users/other/secret.md", CWD)).toBeNull();
    expect(resolvePreviewRelPath("/tmp/x.go", CWD)).toBeNull();
  });

  it("rejects absolute paths when there is no cwd", () => {
    expect(resolvePreviewRelPath("/Users/me/proj/README.md", "")).toBeNull();
  });

  it("rejects relative paths too when there is no cwd", () => {
    // 「变动」模式的行可能来自消息派生,即使会话没有 cwd 也会渲染;没有 cwd 后端
    // 必回 WorkspaceFsNoCwd,预览按钮随行消失(spec「无 cwd ... 预览按钮随行消失」)。
    expect(resolvePreviewRelPath("README.md", "")).toBeNull();
    expect(resolvePreviewRelPath("internal/a.go", "")).toBeNull();
  });

  it("rejects non-previewable files regardless of path shape", () => {
    expect(resolvePreviewRelPath("archive.zip", CWD)).toBeNull();
    expect(resolvePreviewRelPath("/Users/me/proj/archive.zip", CWD)).toBeNull();
  });

  it("handles Windows absolute paths inside the cwd", () => {
    expect(resolvePreviewRelPath("C:\\proj\\README.md", "C:\\proj")).toBe(
      "README.md",
    );
    expect(resolvePreviewRelPath("C:\\proj\\a.go", "C:\\proj")).toBe("a.go");
    expect(resolvePreviewRelPath("C:\\other\\a.go", "C:\\proj")).toBeNull();
  });
});
