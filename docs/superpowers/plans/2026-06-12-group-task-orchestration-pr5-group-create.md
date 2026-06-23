# 群任务卡编排 PR5:agent 拉起团队(`group_create`)实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 新 MCP tool `group_create(title, memberNames[], brief)` 注入普通单聊轮(群成员轮不注入,防套娃);调用走现有 org 工具同款审批门,用户批准后由发起 agent 当主持人建群、落「自会话拉起」system 消息、`brief` 作为首条群消息投主持人触发首轮;前端复用审批卡 + 新增「已创建群聊 →」跳转卡;e2e 走 fake runtime 全链路。

**Architecture:** 五段——①`chat_svc.turn_mcp` 从单 provider 扩成 provider 列表并透传 `sess.GroupID`(group provider 据此跳过 backing session);②`group_svc/mcp.go` 加 `create:<agentID>:<sessionID>` 第二种 token(同 secret 同验签)+ `group_create` tool 分支;③`group_svc` 服务层 `HandleGroupCreate` 镜像 orgtool 的 waiters 审批模式(Begin → 挂起 → Answer/超时 → 执行 `CreateGroup` + system 消息 + `SendGroupMessage(brief)` → Finish);④前端 `OrgApprovalCard` 按 toolName 路由应答 + canonical-tool registry 按 toolName 特判挂 `GroupCreateCard`;⑤fake runtime 加 `e2e-group-create:` 指令 + spec。

**Tech Stack:** Go 1.26(goconvey + gomock)、React 19 + Vitest、Playwright + `node:sqlite` oracle。

**Spec:** [2026-06-11-group-task-orchestration-design.md](../specs/2026-06-11-group-task-orchestration-design.md) §7.1、§10 PR5。

---

## 背景事实(执行前先读,全部已核实)

- **单聊 MCP 注入接缝**(`internal/service/chat_svc/turn_mcp.go`,共 26 行):
  `TurnMCPProvider func(ctx, a *agent_entity.Agent, sessionID int64) []agentruntime.MCPServerSpec`,
  **目前是单值变量**,bootstrap(`internal/bootstrap/cago.go:161`)只注册了
  `orgtool_svc.Default().BuildTurnMCP`。`appendTurnMCP` 在 `chat_svc/chat.go:2436` 被
  `runTurn` 调用(`sess` 变量在作用域内,`sess.GroupID > 0` 即群成员 backing session),
  capOK = `runner.Capabilities().Has(capability.CapMCPTools)` 已门控。
  既有测试 `turn_mcp_test.go` 用 `RegisterTurnMCPProvider(nil)` 清理——改为列表后这招失效,
  需配 `ResetTurnMCPProviders()`。
- **group MCP server**(`internal/service/group_svc/mcp.go`):无状态 HMAC token
  `b64url(payload).b64url(HMAC-SHA256(secret, payload))`,现 payload = `<groupID>:<memberID>`。
  `lookup` 先 `strings.Cut(tok, ".")` 验签再 `strings.Cut(payload, ":")` 解析两个 int64——
  `create:` 前缀的 payload 在现 lookup 里 ParseInt 失败天然返回 `!ok`(成员通道不会误收 create token),
  但反向(create 通道拒收成员 token)要靠显式前缀判断。tools/call 的 handler 全部走
  「`h.lookup` → `h.authorized`(memberCanPost)」,group_create 不能走这条鉴权链,要单独分支。
  测试模式:`mcp_task_internal_test.go` 的 `postMCP(h, tok, jsonBody)` helper(包内测试)。
- **orgtool 审批门**(照抄的样板):
  - `internal/service/orgtool_svc/approval.go` `handleWriteTool`:`uuid.NewString()` 做 requestID
    → `blocks.OrgApprovalBlock{RequestID, ToolName, ToolInput, Status: "pending"}` →
    `s.waiters.Store(requestID, ch)` → `s.approval.BeginOrgApproval(ctx, sessionID, blk)` →
    `select { case allow := <-ch / case <-time.After(s.approvalTimeout) / case <-r.Context().Done() }`
    → 拒绝/超时**返回文本 result 而非 RPC error**(`textResult("用户拒绝了此操作")` /
    `"审批超时，操作未执行"`),批准后执行并 `FinishOrgApproval(..., "approved", result)`。
  - `approvalTimeout = 4 * time.Minute`(orgtool.go:29,CLI 硬顶 ~285s 留 25s 余量)。
  - `AnswerOrgApproval`(approval.go:30)按 requestID 从 waiters 取 channel 发 allow;
    重复应答/未知 → InvalidParameter。
  - `blocks.OrgApprovalBlock`(`chat_svc/blocks/org_approval.go`)Type = `"org_approval"`,
    字段 RequestID/ToolName/ToolInput/Status/Result——**形状通用,不限 org 工具**,直接复用。
  - `BeginOrgApproval/FinishOrgApproval` 是 `chat_svc` 接口方法(chat.go:108-113),
    要求 session 有**活跃 turn**(MCP 调用发生在 turn 内,天然满足)。
  - 测试样板:`orgtool_svc/approval_test.go`(captureBegin 截 requestID → AnswerOrgApproval → 断言)。
- **group_svc 现状**:
  - `ChatGateway`(gateway.go)是窄接口(EnsureSession/Send/ObserveTurn/Stop/DeleteSession/
    AgentBackendHasCapability),mock 由 `//go:generate mockgen -source gateway.go` 生成。
  - `CreateGroup`(group.go:175)已做 host backendSupportsGroup 门控、部门从 host 派生、
    逐成员 8 人上限 + 门控、`ensureMember` 幂等;**成员参数是 `MemberAgentIDs []int64`**,
    名字解析要自己做(HandleInvite 的解析池 = `agent_repo.Agent().List(ctx)` 过滤 `IsActive()`)。
  - `SendGroupMessage`(group.go:460)自带 ingestMu、收件人为空默认投主持人、persistMessage、
    `enqueueDeliveries(..., "用户", 0)` + kick——**brief 投递直接复用它**(sender 显示为「用户」,
    溯源由 system 消息表达)。
  - `persistMessage` 签名:`(ctx, g, kind, senderMemberID, content, recipients, toUser, sourceMsgID, taskID, taskEvent)`;
    system 消息样板:`s.persistMessage(ctx, g, group_entity.SenderKindSystem, 0, "...", nil, false, 0, 0, "")`。
  - `newGroupSvc`(group.go:99)集中绑 mcp 回调(`s.mcp.ingest = ...`);`NewForTest(gw)` /
    `NewForTestWithNames(gw, names)` 构造测试实例(后者注入 names resolver,绕过 agent_repo)。
  - 测试里 repo mock 注册:`mock_group_repo` + `agent_repo.RegisterAgent(mock_agent_repo.NewMockAgentRepo(ctrl))`
    (task_test.go:26-37);chat session 用 `chat_repo.RegisterSession(mock_chat_repo.NewMockSessionRepo(ctrl))`
    (`internal/repository/chat_repo/session.go:63`,mock 已存在于 `mock_chat_repo/`)。
- **session 实体**:`chat_entity.Session.GroupID`(=0 普通单聊,>0 群 backing session)、
  `AgentID`、`ProjectID`、`Status`。`chat_repo.Session().Find(ctx, id)` 直接可用
  (group_svc 已有跨域 repo accessor 先例:agent_repo)。
- **错误码**:`internal/pkg/code/code.go` 19000 段(group),`zh_cn.go` **和 `en.go`** 都要加文案;
  用法 `i18n.NewError(ctx, code.Xxx)`。
- **前端**:
  - 审批卡 `frontend/src/components/agentre/org-approval/card.tsx`:props =
    `{approval: OrgApprovalData, sessionId}`,answer 调 wailsjs `AnswerOrgApproval`;
    toolName 显示走 `t("orgApproval.tools.<toolName>", {defaultValue: toolName})`;
    状态流转由 chat-streams-store 的 `appendLiveOrgApproval` / `markOrgApprovalResolved` 驱动;
    测试 `card.test.tsx` 在同目录。
  - tool 卡路由 `frontend/src/components/agentre/canonical-tool/registry.tsx`:
    `CanonicalToolRouter` 按 `block.canonical.kind`(后端产生)dispatch,MCP tool 无 canonical
    → `RawToolCard`。**MCP tool 的 `toolName` 实际形态是 `mcp__group__group_create`**
    (claude CLI 命名);fake/e2e 不产 tool block,跳转卡只能靠 Vitest 验。
  - 打开群 tab:`useChatTabsStore.getState().openGroup(groupId, title)`(chat-tabs-store.ts:139);
    侧栏刷新:`useGroupListStore.getState().reload()`(dedup 并发)。
  - wails 绑定生成:改了 `internal/app` 后要 `make generate`;前端测试 mock go 绑定走全局 alias,
    **wailsjs runtime 才需要 per-file vi.mock**(本计划只动 go 绑定)。
- **e2e**:fake runtime(`internal/pkg/agentruntime/runtimes/fake/runtime.go`,`//go:build e2e`)
  已有 `findGroupToolServer(req.MCPServers, tool)` + `postToolCall`(无状态 tools/call,带
  Authorization header);指令模式先例 `e2e-task:<assignee>:<title>`。种子(`e2e/fakes/install.go`)
  已有 `CEO 助手`(系统 agent)+ `E2E Member`。oracle `e2e/fixtures/db.ts`(node:sqlite 只读,
  一律增量断言)。fake 的 Go 单测:`go test -tags e2e -race ./internal/pkg/agentruntime/runtimes/fake`。
  `AGENTRE_PROXY_PORT=0` 已写死在 playwright.config.ts。
- **已知限制(随 org 工具,不在本期修)**:gateway URL 是本机回环,远程(daemon)会话注入的
  MCP server 不可达 → `group_create` 远程不可用,与 org 工具同口径。
- **分支**:从 `develop/group` 切 `feature/group-task-pr5`;gitmoji 提交。

---

### Task 0: 建分支

- [x] **Step 1: 切分支**

```bash
cd /Users/codfrm/Code/agentre/agentre
git checkout develop/group && git pull --ff-only 2>/dev/null; git checkout -b feature/group-task-pr5
```

---

### Task 1: chat_svc — TurnMCPProvider 多 provider + 透传 groupID

**Files:**
- Modify: `internal/service/chat_svc/turn_mcp.go`
- Modify: `internal/service/chat_svc/chat.go:2436`(appendTurnMCP 调用点)
- Modify: `internal/service/orgtool_svc/orgtool.go:53`(BuildTurnMCP 签名)
- Test: `internal/service/chat_svc/turn_mcp_test.go`、`internal/service/orgtool_svc/mcp_test.go:221+`

- [x] **Step 1: 写失败测试**

改写 `turn_mcp_test.go`(包内测试,沿用现有用例结构),覆盖:
①两个 provider 的返回按注册序拼接在 base 之后;②provider 收到 groupID 实参;③capOK=false 不追加;④Reset 清理。

```go
func TestAppendTurnMCP_MultiProvider(t *testing.T) {
	ResetTurnMCPProviders()
	defer ResetTurnMCPProviders()
	var gotGroupID int64
	RegisterTurnMCPProvider(func(_ context.Context, _ *agent_entity.Agent, _ int64, groupID int64) []agentruntime.MCPServerSpec {
		gotGroupID = groupID
		return []agentruntime.MCPServerSpec{{Name: "org"}}
	})
	RegisterTurnMCPProvider(func(_ context.Context, _ *agent_entity.Agent, _ int64, _ int64) []agentruntime.MCPServerSpec {
		return []agentruntime.MCPServerSpec{{Name: "group"}}
	})
	base := []agentruntime.MCPServerSpec{{Name: "base"}}
	out := appendTurnMCP(context.Background(), base, &agent_entity.Agent{}, 9, 5, true)
	if len(out) != 3 || out[0].Name != "base" || out[1].Name != "org" || out[2].Name != "group" {
		t.Fatalf("unexpected specs: %+v", out)
	}
	if gotGroupID != 5 {
		t.Fatalf("provider should receive groupID, got %d", gotGroupID)
	}
	if got := appendTurnMCP(context.Background(), base, &agent_entity.Agent{}, 9, 5, false); len(got) != 1 {
		t.Fatalf("capOK=false must not append, got %+v", got)
	}
}
```

既有用例里的 `RegisterTurnMCPProvider(nil)` 清理全部替换为 `ResetTurnMCPProviders()`,
provider 字面量补第 4 个参数。

- [x] **Step 2: 跑测试确认失败**

```bash
go test -race -run TestAppendTurnMCP ./internal/service/chat_svc/
```
预期:编译失败(`ResetTurnMCPProviders` 未定义 / 签名不符)——红在正确的地方。

- [x] **Step 3: 最小实现**

`turn_mcp.go` 改为:

```go
// TurnMCPProvider 按 (agent, session) 给 turn 注入额外 MCP server —— agent 级
// 内置工具体系的接缝。bootstrap 注册 orgtool_svc / group_svc 的实现;空列表 = 不注入。
// groupID 是 turn 所属 session 的 GroupID(>0 = 群成员 backing session),provider 据此
// 决定是否注入(如 group_create 不进群成员轮,防群中拉群套娃)。
// 与群聊的 extras.mcpServers 叠加;在 runTurn 单点生效,单聊/群聊/Regenerate 全覆盖。
type TurnMCPProvider func(ctx context.Context, a *agent_entity.Agent, sessionID, groupID int64) []agentruntime.MCPServerSpec

var turnMCPProviders []TurnMCPProvider

// RegisterTurnMCPProvider bootstrap 接线入口(可多次,按注册序拼接)。
func RegisterTurnMCPProvider(p TurnMCPProvider) { turnMCPProviders = append(turnMCPProviders, p) }

// ResetTurnMCPProviders 测试清理,防用例间串台。
func ResetTurnMCPProviders() { turnMCPProviders = nil }

// appendTurnMCP runTurn 在组装 RunRequest 时调用;capOK = runner 声明 CapMCPTools。
func appendTurnMCP(ctx context.Context, base []agentruntime.MCPServerSpec, a *agent_entity.Agent, sessionID, groupID int64, capOK bool) []agentruntime.MCPServerSpec {
	if !capOK {
		return base
	}
	for _, p := range turnMCPProviders {
		base = append(base, p(ctx, a, sessionID, groupID)...)
	}
	return base
}
```

`chat.go:2436` 调用点改为:

```go
MCPServers: appendTurnMCP(ctx, extras.mcpServers, a, sess.ID, sess.GroupID, runner.Capabilities().Has(capability.CapMCPTools)),
```

`orgtool_svc/orgtool.go` BuildTurnMCP 补参数(行为不变,org 工具继续在群轮也注入):

```go
func (s *orgtoolSvc) BuildTurnMCP(_ context.Context, a *agent_entity.Agent, sessionID int64, _ int64) []agentruntime.MCPServerSpec {
```

`orgtool_svc/mcp_test.go:221+` 的 `BuildTurnMCP(...)` 调用补第 4 个实参 `0`。
`bootstrap/cago.go:161` 不用动(函数值签名随之匹配)。

- [x] **Step 4: 跑测试确认通过**

```bash
go test -race ./internal/service/chat_svc/ ./internal/service/orgtool_svc/
```
预期:全 PASS。

- [x] **Step 5: 提交**

```bash
git add internal/service/chat_svc/turn_mcp.go internal/service/chat_svc/turn_mcp_test.go internal/service/chat_svc/chat.go internal/service/orgtool_svc/orgtool.go internal/service/orgtool_svc/mcp_test.go
git commit -m "✨ chat_svc: TurnMCPProvider 扩成 provider 列表并透传 groupID"
```

---

### Task 2: group_svc MCP 层 — create token + `group_create` tool 分支

**Files:**
- Modify: `internal/service/group_svc/mcp.go`
- Test: `internal/service/group_svc/mcp_create_internal_test.go`(新建,包内测试)

- [x] **Step 1: 写失败测试**

新建 `mcp_create_internal_test.go`(`postMCP` helper 在 `mcp_task_internal_test.go` 已有,直接用):

```go
package group_svc

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestGroupMCPCreateToken(t *testing.T) {
	Convey("MintCreateToken/lookupCreate 往返;成员 token 不被 create 通道接受、create token 不被成员通道接受", t, func() {
		h := newGroupMCP(nil)
		tok := h.MintCreateToken(7, 99)
		ref, ok := h.lookupCreate(tok)
		So(ok, ShouldBeTrue)
		So(ref.agentID, ShouldEqual, 7)
		So(ref.sessionID, ShouldEqual, 99)
		// 同 (agent, session) 确定性
		So(h.MintCreateToken(7, 99), ShouldEqual, tok)
		// 成员 token 进 create 通道 → 拒
		_, ok = h.lookupCreate(h.MintToken(5, 100))
		So(ok, ShouldBeFalse)
		// create token 进成员通道 → 拒
		_, ok = h.lookup(tok)
		So(ok, ShouldBeFalse)
	})
}

func TestGroupMCPGroupCreateTool(t *testing.T) {
	Convey("group_create → 回调收到 agentID/sessionID/title/memberNames/brief,响应回传回调 text", t, func() {
		var gotAgent, gotSession int64
		var gotTitle, gotBrief string
		var gotMembers []string
		h := newGroupMCP(nil)
		h.groupCreate = func(_ context.Context, agentID, sessionID int64, title string, memberNames []string, brief string) (string, error) {
			gotAgent, gotSession, gotTitle, gotMembers, gotBrief = agentID, sessionID, title, memberNames, brief
			return "group created: id=12 title=" + title, nil
		}
		tok := h.MintCreateToken(7, 99)
		rr := postMCP(h, tok, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"group_create","arguments":{"title":"新功能开发组","memberNames":["开发","测试"],"brief":"按设计稿重构 UI,验收:e2e 通过"}}}`)
		So(rr.Code, ShouldEqual, 200)
		So(gotAgent, ShouldEqual, 7)
		So(gotSession, ShouldEqual, 99)
		So(gotTitle, ShouldEqual, "新功能开发组")
		So(gotMembers, ShouldResemble, []string{"开发", "测试"})
		So(gotBrief, ShouldEqual, "按设计稿重构 UI,验收:e2e 通过")
		So(rr.Body.String(), ShouldContainSubstring, "group created: id=12")
	})

	Convey("成员 token 调 group_create → 401;create token 调 group_send → 401", t, func() {
		h := newGroupMCP(nil)
		h.groupCreate = func(context.Context, int64, int64, string, []string, string) (string, error) { return "", nil }
		rr := postMCP(h, h.MintToken(5, 100), `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"group_create","arguments":{"title":"x","memberNames":["a"],"brief":"b"}}}`)
		So(rr.Code, ShouldEqual, 401)
		rr = postMCP(h, h.MintCreateToken(7, 99), `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"group_send","arguments":{"body":"hi"}}}`)
		So(rr.Code, ShouldEqual, 401)
	})

	Convey("tools/list 含 group_create schema", t, func() {
		h := newGroupMCP(nil)
		rr := postMCP(h, "", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
		So(rr.Body.String(), ShouldContainSubstring, `"group_create"`)
	})
}
```

- [x] **Step 2: 跑测试确认失败**

```bash
go test -race -run 'TestGroupMCPCreateToken|TestGroupMCPGroupCreateTool' ./internal/service/group_svc/
```
预期:编译失败(`MintCreateToken`/`lookupCreate`/`groupCreate` 未定义)。

- [x] **Step 3: 最小实现**

`mcp.go` 增量(沿用既有注释风格):

```go
// createTokenPrefix 标记「单聊建群」token 的 payload 前缀,与成员 token(groupID:memberID)
// 共用同一 secret 与验签;两类 token 互不通行(group_create 只认 create token,群工具只认成员 token)。
const createTokenPrefix = "create:"

type createRef struct{ agentID, sessionID int64 }
```

`groupMCP` 结构体加回调字段(放在 taskCancel 之后):

```go
	// groupCreate 是 group_create tool 的回调:审批 + 建群在 svc 层完成,返回写回 CLI 的
	// result 文本(拒绝/超时也是文本,不走 RPC error,镜像 orgtool 审批语义)。
	groupCreate func(ctx context.Context, agentID, sessionID int64, title string, memberNames []string, brief string) (string, error)
```

token 函数(把验签从 lookup 里抽出共用):

```go
// MintCreateToken 为某 (agent, session) 签一个单聊建群 token(确定性,跨重启前提同 MintToken)。
func (h *groupMCP) MintCreateToken(agentID, sessionID int64) string {
	payload := createTokenPrefix + strconv.FormatInt(agentID, 10) + ":" + strconv.FormatInt(sessionID, 10)
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + h.sign(payload)
}

// verifyPayload 验签并还原 payload;签名不符 / 格式非法 → !ok。
func (h *groupMCP) verifyPayload(tok string) (string, bool) {
	payloadB64, sig, ok := strings.Cut(tok, ".")
	if !ok {
		return "", false
	}
	payload, err := base64.RawURLEncoding.DecodeString(payloadB64)
	if err != nil || !hmac.Equal([]byte(h.sign(string(payload))), []byte(sig)) {
		return "", false
	}
	return string(payload), true
}

// lookupCreate 验签并解出 create token 绑定的 (agent, session);成员 token → !ok。
func (h *groupMCP) lookupCreate(tok string) (createRef, bool) {
	payload, ok := h.verifyPayload(tok)
	if !ok {
		return createRef{}, false
	}
	rest, found := strings.CutPrefix(payload, createTokenPrefix)
	if !found {
		return createRef{}, false
	}
	aStr, sStr, ok := strings.Cut(rest, ":")
	if !ok {
		return createRef{}, false
	}
	agentID, err1 := strconv.ParseInt(aStr, 10, 64)
	sessionID, err2 := strconv.ParseInt(sStr, 10, 64)
	if err1 != nil || err2 != nil {
		return createRef{}, false
	}
	return createRef{agentID, sessionID}, true
}
```

`lookup` 改为基于 `verifyPayload` 并显式拒绝 create payload:

```go
func (h *groupMCP) lookup(tok string) (memberRef, bool) {
	payload, ok := h.verifyPayload(tok)
	if !ok || strings.HasPrefix(payload, createTokenPrefix) {
		return memberRef{}, false
	}
	gStr, mStr, ok := strings.Cut(payload, ":")
	...(其余不变)
}
```

`ServeHTTP`:Arguments 加 `MemberNames []string \`json:"memberNames"\``;
`tools/list` 列表追加 `groupCreateToolSchema()`;
`tools/call` 在 `ref, ok := h.lookup(...)` **之前**插入 group_create 专属分支:

```go
	case "tools/call":
		if rpc.Params.Name == "group_create" {
			cref, ok := h.lookupCreate(bearer(r))
			if !ok {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			if h.groupCreate == nil {
				writeRPCError(w, rpc.ID, -32000, "group create not wired")
				return
			}
			text, err := h.groupCreate(r.Context(), cref.agentID, cref.sessionID,
				rpc.Params.Arguments.Title, rpc.Params.Arguments.MemberNames, rpc.Params.Arguments.Brief)
			if err != nil {
				writeRPCError(w, rpc.ID, -32000, err.Error())
				return
			}
			writeRPCResult(w, rpc.ID, map[string]any{"content": []any{map[string]any{"type": "text", "text": text}}})
			return
		}
		ref, ok := h.lookup(bearer(r))
		...(既有逻辑不变)
```

schema(参照 spec §7.1:brief 必须完整转述,因单聊上下文不带进群;明示需用户审批):

```go
func groupCreateToolSchema() map[string]any {
	return map[string]any{
		"name":        "group_create",
		"description": "为一项需要多人协作的任务创建群聊并自任主持人(需用户在聊天里批准后才执行)。memberNames 填初始成员 agent 的显示名(可跨部门,后续也可在群内 group_invite 招募)。你的当前对话上下文不会带进群,brief 必须完整转述需求与验收标准——它会作为首条群消息发给你的群内分身,作为拆解任务的唯一依据。",
		"inputSchema": map[string]any{
			"type":     "object",
			"required": []string{"title", "memberNames", "brief"},
			"properties": map[string]any{
				"title":       map[string]any{"type": "string", "description": "群标题"},
				"memberNames": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "初始成员显示名(不含你自己;最多 7 个,主持人占 1 席)"},
				"brief":       map[string]any{"type": "string", "description": "完整需求转述 + 验收标准(首条群消息,拆任务的依据)"},
			},
		},
	}
}
```

- [x] **Step 4: 跑测试确认通过(守住既有 mcp 用例)**

```bash
go test -race ./internal/service/group_svc/
```
预期:全 PASS(既有 group_send/invite/task 的 token 用例不受影响)。

- [x] **Step 5: 提交**

```bash
git add internal/service/group_svc/mcp.go internal/service/group_svc/mcp_create_internal_test.go
git commit -m "✨ group_svc: group MCP 增 create token 与 group_create tool 分支"
```

---

### Task 3: group_svc 服务层 — HandleGroupCreate(审批门)+ AnswerGroupCreateApproval + BuildCreateTurnMCP

**Files:**
- Create: `internal/service/group_svc/create.go`
- Modify: `internal/service/group_svc/gateway.go`(ChatGateway 加审批两方法)
- Modify: `internal/service/group_svc/group.go`(groupSvc 字段 + newGroupSvc 接线)
- Modify: `internal/pkg/code/code.go` + `internal/pkg/code/zh_cn.go` + `internal/pkg/code/en.go`
- Test: `internal/service/group_svc/create_test.go`(新建)

- [x] **Step 1: 错误码先行(无测试,纯常量)**

`code.go` 19000 段 `GroupTaskSelfAssign` 之后追加:

```go
	GroupCreateSessionInvalid // group_create: 会话无效(不存在/已归档/不属于该 agent)
	GroupCreateNested         // group_create: 群成员轮内禁止再拉群(防套娃)
	GroupCreateMemberUnknown  // group_create: 成员名找不到对应可用 agent
```

`zh_cn.go`:

```go
	GroupCreateSessionInvalid: "会话无效,无法创建群聊",
	GroupCreateNested:         "群聊成员不能再创建群聊",
	GroupCreateMemberUnknown:  "找不到可用的成员: %v",
```

`en.go`(对应位置,与文件既有英文风格一致):

```go
	GroupCreateSessionInvalid: "invalid session, cannot create group",
	GroupCreateNested:         "group members cannot create another group",
	GroupCreateMemberUnknown:  "no available agent named: %v",
```

- [x] **Step 2: 写失败测试**

`gateway.go` 的 ChatGateway 先加方法(测试要用 mock;委托实现也一并写,见 Step 4):

```go
	// BeginGroupCreateApproval / FinishGroupCreateApproval 把 group_create 的审批卡
	// 路由到发起 agent 的单聊流(复用 chat_svc 的 org_approval block 管线,无新 UI 形态)。
	BeginGroupCreateApproval(ctx context.Context, sessionID int64, blk *chatblocks.OrgApprovalBlock) error
	FinishGroupCreateApproval(ctx context.Context, sessionID int64, requestID, status, result string) error
```

(import 加 `chatblocks "github.com/agentre-ai/agentre/internal/service/chat_svc/blocks"`。)
然后重生成 mock:

```bash
cd /Users/codfrm/Code/agentre/agentre && make mock
```

新建 `create_test.go`(外部测试包,样板 = `approval_test.go` + `task_test.go` 杂交):

```go
package group_svc_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/chat_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/group_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/capability"
	"github.com/agentre-ai/agentre/internal/pkg/consts"
	"github.com/agentre-ai/agentre/internal/repository/agent_repo"
	"github.com/agentre-ai/agentre/internal/repository/agent_repo/mock_agent_repo"
	"github.com/agentre-ai/agentre/internal/repository/chat_repo"
	"github.com/agentre-ai/agentre/internal/repository/chat_repo/mock_chat_repo"
	"github.com/agentre-ai/agentre/internal/repository/group_repo"
	"github.com/agentre-ai/agentre/internal/repository/group_repo/mock_group_repo"
	"github.com/agentre-ai/agentre/internal/service/chat_svc/blocks"
	"github.com/agentre-ai/agentre/internal/service/group_svc"
	"github.com/agentre-ai/agentre/internal/service/group_svc/mock_group_svc"
	. "github.com/smartystreets/goconvey/convey"
	"go.uber.org/mock/gomock"
)

// ※ 各 mock 的注册/构造按 task_test.go 的 registerTaskMocks 模式展开;
//   chat session 用 chat_repo.RegisterSession(mock_chat_repo.NewMockSessionRepo(ctrl))。

func TestHandleGroupCreate_ApprovedExecutes(t *testing.T) {
	Convey("批准 → 解析成员名建群(项目继承自发起会话)+ system 拉起消息 + brief 投主持人;result 文本含 group id", t, func() {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		gw := mock_group_svc.NewMockChatGateway(ctrl)

		sessRepo := mock_chat_repo.NewMockSessionRepo(ctrl)
		chat_repo.RegisterSession(sessRepo)
		agentRepo := mock_agent_repo.NewMockAgentRepo(ctrl)
		agent_repo.RegisterAgent(agentRepo)
		groupRepo := mock_group_repo.NewMockGroupRepo(ctrl)
		group_repo.RegisterGroup(groupRepo)
		memberRepo := mock_group_repo.NewMockGroupMemberRepo(ctrl)
		group_repo.RegisterMember(memberRepo)
		msgRepo := mock_group_repo.NewMockGroupMessageRepo(ctrl)
		group_repo.RegisterMessage(msgRepo)
		taskRepo := mock_group_repo.NewMockGroupTaskRepo(ctrl)
		group_repo.RegisterTask(taskRepo)

		// 发起会话:agent 7 的普通单聊(GroupID=0),项目 3
		sessRepo.EXPECT().Find(gomock.Any(), int64(99)).Return(&chat_entity.Session{
			ID: 99, AgentID: 7, ProjectID: 3, GroupID: 0, Status: consts.ACTIVE,
		}, nil)
		// 成员名解析池
		agentRepo.EXPECT().List(gomock.Any()).Return([]*agent_entity.Agent{
			{ID: 7, Name: "部门负责人", Status: consts.ACTIVE},
			{ID: 8, Name: "开发", Status: consts.ACTIVE},
		}, nil).AnyTimes()
		agentRepo.EXPECT().Find(gomock.Any(), gomock.Any()).Return(&agent_entity.Agent{ID: 7, Name: "部门负责人", Status: consts.ACTIVE}, nil).AnyTimes()
		// CreateGroup 路径(host + 1 成员;backendSupportsGroup 走 gw)
		gw.EXPECT().AgentBackendHasCapability(gomock.Any(), gomock.Any(), capability.CapMCPTools).Return(true, nil).AnyTimes()
		groupRepo.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, g *group_entity.Group) error {
			So(g.Title, ShouldEqual, "新功能开发组")
			So(g.HostAgentID, ShouldEqual, 7)
			So(g.ProjectID, ShouldEqual, 3) // 项目继承自发起会话
			g.ID = 12
			return nil
		})
		groupRepo.EXPECT().Find(gomock.Any(), int64(12)).Return(&group_entity.Group{ID: 12, HostAgentID: 7, Status: consts.ACTIVE}, nil).AnyTimes()
		groupRepo.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
		memberRepo.EXPECT().FindByGroupAndAgent(gomock.Any(), int64(12), gomock.Any()).Return(nil, nil).AnyTimes()
		var hostMemberID int64 = 0
		memberRepo.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, m *group_entity.GroupMember) error {
			m.ID = 100 + m.AgentID
			if m.Role == group_entity.RoleHost {
				hostMemberID = m.ID
			}
			return nil
		}).Times(2) // host + 开发
		memberRepo.EXPECT().ListByGroup(gomock.Any(), int64(12)).DoAndReturn(func(context.Context, int64) ([]*group_entity.GroupMember, error) {
			return []*group_entity.GroupMember{
				{ID: 107, GroupID: 12, AgentID: 7, Role: group_entity.RoleHost, Status: group_entity.MemberActive},
				{ID: 108, GroupID: 12, AgentID: 8, Status: group_entity.MemberActive},
			}, nil
		}).AnyTimes()
		taskRepo.EXPECT().ListByGroup(gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
		msgRepo.EXPECT().ListByGroup(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
		msgRepo.EXPECT().NextSeq(gomock.Any(), int64(12)).Return(int64(1), nil).AnyTimes()
		// 两条消息:system 拉起 + brief(user → host)
		var persisted []*group_entity.GroupMessage
		msgRepo.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, m *group_entity.GroupMessage) error {
			persisted = append(persisted, m)
			return nil
		}).Times(2)

		// 审批:截 Begin 的 requestID → goroutine 里 Answer(true)
		begun := make(chan string, 1)
		gw.EXPECT().BeginGroupCreateApproval(gomock.Any(), int64(99), gomock.Any()).DoAndReturn(
			func(_ context.Context, _ int64, blk *blocks.OrgApprovalBlock) error {
				begun <- blk.RequestID
				return nil
			})
		gw.EXPECT().FinishGroupCreateApproval(gomock.Any(), int64(99), gomock.Any(), "approved", gomock.Any()).Return(nil)

		svc := group_svc.NewForTestWithNames(gw, map[int64]string{7: "部门负责人", 8: "开发"})
		done := make(chan struct{})
		var text string
		var err error
		go func() {
			defer close(done)
			text, err = svc.HandleGroupCreate(context.Background(), 7, 99, "新功能开发组", []string{"开发"}, "按设计稿重构 UI,验收:e2e 通过")
		}()
		reqID := <-begun
		_, aerr := svc.AnswerGroupCreateApproval(context.Background(), &group_svc.AnswerGroupCreateApprovalRequest{SessionID: 99, RequestID: reqID, Allow: true})
		So(aerr, ShouldBeNil)
		<-done

		So(err, ShouldBeNil)
		So(text, ShouldContainSubstring, "group created: id=12")
		So(text, ShouldContainSubstring, "新功能开发组")
		So(hostMemberID, ShouldNotEqual, 0)
		So(len(persisted), ShouldEqual, 2)
		So(persisted[0].SenderKind, ShouldEqual, group_entity.SenderKindSystem)
		So(persisted[0].Content, ShouldContainSubstring, "部门负责人")
		So(persisted[1].SenderKind, ShouldEqual, group_entity.SenderKindUser)
		So(persisted[1].Content, ShouldContainSubstring, "按设计稿重构 UI")
	})
}

func TestHandleGroupCreate_Denied(t *testing.T) {
	Convey("拒绝 → 不建群(无 group Create),返回拒绝文案,Finish(denied)", t, func() {
		// mock 装配同上,但 groupRepo.Create / msgRepo.Create 一律不 EXPECT(调用即 fail)
		// gw.EXPECT().FinishGroupCreateApproval(..., "denied", "") 一次
		// Answer(allow=false) 后断言 text 含 "用户拒绝",err == nil
	})
}

func TestHandleGroupCreate_Timeout(t *testing.T) {
	Convey("超时 → Finish(expired),返回超时文案", t, func() {
		// svc 用 NewForTestWithNames 后通过 group_svc.SetApprovalTimeoutForTest(svc, 50*time.Millisecond)
		// (create.go 提供测试钩子) 不 Answer,直接等 HandleGroupCreate 返回
		// 断言 text 含 "审批超时",gw.EXPECT().FinishGroupCreateApproval(..., "expired", "")
	})
}

func TestHandleGroupCreate_Guards(t *testing.T) {
	Convey("群成员轮(session.GroupID>0)→ GroupCreateNested,且不 Begin", t, func() {
		// sessRepo.Find 返回 GroupID: 5 的 session → err 含「群聊成员不能再创建群聊」
		// gw 上没有任何 EXPECT(Begin 调用即 fail)
	})
	Convey("会话不存在 / agent 不匹配 → GroupCreateSessionInvalid", t, func() {
		// sessRepo.Find 返回 nil → err;再 Find 返回 AgentID 8 ≠ 7 → err
	})
	Convey("成员名解析不到 → GroupCreateMemberUnknown,且不 Begin", t, func() {
		// agentRepo.List 池里没有「测试」→ HandleGroupCreate(..., []string{"测试"}, ...) → err 含「测试」
	})
}

func TestBuildCreateTurnMCP(t *testing.T) {
	Convey("普通单聊 → 注入 group server 只带 group_create;token 经 lookupCreate 可验", t, func() {
		// svc.SetGatewayBaseURL("http://127.0.0.1:1") 后:
		// specs := svc.BuildCreateTurnMCP(ctx, &agent_entity.Agent{ID: 7}, 99, 0)
		// So(specs, ShouldHaveLength, 1); Tools == []string{"group_create"};URL 以 /mcp/group/ 结尾
	})
	Convey("群成员轮(groupID>0)/ a==nil / baseURL 空 → 不注入", t, func() {
		// 三种情形分别返回 nil
	})
}
```

(`Denied/Timeout/Guards/BuildCreateTurnMCP` 四个用例按注释展开成完整 mock 装配——结构与
Approved 用例相同,只是 EXPECT 集不同;执行者照 `orgtool_svc/approval_test.go` 的
`TestOrgApproval_Denied` / `TestOrgApproval_Timeout` 抄等价结构。)

- [x] **Step 3: 跑测试确认失败**

```bash
go test -race -run 'TestHandleGroupCreate|TestBuildCreateTurnMCP' ./internal/service/group_svc/
```
预期:编译失败(`HandleGroupCreate` 等未定义)。

- [x] **Step 4: 最小实现**

`gateway.go` 委托实现追加:

```go
func (chatSvcGateway) BeginGroupCreateApproval(ctx context.Context, sessionID int64, blk *chatblocks.OrgApprovalBlock) error {
	return chat_svc.Chat().BeginOrgApproval(ctx, sessionID, blk)
}

func (chatSvcGateway) FinishGroupCreateApproval(ctx context.Context, sessionID int64, requestID, status, result string) error {
	return chat_svc.Chat().FinishOrgApproval(ctx, sessionID, requestID, status, result)
}
```

`group.go` 的 groupSvc 结构体加字段:

```go
	createWaiters   sync.Map      // requestID(string) → chan bool;挂起的 group_create 等审批决议
	approvalTimeout time.Duration // group_create 审批超时(默认 4min,对齐 orgtool 的 CLI 硬顶余量)
```

`newGroupSvc` 里初始化 + 接线(放在 `s.mcp.authz = ...` 之前):

```go
	s.approvalTimeout = 4 * time.Minute
	// group_create:单聊轮经审批门拉起团队(spec §7.1)。
	s.mcp.groupCreate = s.HandleGroupCreate
```

新建 `create.go`:

```go
package group_svc

import (
	"context"
	"fmt"
	"time"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
	"github.com/agentre-ai/agentre/internal/pkg/code"
	"github.com/agentre-ai/agentre/internal/pkg/consts"
	"github.com/agentre-ai/agentre/internal/repository/agent_repo"
	"github.com/agentre-ai/agentre/internal/repository/chat_repo"
	chatblocks "github.com/agentre-ai/agentre/internal/service/chat_svc/blocks"
	"github.com/cago-frame/cago/pkg/i18n"
	"github.com/cago-frame/cago/pkg/logger"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// BuildCreateTurnMCP 实现 chat_svc.TurnMCPProvider:给普通单聊轮注入 group_create。
// 群成员轮(groupID>0)不注入 —— 防群中拉群套娃(spec §7.1);能力门控(CapMCPTools)
// 由 chat_svc.appendTurnMCP 统一处理,这里不重复判。
func (s *groupSvc) BuildCreateTurnMCP(_ context.Context, a *agent_entity.Agent, sessionID, groupID int64) []agentruntime.MCPServerSpec {
	if a == nil || groupID > 0 || s.gatewayBaseURL == "" {
		return nil
	}
	return []agentruntime.MCPServerSpec{{
		Name:    "group",
		URL:     s.gatewayBaseURL + "/mcp/group/",
		Headers: map[string]string{"Authorization": "Bearer " + s.mcp.MintCreateToken(a.ID, sessionID)},
		Tools:   []string{"group_create"},
	}}
}

// HandleGroupCreate 是 group_create MCP tool 的服务端入口:校验发起会话 → 审批门挂起 →
// 批准后建群(发起者=主持人,项目继承发起会话)+ system 拉起消息 + brief 作为首条群消息
// 投主持人触发首轮。返回写回 CLI 的 result 文本;拒绝/超时也是文本(nil err),镜像 orgtool。
func (s *groupSvc) HandleGroupCreate(ctx context.Context, agentID, sessionID int64, title string, memberNames []string, brief string) (string, error) {
	// 按 DB 现状校验发起会话(token 无状态,签发后会话可能已归档/换 agent)。
	sess, err := chat_repo.Session().Find(ctx, sessionID)
	if err != nil {
		return "", err
	}
	if sess == nil || sess.Status != consts.ACTIVE || sess.AgentID != agentID {
		return "", i18n.NewError(ctx, code.GroupCreateSessionInvalid)
	}
	if sess.GroupID > 0 { // 群成员轮内禁止再拉群(防套娃);正常注入下走不到,防御伪造 token 场景
		return "", i18n.NewError(ctx, code.GroupCreateNested)
	}
	memberIDs, err := s.resolveCreateMembers(ctx, agentID, memberNames)
	if err != nil {
		return "", err
	}

	// 审批门:挂起当前 MCP 调用直至用户决议/超时(复用 org_approval block 管线)。
	requestID := uuid.NewString()
	blk := &chatblocks.OrgApprovalBlock{RequestID: requestID, ToolName: "group_create",
		ToolInput: map[string]any{"title": title, "memberNames": memberNames, "brief": brief}, Status: "pending"}
	ch := make(chan bool, 1)
	s.createWaiters.Store(requestID, ch)
	defer s.createWaiters.Delete(requestID)
	if err := s.gw.BeginGroupCreateApproval(ctx, sessionID, blk); err != nil {
		return "", fmt.Errorf("审批通道不可用: %w", err)
	}
	select {
	case allow := <-ch:
		if !allow {
			_ = s.gw.FinishGroupCreateApproval(ctx, sessionID, requestID, "denied", "")
			return "用户拒绝了此操作", nil
		}
	case <-time.After(s.approvalTimeout):
		_ = s.gw.FinishGroupCreateApproval(ctx, sessionID, requestID, "expired", "")
		return "审批超时，操作未执行", nil
	case <-ctx.Done():
		_ = s.gw.FinishGroupCreateApproval(context.Background(), sessionID, requestID, "expired", "")
		return "", ctx.Err()
	}

	detail, err := s.CreateGroup(ctx, &CreateGroupRequest{
		Title:          title,
		HostAgentID:    agentID,
		ProjectID:      sess.ProjectID, // 群目录 = 发起会话的项目目录
		MemberAgentIDs: memberIDs,
	})
	if err != nil {
		_ = s.gw.FinishGroupCreateApproval(ctx, sessionID, requestID, "approved", "执行失败: "+err.Error())
		return "已批准但执行失败: " + err.Error(), nil
	}
	g := detail.Group
	if _, perr := s.persistMessage(ctx, g, group_entity.SenderKindSystem, 0,
		"本群由 "+s.names(ctx, agentID)+" 自会话拉起", nil, false, 0, 0, ""); perr != nil {
		logger.Ctx(ctx).Warn("group_svc.HandleGroupCreate: system message persist failed", zap.Error(perr))
	}
	// brief 作为首条群消息投主持人(收件人为空默认主持人),触发其群内首轮。
	if serr := s.SendGroupMessage(ctx, &SendGroupMessageRequest{GroupID: g.ID, Text: brief}); serr != nil {
		logger.Ctx(ctx).Warn("group_svc.HandleGroupCreate: brief send failed", zap.Error(serr))
	}
	result := fmt.Sprintf("group created: id=%d title=%s", g.ID, g.Title)
	_ = s.gw.FinishGroupCreateApproval(ctx, sessionID, requestID, "approved", result)
	logger.Ctx(ctx).Info("group_svc.HandleGroupCreate: created",
		zap.Int64("groupID", g.ID), zap.Int64("hostAgentID", agentID), zap.Int64("sessionID", sessionID))
	return result, nil
}

// resolveCreateMembers 把成员显示名解析成 agent id(池=全部 active agent,与 invite 同口径;
// 名字找不到 → 显式报错,不静默跳过 —— 自主建群必须让模型知道谁没拉到)。
func (s *groupSvc) resolveCreateMembers(ctx context.Context, hostAgentID int64, names []string) ([]int64, error) {
	pool, err := agent_repo.Agent().List(ctx)
	if err != nil {
		return nil, err
	}
	byName := map[string]int64{}
	for _, a := range pool {
		if a.IsActive() {
			byName[a.Name] = a.ID
		}
	}
	out := make([]int64, 0, len(names))
	for _, n := range names {
		id, ok := byName[n]
		if !ok {
			return nil, i18n.NewError(ctx, code.GroupCreateMemberUnknown, n)
		}
		if id == hostAgentID {
			continue // 主持人无需自列,CreateGroup 也会跳过
		}
		out = append(out, id)
	}
	return out, nil
}

// AnswerGroupCreateApprovalRequest 前端审批入口(wails binding)。
type AnswerGroupCreateApprovalRequest struct {
	SessionID int64  `json:"sessionId"`
	RequestID string `json:"requestId"`
	Allow     bool   `json:"allow"`
}

// AnswerGroupCreateApprovalResponse 应答返回(无字段)。
type AnswerGroupCreateApprovalResponse struct{}

// AnswerGroupCreateApproval 唤醒挂起的 group_create 调用。重复应答/已超时/未知 → InvalidParameter。
func (s *groupSvc) AnswerGroupCreateApproval(ctx context.Context, req *AnswerGroupCreateApprovalRequest) (*AnswerGroupCreateApprovalResponse, error) {
	v, ok := s.createWaiters.LoadAndDelete(req.RequestID)
	if !ok {
		return nil, i18n.NewError(ctx, code.InvalidParameter)
	}
	v.(chan bool) <- req.Allow
	return &AnswerGroupCreateApprovalResponse{}, nil
}

// SetApprovalTimeoutForTest 测试钩子:缩短审批超时。
func SetApprovalTimeoutForTest(svc GroupSvc, d time.Duration) {
	if s, ok := svc.(*groupSvc); ok {
		s.approvalTimeout = d
	}
}
```

注意:
- `group_entity` import 按需补;`s.names` 是 groupSvc 既有的 name resolver 字段。
- `GroupSvc` 接口(group.go:46)加 `HandleGroupCreate` / `AnswerGroupCreateApproval` /
  `BuildCreateTurnMCP` 三个方法声明。
- `code.GroupCreateMemberUnknown` 文案带 `%v`,`i18n.NewError(ctx, code, n)` 传名字
  (与文件内既有带参错误码用法一致;若 cago i18n 不支持参数,改为错误文案后拼
  `+ ": " + n` 的 fmt.Errorf 包装,执行时以编译/现有用法为准)。

- [x] **Step 5: 跑测试确认通过(全包)**

```bash
go test -race ./internal/service/group_svc/ ./internal/pkg/code/
```
预期:全 PASS。

- [x] **Step 6: 提交**

```bash
git add internal/service/group_svc/ internal/pkg/code/
git commit -m "✨ group_svc: HandleGroupCreate 审批门建群 + BuildCreateTurnMCP 单聊注入"
```

---

### Task 4: 接线 — bootstrap provider 注册 + wails binding

**Files:**
- Modify: `internal/bootstrap/cago.go:161` 附近
- Modify: `internal/app/group.go`
- Generated: `frontend/wailsjs/`(make generate)

- [x] **Step 1: bootstrap 注册 group provider**

`cago.go:161` 后追加一行:

```go
	chat_svc.RegisterTurnMCPProvider(orgtool_svc.Default().BuildTurnMCP)
	// group_create:单聊轮注入(群成员轮在 provider 内按 groupID 跳过)。
	chat_svc.RegisterTurnMCPProvider(group_svc.Default().BuildCreateTurnMCP)
```

- [x] **Step 2: wails binding**

`internal/app/group.go` 追加(binding 只做 parse → svc → return):

```go
// AnswerGroupCreateApproval group_create 拉起团队的审批决策(批准/拒绝)。
func (a *App) AnswerGroupCreateApproval(req *group_svc.AnswerGroupCreateApprovalRequest) (*group_svc.AnswerGroupCreateApprovalResponse, error) {
	return group_svc.Default().AnswerGroupCreateApproval(a.ctx, req)
}
```

- [x] **Step 3: 生成绑定 + 后端全量测试**

```bash
make generate && make test-backend
```
预期:`frontend/wailsjs/go/app/App.js|d.ts` 出现 `AnswerGroupCreateApproval`;后端测试全 PASS。

- [x] **Step 4: 提交**

```bash
git add internal/bootstrap/cago.go internal/app/group.go frontend/wailsjs/
git commit -m "🔌 app: group_create 审批 binding + 单聊 MCP provider 接线"
```

---

### Task 5: 前端 — 审批卡按 toolName 路由 + 批准后刷新群列表 + i18n

**Files:**
- Modify: `frontend/src/components/agentre/org-approval/card.tsx`
- Modify: `frontend/src/i18n/locales/zh-CN/common.json` + `frontend/src/i18n/locales/en/common.json`
- Test: `frontend/src/components/agentre/org-approval/card.test.tsx`

- [x] **Step 1: 写失败测试**

`card.test.tsx` 追加(沿用文件内 `pending()` 工厂与既有 mock 方式;wailsjs go 绑定 mock 走
全局 alias,确认文件头部现有 `vi.mock`/import 形态后同样处理 `AnswerGroupCreateApproval`
与 `useGroupListStore`):

```tsx
const groupCreatePending = (overrides: Partial<OrgApprovalData> = {}): OrgApprovalData => ({
  requestId: "gc-1",
  toolName: "group_create",
  toolInput: { title: "新功能开发组", memberNames: ["开发"], brief: "按设计稿重构" },
  status: "pending",
  ...overrides,
});

it("routes group_create answers to AnswerGroupCreateApproval (not AnswerOrgApproval)", async () => {
  render(<OrgApprovalCard approval={groupCreatePending()} sessionId={42} />);
  fireEvent.click(screen.getByText("Approve"));
  await waitFor(() => expect(mockAnswerGroupCreateApproval).toHaveBeenCalledTimes(1));
  expect(mockAnswerGroupCreateApproval.mock.calls[0][0]).toMatchObject({
    sessionId: 42, requestId: "gc-1", allow: true,
  });
  expect(mockAnswerOrgApproval).not.toHaveBeenCalled();
});

it("reloads the group list when a group_create approval resolves approved", () => {
  render(<OrgApprovalCard approval={groupCreatePending({ status: "approved", result: "group created: id=12 title=新功能开发组" })} sessionId={42} />);
  expect(mockGroupListReload).toHaveBeenCalled();
});

it("shows the i18n label for group_create", () => {
  render(<OrgApprovalCard approval={groupCreatePending()} sessionId={42} />);
  expect(screen.getByText("Create group chat")).toBeDefined();
});
```

- [x] **Step 2: 跑测试确认失败**

```bash
cd frontend && pnpm test -- src/components/agentre/org-approval/card.test.tsx
```
预期:FAIL(路由/刷新/文案均未实现)。

- [x] **Step 3: 最小实现**

`card.tsx`:

```tsx
import { AnswerGroupCreateApproval, AnswerOrgApproval } from "@/wailsjs/go/app/App"; // 路径按文件现有 import 改
import { group_svc, orgtool_svc } from "@/wailsjs/go/models";
import { useGroupListStore } from "@/stores/group-list-store";

// answer 内按 toolName 分流:
const answer = async (allow: boolean) => {
  ...
  if (approval.toolName === "group_create") {
    await AnswerGroupCreateApproval(
      group_svc.AnswerGroupCreateApprovalRequest.createFrom({
        sessionId, requestId: approval.requestId, allow,
      }),
    );
  } else {
    await AnswerOrgApproval(/* 原样 */);
  }
  ...
};

// 批准落地后侧栏要立刻看到新群(fake/e2e 不产 tool block,刷新只能挂在审批卡上):
useEffect(() => {
  if (approval.toolName === "group_create" && approval.status === "approved") {
    void useGroupListStore.getState().reload(); // reload 自带并发去重,历史卡重挂载多刷一次无害
  }
}, [approval.toolName, approval.status]);
```

i18n 两语言:

```jsonc
// zh-CN/common.json → orgApproval.tools 下
"group_create": "创建群聊"
// en/common.json → orgApproval.tools 下
"group_create": "Create group chat"
```

- [x] **Step 4: 跑测试确认通过(含 i18n 覆盖测试)**

```bash
cd frontend && pnpm test -- src/components/agentre/org-approval/card.test.tsx src/__tests__/i18n.test.ts
```
预期:PASS。

- [x] **Step 5: 提交**

```bash
git add frontend/src/components/agentre/org-approval/ frontend/src/i18n/
git commit -m "✨ frontend: 审批卡支持 group_create(应答路由+批准后刷新群列表+i18n)"
```

---

### Task 6: 前端 — 「已创建群聊 →」跳转卡

**Files:**
- Create: `frontend/src/components/agentre/canonical-tool/group-create/card.tsx`
- Modify: `frontend/src/components/agentre/canonical-tool/registry.tsx`
- Modify: i18n 两语言
- Test: `frontend/src/components/agentre/canonical-tool/group-create/card.test.tsx`

- [x] **Step 1: 写失败测试**

```tsx
// card.test.tsx(toolBlock/resultBlock 工厂参照 canonical-tool/raw/card.test.tsx)
const use = {
  type: "tool_use",
  toolName: "mcp__group__group_create",
  toolInput: { title: "新功能开发组", memberNames: ["开发"], brief: "..." },
} as unknown as ChatBlockData;
const result = (text: string) => ({ type: "tool_result", text }) as unknown as ChatBlockData;

it("renders the jump card and opens the group tab on click", () => {
  render(<GroupCreateCard toolBlock={use} resultBlock={result("group created: id=12 title=新功能开发组")} />);
  expect(screen.getByText("Group chat created")).toBeDefined();
  expect(screen.getByText("新功能开发组")).toBeDefined();
  fireEvent.click(screen.getByRole("button"));
  expect(mockOpenGroup).toHaveBeenCalledWith(12, "新功能开发组");
});

it("falls back to RawToolCard while pending or when the result is not parseable", () => {
  // resultBlock undefined → 不渲染跳转按钮(断言无 button);
  // result("用户拒绝了此操作") → 同样回退
});

// registry.test.tsx 或本文件内:router 对 mcp__group__group_create 选中 GroupCreateCard
it("CanonicalToolRouter dispatches mcp__*__group_create to GroupCreateCard", () => {
  render(<CanonicalToolRouter toolBlock={use} resultBlock={result("group created: id=12 title=x")} />);
  expect(screen.getByText("Group chat created")).toBeDefined();
});
```

- [x] **Step 2: 跑测试确认失败**

```bash
cd frontend && pnpm test -- src/components/agentre/canonical-tool/group-create/card.test.tsx
```
预期:FAIL(组件不存在)。

- [x] **Step 3: 最小实现**

`group-create/card.tsx`(shadcn 组件 + i18n;样式对齐 canonical-tool 既有卡片的紧凑行风格):

```tsx
import { useTranslation } from "react-i18next";
import { Users, ArrowRight } from "lucide-react";
import { Button } from "@/components/ui/button";
import { useChatTabsStore } from "@/stores/chat-tabs-store";
import { RawToolCard } from "../raw/card";
import type { CanonicalCardProps } from "../types"; // 按 registry 现有类型 import 调整

const RESULT_RE = /group created: id=(\d+) title=(.*)$/;

export function parseGroupCreateResult(text: string | undefined): { id: number; title: string } | null {
  const m = text?.match(RESULT_RE);
  return m ? { id: Number(m[1]), title: m[2] } : null;
}

export const GroupCreateCard: React.FC<CanonicalCardProps> = (props) => {
  const { t } = useTranslation();
  const parsed = parseGroupCreateResult((props.resultBlock as { text?: string } | undefined)?.text);
  if (!parsed) return <RawToolCard {...props} />; // pending / 拒绝 / 超时 / 执行失败 → 原样
  return (
    <div className="..."> {/* 头部条:Users 图标 + t("groupCreateCard.created") + 群标题 */}
      <Users className="..." />
      <span>{t("groupCreateCard.created")}</span>
      <span className="...">{parsed.title}</span>
      <Button variant="ghost" size="sm"
        onClick={() => useChatTabsStore.getState().openGroup(parsed.id, parsed.title)}>
        {t("groupCreateCard.open")} <ArrowRight className="..." />
      </Button>
    </div>
  );
};
```

`registry.tsx` 的 `CanonicalToolRouter`:

```tsx
const GROUP_CREATE_RE = /^(mcp__.+__)?group_create$/;

export function CanonicalToolRouter(props: CanonicalCardProps) {
  const canonical = (props.toolBlock as { canonical?: CanonicalDTO }).canonical;
  if (!canonical) {
    const toolName = (props.toolBlock as { toolName?: string }).toolName ?? "";
    if (GROUP_CREATE_RE.test(toolName)) return <GroupCreateCard {...props} />;
    return <RawToolCard {...props} />;
  }
  ...
}
```

i18n:

```jsonc
// zh-CN: "groupCreateCard": { "created": "已创建群聊", "open": "打开群聊" }
// en:    "groupCreateCard": { "created": "Group chat created", "open": "Open group" }
```

- [x] **Step 4: 跑前端全量测试**

```bash
cd frontend && pnpm test
```
预期:全 PASS(全量跑,防 per-file wailsjs mock 漏配,见仓库测试规约)。

- [x] **Step 5: 提交**

```bash
git add frontend/src/components/agentre/canonical-tool/ frontend/src/i18n/
git commit -m "✨ frontend: group_create 跳转卡(已创建群聊 → 打开群聊)"
```

---

### Task 7: e2e — fake 指令 + 全链路 spec

**Files:**
- Modify: `internal/pkg/agentruntime/runtimes/fake/runtime.go`
- Test: `internal/pkg/agentruntime/runtimes/fake/runtime_test.go`(已有,追加用例)
- Modify: `e2e/fixtures/db.ts`
- Create: `e2e/tests/group-create.spec.ts`

- [x] **Step 1: fake 指令解析(先红后绿)**

`runtime_test.go` 追加:

```go
func TestParseGroupCreateDirective(t *testing.T) {
	title, members, brief, ok := parseGroupCreateDirective("e2e-group-create:拉起群:E2E Member:e2e-brief 建群冒烟")
	if !ok || title != "拉起群" || len(members) != 1 || members[0] != "E2E Member" || brief != "e2e-brief 建群冒烟" {
		t.Fatalf("parse failed: %q %v %q %v", title, members, brief, ok)
	}
	if _, _, _, ok := parseGroupCreateDirective("无指令文本"); ok {
		t.Fatal("should not match")
	}
}
```

```bash
go test -tags e2e -race -run TestParseGroupCreateDirective ./internal/pkg/agentruntime/runtimes/fake
```
预期:编译失败 → 实现后 PASS。实现(`runtime.go`):

```go
// GroupCreateDirectivePrefix 触发单聊建群的用户指令:
// e2e-group-create:<title>:<成员名逗号分隔>:<brief>。
const GroupCreateDirectivePrefix = "e2e-group-create:"

// parseGroupCreateDirective 解析建群指令(取指令所在行;三段冒号分隔,成员逗号分隔)。
func parseGroupCreateDirective(text string) (title string, members []string, brief string, ok bool) {
	idx := strings.Index(text, GroupCreateDirectivePrefix)
	if idx < 0 {
		return "", nil, "", false
	}
	rest := text[idx+len(GroupCreateDirectivePrefix):]
	if i := strings.IndexByte(rest, '\n'); i >= 0 {
		rest = rest[:i]
	}
	parts := strings.SplitN(rest, ":", 3)
	if len(parts) != 3 {
		return "", nil, "", false
	}
	title, brief = strings.TrimSpace(parts[0]), strings.TrimSpace(parts[2])
	for _, m := range strings.Split(parts[1], ",") {
		if m = strings.TrimSpace(m); m != "" {
			members = append(members, m)
		}
	}
	if title == "" || len(members) == 0 || brief == "" {
		return "", nil, "", false
	}
	return title, members, brief, true
}
```

`Run` 的 goroutine 里、task 接缝之后追加(挂起等审批是预期行为,postToolCall 用 run ctx):

```go
			// 建群接缝(spec §7.1):单聊注入 group_create 时,按指令调 tool;
			// 该调用会挂起直到用户在 UI 批准(e2e spec 负责点批准),失败只写 stderr。
			if spec, ok := findGroupToolServer(req.MCPServers, "group_create"); ok {
				if title, members, brief, found := parseGroupCreateDirective(req.UserText); found {
					if err := postToolCall(ctx, spec, "group_create", map[string]any{
						"title": title, "memberNames": members, "brief": brief,
					}); err != nil {
						fmt.Fprintf(os.Stderr, "fake: group_create failed: %v\n", err)
					}
				}
			}
```

- [x] **Step 2: DB oracle 扩展**

`e2e/fixtures/db.ts` 追加(沿用文件内 node:sqlite 只读模式):

```ts
/** groups 表中指定标题的群数量(group_create 链路 oracle)。 */
export function groupCountByTitle(title: string): number { /* SELECT COUNT(*) FROM groups WHERE title = ? */ }

/** 某群的 active 成员数。 */
export function groupMemberCountByTitle(title: string): number { /* JOIN groups → group_members WHERE status='active' */ }

/** 某群 group_messages 中含给定子串的行数(验 system 拉起消息 / brief 投递)。 */
export function groupMessageCountByTitleAndContent(title: string, contentLike: string): number {}
```

- [x] **Step 3: 写 spec(先红:在实现已齐的此刻应直接绿;若红,红因必须可解释)**

`e2e/tests/group-create.spec.ts` 骨架(单聊打开方式抄 `smoke-chat.spec.ts`,增量断言抄 `group-task.spec.ts`):

```ts
test("agent 自会话拉起团队:指令 → 审批卡 → 批准 → 新群 + brief 首轮", async ({ page }) => {
  const TITLE = `e2e拉起群-${Date.now()}`;
  const baseGroups = groupCountByTitle(TITLE); // 0,但仍按增量纪律

  // 1. 打开 CEO 助手 单聊,发送建群指令
  //    `e2e-group-create:${TITLE}:E2E Member:e2e-brief 建群冒烟`
  // 2. 等审批卡出现(toolName label「创建群聊」),点「批准/Approve」按钮
  //    (按钮文案以 zh-CN locale 实际 key 为准,执行时 grep orgApproval 确认)
  // 3. 断言:侧栏出现 TITLE 群(审批卡 approved 后自动 reload)
  // 4. DB oracle:
  //    groupCountByTitle(TITLE) === baseGroups + 1
  //    groupMemberCountByTitle(TITLE) === 2          // CEO 助手(host) + E2E Member
  //    groupMessageCountByTitleAndContent(TITLE, "自会话拉起") === 1
  //    groupMessageCountByTitleAndContent(TITLE, "e2e-brief 建群冒烟") >= 1  // brief 进群
  // 5. 点开群,group-scroll 里最终出现 e2e-fake-reply:(主持人首轮跑完冒泡)——
  //    fake 主持人轮会对 brief 回 group_send(mentions=["用户"]),自然收敛
});
```

- [x] **Step 4: 跑单 spec 转绿 + 全量 e2e 守既有用例**

```bash
cd e2e && pnpm test -- tests/group-create.spec.ts
make e2e
```
预期:新 spec PASS;既有 4 个 spec 不受影响(单聊轮多注入了一个 group server,
fake 对无指令文本不动作;smoke-chat 等不受干扰)。

- [x] **Step 5: 提交**

```bash
git add internal/pkg/agentruntime/runtimes/fake/ e2e/
git commit -m "✨ e2e: group_create 全链路 spec(fake 指令建群 + 审批 + DB oracle)"
```

---

### Task 8: 全量验证 + 收尾

- [x] **Step 1: 全量检查**

```bash
make test-backend && make lint
cd frontend && pnpm test
```
预期:全部 PASS;lint 无新告警。

- [x] **Step 2: 勾掉本计划全部 checkbox 并提交计划文件**

```bash
git add docs/superpowers/plans/2026-06-12-group-task-orchestration-pr5-group-create.md
git commit -m "📝 plan: 群任务卡编排 PR5(group_create)实施计划+完成勾选"
```

---

## Out of scope(本 PR 明确不做)

- 远程(daemon)会话的 group_create 可用性(gateway 本机回环限制,与 org 工具同口径)。
- 主持人移交、发起 agent 单聊上下文带入群(spec §7.1 明确不带,靠 brief 转述)。
- 真机手动验证(与 PR2/PR3 一起列入合并后的验证清单)。

## 风险与预案

- **chat_svc TurnMCPProvider 签名变更**波及 orgtool_svc 与其测试——Task 1 一次性收口,
  bootstrap 函数值自动匹配;若有第三处实现(grep `TurnMCPProvider` 确认)同步改。
- **审批挂起期间 turn 一直 running**:与 org 工具同语义(4min 超时兜底),不新增风险。
- **e2e 审批按钮文案**:执行 Task 7 前先 `grep -n "orgApproval" frontend/src/i18n/locales/zh-CN/common.json`
  拿真实按钮文案,优先 `getByRole("button", { name: ... })`。
- **`i18n.NewError` 带参错误码**:若 cago 不支持 fmt 参数,GroupCreateMemberUnknown 改为
  固定文案 + handler 层拼名字,测试断言相应放宽(断言含名字即可)。

---

## 执行记录(2026-06-12,与计划的偏差)

- Task 4:`frontend/wailsjs/` 是 gitignore 生成物(仓库惯例),未随提交入库——计划里「git add frontend/wailsjs/」一步按惯例作废。
- Task 7:e2e 实际渲染 en locale,spec 选择器用双语 regex(与既有 spec 口径一致);worktree 下 e2e 须 `GOWORK=off make e2e`。
- 全程在独立 worktree(分支 feature/group-task-pr5)执行,以隔离主 checkout 上的并发会话。
- 全量 vitest 在高负载下有与本 PR 无关的负载型超时 flake(App.test 邮件 Hook 同步、eslint-i18n 等,5s 超时;focused 在本分支与基线均稳过,失败集合跨运行不同)。
- 终审 ready-to-merge;遗留 Minor 备忘:成员解析在审批前的 TOCTOU 窗口(CreateGroup 复检能力门)、brief 以用户身份落库的归因口径、审批挂起段与 orgtool 的结构性重复(第三个使用方出现时抽公共原语)。
