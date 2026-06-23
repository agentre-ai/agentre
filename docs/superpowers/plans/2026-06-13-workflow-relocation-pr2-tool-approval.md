# Part B PR2 · 审批管线泛化为通用 tool_approval 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把现已被 org 工具 + group_create 共用、但命名为 "org" 的审批管线，重命名为通用 `tool_approval`（block 带 `ToolKey`），并把分散在各工具服务里的 waiter + 各自的 `Answer*Approval` binding **上收进 chat_svc** 成单一 `AnswerToolApproval`。纯重构（不改行为），为 PR3 接入 workflow 工具铺路。

**Architecture:** `chat_svc` 成为审批管线的唯一拥有者：`BeginToolApproval` 登记 block + 推流 + **返回等待 channel**；`AnswerToolApproval(requestID)` 路由唤醒；`FinishToolApproval` 落终态 + 推流 + 清 waiter。工具服务（orgtool_svc / group_svc）只 `Begin(→ch) → 等 ch → Finish`，不再各自持 waiter / Answer。前端审批卡 `OrgApprovalCard → ToolApprovalCard`（读 `block.toolApproval`，统一调 `AnswerToolApproval`）。

**Tech Stack:** Go 1.26 (cago), goconvey/gomock, React 19 + TS + Vitest。

**Spec:** `docs/superpowers/specs/2026-06-13-workflow-library-relocation-and-agent-tool-design.md`（Part B「审批管线」节）。

---

## 背景与既有事实（实现者必读）

### 关键认知:管线已通用、已被两工具共用

- `internal/service/chat_svc/blocks/org_approval.go` 的 `OrgApprovalBlock` **无 org 专属字段**:
  ```go
  type OrgApprovalBlock struct {
      RequestID string         `json:"request_id"`
      ToolName  string         `json:"tool_name"`
      ToolInput map[string]any `json:"tool_input,omitempty"`
      Status    string         `json:"status"`
      Result    string         `json:"result,omitempty"`
  }
  func (OrgApprovalBlock) Type() string { return "org_approval" }
  func (OrgApprovalBlock) Audience() cagoblocks.AudienceMask { return cagoblocks.ToUI }
  func init() { cagoblocks.RegisterFactory[OrgApprovalBlock]() }
  ```
- **co-consumers**：org 工具（`orgtool_svc`，ToolName=`org_*`）+ group_create（`group_svc`，ToolName=`group_create`）都建这个 block 走 `chat_svc.Begin/FinishOrgApproval`。前端 `OrgApprovalCard` 已按 `toolName` 通用渲染。
- **waiter 现状（要上收）**：`orgtool_svc` 自持 `waiters sync.Map` + `AnswerOrgApproval`;`group_svc` 同款自持 + `AnswerGroupCreateApproval`。两份重复，本 PR 上收进 chat_svc。

### 现有 chat_svc 审批代码(将改名+扩展)

`internal/service/chat_svc/org_approval.go`(完整,改名为 `tool_approval.go`):

```go
func (s *chatSvc) BeginOrgApproval(ctx context.Context, sessionID int64, blk *blocks.OrgApprovalBlock) error {
    streamAny, ok := s.activeTurnStreams.Load(sessionID)
    if !ok { return fmt.Errorf("chat_svc.BeginOrgApproval: no active turn for session %d", sessionID) }
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

func (s *chatSvc) FinishOrgApproval(ctx context.Context, sessionID int64, requestID, status, result string) error {
    s.orgApprovalsMu.Lock()
    var snapshot *blocks.OrgApprovalBlock
    for _, b := range s.orgApprovals[sessionID] {
        if b.RequestID == requestID { b.Status = status; b.Result = result; cp := *b; snapshot = &cp; break }
    }
    s.orgApprovalsMu.Unlock()
    if snapshot == nil { return fmt.Errorf("chat_svc.FinishOrgApproval: request %s not found (turn finalized?)", requestID) }
    if streamAny, ok := s.activeTurnStreams.Load(sessionID); ok {
        stream := streamAny.(string)
        s.emitter.Emit(ctx, stream, orgApprovalEventPayload(*snapshot))
        if sess, err := chat_repo.Session().Find(ctx, sessionID); err == nil && sess != nil { s.markSessionRunning(ctx, sess, stream) }
    }
    return nil
}

func orgApprovalEventPayload(b blocks.OrgApprovalBlock) map[string]any {
    return map[string]any{"kind": "org_approval", "requestId": b.RequestID, "toolName": b.ToolName, "toolInput": b.ToolInput, "status": b.Status, "result": b.Result}
}

func (s *chatSvc) takeOrgApprovals(sessionID int64) []*blocks.OrgApprovalBlock { /* finalize 取走,pending→expired */ }
func (s *chatSvc) snapshotOrgApprovals(sessionID int64) []blocks.OrgApprovalBlock { /* overlay 拷贝 */ }
```

chat_svc 状态字段(在 `chat.go`)：`orgApprovals map[int64][]*blocks.OrgApprovalBlock`、`orgApprovalsMu sync.Mutex`(初始化在 `chat.go:140`),接口声明 `BeginOrgApproval/FinishOrgApproval`(`chat.go:180/187`),finalize 里 `case chatblocks.OrgApprovalBlock`(`chat.go:~1420`),`types.go` 的 `ChatBlockOrgApproval` DTO,`chat_internal_test.go:TestToChatMessage_OrgApprovalBlock`。

### 现有工具侧 waiter(将删除,上收 chat_svc)

`orgtool_svc/approval.go` 的 `handleWriteTool`：`ch := make(chan bool,1); s.waiters.Store(requestID, ch); defer s.waiters.Delete(requestID); s.approval.BeginOrgApproval(...); select { case allow := <-ch / timeout / ctx.Done }`。`AnswerOrgApproval`：`s.waiters.LoadAndDelete(requestID) → ch <- allow`。`orgtoolSvc` 结构有 `waiters sync.Map`。`ApprovalGateway` 接口(`deps.go`)：`BeginOrgApproval/FinishOrgApproval`。

group_svc 同款(`gateway.go` 的 `BeginGroupCreateApproval/FinishGroupCreateApproval` 实为转发 chat_svc;其自持 waiter + `AnswerGroupCreateApproval`)。

### 现有 App binding(将合并)

`internal/app/orgtool.go`：`App.AnswerOrgApproval(req *orgtool_svc.AnswerOrgApprovalRequest)`。group 侧有 `App.AnswerGroupCreateApproval`。两者合并成 `App.AnswerToolApproval`。

### 前端(将改名)

- `frontend/src/components/agentre/org-approval/card.tsx` `OrgApprovalCard`：props `{approval: OrgApprovalData; sessionId}`;按 `toolName==="group_create"` 路由 `AnswerGroupCreateApproval`,否则 `AnswerOrgApproval`;`group_create` approved 后 reload group list。i18n `orgApproval.*`。
- `transcript-row-view.tsx:520` `case "org_approval"` → `<OrgApprovalCard approval={item.block.orgApproval} .../>`。
- `transcript-rows.ts:67/254` 块类型 `"org_approval"` + `block.orgApproval`。
- `chat-streams-store.ts:17` `OrgApprovalData`、`appendLiveOrgApproval`、`markOrgApprovalResolved`;`chat-streams-host.tsx:178` `case "org_approval"`。
- `use-chat-session.ts` overlay、`use-chat-stream.ts` `LiveBlockType` union 含 `"org_approval"`。
- 生成类型：`chat_svc.ChatBlockOrgApproval`(由 Go `types.go` 经 `make generate` 产出)。

### 测试与构建约束

- Go：`make test-backend`(排除 frontend);聚焦 `go test -race ./internal/service/chat_svc/... ./internal/service/orgtool_svc/... ./internal/service/group_svc/...`。mock 用 mockgen(`make mock`)。**不连真库**(svc 测试注 mock)。
- 前端：`cd frontend && pnpm test`;改 Go `types.go` 后跑 `GOWORK=off make generate` 重生成 `wailsjs`(本仓已在主 checkout,`wailsjs/` 存在)。tsc：`pnpm exec tsc -b`。
- gitmoji 提交;golangci-lint v2(`make lint`)。
- **纯重构原则**：本 PR 不改任何外部行为;现有 org + group_create 审批测试改名/迁移后**必须全绿**——这是零回归护栏。

### 文件结构

**重命名(git mv + 内容改):**
- `internal/service/chat_svc/blocks/org_approval.go` → `tool_approval.go`(+ `_test.go`)
- `internal/service/chat_svc/org_approval.go` → `tool_approval.go`(+ `org_approval_test.go` → `tool_approval_test.go`)
- `frontend/src/components/agentre/org-approval/` → `tool-approval/`(`card.tsx`/`card.test.tsx`)

**改(不改名):** `chat_svc/chat.go`、`chat_svc/types.go`、`chat_svc/chat_internal_test.go`、`orgtool_svc/{approval.go,deps.go,orgtool.go,approval_test.go,mock_orgtool_svc/}`、`group_svc/{gateway.go,create.go,create_test.go}`、`internal/app/orgtool.go`(+ group)、bootstrap `app.go`、前端 `transcript-row-view.tsx`/`transcript-rows.ts`/`chat-streams-store.ts`/`chat-streams-host.tsx`/`use-chat-session.ts`/`use-chat-stream.ts`、i18n `common.json`(zh+en)。

**新增:** chat_svc `toolApprovalWaiters sync.Map` + `AnswerToolApproval`;`App.AnswerToolApproval`。

---

## Task 1: 回归基线(改前先全绿)

**Files:** 无(验证)

- [ ] **Step 1: 跑现有审批测试,确认绿(改前基线)**

Run:
```bash
cd /Users/codfrm/Code/agentre/agentre
go test -race ./internal/service/chat_svc/... ./internal/service/orgtool_svc/... ./internal/service/group_svc/... 2>&1 | tail -20
cd frontend && pnpm test -- src/components/agentre/org-approval src/components/agentre/org/__tests__/org-detail-agent.test.tsx 2>&1 | grep -E "Test Files|Tests "
```
Expected: 全 PASS。记录通过数作为重构后对照。若有预存 flake(见项目记忆 chat_svc Edit -race flake),重跑确认非本改动。

- [ ] **Step 2: 不提交**(纯基线)。

---

## Task 2: block 泛化 OrgApprovalBlock → ToolApprovalBlock(+ ToolKey)

**Files:**
- Rename: `internal/service/chat_svc/blocks/org_approval.go` → `internal/service/chat_svc/blocks/tool_approval.go`
- Rename: `internal/service/chat_svc/blocks/org_approval_test.go` → `tool_approval_test.go`

- [ ] **Step 1: git mv + 改 block 定义**

```bash
git mv internal/service/chat_svc/blocks/org_approval.go internal/service/chat_svc/blocks/tool_approval.go
git mv internal/service/chat_svc/blocks/org_approval_test.go internal/service/chat_svc/blocks/tool_approval_test.go
```

`tool_approval.go` 新内容:
```go
package blocks

import cagoblocks "..." // 保留原 import 路径

// ToolApprovalBlock agent 内置工具(org / group_create / workflow 等)写操作的服务端审批卡。
// ToolKey 标识来源工具(供前端选标题/文案与后处理);Status: pending → approved|denied|expired。
type ToolApprovalBlock struct {
    ToolKey   string         `json:"tool_key"`
    RequestID string         `json:"request_id"`
    ToolName  string         `json:"tool_name"`
    ToolInput map[string]any `json:"tool_input,omitempty"`
    Status    string         `json:"status"`
    Result    string         `json:"result,omitempty"`
}

func (ToolApprovalBlock) Type() string                      { return "tool_approval" }
func (ToolApprovalBlock) Audience() cagoblocks.AudienceMask { return cagoblocks.ToUI }

func init() { cagoblocks.RegisterFactory[ToolApprovalBlock]() }
```

- [ ] **Step 2: 改 block 测试**

`tool_approval_test.go`：把 `OrgApprovalBlock` → `ToolApprovalBlock`,`"org_approval"` → `"tool_approval"`,round-trip 用例补 `ToolKey: "org"` 字段断言。

- [ ] **Step 3: 编译会红(全仓还引用 OrgApprovalBlock)——本 task 暂不单独跑全仓**

仅跑 block 包：`go test ./internal/service/chat_svc/blocks/...`(此包内自洽,应过)。全仓编译在 Task 3-6 改完引用后恢复。

- [ ] **Step 4: 不提交**(与 Task 3 合并提交,避免中间不可编译态)。

---

## Task 3: chat_svc 审批改名 + 上收 waiter + AnswerToolApproval

**Files:**
- Rename: `internal/service/chat_svc/org_approval.go` → `tool_approval.go`(+ `org_approval_test.go` → `tool_approval_test.go`)
- Modify: `internal/service/chat_svc/chat.go`、`types.go`、`chat_internal_test.go`

- [ ] **Step 1: git mv**
```bash
git mv internal/service/chat_svc/org_approval.go internal/service/chat_svc/tool_approval.go
git mv internal/service/chat_svc/org_approval_test.go internal/service/chat_svc/tool_approval_test.go
```

- [ ] **Step 2: 改 `tool_approval.go`(改名 + Begin 返回 channel + Answer + waiter)**

```go
// BeginToolApproval 在 sessionID 活跃 turn 上登记 pending 审批、推流,并返回等待 channel
// (buffered=1)。工具服务 select 该 channel(allow)/超时/ctx。无活跃 turn → error。
func (s *chatSvc) BeginToolApproval(ctx context.Context, sessionID int64, blk *blocks.ToolApprovalBlock) (<-chan bool, error) {
    streamAny, ok := s.activeTurnStreams.Load(sessionID)
    if !ok {
        return nil, fmt.Errorf("chat_svc.BeginToolApproval: no active turn for session %d", sessionID)
    }
    stream := streamAny.(string)
    s.toolApprovalsMu.Lock()
    s.toolApprovals[sessionID] = append(s.toolApprovals[sessionID], blk)
    snapshot := *blk
    s.toolApprovalsMu.Unlock()

    ch := make(chan bool, 1)
    s.toolApprovalWaiters.Store(blk.RequestID, ch)

    s.emitter.Emit(ctx, stream, toolApprovalEventPayload(snapshot))
    if sess, err := chat_repo.Session().Find(ctx, sessionID); err == nil && sess != nil {
        s.markSessionWaiting(ctx, sess, stream)
    }
    return ch, nil
}

// AnswerToolApproval 按 requestID 唤醒挂起的写工具调用(前端审批入口的唯一后端方法)。
// 未知/重复/已超时 → error。
func (s *chatSvc) AnswerToolApproval(ctx context.Context, sessionID int64, requestID string, allow bool) error {
    if requestID == "" {
        return fmt.Errorf("chat_svc.AnswerToolApproval: empty requestID")
    }
    chAny, ok := s.toolApprovalWaiters.LoadAndDelete(requestID)
    if !ok {
        return fmt.Errorf("chat_svc.AnswerToolApproval: request %s not found", requestID)
    }
    chAny.(chan bool) <- allow
    return nil
}

func (s *chatSvc) FinishToolApproval(ctx context.Context, sessionID int64, requestID, status, result string) error {
    s.toolApprovalWaiters.Delete(requestID) // 终态兜底清 waiter(超时/拒绝/ctx 死路径)
    s.toolApprovalsMu.Lock()
    var snapshot *blocks.ToolApprovalBlock
    for _, b := range s.toolApprovals[sessionID] {
        if b.RequestID == requestID {
            b.Status = status
            b.Result = result
            cp := *b
            snapshot = &cp
            break
        }
    }
    s.toolApprovalsMu.Unlock()
    if snapshot == nil {
        return fmt.Errorf("chat_svc.FinishToolApproval: request %s not found (turn finalized?)", requestID)
    }
    if streamAny, ok := s.activeTurnStreams.Load(sessionID); ok {
        stream := streamAny.(string)
        s.emitter.Emit(ctx, stream, toolApprovalEventPayload(*snapshot))
        if sess, err := chat_repo.Session().Find(ctx, sessionID); err == nil && sess != nil {
            s.markSessionRunning(ctx, sess, stream)
        }
    }
    return nil
}

func toolApprovalEventPayload(b blocks.ToolApprovalBlock) map[string]any {
    return map[string]any{
        "kind":      "tool_approval",
        "toolKey":   b.ToolKey,
        "requestId": b.RequestID,
        "toolName":  b.ToolName,
        "toolInput": b.ToolInput,
        "status":    b.Status,
        "result":    b.Result,
    }
}
```
并把 `takeOrgApprovals`/`snapshotOrgApprovals` → `takeToolApprovals`/`snapshotToolApprovals`(逻辑不变,类型换 `ToolApprovalBlock`,map 换 `s.toolApprovals`)。

- [ ] **Step 3: 改 `chat.go` 状态/接口/finalize**

- 字段:`orgApprovals map[int64][]*blocks.OrgApprovalBlock` → `toolApprovals map[int64][]*blocks.ToolApprovalBlock`;`orgApprovalsMu` → `toolApprovalsMu`;**新增** `toolApprovalWaiters sync.Map`。初始化处(`chat.go:140` 附近)同步改名 + 初始化 map。
- 接口(`ChatSvc` interface,`chat.go:180/187`):
  ```go
  BeginToolApproval(ctx context.Context, sessionID int64, blk *chatblocks.ToolApprovalBlock) (<-chan bool, error)
  FinishToolApproval(ctx context.Context, sessionID int64, requestID, status, result string) error
  AnswerToolApproval(ctx context.Context, sessionID int64, requestID string, allow bool) error
  ```
- finalize `case chatblocks.OrgApprovalBlock` → `case chatblocks.ToolApprovalBlock`;`takeOrgApprovals` 调用点 → `takeToolApprovals`。snapshot 调用点同改。

- [ ] **Step 4: 改 `types.go` DTO + `chat_internal_test.go`**

`ChatBlockOrgApproval` → `ChatBlockToolApproval`(加 `ToolKey string` 字段 + json tag `toolKey`);to-chat-message 映射 `case *blocks.ToolApprovalBlock`(或值类型,照原样);`chat_internal_test.go:TestToChatMessage_OrgApprovalBlock` → `...ToolApprovalBlock`,断言加 ToolKey。

- [ ] **Step 5: 改 `tool_approval_test.go`(原 chat_svc org_approval 生命周期测试)**

把 `BeginOrgApproval/FinishOrgApproval` → `Begin/FinishToolApproval`(注意 Begin 现返回 `(<-chan bool, error)`,旧断言 `err :=` 改 `ch, err :=` 并可断言 `ch != nil`);block 用 `ToolApprovalBlock{ToolKey:"org", ...}`;事件 payload 断言 `kind:"tool_approval"` + `toolKey`。**新增**用例:`AnswerToolApproval` 命中 waiter → channel 收到 allow;未知 requestID → error;`FinishToolApproval` 清 waiter 后 Answer 同 requestID → error。

- [ ] **Step 6: 暂不全仓编译(orgtool/group/app 仍引用旧名)。提交 Task 2+3 合并(block + chat_svc 一起,前者无它不可编译)。**

> 注:Task 2/3 必须合并提交——单独 Task 2 让 blocks 包改名但 chat_svc 仍引用旧名→不可编译。合并后 chat_svc 包自洽(但全仓仍红,待 Task 4-6)。

```bash
go test ./internal/service/chat_svc/... 2>&1 | tail -15   # chat_svc 包应过(全仓还红)
git add internal/service/chat_svc/
git commit -m "♻️ approval: chat_svc 审批改名 tool_approval + 上收 waiter + AnswerToolApproval(block 加 ToolKey)"
```

---

## Task 4: orgtool_svc 迁到通用网关(删自持 waiter + AnswerOrgApproval)

**Files:**
- Modify: `internal/service/orgtool_svc/{deps.go, orgtool.go, approval.go, mcp.go}`、`approval_test.go`、`mock_orgtool_svc/mock_deps.go`(regenerate)

- [ ] **Step 1: 改 `deps.go` 的 ApprovalGateway**
```go
type ApprovalGateway interface {
    BeginToolApproval(ctx context.Context, sessionID int64, blk *blocks.ToolApprovalBlock) (<-chan bool, error)
    FinishToolApproval(ctx context.Context, sessionID int64, requestID, status, result string) error
}
```

- [ ] **Step 2: 改 `orgtool.go`**:删 `waiters sync.Map` 字段。

- [ ] **Step 3: 改 `approval.go`**:
- 删 `AnswerOrgApprovalRequest/Response` + `AnswerOrgApproval` 方法(整段移除)。
- `handleWriteTool` 改为用 chat_svc 返回的 channel:
  ```go
  func (s *orgtoolSvc) handleWriteTool(w http.ResponseWriter, r *http.Request, rpcID json.RawMessage, ref orgRef, tool string, rawArgs json.RawMessage) {
      var input map[string]any
      _ = json.Unmarshal(rawArgs, &input)
      requestID := uuid.NewString()
      blk := &blocks.ToolApprovalBlock{ToolKey: agenttool.KeyOrg, RequestID: requestID, ToolName: tool, ToolInput: input, Status: "pending"}

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
          result, execErr := s.execWriteTool(r.Context(), ref, tool, rawArgs)
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
  ```
  (import 从 `blocks` 保留;删掉对 `i18n`/`code` 的 import 若仅 AnswerOrgApproval 用。)

- [ ] **Step 4: regenerate mock**
```bash
cd /Users/codfrm/Code/agentre/agentre && make mock
# 或定向: go generate ./internal/service/orgtool_svc/...
```
确认 `mock_orgtool_svc/mock_deps.go` 的 `MockApprovalGateway` 变成 `BeginToolApproval(...)(<-chan bool,error)` + `FinishToolApproval`。

- [ ] **Step 5: 改 `approval_test.go`**:
- 所有 `MockApprovalGateway.EXPECT().BeginOrgApproval(...)` → `BeginToolApproval(...)`,并让其 `Return(ch, nil)`(测试里造 `ch := make(chan bool,1)`;approved 用例 `ch <- true`,denied `ch <- false`)。`FinishOrgApproval` → `FinishToolApproval`。
- 原 `TestOrgApproval_AnswerInvalid`(测 AnswerOrgApproval 重复/未知)**迁移**到 chat_svc 的 `AnswerToolApproval`(已在 Task 3 Step 5 覆盖)——这里删掉该用例(orgtool 不再有 Answer)。
- 其余用例(Approved/Denied/Timeout/ExecError/BeginFails/UpdateDepartmentMove/CreateAgentInheritsBackend)改用 channel 注入,断言不变。

- [ ] **Step 6: 删 App binding 引用(暂留到 Task 6 统一)** —— 本 task 先不动 `internal/app`。orgtool 包内编译应过:
```bash
go test ./internal/service/orgtool_svc/... 2>&1 | tail -15
```
Expected: PASS(`internal/app` 仍引用 `AnswerOrgApproval` → 全仓红,Task 6 修)。

- [ ] **Step 7: 提交**
```bash
git add internal/service/orgtool_svc/
git commit -m "♻️ orgtool: 审批走 chat_svc 通用 BeginToolApproval(删自持 waiter+AnswerOrgApproval,带 ToolKey=org)"
```

---

## Task 5: group_svc 迁到通用网关(删自持 waiter + AnswerGroupCreateApproval)

**Files:**
- Modify: `internal/service/group_svc/{gateway.go, create.go}`、`create_test.go`、其 mock(若有)

- [ ] **Step 1: 读 group_svc 现状,镜像 Task 4 改造**

先读 `internal/service/group_svc/gateway.go` + `create.go` 看 group_create 审批是怎么调 chat_svc 的(`BeginGroupCreateApproval/FinishGroupCreateApproval` 是否就是转发 `BeginOrgApproval`,以及 waiter/AnswerGroupCreateApproval 在哪)。按实际:
- `ApprovalGateway`(group 侧的窄接口,若有)→ 通用 `BeginToolApproval/FinishToolApproval`。
- 建 block 处 `blocks.OrgApprovalBlock{...}` → `blocks.ToolApprovalBlock{ToolKey: "group_create", ToolName: "group_create", ...}`。
- 删 group 侧 `waiters` + `AnswerGroupCreateApproval`(+ 其 Request/Response 类型),`handleXxx` 改用 Begin 返回的 channel(同 Task 4 Step 3 形态)。

> ToolKey 常量:`"group_create"`(group 工具没有 agenttool registry 项;用字面量或在 group_svc 内定义 const)。

- [ ] **Step 2: regenerate mock(若 group_svc 有 mockgen deps)**：`make mock`。

- [ ] **Step 3: 改 `create_test.go`**:`BeginGroupCreateApproval/FinishGroupCreateApproval`(或底层 Begin/FinishOrgApproval mock)→ 通用 `Begin/FinishToolApproval`,channel 注入;删迁移到 chat_svc 的 Answer 用例。block 断言带 `ToolKey:"group_create"`。

- [ ] **Step 4: group_svc 包编译过**
```bash
go test ./internal/service/group_svc/... 2>&1 | tail -15
```
Expected: PASS(全仓仍红待 Task 6)。

- [ ] **Step 5: 提交**
```bash
git add internal/service/group_svc/
git commit -m "♻️ group: group_create 审批走 chat_svc 通用 BeginToolApproval(删自持 waiter+Answer,ToolKey=group_create)"
```

---

## Task 6: App binding 合并 AnswerToolApproval + 全仓后端绿

**Files:**
- Modify: `internal/app/orgtool.go`(改名/合并)、bootstrap `app.go`(若 RegisterDeps 形态变)、删 group 的 Answer binding 文件/方法

- [ ] **Step 1: 合并 binding**

把 `App.AnswerOrgApproval` + `App.AnswerGroupCreateApproval` 替换成单个：
```go
// AnswerToolApproval agent 内置工具写操作的审批决策(批准/拒绝),按 requestID 路由。
func (a *App) AnswerToolApproval(req *chat_svc.AnswerToolApprovalRequest) (*chat_svc.AnswerToolApprovalResponse, error) {
    if req == nil {
        return nil, ... // InvalidParameter
    }
    err := chat_svc.Default().AnswerToolApproval(a.ctx, req.SessionID, req.RequestID, req.Allow)
    if err != nil {
        return nil, err
    }
    return &chat_svc.AnswerToolApprovalResponse{}, nil
}
```
在 chat_svc 定义 wails 用的 DTO(放 chat_svc/types.go 或 tool_approval.go):
```go
type AnswerToolApprovalRequest struct {
    SessionID int64  `json:"sessionId"`
    RequestID string `json:"requestId"`
    Allow     bool   `json:"allow"`
}
type AnswerToolApprovalResponse struct{}
```
删 `internal/app` 里 org/group 两个旧 Answer 方法 + orgtool/group 的 Answer*Request 类型残留引用。

- [ ] **Step 2: RegisterDeps 不变检查**

`app.go` 的 `orgtool_svc.Default().RegisterDeps(..., chat_svc.Chat())` —— `chat_svc.Chat()`(=*chatSvc 投影)现满足新的通用 `ApprovalGateway`(`BeginToolApproval/FinishToolApproval`),签名兼容则无需改。group_svc 同理。确认编译。

- [ ] **Step 3: 全仓后端编译 + 测试绿**
```bash
make mock   # 确保所有 mock 最新
go build ./...
make test-backend 2>&1 | tail -25
```
Expected: 全 PASS。重点确认 chat_svc / orgtool_svc / group_svc / app 全绿(= org + group_create 行为零回归)。

- [ ] **Step 4: lint**
```bash
make lint 2>&1 | tail -15
```

- [ ] **Step 5: 提交**
```bash
git add internal/app internal/service/chat_svc internal/bootstrap
git commit -m "♻️ approval: App binding 合并为单一 AnswerToolApproval,后端全仓通用化完成"
```

---

## Task 7: 重生成 wailsjs + 前端审批卡改名

**Files:**
- Regenerate: `frontend/wailsjs/`(由 `make generate`)
- Rename: `frontend/src/components/agentre/org-approval/` → `tool-approval/`
- Modify: `transcript-row-view.tsx`、`transcript-rows.ts`、`chat-streams-store.ts`、`chat-streams-host.tsx`、`use-chat-session.ts`、`use-chat-stream.ts`、i18n `common.json`(zh+en)

- [ ] **Step 1: 重生成绑定**
```bash
cd /Users/codfrm/Code/agentre/agentre && GOWORK=off make generate
```
确认 `frontend/wailsjs/go/models.ts` 现有 `chat_svc.ChatBlockToolApproval`(含 `toolKey`),`wailsjs/go/app/App` 有 `AnswerToolApproval`,无 `AnswerOrgApproval`/`AnswerGroupCreateApproval`。

- [ ] **Step 2: git mv 审批卡目录 + 改名组件**
```bash
git mv frontend/src/components/agentre/org-approval frontend/src/components/agentre/tool-approval
git mv frontend/src/components/agentre/tool-approval/card.tsx frontend/src/components/agentre/tool-approval/card.tsx  # 文件名不变,改内容
```
`card.tsx`：`OrgApprovalCard` → `ToolApprovalCard`;props `{approval: ToolApprovalData; sessionId}`;`ToolApprovalData = Omit<chat_svc.ChatBlockToolApproval, "convertValues">`;**统一** Approve/Deny 调 `AnswerToolApproval(chat_svc.AnswerToolApprovalRequest.createFrom({sessionId, requestId, allow}))`(删 group_create 的 `AnswerGroupCreateApproval` 分支——requestID 现由 chat_svc 统一路由);**保留** group_create 特有后处理(approved 后 reload group list:按 `approval.toolKey === "group_create"` 或 `toolName === "group_create"` 触发)。i18n `orgApproval.*` → `toolApproval.*`。

- [ ] **Step 3: 改 transcript 路由**

`transcript-rows.ts`：`type "org_approval"` → `"tool_approval"`,`block.orgApproval` → `block.toolApproval`,`case "org_approval"` → `"tool_approval"`(两处:类型 union 行 + push 行)。
`transcript-row-view.tsx:520`：`case "org_approval"` → `case "tool_approval"`;`<OrgApprovalCard approval={item.block.orgApproval} ...>` → `<ToolApprovalCard approval={item.block.toolApproval} ...>`(import 改路径/名)。

- [ ] **Step 4: 改 streams**

`chat-streams-store.ts`：`OrgApprovalData` → `ToolApprovalData`(基于 `ChatBlockToolApproval`),`appendLiveOrgApproval` → `appendLiveToolApproval`,`markOrgApprovalResolved` → `markToolApprovalResolved`,字段 `orgApproval` → `toolApproval`,payload 加 `toolKey`。
`chat-streams-host.tsx:178`：`case "org_approval"` → `case "tool_approval"`;payload 加 `toolKey: ev.toolKey ?? ""`;调改名后的 append/markResolved。
`use-chat-session.ts`(overlay)、`use-chat-stream.ts`(`LiveBlockType` union `"org_approval"` → `"tool_approval"`)同步改名。

- [ ] **Step 5: i18n 改名**

`common.json`(zh+en)：把 `orgApproval` 对象整体改名为 `toolApproval`(键内 `title/approve/deny/submitFailed/status.*/tools.*` 不变;`tools.*` 工具标签保留 org_*/group_create,workflow_* 在 PR4 加)。两 locale 同步。

- [ ] **Step 6: 前端全绿 + tsc + lint**
```bash
cd frontend && pnpm test 2>&1 | grep -E "Test Files|Tests "
pnpm exec tsc -b && echo TSC_OK
pnpm exec eslint src/components/agentre/tool-approval src/components/agentre/transcript-row-view.tsx src/components/agentre/transcript-rows.ts src/stores/chat-streams-store.ts && echo LINT_OK
cd .. && cd frontend && pnpm test -- src/__tests__/i18n.test.ts 2>&1 | grep -E "Tests "
```
Expected: 全绿。`card.test.tsx`(改名 `tool-approval/card.test.tsx`)与 `org-detail-agent.test.tsx` 内 `org_approval`/`OrgApproval` 断言同步改名后通过。i18n 无 `orgApproval` 残留引用、zh/en 对齐。

- [ ] **Step 7: 提交**
```bash
git add frontend/src frontend/wailsjs 2>/dev/null; git reset frontend/wailsjs   # wailsjs 是 gitignore,不提交
git add frontend/src
git commit -m "♻️ approval(web): OrgApprovalCard→ToolApprovalCard,统一 AnswerToolApproval,org_approval→tool_approval 全链改名"
```

---

## Task 8: 全栈回归校验

**Files:** 无(验证)

- [ ] **Step 1: 后端**
```bash
make test-backend 2>&1 | tail -10
make lint 2>&1 | tail -10
```
- [ ] **Step 2: 前端**
```bash
cd frontend && pnpm test 2>&1 | grep -E "Test Files|Tests "
pnpm exec tsc -b && pnpm lint 2>&1 | tail -5
```
- [ ] **Step 3: 残留扫描(必须为空/仅注释)**
```bash
cd /Users/codfrm/Code/agentre/agentre
grep -rn "OrgApprovalBlock\|BeginOrgApproval\|FinishOrgApproval\|AnswerOrgApproval\|AnswerGroupCreateApproval\|org_approval\|orgApproval\|OrgApprovalCard\|OrgApprovalData" internal frontend/src --include=*.go --include=*.ts --include=*.tsx | grep -v _test.go | grep -v "wailsjs"
```
Expected: 空(或仅历史注释)。`group_create` 作为 ToolName/ToolKey 字面量保留是对的。

- [ ] **Step 4: 真机冒烟(可选,按惯例)** `make dev`：给某 agent 开 org 工具,让它发起一次 `org_create_department`,确认审批卡照常弹出/批准/拒绝;group_create 建群审批照常。无回归即 PR2 完成。

- [ ] **Step 5: 收尾提交(若校验无改动则跳过)**

---

## Self-Review(对照 spec「审批管线」节)

- **block 泛化 + ToolKey**:Task 2 ✓
- **chat_svc 改名 + 上收 waiter + 单一 AnswerToolApproval(Begin 返回 channel)**:Task 3 ✓
- **orgtool / group_svc 删自持 waiter+Answer,走通用网关,带 ToolKey**:Task 4/5 ✓
- **App binding 合并 AnswerToolApproval**:Task 6 ✓
- **前端卡/路由/流/i18n 全链改名 + 统一 Answer**:Task 7 ✓
- **零回归护栏**:Task 1 基线 + 每包改完即跑 + Task 8 全栈 + 残留扫描 ✓
- **类型一致**:`ToolApprovalBlock{ToolKey,RequestID,ToolName,ToolInput,Status,Result}`、`Begin/Finish/AnswerToolApproval`、payload `kind:"tool_approval"`+`toolKey`、前端 `block.toolApproval`/`ToolApprovalCard`/`ToolApprovalData`、binding `AnswerToolApproval` —— 跨 task 命名统一 ✓
- **依赖顺序**:Task 2+3 合并提交(不可编译中间态);Task 4/5 各自包绿但全仓待 6;Task 6 全仓后端绿;Task 7 需先 `make generate` 拿新绑定 ✓
