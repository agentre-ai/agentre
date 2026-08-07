# 自主续轮落库失败不再静默吞轮

> Status: Approved
> Owner: Agentre 桌面端
> Last updated: 2026-08-07

**Objective:** 自主续轮落库失败时，用户能立刻看到这一轮丢了、会话能继续用，且 claude-code 子进程不会被永久焊死在一个没人回答的交互帧上；同时消除引发这次落库失败的两层数据库锁竞争。

**Hard invariant:** `driveAutonomousTurn` 在 drain 事件期间绝不持 chat 会话锁（见 `internal/service/chat_svc/autonomous_turn.go:26-31` 的并发约束：持锁 drain 会与 `pkg/claudecode.Session` 常驻 reader 死锁）。本轮新增的失败处置全部在 drain 之外或以非阻塞方式进行，不得引入该锁。

## Problem

1. **deferred 事务的写升级撞 SQLITE_BUSY 会立即失败，busy handler 根本不参与。** gorm 的 `Transaction()` 开的是 deferred 事务：先 SELECT 拿读快照、再升级写锁；升级冲突时 SQLite 按规范**不调用 busy handler**，直接返回 SQLITE_BUSY。证据：2026-08-07 11:33:35 的日志中，那条 `INSERT INTO chat_messages` 耗时 **0.218ms** 即报 `database is locked (5) (SQLITE_BUSY)`。本地复现实验进一步确认该形态与 journal 模式无关：WAL 模式下 deferred 事务的写升级同样在 **0.02ms** 失败，而同一条件下改用 `BEGIN IMMEDIATE` 则会等锁至超时（实测 5273ms）——即等锁行为只在 IMMEDIATE 下才发生。

2. **现有 `busy_timeout` 配置是空操作，掩盖了问题 1。** `internal/bootstrap/cago.go:317` 挂的 `_pragma=busy_timeout(5000)`，注释称其解决「并发 turn 流式写库撞 SQLITE_BUSY」。但驱动在建立每个连接时**无条件硬编码执行** `pragma BUSY_TIMEOUT(5000)`（`glebarez/go-sqlite@v1.21.2/sqlite.go:880`，且在处理 `_pragma` 之前），该 DSN 参数因而从未改变过任何行为。事故在 busy_timeout 一直为 5000 的前提下照样发生。

3. **`driveAutonomousTurn` 把落库失败降级成静默丢弃整轮。** `internal/service/chat_svc/autonomous_turn.go:89-94`：事务失败 → 记一条 error 日志 → `drainAndDiscard(at.Events)` → return。不翻会话状态、不 emit、不留任何用户可见痕迹。证据：sess-2627 于 11:33:35 命中该分支后，CLI 在 11:33–11:38 产出的 60+ 帧（Bash/Write/Skill/thinking/text）在 CLI transcript 中完整存在，而 `chat_messages` 表中该时段**零条记录**。

4. **被吞掉的帧里包含需要用户回答的交互帧，导致子进程永久等待。** sess-2627 的 CLI 在 11:38:38 发出 `AskUserQuestion`（tool_use id `toolu_01Dr6CQEJYnEUCZ2kCtLscmn`）。该 id 在 transcript 中只出现 1 次、没有配对的 `tool_result`。后果链：CLI 这一轮永不结束 → 用户 11:59:55 发的消息被 CLI `enqueue` 且至今无 `dequeue` → 桌面端 `claudecode runtime: startup watchdog deferred, out-of-band turn holds the frame stream` 每 2 分钟一条持续 1 小时以上 → 界面永久停在「输出中」。子进程 29911 存活 2h07m，`sdk-cli` 最后一次写盘停在 11:38:38。

5. **`Runtime.Abort` 只认用户轮，无法中断带外轮。** `internal/pkg/agentruntime/runtimes/claudecode/runtime.go:239` 的 `if !a.inTurn.Load() { return ErrNoActiveTurn }`：`inTurn` 仅在用户轮（`Run`）置位，自主续轮在飞时为 false。因此带外轮独占帧流期间，无论是问题 3 的失败处置还是用户手动点「停止」，都拿不到中断能力。

6. **数据库运行在 rollback journal 模式下，读写互斥放大了锁竞争。** 实测 `PRAGMA journal_mode` 为 `delete`、`synchronous` 为 `FULL`。该模式下写事务独占整个数据库、读也被阻塞；当前库 1.5 GB（4096 字节 × 373976 页）且约 10 个会话并发流式写，锁竞争窗口被显著放大。这是问题 1 得以频繁命中的土壤。

## Actors and user stories

1. 作为使用 Agentre 的开发者，当自主续轮因落库失败被丢弃时，我要立刻在会话里看到出错而不是永久转圈，这样我能立即重发而不是一小时后才发现被卡住。
2. 作为使用 Agentre 的开发者，当一轮自主续轮被丢弃后，我要子进程随之解除等待，这样我随后发的消息能立刻被处理，而不是排在一个永不出队的队列里。
3. 作为使用 Agentre 的开发者，我要多个会话同时流式写库时不再互相把对方的事务挤失败，这样落库失败本身就成为罕见事件。

## Design decisions

| # | Decision | Basis and rejected option |
|---|---|---|
| 1 | SQLite DSN 追加 `_txlock=immediate`，令所有事务以 `BEGIN IMMEDIATE` 开启 | `glebarez/go-sqlite@v1.21.2/sqlite.go:902` 解析该 DSN 参数；`glebarez/sqlite@v1.11.0/sqlite.go:45` 把 DSN 原样透传给 `sql.Open`。BEGIN 时即取写锁，冲突走 busy handler，5 秒等待窗口随之真正生效（实测对比见问题 1）。**拒绝**：在应用层给 `Transaction()` 包重试 —— 17 处调用点都要改，且没消除升级竞态本身，只是掩盖 |
| 2 | 同时删去 DSN 里的 `_pragma=busy_timeout(5000)` | 驱动已无条件硬编码同值（问题 2），保留它会让读代码的人以为超时是本项目配置的、可调的。**拒绝**：保留作为「显式声明」 —— 它是假的显式，驱动改默认值时这里也不会跟着生效 |
| 3 | 切换 journal 模式为 WAL，令读写不再互斥 | 问题 6。实测 1.5 GB 规模的转换耗时 **0.004 秒**（只改文件头，不重写数据），成本可忽略。**拒绝**：只做决策 1 不动 journal 模式 —— 那只消除升级竞态，写事务之间因读写互斥而排队的土壤仍在 |
| 4 | WAL 转换在启动时显式执行一次并容错，而非挂进 DSN 的 `_pragma` | `_pragma` 由驱动在**每个**连接建立时执行，而首次转换需要独占锁：实测并发连接下 `PRAGMA journal_mode=WAL` 抛 `database is locked`，挂 DSN 会让该次连接建立失败进而阻断启动。journal_mode 是持久化的**数据库**属性，一个连接上执行即永久生效，正是 `sqliteDSN` 现有注释「启动后 Exec PRAGMA 只作用单个连接」的例外。**拒绝**：挂 DSN —— 用启动失败换取一行代码 |
| 5 | DSN 追加 `_pragma=synchronous(NORMAL)` | WAL 模式下 `NORMAL` 仍然崩溃安全（进程崩溃不会损坏数据库），只在断电/内核崩溃时可能丢失最后若干已提交事务，是 WAL 写性能收益的主要来源。当前为 `FULL`。**拒绝**：保持 `FULL` —— 对一个本地桌面应用的聊天记录，每事务一次 fsync 的代价换不回等价的可靠性收益 |
| 6 | 落库最终失败时：会话翻 `error` + 经会话级流推错误 + 主动中断 CLI 这一轮 | 用户决策。**拒绝**：只翻 error 不碰 CLI —— 子进程仍会像 sess-2627 一样被焊死，用户后续消息永远排队 |
| 7 | 错误提示走内存 emitter 的会话级流，不新建错误消息落库 | 落库已经失败，再写一条错误消息大概率同样失败，会退化成第二次静默。会话级流 `AutonomousStreamName(sessionID)` 由 ChatPanel 挂载即订阅、常驻，先于本轮存在（见 `autonomous_turn.go:220-228` 的同款兜底理由） |
| 8 | 放宽 `Runtime.Abort` 的活跃判据为 `inTurn \|\| outOfBandActive()`，而非新增一个 `AbortOutOfBand` | `Abort` 的契约是「中断该会话当前活跃的那一轮」，带外轮就是活跃轮，`inTurn` 只覆盖用户轮是契约遗漏而非设计。顺带修好「带外轮在飞时点停止停不掉」。**拒绝**：新增方法 —— 两个方法语义重叠，调用方还得先判断轮的类型才知道调哪个 |
| 9 | 会话状态用既有的 `error`，不引入新状态值 | `chat_entity.Session` 已有 `idle`/`running`/`waiting`/`error` 四态，`driveAutonomousTurn:178-181` 收尾本就在 `idle`/`error` 之间选。**拒绝**：新增 `dropped` 态 —— 前端、sidebar、CountActive 都要跟着改，收益为零 |

## SQLite 事务与锁定行为

数据库连接以 `BEGIN IMMEDIATE` 开启事务：任一事务在 BEGIN 时即请求写锁，写锁被占时等待至多 5 秒，超时才返回错误。此前 deferred 事务在写升级冲突时 0.2ms 即失败的行为不再出现。

该改动对读路径零影响，依据两点已核对的事实：本仓库 17 处 `.Transaction()` 调用点**全部是写事务**（含 6 处经跨包 repo 调用间接写的），本来就要取写锁，IMMEDIATE 只是把取锁时机从「升级时」提前到「BEGIN 时」；而 repository 层的普通读（`Find`/`List` 等）走 autocommit 单条语句，不经过显式 `BEGIN`，`_txlock` 对其不适用。因此 WAL 带来的读写并发收益不会被 IMMEDIATE 抵消。

## journal 模式

启动时对数据库执行一次 WAL 转换。转换成功后该属性持久写入数据库文件，后续启动无须重复生效判断。

转换失败（典型：转换时刻另有连接持锁）不阻断启动，记录一条可检索的警告后继续以当前 journal 模式运行，下次启动重试。这是本轮必须明确承担的失败路径：应用的可用性不能取决于一次性能优化是否成功。

## 自主续轮落库失败时的可观察结果

前置：自主续轮开始，桌面端为其新建 assistant 消息的事务在重试后仍然失败。

动作与可观察结果：

- 会话状态翻为 `error` 并持久化。失败的是消息写入事务；会话状态写入是独立的一次写，允许其独立成功。若连它也失败，记录日志后继续后续步骤，不再重试。
- 经会话级流推送一条错误事件，前端在该会话中显示这一轮出错。文案沿用既有的 `mapTurnError` 一套，与用户发起的轮次一致。
- 主动中断 CLI 当前这一轮，使子进程解除等待。中断失败（含子进程已消失）只记日志，不影响前两步已经产生的可观察结果。
- 事件流仍被抽干。这是 Hard invariant 要求的底层约束：出口 channel 无人 drain 会导致 `Session` 活跃槽位不释放，后续用户轮全部卡死。抽干发生在上述处置之后。

失败处置本身不得抛出或阻塞 watcher goroutine：`startAutonomousWatcher` 是每会话单 goroutine 顺序处理，一轮的失败处置卡住会波及该会话所有后续自主轮。

## 中断带外轮

`Runtime.Abort(ctx, sessionID)` 在该会话存在活跃用户轮**或**存在活跃带外轮时执行中断；两者都没有时返回 `ErrNoActiveTurn`（契约不变）。

由此产生的第二个可观察改善：带外轮独占帧流期间，用户点「停止」能真正中断这一轮，而不是像此前那样因 `inTurn` 为 false 被判定为无活跃轮。

## 兼容性与运维影响

`_txlock` / `synchronous` 只影响连接行为，不改变数据库文件格式，回退去掉 DSN 参数即可。

WAL 是持久化的数据库属性，且会在数据库旁产生 `-wal` / `-shm` 两个附属文件。由此产生两点运维影响，需要在贡献者文档中写明：

- **直接 `cp` 数据库文件的备份方式不再取得一致副本**，必须连同附属文件一起复制，或改用 `VACUUM INTO`（对当前 1.5 GB 库实测 9 秒）。
- **打开数据库需要对其所在目录有写权限**（要创建 `-shm`），拷贝到只读介质后无法直接打开。

WAL 不支持网络文件系统；`AppDataDir` 在三个平台上均为本地路径，不受影响。回退到 rollback journal 需要显式执行 `PRAGMA journal_mode=DELETE`，不会自动发生。

## Out of scope

- **带外轮活性检测（帧流长时间静默的兜底）** —— 用户决定先不处理。它的判据必须排除「会话处于 `waiting`、正在合法等待用户回答」的情形，设计上比本轮微妙，需要单独一轮。本轮修复后，问题 4 那条因落库失败而起的通路已经闭合：落库成功时 `AskUserQuestion` 走共享 dispatcher（`internal/service/chat_svc/dispatcher_adapters.go:199` → `markSessionWaiting`）正常浮出卡片并翻 `waiting`。
- **`driveSubagentActivity` 的三处 `drainAndDiscard`**（`subagent_activity.go:60,68,76`）—— 同类静默丢弃，但触发条件是「找不到发起消息 / 加载会话失败」而非落库失败，且后台 subagent 活动流不产生需要用户回答的帧。不在本轮事故链上。
- **数据库体积治理** —— 1.5 GB 是锁竞争的背景之一，但压缩历史属于独立课题。
- **恢复已卡死的 sess-2627 现场** —— 手工操作，不由代码交付。

## Testing decisions

| Seam | What it verifies | Prior art |
|---|---|---|
| `chatSvc.driveAutonomousTurn`（repo mock 注入落库失败） | 落库失败时：会话翻 `error`、会话级流收到错误事件、发出中断请求、事件流仍被抽干 | `internal/service/chat_svc/autonomous_turn_test.go` 既有 6 个用例，含 `TestDriveAutonomousTurn_TruncatedTurn_PersistsTerminatedNotCompleted` 的 error 态断言 |
| `claudecode.Runtime.Abort` | 仅带外轮活跃（`inTurn=false`、`outOfBandActive=true`）时执行中断而非返回 `ErrNoActiveTurn`；两者皆无时契约不变 | `internal/pkg/agentruntime/runtimes/claudecode/` 既有 runtime 测试 |
| `bootstrap.sqliteDSN` | DSN 携带 `_txlock=immediate` 与 `synchronous(NORMAL)`，且不再携带冗余的 `busy_timeout` | `internal/bootstrap/` 既有测试 |
| 启动时的 WAL 转换 | 转换失败时记录警告并继续启动，不返回错误 | `internal/bootstrap/cago_test.go`（既有的真实临时 SQLite 例外之一） |

两项无法用单元测试稳定覆盖，由收尾时的源码审查与一次真实启动观察承担：

- `_txlock=immediate` 真正消除升级竞态需要精确的多连接写升级交错，不适合做成常驻用例；已由本轮的本地复现实验证实（0.02ms 立即失败 vs 等锁至 5273ms），审查时确认 DSN 参数未被 cago 的配置装配路径丢弃。
- WAL 实际生效需要观察真实启动后的 `PRAGMA journal_mode` 与附属文件产生情况。

## Links

- 事故会话：sess-2627，provider session `657dd61e-7b69-4152-864e-280d639f4580`
- 关键日志行：`chat_svc/autonomous_turn.go:90` @ 2026-08-07T11:33:35.382+0800

## Open questions

无。
