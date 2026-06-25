# Hook 解释器:自定义路径 + 平台感知 + 文案去重

- 日期:2026-06-25
- 范围:`internal/pkg/hookexec`、`internal/model/entity/hook_entity`、`internal/service/hook_svc`、`internal/app/hook.go`、`migrations/`、`frontend/src/components/agentre/hooks-page.tsx`、i18n
- 状态:已与用户确认,待写实现计划

## 背景与动机

脚本驱动 Hook(见 `2026-06-24-hooks-script-driven-redesign-design.md`)的解释器存在三个问题:

1. **「两个 PowerShell」看着像重复 bug。** `hookexec` 注册表里 `pwsh`(PowerShell 7+/Core,跨平台)和 `powershell`(Windows PowerShell 5.1,仅 Windows)是两个不同的程序,但 i18n 标签都写成字面 `"PowerShell"`(`common.json` zh/en 1104-1105),`INTERP_META` 又给它们相同的 `PS` 缩写 / 图标 / `agent-3` 颜色 → 下拉框出现两行完全一样的 "PowerShell"。这是**文案没区分**,不是真重复。

2. **无法自定义解释器路径。** 解释器是 allowlist 关键字(`hook_entity.ValidInterpreters`),真实二进制由后端 `hookexec.Resolve` 内部 `exec.LookPath(candidates...)` 解析,前端没有路径字段,选中后也看不到 / 改不了解析到的路径(例如 pyenv 的 python、自定义 node)。

3. **无平台感知。** 下拉框无条件列全部 7 个(`INTERP_OPTIONS`),不看 GOOS 也不看实际是否安装。macOS 上选 `powershell` / `cmd` 必然在运行时报 `interpreter not installed`,Windows 上选 `bash` / `sh` 同理。

三者本质同源,统一用一个「后端探测可用解释器」的能力解决。

## 确定的设计决策

经与用户确认:

- **自定义范围 = 仅路径覆盖。** 保留预设下拉(预设仍决定**启动参数 + 临时脚本扩展名**),额外加一个可编辑「二进制路径」字段;不做完全任意的新解释器(deno/ruby/php 等需要自填 args+ext,YAGNI)。
- **可用性呈现 = 按平台隐藏 + 未装置灰。** 不属于本 OS 的预设直接不渲染;属于本平台但未安装的置灰禁用 + 提示。
- **路径框 = 探测路径作占位预览,值留空 = 自动。** 默认可移植(换机器 / 路径变动不会失效),同时让用户看到真实解析路径;想钉死才手动输入。
- **文案去重。** `pwsh` → "PowerShell"、`powershell` → "Windows PowerShell";`INTERP_META` 给两者不同缩写 / 颜色。

## 架构

数据流(本机执行,远端 daemon 暂不在范围内):

```
hookexec.registry (单一事实源: candidates + args + ext + goos)
        │
        ├── Probe(goos)  ── hook_svc.ProbeInterpreters ── App.ProbeInterpreters ──> 前端下拉(过滤+置灰+占位)
        │
        └── Resolve(interpreter, path) ── hookexec.osScriptRunner.Run ── hook_svc.executeHook ──> 子进程
```

### 1. 数据模型

`hook_entity.Hook` 新增一列:

```go
InterpreterPath string `gorm:"column:interpreter_path;type:text;not null;default:''"`
```

语义:
- `interpreter`(预设关键字)**仍然决定** args + ext,`ValidInterpreters` allowlist 不变。
- `interpreter_path` 只覆盖**二进制本身**:
  - 留空 → 按现状 `LookPath(candidates...)` 自动解析(可移植)。
  - 非空 → 严格用这个路径作为 Bin。

`Hook.Check` 不新增对 `interpreter_path` 的校验(entity 不碰文件系统);路径有效性在运行时由 `Resolve` 报错。

**迁移**:追加 `migration202606250001()` 到 `migrationList()` 末尾(当前末尾是 `202606240003`),native SQL `ALTER TABLE hooks ADD COLUMN interpreter_path TEXT NOT NULL DEFAULT ''`,Rollback `ALTER TABLE hooks DROP COLUMN interpreter_path`(沿用仓库现有 ADD/DROP COLUMN 惯例,如 `202606100001`)。**不修改任何已存在迁移**。

### 2. 后端:探测能力与路径覆盖(`internal/pkg/hookexec`)

注册表每行加平台字段(空 = 全平台):

```go
type interpDef struct {
    candidates []string
    args       []string
    ext        string
    goos       []string // 空=全平台;仅 cmd/powershell 标 {"windows"}
}
```

平台归属:
- windows-only(非 windows 隐藏):`cmd`、`powershell`
- 全平台(总是列出,未装置灰):`bash`、`sh`、`node`、`python`、`pwsh`(`pwsh` 是跨平台 Core,mac/linux 可装)

新增探测函数(`goos` 入参,**不依赖编译期常量**,可单测):

```go
type Available struct {
    Key       string
    Path      string // LookPath 解析到的路径,未装为 ""
    Installed bool
}

func Probe(goos string) []Available
```

`Probe` 遍历注册表 → 用 `goos` 过滤掉不适用的行 → 对每行 `exec.LookPath(candidates...)` → 组装结果(保持稳定顺序)。

`Resolve` 增加路径覆盖:

```go
func Resolve(interpreter, interpreterPath string) (*Interp, error)
```

- `interpreter` 仍校验 allowlist 取 args+ext。
- `interpreterPath` 非空:`os.Stat` 校验存在(不存在返回 `ErrInterpreterNotInstalled` 并带上该路径),直接用作 `Bin`。
- `interpreterPath` 空:沿用现有 `LookPath(candidates...)`。

`RunSpec` 加 `InterpreterPath string`;`osScriptRunner.Run` 调 `Resolve(spec.Interpreter, spec.InterpreterPath)`。

### 3. 服务层与绑定(`hook_svc` + `internal/app/hook.go`)

- `hook_svc.executeHook`(`run.go:50`)构造 `RunSpec` 时透传 `InterpreterPath: h.InterpreterPath`。
- `CreateHook` / `UpdateHook`(`hook.go`)读写 `req.InterpreterPath`(`TrimSpace`);`HookItem` DTO 与 hooktool_svc 的 MCP/approval 透传链路补上该字段。
- 新增 `hook_svc.Hook().ProbeInterpreters(ctx) ([]InterpreterOption, error)`:调 `hookexec.Probe(runtime.GOOS)`,映射为 DTO(含 key、是否安装、探测路径)。
- `App.ProbeInterpreters()` 绑定暴露(`internal/app/hook.go`),仅 parse→svc→return。
- `make generate` 刷新 `frontend/wailsjs` 绑定。

### 4. 前端(`hooks-page.tsx` + i18n)

- 表单挂载(或打开编辑器)时调 `ProbeInterpreters()`,用结果**取代写死的 `INTERP_OPTIONS`**:
  - 后端没返回的预设(不属于本 OS)→ 不渲染。
  - `Installed=false` 的 → `SelectItem` `disabled` + 一行「未安装」提示文案。
- 新增「二进制路径」`Input`(绑 `draft.interpreterPath`):
  - 选预设时,用该预设的探测路径作 `placeholder`(如 `auto: /opt/homebrew/bin/python3`),**值不自动写入**;留空 = 自动解析。
  - 用户手输路径则覆盖。
- 文案去重:
  - `hooks.interp.pwsh` → "PowerShell"(zh/en)
  - `hooks.interp.powershell` → "Windows PowerShell"(zh/en)
  - `INTERP_META`:给 `pwsh` / `powershell` 不同 `abbrev`(如 `PS7` / `PS`)与颜色以便视觉区分。
- 新增 i18n key(zh-CN + en 双语):路径字段 label、placeholder 前缀、「未安装」提示。`i18n.test.ts` 与 `make lint`(`no-literal-string`)需通过。

## 测试(TDD,Red → Green)

**`internal/pkg/hookexec`(runner_test.go / exec_test.go)**
- `Probe("darwin")`:不含 `cmd` / `powershell`;含 `bash` 且 `Installed=true`(CI 上 bash 必在)。
- `Probe("windows")`:含 `cmd` / `powershell`。
- `Resolve("python", "/custom/python3")`:Bin = 给定路径(存在时);不存在 → `ErrInterpreterNotInstalled` 且错误信息含该路径。
- `Resolve("python", "")`:回退 LookPath。
- 未知 key 仍 → `ErrUnknownInterpreter`。

**`internal/service/hook_svc`**
- `ProbeInterpreters` 返回探测结果映射(可用 fake/真实 GOOS)。
- `RunHook` / `executeHook` 把 `InterpreterPath` 透传进 `RunSpec`(用 runner mock 断言收到的 spec)。
- `Create/UpdateHook` 持久化 `interpreter_path`(repo mock / sqlmock 断言 SQL 含该列)。

**前端(hooks-page.test.tsx,per-file `vi.mock` wails runtime)**
- 挂载后只渲染探测返回的可用项;未装项 disabled。
- 选不同预设 → 路径框 placeholder 随之变化、值仍为空。
- 手输自定义路径 → 提交 payload 含 `interpreterPath`。
- 两个 PowerShell 文案不同(Windows 场景 mock)。

**迁移自测**:`202606250001_*_test.go` 跑迁移后查 `hooks` 含 `interpreter_path` 列、默认 `''`。

## 范围外(明确不做)

- 远端 agentred daemon 上探测 / 跑脚本(探测仅本机)。
- 完全任意新解释器(自填 args + ext)。
- 路径自动补全 / 浏览文件选择器(纯文本输入即可)。

## 风险与缓解

- **stale 路径**:默认留空 = 自动解析,天然规避;只有用户主动钉死路径才会随机器变动失效,符合预期。
- **安全**:自定义二进制路径相比「已能执行任意脚本」未新增攻击面(脚本本就是 Hook 的功能本体,见 `exec.go:42` 的 gosec nolint 说明)。allowlist 仍约束 args+ext 来自已知预设。
- **迁移 ledger**:仅追加、不动旧迁移,避免历史上群协作迁移 squash 时那类 dup column 重跑事故。
