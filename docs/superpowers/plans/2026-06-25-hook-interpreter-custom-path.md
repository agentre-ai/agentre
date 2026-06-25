# Hook 解释器自定义路径 + 平台感知 + 文案去重 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让脚本 Hook 的解释器能自定义二进制路径、按平台/实际安装情况列出,并把「两个 PowerShell」文案/视觉区分开。

**Architecture:** `hookexec` 注册表(单一事实源)新增 `goos` 平台字段与 `Probe(goos)` 探测函数;`Resolve` 支持路径覆盖;`hook_entity.Hook` 加 `interpreter_path` 列;服务层透传并新增 `ProbeInterpreters` 经 Wails 绑定给前端;前端用探测结果取代写死列表 + 加路径输入框。

**Tech Stack:** Go 1.26 / cago / gormigrate / SQLite;React 19 / TypeScript / Vitest / shadcn `@/components/ui/*` / react-i18next。

## Global Constraints

- TDD 严格 Red→Green→Refactor:无失败测试不写实现。
- 仓库根是 go.work;在 `agentre/` 子目录内跑命令。后端测试用 `make test-backend` 或 `go test -race <pkg>`(`go test ./...` 会扫到 frontend/node_modules)。
- 迁移只追加到 `migrationList()` 末尾,**禁止修改已存在迁移**;DDL 用原生 SQL。
- Repository 单测用 sqlmock;service 单测用 mockgen 注入,不连真库(迁移自测除外)。
- 前端可见 UI 文案必须走 `t(...)` 并同时更新 `frontend/src/i18n/locales/{zh-CN,en}/common.json`;`i18next/no-literal-string` 禁硬编码中文(含 `placeholder` 等可见属性);表单控件用 shadcn `@/components/ui/*`。
- gitmoji 提交;golangci-lint v2。
- **范围外**:远端 daemon 探测/跑脚本;完全任意新解释器(自填 args+ext);文件选择器。
- 注意:`hook_svc/types.go` 的 `HookEventItem` 近期被并发加了 `Kind` 字段,与本任务无关,**勿动勿还原**。

---

## File Structure

- `internal/pkg/hookexec/runner.go` — 改:`interpDef` 加 `goos`;`registry` 给 cmd/powershell 标 windows;加 `interpOrder`、`Available`、`Probe`、`appliesTo`;`Resolve` 改双参支持路径覆盖。
- `internal/pkg/hookexec/exec.go` — 改:`RunSpec` 加 `InterpreterPath`;`Run` 调 `Resolve(spec.Interpreter, spec.InterpreterPath)`。
- `internal/pkg/hookexec/{runner_test.go,exec_test.go}` — 改:`Resolve(...)` 调用补第二参;加 Probe/override 用例。
- `internal/model/entity/hook_entity/hook.go` — 改:`Hook` 加 `InterpreterPath` 字段。
- `migrations/202606250001_hook_interpreter_path.go`(+`_test.go`) — 新建:加 `interpreter_path` 列。
- `migrations/migrations.go` — 改:`migrationList()` 末尾追加 `migration202606250001()`。
- `internal/service/hook_svc/types.go` — 改:`HookItem`/`CreateHookRequest` 加 `InterpreterPath`;新增 `InterpreterOption`。
- `internal/service/hook_svc/hook.go` — 改:Create/Update 读写 `InterpreterPath`;`toHookItem` 投影;接口加 `ProbeInterpreters`;实现 `ProbeInterpreters`。
- `internal/service/hook_svc/run.go` — 改:`executeHook` 把 `h.InterpreterPath` 塞进 `RunSpec`。
- `internal/service/hook_svc/{hook_test.go,run_test.go}` — 改/加:透传与 Probe 用例。
- `internal/app/hook.go` — 改:加 `ProbeInterpreters()` 绑定。
- `frontend/src/i18n/locales/{zh-CN,en}/common.json` — 改:pwsh/powershell 文案;新增 path/notInstalled key。
- `frontend/src/components/agentre/hooks-page.tsx` — 改:类型/bridge/draft/payload/下拉/路径框/INTERP_META/挂载探测。
- `frontend/src/components/agentre/hooks-page.test.tsx` — 新建:探测下拉 + 路径框行为。

---

## Task 1: hookexec 平台字段 + Probe 探测

**Files:**
- Modify: `internal/pkg/hookexec/runner.go`
- Test: `internal/pkg/hookexec/runner_test.go`

**Interfaces:**
- Produces:
  - `type Available struct { Key string; Path string; Installed bool }`
  - `func Probe(goos string) []Available`(按 `interpOrder` 稳定顺序;`goos` 过滤;每项 `LookPath` 决定 `Installed`/`Path`)
  - `interpDef` 新增字段 `goos []string`(空=全平台)

- [ ] **Step 1: 写失败测试**

在 `internal/pkg/hookexec/runner_test.go` 追加:

```go
func TestProbe_DarwinHidesWindowsOnly(t *testing.T) {
	got := Probe("darwin")
	keys := map[string]Available{}
	for _, a := range got {
		keys[a.Key] = a
	}
	for _, win := range []string{"cmd", "powershell"} {
		if _, ok := keys[win]; ok {
			t.Errorf("Probe(darwin) should hide windows-only %q", win)
		}
	}
	if _, ok := keys["sh"]; !ok {
		t.Fatal("Probe(darwin) should list sh")
	}
	if !keys["sh"].Installed || keys["sh"].Path == "" {
		t.Errorf("sh should resolve on unix CI, got %+v", keys["sh"])
	}
}

func TestProbe_WindowsListsCmd(t *testing.T) {
	got := Probe("windows")
	var hasCmd bool
	for _, a := range got {
		if a.Key == "cmd" {
			hasCmd = true
		}
	}
	if !hasCmd {
		t.Error("Probe(windows) should list cmd")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd agentre && go test ./internal/pkg/hookexec/ -run TestProbe`
Expected: FAIL —「undefined: Probe / Available」编译不过。

- [ ] **Step 3: 实现 Probe + 平台字段**

在 `internal/pkg/hookexec/runner.go`:给 `interpDef` 加 `goos []string`;给 registry 的 `powershell`、`cmd` 加 `goos: []string{"windows"}`(其余不动);追加排序键与 Probe:

```go
type interpDef struct {
	candidates []string
	args       []string
	ext        string
	goos       []string // 空=全平台;仅 cmd/powershell 标 {"windows"}
}

// interpOrder 决定 Probe 输出顺序(map 无序)。
var interpOrder = []string{"bash", "sh", "node", "python", "pwsh", "powershell", "cmd"}

// Available 是一个解释器在当前机器上的可用性。
type Available struct {
	Key       string `json:"key"`
	Path      string `json:"path"`
	Installed bool   `json:"installed"`
}

func appliesTo(def interpDef, goos string) bool {
	if len(def.goos) == 0 {
		return true
	}
	for _, g := range def.goos {
		if g == goos {
			return true
		}
	}
	return false
}

// Probe 列出在 goos 平台下适用的解释器及其安装情况。
func Probe(goos string) []Available {
	out := make([]Available, 0, len(interpOrder))
	for _, key := range interpOrder {
		def := registry[key]
		if !appliesTo(def, goos) {
			continue
		}
		a := Available{Key: key}
		for _, name := range def.candidates {
			if bin, err := exec.LookPath(name); err == nil {
				a.Path, a.Installed = bin, true
				break
			}
		}
		out = append(out, a)
	}
	return out
}
```

registry 两行改成(其余 5 行保持原样):

```go
	"pwsh":       {candidates: []string{"pwsh"}, args: []string{"-NoProfile", "-File"}, ext: ".ps1"},
	"powershell": {candidates: []string{"powershell"}, args: []string{"-NoProfile", "-File"}, ext: ".ps1", goos: []string{"windows"}},
	"cmd":        {candidates: []string{"cmd"}, args: []string{"/c"}, ext: ".bat", goos: []string{"windows"}},
```

- [ ] **Step 4: 跑测试确认通过**

Run: `cd agentre && go test ./internal/pkg/hookexec/ -run TestProbe -v`
Expected: PASS(`TestProbe_DarwinHidesWindowsOnly`、`TestProbe_WindowsListsCmd`)。

- [ ] **Step 5: 提交**

```bash
cd agentre && git add internal/pkg/hookexec/runner.go internal/pkg/hookexec/runner_test.go
git commit -m "✨ hookexec.Probe:按平台列出解释器及安装情况(cmd/powershell 标 windows-only)"
```

---

## Task 2: Resolve 路径覆盖 + RunSpec 透传

**Files:**
- Modify: `internal/pkg/hookexec/runner.go`、`internal/pkg/hookexec/exec.go`
- Test: `internal/pkg/hookexec/runner_test.go`、`internal/pkg/hookexec/exec_test.go`

**Interfaces:**
- Consumes: Task 1 的 registry。
- Produces:
  - `func Resolve(interpreter, interpreterPath string) (*Interp, error)`(路径非空→stat 校验后直接当 Bin;空→沿用 LookPath)
  - `RunSpec` 新增字段 `InterpreterPath string`

- [ ] **Step 1: 写失败测试**

在 `internal/pkg/hookexec/runner_test.go` 追加:

```go
func TestResolve_PathOverrideUsesGivenBinary(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("sh not on PATH")
	}
	in, err := Resolve("python", sh) // 借 sh 当 python 二进制,验证「路径覆盖」生效
	if err != nil {
		t.Fatalf("Resolve override: %v", err)
	}
	if in.Bin != sh {
		t.Errorf("Bin = %q, want override %q", in.Bin, sh)
	}
	if in.Ext != ".py" {
		t.Errorf("Ext = %q, want preset .py", in.Ext) // args/ext 仍取自预设
	}
}

func TestResolve_PathOverrideMissingFile(t *testing.T) {
	_, err := Resolve("python", "/no/such/bin")
	if !errors.Is(err, ErrInterpreterNotInstalled) {
		t.Errorf("err = %v, want ErrInterpreterNotInstalled", err)
	}
}
```

> `internal/pkg/hookexec/runner_test.go` 顶部 import 需含 `"os/exec"`(若未导入则补上);`errors` 已在用。

- [ ] **Step 2: 跑测试确认失败**

Run: `cd agentre && go test ./internal/pkg/hookexec/ -run TestResolve_PathOverride`
Expected: FAIL —「too many arguments in call to Resolve」编译不过。

- [ ] **Step 3: 实现路径覆盖 + RunSpec 字段 + 改既有调用**

`runner.go` 顶部 import 增加 `"os"`、`"strings"`;`Resolve` 改为:

```go
// Resolve 校验解释器并解析其二进制路径;interpreterPath 非空时覆盖二进制(args/ext 仍取预设)。
func Resolve(interpreter, interpreterPath string) (*Interp, error) {
	def, ok := registry[interpreter]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownInterpreter, interpreter)
	}
	if p := strings.TrimSpace(interpreterPath); p != "" {
		if info, err := os.Stat(p); err != nil || info.IsDir() {
			return nil, fmt.Errorf("%w: %q", ErrInterpreterNotInstalled, p)
		}
		return &Interp{Bin: p, Args: def.args, Ext: def.ext}, nil
	}
	for _, name := range def.candidates {
		if bin, err := exec.LookPath(name); err == nil {
			return &Interp{Bin: bin, Args: def.args, Ext: def.ext}, nil
		}
	}
	return nil, fmt.Errorf("%w: %q", ErrInterpreterNotInstalled, interpreter)
}
```

`exec.go`:`RunSpec` 加字段、`Run` 改调用:

```go
type RunSpec struct {
	Interpreter     string
	InterpreterPath string
	Command         string
	Env             map[string]string
	Timeout         time.Duration
	MaxOutputBytes  int
}
```

> 注意:`RunSpec` 定义在 `runner.go`(不是 `exec.go`),按实际位置改;字段加在 `Interpreter` 之后。

`exec.go:20` 改:

```go
	in, err := Resolve(spec.Interpreter, spec.InterpreterPath)
```

改既有测试里的旧调用(补第二参 `""`):
- `runner_test.go`:`Resolve("ruby")`→`Resolve("ruby", "")`;`Resolve("sh")`→`Resolve("sh", "")`;`Resolve("pwsh")`→`Resolve("pwsh", "")`。
- `exec_test.go`:三处 `Resolve("sh")`→`Resolve("sh", "")`。

- [ ] **Step 4: 跑测试确认通过**

Run: `cd agentre && go test ./internal/pkg/hookexec/ -v`
Expected: PASS(全部,含新旧用例)。

- [ ] **Step 5: 提交**

```bash
cd agentre && git add internal/pkg/hookexec/
git commit -m "✨ hookexec.Resolve:支持二进制路径覆盖(预设仍管 args+ext)+ RunSpec.InterpreterPath"
```

---

## Task 3: 迁移 + entity 加 interpreter_path

**Files:**
- Create: `migrations/202606250001_hook_interpreter_path.go`、`migrations/202606250001_hook_interpreter_path_test.go`
- Modify: `migrations/migrations.go`、`internal/model/entity/hook_entity/hook.go`

**Interfaces:**
- Produces:`hooks.interpreter_path` 列(TEXT NOT NULL DEFAULT '');`hook_entity.Hook.InterpreterPath string`。

- [ ] **Step 1: 写失败测试**

新建 `migrations/202606250001_hook_interpreter_path_test.go`:

```go
package migrations

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMigration202606250001(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, RunMigrations(gdb))

	require.True(t, gdb.Migrator().HasColumn("hooks", "interpreter_path"),
		"expected hooks.interpreter_path column")

	// 默认空串可插入。
	require.NoError(t, gdb.Exec(
		`INSERT INTO hooks(name,interpreter,command,trigger_type,schedule_expr,timezone,
		 env_json,state_json,next_run_at,enabled,status,createtime,updatetime)
		 VALUES ('j','python','x','schedule','* * * * *','UTC','[]','{}',0,1,1,0,0)`).Error)

	var path string
	require.NoError(t, gdb.Raw(`SELECT interpreter_path FROM hooks WHERE name='j'`).Scan(&path).Error)
	require.Equal(t, "", path)
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd agentre && go test ./migrations/ -run TestMigration202606250001`
Expected: FAIL —「undefined: migration202606250001」或 HasColumn 为 false。

- [ ] **Step 3: 实现迁移 + 注册 + entity 字段**

新建 `migrations/202606250001_hook_interpreter_path.go`:

```go
package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// migration202606250001 hooks 加 interpreter_path:覆盖解释器二进制路径(空=LookPath 自动解析)。
func migration202606250001() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202606250001",
		Migrate: func(tx *gorm.DB) error {
			return tx.Exec(`ALTER TABLE hooks ADD COLUMN interpreter_path TEXT NOT NULL DEFAULT ''`).Error
		},
		Rollback: func(tx *gorm.DB) error {
			return tx.Exec(`ALTER TABLE hooks DROP COLUMN interpreter_path`).Error
		},
	}
}
```

`migrations/migrations.go` 的 `migrationList()` 末尾(`migration202606240004()` 之后)追加:

```go
		migration202606250001(), // hooks.interpreter_path:自定义解释器二进制路径
```

`internal/model/entity/hook_entity/hook.go` 在 `Interpreter` 字段之后插入:

```go
	InterpreterPath string `gorm:"column:interpreter_path;type:text;not null;default:''"`
```

> `Hook.Check` 不改:路径有效性留给运行时 `Resolve` 报错(entity 不碰文件系统)。

- [ ] **Step 4: 跑测试确认通过**

Run: `cd agentre && go test ./migrations/ -run TestMigration202606250001 -v`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
cd agentre && git add migrations/202606250001_hook_interpreter_path.go migrations/202606250001_hook_interpreter_path_test.go migrations/migrations.go internal/model/entity/hook_entity/hook.go
git commit -m "✨ hooks 加 interpreter_path 列 + entity 字段(迁移 202606250001)"
```

---

## Task 4: hook_svc 持久化 + 透传 InterpreterPath

**Files:**
- Modify: `internal/service/hook_svc/types.go`、`internal/service/hook_svc/hook.go`、`internal/service/hook_svc/run.go`
- Test: `internal/service/hook_svc/run_test.go`

**Interfaces:**
- Consumes: Task 2 `RunSpec.InterpreterPath`;Task 3 `hook_entity.Hook.InterpreterPath`。
- Produces:`HookItem.InterpreterPath`、`CreateHookRequest.InterpreterPath`(JSON `interpreterPath`);`executeHook` 把 `h.InterpreterPath` 塞进 `RunSpec`。

- [ ] **Step 1: 写失败测试**

在 `internal/service/hook_svc/run_test.go` 追加一个会捕获 spec 的 runner 与用例:

```go
type captureRunner struct {
	res  *hookexec.RunResult
	spec hookexec.RunSpec
}

func (c *captureRunner) Run(_ context.Context, spec hookexec.RunSpec) (*hookexec.RunResult, error) {
	c.spec = spec
	return c.res, nil
}

func TestRunHook_ThreadsInterpreterPath(t *testing.T) {
	ctrl := gomock.NewController(t)
	mh := mock_hook_repo.NewMockHookRepo(ctrl)
	hook_repo.RegisterHook(mh)
	hook_repo.RegisterHookEvent(mock_hook_repo.NewMockHookEventRepo(ctrl))

	mh.EXPECT().Find(gomock.Any(), int64(1)).Return(&hook_entity.Hook{
		ID: 1, Name: "j", Interpreter: "python", InterpreterPath: "/opt/py/bin/python3",
		Command: "x", EnvJSON: "[]", StateJSON: "{}",
	}, nil)

	cap := &captureRunner{res: &hookexec.RunResult{ExitCode: 0}}
	svc := &hookSvc{now: func() int64 { return 1000 }, runner: cap}

	if _, err := svc.RunHook(context.Background(), &RunHookRequest{ID: 1, DryRun: true}); err != nil {
		t.Fatalf("RunHook: %v", err)
	}
	if cap.spec.InterpreterPath != "/opt/py/bin/python3" {
		t.Errorf("spec.InterpreterPath = %q, want threaded path", cap.spec.InterpreterPath)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd agentre && go test ./internal/service/hook_svc/ -run TestRunHook_ThreadsInterpreterPath`
Expected: FAIL —`spec.InterpreterPath = ""`(executeHook 还没透传)。

- [ ] **Step 3: 实现透传 + DTO + 持久化投影**

`run.go` 的 `executeHook` 里 `RunSpec{...}` 字面量加一行:

```go
		Interpreter:     h.Interpreter,
		InterpreterPath: h.InterpreterPath,
```

`types.go`:`HookItem` 在 `Interpreter` 后加字段;`CreateHookRequest` 在 `Interpreter` 后加字段:

```go
	// HookItem 内:
	Interpreter     string   `json:"interpreter"`
	InterpreterPath string   `json:"interpreterPath"`
```
```go
	// CreateHookRequest 内:
	Interpreter     string   `json:"interpreter" binding:"required"`
	InterpreterPath string   `json:"interpreterPath"`
```

`hook.go`:
- `CreateHook` 的 `&hook_entity.Hook{...}` 字面量加 `InterpreterPath: strings.TrimSpace(req.InterpreterPath),`(放在 `Interpreter:` 后)。
- `UpdateHook` 加 `h.InterpreterPath = strings.TrimSpace(req.InterpreterPath)`(放在 `h.Interpreter = ...` 后)。
- `toHookItem` 的 `&HookItem{...}` 加 `InterpreterPath: h.InterpreterPath,`(放在 `Interpreter: h.Interpreter` 同段)。

- [ ] **Step 4: 跑测试确认通过**

Run: `cd agentre && go test -race ./internal/service/hook_svc/ -v`
Expected: PASS(含新用例与原有用例)。

- [ ] **Step 5: 提交**

```bash
cd agentre && git add internal/service/hook_svc/types.go internal/service/hook_svc/hook.go internal/service/hook_svc/run.go internal/service/hook_svc/run_test.go
git commit -m "✨ hook_svc:持久化 interpreterPath 并透传进 RunSpec"
```

---

## Task 5: hook_svc.ProbeInterpreters + App 绑定

**Files:**
- Modify: `internal/service/hook_svc/types.go`、`internal/service/hook_svc/hook.go`、`internal/app/hook.go`
- Test: `internal/service/hook_svc/hook_test.go`

**Interfaces:**
- Consumes: Task 1 `hookexec.Probe`/`hookexec.Available`。
- Produces:
  - `type InterpreterOption struct { Key string; Path string; Installed bool }`(JSON `key`/`path`/`installed`)
  - `HookSvc.ProbeInterpreters(ctx context.Context) ([]InterpreterOption, error)`
  - `App.ProbeInterpreters() ([]hook_svc.InterpreterOption, error)`

- [ ] **Step 1: 写失败测试**

在 `internal/service/hook_svc/hook_test.go` 追加(若文件无 import `runtime` 则补):

```go
func TestProbeInterpreters_ReturnsPlatformList(t *testing.T) {
	got, err := Hook().ProbeInterpreters(context.Background())
	if err != nil {
		t.Fatalf("ProbeInterpreters: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("expected non-empty interpreter list")
	}
	for _, o := range got {
		if _, ok := hook_entity.ValidInterpreters[o.Key]; !ok {
			t.Errorf("returned key %q not in ValidInterpreters allowlist", o.Key)
		}
	}
	if runtime.GOOS != "windows" {
		for _, o := range got {
			if o.Key == "cmd" || o.Key == "powershell" {
				t.Errorf("non-windows must hide %q", o.Key)
			}
		}
	}
}
```

> import 需含 `"runtime"` 与 `"github.com/agentre-ai/agentre/internal/model/entity/hook_entity"`(若 `hook_test.go` 已 import 后者则复用)。

- [ ] **Step 2: 跑测试确认失败**

Run: `cd agentre && go test ./internal/service/hook_svc/ -run TestProbeInterpreters`
Expected: FAIL —「Hook().ProbeInterpreters undefined」。

- [ ] **Step 3: 实现 DTO + 接口 + 方法 + 绑定**

`types.go` 末尾加:

```go
// InterpreterOption 是某解释器在本机的可用性,供前端下拉过滤/置灰/占位。
type InterpreterOption struct {
	Key       string `json:"key"`
	Path      string `json:"path"`
	Installed bool   `json:"installed"`
}
```

`hook.go`:
- `HookSvc` 接口加一行 `ProbeInterpreters(ctx context.Context) ([]InterpreterOption, error)`。
- import 增加 `"runtime"`。
- 实现:

```go
func (s *hookSvc) ProbeInterpreters(_ context.Context) ([]InterpreterOption, error) {
	avail := hookexec.Probe(runtime.GOOS)
	out := make([]InterpreterOption, 0, len(avail))
	for _, a := range avail {
		out = append(out, InterpreterOption{Key: a.Key, Path: a.Path, Installed: a.Installed})
	}
	return out, nil
}
```

`internal/app/hook.go` 加绑定:

```go
// ProbeInterpreters 返回本平台可用的解释器及其安装情况(供 Hook 表单下拉)。
func (a *App) ProbeInterpreters() ([]hook_svc.InterpreterOption, error) {
	return hook_svc.Hook().ProbeInterpreters(a.ctx)
}
```

- [ ] **Step 4: 跑测试确认通过 + 刷新绑定**

Run: `cd agentre && go test ./internal/service/hook_svc/ -run TestProbeInterpreters -v && make generate`
Expected: 测试 PASS;`make generate` 在 `frontend/wailsjs/go/app/App.*` 生成 `ProbeInterpreters`。

- [ ] **Step 5: 提交**

```bash
cd agentre && git add internal/service/hook_svc/types.go internal/service/hook_svc/hook.go internal/service/hook_svc/hook_test.go internal/app/hook.go frontend/wailsjs
git commit -m "✨ hook_svc.ProbeInterpreters + App 绑定:按平台返回解释器可用性"
```

---

## Task 6: i18n 文案去重 + INTERP_META 视觉区分

**Files:**
- Modify: `frontend/src/i18n/locales/zh-CN/common.json`、`frontend/src/i18n/locales/en/common.json`、`frontend/src/components/agentre/hooks-page.tsx`
- Test: `frontend/src/__tests__/i18n.test.ts`(已有,自动校验 key 覆盖)

**Interfaces:**
- Produces: i18n key `hooks.interp.notInstalled`、`hooks.trigger.interpreterPath`、`hooks.trigger.interpreterPathAuto`、`hooks.trigger.interpreterPathPlaceholder`;`pwsh`/`powershell` 标签区分;`INTERP_META` 两者不同 abbrev/color。

- [ ] **Step 1: 改文案(zh + en)**

`frontend/src/i18n/locales/zh-CN/common.json` 的 `hooks.interp`:

```json
      "pwsh": "PowerShell",
      "powershell": "Windows PowerShell",
      "cmd": "CMD",
      "notInstalled": "未安装"
```

`hooks.trigger`(在该对象内补三个 key,放在 `interpreter` 附近):

```json
      "interpreterPath": "二进制路径",
      "interpreterPathAuto": "自动: {{path}}",
      "interpreterPathPlaceholder": "留空 = 自动解析"
```

`frontend/src/i18n/locales/en/common.json` 对应:

```json
      "pwsh": "PowerShell",
      "powershell": "Windows PowerShell",
      "cmd": "CMD",
      "notInstalled": "Not installed"
```
```json
      "interpreterPath": "Binary path",
      "interpreterPathAuto": "auto: {{path}}",
      "interpreterPathPlaceholder": "Empty = auto-resolve"
```

- [ ] **Step 2: 改 INTERP_META 让两者可区分**

`hooks-page.tsx` 的 `INTERP_META` 两行改成:

```ts
  pwsh: { abbrev: "PS7", icon: Terminal, color: "agent-3" },
  powershell: { abbrev: "PS", icon: Terminal, color: "agent-5" },
```

- [ ] **Step 3: 跑 i18n 测试确认通过**

Run: `cd agentre/frontend && pnpm test -- src/__tests__/i18n.test.ts`
Expected: PASS(zh/en key 一致、无缺失)。

- [ ] **Step 4: 提交**

```bash
cd agentre && git add frontend/src/i18n/locales frontend/src/components/agentre/hooks-page.tsx
git commit -m "🌐 hook 解释器:pwsh/Windows PowerShell 文案与视觉区分 + 路径字段文案"
```

---

## Task 7: 前端探测驱动下拉 + 二进制路径输入

**Files:**
- Modify: `frontend/src/components/agentre/hooks-page.tsx`
- Test: `frontend/src/components/agentre/hooks-page.test.tsx`(新建)

**Interfaces:**
- Consumes: Task 5 `App.ProbeInterpreters`;Task 6 i18n key。
- Produces: 前端 `InterpreterOption` 类型;`HookBridge.ProbeInterpreters`;`Draft.interpreterPath`、`HookWriteRequest.interpreterPath`、`HookItem.interpreterPath`;`ScriptTab` 新增 `interpreters` prop。

- [ ] **Step 1: 写失败测试**

新建 `frontend/src/components/agentre/hooks-page.test.tsx`。它通过 `window.go.app.App` 注入桩 bridge(组件用 `getBridge()` 读取,不直接 import wailsjs runtime,故无需全局 alias;但仍 per-file 自包含 mock):

```tsx
import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { HooksPage } from "./hooks-page";

function installBridge(over: Partial<Record<string, unknown>> = {}) {
  const App = {
    LoadHooks: vi.fn().mockResolvedValue({ hooks: [], events: [] }),
    ProbeInterpreters: vi.fn().mockResolvedValue([
      { key: "bash", path: "/bin/bash", installed: true },
      { key: "python", path: "/usr/bin/python3", installed: true },
      { key: "pwsh", path: "", installed: false },
    ]),
    CreateHook: vi.fn(),
    UpdateHook: vi.fn(),
    DeleteHook: vi.fn(),
    ToggleHook: vi.fn(),
    RunHook: vi.fn(),
    ...over,
  };
  (window as unknown as { go: { app: { App: unknown } } }).go = {
    app: { App },
  };
  return App;
}

describe("HooksPage interpreter dropdown", () => {
  beforeEach(() => installBridge());

  it("lists probed interpreters and disables not-installed", async () => {
    render(<HooksPage />);
    fireEvent.click(await screen.findByTestId("hook-create"));
    fireEvent.click(await screen.findByLabelText("解释器"));
    // 已装项可选;未装的 pwsh 置灰禁用。
    const pwshOption = await screen.findByText("PowerShell");
    expect(pwshOption.closest("[role=option]")).toHaveAttribute(
      "aria-disabled",
      "true",
    );
  });

  it("submits interpreterPath in payload", async () => {
    const App = installBridge();
    render(<HooksPage />);
    fireEvent.click(await screen.findByTestId("hook-create"));
    fireEvent.change(await screen.findByLabelText("二进制路径"), {
      target: { value: "/opt/py/bin/python3" },
    });
    fireEvent.click(screen.getByTestId("hook-save"));
    await waitFor(() =>
      expect(App.CreateHook).toHaveBeenCalledWith(
        expect.objectContaining({ interpreterPath: "/opt/py/bin/python3" }),
      ),
    );
  });
});
```

> 实测时按 `hooks-page.tsx` 现有 `data-testid` / `aria-label` 调整选择器(创建按钮、保存按钮、解释器 Select、路径 Input)。若现缺 `data-testid`,Step 3 顺手补上 `hook-create`/`hook-save` 两个最小 testid(属于本任务范围)。

- [ ] **Step 2: 跑测试确认失败**

Run: `cd agentre/frontend && pnpm test -- src/components/agentre/hooks-page.test.tsx`
Expected: FAIL —「二进制路径」label 不存在 / `ProbeInterpreters` 未被调用。

- [ ] **Step 3: 实现前端接线**

3a. 类型 + bridge(`hooks-page.tsx` 顶部域类型区):

```ts
type InterpreterOption = { key: string; path: string; installed: boolean };
```
`HookItem`、`HookWriteRequest`、`Draft` 各加 `interpreterPath: string;`(放在 `interpreter` 后)。
`HookBridge` 加:

```ts
  ProbeInterpreters: () => Promise<InterpreterOption[]>;
```

3b. draft 工厂:`draftFromHook` 加 `interpreterPath: h.interpreterPath ?? "",`;`emptyDraft` 加 `interpreterPath: "",`。

3c. `save()` 的 `payload` 加 `interpreterPath: draft.interpreterPath,`。

3d. 顶层组件(含 `startCreate`/`save`/`reload` 的那个)加探测 state 与挂载拉取:

```tsx
  const [interpreters, setInterpreters] = useState<InterpreterOption[]>([]);
  useEffect(() => {
    getBridgeMethod("ProbeInterpreters")()
      .then(setInterpreters)
      .catch(() => setInterpreters([]));
  }, []);
```

把 `interpreters` 透传给渲染 `ScriptTab` 处:`<ScriptTab draft={draft} onChange={setDraft} interpreters={interpreters} t={t} />`。

3e. `ScriptTab` 签名加 `interpreters: InterpreterOption[]`,并改解释器下拉 + 加路径框。把 `INTERP_OPTIONS.map(...)` 替换为探测列表;另确保「当前选中的解释器即便被平台过滤掉也出现」:

```tsx
function ScriptTab({
  draft,
  onChange,
  interpreters,
  t,
}: {
  draft: Draft;
  onChange: (next: Draft) => void;
  interpreters: InterpreterOption[];
  t: TFunction;
}) {
  const options =
    interpreters.some((o) => o.key === draft.interpreter) ||
    interpreters.length === 0
      ? interpreters
      : [
          { key: draft.interpreter, path: "", installed: false },
          ...interpreters,
        ];
  const selected = options.find((o) => o.key === draft.interpreter);
```

解释器 `<SelectContent>` 内:

```tsx
              <SelectContent>
                {options.map((opt) => (
                  <SelectItem
                    key={opt.key}
                    value={opt.key}
                    disabled={!opt.installed}
                  >
                    {t(`hooks.interp.${opt.key}`)}
                    {!opt.installed && (
                      <span className="ml-1.5 text-[10px] text-muted-foreground">
                        {t("hooks.interp.notInstalled")}
                      </span>
                    )}
                  </SelectItem>
                ))}
              </SelectContent>
```

在解释器 `</label>` 之后、时区 `<label>` 之前插入「二进制路径」框:

```tsx
          <label className="flex flex-col gap-1">
            <span className="text-[11px] text-muted-foreground">
              {t("hooks.trigger.interpreterPath")}
            </span>
            <Input
              value={draft.interpreterPath}
              onChange={(e) =>
                onChange({ ...draft, interpreterPath: e.target.value })
              }
              placeholder={
                selected?.installed && selected.path
                  ? t("hooks.trigger.interpreterPathAuto", {
                      path: selected.path,
                    })
                  : t("hooks.trigger.interpreterPathPlaceholder")
              }
              className="w-72 font-mono text-xs"
              aria-label={t("hooks.trigger.interpreterPath")}
            />
          </label>
```

3f. 若 Step 1 测试需要,补 `data-testid="hook-create"`(新建按钮)、`data-testid="hook-save"`(保存按钮)到对应元素。

- [ ] **Step 4: 跑测试确认通过**

Run: `cd agentre/frontend && pnpm test -- src/components/agentre/hooks-page.test.tsx`
Expected: PASS(两个用例)。

- [ ] **Step 5: 提交**

```bash
cd agentre && git add frontend/src/components/agentre/hooks-page.tsx frontend/src/components/agentre/hooks-page.test.tsx
git commit -m "✨ hook 表单:探测驱动解释器下拉(平台过滤+未装置灰)+ 二进制路径输入"
```

---

## Task 8: 全量校验

**Files:** 无(校验)

- [ ] **Step 1: 后端全量测试**

Run: `cd agentre && make test-backend`
Expected: PASS。

- [ ] **Step 2: 前端测试 + lint**

Run: `cd agentre && make test-frontend && make lint`
Expected: PASS(含 `i18n.test.ts`、`no-literal-string`、golangci-lint v2)。

- [ ] **Step 3: 收尾提交(若 lint-fix 有格式改动)**

```bash
cd agentre && git add -A && git commit -m "✅ hook 解释器自定义路径:全量测试+lint 通过" || echo "无额外改动"
```

---

## Self-Review(已核对)

- **Spec 覆盖**:两个 PowerShell→Task 6(文案)+Task 1(平台);自定义路径→Task 2/3/4(后端)+Task 7(前端);平台感知→Task 1(Probe)+Task 5(绑定)+Task 7(下拉)。三件事全覆盖。
- **占位扫描**:无 TBD/TODO;每个改代码步骤含完整代码。
- **类型一致**:`Available`(hookexec)→`InterpreterOption`(hook_svc DTO)→前端 `InterpreterOption`,字段 `key/path/installed` 全程一致;`Resolve(interpreter, interpreterPath string)` 双参在 Task 2 定义、Task 2 内更新全部既有调用点;`RunSpec.InterpreterPath` 在 Task 2 定义、Task 4 使用;`Hook.InterpreterPath` 在 Task 3 定义、Task 4 读写。
