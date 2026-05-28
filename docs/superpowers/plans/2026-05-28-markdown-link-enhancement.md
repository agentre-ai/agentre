# AI 消息链接增强 — 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让 AI 消息里的 markdown link 按 URL / cwd 内文件 / cwd 外文件 三种语义分别可点击 + hover 浮窗，URL 走系统浏览器，本地路径走 OS 默认应用。

**Architecture:** 新增 `classifyLink(href, cwd)` 纯函数判别 link 类型 → `RichLink` 组件接管 markdown 的 `<a>` 渲染，挂 HoverCard 浮窗 + 类型 icon + 点击 dispatch → 后端新增 Wails binding `OpenPath(path)` 用 `exec.Command` 调系统默认应用。`MarkdownText` 接收新的 `cwd` prop 透传到 `RichLink`。

**Tech Stack:** TypeScript / React 19 / react-markdown / radix-ui HoverCard / Tailwind / Wails v2 Go binding / goconvey + vitest

**Spec:** `docs/superpowers/specs/2026-05-28-markdown-link-enhancement-design.md`

---

## File Map

| 文件 | 操作 | 职责 |
|---|---|---|
| `frontend/src/lib/link-classify.ts` | Create | `classifyLink(href, cwd)` 纯函数，4 种 kind |
| `frontend/src/lib/link-classify.test.ts` | Create | BDD 单测，覆盖所有 href 形态 |
| `frontend/src/components/ui/hover-card.tsx` | Create | shadcn 风格 wrap `radix-ui` HoverCard |
| `frontend/src/components/agentre/rich-link.tsx` | Create | 接管 `<a>`，挂 popover + icon + 点击 dispatch |
| `frontend/src/components/agentre/rich-link.test.tsx` | Create | BDD 测 RichLink 行为 |
| `frontend/src/components/agentre/markdown-text.tsx` | Modify | `a` override 改 RichLink、新增 `cwd` prop、自实现 `urlTransform` 白名单 |
| `frontend/src/components/agentre/chat.tsx:1269` | Modify | 在 `<MarkdownText>` 上传 `cwd`；`renderMessageBlocks` 把 `cwd` 透传到那里 |
| `internal/app/system.go` | Create | `OpenPath(path) error` Wails binding |
| `internal/app/system_test.go` | Create | goconvey 测 `validateOpenPath` + 平台 dispatch |
| `frontend/wailsjs/go/app/App.*` | Generate | `make generate` 后自动产生 |

---

## Task 1: 安装 HoverCard 基础组件

**Files:**
- Create: `frontend/src/components/ui/hover-card.tsx`

`radix-ui` umbrella 包已经在 dependencies 里（`frontend/package.json`），按 `tooltip.tsx` 同样的 wrap 模式写。

- [ ] **Step 1: 写 hover-card.tsx**

```tsx
// frontend/src/components/ui/hover-card.tsx
import * as React from "react";
import { HoverCard as HoverCardPrimitive } from "radix-ui";

import { cn } from "@/lib/utils";

const HoverCard = HoverCardPrimitive.Root;
const HoverCardTrigger = HoverCardPrimitive.Trigger;

function HoverCardContent({
  className,
  align = "center",
  sideOffset = 8,
  ...props
}: React.ComponentProps<typeof HoverCardPrimitive.Content>) {
  return (
    <HoverCardPrimitive.Portal>
      <HoverCardPrimitive.Content
        align={align}
        sideOffset={sideOffset}
        className={cn(
          "z-50 w-80 rounded-lg border border-border bg-popover p-3 text-popover-foreground shadow-md outline-none",
          "data-[state=open]:animate-in data-[state=open]:fade-in-0 data-[state=open]:zoom-in-95",
          "data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=closed]:zoom-out-95",
          className,
        )}
        {...props}
      />
    </HoverCardPrimitive.Portal>
  );
}

export { HoverCard, HoverCardTrigger, HoverCardContent };
```

- [ ] **Step 2: TypeScript 编译通过**

```bash
cd frontend && pnpm exec tsc -b --noEmit
```

Expected: 无错误。

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/ui/hover-card.tsx
git commit -m "feat(ui): add shadcn-style HoverCard wrapping radix-ui primitive"
```

---

## Task 2: `classifyLink` 纯函数 — TDD

**Files:**
- Test: `frontend/src/lib/link-classify.test.ts`
- Create: `frontend/src/lib/link-classify.ts`

判别规则按 spec：URL prefix → file:// → 绝对路径 → unknown。返回 4 种 discriminated union。

- [ ] **Step 1: 写失败测试**

```ts
// frontend/src/lib/link-classify.test.ts
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
        relPath: "src/foo.go",
      });
    });

    it("when POSIX absolute path with :line then line is parsed", () => {
      expect(classifyLink("/Users/me/proj/src/foo.go:42", CWD)).toEqual({
        kind: "local-internal",
        fullPath: "/Users/me/proj/src/foo.go",
        relPath: "src/foo.go",
        line: 42,
      });
    });

    it("when POSIX absolute path with :line:col then both parsed", () => {
      expect(classifyLink("/Users/me/proj/src/foo.go:42:7", CWD)).toEqual({
        kind: "local-internal",
        fullPath: "/Users/me/proj/src/foo.go",
        relPath: "src/foo.go",
        line: 42,
        col: 7,
      });
    });

    it("when POSIX absolute path outside cwd then kind=local-external", () => {
      expect(classifyLink("/usr/local/bin/agentred", CWD)).toEqual({
        kind: "local-external",
        fullPath: "/usr/local/bin/agentred",
      });
    });

    it("when cwd is empty/undefined then absolute path is local-external", () => {
      expect(classifyLink("/Users/me/proj/foo.go", undefined)).toEqual({
        kind: "local-external",
        fullPath: "/Users/me/proj/foo.go",
      });
    });

    it("when Windows absolute path then kind=local-external (no cwd match)", () => {
      const got = classifyLink("C:\\Users\\x\\foo.go:10", CWD);
      expect(got).toEqual({
        kind: "local-external",
        fullPath: "C:\\Users\\x\\foo.go",
        line: 10,
      });
    });

    it("when href is exactly cwd then relPath is empty", () => {
      expect(classifyLink(CWD, CWD)).toEqual({
        kind: "local-internal",
        fullPath: CWD,
        relPath: "",
      });
    });
  });

  describe("file:// protocol", () => {
    it("when file:///path then treated as POSIX absolute", () => {
      expect(classifyLink("file:///Users/me/proj/foo.go", CWD)).toEqual({
        kind: "local-internal",
        fullPath: "/Users/me/proj/foo.go",
        relPath: "foo.go",
      });
    });

    it("when file:// with URL-encoded chars then decoded", () => {
      expect(
        classifyLink("file:///Users/me/proj/a%20b.go", CWD),
      ).toMatchObject({
        kind: "local-internal",
        fullPath: "/Users/me/proj/a b.go",
      });
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
```

- [ ] **Step 2: 跑测试看失败**

```bash
cd frontend && pnpm exec vitest run src/lib/link-classify.test.ts
```

Expected: 文件不存在 / 函数 undefined。

- [ ] **Step 3: 写实现**

```ts
// frontend/src/lib/link-classify.ts

export type LinkClass =
  | { kind: "url"; url: string }
  | {
      kind: "local-internal";
      fullPath: string;
      relPath: string;
      line?: number;
      col?: number;
    }
  | {
      kind: "local-external";
      fullPath: string;
      line?: number;
      col?: number;
    }
  | { kind: "unknown"; href: string };

const URL_PREFIX = /^(https?:|mailto:|tel:)/i;
const WWW_PREFIX = /^www\./i;
const FILE_PROTOCOL = /^file:\/\//i;
const ABS_POSIX = /^\//;
const ABS_WINDOWS = /^[A-Za-z]:[\\/]/;
const LINE_SUFFIX = /:(\d+)(?::(\d+))?$/;

function stripLineSuffix(p: string): {
  path: string;
  line?: number;
  col?: number;
} {
  const m = LINE_SUFFIX.exec(p);
  if (!m) return { path: p };
  return {
    path: p.slice(0, m.index),
    line: parseInt(m[1], 10),
    col: m[2] !== undefined ? parseInt(m[2], 10) : undefined,
  };
}

function fileURLToPath(href: string): string {
  // file:///Users/x/foo.go → /Users/x/foo.go
  // file:///C:/Users/x/foo.go → C:/Users/x/foo.go
  let p = href.slice("file://".length);
  if (p.startsWith("/") && /^[A-Za-z]:/.test(p.slice(1))) {
    p = p.slice(1);
  }
  try {
    return decodeURI(p);
  } catch {
    return p;
  }
}

export function classifyLink(
  href: string | undefined,
  cwd?: string,
): LinkClass {
  if (!href) return { kind: "unknown", href: "" };

  if (URL_PREFIX.test(href)) return { kind: "url", url: href };
  if (WWW_PREFIX.test(href)) return { kind: "url", url: `http://${href}` };

  let rawPath: string;
  if (FILE_PROTOCOL.test(href)) {
    rawPath = fileURLToPath(href);
  } else if (ABS_POSIX.test(href) || ABS_WINDOWS.test(href)) {
    rawPath = href;
  } else {
    return { kind: "unknown", href };
  }

  const { path: fullPath, line, col } = stripLineSuffix(rawPath);

  if (cwd && (fullPath === cwd || fullPath.startsWith(cwd + "/"))) {
    const relPath = fullPath === cwd ? "" : fullPath.slice(cwd.length + 1);
    return {
      kind: "local-internal",
      fullPath,
      relPath,
      ...(line !== undefined ? { line } : {}),
      ...(col !== undefined ? { col } : {}),
    };
  }

  return {
    kind: "local-external",
    fullPath,
    ...(line !== undefined ? { line } : {}),
    ...(col !== undefined ? { col } : {}),
  };
}
```

- [ ] **Step 4: 跑测试看全 pass**

```bash
cd frontend && pnpm exec vitest run src/lib/link-classify.test.ts
```

Expected: 所有 `it()` PASS。

- [ ] **Step 5: Commit**

```bash
git add frontend/src/lib/link-classify.ts frontend/src/lib/link-classify.test.ts
git commit -m "feat(frontend): add classifyLink pure function for URL/path discrimination"
```

---

## Task 3: `OpenPath` Wails Go binding — TDD

**Files:**
- Test: `internal/app/system_test.go`
- Create: `internal/app/system.go`

参考 `internal/app/update.go:91` `RestartApp` 模式，方法直接挂 App，不进 `_svc`。用 package var `runOpenCmd` 做 exec 注入点便于测试。

- [ ] **Step 1: 写失败测试**

```go
// internal/app/system_test.go
package app

import (
	"errors"
	"runtime"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestValidateOpenPath(t *testing.T) {
	Convey("Given various path inputs", t, func() {
		Convey("when path is empty, then error", func() {
			_, err := validateOpenPath("")
			So(err, ShouldNotBeNil)
		})
		Convey("when path is relative, then error", func() {
			_, err := validateOpenPath("foo/bar.go")
			So(err, ShouldNotBeNil)
		})
		Convey("when path contains '..', then error", func() {
			_, err := validateOpenPath("/foo/../bar.go")
			So(err, ShouldNotBeNil)
		})
		Convey("when POSIX absolute path with :line:col, then return without suffix", func() {
			got, err := validateOpenPath("/Users/x/foo.go:42:7")
			So(err, ShouldBeNil)
			So(got, ShouldEqual, "/Users/x/foo.go")
		})
		Convey("when POSIX absolute path without suffix, then return as-is", func() {
			got, err := validateOpenPath("/Users/x/foo.go")
			So(err, ShouldBeNil)
			So(got, ShouldEqual, "/Users/x/foo.go")
		})
		Convey("when Windows absolute path with line suffix, then strip suffix", func() {
			got, err := validateOpenPath(`C:\Users\x\foo.go:10`)
			So(err, ShouldBeNil)
			So(got, ShouldEqual, `C:\Users\x\foo.go`)
		})
	})
}

func TestOpenPath_dispatchesPlatformCommand(t *testing.T) {
	Convey("Given a stubbed exec runner", t, func() {
		var gotName string
		var gotArgs []string
		origRun := runOpenCmd
		runOpenCmd = func(name string, args ...string) error {
			gotName = name
			gotArgs = args
			return nil
		}
		defer func() { runOpenCmd = origRun }()

		Convey("when OpenPath is called with a valid absolute path", func() {
			a := &App{}
			err := a.OpenPath("/tmp/file.go:42")
			So(err, ShouldBeNil)

			switch runtime.GOOS {
			case "darwin":
				So(gotName, ShouldEqual, "open")
				So(gotArgs, ShouldResemble, []string{"/tmp/file.go"})
			case "windows":
				So(gotName, ShouldEqual, "cmd")
				So(gotArgs, ShouldResemble, []string{"/c", "start", "", "/tmp/file.go"})
			default:
				So(gotName, ShouldEqual, "xdg-open")
				So(gotArgs, ShouldResemble, []string{"/tmp/file.go"})
			}
		})

		Convey("when exec returns error, then OpenPath propagates", func() {
			runOpenCmd = func(name string, args ...string) error {
				return errors.New("boom")
			}
			a := &App{}
			err := a.OpenPath("/tmp/file.go")
			So(err, ShouldNotBeNil)
		})

		Convey("when path is invalid, then exec is not called", func() {
			called := false
			runOpenCmd = func(name string, args ...string) error {
				called = true
				return nil
			}
			a := &App{}
			err := a.OpenPath("relative/path.go")
			So(err, ShouldNotBeNil)
			So(called, ShouldBeFalse)
		})
	})
}
```

- [ ] **Step 2: 跑测试看失败**

```bash
go test -race -run "TestValidateOpenPath|TestOpenPath_" ./internal/app/...
```

Expected: 编译失败 — `validateOpenPath`、`runOpenCmd`、`(*App).OpenPath` 未定义。

- [ ] **Step 3: 写实现**

```go
// internal/app/system.go
package app

import (
	"fmt"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
)

// runOpenCmd is the test seam for exec.Command. Tests swap it; production code
// uses the real exec.
var runOpenCmd = func(name string, args ...string) error {
	return exec.Command(name, args...).Run() //nolint:gosec
}

var lineSuffixRe = regexp.MustCompile(`:\d+(?::\d+)?$`)

// OpenPath 用系统默认应用打开 path。
// path 必须是绝对路径；包含 ".." 时拒绝（防御性，AI 输出基本不会有）。
// 末尾 :line[:col] 后缀会被剥离 —— macOS open / xdg-open 不识别这种语法。
// 行号未来若要支持，由"编辑器 URL scheme"设置项接管（见 spec 未来工作）。
func (a *App) OpenPath(path string) error {
	cleaned, err := validateOpenPath(path)
	if err != nil {
		return err
	}
	return runOpenPlatform(cleaned)
}

func validateOpenPath(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("OpenPath: path is empty")
	}
	if !isAbsolutePath(path) {
		return "", fmt.Errorf("OpenPath: path must be absolute: %s", path)
	}
	if strings.Contains(path, "..") {
		return "", fmt.Errorf("OpenPath: path contains '..': %s", path)
	}
	return lineSuffixRe.ReplaceAllString(path, ""), nil
}

func isAbsolutePath(p string) bool {
	if strings.HasPrefix(p, "/") {
		return true
	}
	// Windows: C:\ 或 C:/
	if len(p) >= 3 && p[1] == ':' && (p[2] == '\\' || p[2] == '/') {
		c := p[0]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') {
			return true
		}
	}
	return false
}

func runOpenPlatform(path string) error {
	switch runtime.GOOS {
	case "darwin":
		return runOpenCmd("open", path)
	case "windows":
		return runOpenCmd("cmd", "/c", "start", "", path)
	default:
		return runOpenCmd("xdg-open", path)
	}
}
```

- [ ] **Step 4: 跑测试看全 pass**

```bash
go test -race -run "TestValidateOpenPath|TestOpenPath_" ./internal/app/...
```

Expected: 全 PASS。

- [ ] **Step 5: 跑 wails generate 更新前端 binding**

```bash
make generate
```

Expected: `frontend/wailsjs/go/app/App.d.ts` 多出 `OpenPath(arg1: string): Promise<void>;`。

- [ ] **Step 6: Commit**

```bash
git add internal/app/system.go internal/app/system_test.go frontend/wailsjs/
git commit -m "feat(app): add OpenPath Wails binding for system default app

Used by frontend RichLink to open local file paths from AI messages.
Strips :line[:col] suffix (OS open commands don't accept it). Rejects
relative paths and '..' segments as a defensive check.
Tests cover validateOpenPath edge cases and per-platform exec dispatch
via a swappable runOpenCmd package var."
```

---

## Task 4: `RichLink` 组件 — TDD

**Files:**
- Test: `frontend/src/components/agentre/rich-link.test.tsx`
- Create: `frontend/src/components/agentre/rich-link.tsx`

RichLink 用 HoverCard 包 `<a>`，按 kind 决定 icon、popover 内容、点击行为。

- [ ] **Step 1: 写失败测试**

```tsx
// frontend/src/components/agentre/rich-link.test.tsx
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const sonnerMocks = vi.hoisted(() => ({
  toast: {
    error: vi.fn(),
    success: vi.fn(),
  },
}));
vi.mock("sonner", () => sonnerMocks);

const openPathMock = vi.fn();
vi.mock("@/../wailsjs/go/app/App", () => ({
  OpenPath: (p: string) => openPathMock(p),
}));

const browserOpenURLMock = vi.fn();
vi.mock("@/../wailsjs/runtime/runtime", () => ({
  BrowserOpenURL: (u: string) => browserOpenURLMock(u),
}));

import { RichLink } from "./rich-link";

const CWD = "/Users/me/proj";

beforeEach(() => {
  openPathMock.mockReset();
  browserOpenURLMock.mockReset();
  sonnerMocks.toast.success.mockReset();
  sonnerMocks.toast.error.mockReset();
});

afterEach(() => {
  vi.useRealTimers();
});

function mockClipboard() {
  const writeText = vi.fn().mockResolvedValue(undefined);
  Object.defineProperty(navigator, "clipboard", {
    configurable: true,
    value: { writeText },
  });
  return writeText;
}

describe("RichLink", () => {
  describe("URL link", () => {
    it("clicking calls BrowserOpenURL, not browser navigation", () => {
      render(
        <RichLink href="https://example.com" cwd={CWD}>
          example
        </RichLink>,
      );
      const link = screen.getByRole("link", { name: /example/ });
      fireEvent.click(link);
      expect(browserOpenURLMock).toHaveBeenCalledWith("https://example.com");
      expect(openPathMock).not.toHaveBeenCalled();
    });

    it("renders external-link icon next to text", () => {
      render(
        <RichLink href="https://example.com" cwd={CWD}>
          example
        </RichLink>,
      );
      expect(screen.getByTestId("rich-link-icon")).toHaveAttribute(
        "data-kind",
        "url",
      );
    });
  });

  describe("Local file link — in cwd", () => {
    it("clicking calls OpenPath with full path + line suffix", () => {
      render(
        <RichLink href="/Users/me/proj/src/foo.go:42" cwd={CWD}>
          foo.go:42
        </RichLink>,
      );
      const link = screen.getByRole("link", { name: /foo\.go:42/ });
      fireEvent.click(link);
      expect(openPathMock).toHaveBeenCalledWith(
        "/Users/me/proj/src/foo.go:42",
      );
      expect(browserOpenURLMock).not.toHaveBeenCalled();
    });

    it("renders file-text icon", () => {
      render(
        <RichLink href="/Users/me/proj/src/foo.go" cwd={CWD}>
          foo.go
        </RichLink>,
      );
      expect(screen.getByTestId("rich-link-icon")).toHaveAttribute(
        "data-kind",
        "local-internal",
      );
    });
  });

  describe("Local file link — outside cwd", () => {
    it("renders folder icon", () => {
      render(
        <RichLink href="/usr/local/bin/agentred" cwd={CWD}>
          agentred
        </RichLink>,
      );
      expect(screen.getByTestId("rich-link-icon")).toHaveAttribute(
        "data-kind",
        "local-external",
      );
    });

    it("clicking calls OpenPath", () => {
      render(
        <RichLink href="/usr/local/bin/agentred" cwd={CWD}>
          agentred
        </RichLink>,
      );
      fireEvent.click(screen.getByRole("link", { name: /agentred/ }));
      expect(openPathMock).toHaveBeenCalledWith("/usr/local/bin/agentred");
    });
  });

  describe("Unknown / fallback", () => {
    it("renders plain anchor without icon for relative paths", () => {
      render(
        <RichLink href="relative/foo.go" cwd={CWD}>
          rel
        </RichLink>,
      );
      const link = screen.getByRole("link", { name: /rel/ });
      expect(link).toBeInTheDocument();
      expect(screen.queryByTestId("rich-link-icon")).not.toBeInTheDocument();
    });

    it("relative path click goes through default navigation (no mock called)", () => {
      render(
        <RichLink href="relative/foo.go" cwd={CWD}>
          rel
        </RichLink>,
      );
      fireEvent.click(screen.getByRole("link", { name: /rel/ }));
      expect(browserOpenURLMock).not.toHaveBeenCalled();
      expect(openPathMock).not.toHaveBeenCalled();
    });
  });

  describe("Copy button in popover", () => {
    it("URL popover copy writes full URL + shows success toast", async () => {
      const writeText = mockClipboard();
      render(
        <RichLink href="https://example.com/long/path" cwd={CWD}>
          ex
        </RichLink>,
      );
      // Force open popover by simulating hover events; radix HoverCard exposes
      // the content when trigger gets pointerenter, but in JSDOM we can also
      // open it via keyboard focus.
      const link = screen.getByRole("link", { name: /ex/ });
      fireEvent.focus(link);
      const copyBtn = await screen.findByRole("button", { name: /复制/ });
      fireEvent.click(copyBtn);
      await waitFor(() => {
        expect(writeText).toHaveBeenCalledWith(
          "https://example.com/long/path",
        );
      });
      expect(sonnerMocks.toast.success).toHaveBeenCalled();
    });

    it("local-internal popover copy writes full path with line suffix", async () => {
      const writeText = mockClipboard();
      render(
        <RichLink href="/Users/me/proj/src/foo.go:42" cwd={CWD}>
          foo.go:42
        </RichLink>,
      );
      fireEvent.focus(screen.getByRole("link", { name: /foo\.go:42/ }));
      const copyBtn = await screen.findByRole("button", { name: /复制/ });
      fireEvent.click(copyBtn);
      await waitFor(() => {
        expect(writeText).toHaveBeenCalledWith(
          "/Users/me/proj/src/foo.go:42",
        );
      });
    });
  });

  describe("Popover content sanity", () => {
    it("local-internal popover shows both project root and relative path", async () => {
      render(
        <RichLink href="/Users/me/proj/src/foo.go" cwd={CWD}>
          foo
        </RichLink>,
      );
      fireEvent.focus(screen.getByRole("link", { name: /foo/ }));
      expect(await screen.findByText("/Users/me/proj")).toBeInTheDocument();
      expect(screen.getByText("src/foo.go")).toBeInTheDocument();
    });

    it("local-external popover shows full path but no project root segment", async () => {
      render(
        <RichLink href="/usr/local/bin/agentred" cwd={CWD}>
          ag
        </RichLink>,
      );
      fireEvent.focus(screen.getByRole("link", { name: /ag/ }));
      expect(
        await screen.findByText("/usr/local/bin/agentred"),
      ).toBeInTheDocument();
      // CWD value should NOT appear in external popover.
      expect(screen.queryByText(CWD)).not.toBeInTheDocument();
    });
  });
});
```

- [ ] **Step 2: 跑测试看失败**

```bash
cd frontend && pnpm exec vitest run src/components/agentre/rich-link.test.tsx
```

Expected: 模块不存在。

- [ ] **Step 3: 写实现**

```tsx
// frontend/src/components/agentre/rich-link.tsx
import {
  Copy as CopyIcon,
  ExternalLink,
  FileText,
  Folder,
  Link as LinkIcon,
  MousePointerClick,
} from "lucide-react";
import * as React from "react";
import { toast } from "sonner";

import {
  HoverCard,
  HoverCardContent,
  HoverCardTrigger,
} from "@/components/ui/hover-card";
import { cn } from "@/lib/utils";
import { classifyLink, type LinkClass } from "@/lib/link-classify";

import { BrowserOpenURL } from "../../../wailsjs/runtime/runtime";
import { OpenPath } from "../../../wailsjs/go/app/App";

const HOVER_OPEN_DELAY_MS = 200;
const HOVER_CLOSE_DELAY_MS = 200;

type RichLinkProps = {
  href?: string;
  className?: string;
  cwd?: string;
  children: React.ReactNode;
};

function lineColSuffix(c: { line?: number; col?: number }): string {
  if (c.line === undefined) return "";
  if (c.col === undefined) return `:${c.line}`;
  return `:${c.line}:${c.col}`;
}

function fullTarget(kind: LinkClass): string {
  switch (kind.kind) {
    case "url":
      return kind.url;
    case "local-internal":
    case "local-external":
      return kind.fullPath + lineColSuffix(kind);
    case "unknown":
      return kind.href;
  }
}

function KindIcon({ kind }: { kind: LinkClass["kind"] }) {
  const props = {
    "data-testid": "rich-link-icon",
    "data-kind": kind,
    className: "inline-block size-3 align-text-bottom",
    "aria-hidden": true,
  } as const;
  switch (kind) {
    case "url":
      return <ExternalLink {...props} />;
    case "local-internal":
      return <FileText {...props} />;
    case "local-external":
      return <Folder {...props} />;
    case "unknown":
      return null;
  }
}

async function copyToClipboard(text: string) {
  try {
    await navigator.clipboard.writeText(text);
    toast.success("已复制", { duration: 2000, position: "bottom-right" });
  } catch (e) {
    toast.error("复制失败");
  }
}

function dispatchClick(kind: LinkClass) {
  switch (kind.kind) {
    case "url":
      BrowserOpenURL(kind.url);
      return;
    case "local-internal":
    case "local-external":
      OpenPath(fullTarget(kind)).catch((err: unknown) => {
        toast.error(
          `打开失败: ${err instanceof Error ? err.message : String(err)}`,
        );
      });
      return;
    case "unknown":
      // 不拦截，让浏览器走默认行为（target=_blank fallback）。
      return;
  }
}

function URLPopover({ kind }: { kind: Extract<LinkClass, { kind: "url" }> }) {
  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center gap-2">
        <span className="inline-flex items-center gap-1 rounded-full bg-primary px-2 py-0.5 text-[10px] font-semibold text-primary-foreground">
          <LinkIcon className="size-3" aria-hidden /> URL
        </span>
        <div className="flex-1" />
        <button
          type="button"
          className="inline-flex items-center gap-1 rounded-md border border-border bg-secondary px-2 py-1 text-xs"
          onClick={() => copyToClipboard(kind.url)}
        >
          <CopyIcon className="size-3" aria-hidden /> 复制
        </button>
      </div>
      <code className="break-all font-mono text-xs text-foreground">
        {kind.url}
      </code>
      <div className="flex items-center gap-1.5 rounded-md bg-secondary px-2 py-1 text-[11px] text-muted-foreground">
        <MousePointerClick className="size-3" aria-hidden />
        点击在系统浏览器中打开
      </div>
    </div>
  );
}

function LineChip({ line, col }: { line?: number; col?: number }) {
  if (line === undefined) return null;
  return (
    <span className="inline-flex items-center rounded-full border border-border bg-secondary px-2 py-0.5 font-mono text-[10px]">
      L{line}
      {col !== undefined ? `:${col}` : ""}
    </span>
  );
}

function LocalInternalPopover({
  kind,
  cwd,
}: {
  kind: Extract<LinkClass, { kind: "local-internal" }>;
  cwd: string;
}) {
  const full = fullTarget(kind);
  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center gap-2">
        <span className="inline-flex items-center gap-1 rounded-full bg-primary px-2 py-0.5 text-[10px] font-semibold text-primary-foreground">
          <FileText className="size-3" aria-hidden /> 本地文件
        </span>
        <LineChip line={kind.line} col={kind.col} />
        <div className="flex-1" />
        <button
          type="button"
          className="inline-flex items-center gap-1 rounded-md border border-border bg-secondary px-2 py-1 text-xs"
          onClick={() => copyToClipboard(full)}
        >
          <CopyIcon className="size-3" aria-hidden /> 复制
        </button>
      </div>
      <div className="flex flex-col gap-0.5 rounded-md bg-secondary px-2.5 py-1.5">
        <div className="flex items-baseline gap-2">
          <span className="w-12 shrink-0 text-[10px] font-semibold text-muted-foreground">
            项目根
          </span>
          <code className="font-mono text-xs text-muted-foreground">{cwd}</code>
        </div>
        <div className="flex items-baseline gap-2">
          <span className="w-12 shrink-0 text-[10px] font-semibold text-muted-foreground">
            相对
          </span>
          <code className="font-mono text-xs font-semibold text-foreground">
            {kind.relPath}
          </code>
        </div>
      </div>
      <code className="break-all font-mono text-[11px] text-muted-foreground">
        {full}
      </code>
      <div className="flex items-center gap-1.5 rounded-md bg-secondary px-2 py-1 text-[11px] text-muted-foreground">
        <MousePointerClick className="size-3" aria-hidden />
        点击用系统默认应用打开
      </div>
    </div>
  );
}

function LocalExternalPopover({
  kind,
}: {
  kind: Extract<LinkClass, { kind: "local-external" }>;
}) {
  const full = fullTarget(kind);
  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center gap-2">
        <span className="inline-flex items-center gap-1 rounded-full bg-muted-foreground px-2 py-0.5 text-[10px] font-semibold text-background">
          <Folder className="size-3" aria-hidden /> 本地文件 · 项目外
        </span>
        <LineChip line={kind.line} col={kind.col} />
        <div className="flex-1" />
        <button
          type="button"
          className="inline-flex items-center gap-1 rounded-md border border-border bg-secondary px-2 py-1 text-xs"
          onClick={() => copyToClipboard(full)}
        >
          <CopyIcon className="size-3" aria-hidden /> 复制
        </button>
      </div>
      <code className="break-all font-mono text-xs font-semibold text-foreground">
        {full}
      </code>
      <div className="text-[11px] text-muted-foreground">
        路径不在当前 cwd 之下，不展示项目根分段。
      </div>
      <div className="flex items-center gap-1.5 rounded-md bg-secondary px-2 py-1 text-[11px] text-muted-foreground">
        <MousePointerClick className="size-3" aria-hidden />
        点击用系统默认应用打开
      </div>
    </div>
  );
}

export function RichLink({ href, className, cwd, children }: RichLinkProps) {
  const kind = React.useMemo(() => classifyLink(href, cwd), [href, cwd]);

  if (kind.kind === "unknown") {
    // unknown 一律走原有 anchor 行为（target=_blank 兜底，不挂 popover）
    return (
      <a
        href={kind.href || undefined}
        className={cn(
          "text-primary underline underline-offset-2 hover:opacity-80",
          className,
        )}
        target="_blank"
        rel="noreferrer noopener"
      >
        {children}
      </a>
    );
  }

  const onClick = (e: React.MouseEvent) => {
    e.preventDefault();
    dispatchClick(kind);
  };

  return (
    <HoverCard openDelay={HOVER_OPEN_DELAY_MS} closeDelay={HOVER_CLOSE_DELAY_MS}>
      <HoverCardTrigger asChild>
        <a
          href={fullTarget(kind)}
          className={cn(
            "text-primary underline underline-offset-2 hover:opacity-80",
            className,
          )}
          onClick={onClick}
        >
          {children}
          <KindIcon kind={kind.kind} />
        </a>
      </HoverCardTrigger>
      <HoverCardContent className="w-[28rem]">
        {kind.kind === "url" ? (
          <URLPopover kind={kind} />
        ) : kind.kind === "local-internal" ? (
          <LocalInternalPopover kind={kind} cwd={cwd ?? ""} />
        ) : (
          <LocalExternalPopover kind={kind} />
        )}
      </HoverCardContent>
    </HoverCard>
  );
}
```

- [ ] **Step 4: 跑测试看全 pass**

```bash
cd frontend && pnpm exec vitest run src/components/agentre/rich-link.test.tsx
```

Expected: 所有测试 PASS。

**Notes for the engineer:**
- Radix HoverCard 在 JSDOM 里 `pointerenter` 不一定能触发 open。测试里用 `fireEvent.focus(trigger)` 因为 Radix HoverCardTrigger 默认 `data-state="open"` on focus（无障碍要求）。如果 JSDOM 没有这个行为，把测试 trigger 改成在 trigger 上 `fireEvent.pointerEnter` + advanceTimers，或者直接 query `HoverCardContent` 强制渲染 —— 别因为这个改动 RichLink 实现。

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/agentre/rich-link.tsx frontend/src/components/agentre/rich-link.test.tsx
git commit -m "feat(chat): add RichLink component with hover popover and click dispatch

URL links → BrowserOpenURL; local file links → OpenPath wails binding.
Hover (200ms delay) shows popover with type badge, full path / URL,
copy button, and click hint. Local-internal popover additionally shows
project root + relative path segments."
```

---

## Task 5: 在 `MarkdownText` 里启用 `RichLink` + 自实现 urlTransform 白名单

**Files:**
- Modify: `frontend/src/components/agentre/markdown-text.tsx`

要做三件事：
1. 给 `MarkdownText` 加 `cwd?: string` prop
2. `a` override 改为 `<RichLink href cwd>`
3. 给 `ReactMarkdown` 加 `urlTransform` —— 默认会把 `file://` 和奇怪绝对路径 strip 掉；自实现白名单放过 http/https/mailto/tel/file/ 绝对路径 + Windows 盘符。

- [ ] **Step 1: 写白名单 transform 测试（加到 `markdown-text.test.tsx`）**

```tsx
// frontend/src/components/agentre/markdown-text.test.tsx — append new describe
describe("MarkdownText URL whitelist", () => {
  it("preserves https href as-is", () => {
    const { container } = render(
      <MarkdownText text="[ex](https://example.com)" />,
    );
    expect(container.querySelector("a")?.getAttribute("href")).toBe(
      "https://example.com",
    );
  });

  it("preserves absolute POSIX path href as-is", () => {
    const { container } = render(
      <MarkdownText text="[f](/Users/me/foo.go:42)" cwd="/Users/me" />,
    );
    expect(container.querySelector("a")?.getAttribute("href")).toBe(
      "/Users/me/foo.go:42",
    );
  });

  it("preserves file:// href as-is", () => {
    const { container } = render(
      <MarkdownText text="[f](file:///Users/me/foo.go)" />,
    );
    const a = container.querySelector("a");
    expect(a?.getAttribute("href")).toBe("file:///Users/me/foo.go");
  });

  it("strips javascript: href", () => {
    const { container } = render(
      <MarkdownText text="[x](javascript:alert(1))" />,
    );
    const a = container.querySelector("a");
    // RichLink falls back to plain anchor with href stripped
    expect(a?.getAttribute("href")).toBeFalsy();
  });
});
```

- [ ] **Step 2: 跑测试看部分失败**

```bash
cd frontend && pnpm exec vitest run src/components/agentre/markdown-text.test.tsx
```

Expected: file:// 那条 fail（react-markdown 默认 strip），javascript: 那条可能 pass，POSIX path 那条可能 fail（取决于 react-markdown 当前 sanitize）。

- [ ] **Step 3: 改 markdown-text.tsx**

```tsx
// frontend/src/components/agentre/markdown-text.tsx

// (在文件顶部 imports 区域已有的下面加)
import { RichLink } from "./rich-link";

// (在 markdownComponents 定义之前加 url 白名单 transform)
const SAFE_HREF_PATTERNS: RegExp[] = [
  /^https?:/i,
  /^mailto:/i,
  /^tel:/i,
  /^file:\/\//i,
  /^www\./i,
  /^\//, // POSIX 绝对
  /^[A-Za-z]:[\\/]/, // Windows 绝对
  /^#/, // fragment
];

function whitelistUrl(url: string): string {
  for (const p of SAFE_HREF_PATTERNS) {
    if (p.test(url)) return url;
  }
  return "";
}
```

把 `markdownComponents.a` 整段替换：

```tsx
// Before (line ~53):
//   a: ({ node: _node, className, ...props }) => (
//     <a {...props} ... target="_blank" rel="noreferrer noopener" />
//   ),
// After:
```

```tsx
// 注意: components 是模块顶层常量；要能拿到 cwd，把组件下沉到 MarkdownText
// 里用 useMemo 构造，避免每次 render 都 new 一个新 components 对象触发
// react-markdown 内部重建 processor。
```

完整修改后的 `markdown-text.tsx` 关键段如下：

```tsx
// 1. 删掉模块顶层的 markdownComponents.a 段
// 2. MarkdownText 改成接收 cwd 并 useMemo 构造 components：

export const MarkdownText = React.memo(function MarkdownText({
  text,
  cwd,
}: {
  text: string;
  cwd?: string;
}) {
  const components = React.useMemo<Components>(
    () => ({
      ...markdownComponentsStatic,
      a: ({ node: _node, href, children, className }) => (
        <RichLink href={href} className={className as string} cwd={cwd}>
          {children}
        </RichLink>
      ),
    }),
    [cwd],
  );

  return (
    <div className="markdown-body break-words text-sm leading-relaxed">
      <ReactMarkdown
        components={components}
        remarkPlugins={markdownRemarkPlugins}
        rehypePlugins={markdownRehypePlugins}
        urlTransform={whitelistUrl}
      >
        {text}
      </ReactMarkdown>
    </div>
  );
});
```

把原来的 `markdownComponents` 重命名为 `markdownComponentsStatic` 并删掉里面的 `a` 字段。

- [ ] **Step 4: 跑全部 markdown-text 测试**

```bash
cd frontend && pnpm exec vitest run src/components/agentre/markdown-text.test.tsx
```

Expected: 全 PASS（包括前面已有的 fenced code 测试 + 新加的白名单测试）。

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/agentre/markdown-text.tsx frontend/src/components/agentre/markdown-text.test.tsx
git commit -m "feat(chat): wire RichLink into MarkdownText with url whitelist

Replaces the plain <a> override with RichLink, threads cwd prop through.
Adds an explicit urlTransform whitelist (http/https/mailto/tel/file://,
POSIX/Windows absolute paths, fragments) so AI-emitted absolute path
hrefs are preserved instead of being stripped by react-markdown's
default sanitizer. javascript:/data: hrefs are dropped."
```

---

## Task 6: chat.tsx 把 cwd 透传到 MarkdownText

**Files:**
- Modify: `frontend/src/components/agentre/chat.tsx:1269` 附近

`renderMessageBlocks` 已经在 1042 行收 `cwd?: string`；它在 1269 行 `return <MarkdownText key={...} text={item.text} />` 没传。补上即可。

- [ ] **Step 1: 改 chat.tsx**

```tsx
// frontend/src/components/agentre/chat.tsx:1269
// Before:
//   case "text":
//     return <MarkdownText key={`text-${idx}`} text={item.text} />;
// After:
      case "text":
        return <MarkdownText key={`text-${idx}`} cwd={cwd} text={item.text} />;
```

只动这一行。

- [ ] **Step 2: TypeScript 编译**

```bash
cd frontend && pnpm exec tsc -b --noEmit
```

Expected: 无错误。

- [ ] **Step 3: 跑 chat 相关测试**

```bash
cd frontend && pnpm exec vitest run src/components/agentre/__tests__/chat.test.tsx src/components/agentre/__tests__/chat-page.test.tsx
```

Expected: 全 PASS（不改行为，只多传 prop）。

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/agentre/chat.tsx
git commit -m "chore(chat): thread cwd into MarkdownText so RichLink can resolve relative path"
```

---

## Task 7: 全量 lint + test

**Files:**
- 无新增

- [ ] **Step 1: Run make check**

```bash
make check
```

Expected: Backend `go test -race ./...` + Frontend Vitest + golangci-lint + ESLint 全 PASS。

如果失败：
- 修复对应文件，重新跑直到通过
- 不允许 `--no-verify` 或 skip
- 失败原因若是已有测试受影响（不该），回头查 cwd 透传是否漏掉某条路径

- [ ] **Step 2: Commit 任何 fix（如果有）**

```bash
git status
# 若有 fix 提交
git add -p
git commit -m "fix: <具体原因>"
```

---

## Task 8: 手动验证（不能省，因为 UI 验证不进 CI）

**Files:**
- 无

- [ ] **Step 1: 启动 dev**

```bash
make dev
```

- [ ] **Step 2: 用例验证 (golden path)**

打开任意 chat session，让 AI 给一段含 markdown link 的回复（或在已有 session 里翻找）。要看：

1. **URL link** (`[name](https://...)`) — 文末有 external-link 小图标；hover 200ms 后浮出 popover，看到 "URL" badge、完整 URL、复制按钮、"点击在系统浏览器中打开"
2. **cwd 内文件** (`[file.go:42](/.../foo.go:42)`) — 文末有 file-text 图标；hover popover 看到 "本地文件" badge + `L42` chip + 项目根 + 相对路径分段 + 完整路径 + 复制按钮
3. **cwd 外文件** (`[ag](/usr/local/bin/agentred)`) — 文末有 folder 图标；hover popover 看到 "本地文件 · 项目外" badge + 完整路径，无分段
4. 点 URL link → 系统默认浏览器打开
5. 点本地路径 link → 系统默认编辑器/文件管理器打开
6. 点 popover 里复制按钮 → toast "已复制"、剪贴板里是完整 URL/path

- [ ] **Step 3: 边界 (edge cases)**

- 相对路径 `[x](relative/foo.go)` — 应渲染成普通 `<a target=_blank>`，无 icon、无 popover、点击行为是默认（webview 拦截）
- 没有 cwd 的对话 (新 session 前？) — 绝对路径应判为 local-external
- 长 URL — popover 里 monospace 文本应换行不溢出
- 行号 :100:25 — `L100:25` chip 正确显示，copy 出来的也包含

- [ ] **Step 4: 验证完毕，关闭 dev**

如果一切 OK，结束。如果有问题，回到对应 Task 修补。

---

## Verification Checklist (final review)

- [ ] `make check` 全绿
- [ ] 手动验证 6 个 golden path + 4 个 edge case 全通过
- [ ] git log 看到独立的 commit 给每个 Task（不要 squash 成一个）
- [ ] Spec 里的 "测试清单" 每条都对应到一个跑过的测试 (`classifyLink` × 全形态 / `RichLink` × hover+click+copy / `OpenPath` × validate+dispatch+error)
- [ ] 没有顺手改无关文件（保持 diff 只在 file map 列的范围）
