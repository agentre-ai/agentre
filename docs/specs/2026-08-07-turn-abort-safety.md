# Turn 中断安全：跨轮 abort race / 无界 interrupt / 带外中断后的会话状态归属

> Status: Draft
> Owner: Agentre 桌面端
> Last updated: 2026-08-07

**Objective:** 让「中断（abort / interrupt）」在跨轮切换、CLI 不回应、带外轮三种边界下都不再伤害错误的轮次、不再挂起 goroutine、不再让会话状态无人 reconcile——三条来自 `docs/specs/2026-08-07-autonomous-turn-resilience.md` 落库后的已知残留风险，用一个统一的 `Aborter` 接口变更 + 两处运行时内部改动闭合。

**Hard invariant:**
- 失败处置不得阻塞 watcher goroutine（`startAutonomousWatcher` 是每会话单 goroutine 顺序处理，一轮处置卡住会波及该会话所有后续自主轮；`TestDriveAutonomousTurn_PersistFailure_InterruptDoesNotBlockWatcher` 明令禁止）。
- `driveAutonomousTurn` / `driveSubagentActivity` 在 drain 事件期间绝不持 chat 会话锁（与 `pkg/claudecode.Session` 常驻 reader 死锁）。
- 不得为「修中断超时」而把错误升级成 `handle.Close()` + 逐出缓存——那会杀掉一个活着的子进程。

## Problem

1. **跨轮 abort race：`Aborter` 没有轮身份，迟到的中断会打到错的轮上。** `internal/pkg/agentruntime/runner.go:552` 的 `Abort(ctx, sessionID)` 只按 sessionID 寻址，语义是「中断该会话**当前活跃的那一轮**」（claudecode `runtimes/claudecode/runtime.go:242` 用 `inTurn || outOfBandActive()` 判定，无任何轮标识）。`failAutonomousTurnPersist` 的中断是异步发出的（`autonomous_turn.go:281` `go func() { _ = s.requestRuntimeAbort(ctx, be, sessionID) }()`），与第 4 步 `drainAndDiscard` 无先后约束：若 drain 结束、CLI 起了**下一轮**（另一个自主轮 / 用户刚发的新消息）后这条迟到中断才落地，它会把错的一轮杀掉。`Stop` 的 `abortOutOfBandTurn`（`chat.go:1822`）同理：`session.Find` 到 Abort 落地之间发生轮切换就打偏。piagent 已有「generation 不得被旧代清理误删」的运行时内防（`runtimes/piagent/runtime_test.go:456` `TestRun_StaleCleanupCannotUnregisterNewerGeneration`），但那是**运行时内部**的陈旧清理保护，调用方依然无法表达「中断某一轮」——接口层没有任何轮身份。

2. **无界 interrupt ctx：不回应中断的 CLI 会把 goroutine 挂到子进程死。** `pkg/claudecode/session.go:769` 的 `Interrupt` 写完 `control_request{interrupt}` 后阻塞在 `select { ctx.Done() / readerDone / 回执 }`，**没有 deadline**。调用方两个都无界：`failAutonomousTurnPersist` 传的是 `context.Background()`（来自 `startAutonomousWatcher`），CLI 不回执就泄漏一个 goroutine 直到子进程死；`Stop` 传的是 Wails 的 `a.ctx`（`internal/app/chat.go:107`），同样无 deadline。**直接加 deadline 更糟**：`runtimes/claudecode/runtime.go:248-252` 把**任何** `Interrupt` 错误（含超时）升级成 `_ = a.handle.Close(ctx)` + `r.cache.Remove(...)`，超时等于杀掉一个活着的子进程。唯一的有界先例 `piStopAbortWriteBound = 500ms`（`chat.go:70`）只用在 Pi 的 graceful aborter 上。

3. **带外中断后会话状态无人 reconcile：中断 subagent 活动轮会让会话停在 running/waiting。** `reconcileOrphanStop`（`chat.go:1791`）对带外轮走「中断它，状态留给那一轮自己收尾」：`abortOutOfBandTurn` 成功即返回 `{Stopped: true}`、**不落库**。该假设对自主轮成立（`driveAutonomousTurn` 收尾翻 idle/error），对 **subagent 活动轮不成立**——`driveSubagentActivity` 按设计不碰会话状态（`subagent_activity.go:56-57`「不翻 session running——会话保持 idle」，全文件不写 `AgentStatus`）。于是中断了一个 subagent 活动轮后，running/waiting 的会话停在原态，要再点一次停止（此时轮已结束 → `ErrNoActiveTurn` → reconcile 回 idle）才恢复。现有测试 `TestStop_OutOfBandTurnIsInterruptedNotReconciled`（`chat_test.go:8613`）用通用 `abortRecordingRunner`，只断言「中断下发 + 不 reconcile」，**不区分自主轮 / subagent 活动轮**，该缺口既存在也没被钉死。

## Actors and user stories

1. 作为使用 Agentre 的开发者，当一轮自主续轮落库失败被中断时，我要这条中断**只作用于失败的那一轮**，这样我不会刚发出一条新消息就被一条迟到的旧中断杀掉。
2. 作为使用 Agentre 的开发者，当 CLI 不回应中断时，我要中断路径**有界返回**且**不杀子进程**，这样桌面端不会泄漏 goroutine、也不会把健康子进程逐出缓存。
3. 作为使用 Agentre 的开发者，当我在带外轮在飞时点「停止」，我要会话状态**一次点停就收干净**，这样我不必再点一次停止才能让侧栏脱离 running/waiting。

## Design decisions

| # | Decision | Basis and rejected option |
|---|---|---|
| 1 | `Aborter.Abort` 增加 per-turn token：`Abort(ctx, sessionID, turnToken uint64)`，`0` = 当前活跃轮（用户点停止语义不变），非 0 = 仅当该轮仍是当前轮才中断、否则 stale no-op；token 由各 runtime 按轮递增并随 `RunResult` / `AutonomousTurn` / `SubagentActivity` 暴露给调用方 | 只有目标明确的内部中断（`failAutonomousTurnPersist`）需要精确寻址；用户面向的 Stop 语义本就是「停当前活跃轮」，用 0 保持。**拒绝**：不改接口、drain 后 join goroutine —— 会重新阻塞 watcher，被 `InterruptDoesNotBlockWatcher` 明令禁止（见问题 2 的挂起）；**拒绝**：在 consumer 加「abort 前再查当前轮」防御 —— runtime 不暴露轮身份，consumer 无从判断，治标不治本；**拒绝**：另加 `AbortTurn` 方法 —— 与既有 `Abort` 语义重叠，调用方还得先判断轮类型，同 #28 拒绝 `AbortOutOfBand` 的理由 |
| 2 | `pkg/claudecode.Session.Interrupt` 的 ack 等待加**有界**超时（`interruptAckBound = 500ms`，与 `piStopAbortWriteBound` 同值，真实 CLI 观察只做校准），超时返回**独立错误** `ErrInterruptPending`；`claudecode.Runtime.Abort` 把 `ErrInterruptPending` 当作「中断已下发、ack 异步到」→ 返回 nil、**不** Close+evict；只有真正的错误（写帧失败 / session closed / CLI 拒绝）才保留 Close+evict 兜底 | 关键事实：ack 回执只能由常驻 readLoop 派发，而 readLoop 可能正停在给本轮 feed 事件上——ack 天然晚于 drain 完成。有界 ack + 不杀子进程后，超时路径变成「中断已发、drain 继续、readLoop 追上来后 ack 到、turn 自然收尾」，正好闭合 `failAutonomousTurnPersist` 的异步形态。**拒绝**：在调用方包 `context.WithTimeout` —— 超时是普通 error，`runtime.go:248` 直接 Close+evict 杀活子进程；**拒绝**：维持现状（无 deadline）—— background ctx 永久挂 goroutine；**拒绝**：Interrupt 完全 fire-and-forget（写完帧即返）—— 丢掉「CLI 拒绝 / 无活跃轮」信号，`reconcileOrphanStop` 的 `ErrNoActiveTurn` 判据失效 |
| 3 | `Abort` 返回值从 `error` 扩为「被中断轮的类型」，`reconcileOrphanStop` 按类型接管会话状态：中断的是自主轮 → 保持「留给轮自己收尾」；中断的是 subagent 活动轮 → 自己 reconcile 回 idle；`ErrNoActiveTurn` → 既有遗孤路径不变 | 让「谁拥有会话状态」显式化：自主轮自己收尾（`driveAutonomousTurn` 翻 idle/error）、subagent 活动轮不碰状态、缺口由发起中断的 `reconcileOrphanStop` 补上。**拒绝**：让 `driveSubagentActivity` 收尾时无条件 reconcile —— 它正常发生在 idle 态（no-op），但 running/waiting 态可能是合法状态（`awaitingPlanAction` / 自动接续窗口），盲目翻 idle 会 clobber；**拒绝**：`reconcileOrphanStop` 中断后无条件立即 reconcile（反向推翻 #28 的 deferral）—— 与自主轮收尾 race（自主轮可能翻 error，被后写的 idle 盖掉），且 CLI 尾帧还在流时就谎报 idle |
| 4 | 决策 1 与决策 3 的接口变更**合并为一次** `Aborter` 变更（token + 被中断轮类型），不拆成两次 | 二者都落在同一个接口的同一批实现（6 个真 runtime + mock + `remote/wire` + daemon handler）上，合并只改一次。**拒绝**：分两次独立改 —— 同一批文件动两遍，中间态还要维护兼容 |

## 决策 1：Aborter 增加 per-turn token（接口 / 行为变更 + 反例）

**行为契约（变更后）**

- `Abort(ctx, sessionID, turnToken)`：`turnToken == 0` → 「中断该会话当前活跃的那一轮」（等价于现行为，用户点停止的语义与 UX 不变）；`turnToken != 0` → 「仅当该轮仍是当前活跃轮时才中断」，否则视为 stale、返回 nil（no-op），不触碰任何其它轮。
- token 由 runtime 按轮生成：每个 `Run`（用户轮）、每轮自主续轮、每轮 subagent 活动入口递增一次会话级计数器（claudecode 在 `claudeActive` 上加 `turnSeq atomic.Uint64`；codex / builtin / openclaw 在各自的 `active[sessionID]` 上加同款；remote 透传 token 并落在 wire 上）。生成的 token 随 `agentruntime.RunResult` / `AutonomousTurn` / `SubagentActivity` 暴露给 `chat_svc`。
- 唯一需要**精确寻址**的调用方是 `failAutonomousTurnPersist`：在落库失败时捕获 `at.Token`，异步 abort 携带它——drain 完成后即使新轮已起，迟到中断也只会 no-op，不会杀掉新轮。用户面向路径（`Stop` 的活跃用户轮 / `abortOutOfBandTurn`）一律传 `0`。

**接口波及面（合并决策 1 + 3 一次改完）**

- `internal/pkg/agentruntime/runner.go:552` `Aborter`：签名 + 返回值。
- 6 个真 runtime 的 `Abort`：claudecode / codex / builtin / piagent / openclaw / remote（外加 `mock_agentruntime` 的 `MockAborter`）。
- 远端链路：`remote/wire/wire.go:303` `AbortParams` 加 token 字段 + `internal/daemon/handlers/runtime.go:955` daemon 侧 Abort 透传。
- `chat_svc` 三处调用点：`Stop`（活跃用户轮）、`abortOutOfBandTurn`、`failAutonomousTurnPersist`。

**反例（rejected）**

- **drain 后 join 异步 abort goroutine**：那条 goroutine 阻塞在 `Interrupt` 的 ack 等待（问题 2 的无界形态），在 watcher 里 join 它 = 把 watcher 挂死，违反 Hard invariant，且被本轮既有测试 `InterruptDoesNotBlockWatcher` 明令禁止。
- **consumer 侧防御（abort 前再查当前轮是否同一轮）**：runtime 不暴露轮身份，`chat_svc` 无从比较；且这是在消费方掩盖生产者（runtime）缺身份的问题，违反「修根因不掩蔽」。
- **不改接口、接受「迟到的中断杀掉新轮」**：正是本次要修的可观察危害——用户刚发出的新消息被一条无主中断腰斩，且无法从日志区分（无轮身份可查）。
- **给每轮起独立子进程（换 session 隔离轮）**：推翻 #28 的常驻进程复用，代价远超收益。

## 决策 2：Interrupt 有界 ack + `ErrInterruptPending` 不杀子进程（行为变更 + 反例）

**行为契约（变更后）**

- `Session.Interrupt(ctx)`：写完 `control_request{interrupt}` 后等 ack，但等的时间**有界**（`interruptAckBound = 500ms`，与 `piStopAbortWriteBound` 同值）。超时返回**独立错误** `ErrInterruptPending`，帧已写、中断在途，CLI 处理到后会自然收尾。取值的唯一未定项是「ack 依赖 readLoop 追平 drain 的派发延迟是否能在 500ms 内到达」，由收尾时的真实 CLI 观察**校准**；若实测不足，作为独立修订走审批，不静默放宽。
- `claudecode.Runtime.Abort` 对 `ErrInterruptPending`：视作「中断已下发」→ 返回 nil（配合决策 3，向 `chat_svc` 报告被中断轮的类型），**不** `Close(ctx)`、**不** `cache.Remove`。只有写帧失败 / session closed / CLI 明确拒绝等**真错误**才保留 `runtime.go:248-252` 的 Close+evict 兜底（那些情形下子进程确实焊死/已死，杀掉才合理）。
- 效果闭环（`failAutonomousTurnPersist`）：中断异步发出 → ack 有界等待超时 → `ErrInterruptPending` → 不杀子进程 → watcher 继续第 4 步 drain → readLoop 追上来派发 ack → turn 以 result 收尾。桌面端不挂 goroutine、不杀活子进程、轮照常收尾。
- 同款无界形态存在于 `StopTask`（`session.go:827` 的 select）——同一修复类，一并纳入本轮（StopBackgroundTask 同样不能因超时杀子进程）。

**反例（rejected）**

- **调用方包 `context.WithTimeout`**：超时是普通 error，`runtime.go:248` 直接 `Close` + `cache.Remove`——杀掉一个活着的子进程，正是本轮 Hard invariant 禁止的。
- **维持现状（无 deadline）**：`failAutonomousTurnPersist` 的 background ctx 永久挂一个 goroutine 到子进程死；`Stop` 的 `a.ctx` 同理。
- **Interrupt 完全 fire-and-forget**：写完帧立刻返回，丢失「CLI 拒绝 / 无活跃轮」信号，`reconcileOrphanStop` 依赖的 `ErrNoActiveTurn` 判据塌掉（无法区分「真有一轮被中断」与「没轮可中断」）。
- **有界 ack + 超时当普通错误但单独豁免 Close**：等价于为超时单开一条不杀路径，但错误类型不区分，其它调用方（看门狗等）会误判为真错误——不如显式 `ErrInterruptPending` 类型语义清晰。

## 决策 3：Abort 上报被中断轮类型，`reconcileOrphanStop` 按类型接管（行为变更 + 反例）

**会话状态归属契约（变更后）**

| 被中断的轮 | 谁 reconcile 会话状态 | 终态 |
|---|---|---|
| 用户轮 | `runTurn` / finalize | idle / error / waiting（既有） |
| 自主续轮 | `driveAutonomousTurn` 收尾 | idle / error（既有） |
| subagent 活动轮 | **`reconcileOrphanStop`（新增）** | idle |
| 无轮（`ErrNoActiveTurn`） | `reconcileOrphanStop` 遗孤路径 | idle（既有） |

- `Abort` 返回被中断轮的类型（决策 1 的同一接口变更顺带携带）。`reconcileOrphanStop` 分支：
  - 中断的是自主轮 → 维持现状：返回 `{Stopped: true}`，状态留给 `driveAutonomousTurn` 收尾（它一定会写 idle/error）。
  - 中断的是 subagent 活动轮 → **自己**把会话翻 idle 并持久化（复用遗孤路径的翻写逻辑）——用户点了停止、没有用户轮在飞，running/waiting 已无合法依据；subagent 活动轮的 `driveSubagentActivity` 不写状态，缺口由这里补上。
  - `ErrNoActiveTurn` / 解析不出 runner → 既有遗孤 reconcile 路径不变。
- 一次点停止即可收干净，不再需要第二次点停止。
- **作用范围（用户确认）**：本变更仅作用于**带外路径**（`reconcileOrphanStop`）；`Stop` 的**活跃用户轮**路径（用户轮刚结束、abort 落在活动轮上）维持现状，仍走既有 `finalize` 收尾，不新增接管。

**反例（rejected）**

- **让 `driveSubagentActivity` 收尾时无条件 reconcile**：活动轮**正常**发生在 idle 态，翻 idle 是 no-op；但 running/waiting 可能是**合法**状态（`awaitingPlanAction` 在等计划批准、自动接续窗口保持 running），subagent 活动轮收尾时盲目翻 idle 会 clobber 掉合法状态。subagent 活动轮**无法**从自身判断「会话的 running/waiting 是否因我而存在」，所以不能由它收尾。
- **`reconcileOrphanStop` 中断后无条件立即 reconcile**（反向推翻 #28 的「留给轮收尾」）：与自主轮的收尾 race——自主轮可能翻 `error`（中断 → `StopErr`），被后写的 idle 盖掉；且 CLI 尾帧还在流时就谎报 idle，正是 #28 明确拒绝的形态。按类型分流后，自主轮仍留给它自己收尾，规避了这两个 race。
- **维持现状**：会话卡 running/waiting、要第二次点停止才 reconcile——本次要修的可见缺陷。

## 会话状态归属与中止的边界

- **用户面向的 Stop 语义不变**：`turnToken=0` = 「停当前活跃轮」，用户点停止时无论当前活跃的是用户轮 / 自主轮 / subagent 活动轮，都中断它（沿用 #28 的 `inTurn || outOfBandActive` 判据）。
- **内部面向的中断是精确寻址**：`failAutonomousTurnPersist` 携带失败轮的 token，只中断那一轮，绝不波及之后新起的轮。
- **终态收敛**：所有被中断的轮，其会话终态最终收敛到 idle 或 error（error 仅当轮带 `StopErr`），且每一步翻转都是既有的 last-write-wins 持久化，不引入新的状态值。
- **失败路径**：`reconcileOrphanStop` 接管翻 idle 的那次写若失败，只记日志、不重试、不阻塞（与遗孤路径现状一致）；`ErrInterruptPending` 路径不改变任何 DB 状态。

## Out of scope

- **带外轮活性检测**（帧流长时间静默的兜底）——与上一轮 `autonomous-turn-resilience.md` 的 Out of scope 一致，判据必须排除「`waiting` 态合法等待用户回答」的情形，设计比本轮微妙，需要单独一轮。
- **远端 Pi 的 `abortGeneration` 内部 generation gate 重构**——它已有 session 对象身份校验，本次只让它适配新接口（透传 token / 返回轮类型），不重写其陈旧清理逻辑。
- **`driveSubagentActivity` 的三处 `drainAndDiscard`**（找不到发起消息 / 加载会话失败）——同类静默丢弃但不在中断链上，维持上一轮 Out of scope。
- **恢复已卡死的历史会话现场**——手工操作，不由代码交付。

## Testing decisions

| Seam | What it verifies | Prior art |
|---|---|---|
| `claudecode.Runtime.Abort`（fake handle 注入 token + ack 延迟） | 带 token 的 abort 在轮已切换时 no-op、不中断新轮；`ErrInterruptPending` 返回 nil 且不 Close/不 evict；`turnToken=0` 行为不变 | `runtimes/claudecode/runtime_test.go` 既有 `TestRuntime_Abort`（`runtime_test.go:733` 钉死 `inTurn \|\| outOfBandActive`） |
| `pkg/claudecode.Session.Interrupt`（fake proc 不回 ack） | 有界 ack 超时返回 `ErrInterruptPending`（errors.Is 可判），写帧只发生一次；正常回执路径仍返 nil | `pkg/claudecode/` 既有 session 测试 |
| `chatSvc.failAutonomousTurnPersist`（repo mock + abortRecordingRunner 带 token） | 异步 abort 携带失败轮的 token；模拟「drain 后新轮已起」时迟到中断 no-op、新轮不受影响 | `autonomous_turn_test.go` 既有 `InterruptDoesNotBlockWatcher` |
| `chatSvc.Stop` / `reconcileOrphanStop`（mock runner 返回「被中断的是 subagent 活动轮」） | 一次点停止即把 running/waiting reconcile 回 idle 并持久化；自主轮分支仍不落库（维持既有测试）；`ErrNoActiveTurn` 遗孤路径不变 | `chat_test.go:8613` `TestStop_OutOfBandTurnIsInterruptedNotReconciled`（需扩为按类型断言） |
| `remote` wire + daemon handler | `AbortParams` 携带 token；daemon 侧透传并返回轮类型 | `remote/runtime_test.go`、`daemon/integration_test.go` 既有 abort 用例 |
| mock 再生成 | `mock_agentruntime.MockAborter` 随接口签名更新 | `make mock` |

单元测试覆盖不到的边界（收尾时源码审查 + 一次真实启动观察承担）：

- 有界 ack 的**真实时长**取决于 CLI 在 readLoop 忙时的派发延迟。取值已定为 500ms，真实 CLI 观察只负责**校准**该值是否够 readLoop 追平后派发 ack；若实测不足，作为独立修订走审批，不静默放宽。
- 跨轮 race 的**精确交错**难以做成常驻用例，由 token 语义的单元测试 + 代码审查覆盖。

## Links

- 上一轮：`docs/specs/2026-08-07-autonomous-turn-resilience.md`（三条残留风险来自其落库后；决策 8 放宽 Abort 活跃判据、`reconcileOrphanStop` 的「留给轮收尾」是本轮决策 3 的直接上下文）
- 关键实现点：`internal/pkg/agentruntime/runner.go:552`（Aborter）、`pkg/claudecode/session.go:735/769`（Interrupt 无界 select）、`runtimes/claudecode/runtime.go:236-255`（Abort + Close/evict）、`internal/service/chat_svc/autonomous_turn.go:281`（异步 abort）、`internal/service/chat_svc/chat.go:1791-1845`（reconcileOrphanStop / abortOutOfBandTurn）
- 既有保护：`internal/service/chat_svc/autonomous_turn_test.go` `TestDriveAutonomousTurn_PersistFailure_InterruptDoesNotBlockWatcher`；`internal/service/chat_svc/chat_test.go:8613` `TestStop_OutOfBandTurnIsInterruptedNotReconciled`
- 先例：`piStopAbortWriteBound = 500ms`（`internal/service/chat_svc/chat.go:70`）；piagent generation 防陈旧清理（`runtimes/piagent/runtime_test.go:456`）

## Open questions

无。
