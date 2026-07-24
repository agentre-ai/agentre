# OpenClaw Agent Backend 接入设计

> 状态：方案与 Mockup 待评审；**未进入正式实现**  
> 分支：`feat/openclaw-integration-mockup`  
> 基线：`main@5a7d1a1`

## 1. 目标与边界

在 Agentre 中新增 `openclaw` Agent Backend，让现有 Agent、组织、会话和聊天 UI 可以通过 OpenClaw Gateway 驱动 OpenClaw agent，同时保持 Agentre 现有数据模型、Wails IPC、远端 `agentred` 和 `agentruntime` 事件体系不变。

本轮只交付：

1. 接入架构与协议映射方案；
2. 能力矩阵、风险和 TDD 实施顺序；
3. 可交互静态 Mockup。

本轮明确不做：

- 不新增 `TypeOpenClaw` 正式代码；
- 不修改数据库 schema；
- 不连接真实 Gateway；
- 不保存任何真实 token；
- 不让 Codex 开始实现。

## 2. 推荐结论

**推荐把 OpenClaw 做成 Gateway-native runtime，而不是 CLI wrapper。**

- 主链路：OpenClaw Gateway WebSocket RPC；
- 备选/诊断：`openclaw agent` CLI，仅用于本地诊断或未来降级，不作为 MVP 运行链路；
- Agentre 前端仍只调用 `internal/app` Wails bindings，不新增 HTTP API；
- 本地 backend 由桌面进程直连 Gateway；远端 backend 由 `agentred` 所在设备直连 Gateway；
- 每条 Agentre `chat_session` 稳定映射到一个 OpenClaw `sessionKey`，并把该 key 存入现有 `ProviderSessionID`；
- OpenClaw token 作为专用 secret 字段处理，不写日志、不进 Mockup、不塞进通用 `env_json`。

## 3. 为什么不优先走 CLI

| 维度 | Gateway WebSocket RPC | `openclaw agent` CLI |
| --- | --- | --- |
| 流式输出 | 原生 `event` 帧 | 需解析 stdout，能力受 CLI 输出格式限制 |
| session 复用 | 原生 `sessionKey` / `sessions.*` | 可传 session，但生命周期与事件观测更弱 |
| abort | `chat.abort` | 依赖进程信号，语义更粗 |
| approvals / ask-user | 可订阅 Gateway 事件并 RPC 回写 | 很难完整映射交互控制 |
| agent / model 枚举 | `agents.list` / `models.list` | 需额外命令与解析 |
| 远端 `agentred` | daemon 内建立 WS 即可 | 需远端安装 CLI 并管理进程 |
| 协议稳定性 | 官方 Gateway 协议，版本协商 | CLI 文本输出更易变化 |
| 实现复杂度 | 需要连接管理和事件路由 | happy path 简单，但完整能力成本更高 |

OpenClaw 官方外部应用文档当前没有公开 npm client package，因此 Go 端应实现最小 Gateway client，只依赖公开 WebSocket JSON 协议，不引入非官方 JS 客户端。

## 4. 总体架构

```mermaid
flowchart LR
  UI[React / shadcn UI] -->|Wails bindings| APP[internal/app]
  APP --> CHAT[chat_svc]
  CHAT --> AR[agentruntime.Runtime]
  AR --> LOCAL[openclaw runtime local]
  AR --> REMOTE[remote runtime wire]
  REMOTE --> D[agentred]
  D --> OC[openclaw runtime daemon]
  LOCAL -->|WebSocket req/res/event| GW[OpenClaw Gateway]
  OC -->|WebSocket req/res/event| GW
  GW --> OA[OpenClaw agent + session + tools]
```

保持仓库现有六层接入方式：

1. **entity**：增加 backend type 与 OpenClaw 专属配置；
2. **repository**：迁移和 CRUD；
3. **service/app**：列表、保存、探测、枚举 agents/models；
4. **agentruntime**：Gateway client、session cache、事件翻译、控制接口；
5. **daemon/wire**：让远端运行能力和本地保持一致；
6. **frontend**：backend 编辑器、连接探测和聊天状态展示。

## 5. Backend 数据模型

### 5.1 推荐字段

`AgentBackend` 增加专用字段，不复用语义错误的 `CLIPath` / `GatewayURL`（后者当前指 Agentre LLM Provider gateway）：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `OpenClawGatewayURL` | string | `ws://127.0.0.1:18789` 或 `wss://...` |
| `OpenClawTokenCiphertext` | secret/blob | Gateway token，加密落库；任何 API 返回均只给 `hasToken` |
| `OpenClawAgentID` | string | 默认 OpenClaw agent id；为空时使用 Gateway 默认 agent |
| `OpenClawDefaultModel` | string | 可选覆盖；为空走 OpenClaw agent/session 默认 |
| `OpenClawSessionMode` | enum | MVP 固定 `per-agentre-session`，预留 `shared` / `ephemeral` |
| `OpenClawTLSInsecure` | bool | 默认 false；仅开发环境显示高级开关 |

如果仓库现有 secret storage 能力不足，MVP 可先使用 OS keyring/项目统一 secret store；**不建议把 token 放进 `EnvJSON` 或明文字段**。

### 5.2 Entity 约束

新增 `TypeOpenClaw = "openclaw"` 和 `openClawKind`：

- 不绑定 Agentre `LLMProvider`；模型与认证由 OpenClaw 管理；
- 不允许 `CLIPath`、`ModelRoutes`、`Sandbox`、`Approval`、`DefaultPermissionMode`；
- 必须有合法 `ws://` / `wss://` URL；
- token 可为空以支持 Gateway 本地无认证配置，但 UI 必须显示安全提醒；
- `DeviceID` 语义沿用：空=桌面设备直连，非空=`agentred` 设备直连。

## 6. Gateway 连接与认证

### 6.1 连接状态机

```text
idle -> connecting -> challenged -> ready
                     \-> auth_failed
connecting/ready -> disconnected -> reconnecting -> ready
                                  \-> failed
```

1. 建立 WebSocket；
2. 接收 `connect.challenge`；
3. 发送首个请求 `connect`，声明兼容协议范围、客户端身份、`operator` role 和最小 scopes；
4. 校验 `hello-ok`/连接响应；
5. 开启 reader loop：按 frame `id` 路由响应，按 `event` 分发事件；
6. 心跳/断线后指数退避重连；正在运行的 turn 不盲目重放，先查 run/session 状态。

### 6.2 最小 scopes

MVP 目标：

- `operator.read`：探测、agents/models/session/history；
- `operator.write`：发送、终止、创建 session、审批回写。

探测结果必须展示实际 granted scopes；scope 不足时保存配置可以允许，但运行前给出明确错误。

### 6.3 幂等与超时

- `agent`、`chat.send`、`sessions.send` 等副作用 RPC 每次都带 UUID idempotency key；
- transport 超时和 agent run 超时分开；
- transport 断开后，只有在确认请求未被接受时才重试；状态不确定时按 run id / session history 对账，避免重复消息。

## 7. Session 生命周期与映射

### 7.1 推荐模式：每个 Agentre chat session 对应一个 OpenClaw session

首次发送：

1. 生成稳定 session key：`agentre:<backendID>:<chatSessionID>`；
2. 可选调用 `sessions.create` 创建/收养该 key，并传 `agentId`、label、model；
3. 把 OpenClaw 返回 key 存入 `RunResult.ProviderSessionID`；
4. 后续 turn 使用相同 key 调 `chat.send` 或 `agent`。

恢复已有会话：

- `ProviderSessionID` 非空时先使用该 key；
- 重连后用 `sessions.resolve` / `sessions.describe` 核对所有权；
- 不从本地 `sessions.json` 直接读取，所有跨进程访问走 Gateway RPC。

### 7.2 新建、复用和清理

- Agentre 新建聊天：创建新 OpenClaw session key；
- Agentre 普通继续聊天：复用；
- Agentre 删除本地聊天：MVP **只删除本地映射，不自动删 OpenClaw session**，避免破坏外部数据；后续可提供显式“同时删除远端会话”；
- 重新生成：MVP 先标为不支持 provider rewind，保留本地消息后走新 session/fork；后续评估 OpenClaw `sessions.create { parentSessionKey, fork:true }`；
- compact：若 Gateway 暴露稳定 session compact RPC则映射，否则显示 capability unavailable。

## 8. Run 与事件映射

### 8.1 建议 RPC

MVP 发送优先使用 `agent`：它直接接受 `message`、`agentId`、`model`、`sessionKey`、`thinking`、`attachments`、`cwd` 和 idempotency key，并返回 run id；随后通过 Gateway agent/chat event 与 `agent.wait` 完成收尾。

`chat.send` 可作为后续对齐 Control UI 的替代入口。实现前应写协议 fixture 测试，验证当前 Gateway 版本两条路径的事件完整性，再固定一种。

### 8.2 OpenClaw → agentruntime

| OpenClaw 信号 | Agentre `agentruntime.Event` | MVP |
| --- | --- | --- |
| chat/agent text delta | `TextDelta` | 支持 |
| thinking delta | `ThinkingDelta` | 能可靠区分时支持 |
| tool start | `ToolUseStart` | 支持 |
| tool result/end | `ToolResult` / `ToolUseEnd` | 支持 |
| usage | `UsageUpdate` | 支持，未知字段忽略 |
| final | `Done` + `RunResult` | 支持 |
| aborted | terminal abort | 支持 |
| error | `ErrorEvent` | 支持 |
| retry/rate limit | `Retry` | 支持 |
| exec/plugin approval request | `ToolPermissionRequest` | MVP 视协议实测决定；至少展示 waiting 状态 |
| ask-user tool/card | `AskUserQuestion` | P1；需明确可回写协议 |
| subagent activity | `SubagentStarted/Progress/Done` | P1 |
| plan/status | `PlanUpdated` / `RuntimeStatus` | 可映射则支持 |

事件必须按 `runId + sessionKey + seq` 去重和排序；连接级事件不能误投到其它 Agentre session。

### 8.3 控制接口

| Agentre runtime interface | OpenClaw RPC | 阶段 |
| --- | --- | --- |
| `Aborter.Abort` | `chat.abort {sessionKey, runId}` | MVP |
| `Steerer.Steer` | 当前 Gateway 无等价稳定 chat steer 时，排队到下一 turn | MVP 降级 |
| `ToolPermissionSink` | exec/plugin approval resolve RPC | P1，协议 fixture 通过后开启 |
| `AskAnswerSink` | 对应 ask-user/tool result 回写 | P1 |
| `GoalController` | OpenClaw goal tool/会话能力 | P2，不在首版承诺 |
| compact/rewind | session compact/fork | P2 |

## 9. Backend 探测与配置 UX

“测试连接”不是只测 TCP，而是返回结构化结果：

1. Gateway URL 可达；
2. 协议协商成功；
3. token/auth 是否通过；
4. Gateway 版本与 protocol version；
5. granted scopes；
6. agents 列表；
7. models 列表；
8. 选中 agent/model 是否存在；
9. 若绑定远端设备，探测必须在该 `agentred` 设备执行。

UI 保存流程：

- 输入地址和 token；
- 点击“测试并加载”；
- 成功后显示 Gateway 版本、延迟、scope，并解锁 agent/model 下拉；
- token 编辑框始终不回显真实值，只显示“已保存”占位；
- agent/model 列表探测失败时允许手填，但给出 warning。

## 10. 远端 `agentred`

OpenClaw backend 应像现有 CLI backend 一样支持 `DeviceID`：

- desktop 只把 backend id / 非敏感配置和运行请求交给 daemon wire；
- token 的远端策略必须明确：
  - 推荐：token 同步到远端设备自己的 secret store，只传 secret reference；
  - 不推荐：每 turn 从 desktop 明文发送 token；
- daemon 注册 `openclaw` runtime import；
- `TestAgentBackend` 在绑定设备时通过 daemon 执行；
- capability 与本地 runtime 用同一份声明，wire 只序列化 canonical events。

首版如 secret 同步设施尚未满足，可把“远端 OpenClaw”标为 P1，MVP 先本地直连；但 entity 和 wire 设计不能堵死远端模式。

## 11. 安全与隐私

- token 永不进入日志、错误详情、Wails model、前端 state dump、测试 fixture、git tracked 文件；
- URL 日志需去掉 query/userinfo；
- 默认只允许 `ws://127.0.0.1`，非 loopback 明文 WS 显示高风险警告；远端地址建议强制 `wss://`；
- TLS 跳过校验默认关闭，并只放在高级设置；
- Gateway 返回的 tool input/result 按现有聊天数据策略落库，不能额外复制到 debug log；
- 测试使用假的 token 和本地 fake Gateway。

## 12. Capability Matrix

| 能力 | MVP | 后续 |
| --- | --- | --- |
| 本地 Gateway 连接/auth/protocol | ✅ | — |
| agent/model 探测与选择 | ✅ | 动态刷新 |
| session 新建/复用/历史核对 | ✅ | fork/delete/compact |
| 文本流式输出 | ✅ | — |
| tool call/result 展示 | ✅ | richer metadata |
| usage/model/context window | ✅ 尽力 | 精确 catalog |
| abort | ✅ | — |
| attachments | 文本/文件能力实测后择一 | 完整多模态 |
| approvals | waiting 展示；回写视协议实测 | 完整 allow/deny/session rule |
| ask-user | ❌ | P1 |
| steer | 降级为下一 turn | 若 Gateway 提供稳定 RPC则原生支持 |
| remote `agentred` | 设计兼容，是否进 MVP取决于 secret store | ✅ |
| autonomous/background/subagent | ❌ | P1/P2 |
| CLI fallback | ❌ | 诊断/降级 |

## 13. TDD / BDD 实施顺序

严格 Red → Green → Refactor。每个 slice 先写 happy path + 至少一个错误/边界场景。

### Slice 1：entity + migration

- Red：`TypeOpenClaw` 最小合法配置通过；非法 URL、互斥字段、未知 session mode 失败；
- Green：kind、字段、migration；
- Refactor：共享 URL/secret 校验。

### Slice 2：fake Gateway protocol client

- Red：challenge/connect 成功、auth 失败、响应乱序、断线、超时、未知 event；
- Green：最小 WS client + request registry；
- Refactor：连接状态机和脱敏错误。

### Slice 3：backend probe

- Red：返回版本/scopes/agents/models；scope 不足和 agent 不存在；
- Green：service + Wails binding + daemon probe wire；
- Refactor：结构化 probe result。

### Slice 4：runtime happy path

- Red：新 session、复用 session、text delta、tool、usage、final；
- Green：OpenClaw runtime translator；
- Refactor：sealed canonical event fixtures。

### Slice 5：abort / reconnect / idempotency

- Red：abort 正确定位 run；断线不重复消息；旧 run event 不串台；
- Green：run registry、dedupe、reconcile；
- Refactor：session cache。

### Slice 6：UI

- Red：创建 OpenClaw backend；测试后加载选项；token 不回显；错误状态可恢复；
- Green：编辑器和状态 badge；
- Refactor：拆分 `OpenClawBackendFields`，避免继续放大 `agent-backends.tsx`。

### Slice 7：审批 / ask-user（P1）

必须先用真实 Gateway fixture 捕获请求/回写帧，再写回归测试；无法稳定复现则不宣称支持。

## 14. 主要风险与对策

1. **Gateway 协议版本变化**：connect 阶段协商；所有 schema fixture 带版本；未知字段宽容、未知事件记录类型不记录内容。
2. **审批事件并非普通 chat event**：MVP 不强行承诺完整回写；先以协议实测决定。
3. **OpenClaw 与 Agentre 都持久化历史**：OpenClaw 是 provider-side 会话，Agentre DB 仍是 UI 事实来源；不把远端 history 无条件覆盖本地历史。
4. **断线重试造成重复消息**：副作用 RPC 强制 idempotency key，重连后先 reconcile。
5. **远端 token 泄漏**：使用 secret reference/store；条件不满足就延后远端能力。
6. **现有 backend 编辑器过大**：新增类型时拆出专属字段组件与纯函数，避免继续堆条件分支。
7. **工具事件结构不完全统一**：Gateway adapter 负责 normalizing，chat_svc 不感知 OpenClaw 私有 schema。

## 15. 评审时需要确认的产品决策

1. 是否接受 Gateway WebSocket RPC 为唯一 MVP 主链路，CLI 只做后续诊断？
2. OpenClaw backend 默认是否采用“一条 Agentre chat session = 一条 OpenClaw session”？（推荐是）
3. MVP 是否先只支持本机 Gateway，远端 `agentred` 放 P1？（若现有 secret store 可复用，可一起做）
4. 删除 Agentre 会话时，是否默认保留 OpenClaw 远端 session？（推荐保留）
5. approvals / ask-user 是否接受分阶段：首版保证展示和 abort，完整回写在真实协议 fixture 验证后进入 P1？

## 16. Mockup

可交互 Mockup：[`docs/mockups/openclaw-integration.html`](../../mockups/openclaw-integration.html)

覆盖：

- Backend 列表中的 OpenClaw 类型；
- 新建/编辑配置、连接探测、agent/model 选择；
- 聊天页连接状态、session 映射、工具调用与审批卡；
- 断线/重连和 scope 错误展示。
