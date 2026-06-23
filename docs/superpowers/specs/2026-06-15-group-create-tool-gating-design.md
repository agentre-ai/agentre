# 创群工具 per-agent 门控（镜像 org 工具）— 设计

- 日期：2026-06-15
- 分支基线：`develop/group`（worktree `agentre-group-create-gating`，分支 `feature/group-create-tool-gating`）
- 关联：[`2026-06-11-agent-org-tool-design.md`](2026-06-11-agent-org-tool-design.md)、[`2026-06-13-workflow-library-relocation-and-agent-tool-design.md`](2026-06-13-workflow-library-relocation-and-agent-tool-design.md)

## 背景与动机

`group_create`（拉群带流程）当前对**所有**单聊 agent 无条件注入：`group_svc.BuildCreateTurnMCP` 只判 `groupID>0`（防群中套娃）与 `gatewayBaseURL`，**不查 per-agent 工具开关**（`create.go:30-40`）。代码注释自承认「group 工具无 agenttool registry 项，故就近定义」（`create.go:24`）。

而组织架构工具（`orgtool_svc`）与流程库工具（`workflowtool_svc`）都走 `internal/pkg/agenttool` 注册表 + `agent.ToolEnabled(key)` 双重门控（注入时 + 调用时各查一次），并在 agent 设置里有 per-agent 开关。

本设计把 `group_create` 对齐到同一套门控：**默认关、按需开，CEO 助手默认开**（镜像 `org` 的 seed 默认；注意 `workflow` 其实全员 opt-in，没有默认 migration）。

## 非目标

- **不改群内成员工具**：`group_send` / `group_invite` / `group_task_*` 由群成员身份 + `CapMCPTools` 门控，与建群开关无关，保持不变。
- **不改建群业务逻辑**：审批门、建群、brief 投递、跳转卡契约（`group created: id=… title=…`）全部不动。
- **不改前端工具开关 UI 框架**：`tool-catalog.ts` / 工具开关组件已为 org/workflow 存在，只新增一项数据。
- 不引入兼容层 / 数据回填（项目未发布，全新 DB 成立）。

## 当前状态（baseline，`develop/group`）

| 工具 | 注入门控 | 调用时复查 | 注册表项 | per-agent 开关 |
| --- | --- | --- | --- | --- |
| org | `!ToolEnabled(KeyOrg)`（`orgtool.go:52`） | `mcp.go:118` 查 DB | ✅ | ✅ |
| workflow | `!ToolEnabled(KeyWorkflow)`（`workflowtool.go:51`） | `mcp.go:118` 查 DB | ✅ | ✅ |
| **group_create** | **仅 `groupID>0`/`gatewayBaseURL`** | **无** | ❌ | ❌ |

CEO 助手（`agents.system_badge='DEFAULT'`）的 `tools_json` 经 migration `202606110001` 为 `[{"key":"org","enabled":true}]`。

---

## 改动设计

### 1. 注册表 `internal/pkg/agenttool/agenttool.go`

- 新增常量 `KeyGroupCreate = "group_create"`（刻意等于现有 `group_svc.toolKeyGroupCreate` 与审批卡 `ToolKey`，保持全链路同值）。
- `registry` 追加：`{Key: KeyGroupCreate, MCPPath: "/mcp/group/", ToolNames: []string{"group_create"}}`。
- 效果：`Keys()` 自动多出 `group_create` → `department_svc` 的 `AvailableTools` 自动带它 → 前端工具列表自动出现该开关，无需前端额外接线。

> 说明：org/workflow 的注入由通用 registry 驱动（`BuildTurnMCP` 读 `def.MCPPath/ToolNames` 并自家 mint token）；`group_create` 的注入仍走专用 `BuildCreateTurnMCP`（create token 与成员 token 不同），registry 项主要用于「出现在可用工具清单 + `ToolEnabled` 查询」。

### 2. 注入门控 `internal/service/group_svc/create.go` `BuildCreateTurnMCP`

在现有 guard 增加开关判断（镜像 `orgtool.go:52`）：

```go
if a == nil || groupID > 0 || s.gatewayBaseURL == "" || !a.ToolEnabled(agenttool.KeyGroupCreate) {
    return nil
}
```

### 3. 调用时复查 `internal/service/group_svc/create.go` `HandleGroupCreate`

create token 无状态，注入后用户可能在设置里关掉开关 → 在 handler 校验发起会话之后、审批门之前，加一次实时 DB 复查（镜像 org/workflow 的 `mcp.go:118` 防御）：

- 用 `agent_repo.Agent().Find(ctx, agentID)` 取 agent，`!a.ToolEnabled(agenttool.KeyGroupCreate)` → 返回拒绝文本（不建群、不挂审批门）。
- 放在 **service 层**（复用 `HandleGroupCreate` 已有的 repo 访问），不给 HTTP handler `groupMCP` 加新依赖——比 org/workflow 把复查塞进 mcp.go 更内聚，语义等价。
- 新增错误码 `GroupCreateToolDisabled`（zh/en 文案），经 `i18n.NewError` 返回。

### 4. 前端 `frontend/src/components/agentre/org/tool-catalog.ts`

- `APPROVAL_TOOLS` 加 `"group_create"`（建群需审批 → 渲染「需审批」徽章）。

### 5. i18n（两个 locale）

`org.agent.tools.names` / `descriptions` 各加 `group_create`：

- zh-CN：name「创建群聊」，desc「允许该 Agent 拉起多 Agent 协作群（需你审批）」
- en：name「Create Group」，desc「Allow this agent to start a multi-agent collaboration group (requires your approval)」

### 6. Migration `migrations/202606150002_group_create_tool_default.go`（追加到 `migrationList()` 末尾）

CEO 助手默认开 `group_create`（**直接覆写为固定两项数组**，因基线状态确定为仅 org）：

```sql
UPDATE agents
SET tools_json = '[{"key":"org","enabled":true},{"key":"group_create","enabled":true}]'
WHERE system_badge = 'DEFAULT'
```

Rollback 还原为 `[{"key":"org","enabled":true}]`。

> 排在群昵称 migration `202606150001` 之后（ID 自然递增），两者无关、互不影响。

---

## TDD 顺序（每步先红后绿）

1. **`agenttool_test.go`**：`Registry()` 含 `group_create`；现有 `require.Len(defs, 2)`、`Keys()==["org","workflow"]` 断言会变红 → 更新为 3 项 / `["org","workflow","group_create"]`。
2. **`group_svc` 注入测试**：开关关 → `BuildCreateTurnMCP` 返回 `nil`；开 → 返回含 `group_create` 的 spec。
3. **`group_svc` 调用复查测试**：`HandleGroupCreate` 在 agent 开关关时返回拒绝、不建群（用 mock/sqlmock 装配 agent_repo + session）。
4. **migration 测试** `202606150002_group_create_tool_default_test.go`：跑完后 `DEFAULT` 的 `tools_json` 同时含 `org` 与 `group_create`（均 enabled）。
5. **前端** `tool-catalog.test.ts`：`group_create` 带「需审批」徽章 + `enabled` 取自 agentTools；`i18n.test.ts`：两 locale 覆盖新键。

## 影响面 / 风险

- **行为变更**：改动后单聊 agent 默认不再有 `group_create`，只有 CEO 助手默认带（其余 agent 需手动勾选）。这正是需求意图（「要进行设置」）。
- e2e：拉群带流程 spec（`group-create.spec`）的发起 agent 须有 `group_create` 开关——若该 spec 用 CEO 助手发起则不受影响；否则需在 e2e seed 给对应 agent 开此工具。**实现时核对 e2e 种子**。
- 远程执行（agentred）：注入门控在 chat_svc 组 RunRequest 时按 agent 实体判定，远程无差异。
