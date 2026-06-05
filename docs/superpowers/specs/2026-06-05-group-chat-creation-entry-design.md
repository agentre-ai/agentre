# 群聊创建入口 + 侧栏混排/筛选 + group_invite 招募 — 设计文档

- 日期：2026-06-05
- 仓库：`agentre/`（Wails 桌面端）
- 关联 issue：#12（群聊 Agent 编排 MVP）已知缺口 #2「创建入口缺失 / 邀请 picker 是 stub」
- 关联设计：[`2026-06-03-group-chat-orchestration-design.md`](./2026-06-03-group-chat-orchestration-design.md)
- 状态：设计已评审锁定（UI/UX 已用 `~/Desktop/agentry.pen` mockup 迭代锁定），下一步 → writing-plans
- UI mockup（`agentry.pen`，light）：
  - **② 群聊 · 新建弹窗** — New Group 弹窗
  - **④ 混排侧栏 · 筛选竖向下拉 + 新建** — 混排会话侧栏（搜索行 = 筛选图标下拉 + 搜索 + `+` 菜单）

## 1. 背景与目标

群聊编排 MVP 已落地（三表 + `group_svc` 并发 fan-out + `group_send` MCP tool + 能力门控 + 前端面板/roster），后端 `GroupCreate` 可用——**但全程没有"创建一个群"的 UI 入口**（侧栏「群聊」分区仅在 `groups.length>0` 时渲染且分区头无 action，命令面板无 source，roster「邀请成员」是 `console.warn` stub）。0 群时无法创建第一个群。

评审中需求扩展为三块（同一设计、分阶段实现）：

1. **创建能力 + 入口**（#12 核心）：后端建群带初始成员 + 资格门控；前端 **New Group 弹窗**（标题 + 协调者 + 项目 + 初始成员）；入口 = 会话侧栏头部 **`+` 菜单**（新建 Agent 会话 / 新建群聊）。
2. **侧栏 IA 重构**：把 agent 与群聊合并成**一个按活跃度排序的混排列表**；搜索行前置一个**筛选图标下拉**（类型 全部/群聊/Agent + 状态 运行中/未读）；**agent 与群都可由用户置顶**。
3. **`group_invite` MCP tool**：协调者显式调用工具把 agent 拉进群，**退役**现有 `@mention` 文本自动招募（`ingest.go:maybeRecruit`），与 `group_send` 同一结构化哲学。

### 1.1 已锁定的决策（brainstorming + mockup 评审）

| 维度 | 决策 |
| --- | --- |
| 侧栏结构 | **混排**：agent 与群聊在一个列表里按**最近活跃**倒序；不再有独立「群聊」分区。pinned 项（系统 agent + 用户置顶项）浮顶 |
| 置顶 | **agent 与群都可用户置顶**（不再只靠 `IsSystem()`）。需新增 `pinned` 字段 + 切换 |
| 创建入口 | 会话侧栏**搜索行**最右 **`+` 按钮 → 菜单**：`新建 Agent 会话`（复用既有新会话流）/ `新建群聊`（开弹窗） |
| 筛选 | 搜索行**最前**一个**图标按钮**（sliders 图标，有激活红点）→ **竖向下拉菜单**（无分区）：`全部 / 群聊 / Agent`（类型，单选）+ `运行中 / 未读`（状态，多选切换）。`未读` 含审批/等待等待处理状态 |
| 弹窗字段 | 群标题（必填）+ 协调者（必填）+ 项目（可选，预填当前项目上下文）+ 初始成员（可选·多选） |
| 协调者/成员候选 | 仅 `CapMCPTools` 且 `chattable` 的 agent（MVP=claudecode）。门控由**后端**派生（新 `SupportsGroup` 字段），前端不写 `backendType==="claudecode"`（OCP） |
| department | 后端从协调者 agent **自动派生**（不暴露 UI）；定义 `group_invite` 可招募池 |
| 建群带成员 | `CreateGroup` 接受 `MemberAgentIDs[]`，一次 service 调用内原子加入（幂等 `ensureMember` + 逐个能力门控 + `maxMembers`） |
| 运行时招募 | 协调者调 `group_invite` MCP tool（结构化），退役 `@mention` 文本招募 |

## 2. 交互模型 / UI-UX（mockup 已锁定）

### 2.1 混排会话侧栏（mockup ④）

`ChatAgentList`（`chat-page.tsx` 渲染、`agent-list.tsx` 提供行组件）：

- **头部搜索行**（一行三件）：
  - **筛选图标按钮**（最前）：`sliders-horizontal` 图标，有激活筛选时角标红点；点开**竖向下拉**（`@/components/ui` popover/dropdown）：
    - 类型（单选，选中打勾）：`全部` · `群聊` · `Agent`
    - 状态（多选切换，前置状态色点）：`● 运行中` · `● 未读`（红色计数角标）
    - 无「类型/状态」分区标题、无分隔线——纯竖向列表。
  - **搜索框**（中间，fill）：占位「搜索 Agent / 群聊」。
  - **`+` 按钮**（最右）：点开菜单 `新建 Agent 会话`（`user-plus`）/ `新建群聊`（`users-round`）。
- **混排列表**（PanelBody）：agent 行与群行在**同一列表**，按**最近活跃**倒序；pinned（系统 agent + 用户置顶的 agent/群）浮顶并带 pin 标记。
  - 群行：浅蓝 `users-round` 头像 + 群标题 + 状态点 + 状态 tag（`3 轮` / `等待你`）。
  - agent 行：纯色头像 + 名字 + 状态 + chevron（展开其会话）。
  - 视觉区分：群=方角 users 头像，agent=圆角纯色头像 + chevron。

### 2.2 新建群聊弹窗（mockup ②）

复用 `newIssueDialog` 的 shadcn `Dialog` 结构。字段：群标题（必填，`Input`）→ 协调者（必填，单选 agent，候选 `supportsGroup && chattable`，hint「仅支持 group_send 的 Agent(claudecode)可作为协调者」）→ 项目（可选 `Select`，打开时预选 `projectContext`）→ 初始成员（可选·多选 `AgentMultiPicker`，avatar chip + 添加，hint「协调者可在群里随时邀请更多成员加入（group_invite）」）→ footer（`⌘+Enter 创建` · 取消 · 创建群聊）。

提交：校验（标题非空 + 协调者已选）→ `GroupCreate({title, coordinatorAgentID, projectID, memberAgentIDs})` → `useGroupListStore.reload()` → `openGroup(id,title)` 聚焦新 tab → 关闭。空候选态（无任何 `supportsGroup` agent）→ 弹窗空状态文案。`AgentMultiPicker` 设计为可复用，roster 的人工「邀请成员」后续可复用（本任务不接）。

## 3. 架构与分层改动（seam 清单，按阶段）

```
—— 阶段 A：创建能力 + 弹窗 + 入口 ——
internal/service/chat_svc/types.go            ChatAgentItem + SupportsGroup bool
internal/service/chat_svc/chat.go             ChatAgents 组装处派生 SupportsGroup
internal/service/group_svc/types.go           CreateGroupRequest + MemberAgentIDs []int64
internal/service/group_svc/group.go           CreateGroup：派生 DepartmentID + 逐个 ensureMember
internal/app/group.go                         GroupCreateRequest + MemberAgentIDs 透传
frontend/.../group-chat/group-new-dialog.tsx  新建群聊弹窗（新增）
frontend/.../group-chat/agent-multi-picker.tsx 可复用 agent 多选（新增）
frontend/.../chat-page.tsx                     头部 `+` 菜单（新建 Agent 会话 / 新建群聊）→ 开弹窗
frontend/src/i18n/locales/{zh-CN,en}/common.json  group.new.* + sidebar.add.* 文案

—— 阶段 B：侧栏混排 IA + 多维筛选 + 用户置顶 ——
migrations/2026xxxxxxxx_*.go                   append：agents + groups 加 pinned 列(原生 SQL)
internal/model/entity/{agent_entity,group_entity}  + Pinned 字段
internal/service/chat_svc/chat.go             ChatAgentItem.Pinned = IsSystem() || a.Pinned；活跃度/未读(attention)信号
internal/app/group.go                          GroupItem + Pinned/活跃度；Group 列表带未读/attention
internal/app/{agent,group}.go                  SetPinned 绑定(agent + group)
frontend/.../chat-page.tsx                     合并 agents+groups 为一条按活跃度排序的混排列表 + 筛选状态(type/status)
frontend/.../group-chat/*  + agent-list.tsx    统一行样式 + 筛选下拉 + 置顶切换(右键/hover)

—— 阶段 C：group_invite tool ——
internal/service/group_svc/mcp.go              group_invite tool schema + ServeHTTP 分支 + invite 回调
internal/service/group_svc/group.go            HandleInvite(协调者门控 + 池校验 + AddGroupMember)；buildGroupSystemPrompt 增说明 + 可招募 roster
internal/service/group_svc/ingest.go           移除 maybeRecruit / recruitableAgentByName
internal/pkg/agentruntime/runtimes/claudecode/session.go  仅协调者 turn 追加 mcp__<Name>__group_invite
internal/model/code/*.go + i18n                + GroupInviteForbidden 等
```

依赖方向不变：`internal/app → service → repository → entity`；`group_svc` 经 `chat_svc.Chat()` accessor 协作；`pkg` 不反向依赖 service。

## 4. 详细设计

### 4.1 资格门控 `SupportsGroup`（A）

`chat_svc.ChatAgents` 组装 `ChatAgentItem` 处（`chat.go` 内 `be := backends[a.AgentBackendID]` 已在手）：

```go
item.SupportsGroup = be != nil &&
    agentruntime.RuntimeFor(agent_backend_entity.BackendType(be.Type)).
        Capabilities().Has(capability.CapMCPTools)
```

零额外查询，无 `backendType=="claudecode"` 字面量（OCP）。前端弹窗两个 picker 用 `supportsGroup && chattable` 过滤。

### 4.2 建群带成员 + department 派生（A）

`CreateGroupRequest` 增 `MemberAgentIDs []int64`。`CreateGroup`：①`DepartmentID==0` → 取协调者 agent 部门；②`g.Check` + 协调者 `backendSupportsGroup`；③`Create` + `ensureMember(coordinator, RoleCoordinator)`；④遍历 `MemberAgentIDs`（去协调者自身）逐个 `backendSupportsGroup` + `maxMembers` → `ensureMember(RoleMember)`（幂等）。任一不支持 → `GroupBackendUnsupported`（前端已过滤，防御性）。`internal/app/group.go` 透传字段。

### 4.3 New Group 弹窗 + `+` 菜单（A）

- `group-new-dialog.tsx`：受控 `Dialog`，form state（title/coordinatorAgentID/projectID/memberAgentIDs），提交走 4.2，镜像 `project-new-dialog.tsx` 的 submit/loading/error。项目默认读 `new-chat-context-store.projectContext` 预选。
- `agent-multi-picker.tsx`：`{agents, value, onChange, exclude?}`，avatar chip + 候选列表（`@/components/ui` popover + checkbox 或 cmdk），过滤 `supportsGroup && chattable`，排除已选协调者。
- 头部 `+` 菜单（`@/components/ui` dropdown-menu）：`新建 Agent 会话`（复用既有新会话入口 / 打开命令面板 new-chat）+ `新建群聊`（开弹窗）。阶段 A 先挂在**现有头部**（创建即刻可用）；阶段 B 由重构后的搜索行承载同一菜单。

### 4.4 侧栏混排 IA + 多维筛选 + 用户置顶（B）

- **混排排序**：`chat-page.tsx` 把 `useChatAgents()` 与 `useGroupList()` 合并成一个 `Array<AgentItem | GroupItem>`，按统一"最近活跃" ts（agent 取 `metas` 中其会话 `max(lastMessageAt)`；群取 `Updatetime`/最后一条群消息 createtime）倒序。pinned 浮顶（pin 标记），其余按活跃度。
- **筛选状态**（本地，建议 persist）：
  - 类型（单选）：全部 / 群聊 / Agent → 过滤混排列表。
  - 状态（多选切换）：`运行中`（agent/群 runStatus running 或有 running 会话）/ `未读`（**未读=待处理/attention**：agent 取 `attentionSessions`/`IsWaitingForUser`，群取 `runStatus==waitingUser`；红点计数 = attention 项数）。
  - > 真·未读消息计数（未读 = 未查看的新消息，需 MarkRead/read-state 落库）作为后续增强；本期 `未读` 先以 attention/待处理状态实现。
- **用户置顶**：
  - 迁移：`agents`、`groups` 各 append 一列 `pinned BOOLEAN NOT NULL DEFAULT 0`（原生 SQL，末尾追加）。
  - 实体 + 仓储：`Pinned` 字段；`ChatAgentItem.Pinned = a.IsSystem() || a.Pinned`（系统 agent 仍恒置顶）；`GroupItem.Pinned`。
  - 绑定：`AgentSetPinned(id, bool)` / `GroupSetPinned(id, bool)`（thin → svc → repo update）。
  - 前端：行上 pin 切换（右键菜单 / hover 按钮）；pinned 浮顶。
- **统一行样式**：群行（users 头像 + 标题 + 状态）与 agent 行（头像 + 名 + chevron→会话）共用密度；`agent-list.tsx` 抽出共用行。

### 4.5 `group_invite` MCP tool（C）

复用 `group_send` 的 MCP-over-HTTP 管线（`mcp.go:groupMCP`，per-member bearer token；`buildGroupMCP` 注入 `RunRequest.MCPServers`）。

- **schema**（`mcp.go` 增 `groupInviteToolSchema()`）：`name=group_invite`，input `{agentNames?:string[], agentIds?:integer[], reason?:string}`（`anyOf` 二选一）。描述：把部门内 Agent 拉进群，只有协调者可调用。
- **ServeHTTP** 增分支：`lookup(bearer)` → `(group, member)` → `invite` 回调（与 `ingest` 并列方法值）。
- **`HandleInvite(ctx, callerMemberID, names, ids, reason)`**：①协调者门控（非 `IsCoordinator()` → `GroupInviteForbidden`）；②在**可招募池**（`g.DepartmentID` 下 `IsActive()` agent）内按 id/名解析；③逐个 `backendSupportsGroup` + `maxMembers` → `ensureMember(RoleMember)` → 落 `sender_kind=system` 的「X 加入了群聊」消息（复用 `persistMessage` + 群事件流，前端已渲染 system 行）；④返回加入结果（id+name）。
- **allowedTools**：`claudecode/session.go` 处 `group_send` 对所有成员追加；`mcp__<Name>__group_invite` **仅协调者成员 turn** 追加（随 `MCPServerSpec`/role 条件下传）；handler 协调者门控为第二道防线。
- **系统提示**（`buildGroupSystemPrompt`）：协调者 suffix 增 `group_invite` 用法 + **可招募 roster**（部门内未进群、支持 `CapMCPTools` 的 agent 的 `名字·角色·id`）。
- **退役 @mention 招募**：删 `ingest.go:maybeRecruit`/`recruitableAgentByName`；`resolveMentionNames` 对未进群名字仅记日志；`applyFallback`/`lastSenderMemberID` 保留。

## 5. 错误处理 / i18n / 能力门控

- 错误码：复用 `GroupTitleRequired`/`GroupCoordinatorRequired`/`GroupBackendUnsupported`/`GroupMemberLimit`；新增 `GroupInviteForbidden`。经 `i18n.NewError(ctx, code)`，前端 toast/inline。
- i18n：`group.new.*`（弹窗）+ `sidebar.add.*`（`+` 菜单）+ `sidebar.filter.*`（筛选项）+ `sidebar.pin.*`，zh-CN/en 双份；`i18n.test.ts` 校验。动态内容（agent/项目/群名、「X 加入了群聊」）不进 `t()`。
- 日志：`logger.Ctx(ctx)`，`group_svc.CreateGroup`/`HandleInvite` 前缀 + `zap` 字段。

## 6. 测试计划（严格 TDD：Red → Green → Refactor）

**A**：`chat_svc`（mockgen）`SupportsGroup` 派生（claudecode=true，其它/无后端=false）；`group_svc` `CreateGroup` 带 `MemberAgentIDs`（协调者+成员落库 / 不支持成员报错 / `DepartmentID==0` 派生 / 超 `maxMembers`）；前端 Vitest 弹窗（校验、提交→create→openGroup、空候选态、项目预填）、`AgentMultiPicker`、`+` 菜单。

**B**：迁移 `*_test.go`（pinned 列）；`chat_svc`/`group_svc` 暴露 `Pinned` + 活跃度/attention；`SetPinned` 绑定（svc + repo sqlmock）；前端 Vitest 混排排序（活跃度 + pinned 浮顶）、筛选（类型/状态过滤）、置顶切换。

**C**：`group_svc` `HandleInvite`（协调者成功 / 非协调者 `GroupInviteForbidden` / 池外跳过 / 超员）；`mcp.go` `group_invite` 路由 + 非法 bearer 拒；`session.go` 协调者 turn allowedTools 含 invite、非协调者不含；招募退役回归（`@` 未进群名不再自动入群）。

后端 `make test-backend`（race）+ golangci-lint v2 0 issues；前端 `tsc --noEmit` + Vitest + eslint（含 `i18next/no-literal-string`）；新绑定字段需 `make generate`。

## 7. 分阶段实现（一份 spec，三个 PR）

- **A — 创建能力 + 弹窗 + 入口**（#12 核心，独立 PR）：4.1 → 4.2 → `make generate` → 4.3。完成即「群聊可被创建」（入口先挂现有头部 `+` 菜单）。
- **B — 侧栏混排 IA**（独立 PR）：4.4（迁移 pinned → 实体/仓储/绑定 → 前端混排 + 筛选下拉 + 置顶）。完成即「agent/群混排 + 多维筛选 + 可置顶」。
- **C — group_invite**（独立 PR）：4.5（tool + handler + allowedTools + roster + 退役 @mention 招募）。

## 8. 范围外（本任务有意不做）

- 真·未读消息计数（MarkRead/read-state 落库）——B 先用 attention/待处理信号；真未读后续。
- roster 人工「邀请成员」picker 接线（缺口 #2 人工入口）——`AgentMultiPicker` 已备好复用。
- 命令面板「新建群聊」source（`+` 菜单已覆盖创建）。
- 群 transcript 内联工具审批（缺口 #1）、群 tab 跨 reload 持久化、远端 daemon agent 入群。
- 工作目录中已有的 `command-palette` Shift+Tab、`pkg/codex` 改动与本任务无关，不动。
