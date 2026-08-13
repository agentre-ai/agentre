import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { classifyFileType, FileTypeIcon } from "./file-type-icon";

describe("classifyFileType", () => {
  it.each([
    ["Dockerfile", "dockerfile"],
    ["Makefile", "makefile"],
    ["CMakeLists.txt", "cmake"],
    ["package.json", "npm"],
    ["package-lock.json", "npm"],
    ["pnpm-lock.yaml", "npm"],
    ["yarn.lock", "npm"],
    ["go.mod", "go"],
    ["go.sum", "go"],
    ["Cargo.toml", "rust"],
    ["Cargo.lock", "rust"],
    ["requirements.txt", "python"],
    ["pyproject.toml", "python"],
    ["Gemfile", "ruby"],
    [".gitignore", "git"],
    [".gitattributes", "git"],
    [".editorconfig", "config"],
    [".npmrc", "npm"],
    [".prettierrc", "config"],
    [".eslintrc", "config"],
  ])("recognizes exact filename %s as %s", (path, id) => {
    expect(classifyFileType(path).id).toBe(id);
  });

  it.each([
    [".env", "config"],
    [".env.local", "config"],
    [".env.production", "config"],
    ["backend.dockerfile", "dockerfile"],
    ["Dockerfile.dev", "dockerfile"],
    ["app.tar.gz", "archive"],
    ["types.d.ts", "typescript"],
  ])("resolves compound form %s as %s", (path, id) => {
    expect(classifyFileType(path).id).toBe(id);
  });

  it.each([
    ["main.go", "go"],
    ["app.py", "python"],
    ["App.tsx", "react-ts"],
    ["component.jsx", "react-js"],
    ["index.ts", "typescript"],
    ["index.js", "javascript"],
    ["lib.rs", "rust"],
    ["Main.java", "java"],
    ["App.kt", "kotlin"],
    ["main.cpp", "cpp"],
    ["header.h", "cpp"],
    ["App.cs", "csharp"],
    ["run.sh", "shell"],
    ["index.html", "html"],
    ["style.css", "css"],
    ["style.scss", "sass"],
    ["index.php", "php"],
    ["App.swift", "swift"],
    ["query.sql", "sql"],
    ["README.md", "markdown"],
    ["data.json", "json"],
    ["config.yaml", "yaml"],
    ["config.toml", "toml"],
    ["doc.xml", "xml"],
    ["notes.txt", "text"],
  ])("maps %s to language identity %s", (path, id) => {
    expect(classifyFileType(path).id).toBe(id);
  });

  it.each([
    ["report.pdf", "pdf"],
    ["doc.doc", "word"],
    ["doc.docx", "word"],
    ["sheet.xls", "excel"],
    ["sheet.xlsx", "excel"],
    ["deck.ppt", "powerpoint"],
    ["deck.pptx", "powerpoint"],
    ["data.csv", "csv"],
    ["photo.png", "image"],
    ["photo.jpg", "image"],
    ["logo.svg", "svg"],
    ["song.mp3", "audio"],
    ["movie.mp4", "video"],
    ["font.ttf", "font"],
    ["bundle.zip", "archive"],
    ["bundle.7z", "archive"],
    ["app.exe", "binary"],
    ["data.sqlite", "database"],
    ["server.key", "key"],
    ["site.crt", "cert"],
    ["bun.lock", "lock"],
  ])("maps %s to format identity %s", (path, id) => {
    expect(classifyFileType(path).id).toBe(id);
  });

  it.each([
    ["C:\\src\\App.TSX", "react-ts"],
    ["/a/b/MAKEFILE", "makefile"],
    ["src/main.GO", "go"],
    ["dir\\sub\\data.JSON", "json"],
  ])("extracts basename and matches case-insensitively for %s", (path, id) => {
    expect(classifyFileType(path).id).toBe(id);
  });

  it.each([
    "",
    "/",
    "\\",
    "noextension",
    "dir/.hidden",
    "file.unknownext",
    ".bashrc",
    "dir/",
  ])("falls back to the neutral unknown identity for %s", (path) => {
    expect(classifyFileType(path)).toEqual({ id: "unknown", tone: "neutral" });
  });

  it("assigns a stable semantic tone per identity", () => {
    expect(classifyFileType("main.go").tone).toBe("cyan");
    expect(classifyFileType("app.py").tone).toBe("yellow");
    expect(classifyFileType("index.ts").tone).toBe("blue");
    expect(classifyFileType("lib.rs").tone).toBe("orange");
    expect(classifyFileType("report.pdf").tone).toBe("red");
    expect(classifyFileType("sheet.xlsx").tone).toBe("green");
    expect(classifyFileType("App.kt").tone).toBe("purple");
    expect(classifyFileType("notes.txt").tone).toBe("neutral");
  });
});

describe("FileTypeIcon", () => {
  it("renders the identity glyph, tone token, fixed 17px square and is decorative", () => {
    render(<FileTypeIcon path="cmd/agentred/main.go" testId="file-icon" />);
    const el = screen.getByTestId("file-icon");

    expect(el).toHaveAttribute("data-file-type", "go");
    expect(el).toHaveClass("bg-file-cyan");
    expect(el).toHaveClass("size-[17px]");
    expect(el).toHaveAttribute("aria-hidden", "true");
  });

  it("falls back to the neutral unknown identity for unrecognized paths", () => {
    render(<FileTypeIcon path="dir/.hidden" testId="file-icon" />);
    const el = screen.getByTestId("file-icon");

    expect(el).toHaveAttribute("data-file-type", "unknown");
    expect(el).toHaveClass("bg-file-neutral");
  });
});
