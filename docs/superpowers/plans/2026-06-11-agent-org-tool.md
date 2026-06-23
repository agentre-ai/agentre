# Agent 可配置工具体系 + org 组织架构工具 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 组织架构页 agent 可按开关启用内置工具；首个工具「获取/管理组织架构」(key=`org`) 以 MCP server 注入 agent 的所有会话，读直接执行、写操作走服务端审批（同步挂起 + 会话内审批卡）。

**Architecture:** spec 见 `docs/superpowers/specs/2026-06-11-agent-org-tool-design.md`。`agents.tools_json` 存开关；`internal/pkg/agenttool` 注册表描述内置工具；新域 `orgtool_svc` 挂 gateway `/mcp/org/`（HMAC token 粒度 (agentID, sessionID)），读走 `department_svc.Load`，写经 `chat_svc.BeginOrgApproval` 落 pending `OrgApprovalBlock` + 推流事件，前端审批卡批准/拒绝后经 `orgtool_svc.AnswerOrgApproval` 唤醒挂起的 MCP handler 执行/拒绝。注入走 `chat_svc.RegisterTurnMCPProvider` DIP 钩子，在 `runTurn` 单点生效（单聊、群聊 backing session、Regenerate 全覆盖——**这是对 spec §6「单聊+群聊两处注入」的实现层简化，行为一致**，群聊成员轮也经 `chat_svc.Send→startTurn→runTurn`）。

**对 spec §5.1 的实现澄清（执行者必读）：** 审批 block 不另起消息行。chat_svc 在内存登记 pending block（带活跃 turn 流名），turn finalize 时 merge 进本轮 assistant 消息的 finalBlocks 落库（与 ToolPermissionBlock 同归宿）；中途 LoadSession 时把内存 pending block overlay 到末条 assistant 消息的投影上（不写库），保证中途打开/刷新会话能看到审批卡。原因：in-flight assistant 消息的 BlocksJSON 在 finalize 被 acc 整体覆盖，外部直写必丢；而单独消息行会破坏 `activeStreamName`（chat.go:427 取末条 assistant 行重建流名）。

**已知行为（非 bug，沿用群聊语义）：** mcp-config 只在 CLI 子进程 spawn 时注入（见 group_svc/mcp.go:20 注释）。工具开关变更对已 spawn 的常驻子进程不生效，下次 spawn（新会话/进程 evict 重建）生效。token 确定性（同 (agent,session) 同值），跨重启验签因 per-process secret 重建而失效 → handler 返回 401，CLI 报错，agent 可感知。

**Tech Stack:** Go 1.26 + gormigrate + goconvey/testify + mockgen；React 19 + TS + Zustand + react-i18next + Vitest。

---

## File Structure

| 文件 | 责任 | 动作 |
| --- | --- | --- |
| `migrations/202606110001_agent_tools.go`(+`_test.go`) | agents.tools_json 列 + CEO 默认开启 | **Create** |
| `internal/model/entity/agent_entity/agent.go`(+`agent_test.go`) | `AgentToolItem` + `GetTools/SetTools/ToolEnabled` + Check 校验 | Modify |
| `internal/pkg/agenttool/agenttool.go`(+`_test.go`) | 内置工具注册表（leaf 层，纯元数据） | **Create** |
| `internal/service/department_svc/types.go` | `AgentToolDTO` + `AgentItem.Tools` + `LoadOrgResponse.AvailableTools` | Modify |
| `internal/service/department_svc/department.go` | Load 填 Tools/AvailableTools | Modify |
| `internal/service/agent_svc/types.go` / `agent.go` | Create/Update 请求带 Tools → entity | Modify |
| `internal/service/chat_svc/blocks/org_approval.go`(+`_test.go`) | `OrgApprovalBlock` | **Create** |
| `internal/service/chat_svc/org_approval.go`(+`_test.go`) | Begin/Finish/take/snapshot + 投影 | **Create** |
| `internal/service/chat_svc/chat.go` | activeTurnStreams 登记、finalize merge、LoadSession overlay、toChatMessage case、TurnMCPProvider 注入 | Modify |
| `internal/service/chat_svc/types.go` | `ChatBlockOrgApproval` | Modify |
| `internal/pkg/agentruntime/runtimes/claudecode/session.go`(+测试) | MCP 注入时追加超时 env | Modify |
| `internal/service/orgtool_svc/{orgtool,mcp,deps,types}.go`(+测试) | token/handler/审批编排/工具执行 | **Create** |
| `internal/bootstrap/cago.go` | 挂 `/mcp/org/` + 注册 provider/baseURL | Modify |
| `internal/app/orgtool.go` | `AnswerOrgApproval` wails binding | **Create** |
| `frontend/src/components/agentre/org/org-detail-agent.tsx` | 「工具」开关区块 | Modify |
| `frontend/src/components/agentre/org/types.ts` | OrgAgent.tools | Modify |
| `frontend/src/components/agentre/org-approval/card.tsx`(+test) | 审批卡组件 | **Create** |
| `frontend/src/components/agentre/chat-streams-host.tsx` | kind `org_approval` 分发 | Modify |
| `frontend/src/stores/chat-streams-store.ts`(+test) | `appendLiveOrgApproval/markOrgApprovalResolved` | Modify |
| `frontend/src/components/agentre/transcript-rows.ts` / `transcript-row-view.tsx` | block type `org_approval` 行 | Modify |
| `frontend/src/i18n/locales/{zh-CN,en}/common.json` | 工具区块 + 审批卡文案 | Modify |

每个 Task 结束跑 `gofmt`/相关测试后即 commit（gitmoji）。后端聚焦测试统一形如 `go test -race -run TestXxx ./internal/...`；全量回归用 `make test-backend`。

---

## Task 0: Spike — claude-code CLI 的 MCP 工具调用超时实测

**目的（spec §5.4）：** 验证 CLI 对挂起的 MCP tools/call 能等多久、`MCP_TIMEOUT` / `MCP_TOOL_TIMEOUT` env（毫秒）能否拉长，决定 `approvalTimeout` 取值。**此任务产出结论，不产出生产代码。**

- [ ] **Step 1: 起一个阻塞 MCP server**

```bash
cat > /tmp/spike-mcp.mjs <<'EOF'
import http from "node:http";
http.createServer((req, res) => {
  let body = "";
  req.on("data", (c) => (body += c));
  req.on("end", () => {
    const rpc = JSON.parse(body || "{}");
    const reply = (result) => {
      res.setHeader("content-type", "application/json");
      res.end(JSON.stringify({ jsonrpc: "2.0", id: rpc.id, result }));
    };
    if (rpc.method === "initialize")
      return reply({ protocolVersion: "2025-06-18", serverInfo: { name: "spike", version: "1" }, capabilities: { tools: { listChanged: false } } });
    if (rpc.method === "notifications/initialized") { res.statusCode = 202; return res.end(); }
    if (rpc.method === "tools/list")
      return reply({ tools: [{ name: "slow_op", description: "blocks 6 minutes then returns", inputSchema: { type: "object", properties: {} } }] });
    if (rpc.method === "tools/call") {
      console.log(new Date().toISOString(), "tools/call received, blocking 6min");
      return setTimeout(() => reply({ content: [{ type: "text", text: "done after 6min" }] }), 6 * 60 * 1000);
    }
    res.statusCode = 202; res.end();
  });
}).listen(18931, () => console.log("spike mcp on :18931"));
EOF
node /tmp/spike-mcp.mjs
```

- [ ] **Step 2: 真 CLI 调用（另开终端），分别在无 env / 有 env 下跑**

```bash
cat > /tmp/spike-mcp.json <<'EOF'
{"mcpServers":{"spike":{"type":"http","url":"http://127.0.0.1:18931"}}}
EOF
# A. 默认超时
time claude -p '调用 mcp__spike__slow_op 工具，等它返回后告诉我结果' \
  --mcp-config /tmp/spike-mcp.json --allowedTools mcp__spike__slow_op --output-format stream-json --verbose 2>&1 | tail -20
# B. 加大超时(10min, 毫秒)
time MCP_TIMEOUT=600000 MCP_TOOL_TIMEOUT=600000 claude -p '调用 mcp__spike__slow_op 工具，等它返回后告诉我结果' \
  --mcp-config /tmp/spike-mcp.json --allowedTools mcp__spike__slow_op --output-format stream-json --verbose 2>&1 | tail -20
```

- [ ] **Step 3: 记录结论**

观察 A 中 tool_result 错误出现的时间（即默认超时）与 B 是否撑过去。把结论写进本文件此处（直接编辑本 plan 追加一段「Spike 结论：默认超时 Xs；MCP_TOOL_TIMEOUT 有效/无效」），并据此定下后续 Task 中两处常量：
- `orgtool_svc` 的 `approvalTimeout`：若 env 有效 → 维持 `5 * time.Minute`；若无效 → 压到「CLI 默认超时 − 10s」。
- Task 8 的 env 注入是否保留（env 无效则删掉 Task 8，并在 commit message 注明）。

> **Spike 结论（2026-06-11 实测，claude CLI v2.1.170）**：默认 MCP 工具超时 = 60s（A 组：server 收到 tools/call 07:59:02Z，CLI 返回 is_error tool_result 08:00:02Z，耗时精确 60s，错误文本 `"The operation timed out."`）；MCP_TIMEOUT/MCP_TOOL_TIMEOUT env **有效但存在更高层硬顶**（B 组设 600000ms：server 收到 tools/call 08:00:50Z，CLI 超时 08:05:35Z，耗时 285s ≈ 4min45s，未撑过 6min，说明有约 300s 的二级上限盖住了 env 配置）；决定：approvalTimeout = **4min**（取 285s 实测上限 − 25s 余量 → 260s，向下取整到 `4 * time.Minute`）；Task 8 **保留**（env 有效，注入可把默认从 60s 提到 ~285s，审批窗口才可用）。

- [ ] **Step 4: 清理** `pkill -f spike-mcp.mjs`，无 commit。

---

## Task 1: migration — agents.tools_json + CEO 默认开启

**Files:** Create `migrations/202606110001_agent_tools.go`、`migrations/202606110001_agent_tools_test.go`；Modify `migrations/migrations.go`

- [ ] **Step 1: 写失败测试**

```go
package migrations

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMigration202606110001AddsAgentTools(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	// agents 表 + CEO seed 由 202605220004 建（依赖 202605220002 departments / 202605220003 backends）
	require.NoError(t, migration202605220002().Migrate(gdb))
	require.NoError(t, migration202605220003().Migrate(gdb))
	require.NoError(t, migration202605220004().Migrate(gdb))

	require.NoError(t, migration202606110001().Migrate(gdb))

	var ceoTools string
	require.NoError(t, gdb.Table("agents").
		Where("system_badge = ?", "DEFAULT").
		Pluck("tools_json", &ceoTools).Error)
	require.JSONEq(t, `[{"key":"org","enabled":true}]`, ceoTools)

	// 非 CEO 行默认 '[]'
	require.NoError(t, gdb.Exec(`INSERT INTO agents (name, department_id, agent_backend_id, status) VALUES ('t', 1, 1, 1)`).Error)
	var plain string
	require.NoError(t, gdb.Table("agents").Where("name = 't'").Pluck("tools_json", &plain).Error)
	require.Equal(t, `[]`, plain)
}
```

> 若 202605220004 的依赖链与上面不符（编译/跑挂），打开该文件按真实依赖补 Migrate 调用，原则：只跑建出 `agents` 表+CEO seed 所需的最少前置。

- [ ] **Step 2: 跑测试确认失败** `go test -race -run TestMigration202606110001 ./migrations/` → FAIL（`migration202606110001` 未定义）

- [ ] **Step 3: 实现**

```go
// migrations/202606110001_agent_tools.go
package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// migration202606110001 adds agents.tools_json — agent 级内置工具开关
// （首个工具 key="org"，组织架构读写）。CEO(system_badge=DEFAULT) 默认开启 org。
func migration202606110001() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202606110001",
		Migrate: func(tx *gorm.DB) error {
			if err := tx.Exec(`ALTER TABLE agents ADD COLUMN tools_json TEXT NOT NULL DEFAULT '[]'`).Error; err != nil {
				return err
			}
			return tx.Exec(`UPDATE agents SET tools_json = '[{"key":"org","enabled":true}]' WHERE system_badge = 'DEFAULT'`).Error
		},
		Rollback: func(tx *gorm.DB) error {
			return tx.Exec(`ALTER TABLE agents DROP COLUMN tools_json`).Error
		},
	}
}
```

`migrations.go` 的 `migrationList()` 末尾追加 `migration202606110001(), // agents.tools_json + CEO 默认开启 org`。

- [ ] **Step 4: 跑测试通过**；**Step 5: Commit** `🗃️ migration: agents.tools_json 工具开关列(CEO 默认开启 org)`

---

## Task 2: entity — AgentToolItem + GetTools/SetTools/ToolEnabled

**Files:** Modify `internal/model/entity/agent_entity/agent.go`；Test `internal/model/entity/agent_entity/agent_test.go`（已有则追加）

- [ ] **Step 1: 写失败测试**（goconvey 或 testify，跟随该文件现状；下面用 testify 风格示意）

```go
func TestAgentTools(t *testing.T) {
	t.Run("空串/坏 JSON 返回空列表", func(t *testing.T) {
		a := &Agent{}
		require.Equal(t, []AgentToolItem{}, a.GetTools())
		a.ToolsJSON = "{bad"
		require.Equal(t, []AgentToolItem{}, a.GetTools())
	})
	t.Run("SetTools/GetTools round-trip + ToolEnabled", func(t *testing.T) {
		a := &Agent{}
		a.SetTools([]AgentToolItem{{Key: "org", Enabled: true}})
		require.Equal(t, `[{"key":"org","enabled":true}]`, a.ToolsJSON)
		require.True(t, a.ToolEnabled("org"))
		require.False(t, a.ToolEnabled("other"))
		a.SetTools(nil)
		require.Equal(t, `[]`, a.ToolsJSON)
		require.False(t, a.ToolEnabled("org"))
	})
	t.Run("Check 校验 ToolsJSON 必须是 JSON 数组", func(t *testing.T) {
		a := &Agent{Name: "x", DepartmentID: 1, AgentBackendID: 1, ToolsJSON: "{bad"}
		require.Error(t, a.Check(context.Background()))
	})
}
```

- [ ] **Step 2: 跑测试失败** `go test -race -run TestAgentTools ./internal/model/entity/agent_entity/` → FAIL（ToolsJSON 未定义）

- [ ] **Step 3: 实现**（镜像 Skills：`agent.go`）

```go
// AgentToolItem Agent 内置工具开关（key 对应 internal/pkg/agenttool 注册表）。
type AgentToolItem struct {
	Key     string `json:"key"`
	Enabled bool   `json:"enabled"`
}
```

struct 中 `SkillsJSON` 下一行加：

```go
	ToolsJSON      string `gorm:"column:tools_json;type:text;not null;default:'[]'"`
```

`SetSkills` 后追加（与 GetSkills/SetSkills 同构）：

```go
func (a *Agent) GetTools() []AgentToolItem {
	out := []AgentToolItem{}
	if a == nil || a.ToolsJSON == "" {
		return out
	}
	_ = json.Unmarshal([]byte(a.ToolsJSON), &out)
	if out == nil {
		out = []AgentToolItem{}
	}
	return out
}

func (a *Agent) SetTools(items []AgentToolItem) {
	if items == nil {
		items = []AgentToolItem{}
	}
	b, _ := json.Marshal(items)
	a.ToolsJSON = string(b)
}

// ToolEnabled 报告某内置工具是否开启。
func (a *Agent) ToolEnabled(key string) bool {
	for _, it := range a.GetTools() {
		if it.Key == key {
			return it.Enabled
		}
	}
	return false
}
```

`Check()` 中 SkillsJSON 校验后追加：

```go
	if !isValidJSONArray(a.ToolsJSON) {
		return i18n.NewError(ctx, code.AgentInvalidPayload)
	}
```

- [ ] **Step 4: 跑测试通过**；**Step 5: Commit** `✨ agent_entity: AgentToolItem 工具开关(GetTools/SetTools/ToolEnabled)`

---

## Task 3: internal/pkg/agenttool — 内置工具注册表

**Files:** Create `internal/pkg/agenttool/agenttool.go`、`internal/pkg/agenttool/agenttool_test.go`

- [ ] **Step 1: 写失败测试**

```go
package agenttool

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRegistry(t *testing.T) {
	defs := Registry()
	require.Len(t, defs, 1)
	require.Equal(t, "org", defs[0].Key)
	require.Equal(t, "/mcp/org/", defs[0].MCPPath)
	require.Contains(t, defs[0].ToolNames, "org_get")
	require.Len(t, defs[0].ToolNames, 7)

	d, ok := Lookup("org")
	require.True(t, ok)
	require.Equal(t, KeyOrg, d.Key)
	_, ok = Lookup("nope")
	require.False(t, ok)

	require.Equal(t, []string{"org"}, Keys())
}
```

- [ ] **Step 2: 跑测试失败** `go test -race ./internal/pkg/agenttool/`

- [ ] **Step 3: 实现**

```go
// Package agenttool 维护 agent 级内置工具注册表(静态元数据)。leaf 层:
// 只描述 key/挂载路径/MCP tool 名,不 import service —— handler 实现在
// internal/service/orgtool_svc,由 bootstrap 按 MCPPath 挂到 gateway。
package agenttool

// Definition 一个内置 agent 工具(以 MCP server 形态注入会话)。
type Definition struct {
	Key       string   // agents.tools_json 的 key,也是 MCPServerSpec.Name
	MCPPath   string   // gateway 挂载路径
	ToolNames []string // server 暴露的 MCP tool 名(全部进 allowedTools,审批在服务端)
}

// KeyOrg 组织架构读写工具。
const KeyOrg = "org"

var registry = []Definition{{
	Key:     KeyOrg,
	MCPPath: "/mcp/org/",
	ToolNames: []string{
		"org_get",
		"org_create_department", "org_update_department", "org_delete_department",
		"org_create_agent", "org_update_agent", "org_delete_agent",
	},
}}

// Registry 返回全部内置工具定义(只读副本)。
func Registry() []Definition {
	out := make([]Definition, len(registry))
	copy(out, registry)
	return out
}

// Lookup 按 key 找定义。
func Lookup(key string) (Definition, bool) {
	for _, d := range registry {
		if d.Key == key {
			return d, true
		}
	}
	return Definition{}, false
}

// Keys 返回全部工具 key(给前端可用工具清单)。
func Keys() []string {
	out := make([]string, 0, len(registry))
	for _, d := range registry {
		out = append(out, d.Key)
	}
	return out
}
```

- [ ] **Step 4: 跑测试通过**；**Step 5: Commit** `✨ agenttool: 内置工具注册表(org 7 个 MCP tool)`

---

## Task 4: DTO 链路 — tools 进 Create/Update/Load

**Files:** Modify `internal/service/department_svc/types.go`、`internal/service/department_svc/department.go`、`internal/service/agent_svc/types.go`、`internal/service/agent_svc/agent.go`；测试加在 `internal/service/agent_svc/agent_test.go` 与 `internal/service/department_svc/department_test.go`（跟随既有 Skills 用例的组织方式——它们怎么 mock repo/造数据，tools 就怎么写）。

- [ ] **Step 1: 写失败测试**——在 agent_svc 既有 Create/Update 测试旁补「请求带 Tools → entity ToolsJSON 落值」断言；在 department_svc 既有 Load 测试旁补「AgentItem.Tools 投影 + LoadOrgResponse.AvailableTools == agenttool.Keys()」断言。镜像同文件中 Skills 的现成断言写法（搜 `Skills` 即得样例），把字段换成 Tools。

- [ ] **Step 2: 跑失败** `go test -race ./internal/service/agent_svc/ ./internal/service/department_svc/`

- [ ] **Step 3: 实现**

`department_svc/types.go`（AgentSkillDTO 旁）：

```go
// AgentToolDTO 与 agent_entity.AgentToolItem 同结构，避免前端引用 entity 包。
type AgentToolDTO struct {
	Key     string `json:"key"`
	Enabled bool   `json:"enabled"`
}
```

`AgentItem` 的 `Skills` 字段下加 `Tools []AgentToolDTO \`json:"tools"\``；`LoadOrgResponse` 加 `AvailableTools []string \`json:"availableTools"\``。

`department_svc/department.go`：`toAgentSkillDTO` 旁加同构的 `toAgentToolDTO(items []agent_entity.AgentToolItem) []AgentToolDTO`；AgentItem 投影处（搜 `Skills:` 赋值点）补 `Tools: toAgentToolDTO(a.GetTools())`；`Load` 返回前补 `resp.AvailableTools = agenttool.Keys()`（import `internal/pkg/agenttool`）。

`agent_svc/types.go`：Create/UpdateRequest 的 `Skills` 字段下各加 `Tools []department_svc.AgentToolDTO \`json:"tools"\``。

`agent_svc/agent.go`：Create/Update 中 `SetSkills` 调用旁补（镜像 skills 的 DTO→entity 转换处）：

```go
	tools := make([]agent_entity.AgentToolItem, 0, len(req.Tools))
	for _, x := range req.Tools {
		tools = append(tools, agent_entity.AgentToolItem{Key: x.Key, Enabled: x.Enabled})
	}
	a.SetTools(tools)
```

（如该文件已有 skills 的等价转换 helper，仿它抽 `toEntityTools`，勿重复内联两次。）

- [ ] **Step 4: 跑测试通过**（连带 `go build ./...` 确认无破坏）；**Step 5: Commit** `✨ agent/department_svc: tools 开关进 Create/Update/Load DTO 链路`

---

## Task 5: blocks.OrgApprovalBlock + ChatBlock 投影

**Files:** Create `internal/service/chat_svc/blocks/org_approval.go`、`internal/service/chat_svc/blocks/org_approval_test.go`；Modify `internal/service/chat_svc/types.go`、`internal/service/chat_svc/chat.go`(toChatMessage)；Create `internal/service/chat_svc/org_approval.go`（本 Task 只放投影函数，编排在 Task 6）

- [ ] **Step 1: 写失败测试**

`blocks/org_approval_test.go`（镜像 `tool_permission_test.go`：goconvey + `cagoblocks.Encode`/对应 Decode round-trip，照该文件 `TestToolPermissionBlock_FactoryRoundTrip` 的完整断言链改写字段）：

```go
package blocks

import (
	"testing"

	cagoblocks "github.com/cago-frame/agents/agent/blocks"
	. "github.com/smartystreets/goconvey/convey"
)

func TestOrgApprovalBlock_TypeAndAudience(t *testing.T) {
	Convey("OrgApprovalBlock 类型 + Audience", t, func() {
		b := OrgApprovalBlock{}
		So(b.Type(), ShouldEqual, "org_approval")
		So(b.Audience(), ShouldEqual, cagoblocks.ToUI)
	})
}

func TestOrgApprovalBlock_FactoryRoundTrip(t *testing.T) {
	Convey("OrgApprovalBlock Encode/Decode round-trip", t, func() {
		b := &OrgApprovalBlock{
			RequestID: "r1",
			ToolName:  "org_create_department",
			ToolInput: map[string]any{"name": "市场部"},
			Status:    "pending",
		}
		sb, err := cagoblocks.Encode(b)
		So(err, ShouldBeNil)
		// Decode/断言链与 TestToolPermissionBlock_FactoryRoundTrip 逐行同构(把字段换成
		// RequestID/ToolName/ToolInput/Status),包括解出指针类型断言与字段相等断言。
		_ = sb
	})
}
```

- [ ] **Step 2: 跑失败** `go test -race -run TestOrgApprovalBlock ./internal/service/chat_svc/blocks/`

- [ ] **Step 3: 实现**

```go
// internal/service/chat_svc/blocks/org_approval.go
package blocks

import cagoblocks "github.com/cago-frame/agents/agent/blocks"

// OrgApprovalBlock 组织架构工具(orgtool)写操作的服务端审批卡。
// Status: pending → approved | denied | expired(超时 / turn 中止 / app 重启悬空)。
// Result 仅 approved 后有值(执行结果或业务错误摘要)。
type OrgApprovalBlock struct {
	RequestID string         `json:"request_id"`
	ToolName  string         `json:"tool_name"`
	ToolInput map[string]any `json:"tool_input,omitempty"`
	Status    string         `json:"status"`
	Result    string         `json:"result,omitempty"`
}

func (OrgApprovalBlock) Type() string                      { return "org_approval" }
func (OrgApprovalBlock) Audience() cagoblocks.AudienceMask { return cagoblocks.ToUI }

func init() { cagoblocks.RegisterFactory[OrgApprovalBlock]() }
```

`chat_svc/types.go`：`ChatBlock` 加字段 `OrgApproval *ChatBlockOrgApproval \`json:"orgApproval,omitempty"\``，并在 `ChatBlockToolPermission` 旁定义：

```go
// ChatBlockOrgApproval 组织架构工具审批卡的前端投影。
type ChatBlockOrgApproval struct {
	RequestID string         `json:"requestId"`
	ToolName  string         `json:"toolName"`
	ToolInput map[string]any `json:"toolInput,omitempty"`
	Status    string         `json:"status"`
	Result    string         `json:"result,omitempty"`
}
```

`chat_svc/org_approval.go`（投影函数，仿 `toolPermissionBlockToChatBlock`）：

```go
package chat_svc

import "github.com/agentre-ai/agentre/internal/service/chat_svc/blocks"

// orgApprovalBlockToChatBlock 历史回放/overlay 路径：持久化 block → 前端 ChatBlock。
func orgApprovalBlockToChatBlock(b blocks.OrgApprovalBlock) ChatBlock {
	return ChatBlock{
		Type: "org_approval",
		OrgApproval: &ChatBlockOrgApproval{
			RequestID: b.RequestID,
			ToolName:  b.ToolName,
			ToolInput: b.ToolInput,
			Status:    b.Status,
			Result:    b.Result,
		},
	}
}
```

`chat.go` `toChatMessage` 中 ToolPermissionBlock 两个 case（值/指针，chat.go:730/733）旁加同构两案：

```go
		case blocks.OrgApprovalBlock:
			out.Blocks = append(out.Blocks, orgApprovalBlockToChatBlock(tb))
		case *blocks.OrgApprovalBlock:
			if tb != nil {
				out.Blocks = append(out.Blocks, orgApprovalBlockToChatBlock(*tb))
			}
```

并在既有 toChatMessage 测试处补一条 org_approval round-trip 断言（镜像 tool_permission 的用例，chat_internal_test.go 搜 `tool_permission`）。

- [ ] **Step 4: 跑通过**；**Step 5: Commit** `✨ chat_svc: OrgApprovalBlock 持久化块 + ChatBlock 投影`

---

## Task 6: chat_svc 审批编排 — Begin/Finish/finalize merge/LoadSession overlay

**Files:** Modify `internal/service/chat_svc/org_approval.go`、`internal/service/chat_svc/chat.go`；Test `internal/service/chat_svc/org_approval_test.go`

接口契约（orgtool_svc 是唯一调用方）：
- `BeginOrgApproval(ctx, sessionID, blk)`：会话无活跃 turn → error；否则登记 + emit `kind:"org_approval"`（pending）+ 翻 waiting。
- `FinishOrgApproval(ctx, sessionID, requestID, status, result)`：status ∈ approved/denied/expired；更新登记 block + emit resolved + 翻回 running；requestID 不存在（已被 finalize 取走）→ error。
- finalize：runTurn 把该会话登记的全部 block merge 进 finalBlocks，未决的标 expired。
- LoadSession：pending overlay 到末条 assistant 消息投影。

- [ ] **Step 1: 写失败测试**（chat_svc 单测不连库的部分用纯内存断言；涉及 `chat_repo.Session().Find` 的 waiting 翻转用该包既有 mock/sqlmock 手法——先读 `tool_permission` 或 `session 状态` 相关既有测试，复用其装配）

核心用例：

```go
func TestOrgApprovalLifecycle(t *testing.T) {
	s := newChatSvcForTest(t) // 跟随包内既有构造手法；emitter 用可断言的 fake

	// 无活跃 turn → Begin 拒绝
	blk := &blocks.OrgApprovalBlock{RequestID: "r1", ToolName: "org_delete_agent", Status: "pending"}
	require.Error(t, s.BeginOrgApproval(ctx, 42, blk))

	// 有活跃 turn → Begin 登记 + emit pending
	s.activeTurnStreams.Store(int64(42), StreamName(42, 7))
	require.NoError(t, s.BeginOrgApproval(ctx, 42, blk))
	// fake emitter 收到 stream=chat:42:7, payload kind=org_approval status=pending

	// Finish 更新 + emit resolved
	require.NoError(t, s.FinishOrgApproval(ctx, 42, "r1", "denied", ""))
	// fake emitter 第二条 payload status=denied

	// takeOrgApprovals: 取走 + pending 标 expired
	blk2 := &blocks.OrgApprovalBlock{RequestID: "r2", Status: "pending"}
	require.NoError(t, s.BeginOrgApproval(ctx, 42, blk2))
	got := s.takeOrgApprovals(42)
	require.Len(t, got, 2)
	// r1=denied 保留, r2 被标 expired
	require.Empty(t, s.snapshotOrgApprovals(42))

	// Finish 已取走的 requestID → error
	require.Error(t, s.FinishOrgApproval(ctx, 42, "r2", "approved", "x"))
}
```

- [ ] **Step 2: 跑失败** `go test -race -run TestOrgApprovalLifecycle ./internal/service/chat_svc/`

- [ ] **Step 3: 实现**

`chatSvc` struct（chat.go:160 区域）加：

```go
	// activeTurnStreams: sessionID(int64) → 当前活跃 turn 的 per-turn 流名(string)。
	// runTurn 起止维护;orgtool 审批(BeginOrgApproval)据此路由审批卡到正确的流。
	activeTurnStreams sync.Map
	// orgApprovals: 本会话进行中 turn 上挂起/已决的组织架构审批 block,
	// finalize 时 merge 进 assistant 消息;LoadSession 时 overlay 到投影。
	orgApprovalsMu sync.Mutex
	orgApprovals   map[int64][]*blocks.OrgApprovalBlock
```

（构造处初始化 `orgApprovals: map[int64][]*blocks.OrgApprovalBlock{}`。）

`org_approval.go` 追加：

```go
// BeginOrgApproval 在 sessionID 当前活跃 turn 上登记一条 pending 审批并推流事件。
// 无活跃 turn 返回错误(orgtool MCP handler 据此拒绝工具调用)。
func (s *chatSvc) BeginOrgApproval(ctx context.Context, sessionID int64, blk *blocks.OrgApprovalBlock) error {
	streamAny, ok := s.activeTurnStreams.Load(sessionID)
	if !ok {
		return fmt.Errorf("chat_svc.BeginOrgApproval: no active turn for session %d", sessionID)
	}
	stream := streamAny.(string)
	s.orgApprovalsMu.Lock()
	s.orgApprovals[sessionID] = append(s.orgApprovals[sessionID], blk)
	snapshot := *blk
	s.orgApprovalsMu.Unlock()

	s.emitter.Emit(ctx, stream, orgApprovalEventPayload(snapshot))
	if sess, err := chat_repo.Session().Find(ctx, sessionID); err == nil && sess != nil {
		s.markSessionWaiting(ctx, sess, stream)
	}
	return nil
}

// FinishOrgApproval 把审批置为终态(approved/denied/expired)并推 resolved 事件。
func (s *chatSvc) FinishOrgApproval(ctx context.Context, sessionID int64, requestID, status, result string) error {
	s.orgApprovalsMu.Lock()
	var snapshot *blocks.OrgApprovalBlock
	for _, b := range s.orgApprovals[sessionID] {
		if b.RequestID == requestID {
			b.Status = status
			b.Result = result
			cp := *b
			snapshot = &cp
			break
		}
	}
	s.orgApprovalsMu.Unlock()
	if snapshot == nil {
		return fmt.Errorf("chat_svc.FinishOrgApproval: request %s not found (turn finalized?)", requestID)
	}
	if streamAny, ok := s.activeTurnStreams.Load(sessionID); ok {
		stream := streamAny.(string)
		s.emitter.Emit(ctx, stream, orgApprovalEventPayload(*snapshot))
		if sess, err := chat_repo.Session().Find(ctx, sessionID); err == nil && sess != nil {
			s.markSessionRunning(ctx, sess, stream)
		}
	}
	return nil
}

func orgApprovalEventPayload(b blocks.OrgApprovalBlock) map[string]any {
	return map[string]any{
		"kind":      "org_approval",
		"requestId": b.RequestID,
		"toolName":  b.ToolName,
		"toolInput": b.ToolInput,
		"status":    b.Status,
		"result":    b.Result,
	}
}

// takeOrgApprovals finalize 时取走本会话全部审批 block;仍 pending 的标 expired
// (turn 被 abort / 子进程死亡时挂起审批不再可决)。
func (s *chatSvc) takeOrgApprovals(sessionID int64) []*blocks.OrgApprovalBlock {
	s.orgApprovalsMu.Lock()
	defer s.orgApprovalsMu.Unlock()
	out := s.orgApprovals[sessionID]
	delete(s.orgApprovals, sessionID)
	for _, b := range out {
		if b.Status == "pending" {
			b.Status = "expired"
		}
	}
	return out
}

// snapshotOrgApprovals LoadSession overlay 用:拷贝当前登记的 block(不取走)。
func (s *chatSvc) snapshotOrgApprovals(sessionID int64) []blocks.OrgApprovalBlock {
	s.orgApprovalsMu.Lock()
	defer s.orgApprovalsMu.Unlock()
	out := make([]blocks.OrgApprovalBlock, 0, len(s.orgApprovals[sessionID]))
	for _, b := range s.orgApprovals[sessionID] {
		out = append(out, *b)
	}
	return out
}
```

`chat.go` 三处接线：

1. `runTurn` 在 `events, result, err := runner.Run(ctx, req)` 成功后（确保失败路径不残留）：

```go
	s.activeTurnStreams.Store(sess.ID, stream)
	defer s.activeTurnStreams.Delete(sess.ID)
```

2. finalize merge——`finalBlocks := acc.Finalize()` 之后、`MarkRunningSubagentsCancelled` 判断之后：

```go
	for _, b := range s.takeOrgApprovals(sess.ID) {
		finalBlocks = append(finalBlocks, b)
	}
```

3. `LoadSession` 在 `resp.Messages` 全部 append 完成后：

```go
	if pend := s.snapshotOrgApprovals(sess.ID); len(pend) > 0 {
		for i := len(resp.Messages) - 1; i >= 0; i-- {
			if resp.Messages[i].Role == "assistant" {
				for _, b := range pend {
					resp.Messages[i].Blocks = append(resp.Messages[i].Blocks, orgApprovalBlockToChatBlock(b))
				}
				break
			}
		}
	}
```

- [ ] **Step 4: 跑通过** + `make test-backend` 回归；**Step 5: Commit** `✨ chat_svc: org 审批编排(Begin/Finish/finalize merge/LoadSession overlay)`

---

## Task 7: chat_svc — TurnMCPProvider 注入钩子

**Files:** Modify `internal/service/chat_svc/chat.go`（或新建 `internal/service/chat_svc/turn_mcp.go`）；Test `internal/service/chat_svc/turn_mcp_test.go`

- [ ] **Step 1: 写失败测试**——纯函数测注入逻辑（不跑真 runTurn）：把注入抽成可测 helper：

```go
func TestAppendTurnMCP(t *testing.T) {
	a := &agent_entity.Agent{ID: 5}
	base := []agentruntime.MCPServerSpec{{Name: "group"}}

	// provider 未注册 → 原样
	RegisterTurnMCPProvider(nil)
	require.Equal(t, base, appendTurnMCP(context.Background(), base, a, 42, true))

	// 注册后 + 支持 CapMCPTools → 追加
	RegisterTurnMCPProvider(func(_ context.Context, ag *agent_entity.Agent, sid int64) []agentruntime.MCPServerSpec {
		require.Equal(t, int64(5), ag.ID)
		require.Equal(t, int64(42), sid)
		return []agentruntime.MCPServerSpec{{Name: "org"}}
	})
	got := appendTurnMCP(context.Background(), base, a, 42, true)
	require.Len(t, got, 2)
	require.Equal(t, "org", got[1].Name)

	// runner 不支持 CapMCPTools → 不追加(软降级)
	require.Equal(t, base, appendTurnMCP(context.Background(), base, a, 42, false))
	RegisterTurnMCPProvider(nil) // 清理,防测试间串台
}
```

- [ ] **Step 2: 跑失败**；**Step 3: 实现**（`turn_mcp.go`）：

```go
package chat_svc

// TurnMCPProvider 按 (agent, session) 给 turn 注入额外 MCP server —— agent 级
// 内置工具体系的接缝。bootstrap 注册 orgtool_svc 的实现;nil = 不注入。
// 与群聊的 extras.mcpServers 叠加;在 runTurn 单点生效,单聊/群聊/Regenerate 全覆盖。
type TurnMCPProvider func(ctx context.Context, a *agent_entity.Agent, sessionID int64) []agentruntime.MCPServerSpec

var turnMCPProvider TurnMCPProvider

// RegisterTurnMCPProvider bootstrap 接线入口。
func RegisterTurnMCPProvider(p TurnMCPProvider) { turnMCPProvider = p }

// appendTurnMCP runTurn 在组装 RunRequest 时调用;capOK = runner 声明 CapMCPTools。
func appendTurnMCP(ctx context.Context, base []agentruntime.MCPServerSpec, a *agent_entity.Agent, sessionID int64, capOK bool) []agentruntime.MCPServerSpec {
	if turnMCPProvider == nil || !capOK {
		return base
	}
	return append(base, turnMCPProvider(ctx, a, sessionID)...)
}
```

`runTurn` 中 `MCPServers: extras.mcpServers`（chat.go:2404）改为：

```go
		MCPServers: appendTurnMCP(ctx, extras.mcpServers, a, sess.ID, runner.Capabilities().Has(capability.CapMCPTools)),
```

- [ ] **Step 4: 跑通过**；**Step 5: Commit** `✨ chat_svc: TurnMCPProvider 注入钩子(agent 工具 → RunRequest.MCPServers)`

---

## Task 8: claudecode — MCP 注入时追加超时 env（按 Task 0 结论取舍）

**Files:** Modify `internal/pkg/agentruntime/runtimes/claudecode/session.go`；测试加在该包既有 `ccBuildClientOpts` 测试旁（搜 `ccBuildClientOpts` 的 _test）

- [ ] **Step 1: 写失败测试**——断言 `len(spec.Req.MCPServers)>0` 时 opts 里 WithEnv 的 env map 含 `MCP_TIMEOUT`/`MCP_TOOL_TIMEOUT`（断言方式跟随既有测试如何检查 opts——若现状是检查生成的 args/client 字段，则同样路径断言 env）。无 MCPServers 时不含。

- [ ] **Step 2: 跑失败**；**Step 3: 实现**——`ccBuildClientOpts` 里把 `claudecode.WithEnv(spec.Env)` 改为先算 env：

```go
	env := spec.Env
	// 注入 MCP server 时拉长 CLI 的 MCP 工具调用超时:orgtool 写操作会同步挂起
	// 等用户审批(approvalTimeout=4min),默认 60s 撑不住。值为毫秒。spike 实测见
	// docs/superpowers/plans/2026-06-11-agent-org-tool.md Task 0。
	if len(spec.Req.MCPServers) > 0 {
		merged := make(map[string]string, len(env)+2)
		for k, v := range env {
			merged[k] = v
		}
		if _, ok := merged["MCP_TIMEOUT"]; !ok {
			merged["MCP_TIMEOUT"] = "600000"
		}
		if _, ok := merged["MCP_TOOL_TIMEOUT"]; !ok {
			merged["MCP_TOOL_TIMEOUT"] = "600000"
		}
		env = merged
	}
```

（`WithEnv(env)` 用新变量；spec.Env 类型若非 map[string]string 按实际类型适配。）

- [ ] **Step 4: 跑通过** `go test -race ./internal/pkg/agentruntime/runtimes/claudecode/`；**Step 5: Commit** `✨ claudecode: MCP 注入时追加 MCP_TIMEOUT/MCP_TOOL_TIMEOUT env`

---

## Task 9: orgtool_svc — token + MCP handler + org_get（读路径）

**Files:** Create `internal/service/orgtool_svc/orgtool.go`、`mcp.go`、`deps.go`、`mcp_test.go`

`deps.go`——消费者侧窄接口 + mockgen（镜像 `group_svc/gateway.go`）：

```go
// Package orgtool_svc 组织架构工具(agent 内置工具 key="org")的 MCP 接入与审批编排。
// 业务执行全部委托 department_svc / agent_svc,本包只做 token/开关校验 + 审批挂起。
package orgtool_svc

//go:generate mockgen -source deps.go -destination mock_orgtool_svc/mock_deps.go

// OrgQuery 读组织架构(department_svc.Load 的窄投影)。
type OrgQuery interface {
	Load(ctx context.Context, req *department_svc.LoadOrgRequest) (*department_svc.LoadOrgResponse, error)
}

// DeptCommand 部门写操作(department_svc 的窄投影)。
type DeptCommand interface {
	Create(ctx context.Context, req *department_svc.CreateDepartmentRequest) (*department_svc.CreateDepartmentResponse, error)
	Update(ctx context.Context, req *department_svc.UpdateDepartmentRequest) (*department_svc.UpdateDepartmentResponse, error)
	Move(ctx context.Context, req *department_svc.MoveDepartmentRequest) (*department_svc.MoveDepartmentResponse, error)
	Delete(ctx context.Context, req *department_svc.DeleteDepartmentRequest) (*department_svc.DeleteDepartmentResponse, error)
}

// AgentCommand agent 写操作(agent_svc 的窄投影)。
type AgentCommand interface {
	Create(ctx context.Context, req *agent_svc.CreateAgentRequest) (*agent_svc.CreateAgentResponse, error)
	Update(ctx context.Context, req *agent_svc.UpdateAgentRequest) (*agent_svc.UpdateAgentResponse, error)
	Move(ctx context.Context, req *agent_svc.MoveAgentRequest) (*agent_svc.MoveAgentResponse, error)
	Delete(ctx context.Context, req *agent_svc.DeleteAgentRequest) (*agent_svc.DeleteAgentResponse, error)
}

// AgentLookup 实时校验调用者 agent 的工具开关(agent_repo 的窄投影)。
type AgentLookup interface {
	Find(ctx context.Context, id int64) (*agent_entity.Agent, error)
}

// ApprovalGateway 审批卡登记/决议(chat_svc 的窄投影)。
type ApprovalGateway interface {
	BeginOrgApproval(ctx context.Context, sessionID int64, blk *blocks.OrgApprovalBlock) error
	FinishOrgApproval(ctx context.Context, sessionID int64, requestID, status, result string) error
}
```

> ⚠️ chat_svc 的 `ChatSvc` 接口（chat.go:110 上方）需把 `BeginOrgApproval/FinishOrgApproval` 加进接口声明，否则 orgtool 经 `chat_svc.Chat()` 拿不到——Task 6 实现时一并加。

`orgtool.go`：

```go
type orgtoolSvc struct {
	mcp             *orgMCP
	mcpOnce         sync.Once
	gatewayBaseURL  string
	approvalTimeout time.Duration

	orgQuery     OrgQuery
	deptCommand  DeptCommand
	agentCommand AgentCommand
	agentLookup  AgentLookup
	approval     ApprovalGateway

	waiters sync.Map // requestID(string) → chan bool(buffered 1)
}

var defaultOrgtool = &orgtoolSvc{approvalTimeout: 4 * time.Minute} // spike 实测 CLI 硬顶 ~285s,留 25s 余量

func Default() *orgtoolSvc { return defaultOrgtool }

// RegisterDeps bootstrap 接线(生产传 department_svc.Department()/agent_svc.Agent()/
// agent_repo.Agent()/chat_svc.Chat());测试注 mock。
func (s *orgtoolSvc) RegisterDeps(q OrgQuery, d DeptCommand, a AgentCommand, l AgentLookup, ap ApprovalGateway) {
	s.orgQuery, s.deptCommand, s.agentCommand, s.agentLookup, s.approval = q, d, a, l, ap
}

// mcpHandlerInit 懒初始化 orgMCP(per-process HMAC secret 在首次访问时生成)。
func (s *orgtoolSvc) mcpHandlerInit() *orgMCP {
	s.mcpOnce.Do(func() { s.mcp = newOrgMCP(s) })
	return s.mcp
}

func (s *orgtoolSvc) MCPHandler() http.Handler  { return s.mcpHandlerInit() }
func (s *orgtoolSvc) SetGatewayBaseURL(u string) { s.gatewayBaseURL = u }

// BuildTurnMCP 实现 chat_svc.TurnMCPProvider:agent 开启 org 工具时返回注入 spec。
func (s *orgtoolSvc) BuildTurnMCP(ctx context.Context, a *agent_entity.Agent, sessionID int64) []agentruntime.MCPServerSpec {
	if a == nil || !a.ToolEnabled(agenttool.KeyOrg) || s.gatewayBaseURL == "" {
		return nil
	}
	def, _ := agenttool.Lookup(agenttool.KeyOrg)
	return []agentruntime.MCPServerSpec{{
		Name:    def.Key,
		URL:     s.gatewayBaseURL + def.MCPPath,
		Headers: map[string]string{"Authorization": "Bearer " + s.mcpHandlerInit().MintToken(a.ID, sessionID)},
		Tools:   def.ToolNames,
	}}
}
```

`mcp.go`——token 与 JSON-RPC 骨架逐字镜像 `group_svc/mcp.go`（同样的 randSecret/sign/lookup/bearer/writeRPCResult/writeRPCError，payload 改 `agentID:sessionID`，serverInfo.name=`agentre-org`），`tools/list` 返回 7 个 schema（描述里写清楚参数语义，写工具描述注明「需要用户审批，调用会挂起直至批准/拒绝」），`tools/call` 分发：

```go
	ref, ok := h.lookup(bearer(r)) // ref = orgRef{agentID, sessionID}
	if !ok { http.Error(w, "unauthorized", http.StatusUnauthorized); return }
	// 实时开关校验:用户关掉开关后旧 token 立即失效
	a, err := h.svc.agentLookup.Find(r.Context(), ref.agentID)
	if err != nil || a == nil || !a.ToolEnabled(agenttool.KeyOrg) {
		http.Error(w, "forbidden", http.StatusForbidden); return
	}
	switch rpc.Params.Name {
	case "org_get":
		resp, err := h.svc.orgQuery.Load(r.Context(), &department_svc.LoadOrgRequest{})
		if err != nil { writeRPCError(w, rpc.ID, -32000, err.Error()); return }
		b, _ := json.Marshal(resp) // DTO json tag 已是前端友好命名,直接序列化
		writeRPCResult(w, rpc.ID, map[string]any{"content": []any{map[string]any{"type": "text", "text": string(b)}}})
	default: // 写工具 → Task 10 审批编排
		h.svc.handleWriteTool(w, r, rpc.ID, ref, rpc.Params.Name, rpc.Params.Arguments)
	}
```

（`Arguments` 解析为 `json.RawMessage`，每个写工具各自 Unmarshal 到自己的参数 struct——比 group 的单 struct 清晰，7 个工具入参互不相同。）

- [ ] **Step 1: 写失败测试**（`mcp_test.go`，httptest + mock deps，覆盖：token round-trip/篡改 401、开关关闭 403、initialize/tools-list 帧、org_get 返回 Load 序列化结果）
- [ ] **Step 2: 跑失败** `go test -race ./internal/service/orgtool_svc/`
- [ ] **Step 3: 实现上述骨架 + `make mock` 生成 deps mock**
- [ ] **Step 4: 跑通过**；**Step 5: Commit** `✨ orgtool_svc: org MCP server(token/开关校验/org_get 读路径)`

---

## Task 10: orgtool_svc — 写工具审批挂起 + AnswerOrgApproval + 执行映射

**Files:** Modify `internal/service/orgtool_svc/mcp.go`、`orgtool.go`；Create `types.go`（写工具参数 struct）、`approval_test.go`

写工具参数（`types.go`）：

```go
type createDepartmentArgs struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	ParentID    int64  `json:"parentId"`
}
type updateDepartmentArgs struct { // 零值字段=不变;ParentID 用指针区分"不动"与"挪到顶级"
	ID          int64   `json:"id"`
	Name        string  `json:"name"`
	Description *string `json:"description"`
	LeadAgentID *int64  `json:"leadAgentId"`
	ParentID    *int64  `json:"parentId"`
}
type deleteDepartmentArgs struct {
	ID       int64  `json:"id"`
	Strategy string `json:"strategy"` // reparent(默认)|cascade
}
type createAgentArgs struct {
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	DepartmentID  int64    `json:"departmentId"`
	ParentAgentID int64    `json:"parentAgentId"`
	BackendID     int64    `json:"backendId"` // 0=继承调用者 agent 的 backend
	Prompt        []string `json:"prompt"`
}
type updateAgentArgs struct {
	ID            int64    `json:"id"`
	Name          string   `json:"name"`
	Description   *string  `json:"description"`
	Prompt        []string `json:"prompt"`
	DepartmentID  *int64   `json:"departmentId"`  // 与 ParentAgentID 互斥;非 nil → Move
	ParentAgentID *int64   `json:"parentAgentId"`
}
type deleteAgentArgs struct {
	ID int64 `json:"id"`
}
```

挂起编排（`orgtool.go`）：

```go
// handleWriteTool 写工具统一入口:登记审批 → 挂起等待 → 终态分发。
func (s *orgtoolSvc) handleWriteTool(w http.ResponseWriter, r *http.Request, rpcID json.RawMessage, ref orgRef, tool string, rawArgs json.RawMessage) {
	var input map[string]any
	_ = json.Unmarshal(rawArgs, &input)
	requestID := uuid.NewString() // github.com/google/uuid v1.6.0 已在 go.mod
	blk := &blocks.OrgApprovalBlock{RequestID: requestID, ToolName: tool, ToolInput: input, Status: "pending"}

	ch := make(chan bool, 1)
	s.waiters.Store(requestID, ch)
	defer s.waiters.Delete(requestID)

	if err := s.approval.BeginOrgApproval(r.Context(), ref.sessionID, blk); err != nil {
		writeRPCError(w, rpcID, -32000, "审批通道不可用: "+err.Error())
		return
	}

	select {
	case allow := <-ch:
		if !allow {
			_ = s.approval.FinishOrgApproval(r.Context(), ref.sessionID, requestID, "denied", "")
			writeRPCResult(w, rpcID, textResult("用户拒绝了此操作"))
			return
		}
		result, err := s.execWriteTool(r.Context(), ref, tool, rawArgs)
		if err != nil {
			// 业务校验失败(循环挂载/CEO 不可删等)也算 approved 终态,错误进 Result 给 agent 纠错
			_ = s.approval.FinishOrgApproval(r.Context(), ref.sessionID, requestID, "approved", "执行失败: "+err.Error())
			writeRPCResult(w, rpcID, textResult("已批准但执行失败: "+err.Error()))
			return
		}
		_ = s.approval.FinishOrgApproval(r.Context(), ref.sessionID, requestID, "approved", result)
		writeRPCResult(w, rpcID, textResult(result))
	case <-time.After(s.approvalTimeout):
		_ = s.approval.FinishOrgApproval(r.Context(), ref.sessionID, requestID, "expired", "")
		writeRPCResult(w, rpcID, textResult("审批超时，操作未执行"))
	case <-r.Context().Done():
		_ = s.approval.FinishOrgApproval(context.Background(), ref.sessionID, requestID, "expired", "")
	}
}

// AnswerOrgApprovalRequest 前端审批入口(wails binding)。
type AnswerOrgApprovalRequest struct {
	SessionID int64  `json:"sessionId"`
	RequestID string `json:"requestId"`
	Allow     bool   `json:"allow"`
}
type AnswerOrgApprovalResponse struct{}

// AnswerOrgApproval 唤醒挂起的写工具调用。重复应答/已超时 → InvalidParameter。
func (s *orgtoolSvc) AnswerOrgApproval(ctx context.Context, req *AnswerOrgApprovalRequest) (*AnswerOrgApprovalResponse, error) {
	if req == nil || req.RequestID == "" {
		return nil, i18n.NewError(ctx, code.InvalidParameter)
	}
	chAny, ok := s.waiters.LoadAndDelete(req.RequestID)
	if !ok {
		return nil, i18n.NewError(ctx, code.InvalidParameter)
	}
	chAny.(chan bool) <- req.Allow
	return &AnswerOrgApprovalResponse{}, nil
}
```

`execWriteTool` 映射（同文件；merge 语义:update 先 `orgQuery.Load` 找现值，参数零值/nil 沿用现值；`updateDepartmentArgs.ParentID`/`updateAgentArgs.DepartmentID/ParentAgentID` 非 nil 且变化 → 走对应 `Move`；`createAgentArgs.BackendID==0` → 取 `agentLookup.Find(ref.agentID).AgentBackendID`；执行结果 result 文案形如 `"已创建部门「市场部」(id=7)"`，给 agent 看的动态内容、不进 i18n）。每个分支调用 Task 9 deps 接口，无业务逻辑下沉。

- [ ] **Step 1: 写失败测试**（mock deps + httptest；用例：批准→exec 成功/exec 业务错、拒绝、超时（svc.approvalTimeout 测试里调成 50ms）、AnswerOrgApproval 重复应答报错、update 带 parentId 变化分发 Move、createAgent 缺省 backend 继承调用者）
- [ ] **Step 2: 跑失败**；**Step 3: 实现**；**Step 4: 跑通过 + `make test-backend`**
- [ ] **Step 5: Commit** `✨ orgtool_svc: 写工具服务端审批(同步挂起/批准执行/拒绝/超时)`

---

## Task 11: bootstrap 接线 + wails binding + generate

**Files:** Modify `internal/bootstrap/cago.go`；Create `internal/app/orgtool.go`；跑 `make generate`

- [ ] **Step 1: bootstrap**（cago.go:148-149 group 接线后追加）：

```go
	// 挂组织架构工具 MCP handler(/mcp/org/),并注册 TurnMCPProvider:
	// agent 开了 org 工具的会话 turn 注入该 MCP server(审批在服务端,见 orgtool_svc)。
	orgtool_svc.Default().RegisterDeps(
		department_svc.Department(), department_svc.Department(),
		agent_svc.Agent(), agent_repo.Agent(), chat_svc.Chat(),
	)
	gw.RegisterMCP("/mcp/org/", orgtool_svc.Default().MCPHandler())
	orgtool_svc.Default().SetGatewayBaseURL(gw.BaseURL())
	chat_svc.RegisterTurnMCPProvider(orgtool_svc.Default().BuildTurnMCP)
```

> `department_svc.Department()` 同时满足 OrgQuery 与 DeptCommand 两个窄接口（接口分离，实现同一个）。注意此处在 `chat_svc.RegisterGateway(gw)` 之后、确保 chat_svc 单例已初始化（看 cago.go 既有初始化顺序，放在 group 接线同一区域即可）。

- [ ] **Step 2: binding**（`internal/app/orgtool.go`，镜像 `internal/app/agent.go` 的三行风格）：

```go
package app

import (
	"github.com/agentre-ai/agentre/internal/service/orgtool_svc"
)

// AnswerOrgApproval 组织架构工具写操作的审批决策(批准/拒绝)。
func (a *App) AnswerOrgApproval(req *orgtool_svc.AnswerOrgApprovalRequest) (*orgtool_svc.AnswerOrgApprovalResponse, error) {
	return orgtool_svc.Default().AnswerOrgApproval(a.ctx, req)
}
```

- [ ] **Step 3:** `make generate`（刷新 wailsjs bindings：AnswerOrgApproval + AgentToolDTO/tools/availableTools 模型）；`go build ./...` + `make test-backend` 全绿
- [ ] **Step 4: Commit** `🔌 bootstrap/app: orgtool MCP 挂载 + TurnMCPProvider + AnswerOrgApproval binding`

---

## Task 12: 前端 — 组织架构页「工具」开关区块

**Files:** Modify `frontend/src/components/agentre/org/org-detail-agent.tsx`、`frontend/src/components/agentre/org/types.ts`(OrgAgent 加 `tools`)、`frontend/src/components/agentre/org/use-org-data.ts`(LoadOrg 透传 `availableTools`)、`frontend/src/i18n/locales/{zh-CN,en}/common.json`；Test 跟随 org 既有测试位置（搜 `org-detail-agent` 的 test 文件；无则新建 `org/__tests__/org-detail-agent.test.tsx`）

- [ ] **Step 1: 写失败测试**——用例：① 渲染 availableTools=["org"]、agent.tools=[] 时显示「组织架构」开关且为关闭态；② 点击切换后保存，`onUpdate` 收到 `tools:[{key:"org",enabled:true}]`；③ i18n key 进 `i18n.test.ts` 覆盖（zh-CN/en 同步加 key 后该测试自然约束）。

- [ ] **Step 2: 跑失败** `cd frontend && pnpm test -- org-detail-agent`

- [ ] **Step 3: 实现**

state（镜像 skills，org-detail-agent.tsx:97 旁）：

```tsx
const [tools, setTools] = React.useState<department_svc.AgentToolDTO[]>(() => {
  const cur = new Map((props.agent.tools ?? []).map((t) => [t.key, t.enabled]));
  // 以可用工具清单为骨架,未配置过的 key 默认关闭
  return (props.availableTools ?? []).map((key) => ({ key, enabled: cur.get(key) ?? false }));
});
const toggleTool = (key: string) =>
  setTools((prev) => prev.map((t) => (t.key === key ? { ...t, enabled: !t.enabled } : t)));
```

`handleSave` 的 `UpdateAgentRequest.createFrom` payload 中 `skills,` 后加 `tools,`。

JSX——Skills 区块（396-438 行）之后加同构区块，开关按钮样式与 skills badge 完全一致（`role="switch"` + 同 className），文案：

```tsx
<section className="space-y-2.5" data-slot="agent-section-tools">
  <div className="flex items-center justify-between">
    <h3 className="font-mono text-2xs font-semibold uppercase tracking-wide text-muted-foreground">
      {t("org.agent.tools.title")}
    </h3>
  </div>
  {tools.length === 0 ? (
    <p className="text-2xs text-muted-foreground">{t("org.agent.tools.empty")}</p>
  ) : (
    <div className="flex flex-wrap gap-1.5" role="group" aria-label={t("org.agent.tools.list")}>
      {tools.map((tl) => (
        <button key={tl.key} type="button" role="switch" aria-checked={tl.enabled}
          aria-label={t(`org.agent.tools.names.${tl.key}`)}
          title={t(`org.agent.tools.descriptions.${tl.key}`)}
          onClick={() => toggleTool(tl.key)}
          className={cn(/* 同 skills badge 的两态 className,原样复制 */)}>
          {t(`org.agent.tools.names.${tl.key}`)}
        </button>
      ))}
    </div>
  )}
</section>
```

`availableTools` 从 `LoadOrg()` 响应取（use-org-data.ts setState 加 `availableTools: res.availableTools ?? []`），经 org 页容器 props 传入 OrgDetailAgent。

i18n（两语言同步）：

```json
"org.agent.tools": {
  "title": "工具 / TOOLS",
  "empty": "暂无可用工具",
  "list": "工具开关",
  "names": { "org": "组织架构" },
  "descriptions": { "org": "允许该 Agent 查询并管理部门与成员（写操作需你审批）" }
}
```

（en 对应翻译；key 实际格式跟随该文件既有嵌套风格——zh-CN common.json 的 org 段是嵌套对象还是扁平 key，照抄现状。）

- [ ] **Step 4: 跑通过** `pnpm test -- org-detail-agent && pnpm test -- i18n`；**Step 5: Commit** `✨ org 页: agent 工具开关区块(org 组织架构工具)`

---

## Task 13: 前端 — 审批卡（流事件 + store + transcript 行 + 卡片）

**Files:** Modify `frontend/src/stores/chat-streams-store.ts`、`frontend/src/components/agentre/chat-streams-host.tsx`、`frontend/src/components/agentre/transcript-rows.ts`、`frontend/src/components/agentre/transcript-row-view.tsx`、`frontend/src/hooks/use-chat-stream.ts`(kind union 加 `"org_approval"`)、i18n 两份；Create `frontend/src/components/agentre/org-approval/card.tsx` + 测试（store 测试进 `stores/__tests__/chat-streams-store.test.ts`，行为测试进 `components/agentre/__tests__/`）

数据形态约定（后端 Task 6 的 payload / Task 5 的 ChatBlock）：
- 流事件：`{kind:"org_approval", requestId, toolName, toolInput, status, result}`，status=pending 为新卡、其余为决议更新。
- 持久化块：`block.type === "org_approval"`，`block.orgApproval = {requestId, toolName, toolInput, status, result}`。

- [ ] **Step 1: 写失败测试**

store（镜像 `appendLiveToolPermissionRequest/markToolPermissionResolved` 的既有用例，chat-streams-store.test.ts 搜 tool_permission）：append 建块、mark 按 requestId 更新 status/result、未知 requestId no-op。

卡片：① pending 渲染工具名文案 + 入参 JSON + 批准/拒绝按钮；② 点批准调 `AnswerOrgApproval({sessionId, requestId, allow:true})`（mock wailsjs）；③ status=approved/denied/expired 渲染只读徽标 + result 文本、无按钮。

transcript-rows：blocks 含 org_approval 时产出对应 row item（镜像 tool_permission_request case 的用例）。

- [ ] **Step 2: 跑失败** `pnpm test -- chat-streams-store org-approval transcript-rows`

- [ ] **Step 3: 实现**

store——镜像 chat-streams-store.ts:427/447 两函数：

```ts
appendLiveOrgApproval: (sessionId, payload) =>
  // 与 appendLiveToolPermissionRequest 同构:push 一个 {type:"org_approval", orgApproval: payload} live block
markOrgApprovalResolved: (sessionId, payload) =>
  // 与 markToolPermissionResolved 同构:按 orgApproval.requestId 找块,覆盖 status/result
```

chat-streams-host.tsx kind 分发（tool_permission_request case 旁）：

```ts
case "org_approval": {
  const payload = { requestId: ev.requestId, toolName: ev.toolName, toolInput: ev.toolInput, status: ev.status, result: ev.result };
  if (ev.status === "pending") appendLiveOrgApproval(sessionId, payload);
  else markOrgApprovalResolved(sessionId, payload);
  break;
}
```

use-chat-stream.ts kind union 加 `| "org_approval"`，事件 payload 类型补 `orgApproval` 相关字段（跟随该文件对 toolPermission 字段的声明方式）。

transcript-rows.ts（tool_permission_request case 旁）：

```ts
case "org_approval": {
  items.push({ block: b, type: "org_approval" });
  break;
}
```

RenderItem union 加 `{ block: ChatBlockData; type: "org_approval" }`；transcript-row-view.tsx 对应 case 渲染 `<OrgApprovalCard block={...} sessionId={...} />`。

卡片 `org-approval/card.tsx`——视觉对齐 `canonical-tool/tool-permission/card.tsx`（同样的图标行+参数 pre 块+按钮排布，可直接参考其 className），逻辑：

```tsx
export const OrgApprovalCard: React.FC<{ approval: OrgApprovalData; sessionId: number }> = ({ approval, sessionId }) => {
  const { t } = useTranslation();
  const [submitting, setSubmitting] = React.useState(false);
  const answer = async (allow: boolean) => {
    setSubmitting(true);
    try {
      await AnswerOrgApproval(orgtool_svc.AnswerOrgApprovalRequest.createFrom({ sessionId, requestId: approval.requestId, allow }));
    } catch {
      toast.error(t("orgApproval.submitFailed")); // toast 用法跟随 tool-permission 卡现状
    } finally {
      setSubmitting(false);
    }
  };
  // 标题: t(`orgApproval.tools.${approval.toolName}`, { defaultValue: approval.toolName })
  // pending: 入参 <pre>{JSON.stringify(approval.toolInput, null, 2)}</pre> + 批准/拒绝按钮(disabled=submitting)
  // approved/denied/expired: 徽标 t(`orgApproval.status.${approval.status}`) + approval.result 文本(动态内容,原样展示)
};
```

i18n（zh-CN 示意，en 同步）：

```json
"orgApproval": {
  "title": "组织架构操作审批",
  "approve": "批准",
  "deny": "拒绝",
  "submitFailed": "审批提交失败",
  "status": { "approved": "已批准", "denied": "已拒绝", "expired": "已过期" },
  "tools": {
    "org_create_department": "创建部门",
    "org_update_department": "修改部门",
    "org_delete_department": "删除部门",
    "org_create_agent": "创建 Agent",
    "org_update_agent": "修改 Agent",
    "org_delete_agent": "删除 Agent"
  }
}
```

ChatBlockData 类型：在前端 block 类型定义处（transcript-rows.ts 引用的 ChatBlockData 来源文件）加 `orgApproval?: { requestId: string; toolName: string; toolInput?: Record<string, unknown>; status: string; result?: string }`。

**Expired 渲染规则（spec §5.2）：** 卡片只信 `status` 字段——后端 finalize 已把悬空 pending 落成 expired；LoadSession overlay 的 pending 是真 pending（进程内仍可决）。前端无需按「会话是否活跃」自行推断。

- [ ] **Step 4: 跑通过** `pnpm test`（前端全量）；**Step 5: Commit** `✨ chat: org 审批卡(流事件/store/transcript 行/批准拒绝)`

---

## Task 14: 收尾 — 全量回归 + 真机手动验证

- [ ] **Step 1:** `make check`（lint + 后端 + 前端测试）全绿；不绿先修。
- [ ] **Step 2: 手动验证脚本**（`make dev`）：
  1. 组织架构页 → CEO 编辑弹窗：「工具/组织架构」默认开启（migration 生效）；随便开一个普通 agent 的 org 工具并保存。
  2. 与该 agent 新建单聊（新 spawn 才有 mcp-config）→ 发「查一下当前组织架构」→ agent 调 `org_get` 直接返回部门树。
  3. 发「创建一个市场部」→ 会话里出现审批卡（pending）→ 批准 → 工具返回成功、组织架构页出现市场部。
  4. 再发一个删除请求 → 拒绝 → agent 收到「用户拒绝」并继续对话。
  5. 挂起期间切走再切回会话 → 审批卡仍可见（LoadSession overlay）。
  6. 关掉该 agent 的 org 工具 → 同一会话再调用 → 工具 403 报错（实时开关校验）。
  7. 群聊拉该 agent 发起一轮，让它查组织架构 → backing session 内工具可用。
- [ ] **Step 3:** 把手动验证结果记录到 PR 描述；未通过项回到对应 Task 修复。
- [ ] **Step 4: Commit**（如有修复）+ 按 `superpowers:finishing-a-development-branch` 走分支收尾。

---

## Self-Review 检查记录

- **Spec 覆盖**：§3 数据模型→Task 1/2/4；§3.2 注册表→Task 3；§4 MCP server→Task 9/10；§5 审批流→Task 5/6/10/13；§5.4 spike→Task 0/8；§6 注入门控→Task 7（单点化，行为等价）；§7 前端→Task 12/13；§8 测试→各 Task TDD 步骤。spec §4.2「org_get 附可用 backend 列表」的实现为：agents 列表内含 backend 摘要 + create_agent 缺省继承调用者 backend，未单列全量 backend 清单（避免额外依赖；如 agent 需要指定其他 backend，用现有 agent 的 backendId）。
- **类型一致性**：`AgentToolItem/AgentToolDTO`、`OrgApprovalBlock(status: pending|approved|denied|expired)`、流事件 kind `org_approval`、binding `AnswerOrgApproval` 全文一致。
- **已知开放点**：Task 0 spike 可能改写 `approvalTimeout` 与 Task 8 的去留——执行者必须先做 Task 0。
