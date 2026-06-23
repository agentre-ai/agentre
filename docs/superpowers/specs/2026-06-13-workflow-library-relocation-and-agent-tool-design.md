# 流程库重定位 + 流程管理 Agent 工具 — 设计

- 日期：2026-06-13
- 分支基线：`develop/group`
- 设计稿：`~/Desktop/agentry.pen`（仅 pencil MCP），新增帧：
  - `流程库管理弹窗 · 浏览预览`（id `mM5u0`）
  - `流程库管理弹窗 · 内联编辑`（id `ZboYf`）
  - `流程库管理弹窗 · 内联删除确认`（id `m4HBy4`）
  - `入口① 建群弹窗 · 协作流程行(加管理入口)`（id `Ae4I5`）
  - `入口② 命令面板 · 流程库命令`（id `AUa8B`）

## 背景与动机

「流程库」当前占用左侧导航栏（rail）一个一级槽位 + 一个整页路由 `/workflows`。它本质是**次级功能**：跨群复用的协作流程（SOP），在建群时绑定到群、由主持人每轮注入。一个一级 rail 槽位（rail 共 6 项：Chat / Projects / Issues / Org / Workflows / Hooks）给它太重了。

本设计做两件相关但独立的事：

- **Part A（前端 UI/UX）**：把流程库从 rail 槽位 + 整页路由，改造成一个**全局管理弹窗**，从「使用处」入口打开。腾出 rail 槽位（6 → 5）。
- **Part B（后端 + 少量前端）**：新增一个**流程管理 Agent 工具**，让 AI（被授予该工具的 agent，如主持人）能像管理组织架构那样，列出 / 新建 / 编辑 / 删除流程。镜像现有 org 工具（`orgtool_svc`）的形态与审批门。

两部分共享同一个 `workflow_svc` CRUD（已存在），互不依赖，可拆成独立 PR。

## 非目标

- **不改流程的 CRUD 服务**：`workflow_entity` / `workflow_repo` / `workflow_svc`（List/Create/Update/Delete + `groupCounts`）已完整存在，本设计复用，不重写。
- **不改群绑定与注入**：`group_entity.WorkflowID` 绑定、`group_svc.buildGroupSystemPrompt` 的 SOP 注入逻辑保持不变。
- 不引入新的迁移（流程表 `workflows` 已由迁移 `202606110002` 建好）。

---

## Part A — 流程库管理弹窗（UI 重定位）

### 当前状态（待移除/改造）

- `frontend/src/App.tsx`
  - `navItems` 里的 `/workflows`（`routeIcon`，`nav.workflows`）一级 rail 项（`App.tsx:94-98`）
  - `pageBreadcrumbKeys["/workflows"]`（`App.tsx:118`）
  - 路由 `<Route path="/workflows" element={<WorkflowsPage />} />`（`App.tsx:886`）
- `frontend/src/components/agentre/workflows/workflows-page.tsx` — 整页 master-detail（340px 列表 + 预览 + footer 编辑按钮），编辑/删除走独立弹窗。
- `frontend/src/components/agentre/workflows/workflow-edit-dialog.tsx`、`workflow-delete-dialog.tsx` — 独立弹窗。

> 唯一引用 `/workflows` 的是 `App.tsx` 与页面自身；提交的 e2e 套件无引用（仅 `e2e/scratch/` throwaway 用过 `nav-workflows`，gitignore，不计）。重定位低风险。

### 目标信息架构

- 删除 rail 项 + 路由 + 面包屑。rail 6 → 5。
- 旧 lastPath 若是 `/workflows`，由 `<Route path="*" element={<Navigate to="/chat" replace />} />` 兜底重定向到 `/chat`（安全）。

### 管理弹窗（`WorkflowManagerDialog`）

单个全局 modal，挂在应用根（与 `<CommandPalette/>` 并列于 `AppLayout`），由一个 zustand store 控制开关。视觉沿用既有对话框样式（`⑤ 流程编辑弹窗` / `Modal/Dialog`：`$card` 底、`$radius-xl`、48 blur 阴影、`$border` 描边）。

布局（约 920×640，见 Pencil `mM5u0`）：

- **Header**：流程图标（primary-soft 方块）+ 标题「流程库」+ 副标题「跨群复用的协作流程(SOP) · 主持人每轮注入绑定流程的最新内容」+ 弹簧 + 「新建流程」主按钮 + 关闭(x)。Header 在所有内部模式下**保持不变**。
- **Body（横向）**：
  - **ListColumn（约 300px）**：顶部搜索框 + 流程行列表（名称 / 使用中徽标 / 摘要首行 / 更新时间）。选中行：primary-soft 底 + 左侧 3px primary accent。
  - **DetailPane（fill）**：有三种内部模式（**就地切换，不叠新弹窗**）：
    - **浏览/预览态**（默认，见 `mM5u0`）：pHeader（图标 + 名称 + meta「N 个群使用中 · 更新于… · 修改即时生效」）+ 可滚动正文卡（`MarkdownText` 渲染 SOP）+ footer（「编辑流程」主按钮 + 删除图标按钮）。
    - **内联编辑态**（见 `ZboYf`）：点「新建流程」或「编辑流程」时，右栏就地换成编辑器：流程名称 input、流程正文 Markdown textarea、「插入骨架模板」link、提示行；footer 换成「⌘+Enter 保存 · Esc 取消 / 取消 / 保存流程」。
    - **内联删除确认态**（见 `m4HBy4`）：点删除图标，footer 就地展开 destructive 确认条（「『X』正被 N 个群使用；删除后这些群按『不绑定流程』处理，且不可恢复。」+ 取消 / 删除流程），不弹新窗。

> 弹窗嵌套深度：命令面板进入 = 深度 1；建群弹窗进入 = 深度 2（建群弹窗在底）。内联模式切换不再叠窗，最大深度恒为 2。

### 入口

1. **命令面板**（全局 / SOP 编写入口，见 `AUa8B`）：在命令模式新增一个 action source，暴露两条命令：
   - 「打开流程库」→ 关面板 + 打开管理弹窗（浏览态）。
   - 「新建流程」→ 关面板 + 打开管理弹窗并直接进入空白编辑器。
   命令面板的 source 数组（`command-palette.tsx:57` `SOURCES`）注释明确支持「导航/动作」源，这是既定扩展点。source 的 `onSelect` 通过 store 打开弹窗 + `ctx.close()`。
2. **建群弹窗**（使用处，见 `Ae4I5`）：在「协作流程」label 行右侧加一个 ghost「管理流程」link（`group-new-dialog.tsx:191-218` 的 workflow `<Select>` 区域）。点开管理弹窗；弹窗关闭后建群弹窗重新 `WorkflowList()` 刷新下拉，使新建流程立即可选。

### 组件与文件改动（Part A）

- **新增** `frontend/src/stores/workflow-manager-store.ts`：`{ open: boolean; mode: "browse" | "create"; openBrowse(); openCreate(); close() }`。
- **新增** `frontend/src/components/agentre/workflows/workflow-manager-dialog.tsx`：折叠原 `workflows-page` 的 列表 + 预览，加内联编辑/删除态。复用 `useWorkflows` hook、`MarkdownText`。
- **抽取** 编辑表单：把 `workflow-edit-dialog.tsx` 的表单主体抽成 `workflow-editor-form.tsx`（名称 + 正文 + 插入模板 + 提示），供管理弹窗内联编辑态使用。删除态用内联确认条（不再用 `workflow-delete-dialog`）。
- **新增** 命令面板 source `frontend/src/components/agentre/command-palette/sources/workflow-actions-source.tsx`，注册进 `SOURCES`。
- **改** `frontend/src/components/agentre/group-chat/group-new-dialog.tsx`：加「管理流程」link + 关闭后刷新 `WorkflowList()`。
- **改** `frontend/src/App.tsx`：删 nav 项 / 路由 / 面包屑；在根挂 `<WorkflowManagerDialog/>`。
- **移除** `workflows-page.tsx`（及其 test）、`workflow-delete-dialog.tsx`；`workflow-edit-dialog.tsx` 视抽取结果保留或合并。
- **i18n**：删 `nav.workflows`（变为未使用）；新增管理弹窗 + 入口文案于 `zh-CN` 与 `en` 的 `common.json`；保留并扩展现有 `workflows.*`。
- **测试**：管理弹窗 test（浏览/编辑/删除三态 + store 驱动开关）、命令面板 source test、`group-new-dialog` 扩展 test、`i18n.test.ts` 覆盖。原 `use-workflows`、编辑表单 test 平移复用。

---

## Part B — 流程管理 Agent 工具（`workflowtool_svc`）

### 动机

让 AI 像管理组织架构（org 工具）一样管理流程库：被授予该工具的 agent 可通过 MCP 调用列出 / 新建 / 编辑 / 删除流程。与 Part A 的人类 UI 弹窗并列，二者都落到同一个 `workflow_svc` CRUD。

### 架构（镜像 `orgtool_svc`）

参照 org 工具（`internal/service/orgtool_svc/`，spec `2026-06-11-agent-org-tool-design.md`）：以**内嵌 MCP-over-HTTP server** 形态，按 turn 注入会话；per-agent 用 `agent_entity.ToolEnabled(key)` 门控；写操作走用户审批门。

新增包 `internal/service/workflowtool_svc/`：

- `workflowtool.go`：单例 `Default()`、`RegisterDeps()`、`MCPHandler()`、`BuildTurnMCP()`（仅当 `ToolEnabled(KeyWorkflow)` 且有 gateway URL 才注入）。
- `mcp.go`：MCP-over-HTTP（`initialize` / `tools/list` / `tools/call`）+ HMAC token 签发与校验 + 实时 enable 校验（403 if 关）+ `tools/list` schema。
- `approval.go`：写工具 handler（create/update/delete）+ 审批挂起/恢复 + `execWriteTool` 分发。
- `types.go`：各工具入参结构。
- `deps.go`：消费侧窄接口（DIP/ISP）：
  - `WorkflowQuery`（List/Get）、`WorkflowCommand`（Create/Update/Delete）—— 由 `workflow_svc` 适配实现；
  - `ApprovalGateway`（Begin/Finish 审批）、`AgentLookup`（按 id 查 agent 校验 `ToolEnabled`）。
- `mock_workflowtool_svc/`：mockgen 生成。

### 暴露的工具

注册表 `internal/pkg/agenttool/agenttool.go` 新增：

```go
const KeyWorkflow = "workflow"
// registry += { Key: KeyWorkflow, MCPPath: "/mcp/workflow/",
//   ToolNames: ["workflow_list", "workflow_create", "workflow_update", "workflow_delete"] }
```

- **`workflow_list`**（读，**无需审批**）：返回流程列表（id / name / 正文 / 使用中群数 / 更新时间）。镜像 `org_get`。
- **`workflow_create` / `workflow_update` / `workflow_delete`**（写，**需用户审批**）：镜像 org 写工具，调用挂起直至批准/拒绝/超时（沿用 org 的 ~4min 超时）。执行落 `workflow_svc.Create/Update/Delete`。删除前在审批文案里带出「正被 N 个群使用」。

### 能力门控 + 前端开关

- 后端：`agenttool.Keys()` 自动把 `workflow` 纳入「可用工具清单」（`availableTools`）。
- 前端：`org/tool-catalog.ts` 把 `"workflow"` 加入 `APPROVAL_TOOLS` 集合（写需审批 → 带审批徽标）；i18n 新增 `org.agent.tools.names.workflow` =「流程库」与 `descriptions.workflow` =「允许该 Agent 查询并管理协作流程（写操作需你审批）」。能力 picker（`org-detail-agent.tsx`）按 key 自动渲染，**无需新 UI**。

### 审批管线：泛化为通用 tool_approval（已定，含探查修正）

**决定：完整重命名为通用「工具审批」管线（方案 1 彻底版，OCP）。**

**探查修正（2026-06-14）**：审批管线**其实已经是通用的、且已被两个工具共用**——`chat_svc/blocks.OrgApprovalBlock` 无任何 org 专属字段（只有 `{RequestID, ToolName, ToolInput, Status, Result}`），`chat_svc.Begin/FinishOrgApproval` 也是通用逻辑（只是名字叫 org）；**org 工具与 `group_create`（group_svc）已经共用同一套 block + Begin/Finish**，前端 `OrgApprovalCard` 已按 `toolName` 通用渲染、并对 `group_create` 路由到独立 `AnswerGroupCreateApproval`。所以「泛化」不是从零搭共享机制，而是**把已经共享的机制改名为通用 + 统一 Answer 入口**。

落地（PR2，前置重构）：

- **block**：`chat_svc/blocks/org_approval.go` → `tool_approval.go`；`OrgApprovalBlock` → `ToolApprovalBlock`，新增字段 `ToolKey string`（"org" / "group_create" / "workflow"）；`Type()` 返回 `"tool_approval"`；factory 重注册。
- **chat_svc**：`org_approval.go` → `tool_approval.go`；`BeginOrgApproval/FinishOrgApproval/takeOrgApprovals/snapshotOrgApprovals` → `Begin/Finish/take/snapshotToolApproval`；事件 payload `kind:"org_approval"` → `"tool_approval"`（带 `toolKey`）；map 字段 `orgApprovals` → `toolApprovals`。
- **统一 waiter + Answer**：把现在分散在 orgtool_svc / group_svc 各自的 `waiters sync.Map` + 各自的 `Answer*Approval` binding **上收进 chat_svc**：`BeginToolApproval` 登记 block **并返回等待 channel**；新增唯一 `chat_svc.AnswerToolApproval(ctx, sessionID, requestID, allow)` 按 requestID 路由唤醒。工具服务只 `BeginToolApproval(→ch) → 等 ch → FinishToolApproval`，不再各自持 waiter / Answer。
- **orgtool_svc / group_svc**：`ApprovalGateway` 接口换成通用 `BeginToolApproval/FinishToolApproval`；建 block 时带 `ToolKey`（"org" / "group_create"）；删掉各自的 `waiters` 与 `AnswerOrgApproval`/`AnswerGroupCreateApproval`。
- **App binding**：`AnswerOrgApproval` + `AnswerGroupCreateApproval` 合并成单个 `App.AnswerToolApproval` → `chat_svc.Default().AnswerToolApproval`。
- **前端**：`OrgApprovalCard` → `ToolApprovalCard`，读 `block.toolApproval`（带 `toolKey`），统一调 `AnswerToolApproval`；`transcript-rows.ts`/`transcript-row-view.tsx` 的 `case "org_approval"` → `"tool_approval"`、`block.orgApproval` → `block.toolApproval`；`chat-streams-store.ts`/`chat-streams-host.ts`/`use-chat-session.ts`/`use-chat-stream.ts` 的 `org_approval`→`tool_approval`、`OrgApprovalData`→`ToolApprovalData`、`appendLiveOrgApproval`→`appendLiveToolApproval`、`markOrgApprovalResolved`→`markToolApprovalResolved`。i18n `orgApproval.*` → `toolApproval.*`（工具标签 `tools.*` 保留，PR4 加 `workflow_*`）。
- **回归护栏（关键）**：现有 org 审批测试（`orgtool_svc/approval_test.go`、`chat_svc/org_approval_test.go`、`blocks/org_approval_test.go`、前端 `org-approval/card.test.tsx`、`org-detail-agent.test.tsx`）+ group_create 审批测试（`group_svc/create_test.go`）**全部随之改名/迁移并保持绿**——这是「重构优于打补丁」下保证 org/group 零回归的安全网。

> PR2 是 Part B 的前置重构（仅改名+上收 waiter，不改行为）；PR3 接入 workflow 时直接用 `BeginToolApproval(ToolKey="workflow")`，无需再碰审批 plumbing。

### 文件改动（Part B）

- **改** `internal/pkg/agenttool/agenttool.go`：加 `KeyWorkflow` + registry。
- **新增** `internal/service/workflowtool_svc/`（见上）+ `mock_workflowtool_svc/`。
- **改** `internal/bootstrap/cago.go`：`gw.RegisterMCP("/mcp/workflow/", workflowtool_svc.Default().MCPHandler())`；注册 workflow 的 `TurnMCPProvider`；`RegisterDeps` 把 `workflow_svc` 适配为 `WorkflowQuery/Command`、复用 agent lookup 与（PR2 后的）通用审批网关。
- **审批管线**：PR2 已完成通用 `tool_approval` 改名+统一 Answer；PR3 直接复用。

### 文件改动（Part B）

- **改** `internal/pkg/agenttool/agenttool.go`：加 `KeyWorkflow` + registry。
- **新增** `internal/service/workflowtool_svc/`（见上）+ `mock_workflowtool_svc/`。
- **改** `internal/bootstrap/cago.go`：`gw.RegisterMCP("/mcp/workflow/", workflowtool_svc.Default().MCPHandler())`；注册 workflow 的 `TurnMCPProvider`；`RegisterDeps` 把 `workflow_svc` 适配为 `WorkflowQuery/Command`、复用 agent lookup 与审批网关。
- **审批管线**：按所选方案改 `chat_svc`（泛化）或新增 workflow 专属块。
- **前端**：`tool-catalog.ts` + i18n（如上）；审批渲染按所选方案复用/新增。
- **测试**：`workflowtool_svc/mcp_test.go`（token / enable 门控 / schema / 路由）、`approval_test.go`（批准执行 / 拒绝 / 超时 / 执行错误）、`chat_svc` 审批生命周期 test、bootstrap 装配 test。镜像 org 工具的测试矩阵。

---

## Part C — `group_create` 绑定流程（拉群带流程，2026-06-15 追加）

### 动机

让「agent 自己创建流程 → 选定 agent 拉群并用上这个流程」形成闭环。今天 `group_create` MCP 工具只收 `{title, memberNames, brief}`，**无法绑定流程**——只有人类建群弹窗（`group-new-dialog.tsx` 的「协作流程」下拉）能绑。`CreateGroup` 早已支持 `WorkflowID`（`group_entity.WorkflowID` 落库、主持人每轮注入最新正文），所以本期只需把这条入参从工具透传到 `CreateGroup`，**不改任何绑定/注入语义**（与「非目标：不改群绑定与注入」一致）。

典型链路：agent 先用 Part B 的 `workflow_create` 建流程（结果文本回传 `id`），再用 `group_create` 携 `workflowId` 拉群；建群后主持人首轮即注入该流程正文。

### 改动（`group_svc`，最小透传）

- **工具 schema**（`mcp.go::groupCreateToolSchema`）：`properties` 加可选 `workflowId`（integer，描述：「可选；绑定一个协作流程(SOP)的 id，主持人每轮注入其最新正文。先用 workflow_list 查或 workflow_create 建，省略或 0=不绑定」）。**不进 `required`**。
- **arguments 解码**（`mcp.go` 的 `Arguments` 匿名 struct）：加 `WorkflowID int64 \`json:"workflowId"\``。
- **MCP 路由**（`mcp.go` 的 `group_create` 分支）：把 `rpc.Params.Arguments.WorkflowID` 作为新参传给 `h.groupCreate(...)`。
- **回调类型**（`mcp.go` 的 `groupCreate func(...)` 字段）：签名末尾加 `workflowID int64`。
- **接口 + 实现**（`group.go::GroupSvc.HandleGroupCreate` 接口、`create.go::HandleGroupCreate` 实现，`group.go:138` 的 `s.mcp.groupCreate = s.HandleGroupCreate` 绑定自动适配）：加 `workflowID int64` 形参；①填进审批卡 `ToolInput`（`map[string]any{... "workflowId": workflowID}`，让用户在审批时看到绑了哪个流程）；②透传 `CreateGroupRequest{... WorkflowID: workflowID}`。
- **不做校验**：无效/不存在的 `workflowId` 沿用现有注入侧 `IsActive()` 软门——绑了但流程不存在/已删时，注入侧静默按「不绑定」处理（与人类弹窗绑定后流程被删的既有行为一致）。不额外报错，保持透传最小化。

### 不变量

- 仍只透传到既有 `CreateGroup`，不新增建群路径；`group_create` 仍走 PR2 通用 `tool_approval`（`ToolKey="group_create"`），审批门不变。
- 群成员轮（`groupID>0`）不注入 `group_create`（防套娃）——本改动不触及该门控。
- 无新迁移、无新错误码（`workflows` 表与 `groups.workflow_id` 列已存在）。

---

## 分层与不变量校验

- 前端只走 Wails binding（不加 HTTP-style app API）；管理弹窗经 `useWorkflows` → `app/workflow.go` → `workflow_svc`。
- 工具侧：`workflowtool_svc`（service）依赖 `workflow_svc` 的**窄接口**（DIP），实现由 bootstrap `RegisterDeps` 注入；`pkg/agenttool` 是 leaf，不 import service。
- 复用现有 CRUD，新增能力以「加一个工具 / 加一个 source / 加一个弹窗」的方式扩展（OCP），不改 switch、不改 producer。
- TDD：每个新单元先写失败的行为/回归测试再实现；repo 用 sqlmock，service 用 mockgen，不连真库。

## PR 拆分建议

- **PR1（Part A 前端）**：管理弹窗 + store + 两入口 + 移除 rail/route + i18n + 测试。
- **PR2（Part B 前置重构）**：泛化 org 审批管线为通用「工具审批」（后端 block/begin/finish/answer + stream kind + 前端渲染器），org 工具改走共享入口；先补/跑 org 审批回归测试，泛化后全绿。
- **PR3（Part B 后端）**：`agenttool` 注册 `KeyWorkflow` + `workflowtool_svc` + bootstrap 装配（MCP 挂载 / TurnMCPProvider / RegisterDeps 适配 `workflow_svc`）+ 后端测试。接共享审批管线。
- **PR4（Part B 前端）**：`tool-catalog` 加 `workflow` + i18n 名称/描述；审批渲染按 `toolKey` 适配 workflow 文案。
- **PR5（Part C 后端）**：`group_create` 工具加可选 `workflowId` 透传到 `CreateGroup.WorkflowID`（schema + arguments + 回调 + 接口/实现 + 审批卡 ToolInput）。独立于 Part B，但产品上「先建流程再拉群带流程」需 PR3 一起才闭环。

> Part A（PR1）与 Part B（PR2→PR3→PR4）独立，可并行；Part B 内部 PR2 必须先于 PR3/PR4。Part C（PR5）只依赖既有 `group_create` + `CreateGroup`，与 Part B 解耦，可独立落；闭环演示需 PR3+PR5 都在。

## 开放决策 / 风险

1. **审批管线**：已定为泛化共享管线（见上）。泛化触及 org 审批 + 前端渲染器，靠回归测试护栏保证 org 零回退。
2. `workflow_list` 是否对 AI 暴露**全部**流程正文（可能较长 → token）；可仿 `org_get` 做投影（名称 + 使用数 + 截断/按需取正文）。倾向：list 返回名称 + 使用数 + 更新时间，正文按 `workflow_get` 单取（是否需要 `workflow_get` 待定，本期可先不加，list 直接带正文，若 token 压力再拆）。
3. 远程执行（agentred）下工具注入与 org 工具同款限制（已知）。
4. 真机/e2e 验证未列入实现，按既有惯例人工冒烟。
