# Pi Agent 支持自定义 LLM 供应商

状态：草稿（待用户批准）<br>
创建：2026-08-05

**目标（Objective）：** piagent 后端可绑定一个 Agentre 自定义 LLM 供应商（`llm_providers` 记录），让 Pi CLI 直接调用该供应商的 `BaseURL`，使用该记录的 `APIKey` 与默认 `Model`；未绑定供应商时行为与现状完全一致（Pi 使用自己的 `~/.pi/agent` 配置）。

**硬性不变量（Hard invariant）：** 未绑定供应商的 piagent 后端不改变任何现有行为；APIKey 永不写入任何磁盘文件；用户自己的 `~/.pi/agent` 配置（models.json / auth.json / settings.json / skills / extensions / trust.json）永不被动修改。

## 问题

1. **piagent 后端无法绑定 Agentre 的 LLM 供应商。** `internal/model/entity/agent_backend_entity/kinds.go` 的 `piAgentKind.ProviderTypeMatch` 恒返回 `false`，`ValidateExtra` 拒绝任何非空 `LLMProviderKey`；前端 `frontend/src/components/agentre/agent-backends.tsx` 的 `matchingProviders("piagent")` 返回 `[]`。用户要用自定义供应商只能手改 `~/.pi/agent/models.json`（本机现有 `local-llm` / `deepseek-responses` 即如此），与 claudecode / codex / builtin「在 Agentre 里配供应商」的体验不一致。
2. **Pi CLI 原生支持自定义 provider，Agentre 未开放这层能力。** `pi`（本机 0.83.0）通过 `--model provider/id` / `--provider <name>` 选择模型，provider 可由 `~/.pi/agent/models.json` 或扩展 `pi.registerProvider()` 注册；`llm_provider_entity` 的三种 Type（`anthropic` / `openai-chat` / `openai-response`）分别对应 Pi 原生 API 形状 `anthropic-messages` / `openai-completions` / `openai-responses`，映射完整。
3. **架构已预留管理 Pi 配置的空间。** `kinds.go` 的 `reservedEnvKeys` 已包含 `PI_CODING_AGENT_DIR` / `PI_CODING_AGENT_SESSION_DIR`（拒绝用户 env_json 写入），说明 agentre 管理 Pi 配置是被预期的方向，只是尚未落地。

## 参与者与用户故事

- **Agentre 用户**：在 agent-backends 编辑器里给 Pi Agent 选一个自定义 LLM 供应商（anthropic / OpenAI 兼容 / OpenAI Responses），Pi 聊天直接走该供应商，不再手改 `~/.pi/agent`。
- **Pi Agent runtime**（`runtimes/piagent`）：在 `RunRequest.Provider` 非空时生成 provider 扩展、注入 env 密钥、下发模型选择；为空时保持现状。
- **Pi CLI**：加载 agentre 注入的扩展，向 `BaseURL` 发起请求，按 `--model` 选中模型，上报真实模型 id / 上下文窗口。

## 设计决策

| # | 决策 | 依据与已拒绝方案 |
|---|---|---|
| 1 | **机制 = 生成 provider 扩展（`pi.registerProvider`）+ APIKey 走子进程 env** | 复用 mcpbridge 的 `--extension` 注入通道（`session.go` 已有 `WithExtension`）；扩展静态无密钥，密钥只进 env，永不落盘，完全不碰用户 `~/.pi/agent`。已拒绝：合并写 `~/.pi/agent/models.json`+`auth.json` —— 污染共享配置、并发读改写、残留与误删用户条目、密钥落盘；已拒绝：`PI_CODING_AGENT_DIR` 指向隔离目录 —— pi 会丢失用户自己的 settings/skills/extensions/trust，除非整体拷贝（陈旧副本问题）。 |
| 2 | **Provider 类型范围 = 三类全收**（`anthropic` / `openai-chat` / `openai-response`） | Pi 原生支持三种 API 形状，pi 是通用 agent；`ProviderTypeMatch` 对三类返回 true，前端选择器展示三类。已拒绝：只收 OpenAI 系列 —— 用户已有 anthropic 兼容需求（`~/.pi/agent` 中 `local-llm` 即走 openai-responses 之外的形态），且与 pi 的通用定位不符。 |
| 3 | **绑定供应商时校验默认 Model 非空** | `--model` 必须能选中该供应商下的模型；Model 为空无法保证命中该 provider（可能落到 pi 默认）。已拒绝：Model 空时走 pi 默认 —— 用户意图不明确且大概率打错供应商。 |
| 4 | **密钥永不落盘**：APIKey 只出现在本次子进程 env（`AGENTRE_PI_API_KEY_<sanitizedKey>`），扩展文件只含 `baseUrl` / `api` / 模型元数据 / `apiKey: "$AGENTRE_PI_API_KEY_..."` env 引用 | `registerProvider` 的 `apiKey` 与 models.json 同款 `$ENV_VAR` 解析；codex 后端已有「token 只进 env 不进 argv/config」的先例。已拒绝：把明文密钥写进扩展文件 —— 密钥落盘，删除后端后仍残留。 |
| 5 | **远端（agentred）纳入本轮** | daemon 已通过 `ProviderLookup.FindByKey` 把完整 `req.Provider` 填给 runtime（`handlers/runtime.go`），扩展物化在运行进程的 AppDataDir 内，两端同一处代码，几乎零额外成本。已拒绝：本轮排除远端 —— 与其它后端能力不一致。 |
| 6 | **Test 连通性对齐绑定供应商**：prober 对绑定供应商的 piagent 用与 runtime 同一渲染逻辑物化扩展 + env 密钥 + `--model`，使 Test 结果反映绑定供应商的真实连通性；`cliprober.ProbeRequest` 增加扩展注入。 | agent-backend.md §2.3 不变量：Test 与 chat path 必须共享同一装配规则，否则对绑定供应商的 Test 会测到 Pi 自身 `~/.pi/agent`，给出误导性结果。已拒绝：Test 保持现状 —— 与 chat run 漂移，用户点了 Test 得到假阳性。 |

## 用户流程

### 绑定自定义供应商

1. 用户在 agent-backends 编辑器选择 backend 类型为 `piagent`。现在 provider 选择器会列出全部三类 LLM 供应商（`anthropic` / `openai-chat` / `openai-response`），可任意选择其一。
2. 保存时后端校验：绑定供应商的 `LLMProvider` 必须存在、`IsActive()` 为真、且 `Model` 非空，否则报配置错误，不落库。
3. 保存后 `agents.llm_provider_key` 写入该供应商的稳定 key；与 claudecode / codex 的绑定语义一致。

### 运行时（绑定供应商）

1. `chat_svc` 按 `LLMProviderKey` 查出供应商并填 `RunRequest.Provider`（本地与 daemon 两端都由同一机制提供完整记录）。
2. piagent runtime 检测 `req.Provider != nil`，执行：
   - 按供应商 key + 默认 Model 生成（若缺失）内容哈希的静态扩展 `<AppDataDir>/piagent/ext/agentre-provider-<hash>.mjs`，内容调用 `pi.registerProvider("agentre-<key>", { name, baseUrl, api, apiKey: "$AGENTRE_PI_API_KEY_<sanitizedKey>", models: [{ id: <Model>, name, contextWindow, maxTokens, reasoning: true, input: ["text","image"] }] })`；
   - 把扩展绝对路径追加到 `--extension`（与 MCP 桥扩展并列）；
   - 子进程 env 增加 `AGENTRE_PI_API_KEY_<sanitizedKey>=<APIKey>`；
   - 模型选择变为 `--model agentre-<key>/<provider.Model>`（`provider.Model` 为空已在保存时拦截，此处仅兜底为空时沿用现状不传 model）。
3. 其余流转不变：`get_state` / `get_session_stats` / 原生 Session resume / steer / abort / compact / 图片输入 / 上下文窗口上报。
4. `result.Model` 仍读 usage 帧的真实模型 id（`runtime.go` 现有逻辑），UI 展示实际模型不受影响；`models[].contextWindow` 填 `provider.ContextWindow`（0 则省略），Pi 的 `get_state` 返回该窗口，现有 `CapReportContextWindow` 路径直接受益。

### 运行时（未绑定供应商）

与现状完全一致：不传 `--provider` / `--model`（或沿用 `defaultModelForBackend` 的空回退），Pi 使用自己的 `~/.pi/agent` 配置。

### 测试连通性（Test connectivity）

1. 用户在 agent-backends 编辑器对绑定供应商的 piagent 点 "Test connectivity"。
2. prober 用与 runtime 完全相同的渲染逻辑物化 provider 扩展、注入 env 密钥、构造 `--model`（同一 `agentruntime` 纯函数），再 spawn Pi 跑固定 ping。
3. 结果反映绑定供应商的真实连通性；失败路径与运行时一致（APIKey 空 / 扩展物化失败 / Pi 找不到注入 provider）。

## 边界与失败处理

- 供应商已绑定但 `IsActive()` 为假 / 不存在：复用 `chat_svc` 现有的不可聊天提示与错误路径，runtime 不额外处理。
- 供应商已绑定但 `APIKey` 为空：视为配置错误，本轮 Run 返回明确错误（不带密钥内容），提示用户补充 APIKey；与 daemon `FindByKey` 对空 APIKey 的检查一致。
- 扩展物化失败（写盘错误）：本轮 Run 返回错误并记录 `providerKey`（不含密钥）。
- Pi 找不到注入的 provider / 模型（例如 Pi 版本过旧不支持 `registerProvider` 或 env 引用）：走现有错误帧与诊断日志路径（`logPiFailureDiagnostics`），日志只记录 provider key / model id，不记录 APIKey。
- `ReasoningEffort`：绑定后继续透传 `--thinking`；扩展声明 `reasoning: true` 且不设 `thinkingLevelMap`，使用 Pi 默认映射。
- 日志与观察：`piTurnLogFields` 增加 provider key 字段，明确不打印密钥。

## 安全 / 隐私 / 兼容性

- **密钥**：APIKey 只进入本次子进程 env，不写入扩展文件、配置 JSON 或日志；扩展文件不含任何机密，内容按 provider 内容哈希，可安全持久化。
- **不污染用户 Pi 配置**：本功能不读改写、不删除 `~/.pi/agent` 下任何文件；用户自己的 models.json / auth.json / settings / skills / extensions / trust 完全保留。
- **兼容性**：`api` 映射固定为 `anthropic→anthropic-messages`、`openai-chat→openai-completions`、`openai-response→openai-responses`；本轮不声明高级 `compat`（thinkingFormat / cacheControl 等），用户仍可用 env_json 覆盖其它 Pi 行为（保留键 `PI_CODING_AGENT_DIR` / `PI_CODING_AGENT_SESSION_DIR` / `PI_OFFLINE` 仍被拒入）。
- **前端**：provider 选择器对 piagent 展示三类供应商；新增文案走 i18n（`zh-CN` / `en` 的 `common.json`）。

## 不在范围内

- 不实现 piagent 的反向通道扩展：`CapToolPermission` / `CapAnswerUserAsk` / `CapSetPermission` / `CapForkSession`（保持现状不支持）。
- 不改写 Pi 自己的 `~/.pi/agent` 配置（含 models.json / auth.json）。
- 不做自定义 provider 的动态模型发现（扩展只含绑定供应商的默认 `Model` 一条）。
- 不为 piagent 引入 `model_routes`（piagent 无 tier 概念，仍拒绝该字段）。
- 不为自定义 provider 做 OAuth / 多供应商同会话切换。

## 测试决策

| 接缝 | 验证内容 | 先例 |
|---|---|---|
| 纯函数「LLMProvider → 扩展源 / env 映射」 | 三种 Type 的 `api` 映射、`baseUrl` / `ContextWindow` / `MaxOutput` / `Model` 透传、`agentre-<key>` provider 命名、env 键 sanitize、`--model agentre-<key>/<model>` 组合、空 Model 时兜底行为 | `translator_test.go` 纯函数表驱动 |
| Entity `piAgentKind` | `ProviderTypeMatch` 对三类 true / 其它 false；`ValidateExtra` 放行非空 `LLMProviderKey` 但仍拒绝 `ModelRoutes` / `Sandbox` / `Approval` / `DefaultPermissionMode` / `DefaultModel` | `kinds.go` 旁 `*_test.go` |
| Runtime 集成（fake session factory） | `req.Provider != nil` 时 env 含 `AGENTRE_PI_API_KEY_*`、`--extension` 含 provider 扩展路径、`--model` 为 `agentre-<key>/<model>`；`Provider == nil` 时无这些注入；绑定 + APIKey 空返回配置错误 | `runtime_test.go` 的 `SetSessionFactoryForTest` 模式 |
| 前端 provider 选择器 | piagent 显示三类供应商、可保存绑定、Model 空校验提示 | `agent-backends` 现有测试 + `i18n.test.ts` |
| prober 对齐 | 绑定供应商的 piagent：prober 物化同一扩展 + env 密钥 + `--model`，与 chat run 同源不漂移 | `prober_test.go` + `cliprober/probe_test.go` |
| 远端 | daemon 已解 `req.Provider`，沿用 `runtime_imports_test.go` 能力协议测试；不新增 wire 字段 | `runtime_imports_test.go` |

无法自动化的部分：真实 Pi CLI 对注入 provider 的端到端调用（baseUrl 连通性 / 模型可用性）由本地人工跑一轮绑定聊天验证；wrap-up 时以源码审查 + 一次真实运行快照覆盖。

## 开放问题

（批准前必须为空）
