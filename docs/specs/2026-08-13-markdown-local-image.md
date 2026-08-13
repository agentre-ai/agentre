# 聊天转录 markdown 本地图片渲染

> Status: Draft
> Owner: chat experience / frontend
> Last updated: 2026-08-13

**Objective:** 让聊天转录里的 markdown 引用的本地图片（相对 / 绝对 / file:// 路径，落在会话工作目录内）以内联图片渲染出来，复用会话级文件读取；同时**不改变链接白名单的安全边界**。

**Hard invariants:**

1. **链接白名单逐字节不回归。** `href`（链接）的放行规则与今天完全一致：相对 href 仍被剥掉、`javascript:` / `data:` 仍被剥掉。本次只放宽图片 `src`，且必须由 `img` 组件兜住，原始相对 `src` 不得落到 DOM 的 `<img src>` 上。
2. **自动读取只发生在「本地路径 + 有会话上下文 + 解析后落在 cwd 内 + 图片扩展名 allowlist」四条件同时满足时。** 前端分类只是 UX 预判，真正的边界由后端 `WorkspaceFsReadFile → workspacefs.ReadFile` 强制（cwd 内 + 符号链接防逃逸 + 图片扩展名 allowlist + 10 MiB 上限），前端不可绕过。
3. **本地图片的「自动读取」与「点击打开」共用同一条 cwd 边界**：落在 cwd 内才允许读、才允许点；越出 cwd 或无法解析（无 cwd + 相对路径）一律不读、不可点。
4. **无会话上下文的调用点行为不变。** 文件预览面板的 markdown 档、计划卡等不传 `sessionId` 的 `MarkdownText` 用法，本地图片仍不渲染（与今天一致）。
5. **远程图片现状不变。** `http(s)` / `www.` 图片直接渲染 `<img>`；`data:` 图片维持现状不放开。
6. **后端 / wire / 迁移零改动。** 本轮是纯前端改动，复用既有 `WorkspaceFsReadFile` 绑定。
7. **新增 UI 文案走 i18n**，`zh-CN` 与 `en` 双语，由 `frontend/src/__tests__/i18n.test.ts` 守卫；新增渲染组件并入对话流排版护栏的扫描清单。

## Problem

1. **相对路径图片被 URL 白名单剥掉 `src`，导致不渲染。** `frontend/src/components/agentre/markdown-text.tsx:125-143` 的 `whitelistUrl` 只放行 `http/https/mailto/tel/file/www/绝对 POSIX/绝对 Windows`，相对路径返回 `""`；该函数作为 `urlTransform={whitelistUrl}`（`markdown-text.tsx:348`）传给 react-markdown，而 react-markdown v10 会把 `urlTransform` 应用到 `img` 的 `src`（`html-url-attributes` 的 `urlAttributes.src` 包含 `img`）。
2. **绝对路径 / file:// 图片虽不被剥 `src`，但没有 `img` 渲染组件。** `markdownComponentsStatic` 只定义了 `a`（交给 `RichLink`），没有 `img`，react-markdown 默认渲染 `<img src="/abs/...">`，在 Wails webview 里无法加载本地文件系统路径。
3. **真实证据：** 会话 sess-2893（`projects.id=2`，cwd=`/Users/codfrm/Code/agentre/agentre`）的 assistant 消息 21064 末段 text block 里含 `![会话右侧文件语言图标 Mockup](.dev-kit/artifacts/2026-08-13-session-file-icons/mockups/Y0cnD.png)`，该文件确实存在，但前端不显示图片。

## Actors and user stories

1. 作为**查看 agent 生成的本地 mockup / 截图引用的用户**，我要在聊天里直接看到图片，而不是一个失效的图片占位。
2. 作为**agent**，我要能用 markdown 相对路径引用会话工作目录里的图片，并让用户直接看到结果。
3. 作为**读旧转录的用户**，我要本地图片的显示行为稳定可预期：能显示的显示、不能显示的有明确的「无法预览」兜底，而不是静默消失。

## Design decisions

| # | Decision | Basis and rejected option |
|---|---|---|
| 1 | 复用既有 `WorkspaceFsReadFile(sessionId, relPath)` 读取本地图片，转成 `data:${contentType};base64,${content}` 渲染 | 后端已具备完整边界：`workspacefs.ReadFile`（`internal/pkg/workspacefs/readfile.go`）强制 cwd 内 + 符号链接防逃逸 + 图片扩展名 allowlist + 10 MiB 上限；本机 / 远端会话走同一绑定（`App.d.ts:387`），文件预览面板已用它渲染图片（`file-preview-panel.tsx:401`）。Rejected: 直接 `file://` 当 `src` —— webview 加载不了本地路径，且丢失 cwd 边界与远端会话能力；Rejected: 前端新建文件读取绑定 —— 违背「Wails 绑定只做 parse→svc→return」的既有分层 |
| 2 | 范围仅限聊天转录的 markdown 图片（`sessionId` 存在时）；文件预览 markdown 档、计划卡等不传 `sessionId` 的调用点行为不变 | 本次 bug 在聊天转录。文件预览的 markdown 有自己不同的解析上下文（相对其所在 `.md` 的目录），一并修会扩大范围。Rejected: 顺带修文件预览 markdown 的图片 —— 解析基准不同，是另一个 feature |
| 3 | 图片按 src 分成四档：`remote`（http/https/www，原样 `<img>`）、`plain`（空 / data: / javascript: / 其它协议，或本地路径但无会话上下文，原样 `<img>` 且 src 剥空）、`fetch`（本地路径 + 有 sessionId + 落在 cwd 内 + 图片扩展名，读取渲染）、`fallback`（本地路径 + 有 sessionId 但越出 cwd / 非图片扩展名 / 无法解析，渲染兜底 chip） | 用户已裁定失败兜底为 chip（选项 A）。分档让「维持现状」与「新增能力」边界清晰。Rejected: 只做 fetch / 其它，不做 fallback —— 失败会静默消失，违背 user story 3 |
| 4 | `whitelistUrl` 变为按 `key` 区分：仅当 `key === "src"` 时放行相对路径，`href` 分支逐字节不变 | react-markdown 在渲染组件前就地把 `urlTransform` 结果写进 `node.properties.src`，组件拿不到原始相对 src；唯一入口是让 `urlTransform` 对 src 放行。Rejected: 全局放行相对路径 —— 会连带放宽链接（href）并改变其安全边界（Hard invariant 1） |
| 5 | 图片扩展名 allowlist 复用后端 / 前端已对齐的同一份（`previewKind(path) === "image"`） | 前端 `previewable.ts:107` 的 `previewKind` 与后端 `readfile.go:116` 的 `imageMIME` 是同一份 allowlist（png/jpg/jpeg/gif/webp/avif/bmp/ico/svg），单一事实源，避免前端预判与后端判定分叉 |
| 6 | 本地图片的读取与点击打开共用 cwd 边界：落在 cwd 内才读、才可点；越出 cwd / 无法解析则不可点 | 与「变动」模式预览按钮的既有边界一致（`previewable.ts:154` `resolvePreviewRelPath`：绝对路径越出 cwd 一律不出预览）。Rejected: 越出 cwd 的绝对路径也可点击打开 —— 用户裁定「越界时不可点」 |

## 本地图片的分类与渲染

markdown 中每个图片，按 `src`（`alt` 作回退文本）分成四档，可观察行为如下：

- **remote**（`http://` / `https://` / `www.`）：原样渲染 `<img src="…" alt="…">`，与今天完全一致。
- **plain**（`src` 为空，或 `data:` / `javascript:` 等非本地、非 http 协议，或**本地路径但无会话上下文**）：原样渲染 `<img>`，其中本地相对路径的 `src` 保持剥空——即文件预览 markdown 档、计划卡等处的本地图片仍不渲染，与今天一致。
- **fetch**（本地路径 + 有 `sessionId` + 解析后落在 cwd 内 + 扩展名是图片 allowlist）：调 `WorkspaceFsReadFile(sessionId, relPath)`，成功后渲染 `<img src={`data:${contentType};base64,${content}`} alt="…">`。
- **fallback**（本地路径 + 有 `sessionId`，但越出 cwd / 非图片扩展名 / 无 cwd 无法解析）：渲染兜底 chip（见下）。

本地路径的识别与解析复用既有规则：`file://` 还原为路径、绝对 POSIX / 绝对 Windows 直接用、相对路径经 cwd 解析（`toRelPath`，`previewable.ts:129`）。非 ASCII / 空格经 markdown 百分号编码时先解码（与 `link-classify.ts` 的 `decodeLocalPath` 同源）。

`fetch` 档的读取中显示 muted 占位（不闪动布局）；读取结果 `tooLarge` 或 `binary` 或读取失败时，不渲染图片，改渲染兜底 chip。

## 失败兜底 chip

不可预览的本地图片渲染为内联 chip，形态对齐现有文件引用 chip：`[文件图标] 文件名 · 无法预览`；`tooLarge` 时状态文本为「图片过大」。可观察行为：

- **可点**：当且仅当路径解析后落在会话 cwd 内（相对路径经 cwd 解析、绝对路径已在 cwd 内、file:// 已在 cwd 内）。点击调 `OpenPath(绝对路径)` 用默认应用打开文件。
- **不可点**：无 cwd 的相对路径，或绝对路径越出 cwd。chip 退化为纯文本，不响应点击。

「无法预览」与「图片过大」两种原因对用户可区分；文件名来自 src 的 basename，是动态内容，不进 i18n。

## 安全边界

自动读取仅发生在 `fetch` 档。前端分类（`previewKind` + `toRelPath`）只决定「读不读」与「显示哪种兜底」，是 UX 预判；真正不可绕过的边界是后端 `workspacefs.ReadFile`：cwd 内校验（含 `..` / 绝对路径 / 符号链接跟随后的重校验，拒绝为 `ErrPathRefused`）、图片扩展名 allowlist、图片 10 MiB 上限。前端任何路径伪造都会被后端拦下。

`href` 白名单在本轮不变：相对链接、`javascript:`、`data:` 的既有处理原样保留。图片 `src` 的放行只发生在 `urlTransform` 的 `src` 分支，且 `img` 组件恒被注册，原始相对 `src` 不会落到 DOM `<img>` 元素上。

## 可访问性

兜底 chip 可点时是原生 `button`，可 Tab 聚焦、带 `focus-visible` 的既有 ring 样式；不可点时是纯文本，不提供误导性焦点。渲染出的图片保留 `alt`（来自 markdown 图片的 alt 文本）。占位与状态颜色不承载唯一语义（状态有文字冗余）。

## Out of scope

- **`data:` 图片**：维持现状不放开。
- **http/https 图片**：维持现状直接渲染，不做点击打开、不下载、不代理。
- **相对链接（href）**：维持现状（被剥掉）。
- **文件预览面板 markdown 档、计划卡里的图片**：维持现状（不传 `sessionId`，本地图片不渲染）。
- **点击放大 / 灯箱 / 下载**：渲染出的图片不做交互。
- **后端 / wire / 迁移**：零改动。

## Testing decisions

| Seam | What it verifies | Prior art |
|---|---|---|
| 图片分类纯函数（四档判定） | remote / plain / fetch / fallback 各档命中；相对路径 + cwd + 图片扩展名 → fetch；相对路径 + 非图片扩展名 → fallback（可点）；绝对路径越出 cwd → fallback（不可点）；无 cwd + 相对路径 → fallback（不可点）；`data:` / `javascript:` / 空 → plain；无 sessionId → plain；file:// 还原；非 ASCII 解码 | 无（新增）；解析规则与 `link-classify.test.ts` 同源，可参照 |
| `whitelistUrl` 回归 | href 相对路径仍剥掉、`javascript:` 仍剥掉、`https` 保留；src 相对路径放行、src `data:` 仍剥掉 | `markdown-text.test.tsx` 的「URL whitelist」describe |
| `MarkdownText` 组件测试 | 本地图片（mock `WorkspaceFsReadFile`）渲染为 data URL `<img>`；读取失败 / tooLarge / binary 渲染兜底 chip；remote 图片原样 `<img>`；chip 可点 / 不可点与 openPath 一致 | `markdown-text.test.tsx` |
| i18n 守卫 | 新增「无法预览」「图片过大」zh-CN / en 双语齐全，无硬编码中文 | `frontend/src/__tests__/i18n.test.ts` |
| 排版守卫 | 新增组件进入 `transcript-typography-guard.test.ts` 的 SCANNED 清单，不引入被 token 取代的字面量 | `frontend/src/components/agentre/__tests__/transcript-typography-guard.test.ts` |

**不适合自动化的部分：** chip 在对话流里的视觉协调、以及真实会话（sess-2893）里 mockup 图片确实渲染出来，由收尾时按 `docs/verification.md` 驱动真实应用观察并留证据；`WorkspaceFsReadFile` 的远端会话路径由既有的后端测试覆盖，本轮不新增。

## Links

- 根因证据（只读排查，非本轮产物）：sess-2893 消息 21064（本地 `agentre.db`，`chat_messages.blocks_json`）
- 图片读取实现：`internal/pkg/workspacefs/readfile.go`
- 会话级文件读取绑定：`frontend/wailsjs/go/app/App.d.ts` 的 `WorkspaceFsReadFile`
- 文件预览图片渲染参考：`frontend/src/components/agentre/file-preview/file-preview-panel.tsx`
- 路径判定参考：`frontend/src/lib/link-classify.ts`、`frontend/src/components/agentre/chat-context-sidebar/previewable.ts`
- 前端硬规则：`docs/frontend.md`（shadcn / i18n / lint）
