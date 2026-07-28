# Pi Agent 使用原生 Session 存储

状态：已批准（用户于 2026-07-28 确认）<br>
创建：2026-07-28<br>
更新：2026-07-28

## 问题与目标

Agentre 当前为 Pi Agent 显式传入 `--session-dir`，并把每个 `chat_sessions.id` 映射为 `<AppDataDir>/piagent/sessions/agentre-<id>.jsonl`。这重复实现了 Pi 已原生提供的 Session 存储、ID、恢复和查询机制，也使 Agentre 必须维护一套额外的路径规则。

本规格的目标是：**Agentre 不再为 Pi 配置自定义 Session 存储位置或自定义 JSONL 文件名；Pi 自己在其默认（或用户自行配置的）Session 根目录中创建和保存 Session，Agentre 只持久化 Pi 返回的原生 Session ID，并在后续轮次用该 ID 恢复。**

这是一次直接切换。已有 Agentre 专用 Pi JSONL 不迁移、不回退、不继续读取；已有 Agentre 聊天记录仍保留，但对应的模型上下文会在切换后的下一次普通发送时从新的 Pi 原生 Session 开始。

## 参与者与用户故事

- **Agentre 用户**：希望 Pi Agent 使用 Pi 自己的 Session 管理方式，不产生 Agentre 私有的 Session 存储约定。
- **Agentre Pi runtime**：负责启动 Pi RPC、取得原生 Session ID，并通过 `RunResult.ProviderSessionID` 把它交还给桌面端持久化。
- **Pi CLI**：负责选择默认 Session 根目录、创建 JSONL、按原生 Session ID 恢复，以及报告 Session 状态。

## 用户流程

### 新的或直接切换后的会话

1. 用户在 Agentre 的 Pi Agent 聊天中发送消息。
2. 当 `chat_sessions.provider_session_id` 为空时，Agentre 启动 Pi RPC，但不传 `--session-dir` 或 `--session`。
3. Agentre 在发送 prompt 前通过 Pi RPC `get_state` 读取非空的 `sessionId`，并把该 ID 作为本轮 `RunResult.ProviderSessionID` 返回和持久化。
4. Pi 将 Session 保存到自己的默认目录；如果用户通过 Pi 自己的环境变量配置了 Session 根目录，则遵循用户配置。
5. 后续轮次以 `pi --session <原生 Session ID>` 恢复同一 Pi Session，仍不传 `--session-dir`。

### 已有 Agentre 专用 Session

1. 升级时不扫描、不复制、不移动、不删除 `<AppDataDir>/piagent/sessions/agentre-*.jsonl`。
2. 已有 Pi 聊天若没有 `provider_session_id`，下一次普通发送按“新会话”流程创建新的 Pi 原生 Session。
3. Agentre 中已有的用户消息、助手消息、标题和状态保持不变；只有 Pi 模型可见的历史上下文从新 Session 开始。
4. 在首次普通发送成功取得原生 Session ID 之前，手动压缩不可用，并返回现有“没有可压缩的 provider session”错误。

### 原生 Session 丢失

1. 如果数据库中已有 Pi Session ID，但 Pi 报告该 ID 不存在，当前轮不得静默创建替代 Session。
2. Agentre 清空失效的 `provider_session_id`，向用户返回现有“CLI 会话已过期”错误。
3. 用户下一次重新发送时创建新的 Pi 原生 Session。

## 范围

- Pi 普通聊天 runtime 停止传入 `--session-dir` 和自定义 JSONL 路径。
- 首轮在发送 prompt 前读取 Pi RPC `get_state.sessionId`，并尽早返回、持久化原生 Session ID。
- 后续普通发送、自动续轮和显式 compact 通过原生 Session ID 恢复。
- “复制启动命令”对已有原生 Session 使用 `--session <id>`；无 ID 时输出普通 `pi` 命令。
- Pi 连通性探测使用临时、不持久化的 `--no-session`，不污染用户的 Pi Session 列表。
- 原生 Session 不存在时提供可恢复的 session-gone 行为。
- 更新 Pi backend 的生活文档，使 Session mode 与实际实现一致。

## 非目标

- 不迁移、导入、复制、重命名或删除已有 `<AppDataDir>/piagent/sessions/agentre-*.jsonl`。
- 不从已有 JSONL 反查并补写 `provider_session_id`。
- 不把 Agentre 聊天 transcript 回放进新的 Pi Session。
- 不改变 Pi 自己的默认 Session 根目录规则，也不覆盖用户自行设置的 `PI_CODING_AGENT_SESSION_DIR`。
- 不修改 Pi 原生 JSONL 格式、Session ID 格式、分支、compact 或恢复算法。
- 不改变 Claude Code、Codex、builtin runtime 或前端 UI。
- 不把本改动混入正在进行的 Pi 自动压缩结算修复；该计划完成后再执行本规格。

## 行为要求

### R1 — Pi 拥有 Session 存储位置

Agentre **MUST NOT** 为普通 Pi 聊天、自动续轮、显式 compact 或复制启动命令传入 `--session-dir`，也不得为新会话生成 `agentre-<chatSessionID>.jsonl` 路径。

- 没有原生 Session ID的首轮不得传 `--session`。
- 有原生 Session ID 的后续轮次只传 `--session <id>`。
- 用户自己通过 Pi 环境配置的 Session 根目录继续生效，Agentre 不覆盖它。

### R2 — 在 prompt 前取得并持久化原生 Session ID

Pi RPC 客户端 **MUST** 在发送首个 prompt 或 compact 命令前调用 `get_state`，读取非空 `sessionId`。

- 该 ID 必须进入 `RunResult.ProviderSessionID`，并在事件流结束前即可由 chat service 持久化。
- `get_state` 失败、进程提前退出或返回空 `sessionId` 时，当前命令必须失败，且不得继续发送 prompt/compact，避免产生无法恢复的未跟踪 Session。
- 日志只能记录 Session ID 和错误，不得记录 Session 文件内容或凭证。

### R3 — 后续轮次按原生 ID 恢复

当 `chat_sessions.provider_session_id` 非空时，Agentre **MUST** 将完整 ID作为 `--session` 参数传给 Pi，并确认 `get_state` 返回了非空 Session ID。

- 普通发送、自动续轮和显式 compact 使用同一恢复规则。
- 复制启动命令使用相同 ID，不再包含 Agentre 数据目录路径。
- 不得使用 `pi -c` 或“最近 Session”选择，因为它不能稳定绑定到指定 Agentre 聊天。

### R4 — 原生 Session 丢失时显式失败并允许下一次重建

如果 Pi 对 `--session <id>` 报告 Session 不存在，Agentre **MUST**：

1. 将该错误分类为 provider Session 已失效；
2. 清空并持久化当前 `chat_sessions.provider_session_id`；
3. 当前轮返回现有本地化的 Session 过期错误，不在同一轮静默重试；
4. 下一次普通发送按 R1/R2 创建新的 Pi 原生 Session。

其他 `get_state`、启动或传输错误不得错误地清空仍可能有效的 Session ID。

### R5 — 直接切换且不兼容旧专用 JSONL

Agentre **MUST NOT** 为已有专用 JSONL 提供迁移或运行时 fallback。

- 旧文件保持字节不变并留在原位置，但新代码不读取它们。
- 已有 Agentre Pi 聊天在 `provider_session_id` 为空时，从下一次普通发送开始建立新 Pi 上下文。
- 已有 Agentre transcript 不得被删除或修改。
- 回滚旧代码后可以重新读取旧专用 JSONL；新版本期间产生的原生 Pi Session 不会自动合并回旧文件。

### R6 — 探测与 compact 不制造错误 Session

- Pi 连通性探测 **MUST** 使用 `--no-session`，测试结束后不得在 Pi 默认 Session 目录留下新 JSONL。
- Pi 显式 compact **MUST** 要求已有非空 `provider_session_id`；没有 ID 时返回现有 `ChatCompactNoSession` 语义，不得新建空 Session 后压缩。
- 有有效 ID 时，compact 必须按 R3 恢复并保持现有 compact 事件与完成语义。

## 状态、数据与接口

- `chat_sessions.provider_session_id` 对 Pi backend 的含义从“通常为空，恢复依赖 Agentre 确定路径”变为“Pi 原生 Session ID”。字段结构和数据库 schema 不变。
- `RunResult.ProviderSessionID` 继续作为 runtime → chat service 的跨本地/远端状态回传接口，不允许 runtime 反向依赖 repository。
- Pi RPC `get_state` 是 Session ID 的权威来源；不得从文件名、Agentre chat ID 或环境变量推导原生 ID。
- 远端执行仍由运行 Pi 的 daemon 读取原生 ID并回传；桌面只存 ID，不存或推导远端绝对文件路径。
- 启动命令和 runtime 共用同一恢复语义，避免“应用内继续的是 A，复制命令打开的是 B”。

## 持久化数据变化

本改动不修改 schema，也不批量改写或删除数据，但会改变既有 Session 数据的使用方式，因此按 **needs migration（明确选择不迁移的直接切换）** 管理。

| 变化 | 影响级别 | 影响既有数据 | 迁移与回滚 |
| --- | --- | --- | --- |
| `<AppDataDir>/piagent/sessions/agentre-*.jsonl` 从“活跃恢复来源”变为“新代码忽略的遗留文件” | needs migration | 每个安装中全部已有 Agentre 专用 Pi JSONL；当前开发机在 2026-07-28 17:00 +0800 的只读快照为 42 个文件、约 22 MB，该数量会随旧代码继续运行而增长 | 不执行迁移；不移动、不删除、不改写。回滚旧代码后重新作为恢复来源 |
| Pi backend 的 `chat_sessions.provider_session_id` 从“通常为空”变为“Pi 原生 Session ID” | compatible for future writes | 既有空值不批量更新；用户下一次普通发送成功后按现有 session 更新流程写入新 ID | 回滚旧代码可读取该字段但会恢复旧专用 JSONL；再次升级后可继续使用已写入的原生 ID |

- **不可逆部分：本次没有。** 旧 JSONL 和数据库 transcript 均不删除。直接切换造成的是上下文可见性断开，不是文件内容丢失。
- **备份方案：** 因实现不写旧目录，不要求自动备份；验收必须在操作前后记录遗留目录文件数量、总大小和代表性文件 SHA-256，证明字节未变。
- **回滚窗口：** 代码可随时回滚。回滚后恢复旧 JSONL 上下文，但新版本期间写入原生 Pi Session 的新增上下文不会自动出现在旧 JSONL 中；再次升级可按数据库中的原生 ID继续。
- **滚动版本：** 新桌面配旧 daemon 时，旧 daemon 仍按旧路径执行，升级 daemon 后下一轮直接切到原生 Session；旧桌面配新 daemon 时，现有 `RunResult.ProviderSessionID` 回传链路可保存原生 ID。两种组合都不迁移旧 JSONL，也不得删除数据。

## 安全、隐私、兼容性与可访问性

- **安全与隐私：** 不新增数据上传；Session ID 可进入结构化日志，但 Session 文件内容、prompt、token 和凭证不得作为本改动的诊断证据落盘。
- **文件权限：** 原生 Session 文件权限与目录由 Pi 自己管理；Agentre 不创建或 chmod Pi Session 根目录。
- **兼容性：** 以当前 Pi 文档化的 `--session <path|id>`、RPC `get_state.sessionId` 和 `--no-session` 契约为基线。旧 Agentre 专用 JSONL 明确不兼容。
- **可访问性：** 无 UI、交互控件或文案变化，不适用。

## 实现决策

1. **存 Pi 原生 Session ID，不存原生绝对文件路径。** ID 是 Pi RPC 明确暴露的稳定身份，也能通过现有远端 wire 回传；绝对路径会把桌面绑定到执行机器的文件系统。
2. **首个 prompt 前同步读取 `get_state`。** 拒绝只在 turn 结束后的 `get_session_stats` 才取 ID，因为应用崩溃、用户中止或长 turn 会留下尚未持久化身份的 Session。
3. **Session ID 缺失时 fail closed。** 拒绝继续发送 prompt 后再“尽量保存”，因为下一轮无法确定恢复目标。
4. **不使用 `-c`/最近 Session。** 最近项会受同一 cwd 下其他 Pi 实例影响，不能保证 Agentre chat 与 Pi Session 一一对应。
5. **探测使用 `--no-session`。** 拒绝让测试调用使用默认持久化，因为“测试连接”不应污染用户的 Session 选择器。
6. **保留通用 Pi client 的自定义 `WithSessionDir` 能力，但 Agentre 产品代码不调用它。** `pkg/piagent` 是可复用封装，删除通用参数支持不是本需求；产品侧新增明确的 ephemeral/no-session 选项供探测使用。
7. **Session gone 沿用现有 provider-session 恢复模式。** 当前轮显式报错并清 ID，下一轮才新建，避免用户不知情地在同一轮丢失上下文。
8. **先完成正在进行的自动压缩结算计划，再实施本规格。** 两项都触及 Pi RPC stream/runtime，串行执行可避免测试和终止语义互相覆盖。

明确拒绝：

- 继续维护 `<AppDataDir>/piagent/sessions`，即使只作为“临时兼容”。
- 自动扫描旧 JSONL 并导入 Pi 默认目录。
- 用 Agentre 数字 chat ID推导 Pi Session ID。
- 发生恢复错误时在同一轮静默创建新 Session。
- 通过回放 Agentre transcript 伪造 Pi 原生历史。

## 测试接缝

- **`pkg/piagent` 脚本化进程边界：** fake process runner 断言 argv、接收 `get_state`、返回 Session ID，并确认 prompt/compact 的发送顺序；覆盖 state 失败、空 ID、Session 不存在和 `--no-session`。
- **Pi runtime 边界：** fake Pi client/session 观察 `RunResult.ProviderSessionID` 在事件流开始前可用、后续请求按 ID恢复、普通能力不回归。
- **chat service 边界：** repository mock 断言 Pi 的原生 ID在 runner-start 持久化；Session gone 清 ID；无 ID compact 被拒绝，有 ID compact 放行。
- **启动命令边界：** Pi 命令有 ID时只包含 `--session <id>`，无 ID时不含任何 Session flag，所有情况均不含 `--session-dir` 和 Agentre 数据路径。
- **只读遗留数据检查：** 在隔离副本或当前遗留目录上记录文件计数、总大小和 SHA-256，执行验证后保持一致；不向旧 JSONL 追加测试消息。
- **真实 Pi RPC 集成：** 在隔离环境中启动当前 Pi，验证无 `--session-dir` 时 `get_state` 返回原生 ID、同一 ID可被 `--session` 恢复、`--no-session` 不产生文件。GUI E2E fake runtime 无法观察真实 Pi argv，因此不作为本规格的主验收接缝。

## 验收标准

### A1（R1、R2）首轮使用 Pi 默认存储并取得原生 ID

Given 一个 `provider_session_id` 为空的 Pi 聊天；<br>
When 用户发起普通发送；<br>
Then Pi 进程 argv 不包含 `--session-dir` 或 `--session`，客户端先收到成功的 `get_state` 和非空 `sessionId`，再发送 prompt，并将该 ID写入 `RunResult.ProviderSessionID`。

### A2（R2）无法取得身份时不发送 prompt

Given Pi 的 `get_state` 失败、进程提前退出或返回空 `sessionId`；<br>
When Agentre 尝试开始普通发送；<br>
Then 当前调用返回错误，stdin 中没有 prompt 命令，数据库中不写入伪造或空的 provider Session ID。

### A3（R2）原生 ID在流结束前持久化

Given Pi RPC 已返回原生 Session ID，但 assistant turn 仍在运行；<br>
When runtime 把 `RunResult` 交给 chat service；<br>
Then chat service 在 runner-start 路径持久化该 ID，应用无需等待事件流关闭即可恢复这个 Session。

### A4（R3）后续普通发送和自动续轮恢复同一 Session

Given `chat_sessions.provider_session_id` 已保存一个有效 Pi 原生 ID；<br>
When 用户发送下一条消息或 Agentre 发起自动续轮；<br>
Then Pi argv 包含且只包含 `--session <该完整 ID>` 这一项 Session 选择参数，`get_state` 返回同一 Session 身份，历史上下文可见。

### A5（R3）复制启动命令与应用内恢复一致

Given 当前 Pi 聊天已有原生 Session ID；<br>
When 用户获取可复制的启动命令；<br>
Then 命令包含 `pi --session <id>`，不包含 `--session-dir`、`agentre-<id>.jsonl` 或 Agentre AppDataDir；无原生 ID时命令不含任何 Session flag。

### A6（R4）原生 Session 被删除后可恢复

Given 数据库保存的 Pi 原生 Session ID已不在 Pi Session 存储中；<br>
When 用户发送消息；<br>
Then 当前轮返回 Session 过期错误、数据库中的 ID被清空且不在同一轮静默重试；下一次普通发送创建并保存新的原生 Session ID。

### A7（R5）旧专用 JSONL 被忽略且保持不变

Given 一个已有 Agentre Pi 聊天、空 `provider_session_id` 和对应的 `agentre-<id>.jsonl`；<br>
When 切换后的代码发起下一次普通发送；<br>
Then argv 不引用旧文件，新 Pi 原生 Session 被创建并保存，Agentre transcript 保持原样，旧 JSONL 的文件数量、总大小和代表性 SHA-256 前后不变。

### A8（R5）回滚不声称合并两套历史

Given 新版本已产生原生 Pi Session，旧专用 JSONL 仍在；<br>
When 对回滚行为做兼容性验证；<br>
Then旧代码仍能读取旧 JSONL，文件未被新版本改写；同时验证记录明确说明原生 Session 的新增历史不会自动合并进旧 JSONL。

### A9（R6）连通性探测不产生持久 Session

Given 用户执行 Pi backend 连通性测试；<br>
When probe 启动 Pi；<br>
Then argv 包含 `--no-session` 且不含 `--session-dir`，测试前后 Pi Session 文件集合不增加。

### A10（R6）compact 只作用于已建立的原生 Session

Given 一个没有原生 Session ID的直接切换旧聊天；<br>
When 用户请求 compact；<br>
Then返回现有 `ChatCompactNoSession` 语义且不启动 Pi；Given 同一聊天先完成普通发送并保存原生 ID；When 再请求 compact；Then Pi 通过 `--session <id>` 恢复并保持现有 compact 完成语义。

## 开放问题

无。

## 参考

- Pi Sessions：原生 JSONL 存储、`--session <path|id>`、`--no-session`。
- Pi RPC：`get_state` 返回 `sessionId`/`sessionFile`，`get_session_stats` 返回 Session 统计。
- Pi Session Format：Session header 和原生 Session ID。
- Agentre 当前 Pi runtime、启动命令、probe 与 `RunResult.ProviderSessionID` 状态回传契约。
