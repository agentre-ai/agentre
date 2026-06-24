# Hooks 模块重构：脚本驱动的可调度 Hook 设计

日期：2026-06-24
状态：已与用户确认设计方向（含 UI mockup）

## 1. 背景与目标

现有 Hooks 模块是「信号源 + 路由规则 + 事件日志」三段式：

- 三张表 `hook_sources` / `hook_rules` / `hook_events`；
- 声明了 `email/github/slack/schedule/webhook/system` 六种 source kind，但**只有 email 真能拉数据**（`email.go` 里的 IMAP 轮询器），其余全是空壳占位；
- 路由是手写的弱 DSL（`field contains "x"` / `field = "y"`）；
- dispatch 是死桩——命中事件永远 `"Agent runtime dispatch is not enabled yet."`，从不真正起 Agent。

用户确认的三个真实痛点（按主次）：

1. **主线：加新源不该改 Go 发版。** 每加一个数据源都要写 Go fetcher + 改 `SourceConfig` + 重新发版。核心诉求是**开放性**——用户/AI 自己能接任意数据源。
2. condition DSL 太弱，表达不了真实处理/过滤逻辑。
3. source/rule/event 三段过重。

> 注：`agentre` **尚未发布**，迁移/重构可**硬删旧数据、无需兼容层**（见 [[project_release_status]]）。

### 已锁定的产品决策

| 决策点 | 结论 |
| --- | --- |
| 形态 | **彻底统一**：砍掉 source kind 连接器模型，塌成单一「**脚本 Hook**」概念。**连 email/IMAP 一并删除**——以后要 email，就是再写一个脚本 Hook（IMAP node 库或 provider API）。 |
| 执行运行时 | **真子进程，按 hook 声明的解释器执行。** 每个 hook 自带 `interpreter` 字段（`bash`/`sh`/`node`/`python`/`pwsh`/`powershell`/`cmd`…，allowlist），host 把脚本写临时文件后用该解释器跑。**不引入嵌入式沙箱解释器。** 理由：agentre 本就在用户机上跑 claude-code/codex 子进程（claude-code 自己就能跑任意命令），信任边界早已跨过。**全平台支持**：Windows 上用 `node`/`python`/`pwsh`/`cmd` 声明，macOS/Linux 用 `bash`/`sh`/`node`/`python`——env-in/stdout-out 契约与解释器无关。 |
| 处理终点 | **只产出结构化事件 + 落库/展示**。dispatch（真正起 Agent）**不在本期**——事件先沉淀成日志，未来再接编排（见 §11）。 |
| 触发 | MVP **仅 cron** 调度 + 手动 `hook_run`（试运行 / 立即运行）；数据模型**预留 `trigger_type` 字段**，未来加 webhook 被动推送不用再改表。（砍掉「间隔」二选一，简化为单一 cron 表达式。） |
| Host↔脚本契约 | **env 进、stdout JSON 出**（§4）：与语言无关、零 SDK 安装、可一键试运行。 |
| 数据模型 | 塌成两表：**`hooks`（脚本+调度+env+state）** + **`hook_events`（产出日志）**。删 `hook_sources` / `hook_rules`。 |
| AI 创作面 | 新增内置工具 `hook`（与 `org`/`subagent`/`orchestrate` 并列的 agenttool），让 agent 在对话里 `hook_create` 写脚本、`hook_run` 试跑、注册调度。 |

## 2. 总体架构

```
Hooks 页（脚本编辑器 + 运行日志）            对话里的 Agent（开启 hook 工具）
        │ CreateHook/UpdateHook/RunHook              │ MCP: hook_create / hook_run / …
        ▼                                            ▼
  internal/app/hook.go (Wails 绑定)        gateway /mcp/hook/ ──► hooktool_svc
        │                                            │ (token + 开关门控 + tool_approval)
        └──────────────┬─────────────────────────────┘
                       ▼
                  hook_svc（CRUD + 调度 + 试运行）
                       │ 注入 ScriptRunner 接口（生产=osScriptRunner,测试=fake）
        ┌──────────────┼───────────────────────────┐
        ▼              ▼                             ▼
   hook_repo      Scheduler (ticker)          ScriptRunner
   (sqlmock)      扫 next_run_at ≤ now         按 interpreter 跑临时脚本文件
        │         并发跑 due hooks             env: HOOK_STATE + 各 env/密钥
        ▼              │                       超时/输出上限/超限即杀
   hooks /             ▼                             │ stdout
   hook_events    解析 stdout JSON ──► 去重(dedupeKey)+落库 hook_events
                  ──► 回写 state_json + last_run_* + 重算 next_run_at
```

核心复用现有两条成熟路径：

- **调度**复用 `hook_svc/email.go` 里 `StartEmailPoller → pollDueEmailSources` 的「ticker 扫到期源逐个跑」骨架，泛化成「扫到期 hook 逐个跑脚本」。
- **MCP 工具接入**复用 `agenttool` 注册表 + `BuildTurnMCP` + `RegisterTurnMCPProvider` + 通用 `tool_approval` 写工具审批（见 [[project_agent_tooling_features]]，「第三个写工具直接复用别再搭」）。

## 3. 数据模型

### 3.1 `hooks`（替代 source+rule）

```go
type Hook struct {
    ID           int64
    Name         string // 唯一（同 source 名唯一）
    Interpreter  string // 'bash'|'sh'|'node'|'python'|'pwsh'|'powershell'|'cmd'（allowlist，见 §5）
    Command      string // 脚本正文（按 Interpreter 解释）
    TriggerType  string // 'schedule'（预留 'webhook'）
    ScheduleExpr string // cron 表达式（如 */5 * * * *）
    Timezone     string // cron 时区，默认 Asia/Shanghai
    EnvJSON      string // [{key,value,secret bool}]，secret 落库脱敏返回前端
    StateJSON    string // 不透明游标 blob，脚本上次返回的 state
    NextRunAt    int64
    Enabled      int
    // 最近一次运行（展示在头部 / 列表状态点）
    LastRunAt      int64
    LastStatus     string // 'ok' | 'failed' | 'running' | ''
    LastError      string
    LastDurationMs int64
    TotalCount     int64  // 累计入库事件数
    Status         int    // 软删
    Createtime, Updatetime int64
}
```

- `EnvJSON` 单列承载「环境变量 + 密钥」：`secret=true` 的条目 UI 显示 `••••`，沿用现有 `maskedSecret` + `preserveSourceSecrets` 的「掩码即保留旧值」逻辑（§4.3）。非密钥即普通配置。
- `Interpreter` 取自 allowlist，`Hook.Check` 校验；host 据此选执行方式（§5）。脚本内仍可自由再 shell-out（如 bash 脚本里调 `curl`/`jq`，node 脚本里 `require` 库）。

### 3.2 `hook_events`（产出日志，重建）

```go
type HookEvent struct {
    ID         int64
    HookID     int64
    Title      string // 必填
    DedupeKey  string // 可选；非空时 (hook_id, dedupe_key) 唯一去重
    PayloadJSON string // 脚本产出的任意 JSON
    ReceivedAt int64
    Status     int
    Createtime, Updatetime int64
}
```

- `DedupeKey` 泛化了 email 的 `MessageID/UID` 去重：host 落库前查 `(hook_id, dedupe_key)`，存在则跳过（`FindByDedupeKey`）。空 key 一律插入（不去重）。
- **不设 `hook_runs` 运行历史表**（MVP）：仅在 `hooks` 上保留 `LastRun*`。运行历史是未来增强（§11）。

### 3.3 迁移

新建迁移 `202606240001_hooks_script_redesign.go` 追加到 `migrationList()` 末尾（**不改既有迁移**，紧随 group-features 基线 `202606160001`，见 [[project_migration_squash_group_features]]）：

- `DROP TABLE hook_sources; DROP TABLE hook_rules; DROP TABLE hook_events;`（硬删，未发布无需保数据）
- `CREATE TABLE hooks (...)` + `CREATE TABLE hook_events (...)` + `CREATE UNIQUE INDEX ux_hook_events_dedupe ON hook_events(hook_id, dedupe_key)`（native SQL DDL，不靠 AutoMigrate）。

## 4. Host↔脚本契约

### 4.1 输入（环境变量）

host 起子进程前注入：

- `HOOK_STATE`：该 hook 的 `StateJSON`（空则 `"{}"`）。
- `HOOK_NAME` / `HOOK_ID`：上下文。
- `EnvJSON` 每条 → 一个同名环境变量（含密钥明文，仅进子进程环境）。

### 4.2 输出（stdout 单个 JSON 对象）

```jsonc
{
  "events": [
    { "title": "...", "dedupeKey": "OPS-4821", "payload": { /* 任意 */ } }
  ],
  "state": { "lastKey": "2026-06-24T10:32:00Z" }
}
```

- `events` 可选（默认 `[]`）；每条 `title` 必填，`dedupeKey`/`payload` 可选。
- `state` 可选；存在则整体替换 `StateJSON`。
- **判定**：退出码非 0 → 失败（`LastError` = stderr 尾部）；退出码 0 但 stdout 非合法 JSON → 失败（解析错误）；0 + 合法 JSON → 成功。
- stderr 始终捕获，失败时落 `LastError`，并 `logger.Ctx(ctx)` 记日志。

### 4.3 密钥处理

沿用现状（与 email `AppPassword` 一致）：**明文落库 + UI 脱敏**。`LoadHooks`/`hook_get` 把 `secret=true` 的值替换成 `********`；更新时若传回 `********` 或空则保留旧值（`preserveSecrets`）。**at-rest 加密列为未来增强**（§11），本期不做以保持与既有一致。

## 5. 执行运行时（ScriptRunner 接缝 + 解释器注册表）

```go
type ScriptRunner interface {
    Run(ctx context.Context, spec RunSpec) (*RunResult, error)
}
type RunSpec struct {
    Interpreter    string            // hook 声明的解释器 key
    Command        string            // 脚本正文
    Env            map[string]string
    Timeout        time.Duration     // 默认 30s
    MaxOutputBytes int               // 默认 256KB
}
type RunResult struct {
    Stdout    []byte
    Stderr    []byte
    ExitCode  int
    Duration  time.Duration
    TimedOut  bool
    Truncated bool
}
```

### 5.1 解释器注册表（OCP：加解释器 = 加一条，不改 switch）

`internal/pkg/hookexec` 维护 allowlist，每条描述「如何调用」：

| key | 调用（写临时文件后） | 文件名 | 平台 |
| --- | --- | --- | --- |
| `bash` | `bash <file>` | `*.sh` | mac/linux/(git-bash/WSL) |
| `sh` | `sh <file>` | `*.sh` | mac/linux |
| `node` | `node <file>` | `*.mjs` | 全平台 |
| `python` | `python3`（回退 `python`）`<file>` | `*.py` | 全平台 |
| `pwsh` | `pwsh -File <file>` | `*.ps1` | 全平台（PowerShell 7） |
| `powershell` | `powershell -File <file>` | `*.ps1` | Windows 自带 |
| `cmd` | `cmd /c <file>` | `*.bat` | Windows |

- **写临时文件再执行**（而非 `-c`/`-e` 内联）：跨语言统一、免引号地狱；跑完即删。
- **可用性探测**：`exec.LookPath` 解析声明解释器的二进制；不在 PATH → `hook_run`/调度返回**明确错误**（落 `LastError`），不静默吞。这正是「全平台」的落地方式——hook 声明机器上确实装了的解释器即可。
- 生产实现 `osScriptRunner`：按注册表组 argv，`exec.CommandContext` + 进程组、`ctx` 超时杀整组、`io.LimitReader` 截断 stdout、env 注入。
- `hook_svc` 持有 `ScriptRunner` 字段，默认 `osScriptRunner{}`；**service 单测注入 fake**（镜像现有 `MailFetcher` 接缝，单测**绝不**真起进程）。
- Windows 进程组 kill 走 `taskkill`/job object，Unix 走负 pgid——细节封在 `osScriptRunner` 内的平台分文件（`runner_unix.go` / `runner_windows.go`）。

## 6. 调度器

- `StartScheduler(ctx) context.CancelFunc`：泛化 `pollDueEmailSources`。ticker（15s）扫 `enabled=1 且 next_run_at ≤ now` 的 hook。
- **并发上限** + **不重叠**：内存 inflight set（按 hook id）跳过仍在跑的；并发信号量 `min(4, 核数)`。`LastStatus='running'` 落库供 UI 显示。
- 单次运行流水：标 running → `ScriptRunner.Run` → 解析 stdout → 去重 + 落库 `hook_events`（累加 `TotalCount`）→ 回写 `StateJSON` + `LastRun*` → 重算 `NextRunAt`。
- `NextRunAt` 计算：用 **`robfig/cron/v3`** parser 求 `now` 之后下一个 cron 点（go.mod 现无 cron 依赖，需新增；只用其解析器，调度仍是自家 ticker）。表达式非法 → 落 `LastError` 并按兜底间隔退避，不静默吞。
- 注入 `now func() int64`（现有模式），调度/到期判定全部可测。

## 7. AI 创作面（新域 `hooktool_svc`）

### 7.1 注册表与门控

`internal/pkg/agenttool` 追加：

```go
const KeyHook = "hook"
{Key: KeyHook, MCPPath: "/mcp/hook/", ToolNames: []string{
    "hook_list", "hook_get", "hook_create", "hook_update", "hook_delete", "hook_run"}}
```

- 按 agent `ToolEnabled(KeyHook)` 门控（`tools_json`，**无新 migration**，默认关）；backend 无 `CapMCPTools` 软降级——与 org/subagent 一致。
- bootstrap：`gw.RegisterMCP("/mcp/hook/", hooktool_svc.Default().MCPHandler())` + `chat_svc.RegisterTurnMCPProvider(hooktool_svc.Default().BuildTurnMCP)`。

### 7.2 工具清单（6 个）

| 工具 | 语义 | 写? |
| --- | --- | --- |
| `hook_list` | 列 hook（名/调度/启用/last_status） | 读 |
| `hook_get` | 取单 hook 全文（含 command、env key，密钥脱敏） | 读 |
| `hook_create` | 建 hook（name/command/scheduleKind/scheduleExpr/env） | 写 |
| `hook_update` | 改 hook | 写 |
| `hook_delete` | 删 hook | 写 |
| `hook_run` | 立即执行一次；`dryRun=true`（默认）**不落库/不改 state**，回 stdout + 解析事件 + 去重预览 + 错误，供 AI 边写边验 | 写* |

- 三个写工具（create/update/delete）**复用既有通用 `tool_approval` 门**（不另搭审批）。`hook_run` 是执行——也走审批（首次执行任意脚本应让用户点头）。
- 创作闭环：agent `hook_create` 起草 → `hook_run(dryRun)` 试跑看输出 → 调整 → 注册到调度。Hooks 页是它的可视化与人工接管入口。

## 8. Wails 绑定 + 前端

### 8.1 绑定（`internal/app/hook.go` 重写）

`LoadHooks` / `CreateHook` / `UpdateHook` / `DeleteHook` / `ToggleHook` / `RunHook`（run-now & dry-run）。删除 source/rule/email 相关方法。绑定层只做 parse → `hook_svc.Xxx` → return。

### 8.2 前端（`hooks-page.tsx` 重做）

- 左栏：hook 列表（名 + `<解释器> · 调度` + 状态点 ok/failed/停用）。
- 主面板两 tab：**脚本**（触发 + **解释器选择器**（shadcn `Select`：bash/sh/node/python/pwsh/…）+ 脚本编辑器 + env/密钥 + 内联试运行结果）、**运行日志**（产出事件列表 + 详情 payload，沿用现 `EventLogPanel` 的两栏布局）。
- 删除 source kind / rule / 兜底规则 / IMAP 等所有旧 UI 与文案。新增可见文案全部走 i18n（`zh-CN`/`en` 双份），`i18next/no-literal-string` 把关；`i18n.test.ts` 校 key 覆盖（见 [[reference_frontend_wails_runtime_test_mock]]：渲染 wailsjs 的测试需 per-file mock）。

> 设计稿见本次 mockup（`.superpowers/brainstorm/.../hooks-redesign.html`）与 [[reference_agentry_pen_design_file]]。

## 9. 分层与文件地图

| 层 | 文件 | 动作 |
| --- | --- | --- |
| entity | `internal/model/entity/hook_entity/hook.go` | 重写：`Hook` + `HookEvent`（删 `HookSource`/`HookRule`）+ `Check` |
| migration | `internal/migrate/migrations/202606240001_hooks_script_redesign.go` | 新增：DROP 旧 3 表 + CREATE 新 2 表 + 唯一索引 |
| repo | `internal/repository/hook_repo/*` | 重写：`Hook` CRUD + `ListDue` + `HookEvent` create/`FindByDedupeKey`/list（sqlmock） |
| service | `internal/service/hook_svc/*` | 重写：CRUD + Scheduler + 试运行；删 `email.go` |
| pkg | `internal/pkg/hookexec` | 新增：`ScriptRunner` 接口 + 解释器注册表 + `osScriptRunner`（`runner_unix.go`/`runner_windows.go`） |
| pkg | `internal/pkg/agenttool` | 追加 `KeyHook` 注册表条目 |
| service(MCP) | `internal/service/hooktool_svc/*` | 新增：`MCPHandler` + `BuildTurnMCP` + 6 工具 |
| binding | `internal/app/hook.go` | 重写绑定方法 |
| bootstrap | `internal/bootstrap/cago.go` | 挂 `/mcp/hook/` + `RegisterTurnMCPProvider`；启动 Scheduler（替原 `StartEmailPoller`） |
| frontend | `frontend/src/components/agentre/hooks-page.tsx` + i18n | 重做页面 + 文案 |
| go.mod | — | 新增 `robfig/cron/v3`（cron parser） |

依赖方向不变：`app → hook_svc → hook_repo → entity`；`hooktool_svc` 只调 `hook_svc`，不绕层。

## 10. TDD 测试计划（Red → Green）

1. **entity**：`Hook.Check` / `HookEvent.Check`（名必填、title 必填、JSON 合法、解释器在 allowlist、cron 表达式非空）。
2. **repo（sqlmock）**：Hook CRUD、`ListDue(now)`、HookEvent `Create` / `FindByDedupeKey` / `ListByHook`。
3. **service（fake ScriptRunner，不起真进程）**：
   - 成功：stdout `{events,state}` → 事件解析 + 去重 + 落库 + state 回写 + `LastStatus=ok`；
   - 退出码非 0 → `failed` + `LastError`；stdout 非 JSON → `failed`；超时 → `failed` + `TimedOut`；
   - 去重：相同 `dedupeKey` 第二次跳过；
   - `hook_run(dryRun)` 不落库、不改 state。
   - **解释器解析**：声明的解释器不在 allowlist / 不在 PATH → 明确错误（`hookexec` 注册表查表 + `LookPath` 桩，仍不起真进程）。
4. **scheduler（注入 now + fake runner）**：到期 hook 被跑、`NextRunAt` 重算、运行中不重叠。
5. **hooktool_svc（MCP）**：`BuildTurnMCP` 按开关下发/不下发、token 校验、6 工具路由——镜像 `orgtool_svc/mcp_test.go`。
6. **前端**：页面 vitest + i18n 覆盖测试。

## 11. 不在本期范围（未来）

- **dispatch（派发 Agent）**：从事件起 Agent 的派发**本期完全不做**，事件只落日志；以后要做再单独设计（暂无设计）。
- **webhook 被动推送**：`trigger_type` 已预留，未实现。
- **remote agentred 执行**：本期 desktop 本地跑；远端执行盒跑脚本是后续（参 [[project_agent_tooling_features]] 远端隧道）。
- **`hook_runs` 运行历史表**：本期仅 `LastRun*`。
- **密钥 at-rest 加密**：本期明文+脱敏（与 email 现状一致）。

## 12. 开放问题

1. **`hook_run` 审批粒度**：dry-run（试运行，不落库）是否免审，仅真 run-now / create / update / delete 走 `tool_approval`？倾向 dry-run 免审、其余要审——但「免审就能在用户机上跑任意脚本」这个权衡需用户点头。
2. **解释器 allowlist 范围**：首批就上 `bash`/`sh`/`node`/`python`/`pwsh`/`powershell`/`cmd` 全套，还是先 `bash`/`node`/`python` 三个高频、其余按需加？（注册表加条目成本极低。）

> 已定（不再翻）：Windows **在范围内**（靠 per-hook 解释器声明落地）；解释器**每个 hook 自己声明**；触发**仅 cron**（砍间隔）。
