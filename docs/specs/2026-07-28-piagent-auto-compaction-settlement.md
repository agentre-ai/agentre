# Pi Agent 自动压缩结算兼容性

状态：已确认（用户于 2026-07-28 要求继续执行）<br>
创建：2026-07-28<br>
更新：2026-07-28

## 问题与目标

Agentre 通过 Pi RPC 模式运行 `piagent`。Pi 的 `agent_end` 只表示一次底层 agent run 结束，之后仍可能执行自动重试、自动压缩或排队续轮；`agent_settled` 才表示这些自动动作全部结束。

当前 Agentre 会在部分 `agent_end` 帧上提前结束 RPC 流。同时，用户级 80% 阈值扩展在 `turn_end` 调用 `ctx.compact()`；Pi 的该调用会先中止仍活跃的 agent operation。两者叠加后，工具结果虽然已经产生，后续最终回复可能被中止，Agentre 也可能等不到自己认可的终止帧。

`sess-2223` 已出现该症状：最后一次 assistant 消息以 `toolUse` 结束，工具结果随后落盘，智能压缩也成功写入 compaction 记录，但没有压缩后的最终 assistant 回复；Agentre 最终将会话标记为 `error`。

本规格的目标是：**自动压缩只能发生在完整 agent run 已结算之后，并且 Agentre 必须等待压缩、自动重试和排队续轮全部结束后，才把本次 Pi turn 报告为完成。**

## 参与者与用户故事

- **Agentre 用户**：希望长任务在达到 80% 上下文后自动压缩，但不丢失工具结果之后应继续生成的最终回复。
- **Agentre Pi RPC 客户端**：负责消费 Pi RPC 生命周期并把它翻译为稳定的 runtime 事件。
- **Pi 自动压缩扩展**：负责在实际上下文占用跨过 80% 时发起压缩，并等待压缩完成。

用户流程：

1. 用户通过 Agentre 向 Pi Agent 发起一个可能包含多轮工具调用的请求。
2. Pi 可以发出一个或多个 `agent_end`，并在其后执行重试、自动压缩或续轮。
3. Agentre 持续读取这些事件，不在中间 `agent_end` 上关闭子进程。
4. 当整个 run 已经结算时，80% 阈值扩展检查实际上下文占用；若需要压缩，则发起并等待压缩结束。
5. Pi 在所有自动动作完成后发出 `agent_settled`；Agentre 此时才读取最终 session stats，并向上层报告 Done 或最终 Error。

## 范围

- 修正 Agentre Pi RPC 流的终止语义，使其遵循 `agent_settled`。
- 保留并正确转发 `agent_end` 后发生的 compaction、retry 和 continuation 事件。
- 让用户级 80% 自动压缩扩展从 `turn_end` 迁移到完整结算点，并等待压缩 callback。
- 为成功压缩、重试后成功和最终失败补充回归测试。
- 验证现有 `sess-2223` Pi session 文件仍可由修复后的客户端继续使用，无需改写。

## 非目标

- 不修改 Pi 的原生压缩算法或 `pi-smart-compact` 的 EESV 摘要算法。
- 不改变 80% 阈值、摘要模型或 reasoning level 配置。
- 不新增前端 UI、通知卡片或新的 Wails API。
- 不修复、删除或重写 `sess-2223` 已有的聊天消息、compaction 记录或 Agentre 数据库行。
- 不为缺少 `agent_settled` 的旧版、非当前 Pi RPC 协议实现兼容回退；修复以故障诊断时的 Pi 0.81.1 与当前已安装的 Pi 0.82.1 共同文档化的契约为基线。

## 行为要求

### R1 — 以完整结算事件结束普通 prompt 流

Agentre Pi RPC 客户端 **MUST** 在普通 prompt 流中等待 `agent_settled`，而不是把 `agent_end` 视为最终完成。

- `agent_end` 后的 `compaction_start`、`compaction_end`、retry、续轮消息和工具事件必须继续被消费。
- 收到 `agent_settled` 后，客户端必须读取 session stats，再发出最终 Done。
- 显式 RPC `compact` 命令仍以其 `response{command:"compact"}` 为终止信号，因为该命令不依赖 `agent_settled`。

### R2 — 只报告结算后的最终错误

Agentre Pi RPC 客户端 **MUST** 延迟 `agent_end` 上的 provider/transport error，直到 `agent_settled` 才判断它是否仍是最终错误。

- 如果错误 `agent_end` 后 Pi 自动重试并最终成功，客户端不得向 Agentre 上层报告旧错误。
- 如果结算时最后一个 agent run 仍失败，客户端必须保留现有错误文本、最终错误帧和 stderr tail 诊断，并且不得额外发出 clean Done。
- 如果 Pi 进程在 `agent_settled` 前退出，客户端必须继续报告进程/扫描错误，而不能假装成功。

### R3 — 自动压缩不得中断工具链

80% 阈值扩展 **MUST NOT** 在 `turn_end` 发起压缩。它必须在 Pi 已将 run 标为 settled 的扩展生命周期点检查阈值。

- 达到阈值时，handler 必须等待 `ctx.compact()` 的 `onComplete` 或 `onError` callback 后再返回。
- 压缩进行中不得重复发起第二次压缩。
- 压缩成功或失败后必须解除运行中状态；失败不得让扩展永久卡在 `compacting=true`。
- 上下文占用未达到 80%、占用未知，或刚完成压缩后占用为 null 时不得触发。

### R4 — 保持现有数据和其他后端兼容

修复 **MUST** 只作用于 Pi Agent RPC 生命周期和用户级自动压缩扩展。

- Claude Code、Codex 和 builtin runtime 行为不得改变。
- Pi 显式 compact、图片输入、steer、abort、上下文窗口上报和 MCP bridge 行为不得回归。
- 已存在的 Pi JSONL session（包括 `sess-2223`）必须无需迁移即可继续 resume。

## 状态、错误与恢复

Pi prompt 流维护一个“待结算错误”状态：

- 每个失败的 `agent_end` 更新待结算错误及诊断。
- 后续成功的 `agent_end` 清除待结算错误。
- `agent_settled` 到达时：有待结算错误则发出 Error；没有则读取 stats 并发出 Done。
- compaction/retry/continuation 事件不会自行结束流。

自动压缩扩展维护：上次占用比例、是否正在压缩、上次尝试时间。只有从低于阈值跨到 80% 或更高时触发；session 重载后重新计算。

`sess-2223` 不做自动数据修复。部署修复后，它已有的 compaction summary 仍是有效的 Pi session 边界；用户可以在原会话上继续请求。Agentre 数据库里历史的 `error` 状态由正常的新 turn 生命周期覆盖，而不是直接编辑数据库。

## 持久化数据变化

**本改动不改变持久化数据结构或格式，也不批量改写现有数据。**

- SQLite schema：不变。
- `chat_sessions` / `chat_messages` 内容格式：不变。
- Pi JSONL session/compaction entry 格式：不变。
- 用户级 Pi settings/models 配置值：不变；只修改自动触发扩展的生命周期实现。
- 迁移与回滚：无需数据迁移；代码和扩展均可回滚。已存在的 session 文件在修复前后使用同一格式。

## 兼容性、安全、隐私与可访问性

- **兼容性**：以 Pi 0.81.1–0.82.1 共同文档化的 `agent_settled` RPC 契约为基线；本地验收使用当前安装的 Pi 0.82.1，显式 compact 命令保留独立终止语义。
- **安全与隐私**：不新增数据上传、凭证读取或日志字段；诊断证据不得保存完整 session 内容、API key 或 token。
- **可访问性**：无 UI 变化，不适用；现有前端状态展示只接收更准确的 Done/Error 生命周期。

## 实现决策

1. **选择 `agent_settled` 作为普通 prompt 的唯一 clean terminal**；拒绝继续根据 `agent_end.stopReason` 猜测是否结束，因为 Pi 文档明确声明该事件之后仍可继续自动动作。
2. **错误采用“候选错误，结算时确认”**；拒绝在首个 error `agent_end` 立即返回，因为那会截断 Pi 原生 auto-retry。
3. **扩展迁移到 settled handler 并等待 callback**；拒绝只在 `turn_end` 增加 `ctx.isIdle()` 判断，因为 `turn_end` 本身仍位于活跃 run 内，不能保证工具链已经结束。
4. **不隔离或禁用全局扩展**；Agentre 继续继承用户的 Pi 配置，修复客户端生命周期以真正兼容这些扩展，而不是绕开它们。
5. **不直接修数据库或 JSONL**；现有数据格式有效，根因是运行时终止边界错误。
6. **当前版本验收跟随已安装的 Pi 0.82.1**；2026-07-28 15:33 的外部 `pi update` 将环境从 0.81.1 升级到 0.82.1，后者仍保留相同的 `agent_settled` 契约，故不改变行为要求，只更新本地兼容验证基线。

## 测试接缝

- **Go 模块边界**：`pkg/piagent` 的脚本化 RPC stream 测试，输入完整 JSONL 事件序列，观察 EventDone/EventError、compaction boundary、session stats 和 diagnostics。
- **扩展边界**：用 fake `ExtensionAPI` / `ExtensionContext` 加载用户级触发扩展，观察注册事件、阈值判断、`ctx.compact` 调用时机及 handler Promise 是否等待 callback。
- **本地集成验证**：使用当前真实 Pi 0.82.1 RPC（并保持对故障诊断时 Pi 0.81.1 同一事件契约的覆盖）或等价脚本，覆盖 `agent_end → compaction → agent_settled`，并确认现有 `agentre-2223.jsonl` 可被 Pi 解析/resume；验证时不向原 session 写入测试数据。

## 验收标准

### A1（R1）自动压缩后再完成

Given 一个普通 prompt RPC 序列先发出成功 `agent_end`，随后发出 `compaction_start`、`compaction_end` 和 `agent_settled`；<br>
When Agentre 消费该序列；<br>
Then 它必须保留 compaction boundary，并且只在 `agent_settled` 后读取 stats 和发出 Done。

### A2（R1）中间 agent_end 不终止

Given 一个包含工具调用、多个 `agent_end` 和后续 assistant 输出的序列；<br>
When 中间 `agent_end` 到达；<br>
Then Agentre 必须继续消费后续工具、文本和 continuation，直到 `agent_settled`。

### A3（R2）自动重试成功不泄漏旧错误

Given 第一个 `agent_end` 是 provider error，随后 retry 成功并最终发出成功 `agent_end` 与 `agent_settled`；<br>
When Agentre 消费完整序列；<br>
Then上层只收到 clean Done，不收到旧 EventError，stream error 为空。

### A4（R2）最终失败保留诊断

Given 最后一个 `agent_end` 是 error，之后直接 `agent_settled`；<br>
When Agentre 消费完整序列；<br>
Then 上层收到原错误，且 diagnostics 保留最终 agent_end 帧和 stderr tail，不发出 Done。

### A5（R3）阈值压缩发生在 settled 且被等待

Given 上下文占用第一次达到 80%；<br>
When 扩展收到完整结算事件；<br>
Then 它调用一次 `ctx.compact()`，并且该事件 handler 在 `onComplete`/`onError` 前保持未完成；同一期间的重复 settled 不得触发第二次。

### A6（R3）非阈值场景不触发

Given 上下文占用未知、低于 80%，或压缩后占用为 null；<br>
When 扩展收到结算事件；<br>
Then 不调用 `ctx.compact()`。

### A7（R4）显式 compact 与现有能力不回归

Given Agentre 发出显式 RPC compact 命令；<br>
When Pi 返回 compaction 事件和 compact response；<br>
Then 流仍以 compact response 正常结束，并继续上报 context window；现有 `pkg/piagent` 相关测试全部通过。

### A8（R4）现有 sess-2223 无迁移可读

Given 当前 `agentre-2223.jsonl` 的副本包含既有 compaction entry；<br>
When 当前已安装的 Pi 0.82.1 以只读验证方式加载/检查该副本，或在隔离副本上 resume；<br>
Then session 可解析，原文件字节不变，且修复不要求 SQLite/JSONL 迁移。

## 参考

- Pi 扩展生命周期：`agent_end` 后仍可能 retry/compact，`agent_settled` 表示完全结算。
- Pi RPC 事件契约：普通运行的完整结算事件与 compaction 事件。
- 本地诊断记录：`.dev-kit/artifacts/2026-07-28-piagent-auto-compaction-settlement/diagnostics/root-cause.md`（本地 artifact，不提交 Git）。
