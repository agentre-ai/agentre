# 聊天输入框拖拽文件/文件夹 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把文件/文件夹拖进聊天输入框时,图片文件做附件预览,其余文件与文件夹在光标处插入绝对路径。

**Architecture:** 用 Wails v2 原生 `OnFileDrop`(唯一能拿到绝对路径的途径)做落地源;一个集中式注册表按 `elementFromPoint` 把 drop 路由到光标所在的 composer;纯函数 `resolveDroppedPaths` 把路径解析成 {图片附件, 插入文本},图片候选交后端按路径读取+校验。拖拽永不死路——任何无法成为图片附件的项都降级为插入路径。

**Tech Stack:** Go 1.26 (chat_svc, Wails options) + React 19/TypeScript (TipTap composer, Vitest) + Wails v2.12.0 runtime。

设计依据: `docs/superpowers/specs/2026-06-11-chat-file-drop-design.md`。

---

## 文件结构

**后端 (`agentre/`):**
- `internal/service/chat_svc/types.go` — 新增 `ReadDroppedImagesRequest/Response` + `DroppedImageItem`(修改)
- `internal/service/chat_svc/dropped_image.go` — `ReadDroppedImages` + `readDroppedImage` + `sniffImageMIME`(新建)
- `internal/service/chat_svc/dropped_image_test.go` — 后端单测,用临时文件,不连 DB(新建)
- `internal/app/chat.go` — `ChatReadDroppedImages` 绑定透传(修改)

**前端 (`agentre/frontend/`):**
- `src/components/agentre/chat-input/drop.ts` — `classifyDroppedPaths` / `formatPathsForInput` / `resolveDroppedPaths` 纯逻辑(新建)
- `src/components/agentre/chat-input/__tests__/drop.test.ts` — 纯逻辑单测(新建)
- `src/components/agentre/chat-input/types.ts` — `AIChatInputHandle` 加 `insertText`(修改)
- `src/components/agentre/chat-input/index.tsx` — 实现 `insertText`(修改)
- `src/components/agentre/chat-input/__tests__/insert-text.test.tsx` — `insertText` 单测(新建)
- `src/lib/file-drop.ts` — `registerDropZone` + 全局 `OnFileDrop` 路由(新建)
- `src/lib/__tests__/file-drop.test.ts` — 路由单测(新建)
- `src/components/agentre/chat-input/use-file-drop.ts` — `useFileDropZone` hook(新建)
- `src/components/agentre/chat-input/__tests__/use-file-drop.test.tsx` — hook 单测(新建)
- `src/components/agentre/chat.tsx` — `ChatComposer` 接拖拽 + overlay(修改)
- `src/i18n/locales/{zh-CN,en}/common.json` — `chat.composer.dropHint`(修改)

**装配 (`agentre/`):**
- `main.go` — `options.DragAndDrop{EnableFileDrop:true}`(修改)

> 所有命令都在 `agentre/` 子目录下执行(`cd /Users/codfrm/Code/agentre/agentre`)。提交都进 `agentre/` 这个 git 仓库,gitmoji 风格。后端测试用 `make test-backend`(`go test ./...` 会扫到 `frontend/node_modules`)。前端单测用 `cd frontend && pnpm test -- <file>`。

---

## Task 1: 后端 `ReadDroppedImages`(按路径读图 + 归类)

**Files:**
- Modify: `internal/service/chat_svc/types.go`(在 `SendImage` 定义附近追加)
- Create: `internal/service/chat_svc/dropped_image.go`
- Create: `internal/service/chat_svc/dropped_image_test.go`
- Modify: `internal/app/chat.go`(在 `SendChatMessage` 绑定附近追加)

- [ ] **Step 1: 先加类型(测试要引用)**

在 `internal/service/chat_svc/types.go` 的 `SendImage` 结构体下方追加:

```go
// ── 拖拽图片读取 (ReadDroppedImages) ─────────────────────────────────────────

type ReadDroppedImagesRequest struct {
	Paths []string `json:"paths"`
}

type ReadDroppedImagesResponse struct {
	Items []DroppedImageItem `json:"items"`
}

// DroppedImageItem 是单个拖入路径的归类结果。
//   - Kind=="image": 可作图片附件,Name/MediaType/DataURL 给出。
//   - Kind=="path":  降级为纯路径(目录/超限/类型不符/读失败),调用方应把 Path 当文本插入。
type DroppedImageItem struct {
	Path      string `json:"path"`
	Kind      string `json:"kind"`
	Name      string `json:"name,omitempty"`
	MediaType string `json:"mediaType,omitempty"`
	DataURL   string `json:"dataUrl,omitempty"`
}
```

- [ ] **Step 2: 写失败测试** — `internal/service/chat_svc/dropped_image_test.go`

```go
package chat_svc

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTestPNG(t *testing.T, path string) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write png: %v", err)
	}
}

func TestReadDroppedImages_ClassifiesEachPath(t *testing.T) {
	dir := t.TempDir()

	pngPath := filepath.Join(dir, "shot.png")
	writeTestPNG(t, pngPath)

	txtPath := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(txtPath, []byte("hello world"), 0o644); err != nil {
		t.Fatal(err)
	}
	fakePNG := filepath.Join(dir, "fake.png") // 扩展名像图片,内容是文本
	if err := os.WriteFile(fakePNG, []byte("not really an image"), 0o644); err != nil {
		t.Fatal(err)
	}
	subdir := filepath.Join(dir, "folder")
	if err := os.Mkdir(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "ghost.png")

	resp, err := ReadDroppedImages(context.Background(), &ReadDroppedImagesRequest{
		Paths: []string{pngPath, txtPath, fakePNG, subdir, missing},
	})
	if err != nil {
		t.Fatalf("ReadDroppedImages: %v", err)
	}
	if len(resp.Items) != 5 {
		t.Fatalf("items=%d want 5", len(resp.Items))
	}

	got := resp.Items[0]
	if got.Kind != "image" || got.MediaType != "image/png" || got.Name != "shot.png" ||
		!strings.HasPrefix(got.DataURL, "data:image/png;base64,") {
		t.Fatalf("png item = %+v", got)
	}
	for i, p := range []string{txtPath, fakePNG, subdir, missing} {
		item := resp.Items[i+1]
		if item.Kind != "path" || item.Path != p {
			t.Fatalf("item[%d] = %+v want kind=path path=%s", i+1, item, p)
		}
	}
}

func TestReadDroppedImages_OversizeDegradesToPath(t *testing.T) {
	dir := t.TempDir()
	big := filepath.Join(dir, "big.png")
	if err := os.WriteFile(big, make([]byte, maxDropImageBytes+1), 0o644); err != nil {
		t.Fatal(err)
	}
	resp, err := ReadDroppedImages(context.Background(), &ReadDroppedImagesRequest{Paths: []string{big}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Items[0].Kind != "path" {
		t.Fatalf("oversize item = %+v want kind=path", resp.Items[0])
	}
}

func TestReadDroppedImages_NilRequest(t *testing.T) {
	resp, err := ReadDroppedImages(context.Background(), nil)
	if err != nil || resp == nil || len(resp.Items) != 0 {
		t.Fatalf("nil req: resp=%+v err=%v", resp, err)
	}
}
```

- [ ] **Step 3: 运行,确认编译失败/测试失败**

Run: `cd /Users/codfrm/Code/agentre/agentre && go test ./internal/service/chat_svc/ -run TestReadDroppedImages`
Expected: FAIL —— `undefined: ReadDroppedImages` / `undefined: maxDropImageBytes`。

- [ ] **Step 4: 实现** — `internal/service/chat_svc/dropped_image.go`

```go
package chat_svc

import (
	"context"
	"encoding/base64"
	"net/http"
	"os"
	"path/filepath"
)

// maxDropImageBytes 与前端 MAX_CHAT_IMAGE_BYTES 对齐。
const maxDropImageBytes = 5 * 1024 * 1024

// dropImageMIMEs 是允许作图片附件的嗅探 MIME 集合(对齐 CHAT_IMAGE_ACCEPT)。
var dropImageMIMEs = map[string]struct{}{
	"image/png":  {},
	"image/jpeg": {},
	"image/webp": {},
}

// ReadDroppedImages 按绝对路径读取拖入的图片候选,做 stat/类型/大小校验后归类。
// 永不返回 per-item 错误:任何无法成为图片附件的情况都归为 Kind="path"(降级)。
func ReadDroppedImages(_ context.Context, req *ReadDroppedImagesRequest) (*ReadDroppedImagesResponse, error) {
	resp := &ReadDroppedImagesResponse{}
	if req == nil {
		return resp, nil
	}
	resp.Items = make([]DroppedImageItem, 0, len(req.Paths))
	for _, p := range req.Paths {
		resp.Items = append(resp.Items, readDroppedImage(p))
	}
	return resp, nil
}

// readDroppedImage 判定单个路径;任何失败一律降级为 path。
func readDroppedImage(path string) DroppedImageItem {
	degrade := DroppedImageItem{Path: path, Kind: "path"}

	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return degrade // 不存在 / 目录 / 非常规文件
	}
	if info.Size() > maxDropImageBytes {
		return degrade // 超限,不读取
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return degrade
	}
	mime := sniffImageMIME(data)
	if _, ok := dropImageMIMEs[mime]; !ok {
		return degrade // 类型不符
	}
	return DroppedImageItem{
		Path:      path,
		Kind:      "image",
		Name:      filepath.Base(path),
		MediaType: mime,
		DataURL:   "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data),
	}
}

// sniffImageMIME 用 http.DetectContentType 嗅探前 512 字节。Go 标准库 sniff 表
// 已支持 png/jpeg/webp(net/http/sniff.go),无需自定义 webp 兜底。
func sniffImageMIME(data []byte) string {
	if len(data) > 512 {
		data = data[:512]
	}
	return http.DetectContentType(data)
}
```

- [ ] **Step 5: 运行,确认通过**

Run: `cd /Users/codfrm/Code/agentre/agentre && go test ./internal/service/chat_svc/ -run TestReadDroppedImages -v`
Expected: PASS(3 个测试)。

- [ ] **Step 6: 加 Wails 绑定** — `internal/app/chat.go`,在 `SendChatMessage`(38 行)下方追加

```go
// ChatReadDroppedImages 按绝对路径读取拖入的图片候选,做 stat/类型/大小校验后归类
// (image=可附件 / path=降级为纯路径)。供 composer 拖拽链路区分图片与文件/文件夹。
func (a *App) ChatReadDroppedImages(req *chat_svc.ReadDroppedImagesRequest) (*chat_svc.ReadDroppedImagesResponse, error) {
	return chat_svc.ReadDroppedImages(a.ctx, req)
}
```

- [ ] **Step 7: 重新生成前端绑定**

Run: `cd /Users/codfrm/Code/agentre/agentre && make generate`
Expected: 成功;`frontend/wailsjs/go/app/App.d.ts` 出现 `export function ChatReadDroppedImages(...)`,`frontend/wailsjs/go/models.ts` 出现 `ReadDroppedImagesRequest/Response`、`DroppedImageItem`。
验证: `grep -n "ChatReadDroppedImages" frontend/wailsjs/go/app/App.d.ts`

- [ ] **Step 8: 提交**

```bash
cd /Users/codfrm/Code/agentre/agentre
git add internal/service/chat_svc/types.go internal/service/chat_svc/dropped_image.go \
  internal/service/chat_svc/dropped_image_test.go internal/app/chat.go frontend/wailsjs
git commit -m "✨ chat: 后端 ReadDroppedImages 按路径读图+归类(拖拽图片附件)"
```

---

## Task 2: 前端纯逻辑 `drop.ts`(分类 / 格式化 / 解析)

**Files:**
- Create: `frontend/src/components/agentre/chat-input/drop.ts`
- Create: `frontend/src/components/agentre/chat-input/__tests__/drop.test.ts`

- [ ] **Step 1: 写失败测试** — `__tests__/drop.test.ts`

```ts
import { describe, expect, it } from "vitest";

import {
  classifyDroppedPaths,
  formatPathsForInput,
  resolveDroppedPaths,
  type DroppedImageItem,
} from "../drop";

describe("classifyDroppedPaths", () => {
  it("按扩展名分流图片与其余(含文件夹)", () => {
    const { imageCandidates, plainPaths } = classifyDroppedPaths([
      "/a/x.PNG",
      "/a/y.jpeg",
      "/a/doc.pdf",
      "/a/project", // 文件夹,无扩展名
      "/a/archive.tar.gz",
    ]);
    expect(imageCandidates).toEqual(["/a/x.PNG", "/a/y.jpeg"]);
    expect(plainPaths).toEqual(["/a/doc.pdf", "/a/project", "/a/archive.tar.gz"]);
  });
});

describe("formatPathsForInput", () => {
  it("空数组返回空串", () => {
    expect(formatPathsForInput([])).toBe("");
  });
  it("含空格的路径加双引号,空格分隔,末尾补空格", () => {
    expect(formatPathsForInput(["/a/b.txt", "/c d/e.txt"])).toBe(
      `/a/b.txt "/c d/e.txt" `,
    );
  });
});

describe("resolveDroppedPaths", () => {
  const imageItem = (path: string): DroppedImageItem => ({
    path,
    kind: "image",
    name: path.split("/").pop(),
    mediaType: "image/png",
    dataUrl: "data:image/png;base64,AAAA",
  });

  it("allowImages=false 时图片也降级为路径", async () => {
    const readImages = async () => [];
    const res = await resolveDroppedPaths(["/a/x.png", "/a/y.pdf"], {
      allowImages: false,
      remainingImageSlots: 4,
      readImages,
    });
    expect(res.attachments).toHaveLength(0);
    expect(res.text).toBe(`/a/y.pdf /a/x.png `);
  });

  it("图片在配额内 → 附件,不进插入文本", async () => {
    const res = await resolveDroppedPaths(["/a/x.png", "/a/doc.pdf"], {
      allowImages: true,
      remainingImageSlots: 4,
      readImages: async (p) => p.map(imageItem),
    });
    expect(res.attachments).toEqual([
      { dataUrl: "data:image/png;base64,AAAA", mediaType: "image/png", name: "x.png" },
    ]);
    expect(res.text).toBe(`/a/doc.pdf `);
  });

  it("后端把图片判成 path → 降级插入路径", async () => {
    const res = await resolveDroppedPaths(["/a/x.png"], {
      allowImages: true,
      remainingImageSlots: 4,
      readImages: async (p) => p.map((path) => ({ path, kind: "path" as const })),
    });
    expect(res.attachments).toHaveLength(0);
    expect(res.text).toBe(`/a/x.png `);
  });

  it("配额溢出的图片降级为路径", async () => {
    const res = await resolveDroppedPaths(["/a/1.png", "/a/2.png", "/a/3.png"], {
      allowImages: true,
      remainingImageSlots: 2,
      readImages: async (p) => p.map(imageItem),
    });
    expect(res.attachments).toHaveLength(2);
    expect(res.text).toBe(`/a/3.png `);
  });
});
```

- [ ] **Step 2: 运行,确认失败**

Run: `cd /Users/codfrm/Code/agentre/agentre/frontend && pnpm test -- src/components/agentre/chat-input/__tests__/drop.test.ts`
Expected: FAIL —— 无法解析 `../drop`。

- [ ] **Step 3: 实现** — `frontend/src/components/agentre/chat-input/drop.ts`

```ts
import type { ChatImageAttachment } from "../chat";

export type DroppedImageItem = {
  path: string;
  kind: "image" | "path";
  name?: string;
  mediaType?: string;
  dataUrl?: string;
};

// 对齐 chat.tsx 的 CHAT_IMAGE_ACCEPT(image/png,image/jpeg,image/webp)。
const DROP_IMAGE_EXTENSIONS = ["png", "jpg", "jpeg", "webp"];

// 取路径最后一段的小写扩展名;无扩展名/以点开头/以点结尾 → ""。
function extensionOf(path: string): string {
  const base = path.split(/[\\/]/).pop() ?? "";
  const dot = base.lastIndexOf(".");
  if (dot <= 0 || dot === base.length - 1) return "";
  return base.slice(dot + 1).toLowerCase();
}

export function classifyDroppedPaths(paths: string[]): {
  imageCandidates: string[];
  plainPaths: string[];
} {
  const imageCandidates: string[] = [];
  const plainPaths: string[] = [];
  for (const p of paths) {
    if (DROP_IMAGE_EXTENSIONS.includes(extensionOf(p))) imageCandidates.push(p);
    else plainPaths.push(p);
  }
  return { imageCandidates, plainPaths };
}

// 把绝对路径拼成插入文本:含空白的路径用双引号包裹,空格分隔,末尾补一个空格。
export function formatPathsForInput(paths: string[]): string {
  if (paths.length === 0) return "";
  return paths.map((p) => (/\s/.test(p) ? `"${p}"` : p)).join(" ") + " ";
}

// resolveDroppedPaths 把拖入路径解析成 {要追加的图片附件, 要插入的文本}。
// 纯逻辑 + 注入的 readImages 依赖,便于单测;拖拽永不死路——所有非附件项降级为路径。
export async function resolveDroppedPaths(
  paths: string[],
  opts: {
    allowImages: boolean;
    remainingImageSlots: number;
    readImages: (imagePaths: string[]) => Promise<DroppedImageItem[]>;
  },
): Promise<{ attachments: ChatImageAttachment[]; text: string }> {
  const { imageCandidates, plainPaths } = classifyDroppedPaths(paths);
  const toInsert = [...plainPaths];
  const attachments: ChatImageAttachment[] = [];

  if (!opts.allowImages || imageCandidates.length === 0) {
    toInsert.push(...imageCandidates); // 图片也降级为路径
  } else {
    const items = await opts.readImages(imageCandidates);
    let slots = opts.remainingImageSlots;
    for (const item of items) {
      if (
        item.kind === "image" &&
        slots > 0 &&
        item.dataUrl &&
        item.mediaType &&
        item.name !== undefined
      ) {
        attachments.push({
          dataUrl: item.dataUrl,
          mediaType: item.mediaType,
          name: item.name,
        });
        slots--;
      } else {
        toInsert.push(item.path); // path 归类 或 配额溢出 → 降级
      }
    }
  }

  return { attachments, text: formatPathsForInput(toInsert) };
}
```

- [ ] **Step 4: 运行,确认通过**

Run: `cd /Users/codfrm/Code/agentre/agentre/frontend && pnpm test -- src/components/agentre/chat-input/__tests__/drop.test.ts`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
cd /Users/codfrm/Code/agentre/agentre
git add frontend/src/components/agentre/chat-input/drop.ts \
  frontend/src/components/agentre/chat-input/__tests__/drop.test.ts
git commit -m "✨ chat: 拖拽路径分类/格式化/解析纯逻辑 (drop.ts)"
```

---

## Task 3: `AIChatInput.insertText`

**Files:**
- Modify: `frontend/src/components/agentre/chat-input/types.ts`(`AIChatInputHandle`,7-13 行)
- Modify: `frontend/src/components/agentre/chat-input/index.tsx`(`useImperativeHandle`,258-275 行)
- Create: `frontend/src/components/agentre/chat-input/__tests__/insert-text.test.tsx`

- [ ] **Step 1: 写失败测试** — `__tests__/insert-text.test.tsx`

```tsx
import { act, render } from "@testing-library/react";
import { createRef, type RefObject } from "react";
import { describe, expect, it } from "vitest";

import type { Editor } from "@tiptap/react";

import { AIChatInput } from "../index";
import type { AIChatInputHandle } from "../types";

describe("AIChatInput.insertText", () => {
  it("把文本插入编辑器当前位置", () => {
    const handleRef = createRef<AIChatInputHandle>();
    const editorRef: RefObject<Editor | null> = { current: null };
    render(<AIChatInput ref={handleRef} editorRef={editorRef} onSubmit={() => {}} autoFocus />);

    act(() => {
      handleRef.current!.insertText(`/Users/a/b.txt `);
    });
    expect(editorRef.current!.getText()).toBe("/Users/a/b.txt");
  });
});
```

> TipTap `getText()` 不含末尾的纯空白,所以断言 `"/Users/a/b.txt"`(插入的是带末尾空格的文本)。

- [ ] **Step 2: 运行,确认失败**

Run: `cd /Users/codfrm/Code/agentre/agentre/frontend && pnpm test -- src/components/agentre/chat-input/__tests__/insert-text.test.tsx`
Expected: FAIL —— `insertText` 不在 `AIChatInputHandle` 上 / `handleRef.current.insertText is not a function`。

- [ ] **Step 3: 类型加方法** — `types.ts` 的 `AIChatInputHandle`

```ts
export interface AIChatInputHandle {
  focus: () => void;
  clear: () => void;
  isEmpty: () => boolean;
  submit: () => void;
  loadDraft: (draft: string | AIChatInputDraft) => void;
  insertText: (text: string) => void;
}
```

- [ ] **Step 4: 实现** — `index.tsx` 的 `useImperativeHandle`(在 `loadDraft` 之后追加)

```ts
        loadDraft: (draft) => {
          if (!editor) return;
          historyIndexRef.current = -1;
          applyInputHistoryMessage(editor, draft);
        },
        insertText: (text) => {
          // 与 slash literal_text 插入同手法(index.tsx 内 slashSelectHandler):
          // focus + insertContent,插在当前光标处,不自动发送。
          editor?.chain().focus().insertContent(text).run();
        },
```

- [ ] **Step 5: 运行,确认通过**

Run: `cd /Users/codfrm/Code/agentre/agentre/frontend && pnpm test -- src/components/agentre/chat-input/__tests__/insert-text.test.tsx`
Expected: PASS。

- [ ] **Step 6: 提交**

```bash
cd /Users/codfrm/Code/agentre/agentre
git add frontend/src/components/agentre/chat-input/types.ts \
  frontend/src/components/agentre/chat-input/index.tsx \
  frontend/src/components/agentre/chat-input/__tests__/insert-text.test.tsx
git commit -m "✨ chat-input: AIChatInputHandle 新增 insertText(光标处插入文本)"
```

---

## Task 4: 拖拽路由层 `lib/file-drop.ts`

**Files:**
- Create: `frontend/src/lib/file-drop.ts`
- Create: `frontend/src/lib/__tests__/file-drop.test.ts`

- [ ] **Step 1: 写失败测试** — `__tests__/file-drop.test.ts`

```ts
import { beforeEach, describe, expect, it, vi } from "vitest";

import { OnFileDrop, OnFileDropOff } from "../../../wailsjs/runtime/runtime";
import { __resetDropRegistryForTest, registerDropZone } from "../file-drop";

vi.mock("../../../wailsjs/runtime/runtime", () => ({
  OnFileDrop: vi.fn(),
  OnFileDropOff: vi.fn(),
}));

// 取最近一次传给 OnFileDrop 的回调。
function dropCallback(): (x: number, y: number, paths: string[]) => void {
  const calls = (OnFileDrop as unknown as ReturnType<typeof vi.fn>).mock.calls;
  return calls[calls.length - 1][0];
}

describe("file-drop 路由", () => {
  beforeEach(() => {
    __resetDropRegistryForTest();
    vi.mocked(OnFileDrop).mockClear();
    vi.mocked(OnFileDropOff).mockClear();
  });

  it("首个注册装上全局 OnFileDrop", () => {
    const el = document.createElement("div");
    registerDropZone(el, () => {});
    expect(OnFileDrop).toHaveBeenCalledTimes(1);
    expect(OnFileDrop).toHaveBeenCalledWith(expect.any(Function), true);
  });

  it("drop 路由到光标命中的 zone", () => {
    const a = document.createElement("div");
    const b = document.createElement("div");
    const hitA = vi.fn();
    const hitB = vi.fn();
    registerDropZone(a, hitA);
    registerDropZone(b, hitB);

    document.elementFromPoint = vi.fn(() => b) as never;
    dropCallback()(10, 20, ["/a/x.txt"]);

    expect(hitB).toHaveBeenCalledWith(["/a/x.txt"]);
    expect(hitA).not.toHaveBeenCalled();
  });

  it("光标不在任何 zone → 不抛、不调用", () => {
    const a = document.createElement("div");
    const hitA = vi.fn();
    registerDropZone(a, hitA);
    document.elementFromPoint = vi.fn(() => document.body) as never;
    expect(() => dropCallback()(1, 1, ["/a/x.txt"])).not.toThrow();
    expect(hitA).not.toHaveBeenCalled();
  });

  it("最后一个注销时卸掉监听", () => {
    const el = document.createElement("div");
    const off = registerDropZone(el, () => {});
    off();
    expect(OnFileDropOff).toHaveBeenCalledTimes(1);
  });
});
```

- [ ] **Step 2: 运行,确认失败**

Run: `cd /Users/codfrm/Code/agentre/agentre/frontend && pnpm test -- src/lib/__tests__/file-drop.test.ts`
Expected: FAIL —— 无法解析 `../file-drop`。

- [ ] **Step 3: 实现** — `frontend/src/lib/file-drop.ts`

```ts
import { OnFileDrop, OnFileDropOff } from "../../wailsjs/runtime/runtime";

type DropHandler = (paths: string[]) => void;

// 每窗口唯一的 OnFileDrop 回调 + 多个 composer 的 drop 目标。用 elementFromPoint
// 把 drop 路由到光标命中的 zone。
const registry = new Map<HTMLElement, DropHandler>();
let listening = false;

function handleDrop(x: number, y: number, paths: string[]): void {
  if (paths.length === 0) return;
  const target = document.elementFromPoint(x, y);
  if (!target) return;
  for (const [el, handler] of registry) {
    if (el === target || el.contains(target)) {
      handler(paths);
      return;
    }
  }
}

function ensureListening(): void {
  if (listening) return;
  // useDropTarget=true:Wails 只在带 --wails-drop-target:drop 的元素上空触发。
  OnFileDrop((x, y, paths) => handleDrop(x, y, paths), true);
  listening = true;
}

// registerDropZone 注册 composer 的 drop 目标 + 回调,并打上 Wails useDropTarget
// 所需的 CSS 标记。返回注销函数(最后一个注销时卸掉全局监听)。
export function registerDropZone(el: HTMLElement, handler: DropHandler): () => void {
  registry.set(el, handler);
  el.style.setProperty("--wails-drop-target", "drop");
  ensureListening();
  return () => {
    registry.delete(el);
    el.style.removeProperty("--wails-drop-target");
    if (registry.size === 0 && listening) {
      OnFileDropOff();
      listening = false;
    }
  };
}

// 仅测试用:重置 module 级单例状态。
export function __resetDropRegistryForTest(): void {
  registry.clear();
  listening = false;
}
```

- [ ] **Step 4: 运行,确认通过**

Run: `cd /Users/codfrm/Code/agentre/agentre/frontend && pnpm test -- src/lib/__tests__/file-drop.test.ts`
Expected: PASS(4 个)。

- [ ] **Step 5: 提交**

```bash
cd /Users/codfrm/Code/agentre/agentre
git add frontend/src/lib/file-drop.ts frontend/src/lib/__tests__/file-drop.test.ts
git commit -m "✨ chat: Wails OnFileDrop 路由层(按 elementFromPoint 分发到 composer)"
```

---

## Task 5: `useFileDropZone` hook

**Files:**
- Create: `frontend/src/components/agentre/chat-input/use-file-drop.ts`
- Create: `frontend/src/components/agentre/chat-input/__tests__/use-file-drop.test.tsx`

- [ ] **Step 1: 写失败测试** — `__tests__/use-file-drop.test.tsx`

```tsx
import { fireEvent, render } from "@testing-library/react";
import { useRef } from "react";
import { describe, expect, it, vi } from "vitest";

import { useFileDropZone } from "../use-file-drop";

vi.mock("@/lib/file-drop", () => ({
  registerDropZone: vi.fn(() => () => {}),
}));

function Harness() {
  const ref = useRef<HTMLDivElement>(null);
  const { isDragOver } = useFileDropZone({ ref, enabled: true, onPaths: () => {} });
  return (
    <div ref={ref} data-testid="zone">
      {isDragOver ? "over" : "idle"}
    </div>
  );
}

// 带 dataTransfer.types 的拖拽事件(happy-dom 没有 DataTransfer 构造)。
function fireDrag(el: Element, type: string, hasFiles = true): Event {
  const ev = new Event(type, { bubbles: true, cancelable: true });
  Object.defineProperty(ev, "dataTransfer", { value: { types: hasFiles ? ["Files"] : [] } });
  el.dispatchEvent(ev);
  return ev;
}

describe("useFileDropZone", () => {
  it("dragenter/dragleave 切换 isDragOver", () => {
    const { getByTestId } = render(<Harness />);
    const zone = getByTestId("zone");
    fireDrag(zone, "dragenter");
    expect(zone.textContent).toBe("over");
    fireDrag(zone, "dragleave");
    expect(zone.textContent).toBe("idle");
  });

  it("非文件拖拽不触发高亮", () => {
    const { getByTestId } = render(<Harness />);
    const zone = getByTestId("zone");
    fireDrag(zone, "dragenter", false);
    expect(zone.textContent).toBe("idle");
  });

  it("dragover/drop 调 preventDefault(阻止 webview 导航)", () => {
    const { getByTestId } = render(<Harness />);
    const zone = getByTestId("zone");
    const over = fireDrag(zone, "dragover");
    const drop = fireDrag(zone, "drop");
    expect(over.defaultPrevented).toBe(true);
    expect(drop.defaultPrevented).toBe(true);
  });

  it("drop 后 isDragOver 复位", () => {
    const { getByTestId } = render(<Harness />);
    const zone = getByTestId("zone");
    fireDrag(zone, "dragenter");
    expect(zone.textContent).toBe("over");
    fireDrag(zone, "drop");
    expect(zone.textContent).toBe("idle");
  });
});
```

- [ ] **Step 2: 运行,确认失败**

Run: `cd /Users/codfrm/Code/agentre/agentre/frontend && pnpm test -- src/components/agentre/chat-input/__tests__/use-file-drop.test.tsx`
Expected: FAIL —— 无法解析 `../use-file-drop`。

- [ ] **Step 3: 实现** — `frontend/src/components/agentre/chat-input/use-file-drop.ts`

```ts
import { useEffect, useRef, useState, type RefObject } from "react";

import { registerDropZone } from "@/lib/file-drop";

// useFileDropZone 把一个元素注册成 Wails 文件 drop 目标,并用 HTML5 拖拽事件驱动
// 高亮状态。真实落地路径来自原生 OnFileDrop(经路由层),HTML5 事件只负责:
//   - dragenter/dragleave → isDragOver 高亮(进入计数避免子元素抖动);
//   - dragover/drop preventDefault → 阻止 webview 把拖入文件当导航/打开。
export function useFileDropZone(opts: {
  ref: RefObject<HTMLElement | null>;
  enabled: boolean;
  onPaths: (paths: string[]) => void;
}): { isDragOver: boolean } {
  const { ref, enabled, onPaths } = opts;
  const [isDragOver, setIsDragOver] = useState(false);
  const onPathsRef = useRef(onPaths);
  useEffect(() => {
    onPathsRef.current = onPaths;
  }, [onPaths]);

  useEffect(() => {
    const el = ref.current;
    if (!enabled || !el) return;

    const unregister = registerDropZone(el, (paths) => onPathsRef.current(paths));

    let depth = 0;
    const onDragEnter = (e: DragEvent) => {
      if (!e.dataTransfer?.types.includes("Files")) return;
      depth++;
      setIsDragOver(true);
    };
    const onDragOver = (e: DragEvent) => e.preventDefault();
    const onDragLeave = () => {
      depth = Math.max(0, depth - 1);
      if (depth === 0) setIsDragOver(false);
    };
    const onDrop = (e: DragEvent) => {
      e.preventDefault();
      depth = 0;
      setIsDragOver(false);
    };

    el.addEventListener("dragenter", onDragEnter);
    el.addEventListener("dragover", onDragOver);
    el.addEventListener("dragleave", onDragLeave);
    el.addEventListener("drop", onDrop);
    return () => {
      unregister();
      el.removeEventListener("dragenter", onDragEnter);
      el.removeEventListener("dragover", onDragOver);
      el.removeEventListener("dragleave", onDragLeave);
      el.removeEventListener("drop", onDrop);
    };
  }, [ref, enabled]);

  return { isDragOver };
}
```

- [ ] **Step 4: 运行,确认通过**

Run: `cd /Users/codfrm/Code/agentre/agentre/frontend && pnpm test -- src/components/agentre/chat-input/__tests__/use-file-drop.test.tsx`
Expected: PASS(4 个)。

- [ ] **Step 5: 提交**

```bash
cd /Users/codfrm/Code/agentre/agentre
git add frontend/src/components/agentre/chat-input/use-file-drop.ts \
  frontend/src/components/agentre/chat-input/__tests__/use-file-drop.test.tsx
git commit -m "✨ chat-input: useFileDropZone hook(注册 drop 目标 + 拖拽高亮)"
```

---

## Task 6: `ChatComposer` 接入拖拽 + overlay + i18n

**Files:**
- Modify: `frontend/src/components/agentre/chat.tsx`
- Modify: `frontend/src/i18n/locales/zh-CN/common.json`
- Modify: `frontend/src/i18n/locales/en/common.json`

- [ ] **Step 1: 加 i18n 文案** — 两个 locale 的 `chat.composer` 对象里加 `dropHint`

`zh-CN/common.json`(`chat.composer.placeholder` 同级):
```json
"dropHint": "松开以添加文件 / 文件夹路径",
```
`en/common.json`(同位置):
```json
"dropHint": "Drop to add files / folder paths",
```

> 现有键名通过 `frontend/src/__tests__/i18n.test.ts` 校验静态 `t(...)` 覆盖率,两个 locale 都加即可。

- [ ] **Step 2: chat.tsx 顶部加 import**(与现有 import 同区)

```ts
import { ChatReadDroppedImages } from "../../../wailsjs/go/app/App";
import { chat_svc } from "../../../wailsjs/go/models";
import { resolveDroppedPaths } from "./chat-input/drop";
import { useFileDropZone } from "./chat-input/use-file-drop";
```

- [ ] **Step 3: 在 `ChatComposer` 体内、`handleSend` 附近(chat.tsx:483 前后)加拖拽逻辑**

```ts
  const dropRef = React.useRef<HTMLFormElement>(null);

  const handleDroppedPaths = React.useCallback(
    (paths: string[]) => {
      void (async () => {
        const { attachments, text } = await resolveDroppedPaths(paths, {
          allowImages: !editing && supportsImageInput,
          remainingImageSlots: MAX_CHAT_IMAGE_COUNT - images.length,
          readImages: async (imagePaths) => {
            const resp = await ChatReadDroppedImages(
              chat_svc.ReadDroppedImagesRequest.createFrom({ paths: imagePaths }),
            );
            return (resp.items ?? []).map((it) => ({
              path: it.path,
              kind: it.kind === "image" ? ("image" as const) : ("path" as const),
              name: it.name,
              mediaType: it.mediaType,
              dataUrl: it.dataUrl,
            }));
          },
        });
        if (attachments.length > 0) {
          setImages((prev) => [...prev, ...attachments]);
          setImageError("");
        }
        if (text) inputRef.current?.insertText(text);
      })();
    },
    [editing, supportsImageInput, images.length],
  );

  const { isDragOver } = useFileDropZone({
    ref: dropRef,
    enabled: true,
    onPaths: handleDroppedPaths,
  });
```

- [ ] **Step 4: 给 `<form>` 挂 ref + relative,并加 overlay**

把 form 开标签(chat.tsx:569-578)改成:
```tsx
    <form
      ref={dropRef}
      className={cn(
        "relative w-full border-t border-border bg-background px-5 py-3.5",
        className,
      )}
      onSubmit={handleFormSubmit}
      onKeyDown={handleFormKeyDown}
      onPasteCapture={handlePasteCapture}
      {...props}
    >
```
在 form 的第一个子元素之前(`<div className={cn("flex w-full flex-col …` 那个卡片之前)插入 overlay:
```tsx
      {isDragOver ? (
        <div
          className="pointer-events-none absolute inset-2 z-10 flex items-center justify-center rounded-md border-2 border-dashed border-ring bg-background/85 text-sm font-medium text-foreground"
          aria-hidden="true"
        >
          {t("chat.composer.dropHint")}
        </div>
      ) : null}
```

> overlay 用 `pointer-events-none`,不拦截 form 上的 dragover/drop 事件。

- [ ] **Step 5: 跑相关测试 + lint,确认不回归**

Run: `cd /Users/codfrm/Code/agentre/agentre/frontend && pnpm test -- src/components/agentre/chat-input src/__tests__/i18n.test.ts`
Expected: PASS。
Run: `cd /Users/codfrm/Code/agentre/agentre && make lint-fix`
Expected: 无 `i18next/no-literal-string` 报错(overlay 文案走了 `t(...)`)。

- [ ] **Step 6: 提交**

```bash
cd /Users/codfrm/Code/agentre/agentre
git add frontend/src/components/agentre/chat.tsx \
  frontend/src/i18n/locales/zh-CN/common.json frontend/src/i18n/locales/en/common.json
git commit -m "✨ chat: ChatComposer 接入文件/文件夹拖拽(图片附件/其余插路径)+ 拖拽高亮"
```

---

## Task 7: 启用 Wails 原生文件拖拽

**Files:**
- Modify: `main.go`(`newWailsOptionsForDataDir` 的 `appOptions`,67-87 行)

- [ ] **Step 1: 在 `appOptions` 里加 `DragAndDrop`**

在 `Bind: []interface{}{a}` 之后追加一行:
```go
		Bind: []interface{}{
			a,
		},
		DragAndDrop: &options.DragAndDrop{
			// 启用 Wails 原生拖拽,回调返回拖入文件的绝对路径(webview HTML5 drop 拿不到)。
			// DisableWebViewDrop 保持 false:让 composer 仍收到 HTML5 dragenter/leave 驱动高亮;
			// 真实路径只来自 OnFileDrop。CSSDropProperty/Value 用默认 --wails-drop-target / drop。
			EnableFileDrop: true,
		},
```

- [ ] **Step 2: 编译确认**

Run: `cd /Users/codfrm/Code/agentre/agentre && go build ./...`
Expected: 成功(`options` 包已 import,无需新增 import)。

- [ ] **Step 3: 提交**

```bash
cd /Users/codfrm/Code/agentre/agentre
git add main.go
git commit -m "✨ app: 启用 Wails 原生文件拖拽 (DragAndDrop.EnableFileDrop)"
```

---

## Task 8: 全量验证 + 手动验证

- [ ] **Step 1: 后端全量(race)**

Run: `cd /Users/codfrm/Code/agentre/agentre && make test-backend`
Expected: PASS(无新增失败;`go test ./...` 会扫 node_modules,务必用 `test-backend`)。

- [ ] **Step 2: 前端全量**

Run: `cd /Users/codfrm/Code/agentre/agentre && make test-frontend`
Expected: PASS。

- [ ] **Step 3: lint**

Run: `cd /Users/codfrm/Code/agentre/agentre && make lint`
Expected: 通过。

- [ ] **Step 4: 手动验证(wails dev)** —— 原生拖拽 + 绑定属 I/O 边界,必须真机过一遍

Run: `cd /Users/codfrm/Code/agentre/agentre && make dev`
逐项确认:
1. 拖一个 `.txt` 文件进输入框 → 光标处出现其绝对路径,末尾带空格。
2. 拖一个文件夹进输入框 → 出现文件夹绝对路径。
3. 拖一张 png/jpg/webp 进输入框 → 出现缩略图预览(不是路径)。
4. 一次多选拖入(2 图 + 1 文件夹 + 1 pdf)→ 2 图预览 + 文件夹/pdf 路径。
5. 拖 5 张图(配额 4)→ 4 张预览 + 第 5 张变成路径。
6. 含空格的路径(如 `~/Desktop/my notes.txt`)→ 插入文本被双引号包裹。
7. 拖拽悬停时输入框出现虚线高亮 overlay,松开/移出后消失。
8. 拖到输入框**以外**的区域 → 无反应,且 webview 不导航/打开文件。
9. 群聊 composer 同样生效(多 composer 路由正确)。

- [ ] **Step 5: 收尾**

确认无遗留调试代码;`git status` 干净(只含本功能改动,未夹带仓库里既有的 README/docs/pkg/codex 等无关改动)。

---

## Self-Review(已对照 spec)

- **Spec 覆盖**:Wails 启用(Task 7)/ 路由层(Task 4)/ 分类+格式化+解析(Task 2)/ insertText(Task 3)/ hook+高亮+preventDefault(Task 5)/ composer 集成+overlay+i18n(Task 6)/ 后端读图+stat+sniff+降级(Task 1)/ 手动验证(Task 8)。spec 各节均有对应任务。
- **降级原则**:`resolveDroppedPaths`(图片→path 兜底、配额溢出→path、allowImages=false→path)+ 后端 `readDroppedImage`(目录/超限/类型不符/读失败→path)共同实现"拖拽永不死路";`imageError` 槽位未被拖拽链路写入(保留给粘贴/选择)。
- **类型一致**:后端 `DroppedImageItem{Path,Kind,Name,MediaType,DataURL}` ↔ 前端 `DroppedImageItem`(同字段)↔ `ChatImageAttachment{dataUrl,mediaType,name}`,映射在 Task 6 readImages 闭包内显式完成。`ReadDroppedImagesRequest/Response` 名称在后端类型、绑定、生成 models、前端调用处一致。
- **WEBP 嗅探**:已实测 Go `net/http/sniff.go:122` 原生支持,`sniffImageMIME` 直接用 `http.DetectContentType`,无需自定义兜底(spec 的待验证项已解决)。
- **无占位符**:每个改动步骤都给了完整代码与可运行命令 + 预期输出。
