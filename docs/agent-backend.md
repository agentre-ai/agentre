# 接入新的 AI Agent 后端

新增一个 Agent backend（例如 Gemini CLI / 自家 CLI / 另一个 in-process SDK）时要走的路径、改动点、约束和坑。写代码前先全部看完——`agentruntime.Runtime` 接口看着窄，但配套要补齐的东西散在 entity / repo / service / wire / daemon / 前端六层。

> 前置阅读：[architecture.md](architecture.md)（分层约定）、[development.md](development.md)（TDD/BDD + Fix Discipline）。

---

## 0. 先想清楚的问题

写代码前自问，**不清楚就停下来问用户**，不要写到一半才发现要回炉：

1. **运行模式**：是 in-process（像 builtin，直接吃 LLMProvider 走 cago app/coding），还是 wrap 一个本地 CLI 子进程（像 claudecode / codex）？两者在 ProviderType 匹配、cli_path 校验、env 透传、Prober 实现上完全不一样。
2. **是否支持远端执行**：要走 `agentred` daemon 投到 LAN 机器上跑吗？支持的话要保证 init() 注册到的 `RuntimeFor` 不依赖 desktop-only 的服务（chat_repo / GUI），且 wire 协议覆盖到所有 RPC 帧。
3. **能力矩阵**：能不能 mid-turn steer？能不能 abort？是否有 `can_use_tool` 协议？是否支持 ask_user_question？是否支持 plan / permission mode 切换？是否能 fork session？逐项列出来——这直接决定你要实现 `agentruntime` 哪几个可选子接口。
4. **协议形态**：事件流是 stdout JSONL（claudecode）？JSON-RPC over stdio（codex app-server）？还是内存 channel（builtin）？translator 是无状态纯函数 vs. 有状态聚合，决定从哪写第一条测试。
5. **session 复用 / spawn 单次**：每个 turn 起新进程（codex 当前做法），还是子进程常驻、用 LRU 缓存复用（claudecode）？复用要管理 idle evict、abort 解锁、跨 turn 状态 (permission mode / steer queue)。

---

## 0.5 现有 backend 速查（接口 / 能力 / Permission Mode）

仓库目前内置三个 backend，定位差异明显，**新 backend 选档前先对号入座**：

- **builtin** — in-process 跑 cago `app/coding`，直接吃 `llm_provider` 配置。定位是「轻量自带」，暴露 steer / cancel / abort / image input 这几种 in-process 单 provider 模式天生支持的能力，没有 CLI 子进程开销，也没有 plan / 工具审批等高级协议。
- **claudecode** — wrap 本地 `claude` CLI（Anthropic 家族），通过 stdout JSONL + `control_request` 帧双向通信，子进程常驻并 LRU 复用 session。功能最全：能跑 plan / `can_use_tool` / `AskUserQuestion` / Subagent / fork-session / mid-turn 切 permission mode。
- **codex** — wrap 本地 `codex` CLI（OpenAI 家族），通过 JSON-RPC over stdio app-server 协议交互，每个 turn 起新进程（fire-and-forget）。原生支持 context window 上报、原生 compact turn 和图片输入，但没有 `can_use_tool` 协议、不能 mid-turn 切 mode、也不发 Subagent 事件。

> 仓库里还有 `runtimes/remote/`，它**不是独立 backend**——而是 desktop 调远端 `agentred` daemon 时的代理，capability 通过 `Prefetch` 从 daemon 端真实 backend 同步过来，本节不单列。

数据来源（schema 改动必须三处同步）：

- `internal/pkg/agentruntime/runtimes/{builtin,claudecode,codex}/runtime.go` 的 `Capabilities()` 实现
- `internal/pkg/agentruntime/capability/capability.go` 的 cap 常量
- `runtime_test.go::TestXxxCapabilities` 矩阵断言

### 能力矩阵（Capabilities）

每行 = 一个反向通道。最右列给出该能力的语义和「为什么 ❌」——照搬前先确认你的 backend 是否真有同类协议，没有就老实返 `ErrUnsupported`，不要硬塞模拟实现。

| Capability（常量 / wire string） | 子接口 | builtin | claudecode | codex | 说明 |
| --- | --- | --- | --- | --- | --- |
| `CapSteer` / `"steer"` | `Steerer` | ✅ | ✅ | ✅ | mid-turn 注入用户消息；chat_svc 生成 queuedID，backend 真正消费后必须 emit `SteerConsumed` |
| `CapCancelSteer` / `"cancel_steer"` | `SteerCanceler` | ✅ | ✅ | ❌ | 注入后撤回；codex 的 `Stream.Steer` 是 fire-and-forget，进协议后无法回收 |
| `CapDrainSteer` / `"drain_steer"` | `SteerDrainer` | ❌ | ✅ | ❌ | turn 间 leftover 自动续投到下一 turn；仅 claudecode 维护本地 hook 队列 |
| `CapAbort` / `"abort"` | `Aborter` | ✅ | ✅ | ✅ | 「停止」按钮；必须幂等 + 必须 unblock 所有阻塞 I/O，否则前端永远停在「生成中」 |
| `CapSetPermission` / `"set_permission_mode"` | `PermissionModeSetter` | ❌ | ✅ mid-turn | ✅ 仅 launch | 运行时切 permission mode；codex 协议不允许 mid-turn 切，DB 落库后下次 spawn 生效 |
| `CapAnswerUserAsk` / `"answer_user_ask"` | `AskAnswerSink` | ❌ | ✅ | ✅ | 反向问用户问题（单选 / 多选 / Other / 密码框）；Skip 必须走 deny 而非空 map，否则 turn 静默挂死 |
| `CapToolPermission` / `"tool_permission_gate"` | `ToolPermissionSink` | ❌ | ✅ `can_use_tool` | ❌ | 工具执行前 allow/deny 审批 + 「Remember for session」+ DenyReason 回灌 LLM；codex 无等价协议 |
| `CapForkSession` / `"fork_session"` | `RunRequest.ForkAnchor` 内置 | ❌ | ✅ `--fork-session` | ✅ `thread/rollback` | 「重新生成」从某个 anchor 派生新 session 重跑 |
| `CapReportContextWindow` / `"report_context_window"` | emit `ContextWindowUpdated` | ❌ | ✅ | ✅ | runtime 探到模型实际上下文窗口大小后 emit，前端展示用量条；claudecode SDK 自己不报窗口，translator 在 `system.init` 帧上查 `llmcatalog` 兜底 |
| `CapCompact` / `"compact"` | `RunRequest.Compact=true` | ❌ | ❌ | ✅ | 原生 compact turn——让 LLM 把历史摘要后清掉占用 |
| `CapImageInput` / `"image_input"` | `RunRequest.UserBlocks` 包含 `blocks.ImageBlock` | ✅ | ❌ | ✅ | 用户消息可携带 PNG / JPEG / WebP 图片。builtin 直接透传 cago blocks；codex 将 inline 图片物化为临时本地文件后走 app-server `localImage` |

> **规则**：未声明 cap 调对应接口必须返 `agentruntime.ErrUnsupported`（sentinel 错误，跨进程透明，chat_svc 据此翻成 wire code）。声明 cap=true 但未实现接口会被 `TestXxxCapabilities` 矩阵测试卡掉（type-assert 失败）。

### 运行特性

这张表回答「跑起来长什么样」——进程形态、session 寿命、translator 设计、plan / Subagent 事件来源。新 backend 落到哪一档基本由你选的协议形态决定，**先选定再写代码**，不要中途换。

| 维度 | builtin | claudecode | codex | 备注 |
| --- | --- | --- | --- | --- |
| 运行形态 | in-process（cago app/coding + LLMProvider） | CLI 子进程（stdout JSONL） | CLI 子进程（JSON-RPC over stdio app-server） | 决定 Prober / cli_path 校验 / env 装配路径 |
| ProviderType 绑定 | 任意 LLM provider | Anthropic 家族（含网关代理） | OpenAI / Codex 家族 | entity `BackendKind.ProviderTypeMatch` 实现 |
| Session 模式 | turn-scoped cago `Runner`，turn 结束即销毁 | 子进程常驻 + LRU 缓存复用，跨 turn 复用 session | 每个 turn 起新进程，无本地复用 | 复用要管 idle evict / abort 解锁 / 跨 turn 状态 |
| Translator | 纯函数（cago events → sealed） | 纯函数 + `task_aggregator` 跨 turn 维护 Subagent 列表 | 纯函数 | 状态聚合统一在 `Run` drain 循环里做；translator 必须能在表驱动测试里独立跑 |
| Plan 来源 | 不发 | `TodoWrite` 工具内联（canonical）+ `Task*` 增量聚合（PlanUpdated 快照） | `turn/plan/updated`（Steps）+ `item/plan/delta`（Text）合并到单一 PlanUpdated | 下游 chat_svc 看到的都是同一个 sealed `agentruntime.PlanUpdated` |
| 反向问答 | 不支持 | control_request `can_use_tool` + `AskUserQuestion` 工具 | app-server `item/tool/requestUserInput` JSON-RPC | claudecode 用 question 文本做 key，codex 用 question ID 做 key |
| Subagent 事件 | ❌ | ✅ `SubagentStarted/Progress/Done` | ❌ | 只 claudecode 有原生 `Task` 工具协议 |
| 远端 daemon | ✅ | ✅ | ✅ | runtime 不得依赖 desktop-only 服务（chat_repo / GUI），状态走 `RunResult` 回吐 |

### PermissionModeMeta

permission mode 是**会话级权限状态机**（不是 plan 内容）。三个 backend 的 mode 集合和 mid-turn 切换能力完全不同——前端 `PermissionModePill` 直接读这张 meta 决定渲染。

| 字段 | builtin | claudecode | codex | 字段含义 |
| --- | --- | --- | --- | --- |
| `AllowedModes` | —（未声明 CapSetPermission） | `default, acceptEdits, plan, bypassPermissions` | `default, plan` | 该 backend 合法的 mode 名集合，service 层做白名单 |
| `DefaultMode` | — | `"acceptEdits"` | `"default"` | UI 展示 / 计算用的默认 mode（chat_svc 落库默认值） |
| `LaunchDefaultMode` | — | `""`（不附 `--permission-mode`，pkg/claudecode 内部兜底成 acceptEdits） | `"default"`（协议要求 launch 显式 collaborationMode，**不能空**） | spawn 时 wire 层兜底字符串；空 vs 非空决定「用户未显式选」vs「显式选了 default」 |
| `SwitchableDuringTurn` | — | `true`（写 control_request 即时切） | `false`（落库后下次 spawn 生效） | 前端 pill 在 `agentStatus=="waiting"` 时是否可点 |
| `Order` | — | 同 AllowedModes | 同 AllowedModes | pill 循环顺序，UI 直接按顺序 next |

> **易混字段**：`chat_sessions.permission_mode` = CLI 运行时当前 mode（被 SetPermissionMode 修改）；`chat_sessions.permission_mode_at_launch` = spawn 时下发的快照（由 runtime 经 `RunResult.LaunchPermissionMode` 回吐）。前者前端用于显示当前状态，后者决定 `bypassPermissions` 是否出现在 pill 里——只有 launch 时显式选过 bypass 才会显示该项，避免事后被滥用。

---

## 1. 整体接入路径（必经的 7 层）

新 backend = 沿着以下 7 层各加一刀，缺一不可：

| 层 | 位置 | 必改？ |
| --- | --- | --- |
| 1. Entity 类型 | `internal/model/entity/agent_backend_entity/{agent_backend.go, kinds.go}` | 必须 |
| 2. 数据库迁移 | `migrations/YYYYMMDDNNNN_*.go` + `migrationList()` 末尾 append | 仅当要加新列 |
| 3. CLI/Prober/Env 装配 | `internal/pkg/agentruntime/clienv.go` + `internal/service/agent_backend_svc/{prober.go, resolve_cli.go}` | CLI 类必须；in-process 仅 Prober |
| 4. Runtime + Translator | `internal/pkg/agentruntime/runtimes/<name>/{runtime.go, translator.go}` | 必须 |
| 5. Daemon import | `internal/daemon/runtime_imports.go` blank import | 支持远端就必须 |
| 6. Wails 绑定与 svc 类型 | `internal/service/agent_backend_svc/types.go` + `internal/app/agent.go`（若引入新字段） | 仅当新字段 |
| 7. 前端绑定 + UI gating | `frontend/wailsjs/` 重生（`make generate`）+ capability pill / 选择器 | 必须 |

---

## 2. 各层必做的事

### 2.1 Entity（`agent_backend_entity`）

- 在 `agent_backend.go` 加 `BackendType` 常量：

  ```go
  TypeMyAgent BackendType = "myagent"
  ```

- 在 `kinds.go` 实现 `BackendKind` 接口并登记到 `backendKinds`：

  ```go
  type myAgentKind struct{}
  func (myAgentKind) Type() BackendType { return TypeMyAgent }
  func (myAgentKind) KnownAliases() []string { return nil }            // 没有 model_routes 就 nil
  func (myAgentKind) ProviderTypeMatch(t llm_provider_entity.ProviderType) bool {
      return t == llm_provider_entity.TypeXxx
  }
  func (myAgentKind) AllowsCLIPath() bool { return true }              // CLI 类填 true
  func (myAgentKind) ValidateExtra(ctx context.Context, b *AgentBackend) error {
      // 这里校验 sandbox / approval / default_permission_mode / env_json 独有字段；
      // 公共字段（name / type / env_json 保留键 / model_routes alias 集合 / reasoning_effort）
      // 已经在 AgentBackend.Check 里走过，**不要再重复一遍**。
      return nil
  }
  ```

- `IsXxx()` 便捷判断方法不强制加，但加了 chat_svc / 前端可读性更好（参考 `IsClaudeCode/IsCodex`）。
- 充血模型边界：**新增字段优先放在 entity，方法（校验、默认值、序列化）也写在 entity**，service 只做跨 entity 编排。`AgentBackend.Check` 已经分派到 `BackendKind.ValidateExtra`——千万别在 service 里再写 switch。

### 2.2 数据库迁移

只有新字段时才动。规则：

- 在 `migrations/` **新建** `YYYYMMDDNNNN_xxx.go`，导出 `migrationYYYYMMDDNNNN()`。
- **禁止改动既有迁移**——需要修复的也写新的补丁迁移。
- DDL 走原生 SQL (`tx.Exec(...)`)，不要依赖 `AutoMigrate` 的隐式行为。
- 在 `migrations/migrations.go::migrationList()` **末尾** append。
- 默认值必须能让既有行通过 entity.Check（通常 `'' / '{}' / 0`）。

### 2.3 Prober + CLI 探测 + Env 装配

`agent_backend_svc.Prober` 抽象「对一条 backend 跑一轮自检 → 返回 reply 或 error」，供前端「测试连通性」按钮用。

- 在 `prober.go` 注册：

  ```go
  var proberRegistry = map[agent_backend_entity.BackendType]Prober{
      agent_backend_entity.TypeBuiltin:   builtinProber{},
      agent_backend_entity.TypeMyAgent:   myAgentProber{},
  }
  ```

- CLI 类后端如果要走本地 gateway 测，把 env 装配抽到 `internal/pkg/agentruntime/<name>_env.go`，**和 chat path 的 runtime 共享同一份装配规则**（chat 实跑和 Test 不能漂移；参考 `BuildClaudeCodeEnv` / `BuildCodexEnv`）。
- 如果是 CLI 类，在 `internal/pkg/cliprober/` 里加 `Type` 分支（当前 `cliprober.ResolveCLIPath` 只识别 `claudecode` / `codex`，非这两个值会返 `ErrInvalidType`），让前端编辑器能扫到本机 binary 绝对路径。`agent_backend_svc/resolve_cli.go` 是入口适配层，本身不分类型，不需要改。

### 2.4 Runtime（核心）

新建包 `internal/pkg/agentruntime/runtimes/<name>/`，至少 3 个文件：

1. **`runtime.go`** — 实现 `agentruntime.Runtime` 接口：

   ```go
   var defaultRuntime = New()

   func init() {
       agentruntime.RegisterRuntime(agent_backend_entity.TypeMyAgent, defaultRuntime)
   }

   type Runtime struct { /* sessions map[int64]*active */ }

   func New() *Runtime { ... }

   func (r *Runtime) Capabilities() capability.Capabilities { ... }

   func (r *Runtime) Run(ctx context.Context, req agentruntime.RunRequest) (
       <-chan agentruntime.Event, *agentruntime.RunResult, error,
   ) { ... }
   ```

   - **`Capabilities()` 必须返回稳定结果**（同一 runtime 多次调用相同），前端 capability gating / 远端 prefetch 都依赖这个语义。
   - `Run` 返回的 channel **必须在 turn 结束时 close**（无论 Done / Error / Abort）。**channel 关闭前不允许读 `*RunResult`**——`RunResult` 是异步填充。
   - `RunRequest.Cwd` 非空时直接用作 cwd；为空时回退到 `agentruntime.AgentCwd(req.AgentID)`。**不要反向依赖 `project_svc`**。
   - `ForkAnchor` 非空时实现「重新生成」——参考 claudecode 用 `--fork-session`、codex 用 `thread/rollback`。
   - 主 goroutine 退出前 `unregister(sessionID)` 清掉 sessions map，避免 leak。

2. **`translator.go`** — 把 backend 自家事件翻译成 sealed `agentruntime.Event`：

   ```go
   func translate(ev myAgentEvent) (events []agentruntime.Event, usage *provider.Usage, stopErr error) { ... }
   ```

   - **保持纯函数**——一帧入、0/1/n 帧出，**不读写 runtime 状态**。状态聚合（同安全点连续到达的 SteerConsumed 合并 / pending steer FIFO 配对）在 `runtime.Run` 的 drain 循环里做。
   - 工具识别走 `internal/pkg/agentruntime/canonical`——能识别成 `FileWrite/FileEdit/PlanUpdate/AgentSpawn` 的就填 `ToolCall.Canonical`，UI 投影 / 前端卡片复用同一套；不能识别的留 raw。
   - `EventUsage`：translator 内部按 family 把 `TotalInputTokens` 算好（Anthropic = prompt+cached+cacheCreation；OpenAI = prompt），下游不再做家族判断。

3. **`runtime_test.go`** + **`translator_test.go`** — TDD 必须：
   - **Capabilities 矩阵测试**：声明的 cap=true 与实现的子接口（Steerer / Aborter / 等）必须一致。参考 `runtime_test.go::TestXxxCapabilities`。
   - **Translator pure-fn 测试**：表驱动覆盖每种事件 kind + 边界（空 tool input / 错误帧 / 部分字段缺失）。Convey 嵌套描述场景。
   - **Run 集成测试**：用 fake backend 客户端 / fake session 验证事件批次顺序、`SteerConsumed` 合并、Abort 解锁、RunResult 终态。

#### 可选控制子接口

按 Capabilities 声明实现（**声明了 cap=true 就必须实现对应接口**；matrix 测试会卡掉）：

| Cap | 接口 | 何时实现 |
| --- | --- | --- |
| `CapSteer` | `Steerer` | 支持 mid-turn 注入用户消息 |
| `CapCancelSteer` | `SteerCanceler` | 注入后还能撤回（claudecode 有；codex 无） |
| `CapDrainSteer` | `SteerDrainer` | turn 间 leftover 自动续投（仅 claudecode） |
| `CapAbort` | `Aborter` | 用户「停止」按钮——基本都得实现 |
| `CapSetPermission` | `PermissionModeSetter` | 运行时切换 permission mode |
| `CapAnswerUserAsk` | `AskAnswerSink` | 处理反向 ask_user_question |
| `CapToolPermission` | `ToolPermissionSink` | 处理 `can_use_tool` 协议 |
| `CapForkSession` | `RunRequest.ForkAnchor` 内置语义 | 「重新生成」走 fork |
| `CapReportContextWindow` | emit `ContextWindowUpdated` | runtime 能探到模型实际窗口（codex 协议原生有；claudecode 靠 `llmcatalog.Lookup(model)` 兜底） |
| `CapCompact` | `RunRequest.Compact=true` 内置语义 | 原生 compact turn |
| `CapImageInput` | `RunRequest.UserBlocks` image blocks | 支持多模态用户输入；不支持时 chat_svc 会在调 runtime 前拒绝带图 turn |

不实现的 cap：**chat_svc 拿到前端请求时返回 `ErrUnsupported`**——错误码已经在 wire 层 sentinel 化跨进程透明传递，**不要私造错误**。

#### 控制接口逐个展开（**接入前必读**）

下面把六个反向通道的协议细节、字段语义、claudecode/codex 差异拆开讲。新 backend 接入时，**逐项**对照实现，不要凭直觉猜。

##### A. Steerer / SteerCanceler / SteerDrainer — mid-turn 注入

接口签名（`internal/pkg/agentruntime/runner.go:362-407`）：

```go
type Steerer interface {
    Steer(ctx context.Context, sessionID int64, queuedID, text string) error
}
type SteerCanceler interface {
    CancelSteer(ctx context.Context, sessionID int64, queuedID string) ([]string, error)
}
type SteerDrainer interface {
    DrainPending(ctx context.Context, sessionID int64) []ConsumedSteer
}
```

- **queuedID** 由 chat_svc 在 Enqueue 时生成（UUID），是后续 CancelSteer 的回写句柄。
- **消费回执**：backend 真正把这条 text 注入到对话后，**必须**经 translator emit `agentruntime.SteerConsumed{Steers: [{QueuedID, Text}]}`——chat_svc 据此把对应 chat_message 状态推进到 `consumed`。同安全点连续到达的 SteerConsumed **在 `Run` drain 循环里合并成单批**（参考 builtin/runtime.go `flushSteers`），保持单帧 emit 的 wire 行为。
- **CancelSteer 语义**：
  - `queuedID == ""` → 清空整个 pending 队列，返回被清的 ID 列表（FIFO）
  - `queuedID` 非空 → 单条撤回；不在队列返 `ErrSteerNotFound`（已被 AI 消费 / 从未入队）
- **DrainPending 副作用**：返回非空 slice 时**必须**原子地把 session 标记回 "still in turn"，否则 chat_svc 在两次 Run 之间到的 Steer 会落到 ChatSendInFlight 被丢。codex 故意不实现 SteerDrainer——它的 turn/steer 是 fire-and-forget，没有本地队列。
- **claudecode 实现**：通过 `httpgateway.SteerInbox` push 给 CLI hook 子进程。
- **codex 实现**：调 `*codex.Stream.Steer(text)`，本地维护 pending 队列做 echo 配对。

##### B. Aborter — 「停止」按钮

接口签名（runner.go:422-424）：

```go
type Aborter interface {
    Abort(ctx context.Context, sessionID int64) error
}
```

- **幂等 + 并发安全**：可能和 runner 自己的 drain goroutine 同时被调。
- **必须解锁 I/O**：claudecode 写 `control_request{interrupt}` 一帧到 stdin；codex 调 `turn/interrupt` RPC；builtin 取消 `turnCtx`。**ctx cancel 必须 unblock 所有阻塞读**——这是「停止」生效的前提。
- **返回值**：sessionID 没 in-flight turn 时返 `ErrNoActiveTurn`，chat_svc 翻 `code.ChatStopNoActive`。
- **RunResult.StopErr**：用户主动 Abort 时 runner **应当**填 `agentruntime.ErrAborted`，让 chat_svc 区分「正常 Done / 用户中止 / 真错误」三态。

##### C. ToolPermissionSink — `can_use_tool` 工具审批

> **codex 当前不支持**（无等价协议）。下文以 claudecode 为蓝本，新 backend 有同类协议时照搬。

接口签名（runner.go:218-220）：

```go
type ToolPermissionSink interface {
    SubmitToolPermission(ctx context.Context, sessionID int64, requestID string,
        allow, alwaysAllowSession bool, denyReason string) error
}
```

**完整调用链**：

1. **Backend 收到 can_use_tool 控制请求**（除 AskUserQuestion 以外的工具都走这里）→ translator/runtime emit：

   ```go
   agentruntime.ToolPermissionRequest{
       RequestID:  "ctl-xxx",        // backend 私有句柄（claudecode = control_request.request_id）
       ToolCallID: "toolu_xxx",      // 关联 assistant 流里的 tool_use；race 时可空
       ToolName:   "Bash",           // 工具名，service 据此识别 ExitPlanMode 特例
       Input:      rawInputJSONBytes,// 原 control_request.input 字节，前端自己 JSON.parse
   }
   ```

2. **chat_svc 落库**（`internal/service/chat_svc/tool_permission.go:22-53`）：
   - 转换为 `blocks.ToolPermissionBlock` 加进 acc。
   - 投影成 `ChatBlock{Type:"tool_permission_request"}` 推前端；canonical 装 `ToolPermission{...}` 或（ToolName=="ExitPlanMode" 时）`PlanApproveRequest{...}`。

3. **前端渲染审批卡**：Allow / Deny 两按钮，Allow 可勾 "Remember for session"（alwaysAllowSession），Deny 可填拒绝原因。

4. **前端答复 → service**：调 `AnswerToolPermission(sessionID, requestID, allow, alwaysAllowSession, denyReason, targetPermissionMode)`。

5. **service 反向投回**：
   - `selectRunner()` 类型断言到 `ToolPermissionSink`，调 `SubmitToolPermission(...)`。
   - claudecode 实现：写一帧 control_response 到子进程 stdin：
     - `allow=true` → `PermissionResult{Behavior:"allow", UpdatedInput: parsedInput}`；`alwaysAllowSession=true` 时再附 `UpdatedPermissions=[{type:"addRules", rules:[{toolName}], behavior:"allow", destination:"session"}]`，CLI 自己维护后续 allow rules。
     - `allow=false` → `PermissionResult{Behavior:"deny", Message: denyReason || "User denied..."}`；CLI 把 Message **当 tool_result 回灌给 LLM**，让 AI 拿到具体反馈重新规划。

6. **runtime 完成回写后** emit 终态帧：

   ```go
   agentruntime.ToolPermissionResolved{
       RequestID: "ctl-xxx",
       Allowed: true, AlwaysAllow: true, DenyReason: "",
   }
   ```

   chat_svc patch 回 acc 里那条 ToolPermissionBlock，确保 turn finalize 落盘正确。

**新 backend 接入要点**：
- ToolName 字段必须填——chat_svc 用它做特例识别（ExitPlanMode）。
- DenyReason 不仅是日志——必须能被 backend 回灌给 LLM 作 tool_result（否则 AI 不知道为何被拒）。
- Input 透传原始字节，不要解析后再 marshal 一遍（前端可能依赖原 key 顺序 / 数字精度）。

##### D. AskAnswerSink — 反向问用户问题

接口签名（runner.go:255-257）：

```go
type AskAnswerSink interface {
    SubmitAnswer(ctx context.Context, sessionID int64, requestID string,
        questions []AskQuestion, answers []AskAnswer, skipped bool) error
}
```

**与 ToolPermission 的关键区别**：AskUserQuestion 是**有结构的问答**（单选 / 多选 / Other / 密码框），不是 allow/deny 二元；backend 必须按各自协议聚合答案 map。

**完整调用链**：

1. **Backend 检测到 AskUserQuestion 工具/控制请求** → emit：

   ```go
   agentruntime.UserAskRequest{
       RequestID:        "ctl-xxx",
       ToolCallID:       "toolu_xxx",   // race 时可空，前端按 RequestID 占位
       ParentToolCallID: "task-...",    // subagent 内调用时指向外层 Agent.tool_use_id
       Questions: []AskQuestion{{
           ID: "q1", Question: "...", Header: "...",
           MultiSelect: true, IsOther: true, IsSecret: false,
           Options: []AskOption{{Label, Description}},
       }},
   }
   ```

2. **chat_svc 落 `blocks.UserAskBlock`** + 投影 ChatBlock + canonical UserAsk DTO（`internal/service/chat_svc/ask_user_question.go:21-83`）。

3. **前端 UserAskCard 渲染**：单选 radio / 多选 checkbox / Other 文本框 / IsSecret 密码框；提供 Answer + Skip 两按钮。

4. **前端答复 → service** `AnswerUserQuestion(sessionID, requestID, answers, skipped)`，answers 是 `[]AskAnswerDTO{QuestionIndex, Labels[], OtherText}`。

5. **service 反向投回** `sink.SubmitAnswer(ctx, sessionID, requestID, nil, rtAnswers, skipped)`。**questions 参数留 nil**——backend 自己缓存了 waiter 时的 questions 列表，传 nil 可让 backend 跳过 length 校验。

6. **runtime 写回 backend**：
   - **claudecode**：写 control_response，`UpdatedInput.answers` 按 question 文本聚合 csv labels（`OtherAnswerLabel` 替换为 `OtherText`）；`Behavior:"allow"`。
   - **codex**：响应 app-server 的 `item/tool/requestUserInput` JSON-RPC，payload = `map[codexQuestionID][]string`；按 `buildUserInputAnswers` 装配。
   - **内置 Agent**：in-process channel。

7. **Skipped 语义**：必须让 LLM 优雅看到拒答信号，**不要** allow 一个空 map（会让 turn 静默挂死，hapi gotcha #4）：
   - claudecode：写 deny message。
   - codex：`SubmitUserInput(requestID, map[string][]string{})` 显式空 map。

8. **runtime 完成后** emit `UserAskResolved{RequestID, Answers, Skipped}`，chat_svc patch 回 UserAskBlock。

**新 backend 接入要点**：
- `OtherAnswerLabel` 是哨兵常量（`agentruntime.OtherAnswerLabel`），看到这个 label 要替换为 `AskAnswer.OtherText`，不要原样发给 LLM。
- AskQuestion.ID 是 backend 私有的（codex 用它做 key；claudecode 用 question 文本做 key）——translator 必须把它保留下来，不要丢。
- waiter 缓存：runtime 收到 UserAskRequest 时**必须**把 (RequestID → questions) 缓存起来，SubmitAnswer 才能在 questions=nil 时反查。

##### E. PermissionModeSetter — 运行时切换权限模式

接口签名（runner.go:437-439）：

```go
type PermissionModeSetter interface {
    SetPermissionMode(ctx context.Context, sessionID int64, mode string) error
}
```

> 这是**会话级权限开关**，不是 plan 内容。两个概念分开：
> - **PermissionMode** = "默认是否需要审批 / 是否进入 plan 模式" 的运行时状态机；
> - **Plan 内容** = AI 当前 todo 列表（见 §F）。

**合法 mode 值**（来自 `capability.PermissionModeMeta.AllowedModes`）：
- claudecode：`{default, acceptEdits, plan, bypassPermissions}`
- codex：`{default, plan}`（**禁运行时切换**，`SwitchableDuringTurn: false`）

**两个不同字段**：

| 字段 | 含义 | 谁写 |
| --- | --- | --- |
| `chat_sessions.permission_mode` | CLI 运行时当前模式（被 SetPermissionMode 改变） | chat_svc 的 PermissionModeWriter |
| `chat_sessions.permission_mode_at_launch` | spawn 时下发的 `--permission-mode` 快照 | runtime 通过 `RunResult.LaunchPermissionMode` 回吐，chat_svc 写库 |

历史教训：runtime 不要直接调 `chat_repo.Session().Update...`——agentred daemon 不 bootstrap chat_repo，会 nil panic。**状态走 RunResult 回吐**。

**DefaultMode vs LaunchDefaultMode**（`capability.PermissionModeMeta`）：

| 字段 | 用途 | claudecode | codex |
| --- | --- | --- | --- |
| `DefaultMode` | UI 展示/计算用的默认 mode 名 | `"acceptEdits"` | `"default"` |
| `LaunchDefaultMode` | spawn 时 wire 层兜底字符串 | `""`（不附 flag，让 pkg/claudecode 兜底成 acceptEdits） | `"default"`（协议要求每次 launch 显式 collaborationMode） |

**两种触发路径**：

1. **主动切换**（前端点 PermissionModePill）：
   - 前端 → `SetPermissionMode(sessionID, mode)`
   - service 落库 `chat_sessions.permission_mode` → 尝试 `runner.SetPermissionMode(ctx, sessionID, mode)`
   - claudecode runtime 写 control_request 到 CLI；codex 返 `ErrUnsupported`，下次 spawn 从 DB 读新 mode 启动。

2. **被动切换**（CLI 自己通报）：
   - runtime emit `agentruntime.PermissionModeChanged{Mode: "acceptEdits"}`
   - `handlers.PermissionModeChangedHandler` 落 `PermissionModeChangeBlock` + 通过 `PermissionModeWriter.SetMode` 写库 + emit `StreamSessionStatus` patch。

**前端 PermissionModePill 行为**（capability 投影）：

```ts
const meta = caps.PermissionModeMeta
const canSwitch = meta.SwitchableDuringTurn && agentStatus !== "waiting"
const order = meta.Order         // pill 循环顺序
const showBypass = session.permission_mode_at_launch === "bypassPermissions"
// bypassPermissions 仅在 launch 时显式选过才出现在 pill 里，避免事后被滥用
```

##### F. ExitPlanMode — plan → acceptEdits 的特殊审批流

ExitPlanMode 是**复用 ToolPermission 通道**实现的 plan 退出协议（claudecode 专属，codex 无等价）：

1. CLI 在 plan 模式下完成规划后调用 `ExitPlanMode` 工具 → backend emit `ToolPermissionRequest{ToolName: "ExitPlanMode", Input: {plan: "..."}}`。
2. chat_svc 在 `tool_permission.go:34-41` 检测 `ToolName=="ExitPlanMode"`，**额外装配** `Canonical = PlanApproveRequest{Plan, Actions}`，让前端用 `PlanApproveCard` 渲染（而非通用 ToolPermissionCard）。
3. `Actions` 由 `handlers.BuildPlanApproveActions(launchPermissionMode)` 在 ToolPermissionRequest handler 里装配（`internal/service/chat_svc/handlers/plan_approve.go:16-32`），规则：
   - 普通 launch（空 / default / acceptEdits / plan）→ `[plan.approve.accept_edits, plan.approve.manual, plan.refine]`
   - launch="bypassPermissions" → 第一项**替换**为 `plan.approve.bypass_permissions`（不是追加），得到 `[plan.approve.bypass_permissions, plan.approve.manual, plan.refine]`
   - `plan.refine` 带 `RequiresFeedback: true`——前端展开 feedback textarea；用户提交后走 `Allow=false` + `DenyReason=feedback`（CLI 把 message 当 tool_result 回灌给 AI 继续规划），**不是** allow + 切回 plan mode
4. 前端按用户点的 action 调 `AnswerToolPermission`：approve 类 → `Allow=true, TargetPermissionMode=mapPlanApproveAction(actionID)`（见 `plan_action.go:198-210`：bypass→`bypassPermissions` / accept_edits→`acceptEdits` / manual→`default`）；refine → `Allow=false, DenyReason=feedback`。
5. service 先 `SubmitToolPermission()`（CLI 收到 approve 后自动把 plan → default）。`Allow=true` 且 `TargetPermissionMode` 非空且非 `"default"` 时**接力**调 `SetPermissionMode()` 切到目标——所以 `acceptEdits` / `bypassPermissions` 会接力，`Manual` (=`default`) 不接力，`refine` 因为 `Allow=false` 也不接力（`tool_permission.go:131`）。

**新 backend 接入要点**：有 plan 退出语义的 backend，把 ToolName 起成 `"ExitPlanMode"` 就能直接复用前端 PlanApproveCard——不要新造 tool name。

##### G. Plan 内容更新 — PlanUpdated event

接口形态（**单向 emit，无反向通道**）：

```go
agentruntime.PlanUpdated{Plan: canonical.PlanUpdate{
    Text:    "...",             // 完整 Markdown plan（codex item/plan/delta 透传）
    Steps:   []canonical.PlanStep{{Step, Status: pending|inProgress|completed|canceled}},
    Actions: []canonical.PlanAction{...},  // claudecode 不填（plan 退出走 §F ExitPlanMode 通道）；
                                           // codex 在 plan mode + Text 非空时由 translator
                                           // attachPlanModeActions 附 [Execute, Refine]
}}
```

**两种 wire 形态合并到一个 sealed event**：

- **claudecode 两条路径**——
  - `TodoWrite` 工具调用走 translator → `ToolCall.Canonical = canonical.PlanUpdate`（**不**发独立 PlanUpdated event，前端从 ToolCall 上读 canonical 即可）；
  - `TaskCreate` / `TaskUpdate` 增量调用由 `claudecode/task_aggregator.go` 跨 turn 维护完整任务列表，每次变更 emit `agentruntime.PlanUpdated` 完整快照。
- **codex**：两种触发——`turn/plan/updated` 通知发 `Steps[]`；`item/plan/delta + item/completed{type:"plan"}` 流式发 `Text`。translator 收编到同一个 PlanUpdated event，下游不再二态分支（`runtimes/codex/translator.go:57-80`）。codex 还在 plan mode 下通过 `attachPlanModeActions` 给 PlanUpdate 附 `[plan.execute, plan.refine]` 两个 action，前端 PlanCard 直接渲染按钮。
- **PlanText 保留尾换行**：trim 后会被前端 markdown 渲染端误认为「无尾换行」破坏格式——仅用 `strings.TrimSpace` 做「是否为空」判断，**不要** trim 后再发。

**chat_svc 落 PlanBlock**（`internal/service/chat_svc/plan_block.go:16-73`）：
- 投影成 `ChatBlock{Type:"plan"}`
- 前端 PlanCard 渲染完整文本；`TaskProgressBar` 读 `steps[].status` 做进度条
- 同一 turn 多次 PlanUpdated 走 mutate（按 PlanBlock key 覆盖），不重复落新 block

##### H. Subagent 生命周期 — claudecode Task 工具专属

接口形态（**单向 emit，3 个事件**）：

```go
agentruntime.SubagentStarted{ToolCallID, Info: SubagentInfo{...}}
agentruntime.SubagentProgress{ToolCallID, Info: SubagentInfo{TotalTokens, LastToolName, ToolUses}}
agentruntime.SubagentDone{ToolCallID, Info: SubagentInfo{Status: "completed"|"failed", DurationMs, TotalTokens}}
```

- **ToolCallID** = 外层父级 `Task` / Agent 工具的 tool_use id。
- **Info.Status**：runtime 只产 `running` / `completed` / `failed`；`canceled` 由 `handlers.MarkRunningSubagentsCancelled` 在 turn abort 收尾时推断（CLI 被 interrupt 后 Done 不会到，留 running 会让前端 AgentSpawnCard 永远 spin）。
- **ToolCall / ToolResult 的 ParentToolCallID 字段**：subagent 内部调的工具填外层 Task tool_use id，前端据此把子卡归集到父 SubagentInvocationCard。
- **codex / builtin 目前不发**——只 claudecode 有原生 subagent 协议。新 backend 有类似 fork-execute 工具时再考虑接入这一组事件。

#### Sentinel 错误（必须用，不要造新的）

```go
agentruntime.ErrNoActiveTurn   // sessionID 没有 in-flight turn
agentruntime.ErrSteerNotFound  // CancelSteer 给的 queuedID 已被消费 / 不存在
agentruntime.ErrAborted        // 用户主动 Abort，写到 RunResult.StopErr
agentruntime.ErrUnsupported    // 当前 runtime 不支持该 cap
```

新错误必须有跨进程语义时，**先在 `errors.go` + `wire.go` 同步加 sentinel + error code**，不要在 daemon handler 里临时 stringify。

### 2.5 Daemon import（远端执行）

要让新 backend 在 `agentred` daemon 上跑，**只**改 `internal/daemon/runtime_imports.go`：

```go
import (
    _ "agentre/internal/pkg/agentruntime/runtimes/builtin"
    _ "agentre/internal/pkg/agentruntime/runtimes/claudecode"
    _ "agentre/internal/pkg/agentruntime/runtimes/codex"
    _ "agentre/internal/pkg/agentruntime/runtimes/myagent"   // ← 加这行
)
```

- 注册依赖 init() 副作用——所以 runtime 包的 `init()` **只**做 `RegisterRuntime`，**不要起 goroutine / 读环境变量 / 打开文件**，否则 daemon 进程启动时会触发副作用。
- daemon 端不 bootstrap chat_repo——runtime 内部不能反向依赖 repository 包。需要持久化的运行时状态（比如 `LaunchPermissionMode`）走 `RunResult` 字段回吐给 chat_svc。
- `runtime_imports_test.go` 会枚举 `RegisteredRuntimes()` 跑一轮 capability 协议测试——确保新 runtime 加进去后这个测试还过。

### 2.6 Service / Wails 绑定

只有新增字段时才动：

- `internal/service/agent_backend_svc/types.go`：在 `BackendItem` / `CreateBackendRequest` / `UpdateBackendRequest` / `TestBackendRequest` 加字段。**字段名稳定、json tag 明确**——`make generate` 会把它提到前端 TS 类型。
- `internal/service/agent_backend_svc/agent_backend.go`：在 buildEntity / mapItem 里读写新字段。
- `internal/app/agent.go`：绑定层方法**只做** parse → svc → return，业务塞进 `App` 会被 `go test` 漏掉。

### 2.7 前端

- `make generate` 重生 `frontend/wailsjs/` bindings。
- 编辑器 UI（`frontend/src/components/agentre/agent-backends.tsx` + `agent-backends-utils.ts`）：加 type 选项、新字段表单控件——**统一用 shadcn `@/components/ui/*`**，禁止新增原生 `<select>`。
- Capability gating：前端 hook `useBackendCapabilities` / `useSessionCapabilities`（`frontend/src/components/agentre/capability/`）调 Wails 绑定 `GetBackendCapabilities` / `GetSessionCapabilities`（`internal/app/chat.go` → `chat_svc/ipc/capability.go`），返回 `Capabilities.Set` + `PermissionModeMeta`。组件里读 `caps.has("steer")` / `caps.has("set_permission_mode")` 等来 gating steer chip / abort 按钮 / permission mode pill / ask_user_question 卡。新 cap 加到 capability 枚举后无需改 hook，只改 use 端。

---

## 3. 远端执行（`agentred`）的额外考量

桌面端可以把单个 chat 投到 LAN 的 `agentred` 跑：

```
desktop UI → internal/app → chat_svc → remote.Runtime
           → JSON-RPC over WebSocket (wire.MethodRun)
           → daemon/handlers/RuntimeHandlers
           → agentruntime.RuntimeFor(backendType) — 跑你新写的 *Runtime
```

要走通这条路：

1. Runtime 不依赖 desktop-only 的服务（chat_repo / GUI / system tray）。需要回吐的状态走 `RunResult` 字段，不要直接调 repo。
2. 新增的 sentinel 错误同步加 `wire.ErrCode*`——不然 `errors.Is(err, agentruntime.ErrXxx)` 在客户端会失效。
3. 新增的 Event 类型同步加 `wire.Event*` 编解码——`runtime.event` notification 是按 sealed Event tag 分发的。
4. 测试覆盖远端路径：起一对 in-memory `client.Client` ↔ `daemon.handlers.RuntimeHandlers`，验证 capability 协商 / Run / Abort / Steer 跨进程语义。

---

## 4. TDD / BDD 必备测试清单

**Red 阶段先写、跑、看到失败，再实现。** 没失败测试不写实现代码——这是 [development.md](development.md) §0 的硬规则。

| 测试 | 位置 | 验什么 |
| --- | --- | --- |
| Entity Check 表驱动 | `*_test.go` 同 kinds.go | name / type / env_json 保留键 / model_routes alias / cli_path 允许性 / kind 独有字段 |
| Capabilities 矩阵 | `runtimes/<name>/runtime_test.go` | 声明的 cap=true ↔ 实现了对应接口（type assert） |
| Translator 纯函数 | `runtimes/<name>/translator_test.go` | 每种 backend 事件 kind + 边界（空 input / partial fields / error frame） |
| Run 集成 | `runtimes/<name>/runtime_test.go` | 事件批次顺序、SteerConsumed 合并、Abort 解锁、RunResult 终态、ctx cancel 行为 |
| ToolPermissionSink | `runtimes/<name>/control_test.go` | Allow / Deny / alwaysAllowSession 三态都写回 wire；DenyReason 透传到 LLM；Resolved 帧回吐字段全 |
| AskAnswerSink | `runtimes/<name>/control_test.go` | OtherAnswerLabel 替换为 OtherText；Skipped 走 deny 而非空 map；waiter 缓存按 RequestID 反查 |
| PermissionModeSetter | `runtimes/<name>/control_test.go` | 主动切 wire 帧形态 + 被动 PermissionModeChanged emit；`LaunchPermissionMode` 经 RunResult 回吐 |
| Plan/Subagent 事件 | `runtimes/<name>/translator_test.go` | PlanText 保尾换行 + Steps 合并；Subagent Started/Progress/Done 三态字段完整 |
| Prober | `agent_backend_svc/prober_test.go` | provider missing / 网络错误 / 工具循环正常都翻译到合适 reply/err |
| Wire round-trip | `runtimes/remote/wire/wire_test.go` | 新 Event / 新 sentinel 编解码对称 |
| Daemon registry | `daemon/runtime_imports_test.go` | 新 backend 出现在 `RegisteredRuntimes()` 里 |
| Service create/update/delete | `agent_backend_svc/agent_backend_test.go` | mockgen mock repo，验校验 + 落库字段 |

repo 单测一律 `testutils.Database(t)` + sqlmock，**禁止起真 SQLite**——参考 [development.md](development.md) §测试栈。

---

## 5. 常见坑 / 反模式（不要重蹈）

1. **不要把业务塞进 `internal/app/`**。Wails 绑定只做 `parse → svc.Xxx().Method(ctx, ...) → return`，否则 `go test` 覆盖不到。
2. **不要在 runtime `init()` 里起 goroutine / 读 env / 打开文件**。daemon 进程引导时会触发副作用，而且单测无法控制。`init()` 只做 `RegisterRuntime`。
3. **不要在 chat_svc 里 switch backendType**。新 backend 出现就抽 interface 让 runtime 自己声明能力——参考 capability matrix。`if backend.Type == "claudecode" { ... }` 是 smell。
4. **不要重复 normalize**。env / model_routes 在 entity.Check 已经 normalize 一次，service / runtime 不要再做第二次。
5. **不要让 translator 有状态**。状态聚合在 `Run` 的 drain 循环里做，translator 必须能在测试里独立跑、表驱动断言。
6. **不要把 SteerInbox / SessionCache 等运行期资源 hard-code 到 `New()`**。用包级 `SetXxx` 注入器（参考 `claudecode.Runtime.SetSteerInbox`），bootstrap 时装配，单测可换 fake。
7. **不要私造错误来表达 `ErrNoActiveTurn` / `ErrUnsupported`**。这些 sentinel 跨进程透明，私造 string 会让 chat_svc 翻译不出来。
8. **不要让 runtime 反向依赖 repository / chat_svc**。daemon 进程没 bootstrap repo——一调就 nil panic。状态走 `RunResult` 回吐。
9. **不要忽略 ctx**。`Run` 收到的 ctx 取消后必须 unblock 所有 I/O——这是 chat_svc 实现「停止」按钮的前提（claudecode 通过 control_request、codex 通过 turn/interrupt、builtin 通过 cancel turnCtx）。
10. **不要在新增 capability 的同一次提交里做 drive-by refactor**。Diff 只动 producer + 它的测试。看到无关脏数据先 flag——参考 CLAUDE.md / AGENTS.md §3 / [development.md](development.md) §Fix Discipline。

---

## 6. 提交前自检清单

- [ ] 新增 `BackendType` 常量 + `BackendKind` 实现 + 登记到 `backendKinds`
- [ ] 新字段的迁移文件 append 到 `migrationList()` 末尾，DDL 用原生 SQL，默认值能让既有行通过 Check
- [ ] 新 runtime 包 `init()` 只调 `RegisterRuntime`，无副作用
- [ ] Capabilities 声明与实际实现的子接口一致（matrix 测试过）
- [ ] 反向通道实现到位：`SubmitToolPermission` allow/deny/alwaysAllowSession + DenyReason 透传 LLM；`SubmitAnswer` 处理 OtherText 哨兵 + Skipped 走 deny；`SetPermissionMode` 经 wire 写 backend；`PermissionModeChanged` 经 emit 回 chat_svc
- [ ] runtime 内部缓存了 AskUserQuestion waiter（按 RequestID 反查 questions），SubmitAnswer 收到 nil questions 不报错
- [ ] ExitPlanMode 复用 ToolPermission 通道时 ToolName 字符串就叫 `"ExitPlanMode"`（不要新造 tool name）
- [ ] PlanUpdated 的 Text 字段保留尾换行，仅 TrimSpace 做空判断；Steps 合并到同一 PlanBlock 不重复落
- [ ] Translator 是纯函数，表驱动测试覆盖 happy path + 至少一个 boundary/error
- [ ] `RunRequest.Cwd` 非空时优先用；`ForkAnchor` 非空时走 fork；`ctx` cancel 能 unblock
- [ ] `RunResult` 在 events channel close 前**不**被读；新字段（ProviderSessionID / Usage / Model / LaunchPermissionMode / ContextWindow / UserAnchor）按 backend 能力填
- [ ] 远端：`runtime_imports.go` blank import 已加；新 Event / sentinel 已加 wire 编解码 + round-trip 测试
- [ ] Wails 类型新字段稳定 json tag；`make generate` 已重生 `frontend/wailsjs/`
- [ ] Prober 已注册到 `proberRegistry`；CLI 类后端 env 装配在 `agentruntime/clienv.go` 与 chat path 共享
- [ ] 关键流程打日志：`logger.Ctx(ctx)`、message 用 `package.Method:` 前缀小写、字段用 `zap.Xxx`（参考 [development.md](development.md) §日志）
- [ ] `make check`（lint + test）全过；新包 service/repository 层覆盖率 ≥80%
