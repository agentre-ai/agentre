# Agent 可配置工具体系 + 首个工具「获取/管理组织架构」设计

日期：2026-06-11
状态：已与用户确认

## 1. 背景与目标

为组织架构页面的 agent 增加「工具」配置：用户可按 agent 开启/关闭内置工具，开启后 agent 在其会话中通过 MCP 调用这些工具。首个内置工具是「获取/管理组织架构」（key：`org`），让 agent 能查询并管理部门树与 agent 挂载。

已锁定的产品决策：

| 决策点 | 结论 |
| --- | --- |
| 工具范围 | 读 + 完整管理（查询 / 创建 / 修改（含移动）/ 删除部门和 agent） |
| 生效范围 | 该 agent 的所有会话（单聊 + 群聊） |
| 默认开关 | 默认关闭；唯一例外：CEO（`SystemBadge="DEFAULT"`）默认开启 `org` |
| 写操作安全 | **所有写操作都需用户审批**，采用自建服务端审批流（非 claude-code 原生 tool permission） |
| 审批交互 | 同步挂起 + 会话内审批卡：MCP 写工具调用阻塞等待，用户在会话流里批准/拒绝后工具拿到结果继续 |

## 2. 总体架构

```
组织架构页 agent 编辑弹窗（工具区块开关）
        │ 保存 tools_json
        ▼
agents.tools_json ──► chat_svc startTurn / group scheduler.launchDelivery
                          │ 按 enabled 工具组装 MCPServerSpec（含 (agentID, sessionID) token）
                          ▼
              claude-code / codex 子进程（--mcp-config，org 工具全量进 allowedTools）
                          │ MCP HTTP 调用
                          ▼
              gateway /mcp/org/ ──► orgtool_svc（token + 开关校验）
                          ├─ 读工具：直接调 department_svc / agent_svc 返回
                          └─ 写工具：落 OrgApprovalBlock(pending) + emit 流事件
                                      │ 前端审批卡 批准/拒绝
                                      ▼
                          chat_svc.AnswerOrgApproval ──channel──► 唤醒挂起 handler
                                      ├─ 批准：执行写操作，结果返回 agent
                                      └─ 拒绝/超时：返回拒绝信息
```

## 3. 数据模型与工具注册表

### 3.1 `agents.tools_json`

- 新 migration **追加到 `migrationList()` 末尾**（禁止改既有 migration），native SQL 给 `agents` 加列：`tools_json TEXT NOT NULL DEFAULT '[]'`。
- 同一 migration 内：将 `SystemBadge="DEFAULT"` 的 CEO agent 的 `tools_json` 置为 `[{"key":"org","enabled":true}]`。
- `agent_entity.Agent` 仿 Skills 增加：

```go
type AgentToolItem struct {
    Key     string `json:"key"`
    Enabled bool   `json:"enabled"`
}
func (a *Agent) GetTools() []AgentToolItem
func (a *Agent) SetTools(items []AgentToolItem)
func (a *Agent) ToolEnabled(key string) bool // 列表中存在且 enabled
```

- DTO/保存链路与 Skills 平行：`agent_svc` Create/Update 请求带 tools；`department_svc.Load` 响应的 agent 项带 tools，前端据此渲染开关。

### 3.2 内置工具注册表

- 新包 `internal/pkg/agenttool`（leaf 层，不反向 import service）：

```go
type Definition struct {
    Key string // "org"
    // MCP server 挂载路径段，如 "org" → gateway /mcp/org/
    MCPPath string
    // 该工具暴露的 MCP tool 名列表（用于 allowedTools 组装）
    ToolNames []string
}
func Registry() []Definition
func Lookup(key string) (Definition, bool)
```

- 注册表只描述静态元数据；MCP handler 实现在 service 层（见 §4），由 bootstrap 挂到 gateway。
- 前端可用工具清单：`department_svc.Load`（或既有 org wails binding）响应附 `availableTools: ["org"]`；工具名称/描述文案在前端 i18n 按 key 映射，后端不下发文案。

## 4. org MCP server（新域 `orgtool_svc`）

新包 `internal/service/orgtool_svc`，职责仅为「工具接入 + 审批编排」，业务落地全部调既有 `department_svc` / `agent_svc` 方法（不绕层、不碰 repo）。

### 4.1 挂载与鉴权

- bootstrap 注册：`gw.RegisterMCP("/mcp/org/", orgtool_svc.Default().MCPHandler())`（与 group MCP 并列）。
- 无状态 HMAC token，仿 `group_svc/mcp.go`，但签发粒度为 **(agentID, sessionID)**：`b64url(agentID:sessionID).b64url(HMAC-SHA256(secret, agentID:sessionID))`。sessionID 用于把审批卡路由回发起调用的会话（同一 agent 可并发多会话）。
- handler 每次调用校验：token 签名有效 **且** 该 agent 当前 `ToolEnabled("org")`（用户关掉开关后旧 token 立即失效）。

### 4.2 工具清单（7 个）

| MCP tool | 类型 | 行为 |
| --- | --- | --- |
| `org_get` | 读 | 返回组织架构的 LLM 投影：部门树（id/名称/描述/parent/lead）、agent（id/名称/描述/挂载位置/backend 摘要）。不下发头像 base64/prompt/skills/时间戳等前端专用字段；不单列全量 backend 清单——agent 项自带 backend 摘要，`org_create_agent` 缺省继承调用者 backend |
| `org_create_department` | 写 | → `department_svc.Create` |
| `org_update_department` | 写 | 改名/描述/lead 等 → `Update`；请求含 `parent_id` 变更时内部分发 `Move` |
| `org_delete_department` | 写 | → `department_svc.Delete`（级联语义沿用现状） |
| `org_create_agent` | 写 | → `agent_svc.Create`；`backend_id` 可选，缺省继承调用者 agent 的 backend |
| `org_update_agent` | 写 | → `Update`；挂载位置（department_id / parent_agent_id）变更时内部分发 `Move` |
| `org_delete_agent` | 写 | → `agent_svc.Delete`；CEO 不可删等防护沿用 svc/entity 既有校验，校验失败原样返回错误信息 |

- 读工具直接执行；写工具一律走 §5 审批。
- 工具入参/出参 schema 在 orgtool_svc 内定义；错误（校验失败、循环挂载等）以 MCP tool error 文本返回，让 agent 可自行纠正重试。

## 5. 审批流（核心新机制）

### 5.1 流程

1. MCP 写工具调用进入 handler → 生成 requestID，在 token 对应会话中**落一条 pending 的 `OrgApprovalBlock`**（存 `chat_messages.blocks`，仿 `ToolPermissionBlock`，不新建表），内容含：工具名、入参、人类可读操作摘要（如「创建部门『市场部』，上级：总部」）、状态 pending。
2. emit 流事件（`org_approval_request`，走既有 StreamEvent 通道）→ 前端渲染审批卡。
3. handler goroutine 在进程内 channel 上阻塞等待（按 requestID 路由）。
4. 用户点批准/拒绝 → 前端调新增 wails binding → `chat_svc.AnswerOrgApproval(ctx, sessionID, requestID, allow)` → 更新 block 状态（approved/denied）+ emit resolved 事件 + 向 channel 投递决定。
5. 批准：handler 执行对应 svc 写操作，把执行结果（成功摘要或业务错误）作为 tool result 返回 agent；拒绝：返回固定拒绝文案（首期不带拒绝理由输入）。

### 5.2 超时与悬空

- 挂起等待默认 **4 分钟**超时（spike 实测：CLI 默认 60s，`MCP_TIMEOUT`/`MCP_TOOL_TIMEOUT` env 可拉长但有 ~285s 二级硬顶，故取 4min 留余量）；超时＝拒绝，block 置 expired，tool result 返回超时拒绝文案。
- app 重启导致挂起请求丢失：pending block 无人认领，前端对「非活跃流」的 pending 卡渲染为过期态（与 tool permission 悬空语义对齐）；不做持久化恢复。

### 5.3 与原生 permission 的关系

- org 工具的**读写全部进 `allowedTools`**，CLI 层直接放行，不触发 claude-code 原生 tool permission——避免双重审批弹卡；审批职责完全在服务端。
- 因为审批在服务端：`bypassPermissions` 模式同样拦得住；codex 等 backend 一视同仁。

### 5.4 已知风险（plan 阶段必须先验证）

- claude-code CLI 对 MCP tool 调用自身有超时（默认量级远小于 5 分钟）。spawn 时需通过 env（`MCP_TIMEOUT` / `MCP_TOOL_TIMEOUT` 类）调大并实测；若 CLI 侧无法拉长，则把审批超时压短到 CLI 限制以内，并在审批卡文案中提示剩余时间。该验证是实现计划的第一个 spike 任务。

## 6. 注入与门控

- **单聊**：`chat_svc` 组装 `RunRequest` 前（`turnExtras` 填充处，参考 `chat.go` 中“单聊零值，群聊由 group_svc 填”的注释位置），按 `session.agent_id` 读 agent 的 enabled 工具 → 对每个 enabled 工具构建 `MCPServerSpec`（URL 指向 gateway `/mcp/org/`，Authorization 带 (agentID, sessionID) token）+ 合并 `allowedTools`。
- **群聊**：`group_svc/scheduler.launchDelivery()` 在 group MCP 之外按成员 agent 的工具配置追加 org MCP（mcp-config 支持多 server）。
- **能力门控**：backend 无 `CapMCPTools` → 不注入，软降级（与 group 一致）。
- 注入逻辑做成可复用 helper（如 `orgtool_svc.BuildMCPForAgent(ctx, agentID, sessionID)`），单聊/群聊两处调用，避免重复。
- **已知限制（remote backend 暂不支持）**：MCP server URL 是桌面端回环（`127.0.0.1:<port>/mcp/org/`），远端 agentred daemon 的 gateway 不挂该 handler——远端 CLI 拨不通 MCP server，org 工具静默缺失（不挂起、不报错）。与 `group_send` 同源限制，待 remote MCP 通道方案统一解决。

## 7. 前端

### 7.1 工具配置区块

- `org-detail-agent.tsx` 新增「工具」区块，紧邻 Skills 区块，交互样式参考 Skills badge 开关。
- 数据：可用工具清单来自后端（§3.2），agent 当前开关来自 tools 字段；保存走既有 agent 保存链路（`use-org-data.ts`）。
- 文案 i18n：`zh-CN/en common.json` 增加工具区块标题 + 每个工具的名称/描述（按 key 映射，如 `org.tools.org.name`）。

### 7.2 审批卡

- 新 canonical block 组件（block 类型 `org_approval`），视觉对齐 `canonical-tool/tool-permission/card.tsx`：操作摘要 + 入参细节（可折叠）+ 批准/拒绝按钮；resolved/expired 态只读展示结果。
- 群聊中审批卡出现在调用成员发言流位置（backing session 事件已有冒泡通道）。
- 事件接线：`org_approval_request` / resolved 事件进既有流事件分发，pending 状态参与「等待用户」会话状态展示（对齐 `IsWaitingForUser` 语义，plan 阶段确认是否复用 waiting 状态）。

## 8. 测试（严格 TDD，Red → Green → Refactor）

- **entity**：`GetTools/SetTools/ToolEnabled` 序列化与边界（空串/坏 JSON → 空列表）。
- **migration**：加列 + CEO 默认开启的迁移测试（沿用 migrations 自身可连库的例外）。
- **orgtool_svc**（repo/svc 依赖走 mockgen 注入，不连库）：token 签发/校验/篡改拒绝；开关关闭后调用被拒；`org_get` 聚合；写工具审批挂起 → 批准执行 / 拒绝 / 超时三分支；update 含挂载变更分发 Move。
- **chat_svc / group scheduler 注入**：enabled 才注入、`CapMCPTools` 门控、allowedTools 合并、token 粒度 (agentID, sessionID)。
- **AnswerOrgApproval**：block 状态机（pending→approved/denied/expired）、重复应答幂等。
- **前端 Vitest**:工具区块开关交互、审批卡三态渲染与按钮回调、i18n key 覆盖（`i18n.test.ts`）。
- **e2e（可选，plan 阶段定）**：fake runtime 已会做 MCP HTTP 客户端（group_send 先例），可扩展调 `org_get` + 一个写工具验证审批卡出现与批准后落库（`node:sqlite` oracle 查 departments 表）。

## 9. 不在本期范围

- 审批「总是允许 / 本会话记住选择」。
- 组织架构页的审批中心/待办列表入口（仅会话内审批卡）。
- Skills 开关的实际消费逻辑（本期不动，仍为展示用途）。
- 第二个及以后的内置工具（注册表已留好扩展点）。
- 拒绝时填写理由。

## 10. 实施切分建议（供 writing-plans 参考）

1. spike：CLI MCP tool 超时实测（§5.4），定审批超时参数。
2. 数据层：migration + entity + DTO 链路。
3. orgtool_svc：token + 读工具 + MCP 挂载。
4. 审批流：block + 事件 + AnswerOrgApproval + 写工具挂起编排。
5. 注入：单聊 + 群聊 + 门控。
6. 前端：工具区块 + 审批卡 + i18n。
7. （可选）e2e 扩展。
