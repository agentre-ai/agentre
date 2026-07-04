# 会话级「后台运行中」指示 — 设计

- 日期：2026-07-04
- 范围：`agentre/`（Wails 桌面端）
- 状态：设计已定，待评审 → writing-plans

## 问题（已用真实 CLI + 代码确认）

Claude Code 类型后端派**后台 subagent**（`Agent`/`Task` 工具 `run_in_background: true`）时，会话在整个后台执行窗口内 `agent_status = "idle"`，导致侧栏/tab/呼吸灯把它当作「已读/跑完」，而 subagent 实际还在跑。

真实 CLI（`claude` 2.1.193，持久 stream-json）抓到的帧序列证实：主轮 `result`（dur≈12.7s）先于后台 subagent（`sleep 15`）完成到达 → agentre 主轮 finalize 成 `idle`；`driveSubagentActivity` 刻意「会话保持 idle」；后台完成后 `driveAutonomousTurn` 才短暂 running→idle。

所有会话级状态面（`attention-store.computeAttention`、`CountRunningByAgents` 呼吸灯）都只认 `agent_status='running'`，于是都不反映在跑的后台 subagent。**唯一**反映的是会话自身 ChatPanel header 的后台任务芯片（`background-tasks-chip.tsx`，从 `subagent_state` running + tool_use `run_in_background===true` 派生），但它只在打开该会话时可见。

排除的伪 bug：主轮收尾的 `MarkRunningSubagentsCancelled`（`chat.go:2656`）只在 `aborted` 触发，正常收尾不误标；完成靠 `FlipSubagentStatus` 正确翻 completed。

## 目标 / 非目标

- 目标：让**侧栏 / tab / 命令面板**这些会话级表面能看出「该会话有后台 subagent 在跑」，即使会话已读、`agent_status` 为 idle。
- 非目标：**不改 `running` 语义**——退出二次确认（`CountActive`）、删项目门控（`CountActiveByProject`）、呼吸灯（`CountRunningByAgents`）保持只认真正 running 的轮，不因后台 subagent 而阻塞/点亮。
- 非目标：不加数据库迁移；不改后台任务芯片既有行为。

## 决策（已拍板）

1. **真源 = chat_svc 内存 map + `session_status` 事件**。后台 subagent 易失（随 CLI 子进程/重启消失），无需持久化；重启后 map 空 = 0 天然正确。
2. **优先级**：`needs_attention > running > (error&unread) > bg_running > unread`。idle 会话只要有后台 subagent 在跑，**无论已读未读**都冒 `bg_running`。
3. **视觉**：`bg_running` 复用 running 显示色（`bg-status-running`/现有渲染），仅用 pill 文案「后台 / background」区分。不新增颜色 token / tab status 枚举。

## 架构 / 数据流

```
后台 subagent 起 / 完成 / 取消
  → chat_svc.bgRunning（内存 map: sessionID → set[toolUseID]）
  → ① emit session_status{ agentStatus, needsAttention, bgRunning }   （live）
  → ② ListChatAgents / LoadSession 出口把 map 合进会话 DTO           （批量刷不被 bulkUpsert 洗掉）
        ↓
  session-status-store（新增 bgRunning 字段）
        ↓
  attention-store.computeAttention → 新 reason "bg_running"
        ↓
  侧栏 / tab / 命令面板渲染（reasonToDisplayStatus→running 取色，reason==="bg_running" 出「后台」pill）
```

## 后端设计（chat_svc）

### 内存集合
- `bgRunning sync.Map[int64] → map[string]struct{}`：per-session **运行中后台 subagent 的 tool_use_id 集合**。用集合而非计数器：`add/remove` 幂等，杜绝加减泄漏。`hasBgRunning(sessionID) = 集合非空`。
- 提供内部 helper：`addBgRunning(sid, ids...)`、`removeBgRunning(sid, id)`、`clearBgRunning(sid)`、`bgRunningActive(sid) bool`；每次真变化后触发一次 `session_status` emit（同值短路，避免刷屏）。

### 判据 helper（与前端 `derive.ts` 同款）
- `runningBgSubagentIDs(blocks []ContentBlock) []string`：返回满足「`*SubagentStateBlock` status=="running" 且其父 `tool_use`（ParentToolCallID）入参 `run_in_background===true`」的 **ParentToolCallID**。
  - 前台 subagent（`local_agent` 无 `run_in_background`）主轮本就 running，不纳入。
  - 前台 `local_bash` 已被 `trackSubagentState` 挡在建块之外，天然不纳入。

### 维护点（4 个）
1. **主轮 finalize**（`chat.go`，`SetBlocks(finalBlocks)` 后、非 abort 分支）：`addBgRunning(sid, runningBgSubagentIDs(finalBlocks)...)`。
2. **自主轮 finalize**（`autonomous_turn.go`，`SetBlocks` 后）：同样 `addBgRunning`（后台 subagent 可能在自主轮里再派后台 subagent）。
3. **`FlipSubagentStatus` 成功**（`autonomous_turn.go`，后台完成翻 completed 时）：`removeBgRunning(sid, completedRef.ToolUseID)`。
4. **清空**：abort 收尾（`chat.go` aborted 分支）、error finalize、会话 `CloseSession`/evict → `clearBgRunning(sid)`（CLI 子进程被打断 → 其后台 subagent 全死，防泄漏）。

### 出口携带
- `SessionStatusEvent` 增字段 `BgRunning bool`；`dispatcherEmitter` / 各 emit 点带上 `bgRunningActive(sid)`。
- `LoadSession`、`ListChatAgents` 组会话状态 DTO 时合并 `BgRunning = bgRunningActive(sid)`（app 层 `project.go` 的会话状态结构 + `chat_svc` 出口结构各加一字段）。

## 前端设计

- `stores/types.ts`：`AttentionReason` 加 `"bg_running"`；`SessionStatusPatch`/`SessionStatusValue`、`AttentionInput` 加 `bgRunning: boolean`。
- `session-status-store.ts`：`isSamePatch`/`upsert`/`bulkUpsert`/`bumpDone` 带上 `bgRunning`。
- `attention-store.ts`：`computeAttention` 插入 `bg_running`，顺序：
  ```
  if needsAttention → needs_attention
  if agentStatus==="running" → running
  unread = lastMessageAt > lastReadAt
  if agentStatus==="error" && unread → error
  if bgRunning → bg_running          // 独立于已读/未读
  if agentStatus==="idle" && unread → unread
  return null
  ```
  `useSessionAttention` / `useSessionAttentionList` 把 `bgRunning` 喂进 input。
- `lib/attention-display.ts`：
  - `reasonToDisplayStatus("bg_running")` → `"running"`（复用绿点/现有渲染取色）。
  - `reasonToPillText("bg_running")` → `i18n.t("attention.background")`。
- **label 特判**：`agentSessionFromMeta`（`chat-page.tsx`）、`project-page.tsx`、命令面板 source 等 trailingLabel 逻辑里，`reason==="bg_running"` 时优先出 `reasonToPillText(reason)`（「后台」），而非 running 分支硬编码的 "running"。
- i18n：`attention.background` 加到 `zh-CN/common.json`（「后台」）与 `en/common.json`（`Background`）。

## 测试（TDD，先红后绿）

### 后端
- `runningBgSubagentIDs`：后台 subagent(local_agent+run_in_background)纳入；前台 subagent 不纳入；前台/后台 local_bash 区分；status 非 running 不纳入。
- 维护点：主轮 finalize 后集合含新后台 id；`FlipSubagentStatus` 后移除；abort/error/close 后清空。
- 出口：`session_status` 事件 / `LoadSession` / `ListChatAgents` DTO 携带 `BgRunning`（sqlmock + mock，不连真库）。

### 前端
- `computeAttention`：新优先级全分支（bg_running 独立于已读/未读；error/running/needs_attention 压过 bg_running；bg_running 压过 unread）。
- `session-status-store`：`bgRunning` 经 upsert/bulkUpsert 往返、同值短路。
- `attention-display`：`bg_running` → 显示态 running + pill「后台」。
- 覆盖 `i18n.test.ts`（新 key 双语齐全）。

## 涉及文件（预估）

后端：`internal/service/chat_svc/chat.go`、`autonomous_turn.go`、`types.go`（SessionStatusEvent / 内存 map / helper）、`dispatcher_emitter.go`（emit 带 BgRunning）、`internal/app/project.go`（DTO 字段）、对应 `_test.go`。

前端：`stores/types.ts`、`stores/session-status-store.ts`、`stores/attention-store.ts`、`lib/attention-display.ts`、`components/agentre/chat-page.tsx`、`project-page.tsx`、命令面板 source、`i18n/locales/{zh-CN,en}/common.json`，及各 `*.test.ts(x)`。

## 未决 / 已知边界

- remote claudecode（agentred）当前未实现 `SubagentActivitySource` / 不携带 `CompletedTask`（既有 v1 限制）→ 远端后台 subagent 的 bgRunning 走不通，保持现状（本设计不扩 remote）。
- abort 若中断的是**更早轮**派出、仍在跑的后台 subagent：靠「abort 清空该会话集合」兜底，避免永久 bg_running。
