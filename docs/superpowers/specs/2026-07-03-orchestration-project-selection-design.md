# 编排新建时可选项目 — 设计

日期：2026-07-03 · 仓库：`agentre/`（Wails 桌面端）· 分支：`develop/wyz`

## 目标

新建编排（orchestration run）时，允许用户**可选**地选择一个项目。若选择了项目，则该 Run
下的**所有 agent**（Leader + 所有被派发的子 agent + ask 新建的会话）在运行时以该项目的
`Path` 作为工作目录（cwd）。若未选择项目，行为不变——各 agent 使用自己的默认目录
（`projectId: 0`）。

## 背景（现状）

后端大部分已就绪，缺口在两处：

- `OrchestrationRun.ProjectID`、`CreateRunRequest.ProjectID`、Wails 绑定 `RunCreateRequest.projectId`
  均已存在。
- `chat_entity.Session.ProjectID` 已存在；`project_svc.ResolveSessionCwd(session)` 会把
  `session.ProjectID` 解析为 `project.Path`（远端 backend 走 `project_locations(project_id, device_id)`，
  自由会话 `ProjectID=0` 返回空 cwd 交由远端兜底）。
- `CreateRun` 已经把 `ProjectID` 传给根会话（`EnsureOrchSession`），因此 **Leader/根会话已经能继承项目 cwd**。

**缺口一（后端）**：
- `internal/service/orch_svc/dispatch.go:36`（派发子 agent）与
  `internal/service/orch_svc/ask.go:149`（ask 新建会话）调用 `EnsureOrchSession` 时只传了
  `RunID`，**没有传 `ProjectID`**，导致这些子会话 `ProjectID=0`，丢失项目 cwd。

**缺口二（前端）**：
- `frontend/src/components/agentre/orchestration/run-new-dialog.tsx` 没有项目选择控件，
  `submit()` 里 `projectId` 硬编码为 `0`（第 167 行）。

## 范围决策（已确认）

- **运行目录范围**：Leader + 所有被派发的子 agent 都在项目目录下运行（用户已确认「全部
  agent」）。因此本设计**包含**后端的 `ProjectID` 传播修复。
- **控件文案**：标签用「项目」。
- **控件位置**：放在「起始流程」（flow）选择器**之后**。

## 改动一：后端——把 Run 的 `ProjectID` 传播到子会话 / ask 会话

`orch_svc` 拥有 Run↔Project 关联（`orchSvc.runs orch_repo.RunRepo`，`Find(ctx, id)` 返回带
`ProjectID` 的 `*OrchestrationRun`）。`chat_svc` 保持项目无关，符合分层与 DIP。

1. 在 `orchSvc` 上新增私有辅助方法：

   ```go
   // runProjectID 返回 Run 挂载的 project id（Run 不存在时返回 0，走自由会话兜底）。
   func (s *orchSvc) runProjectID(ctx context.Context, runID int64) (int64, error)
   ```

   实现：`run, err := s.runs.Find(ctx, runID)`；err 直接返回；`run == nil` 返回 `0`；否则返回
   `run.ProjectID`。

2. `dispatch.go` 的 `Dispatch`：在建子会话前调用 `s.runProjectID(ctx, parent.RunID)`，
   把结果填进 `EnsureOrchSessionInput{... ProjectID: projectID}`。

3. `ask.go` 的 `resolveOrCreateAgentSession`：在最后新建会话分支（第 149 行）同样用
   `s.runProjectID(ctx, runID)` 填 `ProjectID`。

后续链路无需改动：`EnsureOrchSession`（`internal/app/orch_adapter.go`）已透传
`ProjectID` → `chat_svc.createOrchChildSession` 落库 → agent 起轮时
`project_svc.ResolveSessionCwd` 解析为 `project.Path` → `RunRequest.Cwd`。

### TDD（后端）

- 在 `internal/service/orch_svc` 写回归测试：给 `runs` mock 注入一个 `ProjectID=10` 的 Run，
  断言 `Dispatch` 调用 `EnsureOrchSession` 时 `in.ProjectID == 10`（当前实现传 0 → 先看红）。
- 同样为 ask 的新建会话分支加一条断言（Run 有项目 → 新建会话带上该 projectId）。
- 既有 `EnsureOrchSession` mock 已被多处使用，用 `DoAndReturn` 捕获入参断言。

## 改动二：前端——在新建编排对话框加「项目」选择器

文件：`frontend/src/components/agentre/orchestration/run-new-dialog.tsx`

- 打开对话框时用 `ProjectListTree()` 加载项目树，复用 `project-new-dialog.tsx` 里
  `flattenTree(nodes, depth)` 的拍平模式（缩进展示，子项目也可选）。加一个
  `projectId` state，默认 `0`；对话框每次打开时随其他字段一起重置为 `0`。
- 在「起始流程」区块**之后**新增一个 shadcn `Select`（`@/components/ui/select`），
  标签 = i18n「项目」。选项：
  - 第一项「无（使用 Agent 默认目录）」= `projectId 0`（默认选中）。
  - 其余为拍平后的项目列表（按 depth 缩进）。
- `submit()` 里把硬编码的 `projectId: 0` 换成所选 `projectId`。
- 新增 i18n key（`orchestration.new.project` 等）到
  `frontend/src/i18n/locales/zh-CN/common.json` 与 `frontend/src/i18n/locales/en/common.json`。
  英文如「Project」/「None (use agent default directory)」。

### TDD（前端）

- Vitest 组件测试：断言选择器渲染、默认落在「无」、选中某项目后 `RunCreate` 以对应
  `projectId` 被调用（mock `ProjectListTree` 与 `RunCreate`）。参考现有 wailsjs runtime
  mock 约定（per-file `vi.mock`）。
- 若改动新增了 `t(...)` key，跑 `frontend/src/__tests__/i18n.test.ts` 保证中英覆盖。

## 测试与门禁

- 后端：`make test-backend` + 聚焦 `go test -race ./internal/service/orch_svc/...`。
- 前端：`cd frontend && pnpm test`（组件测试 + i18n 覆盖）。
- Lint：`make lint`。
- **无 DB 迁移**——所需列（`orchestration_runs.project_id`、`chat_sessions.project_id`）均已存在。

## 不在范围内

- 远端 `project_locations` 的配置方式（沿用现状：远端 agent 要项目 cwd 需已为该 device
  配置 location，否则空 cwd 兜底）。
- 编排列表 / 详情页展示所属项目。
- 回填已存在的历史 Run。
