import { describe, expect, it } from "vitest";

import { classifyLink } from "./link-classify";

const CWD = "/Users/me/proj";

describe("classifyLink", () => {
  describe("URL forms", () => {
    it("when https://… then kind=url, url=original", () => {
      expect(classifyLink("https://example.com/a/b", CWD)).toEqual({
        kind: "url",
        url: "https://example.com/a/b",
      });
    });

    it("when http://… then kind=url", () => {
      expect(classifyLink("http://example.com", CWD)).toMatchObject({
        kind: "url",
        url: "http://example.com",
      });
    });

    it("when www.… then kind=url with http:// prefix added", () => {
      expect(classifyLink("www.example.com", CWD)).toEqual({
        kind: "url",
        url: "http://www.example.com",
      });
    });

    it("when mailto: then kind=url", () => {
      expect(classifyLink("mailto:a@b.com", CWD)).toEqual({
        kind: "url",
        url: "mailto:a@b.com",
      });
    });

    it("when tel: then kind=url", () => {
      expect(classifyLink("tel:+1234", CWD)).toEqual({
        kind: "url",
        url: "tel:+1234",
      });
    });
  });

  describe("Local absolute paths", () => {
    it("when POSIX absolute path inside cwd then kind=local-internal with relPath", () => {
      expect(classifyLink("/Users/me/proj/src/foo.go", CWD)).toEqual({
        kind: "local-internal",
        fullPath: "/Users/me/proj/src/foo.go",
        pathKind: "file",
        relPath: "src/foo.go",
      });
    });

    it("when POSIX absolute path with :line then line is parsed", () => {
      expect(classifyLink("/Users/me/proj/src/foo.go:42", CWD)).toEqual({
        kind: "local-internal",
        fullPath: "/Users/me/proj/src/foo.go",
        pathKind: "file",
        relPath: "src/foo.go",
        line: 42,
      });
    });

    it("when POSIX absolute path with :line:col then both parsed", () => {
      expect(classifyLink("/Users/me/proj/src/foo.go:42:7", CWD)).toEqual({
        kind: "local-internal",
        fullPath: "/Users/me/proj/src/foo.go",
        pathKind: "file",
        relPath: "src/foo.go",
        line: 42,
        col: 7,
      });
    });

    it("when POSIX absolute path outside cwd then kind=local-external", () => {
      expect(classifyLink("/usr/local/bin/agentred", CWD)).toEqual({
        kind: "local-external",
        fullPath: "/usr/local/bin/agentred",
        pathKind: "file",
      });
    });

    it("when POSIX absolute path ends with slash then pathKind=folder", () => {
      expect(classifyLink("/Users/me/proj/docs/", CWD)).toEqual({
        kind: "local-internal",
        fullPath: "/Users/me/proj/docs/",
        pathKind: "folder",
        relPath: "docs/",
      });
    });

    it("when cwd is empty/undefined then absolute path is local-external", () => {
      expect(classifyLink("/Users/me/proj/foo.go", undefined)).toEqual({
        kind: "local-external",
        fullPath: "/Users/me/proj/foo.go",
        pathKind: "file",
      });
    });

    it("when Windows absolute path then kind=local-external (no cwd match)", () => {
      const got = classifyLink("C:\\Users\\x\\foo.go:10", CWD);
      expect(got).toEqual({
        kind: "local-external",
        fullPath: "C:\\Users\\x\\foo.go",
        pathKind: "file",
        line: 10,
      });
    });

    it("when href is exactly cwd then relPath is empty", () => {
      expect(classifyLink(CWD, CWD)).toEqual({
        kind: "local-internal",
        fullPath: CWD,
        pathKind: "folder",
        relPath: "",
      });
    });
  });

  describe("file:// protocol", () => {
    it("when file:///path then treated as POSIX absolute", () => {
      expect(classifyLink("file:///Users/me/proj/foo.go", CWD)).toEqual({
        kind: "local-internal",
        fullPath: "/Users/me/proj/foo.go",
        pathKind: "file",
        relPath: "foo.go",
      });
    });

    it("when file:// with URL-encoded chars then decoded", () => {
      expect(classifyLink("file:///Users/me/proj/a%20b.go", CWD)).toMatchObject(
        {
          kind: "local-internal",
          fullPath: "/Users/me/proj/a b.go",
          pathKind: "file",
        },
      );
    });
  });

  describe("Unknown forms", () => {
    it("when relative path then kind=unknown", () => {
      expect(classifyLink("internal/foo.go", CWD)).toEqual({
        kind: "unknown",
        href: "internal/foo.go",
      });
    });

    it("when href is empty then kind=unknown", () => {
      expect(classifyLink(undefined, CWD)).toEqual({
        kind: "unknown",
        href: "",
      });
    });

    it("when javascript: scheme then kind=unknown", () => {
      // 即使是 well-formed URL prefix，但安全考虑只白名单 http/https/mailto/tel/www
      expect(classifyLink("javascript:alert(1)", CWD)).toEqual({
        kind: "unknown",
        href: "javascript:alert(1)",
      });
    });
  });
});
