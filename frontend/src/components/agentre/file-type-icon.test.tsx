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
    ["foo.rb", "ruby"],
    ["foo.lua", "lua"],
    ["foo.r", "r"],
    ["foo.pl", "perl"],
    ["foo.psd1", "powershell"],
    ["foo.psm1", "powershell"],
    ["foo.dart", "dart"],
    ["foo.scala", "scala"],
    ["foo.sc", "scala"],
    ["foo.clj", "clojure"],
    ["foo.cljs", "clojure"],
    ["foo.cljc", "clojure"],
    ["foo.groovy", "groovy"],
    ["foo.gradle", "groovy"],
  ])("falls back to the Monaco language identity for %s → %s", (path, id) => {
    expect(classifyFileType(path).id).toBe(id);
  });

  it("applies the Monaco fallback case-insensitively on Windows paths", () => {
    expect(classifyFileType("C:\\src\\Foo.RB").id).toBe("ruby");
    expect(classifyFileType("dir\\Sub\\Main.Dart").id).toBe("dart");
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
  it("renders a colored identity glyph in a transparent 17px slot", () => {
    render(<FileTypeIcon path="cmd/agentred/main.go" testId="file-icon" />);
    const el = screen.getByTestId("file-icon");
    const glyph = el.querySelector("svg");

    expect(el).toHaveAttribute("data-file-type", "go");
    expect(el).toHaveClass("size-[17px]");
    expect(el).toHaveClass("text-file-cyan");
    expect(el).not.toHaveClass("bg-file-cyan");
    expect(el).not.toHaveClass("rounded-sm");
    expect(el).not.toHaveClass("text-white");
    expect(el).toHaveAttribute("aria-hidden", "true");
    expect(glyph).toHaveClass("size-[17px]");
  });

  it("falls back to a neutral glyph without restoring a badge background", () => {
    render(<FileTypeIcon path="dir/.hidden" testId="file-icon" />);
    const el = screen.getByTestId("file-icon");

    expect(el).toHaveAttribute("data-file-type", "unknown");
    expect(el).toHaveClass("text-file-neutral");
    expect(el).not.toHaveClass("bg-file-neutral");
  });
});
