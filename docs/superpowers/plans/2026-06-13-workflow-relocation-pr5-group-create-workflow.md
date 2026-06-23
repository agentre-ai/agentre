# Part C PR5 · `group_create` 绑定流程(拉群带流程)实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.
>
> **依赖:** 无前置 PR(只用既有 `group_create` MCP 工具 + `CreateGroup`(已支持 `WorkflowID`) + PR2 通用 `tool_approval`,均已在 `develop/group`)。产品闭环「先建流程再拉群带流程」需 PR3(`workflowtool_svc`)一起,但本 PR 可独立落、独立绿。

**Goal:** 给 `group_create` MCP 工具加一个**可选** `workflowId` 入参,从工具一路透传到 `CreateGroup` 的 `WorkflowID`,让 agent 自主拉群时能绑定一个协作流程(SOP);主持人每轮注入该流程最新正文(注入逻辑既有,不改)。

**Architecture:** 纯透传,改动只在 `group_svc`。链路:`group_create` tool schema → `mcp.go` 的 `Arguments` 解码 → `groupCreate` 回调 → `HandleGroupCreate(... workflowID)` → `CreateGroupRequest{WorkflowID}`(→ `group_entity.WorkflowID` 落库,既有)。同时把 `workflowId` 填进审批卡 `ToolInput`,让用户审批时看到绑了哪个流程。不新增建群路径、不改注入/绑定语义、不加迁移/错误码。无效 id 沿用注入侧 `IsActive()` 软门(静默按「不绑定」)。

**Tech Stack:** Go 1.26 (cago)、goconvey、gomock。

**参考既有事实(实现者必读):**

- `CreateGroupRequest` 已有 `WorkflowID int64`(`group_svc/types.go`),`CreateGroup` 已 `WorkflowID: req.WorkflowID` 落库(`group.go:191`),注入侧 `g.WorkflowID > 0 && wf.IsActive()` 已就绪(`group.go:620`)。**本 PR 不碰这些。**
- `group_create` 当前 schema 仅 `{title, memberNames, brief}`(`mcp.go::groupCreateToolSchema`,`required` 三项)。
- `mcp.go` 的 `Arguments` 匿名 struct(`mcp.go:140-154`)逐字段解 JSON;`group_create` 分支在 `mcp.go:181-198`,调 `h.groupCreate(ctx, agentID, sessionID, title, memberNames, brief)`。
- 回调字段类型在 `mcp.go:44`:`groupCreate func(ctx, agentID, sessionID int64, title string, memberNames []string, brief string) (string, error)`。
- 接口 `GroupSvc.HandleGroupCreate`(`group.go:65`)+ 实现(`create.go:46`);`group.go:138` `s.mcp.groupCreate = s.HandleGroupCreate` 自动适配(两处签名一致改即可)。
- `HandleGroupCreate` 内建审批 block 的 `ToolInput`(`create.go:66-67`):`map[string]any{"title":..., "memberNames":..., "brief":...}`。
- 测试现状:`mcp_create_internal_test.go`(内部,2 处用 `h.groupCreate` 回调签名:line 35、52)、`create_test.go`(外部,7 处调 `svc.HandleGroupCreate(...)`:line 125/175/204/222/237/246/266)。**改签名会让这些不编译——本 PR 必须同步更新它们。**
- 签名改了 `GroupSvc` 接口 → `make mock` 重生成 `mock_group_svc`。

**测试/构建:** `go test -race ./internal/service/group_svc/...`;`make mock` 重生成接口 mock;`make test-backend` + `make lint` 收尾。Go 签名级联改动下,"红"以**编译失败**呈现(先改测试断言 → 不编译 = 红 → 改实现 → 绿)。

**文件结构(只改 group_svc):**

- Modify: `internal/service/group_svc/mcp.go`(schema + Arguments 字段 + 回调类型 + 路由透传)
- Modify: `internal/service/group_svc/group.go`(接口签名)
- Modify: `internal/service/group_svc/create.go`(实现签名 + ToolInput + CreateGroup 透传)
- Modify(test): `internal/service/group_svc/mcp_create_internal_test.go`、`internal/service/group_svc/create_test.go`
- Regen: `internal/service/group_svc/mock_group_svc/`(`make mock`)

---

## Task 1: MCP 层透传 `workflowId`(schema + 解码 + 回调 + 路由)

**Files:**
- Modify: `internal/service/group_svc/mcp.go`
- Test: `internal/service/group_svc/mcp_create_internal_test.go`

- [ ] **Step 1: 先改内部测试(红:回调签名 + 新断言)**

把 `mcp_create_internal_test.go` 里两处回调签名加 `workflowID int64` 形参,并在主用例断言收到 `workflowId` + schema 含 `workflowId`。

`TestGroupMCPGroupCreateTool` 第一个 Convey(line 29-48)改为:
```go
Convey("group_create → 回调收到 agentID/sessionID/title/memberNames/brief/workflowID,响应回传回调 text", t, func() {
	var gotAgent, gotSession, gotWorkflow int64
	var gotTitle, gotBrief string
	var gotMembers []string
	h := newGroupMCP(nil)
	h.groupCreate = func(_ context.Context, agentID, sessionID int64, title string, memberNames []string, brief string, workflowID int64) (string, error) {
		gotAgent, gotSession, gotTitle, gotMembers, gotBrief, gotWorkflow = agentID, sessionID, title, memberNames, brief, workflowID
		return "group created: id=12 title=" + title, nil
	}
	tok := h.MintCreateToken(7, 99)
	rr := postMCP(h, tok, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"group_create","arguments":{"title":"新功能开发组","memberNames":["开发","测试"],"brief":"按设计稿重构 UI,验收:e2e 通过","workflowId":5}}}`)
	So(rr.Code, ShouldEqual, 200)
	So(gotAgent, ShouldEqual, 7)
	So(gotSession, ShouldEqual, 99)
	So(gotTitle, ShouldEqual, "新功能开发组")
	So(gotMembers, ShouldResemble, []string{"开发", "测试"})
	So(gotBrief, ShouldEqual, "按设计稿重构 UI,验收:e2e 通过")
	So(gotWorkflow, ShouldEqual, 5)
	So(rr.Body.String(), ShouldContainSubstring, "group created: id=12")
})
```

第二个 Convey(line 50-57)的回调签名补一个 `int64`:
```go
h.groupCreate = func(context.Context, int64, int64, string, []string, string, int64) (string, error) { return "", nil }
```

`tools/list 含 group_create schema` 用例(line 66-70)末尾补一行断言 schema 暴露了 `workflowId`:
```go
So(rr.Body.String(), ShouldContainSubstring, `"workflowId"`)
```

- [ ] **Step 2: 跑红(编译失败)**

Run: `cd /Users/codfrm/Code/agentre/agentre && go test ./internal/service/group_svc/ -run TestGroupMCPGroupCreateTool 2>&1 | tail -15`
Expected: 编译失败(回调类型 `groupCreate` 还是 6 参,测试给了 7 参)/ 或断言 `workflowId` 缺失。

- [ ] **Step 3: 改 `mcp.go` 回调类型(line 44)**
```go
	groupCreate func(ctx context.Context, agentID, sessionID int64, title string, memberNames []string, brief string, workflowID int64) (string, error)
```

- [ ] **Step 4: 改 `mcp.go` 的 `Arguments` struct(line 140-154 内)加字段**

在 `MemberNames []string` 行下方加:
```go
				WorkflowID  int64    `json:"workflowId"`
```

- [ ] **Step 5: 改 `mcp.go` 的 `group_create` 路由(line 191-192)透传**
```go
			text, err := h.groupCreate(r.Context(), cref.agentID, cref.sessionID,
				rpc.Params.Arguments.Title, rpc.Params.Arguments.MemberNames, rpc.Params.Arguments.Brief,
				rpc.Params.Arguments.WorkflowID)
```

- [ ] **Step 6: 改 `mcp.go::groupCreateToolSchema`(line 359)加可选 `workflowId`**

`properties` map 里在 `brief` 行后加(注意 `required` 不变,仍是 `title/memberNames/brief`):
```go
				"workflowId":  map[string]any{"type": "integer", "description": "可选;绑定一个协作流程(SOP)的 id,主持人每轮注入其最新正文。先用 workflow_list 查或 workflow_create 建;省略或 0 = 不绑定。"},
```
并在 `description` 末尾补一句(可选但建议,引导模型):把现有描述结尾追加「需要按既定流程协作时,先建/查流程再用 workflowId 绑定。」

- [ ] **Step 7: 跑绿**

Run: `cd /Users/codfrm/Code/agentre/agentre && go test ./internal/service/group_svc/ -run TestGroupMCPGroupCreateTool 2>&1 | tail -15`
Expected: PASS(回调收到 `workflowId=5`;schema 含 `workflowId`)。

- [ ] **Step 8: 提交**
```bash
git add internal/service/group_svc/mcp.go internal/service/group_svc/mcp_create_internal_test.go
git commit -m "✨ group(mcp): group_create 加可选 workflowId 入参 + 透传回调(schema/解码/路由)"
```

---

## Task 2: `HandleGroupCreate` 把 `workflowID` 透传到 `CreateGroup` + 审批卡 ToolInput

**Files:**
- Modify: `internal/service/group_svc/group.go`(接口签名)、`internal/service/group_svc/create.go`(实现)
- Test: `internal/service/group_svc/create_test.go`

- [ ] **Step 1: 先改 `create_test.go`(红:加断言 + 同步所有调用点)**

在 `TestHandleGroupCreate_ApprovedExecutes`(line 52-148)里:① 调用处(line 125)末尾传 `workflowID=5`;② 补两条断言。

调用改为:
```go
			text, err = svc.HandleGroupCreate(context.Background(), 7, 99, "新功能开发组", []string{"开发"}, "按设计稿重构 UI,验收:e2e 通过", 5)
```
在 `So(createdGroup.ProjectID, ShouldEqual, 3)` 之后补:
```go
			So(createdGroup.WorkflowID, ShouldEqual, 5) // workflowId 透传到群实体
			So(begunBlk.ToolInput["workflowId"], ShouldEqual, int64(5)) // 审批卡展示绑定的流程
```

其余 6 处 `HandleGroupCreate(...)` 调用(line 175/204/222/237/246/266)在末尾补 `, 0`(这些用例不关心 workflow,传 0=不绑定)。逐处:
- line 175:`...[]string{"开发"}, "brief", 0)`
- line 204:`...[]string{"开发"}, "brief", 0)`
- line 222:`...[]string{"开发"}, "b", 0)`
- line 237:`...nil, "b", 0)`
- line 246:`...nil, "b", 0)`
- line 266:`...[]string{"测试"}, "b", 0)`

- [ ] **Step 2: 跑红(编译失败)**

Run: `cd /Users/codfrm/Code/agentre/agentre && go test ./internal/service/group_svc/ -run TestHandleGroupCreate 2>&1 | tail -15`
Expected: 编译失败(`HandleGroupCreate` 还是 6 参)。

- [ ] **Step 3: 改接口签名 `group.go:65`**
```go
	HandleGroupCreate(ctx context.Context, agentID, sessionID int64, title string, memberNames []string, brief string, workflowID int64) (string, error)
```

- [ ] **Step 4: 改实现 `create.go`**

签名(line 46):
```go
func (s *groupSvc) HandleGroupCreate(ctx context.Context, agentID, sessionID int64, title string, memberNames []string, brief string, workflowID int64) (string, error) {
```

审批 block 的 `ToolInput`(line 66-67)加 `workflowId`:
```go
	blk := &chatblocks.ToolApprovalBlock{ToolKey: toolKeyGroupCreate, RequestID: requestID, ToolName: "group_create",
		ToolInput: map[string]any{"title": title, "memberNames": memberNames, "brief": brief, "workflowId": workflowID}, Status: "pending"}
```

`CreateGroup` 调用(line 87-92)加 `WorkflowID`:
```go
	detail, err := s.CreateGroup(ctx, &CreateGroupRequest{
		Title:          title,
		HostAgentID:    agentID,
		ProjectID:      sess.ProjectID, // 群目录 = 发起会话的项目目录
		MemberAgentIDs: memberIDs,
		WorkflowID:     workflowID, // 可选绑定的协作流程(0=不绑定;注入侧 IsActive 软门)
	})
```

- [ ] **Step 5: 跑绿**

Run: `cd /Users/codfrm/Code/agentre/agentre && go test -race ./internal/service/group_svc/ -run TestHandleGroupCreate 2>&1 | tail -15`
Expected: PASS(含新的 `createdGroup.WorkflowID==5` + `ToolInput["workflowId"]==int64(5)`)。

- [ ] **Step 6: 提交**
```bash
git add internal/service/group_svc/group.go internal/service/group_svc/create.go internal/service/group_svc/create_test.go
git commit -m "✨ group: HandleGroupCreate 透传 workflowID→CreateGroup + 审批卡 ToolInput(拉群带流程)"
```

---

## Task 3: 重生成 mock + 全仓后端绿

**Files:**
- Regen: `internal/service/group_svc/mock_group_svc/`
- 验证:无源码改动

- [ ] **Step 1: 重生成接口 mock(GroupSvc 签名变了)**
```bash
cd /Users/codfrm/Code/agentre/agentre && make mock
```
Expected: `mock_group_svc/mock_*.go` 里 `HandleGroupCreate` 形参更新为 7 参(`go generate` 无报错)。

- [ ] **Step 2: 全仓后端 build + test + lint**
```bash
cd /Users/codfrm/Code/agentre/agentre
go build ./... 2>&1 | tail -5
make test-backend 2>&1 | tail -15
make lint 2>&1 | tail -10
```
Expected: 全 PASS。重点确认 `group_svc` 全绿、无遗漏的 `HandleGroupCreate` 旧 6 参调用点、无 `mock_group_svc` 签名不匹配。

> 若 `go build ./...` 报某处仍按 6 参调 `HandleGroupCreate`/`groupCreate`,那是遗漏的调用点,补传 `0`(或语义对应的 workflowID)。e2e 包(`-tags e2e`)若引用了 `group_create` 工具的 arguments,确认仍兼容(`workflowId` 可选,旧指令不传不受影响)。

- [ ] **Step 3: 提交(若 mock 有 diff)**
```bash
git add internal/service/group_svc/mock_group_svc/
git commit -m "🔧 mock: 重生成 group_svc(HandleGroupCreate 加 workflowID 形参)"
```

---

## Self-Review(对照 spec Part C)

- **schema 加可选 `workflowId`(不进 required)**:Task 1 Step 6 ✓
- **arguments 解码 `workflowId`**:Task 1 Step 4 ✓
- **回调类型 + 路由透传**:Task 1 Step 3/5 ✓
- **接口 + 实现加 `workflowID` 形参**:Task 2 Step 3/4 ✓
- **透传 `CreateGroupRequest.WorkflowID`**:Task 2 Step 4 ✓
- **审批卡 `ToolInput["workflowId"]`(用户审批可见)**:Task 2 Step 4 + 断言 Step 1 ✓
- **不改注入/绑定语义、不加迁移/错误码**:全程只透传,未碰 `CreateGroup`/注入/迁移 ✓
- **无效 id 软门(不额外校验)**:未加校验,沿用既有 `IsActive()` 注入门 ✓
- **既有测试同步更新保持绿(回归护栏)**:Task 1(内部回调签名)+ Task 2(7 处调用点)+ Task 3(mock 重生成)✓
- **类型一致**:`workflowID int64` / json `workflowId` / `ToolInput["workflowId"]` 跨 Task 统一;回调与接口/实现均 7 参 ✓
