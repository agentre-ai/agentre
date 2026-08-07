# Pi Agent 重新生成与编辑消息

状态：已批准（用户于 2026-08-05 确认）<br>
创建：2026-08-05<br>
更新：2026-08-05

## 问题与目标

Agentre 已有通用的“重新生成 assistant 回复”和“编辑 user 消息后重发”流程，但 Pi Agent runtime 尚未接入其 provider Session 分叉能力：

- Pi runtime 没有声明 `CapForkSession`；
- `RunRequest.ForkAnchor` 没有传给 Pi；
- Pi user 消息的原生 Session entry ID 没有写入 `RunResult.UserAnchor` / `chat_messages.fork_anchor`；
- chat service 因而对已有 provider Session 的 Pi 重新生成或编辑返回不支持。

当前 Pi `0.83.0` 的原生 Session 是由 `id` / `parentId` 组成的树，RPC 提供 `fork { entryId }`、`get_entries`、`get_tree`、`get_fork_messages` 和 `get_state`。已在隔离临时 Session 中验证：从第二条 user entry 调用 `fork` 会创建新的原生 Session ID，并让新 Session 的活动路径停在该 user 消息之前。

本规格的目标是：**把 Pi 原生 user entry ID 作为 Agentre 的 fork anchor，使 Pi Agent 精确支持现有的重新生成与编辑消息交互；分叉后继续使用 Pi 返回的新原生 Session ID。**

## 用户流程

### 重新生成 assistant 回复

1. 用户点击某条 Pi assistant 消息的“重新生成”。
2. Agentre 找到该 assistant 之前对应的 user 消息，并读取其 `fork_anchor`。
3. Pi runtime 恢复当前 `provider_session_id`，在同一个 RPC 进程内调用 `fork(entryId)`。
4. Pi 创建新的原生 Session，活动路径停在目标 user 消息之前。
5. Agentre 在同一进程内重新发送原 user 文本与图片。
6. 新 assistant 回复沿现有流式事件链显示；fork 后的新 Pi Session ID覆盖写入 `chat_sessions.provider_session_id`。
7. 新 user entry ID写入新 user 消息的 `fork_anchor`，供后续继续编辑或重新生成。

### 编辑 user 消息

1. 用户点击某条 Pi user 消息的“编辑”，修改文本并确认。
2. Agentre 使用该 user 消息原有的 `fork_anchor` 调用同一 fork 流程。
3. Pi 分叉到原 user 消息之前，Agentre 发送编辑后的新文本。
4. 本地 transcript 从目标 user 起替换为新 user + 新 assistant；Pi 原生 Session 切换到 fork 后的新 ID。

### 升级前的旧 Pi 消息

1. 已有 Pi 消息没有保存原生 user entry ID，其 `fork_anchor` 为空。
2. 用户尝试从这类旧消息重新生成或编辑时，Agentre 显式返回现有“找不到用户锚点”错误。
3. 实现不按文本、顺序、时间戳或 turn 数猜测旧 entry ID，也不创建缺失前文的新 Session。
4. 升级后在已有 Pi Session 中新发送的消息会取得 anchor；这些新消息可正常编辑和重新生成。

## 范围

- `pkg/piagent` 支持在普通 prompt 前调用 RPC `fork(entryId)`。
- fork 和随后的 prompt 必须复用同一个 Pi RPC 进程。
- fork 后通过 `get_state` 取得新的 Pi 原生 Session ID。
- 普通 Pi turn 在发送 prompt 前记录原生 Session tree 的 leaf，并在 turn 结束后读取 entries，精确解析本轮首条 user entry ID。
- Pi runtime 将 `RunRequest.ForkAnchor` 传给 client，并返回 `RunResult.ProviderSessionID` 与 `RunResult.UserAnchor`。
- Pi runtime 声明 `CapForkSession`。
- chat service 对 Pi 复用现有 Regenerate / Edit 截断和重发流程，并要求已有 provider Session 的目标 user 消息具备 anchor。
- 更新 Pi backend 能力文档及相关测试。

## 非目标

- 不兼容、不迁移、不回填升级前已有 Pi 消息的空 `fork_anchor`。
- 不按 user 文本匹配 entry；重复文本不得成为身份依据。
- 不按 Agentre user 消息序号映射 Pi user entry；mid-turn steer 会使两边序列不等价。
- 不修改 Pi 原生 JSONL，不自行实现 Session tree 或 fork 文件。
- 不在 fork 失败时回退成空白 Session，也不回放 Agentre transcript 伪造上下文。
- 不改变 Claude Code、Codex、builtin 的重新生成或编辑语义。
- 不增加数据库字段、迁移或远端 wire 字段。
- 不新增或重做前端按钮、编辑器、提示文案或布局。
- 不实现通用 Pi extension UI dialog 桥接；Pi extension 主动取消 fork 时只按取消失败处理。

## 行为要求

### R1 — fork 与 prompt 在同一个 Pi RPC 进程中完成

当 `RunRequest.ForkAnchor` 非空时，Pi client **MUST**：

1. 按当前 `provider_session_id` 启动并恢复 Pi RPC；
2. 在发送 prompt 前调用 `fork { entryId: <ForkAnchor> }`；
3. 确认 fork 成功且未被取消；
4. 调用 `get_state` 取得 fork 后的非空新 Session ID；
5. 在同一个进程中发送本轮 prompt。

不得先 fork、关闭进程、再按新 Session ID启动另一个进程，因为从第一条 user 消息之前分叉时，新空 Session 可能尚未写出可供第二个进程恢复的文件。

当 `ForkAnchor` 为空时，普通发送保持现有新建或恢复 Session 行为，不调用 fork。

### R2 — 精确记录本轮 Pi user entry ID

需要记录 anchor 的普通 Pi turn **MUST**：

1. 在发送 prompt 前读取当前 Session entries 的活动 leaf；
2. turn 终止后再次读取 entries 与新 leaf；
3. 沿 `parentId` 从新 leaf 回溯到 prompt 前 leaf；
4. 按正向路径选择本轮产生的第一条 `message.role == "user"` entry；
5. 将其稳定 `id` 返回为 `RunResult.UserAnchor`。

该算法必须按树结构和边界识别 entry，不得按文本匹配。它应能区分：

- 历史中重复出现的相同 prompt；
- 本轮原始 user prompt 之后产生的 steer / follow-up user entry；
- fork 后保留下来的旧路径 entries；
- prompt 前发生的 compaction 或 custom entry。

如果 prompt 被 Pi extension 完全处理、没有产生普通 user entry，或终止元数据不可用，则 `UserAnchor` 可以为空，但不得伪造 ID。

### R3 — Agentre 复用现有状态字段

实现 **MUST** 复用现有接口与持久化字段：

- `RunRequest.ForkAnchor`：目标 Pi user entry ID；
- `RunResult.ProviderSessionID`：本轮实际使用的 Pi 原生 Session ID，fork 后必须是新 ID；
- `RunResult.UserAnchor`：新 prompt 对应的 Pi user entry ID；
- `chat_sessions.provider_session_id`：保存当前 Pi 原生 Session ID；
- `chat_messages.fork_anchor`：保存对应 user 消息的 Pi entry ID。

fork 创建新 Session 时，保留路径中的旧 entries 继续使用其原始 ID，因此 Agentre 中未被截断的旧消息 anchor 无需改写。

不得让 Pi runtime 直接依赖 chat repository；状态继续通过通用 `RunRequest` / `RunResult` 跨本地和远端执行链路传递。

### R4 — chat service 对 Pi 使用精确 anchor，旧数据 fail closed

对于已有非空 `provider_session_id` 的 Pi Session：

- Regenerate 必须把目标 assistant 前 user 消息的 `fork_anchor` 传给 runner；
- Edit 必须把目标 user 消息的 `fork_anchor` 传给 runner；
- anchor 为空时必须在截断和启动新 turn 之前返回现有 `ChatRegenerateNoUserAnchor` 语义；
- 不得返回原有 `ChatRegenerateUnsupported`；
- 不得清空 provider Session 后按全新上下文继续。

对于尚无 provider Session 的失败首轮，沿用现有“没有 provider fork 动作”的行为，不为不存在的 Session 制造 anchor。

### R5 — capability 与现有 UI 对齐

Pi runtime **MUST** 声明 `CapForkSession`，使 backend 能力查询反映已经实现的 provider Session 分叉能力。

现有 transcript 已提供 assistant regenerate 和 user edit 入口，本规格不新增前端交互。能力声明、服务行为和实际 runtime 支持必须一致。

### R6 — 失败保持显式且不伪造成功

- Pi `fork` RPC 返回失败、未知 entry、`cancelled: true`、进程退出或超时时，当前 runtime 启动失败且不得发送 prompt。
- fork 后 `get_state` 返回空 ID或仍返回旧 ID时，当前 runtime 启动失败且不得发送 prompt。
- 失败不得改用最近 Session、空 Session、文本匹配或 transcript 回放继续。
- 如果 assistant turn 已经完成，但结束阶段读取 user anchor 元数据失败，必须保留已经生成的回答；该 user 消息不写伪造 anchor，之后的 Edit / Regenerate 按 R4 显式拒绝。
- 错误和诊断日志可以记录 Session ID、entry ID和 RPC command，但不得记录 prompt、图片、Session 文件内容或凭证。

## 状态、数据与接口

### Pi 原生 Session tree

- Session entry 的 `id` 是本规格的 provider anchor。
- `parentId` 定义活动路径与分支关系。
- `get_entries` 的 `leafId` 是 prompt 前后边界的权威来源。
- `fork(entryId)` 的语义是创建新的 Session，并把活动路径放在所选 user entry 之前。
- fork 后 `get_state.sessionId` 是新的 provider Session 身份，不从文件名或旧 ID推导。

### Agentre 持久化

本改动没有 schema 变化，也不批量修改已有数据。

| 数据 | 新行为 | 旧数据处理 |
| --- | --- | --- |
| `chat_messages.fork_anchor` | 新 Pi user 消息保存原生 entry ID | 既有空值保持为空，不回填 |
| `chat_sessions.provider_session_id` | fork 后覆盖为新的 Pi 原生 Session ID | 既有 ID继续作为 fork 来源 Session |
| 本地 transcript | 继续由现有 Regenerate / Edit 事务截断并创建新消息 | 不主动改写；只有用户触发操作时按现有流程变化 |

持久化分类为 **compatible future writes**：未来写入开始携带 anchor；没有数据库迁移、批量任务或不可逆数据改写。

### 远端执行

`RunRequest.ForkAnchor`、`RunResult.UserAnchor` 和 `RunResult.ProviderSessionID` 已存在于通用 runtime 契约。远端 Pi 由 daemon 在拥有原生 Session 的机器上执行 fork，并把结果通过现有 wire 返回桌面；桌面不读取或推导远端 Session 文件。

## 实现决策

1. **使用 Pi RPC `fork`，不直接修改 JSONL。** Pi 自己负责 extension hook、路径复制、Session header 和活动 leaf。
2. **fork 与 prompt 复用一个进程。** 这是第一条 user 消息也能可靠重跑的必要条件。
3. **entry ID 是唯一 anchor。** 文本、顺序和时间戳只描述内容或位置，不能稳定标识树节点。
4. **用 prompt 前 leaf + prompt 后 ancestry 找新 user entry。** 只保存一个前置边界，并能把原始 prompt 与后续 steer 区分开。
5. **不兼容旧空 anchor。** 用户已明确批准直接切换；不接受可能分叉错误的 heuristic。
6. **元数据失败不否定已完成回答。** anchor 是后续操作所需状态，不应在 assistant 内容已经生成后删除或伪装本轮结果。
7. **复用通用 runtime 状态回传。** 不新增 Pi 专用数据库接口或 daemon payload。
8. **不修改前端。** 已有 UI 已调用同一 Regenerate / Edit service；本轮只补齐 provider 能力。

明确拒绝：

- 通过 `get_fork_messages` 的文本字段反查目标；
- 用第 N 条 Agentre user 消息匹配第 N 条 Pi user entry；
- 读取、编辑或复制 Pi JSONL 来制造分支；
- fork 失败后清空 `provider_session_id` 并静默重试；
- 为旧消息生成假的 anchor；
- 把 Pi fork 逻辑放进 Wails binding 或 chat repository。

## 失败与恢复

| 失败 | 当前操作 | 后续恢复 |
| --- | --- | --- |
| 目标消息没有 anchor | Regenerate / Edit 在服务层拒绝，不截断本地历史 | 用户从升级后产生、具备 anchor 的消息操作 |
| Pi Session 不存在 | 沿用现有 `ErrSessionNotFound` / provider Session 失效处理 | 下一次普通发送按现有恢复规则建立新 Session |
| fork entry 不存在或 RPC 失败 | 不发送 prompt，返回 runtime 错误 | 本地历史若尚未截断则保持；按现有错误 UI重试或另发消息 |
| fork 被 extension 取消 | 视为 fork 失败，不发送 prompt | 用户调整 Pi extension 后重试 |
| fork 后 Session ID无效 | fail closed，不发送 prompt | 保留诊断并重试；不得猜测 ID |
| turn 完成但 user anchor 未解析 | 回答照常完成，`UserAnchor` 为空 | 该条消息后续 Regenerate / Edit 被显式拒绝 |

## 测试接缝

### `pkg/piagent` 脚本化 RPC 进程

- 捕获 stdin，断言恢复 state → fork → 新 state → prompt 的顺序。
- 断言 fork 和 prompt 发生在同一个 fake process。
- 构造 fork `cancelled: true`、失败 response、空/未变化 Session ID，断言 prompt 未发送。
- 构造 prompt 前后 `get_entries`：包含重复 user 文本、custom/compaction entry 和后续 steer，断言选择树边界后的第一条 user entry ID。
- 覆盖没有新 user entry 与结束元数据失败时 anchor 为空、已生成内容仍完成。

### Pi runtime

- fake session 观察 `RunRequest.ForkAnchor` 被传入 Pi client adapter。
- scripted stream 返回新 Session ID与 user anchor，断言 `RunResult.ProviderSessionID` / `UserAnchor`。
- capability 测试断言 `CapForkSession == true`。
- fork/session gone 错误继续映射到 backend-neutral runtime error，不破坏 Close 与流结算。

### chat service

- Regenerate(Pi) 对有 provider Session + 有 anchor 的消息透传 entry ID，并沿现有截断路径启动新 turn。
- Edit(Pi) 复用相同 anchor 规则并发送新文本。
- 有 provider Session + 空 anchor 时，在 repository truncate / runner start 前返回 `ChatRegenerateNoUserAnchor`。
- runner 返回的新 provider Session ID与 user anchor 继续由现有持久化测试覆盖。

### 文档与静态检查

- Pi backend 能力表与 runtime capability 测试一致。
- 运行聚焦 Go 测试、`make test-backend` 和 `make lint`；前端无改动，不新增 frontend 测试。
- 使用 staged-tree 文档验证脚本检查新增规格链接与事实。

### 真实 Pi RPC 验证

在隔离临时 Session 中使用当前 Pi RPC：

1. 构造至少两轮 user/assistant entries；
2. 对第二条 user entry 调用 `fork`；
3. 断言 fork 成功、新 Session ID不同、活动路径不包含被选 user 及其后续回复；
4. 在同一进程继续 prompt 的完整网络模型调用不是本规格机械测试的前置条件；生产代码由脚本化 RPC 测试证明 command 顺序，真实 smoke 只证明 Pi 原生 fork 契约。

不得在验证中修改用户真实 Pi Session 或记录真实 prompt 内容。

## 验收标准

### A1（R1）重新生成在同一进程 fork 后重发

Given 一个已有原生 Session ID且目标 user 消息有 Pi entry anchor 的会话；<br>
When 用户重新生成其后的 assistant 回复；<br>
Then Pi client 在同一个 RPC 进程中先调用 `fork(entryId)`、取得不同的非空 Session ID，再发送原 user 文本，且 `RunResult.ProviderSessionID` 为新 ID。

### A2（R1、R3）编辑消息 fork 后发送新文本

Given 一个已有 Pi anchor 的 user 消息；<br>
When 用户将其文本从 A 编辑为 B并确认；<br>
Then Agentre 以该 anchor 分叉、发送 B而不是 A，本地 transcript 从目标 user 起替换，并保存 fork 后的新 Pi Session ID。

### A3（R2）重复文本与 steer 不影响新 anchor

Given 历史中已有与当前 prompt 相同的文本，且当前 turn 在原始 prompt 后又产生 steer user entry；<br>
When turn 结束并解析 entries；<br>
Then `RunResult.UserAnchor` 等于 prompt 前 leaf 之后路径中的第一条 user entry ID，既不是历史重复文本的 ID，也不是 steer 的 ID。

### A4（R3）新 anchor 可连续分叉

Given 一次 Regenerate 或 Edit 已完成并返回新 user entry ID；<br>
When chat service 持久化本轮结果；<br>
Then新 user 消息的 `fork_anchor` 等于该 ID，未被截断的旧消息 anchor 保持不变，之后可以再次从任一保留且有 anchor 的 user 消息分叉。

### A5（R4）旧空 anchor 明确拒绝且不破坏历史

Given 一个已有非空 Pi provider Session ID、但目标 user 消息 `fork_anchor` 为空的旧会话；<br>
When 用户尝试 Regenerate 或 Edit；<br>
Then返回 `ChatRegenerateNoUserAnchor`，不调用 runner、不截断数据库消息、不清空 provider Session，也不按文本猜测 entry。

### A6（R5）能力查询声明 Pi 支持分叉

Given 前端或其他调用方查询 Pi backend capabilities；<br>
When runtime capability 被序列化；<br>
Then结果包含 `fork_session`，且实际 Regenerate / Edit 路径不再返回 Pi unsupported。

### A7（R6）fork 失败不发送 prompt

Given Pi 对 fork 返回失败、取消、空新 Session ID或与旧 ID相同；<br>
When Agentre 尝试从 anchor 重跑；<br>
Then runtime 返回错误，fake process stdin 中 fork 之后没有 prompt，且没有创建伪造的 `ProviderSessionID` / `UserAnchor`。

### A8（R6）anchor 元数据失败不删除已完成回答

Given Pi 已正常生成并结算 assistant 回复，但结束阶段无法解析本轮 user entry；<br>
When runtime 完成 turn；<br>
Then assistant 内容与正常完成状态保留，`RunResult.UserAnchor` 为空且无伪造 ID，后续从该消息分叉按 A5 拒绝。

## 开放问题

无。

## 参考

- Pi RPC：`fork`、`get_entries`、`get_tree`、`get_fork_messages`、`get_state`。
- Pi Sessions / Session Format：原生 Session tree、entry `id` / `parentId` 与 fork 行为。
- Agentre `docs/specs/2026-07-28-piagent-native-sessions.md`：Pi 原生 Session ID持久化契约。
- Agentre 通用 runtime `RunRequest.ForkAnchor` / `RunResult.UserAnchor` / `RunResult.ProviderSessionID`。
- Agentre 现有 Regenerate / Edit service 与 transcript UI。
