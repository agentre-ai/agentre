# 群聊侧栏 Phase B 实现计划 — 混排列表 + 用户置顶

> **For agentic workers:** 严格 TDD（Red→Green→Refactor）。每个 task 先写失败测试、跑、看它失败、再实现、跑、看绿、提交。

**Goal:** 补齐 Phase B 的两个缺口（spec §2.1 / §4.4）：①agent 与群**混排**成一个按活跃度排序的列表（去掉独立「群聊」分区）；②**用户置顶**完整纵切（迁移 → 实体 → 仓储 `SetPinned` → service → 绑定 `SetAgentPinned`/`GroupSetPinned` → 前端置顶切换），agent 与群都可置顶。

**Architecture:** 分层 `internal/app → service → repository → entity`；`chat_svc.ListAgents`（`chat.go:268`）把 `Pinned: a.IsSystem()` 改为 `a.IsSystem() || a.Pinned`；agent 置顶走 `agent_svc.SetPinned → agent_repo.SetPinned`，群置顶走 `group_svc.SetGroupPinned → group_repo.SetPinned`（独立方法，不动 `group_repo.Update` 的列白名单）。前端把 `useChatAgents()` + `useGroupList()` 合并成一个 `MixedRow[]` 按活跃度排序，pinned 浮顶。

**Tech Stack:** Go（gormigrate / sqlmock 仓储测试 / mockgen+goconvey service 测试 / 迁移自测跑真 in-memory sqlite）；React19/TS、zustand、shadcn `@/components/ui/*`、react-i18next、Vitest（radix 用 `userEvent.setup({pointerEventsCheck:0})`）。

**关联 spec:** `docs/superpowers/specs/2026-06-05-group-chat-creation-entry-design.md` §2.1 / §4.4。

**已完成(勿重做):** Phase B 的筛选下拉(类型 all/groups/agents + 状态 running/unread)与 agent 活跃度排序已在 `chat-page.tsx` 实现并提交(`2b82f58`)。

---

## 已锁定的设计决策（执行时照此，评审可推翻）

1. **绑定命名**：`SetAgentPinned`（随 `internal/app/agent.go` 的 `VerbAgent` 习惯）+ `GroupSetPinned`（随 `group.go` 的 `GroupXxx` 习惯）。前端用 wails 生成的对应名。
2. **`group_repo.SetPinned` 顺带 bump `updatetime`** → 刚置顶的群在活跃度排序里浮上来。
3. **非 pinned 区不再加「AGENTS」分区标题**（mockup ④：单个 PINNED 标签 + 其余按活跃度，无标题）。
4. **置顶控件 = 行头常显 pin 图标按钮**（`Pin`/`PinOff`，`stopPropagation`），最易在 jsdom 断言；非 hover-only / 非右键菜单。

## 风险（执行时盯紧）

- **最高风险**：现有 `describe("ChatPage sidebar — 群聊分区")`（chat-page.test.tsx）硬断言被删掉的「Group Chats」分区标题，Task 9 必须改写;相邻 `混排筛选与顶部新建` 套件的 role 查询要保持仍然通过。
- **活跃度排序在 jsdom 的水合**：agent 活跃 ts 取自 `useSessionMetaStore.lastMessageAt`，可能不从 `ListChatAgents` mock 自动水合 → 先查 `session-meta-store` 在测试里怎么填的；若精确交错不可复现，退化成「无 Group Chats 标题 + 群行在扁平列表里」断言。
- **reload 接线**：agent 置顶后 `useChatAgentsStore.reload`，群置顶后 `useGroupListStore.reload`，否则浮顶不即时。
- **appMocks 覆盖**：`SetAgentPinned`/`GroupSetPinned` 必须加进 chat-page.test.tsx 的 `appMocks` hoisted 块，否则 jsdom 里真 wails import 抛错。

---

## 顺序：后端置顶纵切(1→7) → 前端混排(9) → 前端置顶切换(10)

> 详细任务码见本次会话生成的 Plan（架构师产出），含每个 task 的失败测试 / 实现 / run 命令 / 逐文件 `git add`。要点：
>
> - **T1 迁移** `migrations/202606030003_pinned.go`（原生 SQL ALTER agents/groups 加 `pinned BOOLEAN NOT NULL DEFAULT 0`）+ 自测 + append `migrationList()`。
> - **T2 实体** `agent_entity.Agent.Pinned` / `group_entity.Group.Pinned`。
> - **T3 仓储** `agent_repo.SetPinned` / `group_repo.SetPinned`(sqlmock 测试) + 重新 mockgen。
> - **T4 chat_svc** `chat.go:268` → `a.IsSystem() || a.Pinned`（service 测试覆盖系统/用户置顶/普通）。
> - **T5 agent_svc** `SetPinned(req)`（mock repo 测试）。
> - **T6 group_svc** `SetGroupPinned(id,bool)`（mock repo 测试）。
> - **T7 绑定** `app.SetAgentPinned` / `app.GroupSetPinned` + `GroupItem.Pinned` + `make generate`（`frontend/wailsjs/` 是 gitignore 生成物，不提交）。
> - **T8 i18n** `chatPage.pin.*`(zh/en)，与 T10 同提或先提。
> - **T9 前端混排** 合并 agents+groups 成一个 `MixedRow[]`（agent ts=`max(lastMessageAt)`，群 ts=`updatetime`），pinned 浮顶；删独立「群聊」分区；改写 `群聊分区` 测试套件 + 加混排排序测试。
> - **T10 前端置顶切换** `agent-list.tsx` AgentGroup 头 + `GroupRow` 加 pin 按钮 → `SetAgentPinned`/`GroupSetPinned` + reload；加测试。

## 收尾校验

- 后端 `go test -race ./internal/... ./migrations/...` + golangci-lint v2 0 issues。
- 前端 `cd frontend && pnpm test` + `pnpm exec tsc --noEmit` + `pnpm exec eslint`。
- 确认 `frontend/wailsjs/` 未被 stage。
- **每个 commit 只 `git add` 本 task 的文件，绝不 `git add -A`**（工作区有无关并行脏文件 chat-panel/command-palette/pkg.claudecode/pkg.codex 等，绝不碰）。
