# Agent 工具「调用子 Agent」(subagent) 设计

日期：2026-06-15
状态：已与用户确认

## 1. 背景与目标

在已有「Agent 可配置工具体系」之上新增第三个内置工具 `subagent`（前两个为 `org` / `workflow`），让一个 agent 能把一段子任务委派给**另一个已配置的 agent** 执行，并**同步拿回该子 agent 的最终输出**——语义对齐各 backend 原生的 sub-agent / Task 工具，但跨 backend 通用（claude-code 的 agent 可委派给 codex / piagent 的 agent，反之亦然）。

已锁定的产品决策：

| 决策点 | 结论 |
| --- | --- |
| 子 agent 身份 | **已配置的具名 agent**（按名字调用，跑它自己的 system prompt / backend / 已启用工具），非临时匿名 sub-agent |
| 返回语义 | **纯同步阻塞**：工具调用阻塞直到子 agent 跑完，最终输出原样作为 tool result 返回 |
| 会话模型 | **每次调用新建隔离的一次性会话**：调用间无记忆，不污染子 agent 自己的主会话；并发调用天然互不干扰。会话**继承调用方会话的项目/工作目录**（子 agent 因此能在调用方项目里干活），且**调用结束后保留**（不软删，可事后查看）|
| 工具配置 | 与 `org` 工具一致——**按 agent 开启/关闭**（`agents.tools_json`），默认关闭，`CapMCPTools` 门控 |
| 安全 | 无逐次审批门（启用开关即授权，同 `group_send`）；**环/自调用检测**兜底（**不设深度上限**——委派不能成环，环检测已把链长天然封顶在「不同 agent 数」内）；**不设应用层超时**（让子 agent 跑到完成；调用方 CLI 的 MCP 调用自身有上限，届时 ctx 取消，据此中止子 agent）；子 agent 的文件写仍走其自身 permission mode |

> 与 §9「不在本期范围」配合阅读：临时匿名 sub-agent、逐次审批、跨调用记忆均不在本期。

## 2. 总体架构

```
组织架构页 agent 编辑弹窗（工具区块：subagent 开关）
        │ 保存 tools_json
        ▼
agents.tools_json ──► chat_svc runTurn / group scheduler.launchDelivery
                          │ enabled → 组装 subagent MCPServerSpec（含 (agentID, sessionID) token）
                          ▼
              claude-code / codex / piagent 子进程（--mcp-config）
                          │ MCP HTTP 调用 agent_list / agent_call
                          ▼
              gateway /mcp/subagent/ ──► subagent_svc（token + 开关 + 深度/环 校验）
                          ├─ agent_list：投影返回可调用 agent 清单
                          └─ agent_call(agent_name, prompt)：
                                  ① 解析 agent_name → 子 agent
                                  ② 新建一次性隔离会话
                                  ③ chat_svc.Send + ObserveTurn 阻塞等待
                                  ④ 子 agent 最终文本作为 tool result 返回
```

核心复用既有「一个 agent 触发另一个 agent 起轮」的机制——`group_svc/scheduler.launchDelivery()` 已经在做 `EnsureSession → chat_svc.Send → ObserveTurn → 后台收结果`；本工具是它的**同步、点对点、结果回灌调用方**版本。

## 3. 工具注册表与门控

### 3.1 注册表条目

- `internal/pkg/agenttool` 新增：

```go
const KeySubagent = "subagent"

// registry 追加
{Key: KeySubagent, MCPPath: "/mcp/subagent/", ToolNames: []string{"agent_list", "agent_call"}}
```

- 注册表只描述静态元数据；MCP handler 实现在 service 层（§4）。

### 3.2 门控（与 org 工具同源）

- 按 agent：`a.ToolEnabled(KeySubagent)`（沿用 `agent_entity.Agent` 既有的 `GetTools/SetTools/ToolEnabled` + `tools_json`，**无新 migration**——只是多一个合法 key；默认关闭，**不复制** org 的「CEO 默认开启」特例）。
- 能力门控：backend 无 `CapMCPTools` → 不注入，软降级（与 org / group 一致）。
- 实时校验：MCP handler 每次调用校验 token 签名有效 **且** 该 agent 当前 `ToolEnabled(KeySubagent)`（关掉开关后旧 token 立即失效）。

## 4. subagent MCP server（新域 `subagent_svc`）

新包 `internal/service/subagent_svc`，职责仅为「工具接入 + 子 agent 起轮编排」：解析 agent、建一次性会话、调 `chat_svc` 起轮、回灌结果。业务落地全部调既有 service（`agent_svc` 查 agent、`chat_svc` 起轮），不绕层、不碰 repo。

### 4.1 挂载与鉴权

- bootstrap：`gw.RegisterMCP("/mcp/subagent/", subagent_svc.Default().MCPHandler())` + `chat_svc.RegisterTurnMCPProvider(subagent_svc.Default().BuildTurnMCP)`（与 org / workflow 并列）。
- 无状态 HMAC token，签发粒度 **(agentID, sessionID)**，仿 `orgtool_svc`；`BuildTurnMCP` 按 `ToolEnabled(KeySubagent)` 决定是否下发 spec。

### 4.2 工具清单（2 个）

| MCP tool | 入参 | 行为 |
| --- | --- | --- |
| `agent_list` | 无 | 返回可调用 agent 的 LLM 投影：`name` / `description`(角色/职责) / `backend` 摘要。不下发头像 base64 / prompt / skills / 时间戳。供调用方发现可委派对象（无需启用 `org` 工具） |
| `agent_call` | `agent_name`(必填), `prompt`(必填) | 跑该 agent 为子 agent，**阻塞**直到其完成，最终助手文本作为 tool result 返回 |

- 入参/出参 schema 在 subagent_svc 内定义；错误（未知 agent、环违规、子 agent 出错/取消）以 MCP tool error 文本返回，让调用方可自行纠正/重试。
- `agent_list` 列全部已配置 agent（含调用者自身——自调用由环检测拦截，文案提示）。

### 4.3 `agent_call` 执行流程

1. 解析 `agent_name` → 子 agent（找不到 → MCP error，附 `agent_list` 提示）。
2. **环校验**（§5）：成环/自调用 → MCP error，不起轮（不设深度上限）。
3. 读调用方会话项目（`SessionProjectID(parentSessionID)`），新建**一次性隔离会话**（`SessionPurposeSubagentCall`，always-new；**继承该项目/工作目录**；不复用子 agent 主会话，不复用历史调用会话）。
4. 组 `chat_svc.SendRequest`：`SessionID`=一次性会话、`AgentID`=子 agent、`Text`=prompt、`MCPServers`/`SystemPromptSuffix` 走子 agent 的常规 `appendTurnMCP` 注入（子 agent 自身启用的 org/workflow/subagent 等工具照常生效）、`EmitTurnStartedBypass=true`、并携带 **call-chain**（§5）。
5. `ObserveTurn(sessionID)` 订阅 → `chat_svc.Send` → 阻塞在结果 channel（仿 `launchDelivery`，但前台等待而非后台 `gogo.Go`）。**不设应用层超时**；另一 select 分支监听 `ctx.Done()`（调用方 CLI 放弃/用户取消）→ `Stop` 子 agent + 返回取消 error。
6. 取子 agent 最终助手文本作为 tool result 返回；子 agent 出错/被取消 → MCP error；子 agent 只产生工具调用无文本 → 返回简短说明。
7. 调用结束清理 chain 映射（进程内）。**一次性会话本身保留**（不软删，可事后查看子 agent 干了什么）。

## 5. 安全：环检测（无深度上限）

子 agent 自身也可能启用 `subagent` 工具，存在「A→B→A」环或无限委派风险，需兜底：

- **call-chain**：一条委派链上的 agentID 列表。顶层（用户单聊/群聊）会话不在 chain 中。
- **进程内映射**：subagent_svc 维护 `一次性会话ID → call-chain`。`agent_call` 进来时按 token 里的 `parentSessionID` 查父 chain（父若是普通会话则为空），`newChain = parentChain + [parentAgentID]`。
- **判定**：`calleeAgentID ∈ newChain`（成环/自调用）→ 拒绝，MCP error 文本返回。**不设深度上限**——委派关系不能成环，故环检测已把链长天然封顶在「不同 agent 数」内，不会无限递归。
- **登记/清理**：起轮前把 `一次性会话ID → newChain` 登记进映射，调用结束（含出错/取消）清理。
- 一次性会话 + 同步语义下，chain 仅在调用存活期间有意义；进程重启会连带整条链一起终止，故**进程内映射足够，无需新增 DB 列**。

## 6. 无应用层超时与 permission mode

### 6.1 不设应用层超时

- **不在 subagent_svc 侧设超时**：让子 agent 跑到完成。`agent_call` 阻塞在结果 channel，仅另设 `ctx.Done()` 分支兜底。
- 调用方 CLI 对 MCP tool 调用自身有上限（默认 ~60s，`MCP_TIMEOUT`/`MCP_TOOL_TIMEOUT` env 已在 claudecode spawn 处设为 `600000`，但实测有 ~285s 二级硬顶）。届时调用方 CLI 放弃 → MCP HTTP 请求 ctx 取消 → `ctx.Done()` 命中 → `Stop` 子 agent + 返回取消 error（不留悬空 turn）。
- 即「我们不主动施加更短的限制」，长任务能否完成取决于调用方 CLI 的 MCP 上限。

### 6.2 子 agent 的 permission mode（重要）

- 子 agent 在**无人值守**上下文运行：没有交互式审批人。若子 agent 的 permission mode 会因工具审批挂起，调用将停滞直至超时。
- 本期：子 agent 一次性会话沿用其 backend 的 launch 默认 mode，并**在文档中明确**——拟作为子 agent 调用对象的 agent 应配置为 `acceptEdits` / `bypassPermissions`，避免挂起。
- **无逐次审批门**（区别于 org 写工具）：启用开关即授权，同 `group_send`；子 agent 自身的文件写仍走其自身 permission mode。

## 7. 注入与门控（复用 org 既有路径）

- 单聊 / 群聊注入、`CapMCPTools` 门控、allowedTools 合并，全部沿用 `chat_svc.appendTurnMCP` + `RegisterTurnMCPProvider` 既有路径，subagent_svc 只提供 `BuildTurnMCP`。
- **已知限制（remote backend 暂不支持）**：MCP server URL 是桌面端回环（`127.0.0.1:<port>/mcp/subagent/`），远端 agentred daemon 的 gateway 不挂该 handler——远端 CLI 拨不通，subagent 工具静默缺失。与 `org` / `group_send` 同源限制，待 remote MCP 通道统一方案解决。

## 8. 前端

- `org-detail-agent.tsx` 工具区块（org/workflow 所在区块）新增 `subagent` 开关，交互/样式与 org 开关一致；保存走既有 agent 保存链路。
- 文案 i18n：`zh-CN/en common.json` 增加 `subagent` 工具的名称/描述（按 key 映射）；`i18n.test.ts` 覆盖。
- 子 agent 调用在转录区**复用既有 tool-call 渲染**，不新增 canonical block 组件。

## 9. 不在本期范围

- 临时匿名 / ad-hoc sub-agent（仅具名已配置 agent）。
- async / handle + 轮询返回（仅同步；单次调用受调用方 CLI 的 MCP 上限约束）。
- 跨调用记忆（每次新建一次性隔离会话，调用间无记忆；会话保留但不被复用）。
- 逐次审批门、子 agent 审批回灌到调用方会话。
- 子 agent 调用对象的白名单/可见性配置（本期可调用全部已配置 agent）。
- remote agentred backend 的 subagent 工具（同 org / group 限制）。

## 10. 测试（严格 TDD，Red → Green → Refactor）

- **agenttool 注册表**：`KeySubagent` 条目存在、`Lookup` 命中、`ToolNames` 正确。
- **subagent_svc**（依赖走 mockgen 注入，不连库）：
  - token 签发/校验/篡改拒绝；开关关闭后调用被拒。
  - `agent_list` 投影（含字段裁剪：无头像/prompt）。
  - `agent_call` happy path：解析 agent → 建会话 → Send/Observe → 返回最终文本（`chat_svc` 起轮/订阅接口以 mock 注入）。
  - 未知 agent_name → error。
  - **环检测**（callee 已在链中 / 自调用 拒绝；**无深度上限**——长无环链放行）。
  - **ctx 取消分支**：取消 → `Stop` 子 agent + 取消 error。
  - 子 agent 出错 → error 透传。
  - **一次性会话**：每次新建、不复用、**继承调用方项目**、调用后清理 chain 映射（会话保留不删）。
- **注入**：enabled 才注入、`CapMCPTools` 门控、token 粒度 (agentID, sessionID)。
- **前端 Vitest**：工具区块 subagent 开关交互、i18n key 覆盖。
- **e2e（可选，plan 阶段定）**：fake runtime 已会做 MCP HTTP 客户端（group_send / org 先例），可扩展 `agent_call` 验证子 agent 真起轮并回灌（`node:sqlite` oracle 查 chat_sessions/messages）。

## 11. 实施切分建议（供 writing-plans 参考）

1. agenttool 注册表加 `KeySubagent`（+ 测试）。
2. subagent_svc：token + MCP 挂载 + `agent_list`。
3. `agent_call`：会话编排（Send/Observe 阻塞）+ 结果回灌。
4. 深度/环检测 + 超时处理。
5. 注入接线（bootstrap + BuildTurnMCP）+ `CapMCPTools` 门控。
6. 前端：工具区块开关 + i18n。
7. （可选）e2e 扩展。
