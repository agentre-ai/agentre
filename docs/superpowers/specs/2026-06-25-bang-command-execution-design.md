# 对话发送框 `!` 本地命令执行 设计

- 日期:2026-06-25
- 范围:仅 `agentre/`(桌面端 Wails 应用)
- 状态:已批准设计,待用户复核 → 转实施计划

## 背景与目标

AI 对话发送框(`AIChatInput` / `ChatComposer`)目前只能给 agent 发消息。希望对齐 **Claude Code 自身的 `!` bash 模式**:当**第一个字符是 `!`** 时,发送框切换为「命令模式」——直接在**当前会话的项目工作目录**里执行一条 shell 命令,而**不经过 AI**。发送框需有明显提示。

这是一个**纯本地便捷工具**:命令与输出只展示给用户,**不进入 agent 上下文**;因为 `!` 完全绕开 claudecode/codex 会话,AI 天然看不到,无需额外过滤。

## 决策(已与用户确认)

| 维度 | 决策 |
|---|---|
| 触发 | 发送框**首字符** `!` → 命令模式;删掉 `!` → 退回对话模式 |
| 与 AI 关系 | **私有本地工具**:不喂给 agent、不进上下文 |
| 长任务/交互 | **Stream + Stop + Open-in-terminal**:走 PTY,输出实时流入内联卡片;可停止;可「在终端中打开」接管同一进程拿到完整 stdin/颜色/回滚 |
| 持久化 | **临时(仅前端)**:本地命令记录存在前端 store,刷新可见、整应用重启后消失;**不落库、无迁移** |
| 结果落点 | **内联**进 transcript,视觉上明确区分于 AI 工具卡(标注「本地命令 · 不发送给 AI」) |

## 设计原则

**复用已有 PTY / 终端基建,几乎零新增基础设施**(OCP 扩展,不重写):

- `internal/pkg/pty/local` 已用 `creack/pty` 跑 `exec.Command(shell, "-l")`,有流式输出、resize、`SIGHUP→SIGKILL` 优雅杀进程。
- `terminal_svc` 已有输出合并(10ms / 32KB 聚合)、base64 编码、`DataEventName(id)` / `ExitEventName(id)`(退出事件已带 exit code)。
- `ResolveSessionCwd(ctx, session, backend)` 已能解析项目会话 / 自由会话 / 远端设备的工作目录。
- 终端已是**一等 tab**(`chat-tabs-store` 的 `kind:"terminal"`,前端生成 `terminalId`,`TerminalOpen/Write/Resize/Close` 绑定,xterm 订阅 data/exit 事件)。

「Open in terminal」= 用**同一个 `terminalId`** 开一个 terminal tab,与内联卡片**共享同一个 PTY**。

## A. 发送框命令模式(前端)

文件:`frontend/src/components/agentre/chat-input/index.tsx`(TipTap `AIChatInput`)、`chat.tsx`(`ChatComposer`)。

- **检测**:取编辑器纯文本(`extractPlainText`,见 `chat-input/content.ts`),首字符为 `!` ⇒ `commandMode = true`。该状态上抛给 `ChatComposer`(类似既有 `isEmpty`)。
- **视觉提示**:
  - 顶部模式横幅(复用编辑模式横幅 `chat.composer.editing.*` 的样式位):文案 `chat.composer.command.banner` —— 「命令模式 · 在项目目录执行 · Enter 运行 · 删除 ! 退出」。
  - 左侧彩色 accent + 终端 glyph 的发送按钮(图标/`aria-label` 切到 `chat.composer.command.run`)。
  - 命令模式下 placeholder 不变(已有内容),仅靠横幅+accent 区分。
- **退出**:删掉首 `!` ⇒ 自动回对话模式(状态随输入实时重算)。
- **`/` 斜杠菜单在命令模式下抑制**:`!` 仅在位置 0 触发,`/` 是词边界触发,二者互斥;命令模式时不再弹 slash 候选(`useSlashMenu` 接 `commandMode` 短路)。
- **提交**:命令模式下 Enter 走命令分支而非 `onSubmit(text)`;**剥掉首 `!`**,trim,空命令(只有 `!`)⇒ no-op、保持命令模式。
- 提交分支调用新回调 `onRunCommand(command: string)`,由 `ChatPanel` 实现(见 C/D)。

> 字面以 `!` 开头的对话消息属极边界,v1 不做转义(需要时先打空格或换措辞);留作 YAGNI。

## B. 后端 —— PTY 跑指定命令 + 会话级绑定

### B1. PTY 后端泛化(`internal/pkg/pty/local/local.go`)

`Open` 当前固定 `exec.Command(shell, "-l")`。泛化为可选 argv 覆盖:

- 默认仍是登录交互 shell `[$SHELL, -l]`(现有 terminal 行为不变,LSP 保持)。
- 命令模式传 `[$SHELL, -lc, "<command>"]`——**登录 shell `-l`** 以带上用户真实 `PATH`/环境(dev 命令依赖之),`-c` 跑具体命令。
- `pty.Backend.Open` 接口加可选 `Command []string`(或 `OpenOptions`),空则走默认。`remote` 后端同步透传。

### B2. `terminal_svc` 新增 `OpenCommand`(`internal/service/terminal_svc/service.go`)

与现有 `Open(ctx, terminalID, deviceID, cwd, cols, rows)` 平行,新增
`OpenCommand(ctx, terminalID, deviceID, cwd, command, cols, rows)`:把 `command` 作为 argv 传进 PTY 后端,**复用同一套 `pump` 流式聚合 / `DataEventName` / `ExitEventName` / `Close`**。退出事件已带 exit code,直接复用。

### B3. Wails 绑定 `TerminalRunCommand`(终端绑定文件,如 `internal/app/terminal.go`)

```
TerminalRunCommand(terminalID string, sessionID int64, command string, cols, rows int) error
```

- 按 sessionID 解析后端 + cwd:复用 `ResolveSessionCwd(ctx, session, backend)`(项目会话→项目路径;自由会话→agent 目录;远端→设备路径)。这与现有 `TerminalOpen` 由绑定层做 `projectId→cwd` 解析同构。
- 解析出 deviceID(本地/远端),调用 `terminal_svc.OpenCommand`。
- `terminalID` 由前端生成(与现有终端一致)。
- 停止复用现有 `TerminalClose(terminalID)`(`SIGHUP→SIGKILL`)。
- 远端会话因 `terminal_svc` 已按 device 选 `remote` PTY 后端而**天然支持**;v1 先验本地,远端列入复核项(下文「不做」)。

## C. 内联「本地命令」卡片 + 临时 store + transcript 合并

### C1. 临时 store(前端,不落库)

新增 `useLocalCommandsStore`(或并入 `chat-streams-store`),按 `sessionId` 维护有序条目:

```
LocalCommandEntry {
  id: string          // = terminalId(前端生成)
  command: string
  createdAt: number    // 客户端 Date.now(),作为 transcript 排序键
  status: "running" | "done" | "failed" | "stopped"
  exitCode?: number
  output: string       // 累积输出(ANSI 原文)
}
```

### C2. 卡片组件 `LocalCommandCard`

- 视觉**明确区别于 AI 工具卡**:终端 glyph 头部 + 「本地命令 · 不发送给 AI」标记(`localCommand.notSharedWithAI`),命令以等宽展示。
- 命令的发起单点收口在 `ChatPanel.onRunCommand`(生成 `terminalId`、push `running` 条目、调 `TerminalRunCommand`,见数据流);**卡片是纯渲染+订阅方**:按条目的 `terminalId` 订阅 `terminal data/exit` 事件(复用 `use-terminal.ts` 里 base64 解码逻辑)→ 把输出 append 进 store 条目。
- 渲染:**输出(ANSI 去码后的 tail,等宽可滚)** + 状态胶囊(运行中+计时 / 完成+退出码 / 失败 / 已停止)+ 动作:
  - **停止**(运行中):调 `TerminalClose(terminalId)` → 状态置 `stopped`。
  - **在终端中打开**(运行中):见 D。
  - 退出后保留最终输出;退出码非 0 ⇒ `failed`。
- 内联卡片只做**只读输出**;完整保真(颜色/stdin/回滚)交给「在终端中打开」。

> ANSI:内联卡片去码展示(轻量);真彩交互在终端 tab 的 xterm 里。

### C3. transcript 合并

transcript 由持久化消息渲染(`transcript-rows.ts` 纯函数 + 块级虚拟化)。本地命令条目**按 `createdAt` 归并**进消息行:运行时即「当下」,自然落在当前尾部;向上滚动保持其位置。`transcript-rows` 入参加上 `localCommands`,按时间戳与消息 `createdAt` 归并排序产出行。整应用重启后 store 清空 ⇒ 条目消失(符合「临时」)。

## D. 「在终端中打开」—— 接管同一 PTY(attach 模式)

文件:`frontend/src/stores/chat-tabs-store.ts`、`terminal/use-terminal.ts`、`chat-tabs/chat-panel-host.tsx`。

- `chat-tabs-store` 新增 `attachTerminal({ terminalId, title, sessionId })`:建一个 `kind:"terminal"` tab,`meta.terminalId = 既有 id`、`meta.attach = true`。
- 终端面板 attach 分支:**跳过 `TerminalOpen`**(PTY 已存在),先用 `useLocalCommandsStore` 里该 `terminalId` 的 `output` **seed 进 xterm**,再订阅 data/exit、放开 `Write`/`Resize`/`Close`;首次 `fit` 后发 `TerminalResize` 同步 PTY 尺寸。
- 卡片与终端 tab **同时订阅**同一组事件(幂等,二者都继续更新);用户在终端里可输入 stdin、看真彩、回滚。
- 「在终端中打开」**仅运行中有意义**;进程已退出后 PTY 已消失,卡片仅展示最终输出(不再提供接管)。

## i18n(zh-CN + en,均走 `t(...)`)

`frontend/src/i18n/locales/{zh-CN,en}/common.json` 新增:

- `chat.composer.command.banner`、`chat.composer.command.run`(发送按钮 `aria-label`/title)。
- `localCommand.notSharedWithAI`、`localCommand.status.{running,done,failed,stopped}`、`localCommand.exitCode`、`localCommand.stop`、`localCommand.openInTerminal`。

静态 key 与 locale 覆盖过 `frontend/src/__tests__/i18n.test.ts`;不翻译命令本身与终端动态输出。

## 数据流

```
用户输首字符 ! → ChatComposer.commandMode=true(横幅+accent+终端按钮,抑制 / 菜单)
  └─ Enter → 剥 ! → onRunCommand(command)
       └─ ChatPanel: 生成 terminalId,push LocalCommandEntry(running),TerminalRunCommand(terminalId, sessionId, command, cols, rows)
            ├─ 绑定层 ResolveSessionCwd → device+cwd → terminal_svc.OpenCommand → PTY 跑 $SHELL -lc command
            ├─ DataEventName(terminalId) 流 → LocalCommandCard append output(去码 tail 展示)
            └─ ExitEventName(terminalId) → status=done/failed(+exitCode)

停止:Stop → TerminalClose(terminalId) → SIGHUP→SIGKILL → exit 事件 → status=stopped
接管:Open in terminal → attachTerminal(terminalId) → terminal tab seed 既有 output + 订阅 + 放开 stdin/resize(共享同一 PTY)
重启:前端 store 清空 → 本地命令条目消失(临时)
```

## 测试(TDD:先红后绿)

### Go 单测
- `internal/pkg/pty/local`:argv 覆盖——默认 `[$SHELL,-l]`,命令模式 `[$SHELL,-lc,cmd]`;命令在指定 cwd 跑、输出可读、退出码正确、`Close` 能杀。
- `terminal_svc.OpenCommand`:流式输出聚合、`ExitEventName` 带退出码、`Close` 杀进程(沿用 terminal_svc 既有测试套路)。
- `TerminalRunCommand` 绑定:mock cwd resolver / 会话仓库,断言项目会话 / 自由会话解析出的 cwd 与 device 正确传入 `OpenCommand`。

### 前端 vitest
- 命令模式检测:首字符 `!` 进模式、删除退出、提交剥 `!` 与 trim、空命令 no-op、横幅渲染、`/` 菜单被抑制。
- `LocalCommandCard` 生命周期:模拟 data/exit 事件驱动 running→done/failed/stopped;Stop 调 `TerminalClose`;Open-in-terminal 调 `attachTerminal`(attach tab 带同 terminalId)。
- transcript 合并:本地条目按 `createdAt` 归并到正确位置。
- i18n 覆盖(`i18n.test.ts`)。

## 不做(YAGNI / 范围纪律)

- **不落库、无迁移**(临时 store);本地命令重启即消失。
- **不喂 AI**:不向 agent 上下文注入命令/输出。
- **远端会话**:`terminal_svc` 已按 device 选 `remote` PTY 后端,代码层天然覆盖;v1 先验本地,远端单独手验,不为其新增代码。
- **内联卡片不内嵌完整 xterm**:去码 tail 展示即可,真彩交互走「在终端中打开」。
- 不做 `!!` 字面转义、不做独立命令历史(复用/省略消息历史)。
- **不碰与本任务无关的文件**(工作区现有 e2e/前端零散改动不纳入本次提交)。
