# Part B PR3 · 流程管理 Agent 工具(workflowtool_svc)实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.
>
> **依赖:** 本 PR 建立在 **PR2(通用 tool_approval)已合并** 之上 —— 用 `chat_svc.BeginToolApproval(ctx, sessionID, *blocks.ToolApprovalBlock) (<-chan bool, error)` + `FinishToolApproval`。若 PR2 未落，先做 PR2。

**Goal:** 新增 `internal/service/workflowtool_svc/`,以内嵌 MCP-over-HTTP server 形态给被授予 `workflow` 工具的 agent 暴露 `workflow_list`(读)/`workflow_create`/`workflow_update`/`workflow_delete`(写,走用户审批),执行落已有的 `workflow_svc` CRUD。镜像 `orgtool_svc`。

**Architecture:** 与 orgtool 同构:`agenttool.KeyWorkflow` 注册表项 + `/mcp/workflow/` 挂载;per-agent `agent_entity.ToolEnabled("workflow")` 门控 + HMAC token;写工具走 PR2 的通用审批网关(`ToolKey="workflow"`);业务委托 `workflow_svc`(窄接口 `WorkflowQuery`/`WorkflowCommand`,DIP);bootstrap 接线 + `TurnMCPProvider`。

**Tech Stack:** Go 1.26 (cago)、mockgen、内嵌 MCP-over-HTTP(参照 orgtool_svc)。

**参考实现:** `internal/service/orgtool_svc/`(逐文件镜像);**业务依赖:** `internal/service/workflow_svc/`(已存在的 CRUD)。

---

## 背景与既有事实(实现者必读)

### workflow_svc(已存在,直接复用,不改)
`internal/service/workflow_svc/workflow.go` + `types.go`:
```go
type WorkflowSvc interface {
    List(ctx, *ListWorkflowsRequest) (*ListWorkflowsResponse, error)     // resp.Items []WorkflowItem
    Create(ctx, *CreateWorkflowRequest) (*CreateWorkflowResponse, error) // req {Name, Content}; resp.Item
    Update(ctx, *UpdateWorkflowRequest) (*UpdateWorkflowResponse, error) // req {ID, Name, Content}; resp.Item
    Delete(ctx, *DeleteWorkflowRequest) (*DeleteWorkflowResponse, error) // req {ID}
}
func Workflow() WorkflowSvc { return defaultWorkflow }
// WorkflowItem { ID int64; Name, Content string; GroupCount int; Createtime, Updatetime int64 } (json: id/name/content/groupCount/...)
```
`List` 的 item 已带 `GroupCount`(使用中群数)。`Update` 需要 name+content(无 Get;合并现值靠先 List 找 id)。

### orgtool_svc(逐文件镜像的源)
完整源在 `internal/service/orgtool_svc/{orgtool.go,mcp.go,approval.go,deps.go,types.go}` + `mock_orgtool_svc/`。关键可复制骨架:
- **token/HMAC/RPC helpers**(`mcp.go` 的 `MintToken/sign/lookup/ServeHTTP/bearer/writeRPCResult/writeRPCError/randSecret`)—— 与业务无关,**逐字复制**,把 `org`/`orgMCP`/`orgRef`/`KeyOrg` → `workflow`/`workflowMCP`/`workflowRef`/`KeyWorkflow`,server name `agentre-org`→`agentre-workflow`。
- **singleton + BuildTurnMCP**(`orgtool.go`)—— 复制,deps 换成 workflow 的。
- **handleWriteTool 审批骨架**(`approval.go`)—— 复制 PR2 改造后的版本(用 `BeginToolApproval` 返回的 channel),`ToolKey` 用 `agenttool.KeyWorkflow`。

### agenttool 注册表
`internal/pkg/agenttool/agenttool.go` 现仅 `KeyOrg`。`Keys()` 喂前端 `availableTools`,加 `KeyWorkflow` 后能力 picker 自动出现(前端在 PR4 配文案/审批徽标)。

### PR2 后的审批网关(本 PR 的消费契约)
```go
// chat_svc(PR2 后):
BeginToolApproval(ctx, sessionID int64, blk *blocks.ToolApprovalBlock) (<-chan bool, error)
FinishToolApproval(ctx, sessionID int64, requestID, status, result string) error
// blocks.ToolApprovalBlock { ToolKey, RequestID, ToolName, ToolInput, Status, Result }
```

### 测试/构建
- `make test-backend`;聚焦 `go test -race ./internal/service/workflowtool_svc/...`。`make mock` 生成 deps mock。svc 测试注 mock,不连库。
- 改 agenttool/bootstrap 后 `go build ./...`。
- 远程执行(agentred)下与 org 工具同款限制(已知,不在本 PR 处理)。

### 文件结构(新增)
- `internal/pkg/agenttool/agenttool.go`(改:加 KeyWorkflow)
- `internal/service/workflowtool_svc/workflowtool.go`(singleton/RegisterDeps/MCPHandler/BuildTurnMCP/SetGatewayBaseURL)
- `internal/service/workflowtool_svc/mcp.go`(workflowMCP:token + ServeHTTP + schemas + workflow_list 读路径)
- `internal/service/workflowtool_svc/approval.go`(handleWriteTool + execWriteTool + create/update/delete)
- `internal/service/workflowtool_svc/deps.go`(WorkflowQuery/WorkflowCommand/AgentLookup/ApprovalGateway)
- `internal/service/workflowtool_svc/types.go`(arg structs)
- `internal/service/workflowtool_svc/mock_workflowtool_svc/`(mockgen)
- `internal/service/workflowtool_svc/{mcp_test.go,approval_test.go}`
- bootstrap `internal/bootstrap/cago.go`(挂 /mcp/workflow/ + provider)、`internal/app/app.go`(RegisterDeps)

---

## Task 1: agenttool 注册 KeyWorkflow

**Files:**
- Modify: `internal/pkg/agenttool/agenttool.go`
- Test: `internal/pkg/agenttool/agenttool_test.go`(若存在则补;否则建)

- [ ] **Step 1: 写/补测试**
```go
func TestRegistry_HasWorkflow(t *testing.T) {
    d, ok := agenttool.Lookup(agenttool.KeyWorkflow)
    if !ok { t.Fatal("workflow not registered") }
    if d.MCPPath != "/mcp/workflow/" { t.Fatalf("path=%s", d.MCPPath) }
    want := []string{"workflow_list", "workflow_create", "workflow_update", "workflow_delete"}
    if !slices.Equal(d.ToolNames, want) { t.Fatalf("tools=%v", d.ToolNames) }
    keys := agenttool.Keys()
    if !slices.Contains(keys, "workflow") || !slices.Contains(keys, "org") { t.Fatalf("keys=%v", keys) }
}
```

- [ ] **Step 2: 跑红** `go test ./internal/pkg/agenttool/...`

- [ ] **Step 3: 实现**
```go
// KeyWorkflow 协作流程(SOP)读写工具。
const KeyWorkflow = "workflow"

var registry = []Definition{
    { Key: KeyOrg, MCPPath: "/mcp/org/", ToolNames: []string{
        "org_get",
        "org_create_department", "org_update_department", "org_delete_department",
        "org_create_agent", "org_update_agent", "org_delete_agent",
    }},
    { Key: KeyWorkflow, MCPPath: "/mcp/workflow/", ToolNames: []string{
        "workflow_list", "workflow_create", "workflow_update", "workflow_delete",
    }},
}
```

- [ ] **Step 4: 跑绿** `go test ./internal/pkg/agenttool/...`

- [ ] **Step 5: 提交**
```bash
git add internal/pkg/agenttool/
git commit -m "✨ agenttool: 注册 KeyWorkflow(/mcp/workflow/ + 4 工具)"
```

---

## Task 2: workflowtool_svc deps + types

**Files:**
- Create: `internal/service/workflowtool_svc/deps.go`、`types.go`

- [ ] **Step 1: deps.go**
```go
// Package workflowtool_svc 流程管理工具(agent 内置工具 key="workflow")的 MCP 接入与审批编排。
// 业务执行委托 workflow_svc,本包只做 token/开关校验 + 审批挂起。
package workflowtool_svc

import (
    "context"

    "github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
    "github.com/agentre-ai/agentre/internal/service/chat_svc/blocks"
    "github.com/agentre-ai/agentre/internal/service/workflow_svc"
)

//go:generate mockgen -source deps.go -destination mock_workflowtool_svc/mock_deps.go

// WorkflowQuery 读流程(workflow_svc.List 的窄投影)。
type WorkflowQuery interface {
    List(ctx context.Context, req *workflow_svc.ListWorkflowsRequest) (*workflow_svc.ListWorkflowsResponse, error)
}

// WorkflowCommand 流程写操作(workflow_svc 的窄投影)。
type WorkflowCommand interface {
    Create(ctx context.Context, req *workflow_svc.CreateWorkflowRequest) (*workflow_svc.CreateWorkflowResponse, error)
    Update(ctx context.Context, req *workflow_svc.UpdateWorkflowRequest) (*workflow_svc.UpdateWorkflowResponse, error)
    Delete(ctx context.Context, req *workflow_svc.DeleteWorkflowRequest) (*workflow_svc.DeleteWorkflowResponse, error)
}

// AgentLookup 实时校验调用者 agent 的工具开关(agent_repo 的窄投影)。
type AgentLookup interface {
    Find(ctx context.Context, id int64) (*agent_entity.Agent, error)
}

// ApprovalGateway 通用工具审批(chat_svc 的窄投影,PR2 后)。
type ApprovalGateway interface {
    BeginToolApproval(ctx context.Context, sessionID int64, blk *blocks.ToolApprovalBlock) (<-chan bool, error)
    FinishToolApproval(ctx context.Context, sessionID int64, requestID, status, result string) error
}
```

- [ ] **Step 2: types.go**
```go
package workflowtool_svc

// 写工具参数 struct。update 用指针区分"不传(沿用现值)"与"显式改"。
type createWorkflowArgs struct {
    Name    string `json:"name"`
    Content string `json:"content"`
}

type updateWorkflowArgs struct {
    ID      int64   `json:"id"`
    Name    *string `json:"name"`    // nil=不变
    Content *string `json:"content"` // nil=不变
}

type deleteWorkflowArgs struct {
    ID int64 `json:"id"`
}
```

- [ ] **Step 3: 生成 mock**
```bash
cd /Users/codfrm/Code/agentre/agentre && go generate ./internal/service/workflowtool_svc/...
```
(暂不全编译——orgtool.go/mcp.go/approval.go 还没建;deps/types 包内自洽即可。)

- [ ] **Step 4: 提交**(与 Task 3-4 一起最终提交,见 Task 4)。

---

## Task 3: workflowtool.go(singleton/BuildTurnMCP) + mcp.go(token+ServeHTTP+schemas)

**Files:**
- Create: `internal/service/workflowtool_svc/workflowtool.go`、`mcp.go`

- [ ] **Step 1: workflowtool.go(镜像 orgtool.go)**
```go
package workflowtool_svc

import (
    "context"
    "net/http"
    "sync"
    "time"

    "github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
    "github.com/agentre-ai/agentre/internal/pkg/agentruntime"
    "github.com/agentre-ai/agentre/internal/pkg/agenttool"
)

type workflowtoolSvc struct {
    mcp             *workflowMCP
    mcpOnce         sync.Once
    gatewayBaseURL  string
    approvalTimeout time.Duration

    query    WorkflowQuery
    command  WorkflowCommand
    lookup   AgentLookup
    approval ApprovalGateway
}

var defaultWorkflowtool = &workflowtoolSvc{approvalTimeout: 4 * time.Minute}

func Default() *workflowtoolSvc { return defaultWorkflowtool }

func (s *workflowtoolSvc) RegisterDeps(q WorkflowQuery, c WorkflowCommand, l AgentLookup, ap ApprovalGateway) {
    s.query, s.command, s.lookup, s.approval = q, c, l, ap
}

func (s *workflowtoolSvc) mcpHandlerInit() *workflowMCP {
    s.mcpOnce.Do(func() { s.mcp = newWorkflowMCP(s) })
    return s.mcp
}

func (s *workflowtoolSvc) MCPHandler() http.Handler { return s.mcpHandlerInit() }
func (s *workflowtoolSvc) SetGatewayBaseURL(u string) { s.gatewayBaseURL = u }

func (s *workflowtoolSvc) BuildTurnMCP(_ context.Context, a *agent_entity.Agent, sessionID int64, _ int64) []agentruntime.MCPServerSpec {
    if a == nil || !a.ToolEnabled(agenttool.KeyWorkflow) || s.gatewayBaseURL == "" {
        return nil
    }
    def, ok := agenttool.Lookup(agenttool.KeyWorkflow)
    if !ok {
        return nil
    }
    return []agentruntime.MCPServerSpec{{
        Name:    def.Key,
        URL:     s.gatewayBaseURL + def.MCPPath,
        Headers: map[string]string{"Authorization": "Bearer " + s.mcpHandlerInit().MintToken(a.ID, sessionID)},
        Tools:   def.ToolNames,
    }}
}
```

- [ ] **Step 2: mcp.go —— 复制 orgtool_svc/mcp.go 的 token/RPC 骨架,替换业务路由**

逐字复制 `orgtool_svc/mcp.go` 的:`workflowRef`(=orgRef)、`workflowMCP`(=orgMCP) 结构、`newWorkflowMCP`、`MintToken`、`sign`、`lookup`、`bearer`、`writeRPCResult`、`writeRPCError`、`randSecret`(替换标识符 org→workflow,server name `agentre-org`→`agentre-workflow`,panic 文案 `orgtool_svc`→`workflowtool_svc`)。

`ServeHTTP` 的 `tools/call` 分支换成 workflow 业务:
```go
case "tools/call":
    ref, ok := h.lookup(bearer(r))
    if !ok { http.Error(w, "unauthorized", http.StatusUnauthorized); return }
    if h.svc.lookup == nil { http.Error(w, "service unavailable", http.StatusServiceUnavailable); return }
    a, err := h.svc.lookup.Find(r.Context(), ref.agentID)
    if err != nil || a == nil || !a.ToolEnabled(agenttool.KeyWorkflow) {
        http.Error(w, "forbidden", http.StatusForbidden); return
    }
    switch rpc.Params.Name {
    case "workflow_list":
        resp, err := h.svc.query.List(r.Context(), &workflow_svc.ListWorkflowsRequest{})
        if err != nil { writeRPCError(w, rpc.ID, -32000, err.Error()); return }
        b, _ := json.Marshal(workflowListView(resp))
        writeRPCResult(w, rpc.ID, map[string]any{"content": []any{map[string]any{"type": "text", "text": string(b)}}})
    default:
        if !isWorkflowWriteTool(rpc.Params.Name) { writeRPCError(w, rpc.ID, -32601, "unknown tool"); return }
        h.svc.handleWriteTool(w, r, rpc.ID, ref, rpc.Params.Name, rpc.Params.Arguments)
    }
```
`initialize` 的 serverInfo name 用 `"agentre-workflow"`;`tools/list` → `writeRPCResult(w, rpc.ID, map[string]any{"tools": workflowToolSchemas()})`。

`isWorkflowWriteTool`:
```go
func isWorkflowWriteTool(name string) bool {
    def, ok := agenttool.Lookup(agenttool.KeyWorkflow)
    if !ok { return false }
    return name != "workflow_list" && slices.Contains(def.ToolNames, name)
}
```

`workflowListView`(LLM 投影,去掉对 LLM 无用字段——这里正文有用,保留;时间戳转可读可选,保留原始即可):
```go
type workflowListItemView struct {
    ID         int64  `json:"id"`
    Name       string `json:"name"`
    GroupCount int    `json:"groupCount"`
    Content    string `json:"content"`
}
func workflowListView(resp *workflow_svc.ListWorkflowsResponse) any {
    items := make([]workflowListItemView, 0, len(resp.Items))
    for _, it := range resp.Items {
        items = append(items, workflowListItemView{ID: it.ID, Name: it.Name, GroupCount: it.GroupCount, Content: it.Content})
    }
    return map[string]any{"workflows": items}
}
```
> 注:list 直接带 Content。spec 开放项2 提到若 token 压力再拆 `workflow_get`;本期不拆。

`workflowToolSchemas`:
```go
func workflowToolSchemas() []any {
    const approvalNote = "（需要用户审批,调用会挂起直至批准/拒绝/超时）"
    return []any{
        map[string]any{
            "name": "workflow_list",
            "description": "列出全部协作流程(SOP):id、名称、使用中群数、正文。无参数。",
            "inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
        },
        map[string]any{
            "name": "workflow_create", "description": "新建协作流程" + approvalNote,
            "inputSchema": map[string]any{"type": "object", "required": []string{"name"}, "properties": map[string]any{
                "name":    map[string]any{"type": "string", "description": "流程名称(必填)"},
                "content": map[string]any{"type": "string", "description": "流程正文(Markdown:角色/步骤/交付物/验收)"},
            }},
        },
        map[string]any{
            "name": "workflow_update", "description": "更新协作流程(只传要改的字段)" + approvalNote,
            "inputSchema": map[string]any{"type": "object", "required": []string{"id"}, "properties": map[string]any{
                "id":      map[string]any{"type": "integer", "description": "流程 id(必填)"},
                "name":    map[string]any{"type": "string", "description": "新名称"},
                "content": map[string]any{"type": "string", "description": "新正文(Markdown)"},
            }},
        },
        map[string]any{
            "name": "workflow_delete", "description": "删除协作流程;绑定它的群将按「不绑定流程」处理" + approvalNote,
            "inputSchema": map[string]any{"type": "object", "required": []string{"id"}, "properties": map[string]any{
                "id": map[string]any{"type": "integer", "description": "流程 id(必填)"},
            }},
        },
    }
}
```

- [ ] **Step 3: 提交**(与 Task 4 一起)。

---

## Task 4: approval.go(handleWriteTool + execWriteTool) + 包内编译绿

**Files:**
- Create: `internal/service/workflowtool_svc/approval.go`

- [ ] **Step 1: approval.go**
```go
package workflowtool_svc

import (
    "context"
    "encoding/json"
    "fmt"
    "net/http"
    "time"

    "github.com/google/uuid"

    "github.com/agentre-ai/agentre/internal/pkg/agenttool"
    "github.com/agentre-ai/agentre/internal/service/chat_svc/blocks"
    "github.com/agentre-ai/agentre/internal/service/workflow_svc"
)

// handleWriteTool 写工具统一入口:登记审批 → 挂起等待 → 终态分发(用 PR2 通用网关)。
func (s *workflowtoolSvc) handleWriteTool(w http.ResponseWriter, r *http.Request, rpcID json.RawMessage, ref workflowRef, tool string, rawArgs json.RawMessage) {
    var input map[string]any
    _ = json.Unmarshal(rawArgs, &input)
    requestID := uuid.NewString()
    blk := &blocks.ToolApprovalBlock{ToolKey: agenttool.KeyWorkflow, RequestID: requestID, ToolName: tool, ToolInput: input, Status: "pending"}

    ch, err := s.approval.BeginToolApproval(r.Context(), ref.sessionID, blk)
    if err != nil {
        writeRPCError(w, rpcID, -32000, "审批通道不可用: "+err.Error())
        return
    }
    select {
    case allow := <-ch:
        if !allow {
            _ = s.approval.FinishToolApproval(r.Context(), ref.sessionID, requestID, "denied", "")
            writeRPCResult(w, rpcID, textResult("用户拒绝了此操作"))
            return
        }
        result, execErr := s.execWriteTool(r.Context(), tool, rawArgs)
        if execErr != nil {
            _ = s.approval.FinishToolApproval(r.Context(), ref.sessionID, requestID, "approved", "执行失败: "+execErr.Error())
            writeRPCResult(w, rpcID, textResult("已批准但执行失败: "+execErr.Error()))
            return
        }
        _ = s.approval.FinishToolApproval(r.Context(), ref.sessionID, requestID, "approved", result)
        writeRPCResult(w, rpcID, textResult(result))
    case <-time.After(s.approvalTimeout):
        _ = s.approval.FinishToolApproval(r.Context(), ref.sessionID, requestID, "expired", "")
        writeRPCResult(w, rpcID, textResult("审批超时，操作未执行"))
    case <-r.Context().Done():
        _ = s.approval.FinishToolApproval(context.Background(), ref.sessionID, requestID, "expired", "")
    }
}

func textResult(text string) map[string]any {
    return map[string]any{"content": []any{map[string]any{"type": "text", "text": text}}}
}

func (s *workflowtoolSvc) execWriteTool(ctx context.Context, tool string, rawArgs json.RawMessage) (string, error) {
    switch tool {
    case "workflow_create":
        return s.createWorkflow(ctx, rawArgs)
    case "workflow_update":
        return s.updateWorkflow(ctx, rawArgs)
    case "workflow_delete":
        return s.deleteWorkflow(ctx, rawArgs)
    default:
        return "", fmt.Errorf("未知写工具: %s", tool)
    }
}

func (s *workflowtoolSvc) createWorkflow(ctx context.Context, rawArgs json.RawMessage) (string, error) {
    var args createWorkflowArgs
    if err := json.Unmarshal(rawArgs, &args); err != nil {
        return "", err
    }
    resp, err := s.command.Create(ctx, &workflow_svc.CreateWorkflowRequest{Name: args.Name, Content: args.Content})
    if err != nil {
        return "", err
    }
    return fmt.Sprintf("已创建流程「%s」(id=%d)", resp.Item.Name, resp.Item.ID), nil
}

func (s *workflowtoolSvc) updateWorkflow(ctx context.Context, rawArgs json.RawMessage) (string, error) {
    var args updateWorkflowArgs
    if err := json.Unmarshal(rawArgs, &args); err != nil {
        return "", err
    }
    cur, err := s.loadWorkflow(ctx, args.ID)
    if err != nil {
        return "", err
    }
    name := cur.Name
    if args.Name != nil {
        name = *args.Name
    }
    content := cur.Content
    if args.Content != nil {
        content = *args.Content
    }
    resp, err := s.command.Update(ctx, &workflow_svc.UpdateWorkflowRequest{ID: args.ID, Name: name, Content: content})
    if err != nil {
        return "", err
    }
    return fmt.Sprintf("已更新流程「%s」(id=%d)", resp.Item.Name, resp.Item.ID), nil
}

func (s *workflowtoolSvc) deleteWorkflow(ctx context.Context, rawArgs json.RawMessage) (string, error) {
    var args deleteWorkflowArgs
    if err := json.Unmarshal(rawArgs, &args); err != nil {
        return "", err
    }
    cur, err := s.loadWorkflow(ctx, args.ID)
    if err != nil {
        return "", err
    }
    if _, err := s.command.Delete(ctx, &workflow_svc.DeleteWorkflowRequest{ID: args.ID}); err != nil {
        return "", err
    }
    return fmt.Sprintf("已删除流程「%s」(id=%d,原被 %d 个群使用)", cur.Name, args.ID, cur.GroupCount), nil
}

// loadWorkflow 从 List 里按 id 找现值(update merge / delete 文案需要)。
func (s *workflowtoolSvc) loadWorkflow(ctx context.Context, id int64) (*workflow_svc.WorkflowItem, error) {
    resp, err := s.query.List(ctx, &workflow_svc.ListWorkflowsRequest{})
    if err != nil {
        return nil, err
    }
    for i := range resp.Items {
        if resp.Items[i].ID == id {
            return &resp.Items[i], nil
        }
    }
    return nil, fmt.Errorf("找不到流程(id=%d)", id)
}
```
> 注:`workflow_svc.ListWorkflowsResponse.Items` 是 `[]WorkflowItem`(值切片)还是 `[]*WorkflowItem`,以 `types.go` 实际为准,`loadWorkflow` 取址相应调整。

- [ ] **Step 2: 包内编译 + vet**
```bash
go build ./internal/service/workflowtool_svc/...
go vet ./internal/service/workflowtool_svc/...
```
Expected: 通过。

- [ ] **Step 3: 提交 Task 2+3+4**
```bash
git add internal/service/workflowtool_svc/
git commit -m "✨ workflowtool: MCP server + 审批写工具(create/update/delete)+ workflow_list(镜像 orgtool,ToolKey=workflow)"
```

---

## Task 5: 单元测试(mcp_test + approval_test,镜像 orgtool)

**Files:**
- Create: `internal/service/workflowtool_svc/mcp_test.go`、`approval_test.go`

- [ ] **Step 1: mcp_test.go(镜像 orgtool mcp_test)**

覆盖:`TestWorkflowMCP_TokenRoundTrip`(MintToken→lookup 还原 agent/session;改一字节验签失败);`TestWorkflowMCP_SwitchOffForbids`(ToolEnabled=false → tools/call 403,用 MockAgentLookup 返回关开关的 agent);`TestWorkflowMCP_DepsNotRegistered`(lookup==nil → 503);`TestWorkflowMCP_InitializeAndToolsList`(tools/list 返回 4 个 schema,名字齐);`TestWorkflowMCP_ListReturnsProjection`(workflow_list → MockWorkflowQuery.List 返回 2 条 → result JSON 含 workflows[].name/groupCount/content)。

构造:`svc := &workflowtoolSvc{approvalTimeout: 50*time.Millisecond}; svc.RegisterDeps(mockQuery, mockCmd, mockLookup, mockApproval); h := svc.mcpHandlerInit()`;用 `httptest` 造 `tools/call` 请求,Authorization 用 `h.MintToken(agentID, sessionID)`。

- [ ] **Step 2: approval_test.go(镜像 orgtool approval_test,channel 注入)**

覆盖:
- `TestWorkflowApproval_ApprovedExecutes`:`mockApproval.EXPECT().BeginToolApproval(...).Return(ch, nil)`(`ch:=make(chan bool,1); ch<-true`);`mockCmd.EXPECT().Create(...)` 返回 item;`FinishToolApproval(...,"approved", 含"已创建流程")`;result 文本含成功。
- `TestWorkflowApproval_ApprovedButExecError`:Create 返回 error → `FinishToolApproval(...,"approved","执行失败: ...")`。
- `TestWorkflowApproval_Denied`:`ch<-false` → `FinishToolApproval(...,"denied","")`,不 exec。
- `TestWorkflowApproval_Timeout`:ch 不写 → 50ms 超时 → `FinishToolApproval(...,"expired","")`。
- `TestWorkflowApproval_BeginFails`:`BeginToolApproval` 返回 `(nil, err)` → RPC error,无 Finish/exec。
- `TestWorkflowApproval_UpdateMerge`:update 只传 name → `loadWorkflow`(MockQuery.List)取现值 content,`Update` 收到 merge 后 name+原 content。
- `TestWorkflowApproval_DeleteMessage`:delete → 文案含原 GroupCount。

驱动:直接调 `svc.handleWriteTool(httptest.NewRecorder(), req, rpcID, workflowRef{agentID,sessionID}, "workflow_create", rawArgs)`(参照 orgtool approval_test 的调用方式)。

- [ ] **Step 3: 跑绿**
```bash
make mock
go test -race ./internal/service/workflowtool_svc/... 2>&1 | tail -20
```
Expected: 全 PASS。

- [ ] **Step 4: 提交**
```bash
git add internal/service/workflowtool_svc/
git commit -m "✅ workflowtool: mcp + 审批单测(token/门控/schema/list/approve/deny/timeout/merge)"
```

---

## Task 6: bootstrap 接线 + 全仓后端绿

**Files:**
- Modify: `internal/bootstrap/cago.go`、`internal/app/app.go`

- [ ] **Step 1: cago.go 挂 MCP + provider**

在 org 工具接线之后(`gw.RegisterMCP("/mcp/org/", ...)` 那段附近)加:
```go
gw.RegisterMCP("/mcp/workflow/", workflowtool_svc.Default().MCPHandler())
workflowtool_svc.Default().SetGatewayBaseURL(gw.BaseURL())
chat_svc.RegisterTurnMCPProvider(workflowtool_svc.Default().BuildTurnMCP)
```

- [ ] **Step 2: app.go RegisterDeps(在 RegisterChat 之后,与 orgtool 同段)**
```go
// workflow_svc.Workflow() 同时满足 WorkflowQuery + WorkflowCommand。
workflowtool_svc.Default().RegisterDeps(
    workflow_svc.Workflow(), workflow_svc.Workflow(),
    agent_repo.Agent(), chat_svc.Chat(),
)
```
> `chat_svc.Chat()`(PR2 后)满足通用 `ApprovalGateway`(`BeginToolApproval/FinishToolApproval`)。`agent_repo.Agent()` 满足 `AgentLookup`(同 orgtool)。

- [ ] **Step 3: 全仓后端 build + test**
```bash
make mock && go build ./...
make test-backend 2>&1 | tail -15
make lint 2>&1 | tail -10
```
Expected: 全 PASS。

- [ ] **Step 4: bootstrap 装配测试(若有 `bootstrap/cago_test.go` 风格)**

若仓内有 gateway/bootstrap 冒烟测试,确认 `/mcp/workflow/` 注册不 panic;否则跳过(由 `go build ./...` + 启动覆盖)。

- [ ] **Step 5: 提交**
```bash
git add internal/bootstrap internal/app
git commit -m "🔧 workflowtool: bootstrap 挂 /mcp/workflow/ + TurnMCPProvider + RegisterDeps(workflow_svc/agent_repo/chat_svc)"
```

---

## Self-Review(对照 spec Part B)

- **KeyWorkflow 注册**:Task 1 ✓
- **workflowtool_svc 5 文件镜像 orgtool**:Task 2(deps/types)+3(workflowtool/mcp)+4(approval)✓
- **4 工具:list 读无审批 / create·update·delete 写走 PR2 通用审批(ToolKey=workflow)**:Task 3 schemas + 4 approval ✓
- **per-agent ToolEnabled 门控 + HMAC token(镜像 orgtool)**:Task 3 mcp.go ✓
- **业务委托 workflow_svc(DIP 窄接口)**:Task 2 deps + 4 exec ✓
- **bootstrap 挂载 + provider + RegisterDeps**:Task 6 ✓
- **测试矩阵镜像 orgtool**:Task 5 ✓
- **类型一致**:`workflowRef`/`workflowMCP`/`KeyWorkflow`/`Begin·FinishToolApproval`/`ToolKey="workflow"`/工具名 `workflow_{list,create,update,delete}` 跨 task 统一 ✓
- **前端门控/审批渲染**:在 PR4(本 PR 不含;能力 picker 自动出现 key,文案/审批徽标 PR4 配)。
