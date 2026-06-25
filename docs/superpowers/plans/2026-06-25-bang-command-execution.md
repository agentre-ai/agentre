# `!command` 本地命令执行 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 AI 对话发送框中,首字符为 `!` 时切换为"命令模式",通过已有 PTY 在会话工作目录直接执行 shell 命令(不经过 AI),输出实时流入 transcript 内联卡片,可停止、可在终端中接管。

**Architecture:** 复用现有 `pty` / `terminal_svc` / 终端 tab 基建。后端给 `pty.Spec` 增加 `Command` 字段(空=登录交互 shell,非空=`$SHELL -l -c <command>`),`terminal_svc.OpenCommand` 透传,新 Wails 绑定 `TerminalRunCommand(terminalID, sessionID, command, cols, rows)` 经会话解析 cwd/device 后调用。前端:发送框检测首字符 `!` → 命令模式 UI;`ChatPanel.onRunCommand` 生成 `terminalId`、起命令、单点订阅 `terminal:<id>:data/exit` 写入临时 zustand store;卡片与"在终端中打开"的 xterm 都从该 store 渲染(单一 Wails 订阅,避免 `EventsOff` 互删)。命令完全独立于 agent turn(AI 流式时也能并发运行)。

**Tech Stack:** Go 1.26(`creack/pty`、gomock、testify)、React 19 / TypeScript / Vitest、TipTap、zustand、Wails v2 bindings、xterm.js。

## Global Constraints

- **TDD 严格 Red→Green→Refactor**;先写失败测试并确认按正确原因失败,再实现。
- **仓库单测铁律**:repository 用 sqlmock;service 单测用 mockgen + `RegisterXxx` 注入,不连库;PTY/terminal_svc 既有测试用 gomock 模拟 `PTYBackend`/`Handle`。
- **新可见 UI 文案必须走 i18n**:`react-i18next` 的 `t(...)` + `frontend/src/i18n/locales/{zh-CN,en}/common.json`;`i18next/no-literal-string` 禁止 JSX 硬编码中文;静态 key 过 `frontend/src/__tests__/i18n.test.ts`。
- **前端表单控件统一用 shadcn `@/components/ui/*`**;禁止原生 `<select>`。
- **不落库、无迁移**:本地命令记录仅存前端 zustand store(整应用重启即消失)。
- **不喂 AI**:`!` 命令绕开 agent,输出永不进 agent 上下文。
- **关键流程必须日志**:`logger.Ctx(ctx)`,消息前缀 `package.Method:`,动态值走 `zap.Xxx(...)`。
- **只改与本任务相关文件**;工作区现有无关脏改动不纳入提交。
- 后端 Go 命令(focused):`cd agentre && go test -race -run TestName ./path/...`。前端:`cd agentre/frontend && pnpm test -- path/to/file.test.tsx`。绑定刷新:`cd agentre && make generate`。
- gitmoji 提交;当前分支 `develop/wyz`。

---

## File Structure

**后端(`agentre/`):**
- `internal/pkg/pty/pty.go` — 给 `Spec` 加 `Command string`。
- `internal/pkg/pty/local/local.go` — `Open` 按 `spec.Command` 决定 argv。
- `internal/pkg/pty/remote/remote.go` — `Open` 透传 `Command`。
- `pkg/agentred/protocol/terminal.go` — `TerminalOpenParams` 加 `Command`。
- `internal/daemon/handlers/terminal.go` — `Open` 把 `p.Command` 灌进 `pty.Spec`。
- `internal/service/terminal_svc/service.go` — 抽出私有 `open(...)`,加 `OpenCommand(...)`。
- `internal/service/chat_svc/exec_target.go`(新)— `ResolveSessionExecTarget` + 纯函数 `execDeviceID`。
- `internal/app/terminal.go` — 新绑定 `TerminalRunCommand`。

**前端(`agentre/frontend/src/`):**
- `i18n/locales/{zh-CN,en}/common.json` — 新增 `chat.composer.command.*` 与 `localCommand.*`。
- `components/agentre/chat-input/index.tsx` + `types.ts` — 命令模式检测 + 命令提交 + 抑制 slash。
- `components/agentre/chat.tsx` — `ChatComposer` 命令模式横幅 + 运行按钮。
- `stores/local-commands-store.ts`(新)— 临时本地命令 store。
- `components/agentre/local-command/card.tsx`(新)— `LocalCommandCard`。
- `components/agentre/local-command/ansi.ts`(新)— 轻量 ANSI 去码。
- `components/agentre/chat-panel.tsx` — `onRunCommand`:起命令 + 单点订阅写 store。
- `components/agentre/transcript-rows.ts` — 归并本地命令条目成行。
- `stores/chat-tabs-store.ts` — `attachTerminal` + terminal meta 加 `attach`。
- `components/agentre/terminal/terminal-panel.tsx` + `chat-tabs/chat-panel-host.tsx` — attach 模式(从 store 渲染)。

---

## Task B1: `pty.Spec.Command` + 本地后端按命令启动

**Files:**
- Modify: `internal/pkg/pty/pty.go:10-16`
- Modify: `internal/pkg/pty/local/local.go:28-61`
- Test: `internal/pkg/pty/local/local_test.go`

**Interfaces:**
- Produces: `pty.Spec.Command string`(空=登录交互 shell;非空=`$SHELL -l -c <command>`)。

- [ ] **Step 1: 写失败测试** — 追加到 `internal/pkg/pty/local/local_test.go`(镜像既有 `TestLocalBackend_OpenEchoRoundTrip` 风格):

```go
func TestLocalBackend_OpenCommand_RunsAndExits(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	be := local.NewBackend()
	h, err := be.Open(ctx, pty.Spec{
		Cwd: os.TempDir(), Shell: "/bin/sh",
		Command: "echo cmd-mode-ok", Cols: 80, Rows: 24,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Close() })

	deadline := time.After(3 * time.Second)
	var buf bytes.Buffer
	for {
		select {
		case chunk, ok := <-h.Data():
			buf.Write(chunk)
			if bytes.Contains(buf.Bytes(), []byte("cmd-mode-ok")) {
				goto awaitExit
			}
			if !ok {
				t.Fatalf("data closed before output; got %q", buf.String())
			}
		case <-deadline:
			t.Fatalf("timeout; got %q", buf.String())
		}
	}
awaitExit:
	select {
	case info := <-h.Exit():
		require.Equal(t, 0, info.Code)
	case <-time.After(3 * time.Second):
		t.Fatal("command did not exit")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd agentre && go test -race -run TestLocalBackend_OpenCommand_RunsAndExits ./internal/pkg/pty/local/`
Expected: FAIL — `unknown field 'Command' in struct literal`(编译失败)。

- [ ] **Step 3: 加 `Command` 字段** — `internal/pkg/pty/pty.go`,把 `Spec` 改为:

```go
type Spec struct {
	Cwd     string
	Shell   string   // empty → backend decides (typically $SHELL else /bin/sh)
	Command string   // empty → interactive login shell; non-empty → run `$SHELL -l -c <Command>`
	Env     []string // appended to base env; "TERM=xterm-256color" is injected by backend
	Cols    uint16
	Rows    uint16
}
```

- [ ] **Step 4: 本地后端按命令启动** — `internal/pkg/pty/local/local.go`,把构造 `cmd` 那行替换为按 `spec.Command` 选 argv:

```go
	args := []string{"-l"}
	if spec.Command != "" {
		args = append(args, "-c", spec.Command)
	}
	cmd := exec.Command(shell, args...) //nolint:gosec // G204: shell from spec/$SHELL; command is the user's own authorized local shell input
	cmd.Dir = spec.Cwd
	cmd.Env = append(os.Environ(), append(spec.Env, "TERM=xterm-256color")...)
```

- [ ] **Step 5: 跑测试确认通过**

Run: `cd agentre && go test -race -run 'TestLocalBackend_Open' ./internal/pkg/pty/local/`
Expected: PASS(新测试 + 既有 `TestLocalBackend_OpenEchoRoundTrip` 都绿)。

- [ ] **Step 6: 提交**

```bash
cd agentre && git add internal/pkg/pty/pty.go internal/pkg/pty/local/local.go internal/pkg/pty/local/local_test.go
git commit -m "✨ pty.Spec.Command:本地后端支持以命令启动 PTY(\$SHELL -l -c)"
```

---

## Task B2: 远端透传 `Command`(remote backend + protocol + daemon handler)

**Files:**
- Modify: `internal/pkg/pty/remote/remote.go:37-58`
- Modify: `pkg/agentred/protocol/terminal.go:4-17`
- Modify: `internal/daemon/handlers/terminal.go:69-83`
- Test: `internal/daemon/handlers/terminal_test.go`

**Interfaces:**
- Consumes: `pty.Spec.Command`(B1)。
- Produces: 远端 `terminal.open` 经 `protocol.TerminalOpenParams.Command` 把命令带到 daemon 端 `pty.Spec.Command`。

- [ ] **Step 1: 写失败测试** — 追加到 `internal/daemon/handlers/terminal_test.go`(镜像 `TestTerminal_Open_RegistersHandleAndReturnsID`),断言 daemon 把 `Command` 灌进 backend Spec:

```go
func TestTerminal_Open_ForwardsCommandToSpec(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mbe := mock_handlers.NewMockPTYBackend(ctrl)
	mh := mock_handlers.NewMockPTYHandle(ctrl)
	mh.EXPECT().Data().AnyTimes().Return(make(chan []byte))
	mh.EXPECT().Exit().AnyTimes().Return(make(chan pty.ExitInfo))
	mbe.EXPECT().
		Open(gomock.Any(), gomock.AssignableToTypeOf(pty.Spec{})).
		DoAndReturn(func(_ context.Context, spec pty.Spec) (handlers.PTYHandle, error) {
			assert.Equal(t, "go test ./...", spec.Command)
			return mh, nil
		})

	h := handlers.NewTerminalHandlers(mbe, &recordingEmitter{})
	res, err := h.Open(context.Background(), protocol.TerminalOpenParams{
		SessionID: 1, Cwd: "/tmp", Command: "go test ./...", Cols: 80, Rows: 24,
	})
	require.NoError(t, err)
	assert.NotEmpty(t, res.TerminalID)
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd agentre && go test -race -run TestTerminal_Open_ForwardsCommandToSpec ./internal/daemon/handlers/`
Expected: FAIL — `unknown field 'Command'` in `protocol.TerminalOpenParams`(编译失败)。

- [ ] **Step 3: protocol 加字段** — `pkg/agentred/protocol/terminal.go`,`TerminalOpenParams` 加 `Command`:

```go
type TerminalOpenParams struct {
	SessionID int64    `json:"sessionId"`
	Cwd       string   `json:"cwd"`
	Shell     string   `json:"shell,omitempty"`
	Command   string   `json:"command,omitempty"`
	Env       []string `json:"env,omitempty"`
	Cols      uint16   `json:"cols"`
	Rows      uint16   `json:"rows"`
}
```

- [ ] **Step 4: daemon handler 透传** — `internal/daemon/handlers/terminal.go` 的 `Open`,把 Spec 构造改为带 `Command`:

```go
	hd, err := h.be.Open(ctx, pty.Spec{
		Cwd: p.Cwd, Shell: p.Shell, Command: p.Command, Env: p.Env,
		Cols: p.Cols, Rows: p.Rows,
	})
```

- [ ] **Step 5: remote backend 透传** — `internal/pkg/pty/remote/remote.go` 的 `Open`,`TerminalOpenParams` 字面量加 `Command: spec.Command`:

```go
	if err := b.client.Call(openCtx, "terminal.open", protocol.TerminalOpenParams{
		Cwd: spec.Cwd, Shell: spec.Shell, Command: spec.Command, Env: spec.Env, Cols: spec.Cols, Rows: spec.Rows,
	}, &res); err != nil {
```

- [ ] **Step 6: 跑测试确认通过**

Run: `cd agentre && go test -race -run 'TestTerminal_Open' ./internal/daemon/handlers/ && go build ./internal/pkg/pty/remote/`
Expected: PASS;remote 包编译通过。

- [ ] **Step 7: 提交**

```bash
cd agentre && git add internal/pkg/pty/remote/remote.go pkg/agentred/protocol/terminal.go internal/daemon/handlers/terminal.go internal/daemon/handlers/terminal_test.go
git commit -m "✨ 远端 PTY 透传 Command:protocol + daemon handler + remote backend"
```

---

## Task B3: `terminal_svc.OpenCommand`

**Files:**
- Modify: `internal/service/terminal_svc/service.go:58-113`
- Test: `internal/service/terminal_svc/service_test.go`

**Interfaces:**
- Consumes: `pty.Spec.Command`(B1)。
- Produces: `(*Service).OpenCommand(ctx context.Context, terminalID, deviceID, cwd, command string, cols, rows uint16) error`。

- [ ] **Step 1: 写失败测试** — 追加到 `internal/service/terminal_svc/service_test.go`(镜像 `TestService_Open_Local_RegistersHandle`):

```go
func TestService_OpenCommand_PassesCommandToBackend(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockBE := mocks.NewMockPTYBackend(ctrl)
	mockH := mocks.NewMockHandle(ctrl)
	mockH.EXPECT().Data().AnyTimes().Return(make(chan []byte))
	mockH.EXPECT().Exit().AnyTimes().Return(make(chan pty.ExitInfo))
	mockBE.EXPECT().
		Open(gomock.Any(), pty.Spec{Cwd: "/tmp", Command: "go test ./...", Cols: 80, Rows: 24}).
		Return(mockH, nil)

	sel := terminal_svc.NewBackendSelector(mockBE, func(string) (terminal_svc.PTYBackend, error) {
		t.Fatal("should not call remote factory for local")
		return nil, nil
	})
	svc := terminal_svc.NewService(sel, terminal_svc.NoopEmitter{})

	require.NoError(t, svc.OpenCommand(context.Background(), "t1", "", "/tmp", "go test ./...", 80, 24))
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd agentre && go test -race -run TestService_OpenCommand_PassesCommandToBackend ./internal/service/terminal_svc/`
Expected: FAIL — `svc.OpenCommand undefined`。

- [ ] **Step 3: 抽出私有 `open` 并加 `OpenCommand`** — `internal/service/terminal_svc/service.go`:把现有 `Open` 改名为私有 `open(ctx, terminalID, deviceID string, spec pty.Spec) error`(方法体不变,只把 `backend.Open(openCtx, pty.Spec{Cwd: cwd, Cols: cols, Rows: rows})` 换成 `backend.Open(openCtx, spec)`),再加两个公开入口:

```go
// Open 开一个交互登录 shell(原行为)。
func (s *Service) Open(ctx context.Context, terminalID string, deviceID string, cwd string, cols, rows uint16) error {
	return s.open(ctx, terminalID, deviceID, pty.Spec{Cwd: cwd, Cols: cols, Rows: rows})
}

// OpenCommand 在 cwd 下以 `$SHELL -l -c command` 跑一条命令,复用同一套流式/退出/杀进程。
func (s *Service) OpenCommand(ctx context.Context, terminalID string, deviceID string, cwd string, command string, cols, rows uint16) error {
	return s.open(ctx, terminalID, deviceID, pty.Spec{Cwd: cwd, Command: command, Cols: cols, Rows: rows})
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `cd agentre && go test -race -run 'TestService_Open' ./internal/service/terminal_svc/`
Expected: PASS(新测试 + 既有 `Open` 测试都绿)。

- [ ] **Step 5: 提交**

```bash
cd agentre && git add internal/service/terminal_svc/service.go internal/service/terminal_svc/service_test.go
git commit -m "♻️ terminal_svc:抽出私有 open + 新增 OpenCommand(命令模式)"
```

---

## Task B4: `chat_svc.ResolveSessionExecTarget`(会话 → cwd + deviceID)

**Files:**
- Create: `internal/service/chat_svc/exec_target.go`
- Test: `internal/service/chat_svc/exec_target_test.go`
- Reference: `internal/service/chat_svc/git_state.go:96-118`(会话/agent/backend 加载范式)、`internal/service/chat_svc/cwd.go:70-72`(`ResolveSessionCwd`)

**Interfaces:**
- Produces:
  - `func execDeviceID(be *agent_backend_entity.AgentBackend) string`(纯函数:remote → `be.DeviceID`,否则 `""`)。
  - `func (s *chatSvc) ResolveSessionExecTarget(ctx context.Context, sessionID int64) (cwd string, deviceID string, err error)`。

- [ ] **Step 1: 写失败测试(纯函数)** — `internal/service/chat_svc/exec_target_test.go`:

```go
package chat_svc

import (
	"testing"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/stretchr/testify/assert"
)

func TestExecDeviceID(t *testing.T) {
	assert.Equal(t, "", execDeviceID(nil))
	local := &agent_backend_entity.AgentBackend{}        // 默认本地
	assert.Equal(t, "", execDeviceID(local))
	remote := &agent_backend_entity.AgentBackend{DeviceID: "dev-9"}
	// 让其成为 remote:按 AgentBackend 的实际判定字段设置(见 IsRemote/IsLocal 实现)。
	remote.Kind = agent_backend_entity.BackendKindRemote
	assert.Equal(t, "dev-9", execDeviceID(remote))
}
```

> 实现前先打开 `internal/model/entity/agent_backend_entity` 确认 `IsRemote()/IsLocal()` 依据的字段(上面的 `Kind`/`BackendKindRemote` 按实际改名);`execDeviceID` 内部用 `be.IsRemote()` 判定,测试据此构造 remote 实例。

- [ ] **Step 2: 跑测试确认失败**

Run: `cd agentre && go test -race -run TestExecDeviceID ./internal/service/chat_svc/`
Expected: FAIL — `execDeviceID undefined`。

- [ ] **Step 3: 实现 `exec_target.go`**:

```go
package chat_svc

import (
	"context"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/agentre-ai/agentre/internal/repository/agent_backend_repo"
	"github.com/agentre-ai/agentre/internal/repository/agent_repo"
	"github.com/agentre-ai/agentre/internal/repository/chat_repo"
	"github.com/agentre-ai/agentre/internal/pkg/i18n"
	"github.com/agentre-ai/agentre/internal/pkg/i18n/code"
	"github.com/cago-frame/cago/pkg/logger"
	"go.uber.org/zap"
)

// execDeviceID 返回 ! 命令应在哪台设备执行:remote 后端取其 DeviceID,本地为空串。
func execDeviceID(be *agent_backend_entity.AgentBackend) string {
	if be != nil && be.IsRemote() {
		return be.DeviceID
	}
	return ""
}

// ResolveSessionExecTarget 给定 sessionID,解析出 ! 命令的执行目标(cwd + deviceID)。
// 复用 ResolveSessionCwd 的项目/自由会话/远端解析规则,绝不连库以外的旁路。
func (s *chatSvc) ResolveSessionExecTarget(ctx context.Context, sessionID int64) (string, string, error) {
	sess, err := chat_repo.Session().Find(ctx, sessionID)
	if err != nil {
		logger.Ctx(ctx).Error("chat_svc.ResolveSessionExecTarget: find session", zap.Int64("sessionId", sessionID), zap.Error(err))
		return "", "", i18n.NewError(ctx, code.OperationFailed)
	}
	if sess == nil {
		return "", "", i18n.NewError(ctx, code.ChatSessionNotFound)
	}
	a, err := agent_repo.Agent().Find(ctx, sess.AgentID)
	if err != nil {
		logger.Ctx(ctx).Error("chat_svc.ResolveSessionExecTarget: find agent", zap.Int64("agentId", sess.AgentID), zap.Error(err))
		return "", "", i18n.NewError(ctx, code.OperationFailed)
	}
	var be *agent_backend_entity.AgentBackend
	if a != nil && a.AgentBackendID > 0 {
		be, err = agent_backend_repo.AgentBackend().Find(ctx, a.AgentBackendID)
		if err != nil {
			logger.Ctx(ctx).Error("chat_svc.ResolveSessionExecTarget: find backend", zap.Int64("backendId", a.AgentBackendID), zap.Error(err))
			return "", "", i18n.NewError(ctx, code.OperationFailed)
		}
	}
	cwd, err := resolveSessionCwd(ctx, sess, be)
	if err != nil {
		return "", "", err
	}
	return cwd, execDeviceID(be), nil
}
```

> import 路径以 `git_state.go` / `cwd.go` 实际引用为准(`i18n`/`code`/`logger` 包路径照抄那两个文件)。

- [ ] **Step 4: 跑纯函数测试确认通过**

Run: `cd agentre && go test -race -run TestExecDeviceID ./internal/service/chat_svc/`
Expected: PASS。

- [ ] **Step 5: 写 `ResolveSessionExecTarget` 的 repo-mock 测试** — 追加到 `exec_target_test.go`,镜像 chat_svc 既有 service 测试(mockgen 仓库 + `RegisterXxx` 注入;参照同目录其它 `_test.go` 的注册写法)。覆盖:本地会话 → `deviceID==""` 且 cwd 来自注入的 `resolveCwdFn`;远端会话 → `deviceID==be.DeviceID`。最少一例:

```go
func TestResolveSessionExecTarget_LocalUsesResolverCwd(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	sessionRepo := repomock.NewMockSessionRepo(ctrl)
	agentRepo := repomock.NewMockAgentRepo(ctrl)
	sessionRepo.EXPECT().Find(gomock.Any(), int64(7)).Return(&chat_entity.Session{ID: 7, AgentID: 3}, nil)
	agentRepo.EXPECT().Find(gomock.Any(), int64(3)).Return(&agent_entity.Agent{ID: 3, AgentBackendID: 0}, nil)
	chat_repo.RegisterSession(sessionRepo)
	agent_repo.RegisterAgent(agentRepo)

	RegisterCwdResolver(func(_ context.Context, sess *chat_entity.Session) (string, error) {
		return "/proj/x", nil
	})

	cwd, dev, err := Default().ResolveSessionExecTarget(context.Background(), 7)
	require.NoError(t, err)
	assert.Equal(t, "/proj/x", cwd)
	assert.Equal(t, "", dev)
}
```

> mock 包名 / `RegisterSession` / `RegisterAgent` / `RegisterCwdResolver` / `Default()` 以仓库实际为准(打开同目录现有 service 测试与 `cwd.go` 的 `RegisterCwdResolver` 确认)。`AgentBackendID: 0` ⇒ 不查 backend ⇒ `be==nil` ⇒ 走本地分支。

- [ ] **Step 6: 跑测试确认失败再补齐至通过**

Run: `cd agentre && go test -race -run TestResolveSessionExecTarget ./internal/service/chat_svc/`
Expected: 先 FAIL(mock/Register 名不符则修正),最终 PASS。

- [ ] **Step 7: 提交**

```bash
cd agentre && git add internal/service/chat_svc/exec_target.go internal/service/chat_svc/exec_target_test.go
git commit -m "✨ chat_svc.ResolveSessionExecTarget:会话→(cwd, deviceID) 解析"
```

---

## Task B5: Wails 绑定 `TerminalRunCommand` + 刷新 bindings

**Files:**
- Modify: `internal/app/terminal.go:14-23`
- Reference: `internal/app/terminal.go`(`a.terminalSvc`、`errTerminalSvcNotInitialized`)、`internal/service/chat_svc`(`Default()`)

**Interfaces:**
- Consumes: `chat_svc.Default().ResolveSessionExecTarget`(B4)、`terminal_svc.OpenCommand`(B3)。
- Produces: 绑定 `App.TerminalRunCommand(terminalID string, sessionID int64, command string, cols, rows uint16) error` → 前端 `App.TerminalRunCommand(terminalID, sessionId, command, cols, rows)`。

- [ ] **Step 1: 实现绑定** — 追加到 `internal/app/terminal.go`(紧邻 `TerminalOpen`):

```go
// TerminalRunCommand 在会话工作目录下,以 `$SHELL -l -c command` 跑一条本地命令(绕开 AI agent)。
// terminalID 由前端生成,与普通终端一致;输出走相同的 terminal:<id>:data/exit 事件。
func (a *App) TerminalRunCommand(terminalID string, sessionID int64, command string, cols, rows uint16) error {
	if a.terminalSvc == nil {
		return errTerminalSvcNotInitialized
	}
	cwd, deviceID, err := chat_svc.Default().ResolveSessionExecTarget(a.ctx, sessionID)
	if err != nil {
		return err
	}
	return a.terminalSvc.OpenCommand(a.ctx, terminalID, deviceID, cwd, command, cols, rows)
}
```

> 顶部 import 加 `chat_svc`(路径与仓库一致:`github.com/agentre-ai/agentre/internal/service/chat_svc`)。

- [ ] **Step 2: 编译确认绑定方法可用**

Run: `cd agentre && go build ./internal/app/`
Expected: 通过。

- [ ] **Step 3: 刷新前端 bindings**

Run: `cd agentre && make generate`
Expected: `frontend/wailsjs/go/app/App.d.ts` 出现 `export function TerminalRunCommand(arg1:string,arg2:number,arg3:string,arg4:number,arg5:number):Promise<void>;`

- [ ] **Step 4: 提交**

```bash
cd agentre && git add internal/app/terminal.go frontend/wailsjs
git commit -m "✨ App.TerminalRunCommand 绑定:会话级 ! 命令执行入口"
```

---

## Task F1: i18n 文案(命令模式 + 本地命令卡片)

**Files:**
- Modify: `frontend/src/i18n/locales/zh-CN/common.json`(`chat.composer` 块内 + 顶层加 `localCommand`)
- Modify: `frontend/src/i18n/locales/en/common.json`(对应)
- Test: `frontend/src/__tests__/i18n.test.ts`(既有,跑通即可)

**Interfaces:**
- Produces 文案 key:`chat.composer.command.banner` / `chat.composer.command.run`;`localCommand.localChip` / `localCommand.notSharedWithAI` / `localCommand.status.{running,done,failed,stopped}` / `localCommand.exitCode` / `localCommand.elapsed` / `localCommand.stop` / `localCommand.openInTerminal`。

- [ ] **Step 1: zh-CN 加 key** — 在 `chat.composer` 对象内加:

```json
"command": {
  "banner": "命令模式 · 在项目目录直接执行 · Enter 运行 · 删除 ! 退出",
  "run": "运行命令"
}
```

顶层(与 `chat` 同级)加:

```json
"localCommand": {
  "localChip": "本地命令",
  "notSharedWithAI": "不发送给 AI",
  "status": { "running": "运行中", "done": "完成", "failed": "失败", "stopped": "已停止" },
  "exitCode": "退出码 {{code}}",
  "elapsed": "{{seconds}}s",
  "stop": "停止",
  "openInTerminal": "在终端中打开"
}
```

- [ ] **Step 2: en 加同结构 key**:

```json
"command": {
  "banner": "Command mode · runs in the project directory · Enter to run · delete ! to exit",
  "run": "Run command"
}
```
```json
"localCommand": {
  "localChip": "Local command",
  "notSharedWithAI": "Not sent to AI",
  "status": { "running": "Running", "done": "Done", "failed": "Failed", "stopped": "Stopped" },
  "exitCode": "Exit {{code}}",
  "elapsed": "{{seconds}}s",
  "stop": "Stop",
  "openInTerminal": "Open in terminal"
}
```

- [ ] **Step 3: 跑 i18n 覆盖测试**

Run: `cd agentre/frontend && pnpm test -- src/__tests__/i18n.test.ts`
Expected: PASS(zh/en key 对齐)。

- [ ] **Step 4: 提交**

```bash
cd agentre && git add frontend/src/i18n/locales/zh-CN/common.json frontend/src/i18n/locales/en/common.json
git commit -m "🌐 ! 命令模式 + 本地命令卡片 i18n 文案(zh/en)"
```

---

## Task F2: `AIChatInput` 命令模式检测 + 命令提交 + 抑制 slash

**Files:**
- Modify: `frontend/src/components/agentre/chat-input/index.tsx:42-63,151-202,227-244`
- Test: `frontend/src/components/agentre/chat-input/__tests__/command-mode.test.tsx`(新)

**Interfaces:**
- Produces 两个新 prop:`onCommandModeChange?: (active: boolean) => void`、`onCommandSubmit?: (command: string) => void`。命令模式定义:`extractPlainText(doc).trimStart()` 以 `!` 开头。提交时若命令模式 → 调 `onCommandSubmit(去掉首个 ! 并 trim)`,否则 `onSubmit`。命令模式下抑制 slash 菜单。

- [ ] **Step 1: 写失败测试** — `command-mode.test.tsx`(顶部 per-file `vi.mock` wailsjs runtime,见仓库既有 chat-input 测试范式):

```tsx
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, it, expect, vi } from "vitest";
import { AIChatInput } from "../index";

describe("AIChatInput command mode", () => {
  it("toggles command mode on leading ! and strips it on submit", async () => {
    const onCommandModeChange = vi.fn();
    const onCommandSubmit = vi.fn();
    const onSubmit = vi.fn();
    render(
      <AIChatInput
        sendOnEnter
        onSubmit={onSubmit}
        onCommandModeChange={onCommandModeChange}
        onCommandSubmit={onCommandSubmit}
      />,
    );
    const box = screen.getByRole("textbox");
    await userEvent.click(box);
    await userEvent.keyboard("!go test ./...");
    expect(onCommandModeChange).toHaveBeenLastCalledWith(true);

    await userEvent.keyboard("{Enter}");
    expect(onCommandSubmit).toHaveBeenCalledWith("go test ./...");
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it("stays normal and routes to onSubmit without leading !", async () => {
    const onCommandSubmit = vi.fn();
    const onSubmit = vi.fn();
    render(<AIChatInput sendOnEnter onSubmit={onSubmit} onCommandSubmit={onCommandSubmit} />);
    const box = screen.getByRole("textbox");
    await userEvent.click(box);
    await userEvent.keyboard("hello{Enter}");
    expect(onSubmit).toHaveBeenCalledWith("hello");
    expect(onCommandSubmit).not.toHaveBeenCalled();
  });
});
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd agentre/frontend && pnpm test -- src/components/agentre/chat-input/__tests__/command-mode.test.tsx`
Expected: FAIL — `onCommandSubmit` 未被调用 / prop 未识别。

- [ ] **Step 3: 加 props** — `AIChatInputProps` 接口追加:

```typescript
  onCommandModeChange?: (active: boolean) => void;
  onCommandSubmit?: (command: string) => void;
```

- [ ] **Step 4: 检测命令模式** — 在 AIChatInput 既有上报 `onEmptyChange` 的同一 editor 更新回调里(`onUpdate`/`onTransaction`),计算并上报命令模式。用一个 ref 去重避免重复触发:

```typescript
const commandModeRef = useRef(false);
// ...在更新回调中(与 onEmptyChange 同处):
const text = extractPlainText(editor.state.doc as unknown as ProseMirrorLikeNode);
const inCommandMode = text.trimStart().startsWith("!");
if (inCommandMode !== commandModeRef.current) {
  commandModeRef.current = inCommandMode;
  onCommandModeChange?.(inCommandMode);
}
```

> 用 `onCommandModeChangeRef`(同 `submitRef`/`sendOnEnterRef` 的 ref 模式)持有最新回调,避免闭包过期。

- [ ] **Step 5: 命令提交分流** — 改 `triggerSubmitRef`(`index.tsx:227-244`)在 `submitRef.current(content)` 前分流:

```typescript
const text = content;
if (text.trimStart().startsWith("!")) {
  const command = text.trimStart().slice(1).trim();
  historyIndexRef.current = -1;
  if (command) onCommandSubmitRef.current?.(command);
  editor.commands.clearContent(true);
  editor.commands.focus();
  return;
}
historyIndexRef.current = -1;
submitRef.current(content);
editor.commands.clearContent(true);
editor.commands.focus();
```

> `onCommandSubmitRef` 同 `submitRef` 的 ref 模式。空命令(只有 `!`)⇒ 不触发、清空、保持聚焦。

- [ ] **Step 6: 抑制 slash 菜单** — 命令模式下不弹 slash:在调用 `slashKeyDownRef.current(event)`(`index.tsx:153` 附近)前加守卫:

```typescript
if (!commandModeRef.current && slashKeyDownRef.current(event)) return true;
```

- [ ] **Step 7: 跑测试确认通过**

Run: `cd agentre/frontend && pnpm test -- src/components/agentre/chat-input/__tests__/command-mode.test.tsx`
Expected: PASS。

- [ ] **Step 8: 提交**

```bash
cd agentre && git add frontend/src/components/agentre/chat-input/index.tsx frontend/src/components/agentre/chat-input/__tests__/command-mode.test.tsx
git commit -m "✨ AIChatInput:首字符 ! 进命令模式,提交分流 onCommandSubmit,抑制 slash"
```

---

## Task F3: `ChatComposer` 命令模式横幅 + 运行按钮

**Files:**
- Modify: `frontend/src/components/agentre/chat.tsx:149-188,641-668,719-781`
- Test: `frontend/src/components/agentre/__tests__/composer-command-mode.test.tsx`(新)

**Interfaces:**
- Consumes:`AIChatInput` 的 `onCommandModeChange`/`onCommandSubmit`(F2)、i18n `chat.composer.command.*`(F1)。
- Produces:`ChatComposerProps` 加 `onRunCommand?: (command: string) => void`;命令模式横幅 + 运行按钮(终端图标),透传 `onCommandSubmit={onRunCommand}` 给内层 `AIChatInput`。

- [ ] **Step 1: 写失败测试** — `composer-command-mode.test.tsx`(per-file `vi.mock` wailsjs;渲染 `ChatComposer`,断言键入 `!` 后出现命令模式横幅 `chat.composer.command.banner` 文案):

```tsx
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, it, expect, vi } from "vitest";
import { ChatComposer } from "../chat";

it("shows command-mode banner when input starts with !", async () => {
  render(<ChatComposer onRunCommand={vi.fn()} />);
  await userEvent.click(screen.getByRole("textbox"));
  await userEvent.keyboard("!ls");
  expect(
    screen.getByText(/命令模式|Command mode/),
  ).toBeInTheDocument();
});
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd agentre/frontend && pnpm test -- src/components/agentre/__tests__/composer-command-mode.test.tsx`
Expected: FAIL — 找不到横幅文案。

- [ ] **Step 3: 加 prop + 状态** — `ChatComposerProps` 加 `onRunCommand?: (command: string) => void;`;组件内加 `const [commandMode, setCommandMode] = useState(false);`。

- [ ] **Step 4: 透传给 AIChatInput** — 在渲染 `AIChatInput` 处加:

```tsx
onCommandModeChange={setCommandMode}
onCommandSubmit={onRunCommand}
```

- [ ] **Step 5: 命令模式横幅** — 在编辑模式横幅(`chat.tsx:641-668`)之后、复用同款类名加命令模式横幅(用 primary 配色);仅 `!editing && commandMode` 时显示:

```tsx
{!editing && commandMode ? (
  <div
    role="status"
    className="flex items-center gap-2 border-b border-primary-text/20 bg-primary-soft px-3 py-1.5 text-[11px]"
  >
    <SquareTerminal className="size-3 shrink-0 text-primary-text" aria-hidden="true" />
    <span className="min-w-0 flex-1 truncate font-medium text-primary-text">
      {t("chat.composer.command.banner")}
    </span>
    <span className="inline-flex items-center gap-1 text-muted-foreground">
      <EyeOff className="size-3" aria-hidden="true" />
      {t("localCommand.notSharedWithAI")}
    </span>
  </div>
) : null}
```

> `SquareTerminal`/`EyeOff` 从 `lucide-react` 引入。

- [ ] **Step 6: 运行按钮态** — 在发送按钮区(`chat.tsx:719-781`),命令模式且非编辑时,把发送按钮换成终端图标的"运行"按钮(`aria-label={t("chat.composer.command.run")}`),保留原发送按钮于非命令模式:

```tsx
{!editing && commandMode ? (
  <Button type="submit" size="icon-sm" aria-label={t("chat.composer.command.run")} title={t("chat.composer.command.run")}>
    <SquareTerminal data-icon="only" aria-hidden="true" />
  </Button>
) : editing ? (
  /* 既有保存按钮 */
) : (
  /* 既有发送按钮 */
)}
```

> 命令模式下隐藏 `QuotaMeter`/`ContextMeter`(它们是 AI turn 概念,与本地命令无关):给那两个的渲染条件追加 `&& !commandMode`。

- [ ] **Step 7: 跑测试确认通过**

Run: `cd agentre/frontend && pnpm test -- src/components/agentre/__tests__/composer-command-mode.test.tsx`
Expected: PASS。

- [ ] **Step 8: 提交**

```bash
cd agentre && git add frontend/src/components/agentre/chat.tsx frontend/src/components/agentre/__tests__/composer-command-mode.test.tsx
git commit -m "✨ ChatComposer:命令模式横幅 + 运行按钮(透传 onRunCommand)"
```

---

## Task F4: 临时本地命令 store `useLocalCommandsStore`

**Files:**
- Create: `frontend/src/stores/local-commands-store.ts`
- Test: `frontend/src/stores/__tests__/local-commands-store.test.ts`(新)

**Interfaces:**
- Produces:
```typescript
export type LocalCommandStatus = "running" | "done" | "failed" | "stopped";
export interface LocalCommandEntry {
  id: string;            // = terminalId
  sessionId: number;
  command: string;
  createdAt: number;     // Date.now(),transcript 排序键
  status: LocalCommandStatus;
  exitCode?: number;
  output: string;        // 累积原始输出(含 ANSI,流式 TextDecoder 解码)
}
// actions:
start(e: { id; sessionId; command; createdAt }): void
appendOutput(id: string, chunk: string): void
finish(id: string, status: Exclude<LocalCommandStatus,"running">, exitCode?: number): void
listForSession(sessionId: number): LocalCommandEntry[]   // 选择器
get(id: string): LocalCommandEntry | undefined
```

- [ ] **Step 1: 写失败测试** — `local-commands-store.test.ts`:

```ts
import { describe, it, expect, beforeEach } from "vitest";
import { useLocalCommandsStore } from "../local-commands-store";

describe("useLocalCommandsStore", () => {
  beforeEach(() => useLocalCommandsStore.setState({ entries: {} }));

  it("starts, appends, finishes", () => {
    const s = useLocalCommandsStore.getState();
    s.start({ id: "t1", sessionId: 5, command: "ls", createdAt: 100 });
    s.appendOutput("t1", "a");
    s.appendOutput("t1", "b");
    s.finish("t1", "done", 0);
    const e = useLocalCommandsStore.getState().get("t1")!;
    expect(e.output).toBe("ab");
    expect(e.status).toBe("done");
    expect(e.exitCode).toBe(0);
  });

  it("listForSession returns only that session, ordered by createdAt", () => {
    const s = useLocalCommandsStore.getState();
    s.start({ id: "b", sessionId: 5, command: "y", createdAt: 200 });
    s.start({ id: "a", sessionId: 5, command: "x", createdAt: 100 });
    s.start({ id: "c", sessionId: 9, command: "z", createdAt: 150 });
    const list = useLocalCommandsStore.getState().listForSession(5);
    expect(list.map((e) => e.id)).toEqual(["a", "b"]);
  });
});
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd agentre/frontend && pnpm test -- src/stores/__tests__/local-commands-store.test.ts`
Expected: FAIL — 模块不存在。

- [ ] **Step 3: 实现 store**(zustand,镜像 `chat-tabs-store` 的写法):

```ts
import { create } from "zustand";

export type LocalCommandStatus = "running" | "done" | "failed" | "stopped";
export interface LocalCommandEntry {
  id: string;
  sessionId: number;
  command: string;
  createdAt: number;
  status: LocalCommandStatus;
  exitCode?: number;
  output: string;
}
interface State {
  entries: Record<string, LocalCommandEntry>;
  start(e: { id: string; sessionId: number; command: string; createdAt: number }): void;
  appendOutput(id: string, chunk: string): void;
  finish(id: string, status: Exclude<LocalCommandStatus, "running">, exitCode?: number): void;
  get(id: string): LocalCommandEntry | undefined;
  listForSession(sessionId: number): LocalCommandEntry[];
}

export const useLocalCommandsStore = create<State>((set, get) => ({
  entries: {},
  start: (e) =>
    set((s) => ({
      entries: { ...s.entries, [e.id]: { ...e, status: "running", output: "" } },
    })),
  appendOutput: (id, chunk) =>
    set((s) => {
      const cur = s.entries[id];
      if (!cur) return s;
      return { entries: { ...s.entries, [id]: { ...cur, output: cur.output + chunk } } };
    }),
  finish: (id, status, exitCode) =>
    set((s) => {
      const cur = s.entries[id];
      if (!cur) return s;
      return { entries: { ...s.entries, [id]: { ...cur, status, exitCode } } };
    }),
  get: (id) => get().entries[id],
  listForSession: (sessionId) =>
    Object.values(get().entries)
      .filter((e) => e.sessionId === sessionId)
      .sort((a, b) => a.createdAt - b.createdAt),
}));
```

- [ ] **Step 4: 跑测试确认通过**

Run: `cd agentre/frontend && pnpm test -- src/stores/__tests__/local-commands-store.test.ts`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
cd agentre && git add frontend/src/stores/local-commands-store.ts frontend/src/stores/__tests__/local-commands-store.test.ts
git commit -m "✨ useLocalCommandsStore:临时本地命令记录(start/append/finish)"
```

---

## Task F5: `LocalCommandCard` 组件(+ ANSI 去码)

**Files:**
- Create: `frontend/src/components/agentre/local-command/ansi.ts`
- Create: `frontend/src/components/agentre/local-command/card.tsx`
- Test: `frontend/src/components/agentre/local-command/__tests__/ansi.test.ts`(新)
- Test: `frontend/src/components/agentre/local-command/__tests__/card.test.tsx`(新)

**Interfaces:**
- Consumes:`useLocalCommandsStore`(F4)、i18n `localCommand.*`(F1)、`App.TerminalClose`(wailsjs)、`useChatTabsStore.attachTerminal`(F8;F5 先用占位回调 prop,F8 接线)。
- Produces:
  - `stripAnsi(s: string): string`。
  - `<LocalCommandCard entryId={string} onOpenInTerminal={(id: string) => void} />`(纯从 store 读 `entryId` 渲染)。

- [ ] **Step 1: 写 ANSI 去码失败测试** — `ansi.test.ts`:

```ts
import { describe, it, expect } from "vitest";
import { stripAnsi } from "../ansi";
it("strips SGR escape sequences", () => {
  expect(stripAnsi("[32mPASS[0m ok")).toBe("PASS ok");
});
```

- [ ] **Step 2: 跑测试确认失败 → 实现 `ansi.ts`**

```ts
// 去除 CSI/SGR 等 ANSI 转义,用于内联卡片只读展示(真彩交互在 xterm)。
// eslint-disable-next-line no-control-regex
const ANSI_RE = /\[[0-9;?]*[ -/]*[@-~]/g;
export function stripAnsi(s: string): string {
  return s.replace(ANSI_RE, "");
}
```

Run: `cd agentre/frontend && pnpm test -- src/components/agentre/local-command/__tests__/ansi.test.ts`
Expected: PASS。

- [ ] **Step 3: 写卡片失败测试** — `card.test.tsx`(per-file `vi.mock` wailsjs runtime + `App.TerminalClose`;用 store 预置条目):

```tsx
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { useLocalCommandsStore } from "../../../../stores/local-commands-store";
import { LocalCommandCard } from "../card";

const close = vi.fn();
vi.mock("../../../../../wailsjs/go/app/App", () => ({ TerminalClose: (...a: unknown[]) => close(...a) }));

describe("LocalCommandCard", () => {
  beforeEach(() => {
    close.mockReset();
    useLocalCommandsStore.setState({ entries: {} });
  });

  it("running shows stop + open-in-terminal; stop calls TerminalClose", async () => {
    useLocalCommandsStore.getState().start({ id: "t1", sessionId: 1, command: "go test", createdAt: 1 });
    useLocalCommandsStore.getState().appendOutput("t1", "=== RUN x\n");
    const onOpen = vi.fn();
    render(<LocalCommandCard entryId="t1" onOpenInTerminal={onOpen} />);
    expect(screen.getByText("go test")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: /停止|Stop/ }));
    expect(close).toHaveBeenCalledWith("t1");
    await userEvent.click(screen.getByRole("button", { name: /在终端中打开|Open in terminal/ }));
    expect(onOpen).toHaveBeenCalledWith("t1");
  });

  it("after exit shows exit code and no action buttons", () => {
    useLocalCommandsStore.getState().start({ id: "t2", sessionId: 1, command: "go test", createdAt: 1 });
    useLocalCommandsStore.getState().finish("t2", "failed", 1);
    render(<LocalCommandCard entryId="t2" onOpenInTerminal={vi.fn()} />);
    expect(screen.getByText(/退出码 1|Exit 1/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /停止|Stop/ })).toBeNull();
    expect(screen.queryByRole("button", { name: /在终端中打开|Open in terminal/ })).toBeNull();
  });
});
```

- [ ] **Step 4: 跑测试确认失败**

Run: `cd agentre/frontend && pnpm test -- src/components/agentre/local-command/__tests__/card.test.tsx`
Expected: FAIL — 模块不存在。

- [ ] **Step 5: 实现 `card.tsx`** — 视觉对齐 Pencil 设计稿(`agentre.pen` 的 `本地命令 — 状态 & 接管`):终端 glyph 头部 + `本地命令` chip + 命令(等宽)+ 状态胶囊(运行中琥珀/完成绿+退出码/失败红+退出码/已停止灰);深色输出区(`stripAnsi` 后 tail);**运行中**才有 `停止`(调 `TerminalClose(entryId)`)+ `在终端中打开`(调 `onOpenInTerminal(entryId)`);退出后无按钮;`不发送给 AI` 标记。用 `useLocalCommandsStore((s) => s.entries[entryId])` 订阅单条;文案全走 `t(...)`;按钮用 `@/components/ui/button`。状态→样式映射用一个常量表(DRY)。

- [ ] **Step 6: 跑测试确认通过**

Run: `cd agentre/frontend && pnpm test -- src/components/agentre/local-command/__tests__/`
Expected: PASS。

- [ ] **Step 7: 提交**

```bash
cd agentre && git add frontend/src/components/agentre/local-command/
git commit -m "✨ LocalCommandCard + stripAnsi:本地命令内联卡片(运行/完成/失败/已停止)"
```

---

## Task F6: `ChatPanel.onRunCommand`(起命令 + 单点订阅写 store + 并发)

**Files:**
- Modify: `frontend/src/components/agentre/chat-panel.tsx`(在 `doSend` 附近加 `runLocalCommand`,并把 `onRunCommand` 传给 `ChatComposer`)
- Test: `frontend/src/components/agentre/__tests__/run-local-command.test.ts`(新,测纯函数 helper)

**Interfaces:**
- Consumes:`App.TerminalRunCommand`(B5)、`useLocalCommandsStore`(F4)、Wails `EventsOn`/`EventsOff`、`base64ToBytes`(复用 `terminal/use-terminal` 的解码工具或同款实现)。
- Produces:`runLocalCommand(sessionId: number, command: string): void` —— 生成 `terminalId`、`store.start`、`TerminalRunCommand`、单点订阅 `terminal:<id>:data/exit`(流式 `TextDecoder` 解码 append 进 store;exit → `finish` + `EventsOff`)。**不读取/不依赖 turn 运行态**(AI 流式时也即时运行)。

- [ ] **Step 1: 抽一个可测纯函数** — 把"data 帧 → 解码增量字符串"抽成 `frontend/src/components/agentre/local-command/decode.ts`:

```ts
export function makeStreamDecoder() {
  const dec = new TextDecoder();
  return (b64: string): string => dec.decode(base64ToBytes(b64), { stream: true });
}
```
> `base64ToBytes` 复用 `terminal/use-terminal.ts` 里同名工具(若是私有则提取到共用 util)。

测试 `run-local-command.test.ts`:

```ts
import { describe, it, expect } from "vitest";
import { makeStreamDecoder } from "../local-command/decode";

it("decodes base64 chunks incrementally", () => {
  const dec = makeStreamDecoder();
  // "hi" = aGk=
  expect(dec("aGk=")).toBe("hi");
});
```

- [ ] **Step 2: 跑测试确认失败 → 实现 `decode.ts` → 通过**

Run: `cd agentre/frontend && pnpm test -- src/components/agentre/__tests__/run-local-command.test.ts`
Expected: 先 FAIL 再 PASS。

- [ ] **Step 3: 在 ChatPanel 实现 `runLocalCommand`**:

```ts
function runLocalCommand(targetSessionId: number, command: string) {
  const terminalId = crypto.randomUUID();
  useLocalCommandsStore.getState().start({
    id: terminalId, sessionId: targetSessionId, command, createdAt: Date.now(),
  });
  const dataEvent = `terminal:${terminalId}:data`;
  const exitEvent = `terminal:${terminalId}:exit`;
  const decode = makeStreamDecoder();
  EventsOn(dataEvent, (p: { data: string }) =>
    useLocalCommandsStore.getState().appendOutput(terminalId, decode(p.data)),
  );
  EventsOn(exitEvent, (p: { code: number; reason: string }) => {
    const status = p.reason === "killed" || p.reason === "signal" ? "stopped" : p.code === 0 ? "done" : "failed";
    useLocalCommandsStore.getState().finish(terminalId, status, p.code);
    EventsOff(dataEvent);
    EventsOff(exitEvent);
  });
  void App.TerminalRunCommand(terminalId, targetSessionId, command, 80, 24).catch((e: unknown) => {
    useLocalCommandsStore.getState().appendOutput(terminalId, String(e));
    useLocalCommandsStore.getState().finish(terminalId, "failed", -1);
    EventsOff(dataEvent);
    EventsOff(exitEvent);
  });
}
```

> `reason` 的 `killed`/`signal` 值以 `pty`/daemon exit `Reason` 实际字符串为准(查 `internal/pkg/pty/local` 的 reaper 与 `terminal_svc` exit 事件);拿不准就以 `code !== 0 && 被 Stop` 归 `stopped`。`80,24` 为初始尺寸,attach 后由终端 fit 校正(F8)。

- [ ] **Step 4: 接 `onRunCommand` 给 ChatComposer** — 找到渲染 `ChatComposer` 处(`chat-panel.tsx` 约 1960 行,`onSubmit` 同处),加:

```tsx
onRunCommand={(command) => runLocalCommand(sessionId, command)}
```

- [ ] **Step 5: 跑全量前端确保无回归**

Run: `cd agentre/frontend && pnpm test -- src/components/agentre/__tests__/run-local-command.test.ts`
Expected: PASS。(`runLocalCommand` 的副作用在 F7 端到端体现;此处验证解码纯函数 + 不破坏构建。)

- [ ] **Step 6: 提交**

```bash
cd agentre && git add frontend/src/components/agentre/chat-panel.tsx frontend/src/components/agentre/local-command/decode.ts frontend/src/components/agentre/__tests__/run-local-command.test.ts
git commit -m "✨ ChatPanel.runLocalCommand:起 ! 命令 + 单点订阅写 store(并发于 AI turn)"
```

---

## Task F7: transcript 归并本地命令条目并渲染卡片

**Files:**
- Modify: `frontend/src/components/agentre/transcript-rows.ts:357-371,418-427`
- Modify: transcript 渲染处(渲染 `TranscriptRow` 的组件,把本地命令行渲染成 `<LocalCommandCard>`)
- Test: `frontend/src/components/agentre/__tests__/transcript-rows-localcmd.test.ts`(新)

**Interfaces:**
- Consumes:`LocalCommandEntry`(F4)、`LocalCommandCard`(F5)。
- Produces:`buildTranscriptRows` 入参加 `localCommands?: LocalCommandEntry[]`;输出 rows 中插入 `item.type === "local_command"` 的行,按 `createdAt` 与消息时间归并。

- [ ] **Step 1: 写失败测试** — `transcript-rows-localcmd.test.ts`,断言本地命令条目按 `createdAt` 归并进 rows(末尾/中间位置):

```ts
import { describe, it, expect } from "vitest";
import { buildTranscriptRows } from "../transcript-rows";

it("merges local command entries by createdAt", () => {
  const res = buildTranscriptRows({
    displayMessages: [], autonomousIds: new Set(),
    localCommands: [{ id: "t1", sessionId: 1, command: "ls", createdAt: 100, status: "running", output: "" }],
  });
  const row = res.rows.find((r) => r.item.type === "local_command");
  expect(row).toBeTruthy();
  // @ts-expect-error narrow
  expect(row!.item.entry.id).toBe("t1");
});
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd agentre/frontend && pnpm test -- src/components/agentre/__tests__/transcript-rows-localcmd.test.ts`
Expected: FAIL — `localCommands` 未识别 / 无该行。

- [ ] **Step 3: 扩展类型与归并** — `transcript-rows.ts`:
  - `BuildTranscriptRowsArgs` 加 `localCommands?: LocalCommandEntry[];`。
  - `TranscriptRowItem` union 加 `| { type: "local_command"; entry: LocalCommandEntry; uiStateKey?: undefined }`。
  - 在 `buildTranscriptRows` 末尾把 `localCommands` 各条转成一个 row(`key: \`localcmd:${entry.id}\``、`messageId: -1` 之类哨兵或复用现有可空字段),再把全部 rows 按"排序键"稳定排序:消息行用其消息 `createdAt`,本地命令行用 `entry.createdAt`。保持既有 `firstRowIndexByMessageId`/`rowIndexByKey` 构建在排序后重算。

> 若 `TranscriptRow` 强依赖 `message: chat_svc.ChatMessage`,给 local_command 行用一个最小的合成 message 或放宽该字段为可选 —— 以最小改动接进既有渲染循环为准;务必不破坏既有消息行测试。

- [ ] **Step 4: 渲染分支** — 在消费 `TranscriptRow` 的渲染组件里,`item.type === "local_command"` 时渲染:

```tsx
<LocalCommandCard
  entryId={item.entry.id}
  onOpenInTerminal={(id) => useChatTabsStore.getState().attachTerminal({ terminalId: id, command: item.entry.command })}
/>
```

> `attachTerminal` 在 F8 实现;此处先引用,F8 完成后端到端可用。本步先保证类型与渲染分支接好(F8 前 `attachTerminal` 可为 no-op 占位以过编译)。

- [ ] **Step 5: 传入 localCommands** — 在调用 `buildTranscriptRows` 处,用 `useLocalCommandsStore((s) => s.listForSession(sessionId))`(`useShallow` 包裹身份集合)把当前会话条目传入。

- [ ] **Step 6: 跑测试确认通过 + 既有 transcript 测试不回归**

Run: `cd agentre/frontend && pnpm test -- src/components/agentre/__tests__/transcript-rows-localcmd.test.ts && pnpm test -- transcript-rows`
Expected: PASS(含既有 `transcript-rows` 套件)。

- [ ] **Step 7: 提交**

```bash
cd agentre && git add frontend/src/components/agentre/transcript-rows.ts frontend/src/components/agentre/__tests__/transcript-rows-localcmd.test.ts
git add -p   # 仅把渲染分支/传参相关改动纳入(排除无关)
git commit -m "✨ transcript:本地命令条目按时间归并并渲染 LocalCommandCard"
```

---

## Task F8: 「在终端中打开」—— 接管同一 PTY(store 驱动 attach 模式)

**Files:**
- Modify: `frontend/src/stores/chat-tabs-store.ts:5-24,301-315`
- Modify: `frontend/src/components/agentre/chat-tabs/chat-panel-host.tsx:99-104,226-256`
- Modify: `frontend/src/components/agentre/terminal/terminal-panel.tsx:19-25,77-112`
- Test: `frontend/src/stores/__tests__/attach-terminal.test.ts`(新)

**Interfaces:**
- Consumes:`useLocalCommandsStore`(F4)、`App.TerminalWrite`/`TerminalResize`(wailsjs)。
- Produces:
  - `TabKind` terminal 分支加 `attach?: boolean; command?: string`。
  - `useChatTabsStore.attachTerminal({ terminalId, command }): void` —— 新建 `kind:"terminal"`、`attach:true` 的 tab 并激活。
  - `TerminalPanel` 加 `attach?: boolean`;attach 时**不调 `TerminalOpen`**,改为:从 `useLocalCommandsStore` 取该 `terminalId` 的 `output` seed 进 xterm,订阅 store 增量写 xterm(用已写长度 ref 取增量);`onData → App.TerminalWrite`、fit 后 `App.TerminalResize`;卸载**不** `TerminalClose`(PTY 归卡片/Stop 管)。**不**在 attach 模式订阅 `terminal:<id>:data/exit`(单一订阅者是 F6 的 store,避免 `EventsOff` 互删)。

- [ ] **Step 1: 写 attachTerminal 失败测试** — `attach-terminal.test.ts`:

```ts
import { describe, it, expect, beforeEach } from "vitest";
import { useChatTabsStore } from "../chat-tabs-store";

describe("attachTerminal", () => {
  beforeEach(() => useChatTabsStore.setState({ tabs: [], activeTabId: undefined }));
  it("creates an active attach terminal tab bound to the same terminalId", () => {
    useChatTabsStore.getState().attachTerminal({ terminalId: "t1", command: "go test" });
    const st = useChatTabsStore.getState();
    const tab = st.tabs.at(-1)!;
    expect(tab.meta).toMatchObject({ kind: "terminal", terminalId: "t1", attach: true });
    expect(st.activeTabId).toBe(tab.id);
  });
});
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd agentre/frontend && pnpm test -- src/stores/__tests__/attach-terminal.test.ts`
Expected: FAIL — `attachTerminal` 不存在。

- [ ] **Step 3: store 加字段 + action** — `TabKind` terminal 分支:

```typescript
  | { kind: "terminal"; projectId: number; deviceId: string; terminalId: string; attach?: boolean; command?: string }
```

加 action:

```typescript
attachTerminal: ({ terminalId, command }: { terminalId: string; command?: string }) =>
  set((state) => {
    const newTab: ChatTab = {
      id: nextId(),
      meta: { kind: "terminal", projectId: 0, deviceId: "", terminalId, attach: true, command },
      isPreview: false, isPinned: false, pinAt: 0, openedAt: now(),
      title: command ? i18n.t("chatTabs.terminal.titleWithCommand", { command }) : i18n.t("chatTabs.terminal.title"),
    };
    return { tabs: [...state.tabs, newTab], activeTabId: newTab.id };
  }),
```

> 在 store 的 actions 类型声明里同步加 `attachTerminal(...)` 签名;`chatTabs.terminal.titleWithCommand` 加进 zh/en i18n(同 F1 风格,值如 `命令:{{command}}` / `Command: {{command}}`),并过 i18n.test。

- [ ] **Step 4: 跑 attach 测试确认通过**

Run: `cd agentre/frontend && pnpm test -- src/stores/__tests__/attach-terminal.test.ts`
Expected: PASS。

- [ ] **Step 5: HostedTerminalPanel 透传 attach** — `chat-panel-host.tsx` 的 `HostedTerminalPanel` 给 `<TerminalPanel>` 加 `attach={meta.attach}`。

- [ ] **Step 6: TerminalPanel attach 分支** — `terminal-panel.tsx`:`TerminalPanelProps` 加 `attach?: boolean`。attach 时改用一个新 `useAttachedTerminal` hook(从 store seed + 写增量 + `App.TerminalWrite`/`TerminalResize`,卸载不 Close),否则走既有 `useTerminal`:

```tsx
// 顶层按 attach 选择数据源(两个 hook 都无条件调用以满足 hooks 规则:非激活的传 noop)
const attached = useAttachedTerminal(attach ? { terminalID, xtermRef } : null);
const live = useTerminal(attach ? NOOP_TERMINAL_ARGS : { terminalID, projectId, deviceId, cols: 80, rows: 24, onData: (d) => xtermRef.current?.write(d), onExit: handleExit });
const { write, resize } = attach ? attached : live;
```

> `useAttachedTerminal({ terminalID, xtermRef })`:用 ref 记 `writtenLen`;`useEffect` 订阅 `useLocalCommandsStore`,首帧 `xterm.write(entry.output)` 并置 `writtenLen=output.length`,后续 `output.slice(writtenLen)` 增量写;返回 `write=(d)=>App.TerminalWrite(terminalID,d)`、`resize=(c,r)=>App.TerminalResize(terminalID,c,r)`;**无** `EventsOn`/`TerminalOpen`/`TerminalClose`。若 hooks 条件调用难处理,改为单一 `useTerminalSource(attach, args)` 内部分支封装,保持 hooks 顺序稳定。

- [ ] **Step 7: 端到端手测脚本(记录于 PR 描述,非自动化)** — `make dev` 起应用:在某会话输入 `!ls -la` Enter → transcript 出运行中卡片并流式输出 → 点「在终端中打开」→ 终端 tab 显示同样输出且可继续输入;输入 `!sleep 30` → 点「停止」→ 卡片转"已停止";AI 生成中再输入 `!git status` → 命令即时运行不打断生成。

- [ ] **Step 8: 跑相关前端测试**

Run: `cd agentre/frontend && pnpm test -- src/stores/__tests__/attach-terminal.test.ts && pnpm test -- terminal`
Expected: PASS(含既有 terminal 套件)。

- [ ] **Step 9: 提交**

```bash
cd agentre && git add frontend/src/stores/chat-tabs-store.ts frontend/src/components/agentre/chat-tabs/chat-panel-host.tsx frontend/src/components/agentre/terminal/terminal-panel.tsx frontend/src/stores/__tests__/attach-terminal.test.ts frontend/src/i18n/locales
git commit -m "✨ 在终端中打开:store 驱动 attach 模式接管同一 PTY(stdin/颜色/回滚)"
```

---

## 收尾:全量校验

- [ ] **Step 1: 后端全量**

Run: `cd agentre && make test-backend`
Expected: PASS。

- [ ] **Step 2: 前端全量 + lint**

Run: `cd agentre && make test-frontend && make lint`
Expected: PASS(含 i18n 覆盖、`no-literal-string`)。

- [ ] **Step 3: 视觉核对** — 对照 `agentre.pen` 三屏(`Agent Chat — ! 命令模式` / `本地命令 — 状态 & 接管` / `本地命令 — 交互 & 并发`)核对命令模式横幅、卡片四态、运行中按钮、在终端中打开。

---

## Self-Review 备忘(写计划时已核对)

- **Spec 覆盖**:命令模式触发/退出(F2/F3)、私有不喂 AI(F6 绕开 SendChatMessage)、PTY 流式(B1/B3)、Stop(F5)、Open-in-terminal 同 PTY(F8)、临时不落库(F4)、transcript 内联归并(F7)、远端透传(B2)、并发非阻塞(F6 不读 turn 态)、i18n(F1)。
- **类型一致**:`Spec.Command`(B1)→ `OpenCommand`(B3)→ `TerminalRunCommand`(B5)→ 前端 `App.TerminalRunCommand`(F6);`LocalCommandEntry`/`status` 全程一致(F4→F5→F7);`attachTerminal` 签名(F8)与 F5/F7 调用一致。
- **已知软点(实现时确认而非占位)**:`AgentBackend.IsRemote()` 判定字段(B4)、chat_svc repo mock 的 `RegisterXxx` 名(B4)、exit `Reason` 字符串到 `stopped` 的映射(F6)、`TranscriptRow.message` 是否需放宽为可选(F7)、attach 模式 hooks 条件调用封装(F8)。
