# AI 消息链接增强 — 设计文档

- **Date**: 2026-05-28
- **Status**: Design approved, pending implementation plan
- **Scope**: 前端 `markdown-text.tsx` + 新增 link 子组件、Wails Go binding `OpenPath`
- **Mockup**: `agentry.pen` → "Markdown Link Enhancement — Mockup"

## 背景

`MarkdownText` 已经接管了 `<a>` 渲染（`markdown-text.tsx:53`），把所有 link 渲染成 primary 色 + 下划线 + `target="_blank"`。问题：

1. AI 输出的本地文件路径形如 `[file.go:42](/Users/me/foo.go:42)`，react-markdown 把绝对路径透传到 `href`，但点击行为是浏览器尝试 navigate 到 `/Users/me/foo.go:42`，在 Wails webview 里基本无效。
2. 长 URL / 长路径在 link 文本里被截短显示（如 `[file.go:42](...)`），用户看不到完整目标。
3. 无法区分 URL、cwd 内路径、cwd 外路径三种语义。

remark-gfm 自动 linkify 已经处理 `https://` / `www.` 裸文本，这次**不做**裸文本路径的 auto-linkify。

### DB 实测 — AI 路径输出形态分布

扫 `chat_messages.blocks_json` 里 assistant 文本，过滤出真实 AI 输出里的路径出现形态：

| 形态 | 次数 | 渲染结果 | 本次是否处理 |
|---|---|---|---|
| `[name](/abs/path:line)` markdown link | 58 | `<a href="/abs/path:line">name</a>` | ✅ 本 spec 覆盖 |
| `[name](relative/path)` markdown link | 0 | — | — |
| `` `internal/foo.go` `` inline code | 49 | `<code>internal/foo.go</code>` | ❌ 不处理 |
| 自由 prose 里裸路径 (无 link / 无 code) | 9 (全部在 ```text 围栏代码块里) | `<code>` block | ❌ 不处理 |

结论：所有真实出现的"可点击需求"都对应 markdown link 形态，本 spec 处理完即可覆盖 ~95%（58/(58+49)）的可点击意图。Inline code path 的剩 49 次留作未来扩展（见 §未来工作）。

## 目标

- 接管 `<a>` 渲染，按 href 形态识别 URL / 本地文件，分别走不同点击行为。
- 默认态在 link 文本旁加**类型 icon 后缀**（file-text / folder / external-link），不需 hover 就能区分。
- Hover ≥ 200ms 浮出 popover，展示完整 URL / 路径、复制按钮、类型 badge；cwd 内的本地文件额外展示 **项目根 + 相对路径分段**。
- 点击：URL 走 `BrowserOpenURL`，本地路径走新加的 `OpenPath(path)` Wails binding。

## 非目标

- 不做裸文本路径 auto-linkify。
- 不做 `file://` 协议特殊处理之外的 URL transform（保持 react-markdown 默认 sanitize，只放开 `file://` 与无协议绝对路径）。
- 不做 link preview（OG image / 网页 title 抓取）。
- 不做 Issue 跳转 / `@mention` 等业务级 link 类型。

## 识别规则

输入：原始 `href` 字符串、上下文 `cwd`（可选）。

```
classify(href, cwd) →
  | { type: 'url',           url: string }
  | { type: 'local-internal', fullPath: string, relPath: string, line?: number, col?: number }
  | { type: 'local-external', fullPath: string,                  line?: number, col?: number }
  | { type: 'unknown',       href: string }
```

判别顺序（self-contained，不读文件系统）：

1. `href` 匹配 `^(https?:|mailto:|tel:)` 或 `^www\.` → **url**
2. `href` 以 `file://` 开头 → 转换成 OS path（`fileURLToPath`），按本地路径处理
3. `href` 以 `/`（POSIX 绝对）或 `^[A-Za-z]:[\\/]`（Windows 绝对）开头 → **local**
4. 否则 → **unknown**（不增强，沿用现有 `<a>` 行为）

本地路径再剥离 `:line[:col]` 后缀（正则 `:(\d+)(?::(\d+))?$`），剩下的就是 `fullPath`。

`cwd` 内外判定：`cwd` 非空且 `fullPath.startsWith(cwd + '/')` → `local-internal`，`relPath = fullPath.slice(cwd.length + 1)`；否则 → `local-external`。

## UI 组件

### `RichLink`（新文件 `frontend/src/components/agentre/rich-link.tsx`）

替换 `markdownComponents.a`。Props：

```ts
type RichLinkProps = {
  href?: string;
  children: React.ReactNode;
  className?: string;
  cwd?: string;
};
```

渲染：

- 调 `classify(href, cwd)`，得到 `kind`。
- `kind === 'unknown'` → fallback 到原有 `<a target="_blank">`，不挂 popover，不挂 icon。
- 其他 → 渲染 `<a>` 文本（保持现样式：primary 色 + 下划线）+ 类型 icon（lucide）：
  - `url` → `external-link`
  - `local-internal` → `file-text`
  - `local-external` → `folder`
- 整个 `<a>` 包在 shadcn `HoverCard` 里（trigger delay 200ms）。
- `onClick`：阻止默认 navigate，按 `kind` dispatch：
  - `url` → `BrowserOpenURL(url)`
  - `local-*` → `OpenPath(fullPath + (line ? ':' + line + (col ? ':' + col : '') : ''))`

### Popover 内容

通用结构：`head`（badge + 复制按钮）+ body（路径/URL 展示）+ `hint`（"点击 ... 打开"）。

| kind | badge | body |
|---|---|---|
| `url` | "URL" (primary 色) | 完整 URL，monospace，可换行 |
| `local-internal` | "本地文件" (agent-2 色) + `L42` 行号 chip | 分段栏：「项目根: ~/Code/agentre/agentre」「相对: internal/foo.go」+ 完整路径（mono，small，muted） |
| `local-external` | "本地文件 · 项目外" (muted 色) + 行号 chip | 完整路径（mono） + 说明文字 "路径不在当前 cwd 之下，不展示项目根分段。" |

复制按钮：复制 `fullPath`（带行号后缀）/ 完整 URL，toast 反馈。

### 现有 `<a>` override 改造

`markdown-text.tsx:53` 的 `a` 实现替换为：

```ts
a: ({ node: _node, href, children, className }) => (
  <RichLink href={href} className={className} cwd={cwd}>
    {children}
  </RichLink>
),
```

`cwd` 通过 `MarkdownText` 新增的 prop 传入；`chat.tsx:1269` 传 `cwd` 给 `<MarkdownText>`。

## 后端 binding

新增 `internal/app/system.go`。参照 `internal/app/update.go:91` 的 `RestartApp` 模式，方法直接在 `App` 上挂 `exec.Command`，不引入新的 `system_svc`（这是单一系统操作，不需要 service 层抽象）。

```go
// OpenPath 用系统默认应用打开指定路径。
// path 可带 :line[:col] 后缀，会原样传给 OS handler（macOS open 会忽略
// 后缀，VS Code 等编辑器接受 file:line:col 形态由 url scheme 决定，
// 这里只做 file path 打开，行号不做特殊处理）。
func (a *App) OpenPath(ctx context.Context, path string) error {
  // macOS: open <path>
  // linux: xdg-open <path>
  // windows: cmd /c start "" <path>
}
```

- 路径校验：拒绝相对路径、拒绝 `..` 段（避免被构造逃逸出 cwd）。— **拒绝**只是为了 panic 早；不是安全边界，因为这是桌面 app，AI 输出已经在用户可见的 chat 里。
- 错误处理：返回 error，前端 toast。
- macOS 用 `exec.Command("open", path)`，去 `:line:col` 后缀（macOS open 不支持 line:col 语法，会报"file not found"）。

行号兼容：先按"截掉行号"的 MVP 做，未来需要"跳到 line:col"可以扩展成 "若用户配置了编辑器 URL scheme（如 `vscode://file/{path}:{line}`）就用 scheme，否则 fallback 到 open"。

## 测试清单 (TDD)

按 BDD `Given/When/Then` 写：

### `classify(href, cwd)` 纯函数测试（`rich-link.test.tsx` 或单独 `link-classify.test.ts`）

- Given href 是 `https://...` / `http://...` / `www....` → url
- Given href 是 `mailto:` / `tel:` → url
- Given href 是 `file:///Users/x/foo.go` → local-internal 或 external（按 cwd）
- Given href 是 `/Users/x/foo.go:42:7` → 解析出 line=42, col=7
- Given href 是 `/Users/x/foo.go:42` → line=42, col=undefined
- Given href 是 `C:\Users\x\foo.go` → local
- Given href 是 cwd 子路径 → local-internal，relPath 正确
- Given href 是 cwd 外绝对路径 → local-external
- Given href 是相对路径 `internal/foo.go` → unknown（不增强）
- Given href 空 → unknown

### `RichLink` 行为测试

- Hover 200ms 后 popover 出现，含 badge + 完整 URL/path + 复制按钮
- 点击 URL link → 调 `BrowserOpenURL`，不发生页面 navigation
- 点击本地路径 link → 调 `OpenPath`
- 点击复制按钮 → 写入 clipboard + toast
- `cwd` 未传时本地路径走 local-external 分支（无分段）
- `kind === 'unknown'` 时不渲染 icon / popover，保持纯 `<a>`

### `OpenPath` Go binding 测试

- Given valid local file path → exec.Command 用对应平台 cmd 调用，error nil
- Given path with `:42` suffix on darwin → 后缀被 strip 后 exec
- Given path 包含 `..` → 拒绝（返回 error）
- Given 相对路径 → 拒绝
- exec 失败 → 错误向上 propagate

`exec.Command` 用 `internal/pkg/exec` 之类的 wrapper 抽象（如果有），方便 stub。

## 未来工作（不在本 spec 范围）

- **编辑器跳转协议**：`OpenPath` 行号目前只 strip 后由 OS 默认 handler 打开（macOS `open` 不支持 line:col 语法）；之后可加 setting `defaultEditorScheme: 'vscode' | 'cursor' | 'idea' | 'system'`，按 scheme 拼 `vscode://file/{path}:{line}:{col}` / `cursor://file/...` / `idea://open?file=...&line=...`，让点击真正跳到指定行。

## 风险 / Open Questions

1. **react-markdown urlTransform** 默认会 strip `file://`。需要在 `ReactMarkdown` 上加 `urlTransform={(url) => url}` 关掉 sanitization，**但这会让 `javascript:` 等危险协议也被透传**。Mitigation：`classify()` 里只对已知安全的几种 prefix 增强，其余 fallback 到原有 `<a>` —— 但原有 `<a>` 拿到 `javascript:` 仍然危险。所以我们的 `urlTransform` 要自己实现：放过 `http/https/mailto/tel/file/^/^[A-Za-z]:`，其他统一 strip。这个 transform 应该和 `classify()` 共用 prefix 表。
2. **行号后缀的 `:` 在 Windows 盘符里的歧义** —— `C:\foo.go:42` 第一次 `:` 是盘符，第二次才是行号。正则要从末尾匹配，且只匹配 `:\d+` 形态。已经在 `classify` 里靠"末尾 anchor"正则规避。
3. **复制按钮的可达性** —— popover 在 hover 离开 link 后会消失。需要 `HoverCard` 的 trigger delay = 200ms / closeDelay > 0，让用户能从 link 滑到 popover 里。shadcn `HoverCard` 默认 closeDelay 300ms，够用。
## 关键文件

- `frontend/src/components/agentre/markdown-text.tsx` — `a` override 改成 `RichLink`，新增 `cwd` prop
- `frontend/src/components/agentre/rich-link.tsx` — 新增
- `frontend/src/components/agentre/rich-link.test.tsx` — 新增（BDD）
- `frontend/src/lib/link-classify.ts` — 新增纯函数（便于跨地方复用 + 独立测）
- `frontend/src/lib/link-classify.test.ts` — 新增
- `frontend/src/components/agentre/chat.tsx:1269` — 给 `<MarkdownText>` 传 `cwd`
- `internal/app/system.go`（新增，参考 `update.go` 模式） — `OpenPath(path string) error`
- `internal/app/system_test.go` — `OpenPath` 单测（exec wrapper stub）
- `migrations/` — **不动**（无 schema 变更）
