# Claude Code 后台任务「自主续轮」捕获设计

**日期**: 2026-06-04
**状态**: 设计完成,待 review
**作者**: Claude (结对 with 王一之)

## 背景

Claude Code 后端的会话,跑「后台任务 + 完成后继续」这类指令会**续不上对话**。复现样本 `sess-359`(title「后台跑一下 sleep 10 完成后看看目录」):

- agentre DB 里这个会话只有 **2 条消息**:user prompt + 一条 assistant「`sleep 10` 已在后台启动(ID `bed0ohka5`),完成后我会收到通知,然后列目录给你看。」会话停在 `idle`,`provider_session_id` 正常写入。
- 但 claude CLI 自己的 transcript(`~/.claude/projects/.../c67f1089-….jsonl`)显示**整条流程其实跑完了**:01:42:40 第一条 assistant → 01:42:47 CLI 自己 enqueue `<task-notification>` → 01:42:50 assistant「sleep 10 完成了,看一下目录」+ `ls` → 01:43:00 完整目录列表。

也就是说:**用户要的目录列表 CLI 真的生成了,但 agentre 完全没收到。**

### 根因(已用真实 CLI 复现确认)

用真实 `claude` CLI 2.1.162、与 agentre 同样的 stream-json 持久 stdin 方式复现,抓到**两个 `result` 帧**,第二轮完全自主(我没写任何 stdin):

```
line 1–44  turn1:启动 sleep 3 (run_in_background) → 「已在后台启动」
line 45    result success            ← result #1,agentre 在这里 finalize 本轮
line 46    system task_notification  ← 后台任务完成,CLI 自主开新一轮
line 47–80 init → assistant 跑 ls → 目录列表
line 81    result success            ← result #2
```

链条两端都已坐实:

1. **生产端(CLI)**:turn 以后台任务收尾时,CLI emit `result` #1 结束本轮,但**持久子进程不退**;后台任务完成后,CLI 把 `<task-notification>` 塞进自己的内部队列,**自主跑完整一轮**(`task_notification` → 模型响应 → 工具调用 → `result` #2)写到 stdout。
2. **消费端(agentre)**:
   - `pkg/claudecode/session.go:197` `routeUntilResult` 读到**第一个** `result` 就返回 `done`、关 channel(`session.go:314-322`)。
   - 子进程**跨 turn 复用、正常收尾不 Close**,只 `markIdle`(`runtimes/claudecode/runtime.go:329`、`cache.go`),所以它的 stdout 跨 turn 存活。
   - 自主续轮的 line 46–81 写进 stdout pipe **没人读**。
   - 下一条用户消息 → cache 命中 → `Session.Turn(M2)` 写 M2 到 stdin,但 `routeUntilResult` **先读到滞留的 line 46–81**,撞上**旧的** `result` #2 直接返回 —— CLI 根本还没处理 M2。

**两个症状,同一个根因**:

- **丢答案**:用户要的目录列表从没在 agentre 出现(本轮看着像提前停了)。
- **永久错位(续不上对话)**:此后每条消息都晚一轮 —— M2 拿到滞留的列表,M3 拿到 M2 的答案,以此类推。
- 附带:`task_notification` 在 `translator.go:68-79` 被翻成 `SubagentDone`(它其实是后台 bash 完成,不是 subagent)。

## 目标 / 非目标

**目标**

- claude CLI 自主跑的「后台任务完成续轮」被 agentre 捕获,作为**一条自动追加的 assistant 轮**实时出现在会话里(无 user 行),状态 `running→idle`。
- 彻底消除上面的帧错位:用户后续消息永远干净对应到自己的 turn。
- 远端(`agentred` daemon)派发的 claudecode 会话同样修好。

**非目标**

- 不改变模型「启动后台任务后主动结束本轮」的行为(本轮该结束就结束;续轮是**另一轮**)。
- 不引入「阻塞 turn 等后台任务完成」的方案(后台任务可长可永不退,如 `npm run dev &`)。
- 不动 codex / builtin / piagent —— 它们没有等价的自主续轮协议,不声明新能力即可。

## 已确认的设计决策

1. **续读逻辑放在 `pkg/claudecode.Session` 的常驻 demux reader**(而非「同一 turn 内延长读到 result #2」)。理由:自主续轮可能在 turn 已 finalize 很久后才触发,且后台任务可能永不完成,不能让 turn 卡住或阻塞用户继续发消息。
2. **续轮作为独立的 assistant 轮**(忠实对应 CLI 的真实行为:turn1 已结束,续轮是 turn2),而非塞进下一条用户消息。
3. **远端 v1 即包含**:加 wire 帧,daemon 转发,`remote.Runtime` 实现新接口。

## 架构总览

```
claude 子进程 stdout
   │
   ▼
[Session.readLoop 常驻 goroutine]  ← 单一 reader,占住 scanner 整个生命周期
   ├─ 有活跃 user Turn → 路由到该 Turn 的 channel ──► Run() 事件流(用户驱动)
   └─ 空闲时遇到「后台 task_notification」开头的一轮
        → 路由到 autonomous sink ──► Session.AutonomousTurns() ──► runtime ──► chat_svc watcher
```

新增的「前向」接口(backend→host),区别于现有 7 个全是「反向」(host→backend)的子接口(Steerer / Aborter / SteerDrainer / ToolPermissionSink / AskAnswerSink / PermissionModeSetter)。最接近的旧概念是 `SteerDrainer` 的 auto-continue(`persistAutoContinueTurn` + 递归 `runTurn`,chat.go:2561),但那是 turn 收尾时 pull 一次;自主续轮是异步 push。

## 组件 ① `pkg/claudecode.Session` —— 常驻 demux reader(本案最大改动)

把「每 Turn 一个 `routeUntilResult` 占 scanner」改成「**一个 `readLoop` goroutine 占 scanner 整个子进程生命周期**」:

- `OpenSession` 时启动 `readLoop`,跑到子进程 EOF / `Close`。
- **单一「活跃 sink」槽位**。`Turn()`:抢 turnMu → 注册 user sink 到槽位 → 写 user frame;reader 把帧路由到该 sink 直到 `result` 关闭它、清空槽位、释放 turn slot。
- **Turn 归属规则(时序无关)**:CLI 串行 emit 各轮(每轮以 `result` 收尾,从不交错)。一轮若**以「后台型 `task_notification`」开头**(`subtype:"task_notification"` + `status:"completed"` + `output_file` 落在 `/tasks/<id>.output` + 无 subagent 元数据)→ 判为**自主轮**,路由到新建的 autonomous sink,经 `Session.AutonomousTurns() <-chan *AutoTurn` 吐出;否则按 FIFO 派给 pending 的 `Turn()` 调用。
- 后台型 `task_notification` 被当作**自主轮的起始标记消费掉**,**不**再作为 `EventTaskNotification` 下发 —— 避免误产 `SubagentDone`。
- 顺带简化(单 reader 模型自然带来):`SetPermissionMode` 里「turnMu.TryLock 自己 drain scanner」那段删掉 —— reader 一直在 drain,`control_response` 不论 turn 状态都 dispatch 到 `ctrlPending`。
- 新增 API:`func (s *Session) AutonomousTurns() <-chan *AutoTurn`,`AutoTurn{ Events <-chan Event; SessionID string; Trigger string }`(Trigger 当前固定 `"background_task"`)。

> ⚠️ 这是改动里**风险最高**的一块:涉及 `turnMu` / `stdinMu` / `ctrlPending` 路由与 Interrupt/SetPermissionMode 的既有并发约束。先写 Red 回归测试(见下)再动,逐步验证既有 `session_test.go` 全绿。

### 区分「后台型」vs「subagent 型」task_notification

- **后台型**(开新一轮、在 result 之后 / 槽位空闲时到达):带 `output_file: .../tasks/<id>.output`、`summary` 形如「Background command …」,无 `subagent_type`。
- **subagent 型**(turn 内,活跃 user sink 还在 streaming):带 `subagent_type` / `description`,无 `output_file`。
- 实现用「字段形态 predicate + 槽位状态」双保险。**确切字段集合在 TDD 用真实 capture(两种各一份)钉死。**

## 组件 ② `agentruntime` —— 新前向接口 + 能力

- `runner.go`:
  ```go
  type AutonomousTurnSource interface {
      AutonomousTurns(sessionID int64) <-chan AutonomousTurn
  }
  type AutonomousTurn struct {
      Events  <-chan Event   // 与 Run 同形;result 后 close
      Result  *RunResult     // Events close 后才可读
      Trigger string         // "background_task"
  }
  ```
- `capability/capability.go`:`CapAutonomousTurn = "autonomous_turn"`,加进 claudecode 的 `Capabilities()`、`runtime_test.go` 矩阵断言。
- claudecode runtime 实现 `AutonomousTurns(sessionID)`:把 cache 里该 session 的 `*claudecode.Session.AutonomousTurns()` 桥接成「翻译后的 `agentruntime.Event` 流 + RunResult」。复用既有 `translator` + drain 聚合逻辑(尽量不复制 `Run` 的 drain 循环;抽出共享的「把一个 claudecode event channel 翻译/聚合成 agentruntime event channel + RunResult」helper)。
- 矩阵测试:声明 `CapAutonomousTurn` ↔ 实现 `AutonomousTurnSource` 必须一致(type assert)。

## 组件 ③ `chat_svc` —— 每会话「自主轮 watcher」

- claudecode 会话**首次活跃**(首个 `runTurn` / cache open)时,启动一个 watcher goroutine:
  ```go
  for at := range src.AutonomousTurns(sess.ID) {
      s.driveAutonomousTurn(ctx, sess, be, prov, at)
  }
  ```
- `driveAutonomousTurn`:复用既有 turn drain / handlers / 落库,但建一条**纯 assistant 消息(无 user 行)** —— 即 `persistAutoContinueTurn`(chat.go:2561)去掉 user 消息那部分;实时 stream 给前端;`running→idle`;从 `at.Result` 落 `provider_session_id` / usage。
- **与用户 turn 串行**:自主轮 streaming 期间会话 `running`;用户此时 `Send` 走既有 `Enqueue`/steer 路径(行为不变)。底层 `Session.turnMu` 保证 CLI 一刻只一轮。
- **watcher 生命周期**:`AutonomousTurns` channel close(子进程 evict / Close)时退出。注意避免 goroutine 泄漏 —— 与 cache evict / `CloseSession` 对齐。
- **精确挂载点**(watcher 在哪起、状态如何与 `acc`/active-stream 协作)在实现期定;候选是 `runTurn` 首轮 spawn 后、或 runtime 在 `AutonomousTurns` 首次被订阅时惰性起。

## 组件 ④ remote(`agentred`)—— wire 支持

- 新 wire 通知:`autonomous_turn_started{sessionID, trigger}`、随后该轮的逐事件流(复用 `runtime.event` 编解码、tag 到这一自主轮)、`autonomous_turn_done{RunResult}`。
- daemon 侧:订阅真实 runtime 的 `AutonomousTurns(sessionID)`,把每一轮转发到对应 WebSocket。
- `remote.Runtime` 实现 `AutonomousTurnSource`:把 daemon push 的自主轮还原成 `AutonomousTurn` 值吐给 chat_svc。
- `wire_test` 加新帧的对称 round-trip。
- 注意 `agent-backend.md` 既有约束:runtime 不反向依赖 chat_repo;状态经 `RunResult` 回传;新 sentinel/Event 同步进 `wire.go` 编解码。

## 数据流(端到端)

```
用户「后台跑 sleep 10,完成后看目录」
  → Turn1 → result#1 → finalize idle(assistant:「已在后台启动…」)
  …后台任务完成…
  → CLI 自主轮(task_notification → ls → 目录列表 → result#2)
  → Session.readLoop 判定后台型 task_notification → 路由 autonomous sink
  → claudecode runtime 翻译 → chat_svc watcher driveAutonomousTurn
  → 新 assistant 消息「sleep 10 完成了,目录列表…」实时出现 → idle
后续用户消息 = 干净的 Turn2,无错位。
```

## 错误处理

- 自主轮内部出错 → 落一条带 error 的 assistant 消息,状态回 `idle`(绝不卡死会话)。
- watcher 落库失败 → log + 丢弃 + 回 `idle`(对齐既有 `persistAutoContinueTurn` 失败路径,chat.go:2581)。
- 子进程在自主轮中途死亡 → `Events`/`AutonomousTurns` channel close,watcher 干净退出。
- 远端断链 → `remote.Runtime` 的 `AutonomousTurns` channel close,同上。

## 测试策略(TDD,先 Red)

| 测试 | 位置 | 验证 |
|---|---|---|
| **基石回归** | `pkg/claudecode/session_test.go` | fake process 先吐 `[user turn + result#1]`,再吐 `[后台 task_notification + 自主轮 + result#2]`:(a) Turn1 的 channel 只收到 turn1 事件并在 result#1 close;(b) `AutonomousTurns()` 吐出该自主轮及其事件;(c) 之后 `Turn2` 只收到 turn2 帧(**无错位**)。这条直接覆盖本 bug。 |
| task_notification 辨析 | `pkg/claudecode/session_test.go` | 后台型 vs subagent 型 capture 各一,断言前者起自主轮、后者仍是 turn 内 `EventTaskNotification`。 |
| 能力矩阵 | `runtimes/claudecode/runtime_test.go` | `CapAutonomousTurn` ↔ `AutonomousTurnSource` 一致。 |
| 自主轮翻译 | `runtimes/claudecode/*_test.go` | cache 里 session 的自主轮 → 翻译后的 agentruntime 事件流 + RunResult(provider sid / usage)。 |
| watcher 驱动 | `chat_svc` 单测(mock runtime 实现 `AutonomousTurnSource`) | 驱动一个自主轮 → 落一条**纯 assistant 消息(无 user 行)** + stream + 状态 `running→idle`;落库失败回 idle。 |
| remote wire | `runtimes/remote/wire/wire_test.go` | 新帧对称 round-trip;`remote.Runtime` 吐出自主轮。 |
| daemon registry | `daemon/runtime_imports_test.go` | 既有矩阵仍绿。 |

repo 单测一律 `testutils.Database(t)` + sqlmock;service 单测 mockgen 注入。

## 回滚 / 兼容

agentre **未发布**,可 hard delete 老数据、无需兼容层 / migration 回填。新接口是可选子接口,不声明能力的 backend 完全不受影响。本案**无 DB schema 改动**(自主轮复用 `chat_messages`,纯 assistant 行)。

## 实现期待定细节(TDD/计划阶段钉死,非架构阻塞)

1. 后台型 `task_notification` 的确切字段 predicate(两份真实 capture 对照)。
2. `Session.readLoop` 并发重写细节(turnMu/stdinMu/ctrlPending 路由、Interrupt/SetPermissionMode 协作)。
3. chat_svc watcher 的精确启停挂载点与 goroutine 生命周期(对齐 cache evict / CloseSession)。
4. remote wire 帧的具体 tag 方式(自主轮事件如何与普通 `runtime.event` 区分归属)。
