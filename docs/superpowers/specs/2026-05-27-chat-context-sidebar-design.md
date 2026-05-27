# Chat Context Sidebar Design

**Date:** 2026-05-27
**Status:** Draft, awaiting user review
**Mockup:** `agentry.pen` → frame `Agent Chat — With Right Sidebar` (id `MT2Dq`)

## Goal

在 `ChatPanel` 右侧增加一个可收起的上下文侧栏，承担两件事：

1. **消息概览（Outline）** —— 以 user prompt 为粒度的可点击索引，长会话里能快速回头。
2. **会话上下文 chip** —— 把当前会话挂在哪个 git branch、worktree、有没有脏改动这些信息以只读 chip 形式暴露出来，先把"多 agent 同时改一棵代码树"的踩坑场景照亮一点；预留扩展位给后续 view tab。

非目标：消息缩略地图（minimap）、LLM 自动摘要、Commands / Activity 视图 —— MVP 全部不做。

## Architecture

```
ChatPanel
├── toolbar
│     └── (NEW) Button[PanelRight] — toggle sidebar
├── MainRow (NEW, layout=horizontal, fill height)
│   ├── Transcript (existing <section>, flex 1)
│   └── ChatContextSidebar (NEW, 320px fixed)
│       ├── ContextChipBar (always visible)
│       │   ├── Row 1: branchChip · dirtyChip · syncChip(↑/↓)
│       │   └── Row 2: worktreeRow (folder-tree icon + 简短路径)
│       ├── TabBar (Outline | Files)
│       └── ViewBody (active view 内容)
└── composer (unchanged, full width — sidebar 不挤它)
```

### Component boundaries

- `chat-panel.tsx` 改造：把现有 `<section ref={transcriptRef}>` 包进新的 `MainRow` 容器，旁边塞 `<ChatContextSidebar />`。`ChatComposer` 保持在外层不动。
- 新组件 `chat-context-sidebar/index.tsx` 只接 `sessionId` + `cwd` 一对入参，内部读 store。这样它不依赖 ChatPanel 的 `messages` state，可独立测。
- 子视图独立文件：`chat-context-sidebar/views/outline-view.tsx`、`files-view.tsx`，避免 sidebar 主文件超长。
- 新 hook `use-chat-git-state(sessionId)` 封装 RPC + 刷新策略，sidebar 通过它拉 chip 数据。

### Files / directory layout

```
frontend/src/components/agentre/chat-context-sidebar/
├── index.tsx                   # ChatContextSidebar 容器
├── context-chip-bar.tsx        # 三个 chip + worktree 行
├── tab-bar.tsx
├── views/
│   ├── outline-view.tsx        # MVP 默认视图
│   └── files-view.tsx          # MVP 第二视图
└── __tests__/
    ├── context-chip-bar.test.tsx
    ├── outline-view.test.tsx
    └── files-view.test.tsx

frontend/src/hooks/
└── use-chat-git-state.ts       # 拉 git RPC + 跟 doneTick 刷新

frontend/src/stores/
└── chat-sidebar-store.ts       # toggle 开关 + 当前 tab 持久化到 localStorage

internal/service/chat_svc/
└── git_state.go                # 新 RPC: GetSessionGitState(sessionId)
```

## Data Flow

### Outline view

**数据源：** 完全在前端从 `messages[]` 派生，不动 schema、不加 RPC。和 `task-progress/derive.ts` 同构。

```ts
function deriveOutline(messages: SvcChatMessage[]): OutlineItem[] {
  // 1. role=user 的 message → 一项
  // 2. 取 textOfChatMessage(message) 做 preview
  // 3. 该 user message 之后到下一个 user message 之前的 assistant blocks 里：
  //    - 数 Edit/Write tool 调用 → edits
  //    - 有 errorText 或失败 tool result → err = true
  // 4. createtime 转 HH:MM；轮次按数组下标
}
```

唯一前端 derive 的代价：滚动定位依赖 message id 锚点。Transcript 里给每条 user message 渲染一个 `data-message-id={id}` 即可，sidebar 点击直接 `el.scrollIntoView`。

### Files view

**数据源：** 同样前端 derive，扫一遍 `messages[].blocks`，找 tool_use block 的 input 里 `file_path`（claudecode）或 `path`（codex / builtin），按文件路径分组：

```ts
type FileEntry = {
  path: string;              // 相对 cwd 的短路径，cwd 由 session 提供
  edits: number;             // Edit + Write + MultiEdit
  reads: number;             // Read（次要展示）
  lastTurn: number;          // 最近一次出现是第几轮 → 点击跳到那条 user message
};
```

跨 backend 适配在前端做一张映射表 `tool-file-extractor.ts`，covers 三家工具的 input shape。新 backend 加入时只动这张表，不动 sidebar。

### Git state chips

**新增 RPC** `GetSessionGitState(sessionId int64) → ChatSessionGitState`：

```go
type ChatSessionGitState struct {
    Branch       string `json:"branch"`        // ai-chat
    Worktree     string `json:"worktree"`      // 空串=主 git dir；否则 wt 短名
    Dirty        int    `json:"dirty"`         // uncommitted 文件数
    Ahead        int    `json:"ahead"`         // 与 upstream 的领先 commit 数
    Behind       int    `json:"behind"`        // 落后 commit 数
    HasUpstream  bool   `json:"hasUpstream"`   // false = 没 upstream，ahead/behind 不渲染
    NotARepo     bool   `json:"notARepo"`      // true = cwd 不在 git 仓库里，整个 chip 区折叠
    UpdatedAt    int64  `json:"updatedAt"`     // server-side timestamp
}
```

**实现位置：**
- 服务方法：`internal/service/chat_svc/git_state.go` 实现 `func (s *chat) GetSessionGitState(ctx, req)`，复用 `resolveSessionCwd` 拿 cwd。
- Wails 绑定：`internal/app/chat.go` 加 `func (a *App) GetSessionGitState(req) (*resp, error) { return chat_svc.Chat().GetSessionGitState(a.ctx, req) }`，跟现有 `GetChatLaunchCommand` 同型。
- `make generate` 自动 refresh `frontend/wailsjs/go/app/App.js|d.ts`。

**远端会话：** 复用 `resolveSessionCwd` 已有的路由 —— 本地 backend 直接 `os/exec` 跑 `git`；远端 backend 通过 `agentred` JSON-RPC 调用（在 `internal/daemon/` 加一个 handler `git.state`，参数 `{ cwd }`，返回同样 shape）。**MVP 阶段：远端先返回空结果 + 标记 `notARepo=true`，后续 PR 再补 daemon handler**，避免一次性扩散到 daemon 协议。

**git 命令：** 一条复合命令拿全部数据，避免多次 fork：

```
git rev-parse --abbrev-ref HEAD                       # branch
git rev-parse --git-common-dir / --git-dir            # diff 判定 worktree
git status --porcelain=v1 | wc -l                     # dirty
git rev-list --left-right --count @{u}...HEAD         # ahead/behind (有 upstream 时)
```

封装在一个 helper `runGitState(ctx, cwd)`，错误归一成 `notARepo=true`（exit code 128 / "not a git repository"）。

### Refresh strategy

**触发点：**
1. Session 首次加载 / sessionId 变化 → 拉一次
2. 每个 turn 结束（`doneTick` 自增）→ 拉一次
3. 手动按钮（Sidebar 顶部一个小 refresh 图标）→ 立即拉

**不做：** 后台轮询、文件系统 watcher。三种触发已经覆盖"边干活边脏 chip 变化"的可观察场景。

实现：`use-chat-git-state(sessionId)` 内部 `useEffect` 监听 `[sessionId, doneTick]`，外加暴露一个 `refresh()`。

### Sidebar toggle persistence

新 zustand store `chat-sidebar-store.ts`：

```ts
type ChatSidebarStore = {
  open: boolean;             // 默认 true
  activeTab: "outline" | "files";  // 默认 "outline"
  setOpen(open: boolean): void;
  setActiveTab(tab: "outline" | "files"): void;
};
```

中间件 `persist`（zustand 自带）落到 `localStorage` key `chat-sidebar-state`。**全局共享**，不 per-session。所有 ChatPanel 实例订阅同一份。

## UI States

| 状态 | 渲染 |
|---|---|
| `open=false` | sidebar 完全不渲染（不是 `display:none`，省渲染开销）；toolbar 按钮以 muted 色显示 |
| `open=true`，无 session | sidebar 框架在，但 view body 显示 "尚未选择会话" |
| `notARepo=true` | ContextChipBar **整段折叠**，只剩 TabBar + ViewBody（cwd 不是 git repo 时） |
| `hasUpstream=false` | branchChip 正常渲染，syncChip 不渲染；dirtyChip 仍按 dirty 数渲染 |
| `dirty=0` | dirtyChip 不渲染，避免噪声 |
| git RPC 报错 | chip 区显示 "git 状态读取失败"，附 retry 按钮，不让整个 sidebar 挂掉 |
| messages=[] | Outline view 显示 "本会话还没有消息" |

## Error Handling

- Git RPC 失败：吞掉，只在 sidebar chip 区内显示小 inline error + retry。不弹 `notice`、不冒泡到 ChatPanel。
- 远端 backend 暂未实现的 daemon handler：返回 `{ notARepo: true }` 而不是 error，让 sidebar 自然折叠。
- Tab 切换状态来自 store，没有 RPC，无需 error 处理。
- Outline / Files derive 是纯函数，输入空也只是返回空数组。

## Testing

**Backend (Go, goconvey):**
- `git_state_test.go` —— 用 `t.TempDir()` 在测试里建临时 git repo，覆盖：
  - happy path：branch + dirty + ahead/behind
  - 无 upstream：`HasUpstream=false`
  - cwd 不是 git repo：`NotARepo=true`
  - worktree：`git worktree add` 后断言 `Worktree` 非空

**Frontend (Vitest):**
- `outline-view.test.tsx` —— given messages with 3 user prompts and 2 edits + 1 errored tool result, then outline 渲染 3 行带正确 badge
- `files-view.test.tsx` —— given messages with Edit/Write/Read tool calls cross multiple files, then files 聚合按 edits 倒序、lastTurn 正确
- `context-chip-bar.test.tsx` —— mock RPC 返回不同 git state，断言 chip 渲染（dirty=0 时不渲染等条件）
- `chat-sidebar-store.test.ts` —— toggle + activeTab 写入 localStorage、reload 后还原

**End-to-end smoke:** 在 dev 模式打开一个 session，点 toolbar 按钮 toggle，切 tab，点 outline 项跳转。手动 verify，不写自动化。

## Open questions（写完 spec 前自查）

无。三个工程决策（Files 前端 derive / git 刷新策略 / 全局 toggle 持久化）已确认。

## Out of scope

下面这些列在这里是为了**不要在 MVP 顺手做**：

- 拖拽调整 sidebar 宽度
- 缩略地图 / minimap 视图
- LLM 自动摘要视图
- Commands / Activity 视图
- 标记单条 outline 为"已收藏"
- 在 outline 行上 hover 显示完整 assistant 回复 preview
- 跨 backend 的远端 git RPC（daemon handler 在 follow-up）
- per-session 或 per-project 的 sidebar 状态
