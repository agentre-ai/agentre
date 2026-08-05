# Pi subagent 实时工具步骤与多运行分组

> Status: Approved
> Owner: Pi runtime / chat experience
> Approved: 2026-08-05
> Last updated: 2026-08-05

**Objective:** 当 Pi 执行受支持的 subagent 工具时，在现有通用 `AgentSpawnCard` 中按工具调用边界实时展示每个 child Pi 的状态、实际模型、工具步骤和最终输出；完整支持 Pi `0.83.0` 官方 bundled subagent 的 `single` / `parallel` / `chain`，并支持以 dev-kit 为兼容样例的通用 flat-single 自定义协议；同样的信息必须持久化、远端转发并在会话 replay 后恢复。

**Hard invariants:**

1. **工具名是不可绕过的最外层门槛：** 只有名称忽略大小写后包含字面量 `subagent` 的工具才进入任何 subagent decoder；名称不包含 `subagent` 时，即使输入和 details 完全符合官方或 flat-single 结构，也必须继续走现有 `RawToolCard`。通过名称门槛但无法匹配受支持输入协议的候选同样保持 raw。
2. 模糊名称匹配只存在于 Pi runtime，不放宽全局 `canonical.FromToolUse`。
3. Agentre 从 raw `details.messages` / `details.results[].messages` 投影出的 child messages、工具参数和 nested results 只进入 UI 专用 blocks，不得被额外注入外层 LLM 上下文；extension 自己原本返回的顶层 `content[].text` 仍作为普通 outer `ToolResult` 回给父模型，保持既有工具语义。
4. 不修改或要求任何 subagent extension 发出 Agentre 私有协议；兼容逻辑全部位于 Agentre 的 Pi transport/runtime 转换链路。
5. 旧 Claude Code 单子代理卡片的外观、状态和 replay 行为不得因多运行支持而回归。
6. 本地与 daemon 的任何日志生产点都不得记录 complete Pi RPC frames、failure diagnostics payload 或 content-bearing runtime event；raw frame、final error frame/message、stderr tail、stopErrMsg，以及 `ToolCall`、`ToolResult`、`SubagentStarted/Progress/Done` 中的 task、summary、error、工具参数、文件内容、命令输出和 meta 均不得进入日志。

## Compatibility baselines

### Pi official bundled subagent

兼容基准为 `@earendil-works/pi-coding-agent` `0.83.0` 随包示例 `examples/extensions/subagent/`，其结构化进度路径为：

```text
tool_execution_update.partialResult.details.results[].messages
tool_execution_end.result.details.results[].messages
```

官方输入模式：

```json
{"agent":"scout","task":"Inspect the repository","cwd":"/optional"}
```

```json
{
  "tasks": [
    {"agent":"scout","task":"Inspect models"},
    {"agent":"reviewer","task":"Inspect tests"}
  ]
}
```

```json
{
  "chain": [
    {"agent":"scout","task":"Collect context"},
    {"agent":"planner","task":"Use {previous} to prepare a plan"}
  ]
}
```

官方 result 可提供 `mode`、`agentScope`、`projectAgentsDir` 和 `results[]`；每个 result 可包含 `agent`、`agentSource`、`task`、`exitCode`、`messages`、`stderr`、`usage`、`model`、`stopReason`、`errorMessage`、`step`。

### Generic flat-single custom subagent

兼容样例为当前 dev-kit subagent。它每次工具调用只启动一个 child Pi：

```json
{
  "task": "Review the implementation",
  "profile": "read-only",
  "model": "optional/provider-model",
  "thinking": "optional-level",
  "cwd": "/optional"
}
```

其累计进度和最终详情位于：

```text
tool_execution_update.partialResult.details.messages
tool_execution_end.result.details.messages
```

典型 details 同时携带 `task`、`profile`、`cwd`、`exitCode`、`messages`、`stderr`、`usage`、`model`、`stopReason`、`errorMessage`。

Agentre 不导入、不修改也不依赖 dev-kit 源码；dev-kit 只是 flat-single 协议的行为样例。其它自定义 extension 只要符合官方 batch 或 flat-single 结构，也可复用同一转换与 UI。

## Problem

1. **Pi RPC update 在第一层解码时被丢弃。** `pkg/piagent` 当前没有保留 `tool_execution_update.partialResult`，因此累计 child messages 无法到达 runtime。
2. **最终结构化详情被拍平成文本。** `tool_execution_end.result.details` 没有随现有 tool result 事件保留；`toolResultText` 只拼顶层 `content[].text`，导致结束帧也不能补回 child tool boundaries。
3. **现有 runtime 只看见外层工具。** Pi translator 只能生成外层 `ToolCall` / `ToolResult`，名称为 `subagent` 的 extension 工具仍表现成普通工具卡。
4. **现有子代理状态只描述一个运行。** `canonical.AgentSpawn`、`SubagentInfo`、`SubagentStateBlock` 和前端 `AgentSpawnCard` 都假设一个外层工具对应一个 child；官方 parallel/chain 的一个外层调用可包含多个彼此独立的 child Pi。
5. **现有 nested block 只有父调用归属。** `ParentToolCallID` 足以把 child step 收进外层卡，但不足以区分 parallel/chain 中它属于哪个 `results[i]`。
6. **daemon 日志存在 payload 泄漏风险。** `internal/daemon/handlers/runtime.go` 当前会记录多数 runtime event 的完整 JSON；新增 child events 后，工具参数、文件内容和终端输出可能进入日志。
7. **下游基础能力可以复用。** chat service 已能持久化 `ToUI` nested blocks，前端已能把 child steps 收进 `AgentSpawnCard`；正确方案是在 transport/runtime 保留结构、增加通用多运行状态和分组，而不是创建 Pi 专用卡片。

## Actors and user stories

1. **Pi 用户：** 在 child 工具调用到达时立即看到对应 run 进入活动状态，结果到达后看到成功或失败，而不是等整个外层 subagent 完成。
2. **Parallel 用户：** 在一张卡里区分每个 agent 的任务、模型、状态、工具步骤和输出，不把并发 child 的步骤混成一条列表。
3. **Chain 用户：** 看见步骤顺序、当前执行项、失败点和后续 `SKIPPED` 项。
4. **自定义 extension 作者：** 只要输出标准 Pi messages 的官方 batch 或 flat-single 详情，无需修改 Agentre UI 即可获得实时 AgentSpawnCard。
5. **历史记录用户：** 重开会话后看到与流式期间相同的 run 分组和 child steps。
6. **远端执行用户：** agentred 本地与远端行为一致，同时日志不包含 child payload。

## Design decisions

| # | Decision | Basis and rejected option |
|---|---|---|
| 1 | 工具名忽略大小写后包含 `subagent` 只进入候选判断；最终是否支持由输入 decoder 决定。 | 接受 `subagent`、`spawn_subagent` 和 namespaced wrapper。拒绝 name-only 分类，它会误认 `stop_subagent`、`subagent_status` 等控制工具。 |
| 2 | Pi runtime 提供 official bundled 和 generic flat-single 两种显式 envelope decoder；start 时先分类 invocation。官方 batch 与 profile-only flat 可立即锁定，`{agent,task}` single 因两种 extension 都可能使用而保持 pending，首个无歧义结构化 snapshot 锁定 envelope，之后不切换。 | 支持官方多模式、dev-kit 和自定义 `{agent,task}+details.messages`，同时避免把输入相同但 envelope 不同的协议误绑死。拒绝每帧重新猜协议，也拒绝对任意 details 路径做自然语言推断。 |
| 3 | `pkg/piagent` 只负责无损保留 raw `partialResult` / final `result.details`；协议解释位于 Pi runtime adapter。 | transport 拥有 RPC fidelity，runtime 拥有产品语义。拒绝让通用 wrapper 依赖 AgentSpawn 或 extension-specific 类型。 |
| 4 | 两种 decoder 都投影为统一的 invocation/run snapshots，再由共享 tracker 做累计差量。 | child Pi messages 的工具边界语义相同，仅 envelope 不同。拒绝为官方和 dev-kit 各写一套事件、持久化和 UI。 |
| 5 | 现有 `AgentSpawnCard` 扩展为通用 single/multi-run 卡片，不创建 Pi 专用组件。 | 复用状态、nested `RawToolCard`、summary、replay 和主题。拒绝平行的 `PiSubagentCard`。 |
| 6 | nested `ToolCall` / `ToolResult` 在 `ParentToolCallID` 之外增加可选 `SubagentRunID`。 | 第一级确定外层卡，第二级确定 run。该字段是 runtime-neutral，可供未来其它多子代理后端复用。 |
| 7 | 多运行状态通过 `SubagentStarted/Progress/Done` 的完整 run snapshot 传递；前端收到 `runs` 时整数组替换，不做脆弱的嵌套浅合并。 | 官方 parallel 最多八项；官方 chain 不设八项上限，但 snapshot 大小仍由单次用户输入界定，完整替换换取确定的 replay/merge 语义。不得从 parallel 上限推导或施加 chain 上限。拒绝加入大量 run-specific patch event。 |
| 8 | broad name matching 只在 Pi runtime；replay 必须以匹配的 `SubagentStateBlock` 为分类证据。 | 保持历史数据的全局 name-only guard。拒绝把所有 `*subagent*` 加入 `canonical.FromToolUse`。 |
| 9 | 支持 start 后，即使后续 details 缺失或畸形，卡片也不在运行中变回 RawToolCard；该帧只降级为无结构化进度。 | 防止 UI 类型跳变和持久化分叉。 |
| 10 | 本轮只投影 child tool boundaries、run 状态、实际模型和最终输出；usage/cost/context、thinking、逐行 stdout 不投影。 | 直接解决实时可见性和多运行归属，避免把不同 extension 的 usage 语义错误合并。 |
| 11 | Parallel 混合成功/失败使用外层 `partial` 状态；Chain 失败后的未启动项使用 run-level `skipped`。 | 不把部分成功说成 DONE，也不把整个 parallel 简化成纯 ERROR。 |
| 12 | daemon 对所有可能携带 child-derived content 的 runtime event（包括 `ToolCall`、`ToolResult`、`SubagentStarted/Progress/Done`）只记录安全元数据，不记录序列化 event payload。 | child 工具和完整 run snapshots 可能包含 task、源码、输出、错误和凭证。拒绝仅依靠调用方“不传敏感内容”。 |

## Candidate recognition and decoder selection

Recognition happens on `tool_execution_start` so the outer card can appear before child completion.

### Common name gate

1. 保留原始 tool name 用于存储和展示。
2. 在运行任何输入 decoder 之前执行 `strings.Contains(strings.ToLower(name), "subagent")`。
3. 不满足名称 gate 时立即沿用当前普通 Pi tool flow；不得检查官方/flat-single 输入形状，也不得因后续 details 看起来像 child messages 而补分类。
4. 只有通过名称 gate 后，参数才进入 decoder；参数必须是合法 JSON object，非 object、空 payload 或 malformed JSON 不支持。

### Invocation pre-validation and mode classification

通过名称 gate 后，先校验所有已知协议字段；已知字段类型错误是 **poison error**，整个候选立即回退 raw，不得再被另一 decoder 接受：

- `tasks` / `chain` 若出现必须是 array；非空数组的每项 `agent`、`task` 必须为非空 string，可选 `cwd` 若出现必须为 string；`tasks` 超过官方并行上限 8 时不支持，`chain` 不施加该上限。
- `agentScope` 若出现必须为 `user | project | both`。
- `confirmProjectAgents` 若出现必须为 boolean。
- 顶层 `agent`、`task`、`profile`、`model`、`thinking`、`cwd` 若出现必须为 string；参与模式识别的 `agent/task/profile` trim 后必须非空。可选 `model/thinking/cwd` 的空白 string 按 omitted/default 处理，不拒绝调用，以保持 dev-kit 的继承语义。

与官方 `0.83.0` 一致，空 `tasks: []` / `chain: []` 是 inactive mode，不因 key 存在而阻止其它模式；非法非数组或非法非空数组仍是 poison error。

计算 active invocation modes：

- agent-single：`agent` + `task` 均为非空 string；
- parallel：`1 <= tasks.length <= 8`；
- chain：`chain.length > 0`；
- profile-flat：没有 agent-single/parallel/chain，且 `task` + `profile` 均为非空 string。

agent-single、parallel、chain 中必须恰好一个 active；多于一个是 ambiguous，回退 raw。没有 official active mode 时，只允许 profile-flat。未知附加字段忽略，但不能消除已知字段的 poison error。

### Envelope decoder selection

- Parallel / chain invocation 在 start 时锁定 official decoder，后续只读取 `details.results[].messages`。
- Profile-flat（典型 dev-kit `{task,profile}`）在 start 时锁定 flat-single decoder，后续只读取 `details.messages`。
- Agent-single `{agent,task}` 的 input 同时可能来自官方或自定义 flat extension，因此 start 时创建 `single-envelope-pending` tracker。首个 **无歧义且结构有效** 的 update/end snapshot 决定：
  - 只有 array `details.results`，且 `details.mode` 缺失或为 `single`：锁定 official；
  - 只有 array `details.messages`：锁定 flat-single；
  - 两者同时存在或都不合法：该 snapshot 不锁定、不产生 child events。
- Pending 状态收到 `details.results` 但 `details.mode=parallel|chain` 时，该 snapshot 在锁定前即被判为 mode mismatch：忽略且保持 pending，允许后续有效 single/flat snapshot 锁定。
- 一旦 envelope 锁定，后续 snapshot 不得切换 decoder；其它 envelope 字段按未知数据忽略。
- Pending 不妨碍 start 时立即显示 single AgentSpawnCard，因为 agent/task/run-0 静态数据在两种协议中相同。

这直接接受 dev-kit 当前 `{task, profile, model?, thinking?, cwd?}`，也允许自定义 `{agent,task}` extension 在后续使用 flat `details.messages`，同时不会把它预先误绑成官方 `results[]`。

### Unsupported input behavior

- Invocation classifier 不接受时，工具从 start 起就是普通 `ToolCall` / `RawToolCard`。
- `{task_id, action}`、`{status}`、只有 `task` 但没有 `agent/profile`、非法 batch、多个 active official modes 等均回退普通工具。
- malformed official-only known fields（例如 `agentScope: 42` 或 `tasks: "bad"`）不得降级为 generic flat-single。
- 仅名称包含 `subagent` 不产生 `SubagentStateBlock`；名称不包含 `subagent` 时 invocation classifier 和所有 envelope decoder 的调用次数必须为零。
- classification/decoder lock 只保存于本 turn tracker，不需要数据库或全局注册表。

## Normalized runtime model

两个 decoder 输出同一种概念模型；字段名可在实现时按 Go 规范调整，但 wire 语义固定。

```text
SubagentInvocation
  mode: single | parallel | chain
  runs: []SubagentRun

SubagentRun
  id: stable within outer tool call
  index: original input slot
  agent?: official/custom agent name
  profile?: flat-single permission/profile label
  agentSource?: official user/project source
  task: task prompt for this run
  requestedModel?: optional input model alias
  model?: first observed child assistant model
  status: waiting | running | completed | failed | canceled | skipped | unknown
  lastToolName?: latest emitted child tool
  toolUses: unique child tool-call count
  summary?: final child assistant text
  errorMessage?: sanitized structured failure message
```

Rules:

- Official run identity follows original input index, not mutable result text or agent name.
- IDs may be `run-0`, `run-1`, ... because they are scoped by outer tool call; persisted child call IDs still require collision-free encoding of `(outerToolCallID, runID, innerToolCallID)`.
- 不得通过无分隔字符串拼接产生可能碰撞的 child ID；使用带长度编码、结构化 key 或等价无歧义方案。
- Static invocation data lives in canonical `AgentSpawn`; dynamic snapshot data lives in `SubagentStateBlock`. Frontend merges matching runs by ID, with observed model overriding requested model.
- `profile`、`agent`、`model`、task/output are dynamic values and are never translated or rewritten.
- `agentSource` is optional; unknown source is absence, not an invented value.

### Legacy compatibility

- Existing canonical top-level single-run fields (`SubagentType`, `TaskDescription`, `Prompt`, `Model`, counters, status, summary) remain supported.
- `mode` / `runs` are optional. Empty `runs` means the current legacy single-agent contract.
- One normalized run may use the current single layout; multiple runs or `mode=parallel|chain` use the grouped layout.
- Existing Claude events need not synthesize `runs`; their current rendering and persistence continue unchanged.

## Pi transport fidelity

`pkg/piagent` must preserve enough raw structure for runtime decoders without understanding subagent semantics.

1. Add a wrapper event representation for `tool_execution_update` that retains `toolCallId`, tool name when available, and raw `partialResult` JSON.
2. Preserve raw `tool_execution_end.result.details` alongside existing top-level text extraction and error status.
3. Existing top-level `content[].text` behavior remains the source of the ordinary outer `ToolResult.Content`.
4. Missing/malformed `partialResult` or `details` is non-fatal and does not stop the Pi turn.
5. Raw structured fields are not logged by the wrapper.
6. The initial Red tests must demonstrate both current loss points independently: update is absent, and final details are unrecoverable.
7. Pi RPC `extension_ui_request` is retained only as safe control metadata needed to send a response; its title/message/options are not logged or projected into subagent state.

## Extension UI confirmation policy

Pi RPC dialog methods block the extension until the client sends a matching `extension_ui_response`. Agentre has no extension-UI dialog bridge in this round, so the transport must use a secure deterministic default:

1. For dialog methods `confirm`, `select`, `input` and `editor`, immediately write `{"type":"extension_ui_response","id":<same-id>,"cancelled":true}` to Pi stdin.
2. Never auto-confirm project-local agents and never infer consent from project trust or tool input.
3. Fire-and-forget UI methods may be ignored after consuming the frame; they receive no response.
4. The control response must be serialized through the existing single writer/command boundary so it cannot interleave with steer/abort commands.
5. Request title/message/options are not persisted or logged.
6. For a supported official invocation with `agentScope=project|both`, `confirmProjectAgents != false`, no child activity and terminal official `results: []`, map all declared runs and aggregate to `canceled`; preserve the extension's top-level cancellation text as outer summary.
7. This policy prevents indefinite RUNNING cards. A future interactive extension-UI bridge is a separate feature.

## Runtime event flow

### Supported start

For a supported candidate:

1. Emit the ordinary outer `ToolCall` first with `canonical.agent.spawn` static invocation data.
2. Create a turn-local tracker bound to the invocation classification, current envelope lock (`official` / `flat-single` / `single-envelope-pending`) and outer tool-call ID.
3. Emit `SubagentStarted` with a full initial snapshot.
4. Initial per-run states:
   - official/flat single: run 0 is `running`;
   - official chain: run 0 is `running`, later runs are `waiting`;
   - official parallel: all runs are `waiting` until structured child activity identifies them; the outer status is still `running`.
5. Synchronous Pi subagents keep empty background `Kind`; they do not enter the Claude background-task panel.

### Structured update

For each `tool_execution_update` belonging to the outer call:

1. A locked decoder reads only its supported envelope; a `single-envelope-pending` tracker first applies the unambiguous envelope-lock rules above:
   - official: `partialResult.details.results[].messages`;
   - flat-single: `partialResult.details.messages`.
2. Convert newly observed messages into normalized per-run changes.
3. Emit newly discovered nested `ToolCall` / `ToolResult` with both `ParentToolCallID` and `SubagentRunID`.
4. Update first observed model, per-run tool count, last tool name, output/status evidence.
5. Emit one `SubagentProgress` full snapshot if any run metadata changed.
6. Repeated cumulative snapshots emit no duplicate child boundary or duplicate result.

### End

For `tool_execution_end`:

1. Decode final details through the same tracker to recover events absent from updates.
2. Emit missing nested calls/results first.
3. Finalize every run using authoritative final evidence and emit final progress if needed.
4. Emit `SubagentDone` with the full terminal snapshot.
5. Emit the ordinary outer `ToolResult` last, preserving current top-level text and `isError` behavior.
6. Remove the tracker after end.

### Abort without end

If the whole Pi turn terminates before an end frame:

- Discard runtime trackers.
- Existing chat-service cleanup marks the outer subagent canceled.
- Cleanup also marks non-terminal normalized runs (`waiting` / `running`) as `canceled`, preserving already terminal runs.
- No replayed run may remain permanently `running` after turn abort.

### Unsupported tools

Unsupported candidates retain existing PreToolUse/PostToolUse translation and never emit `SubagentStarted/Progress/Done`.

## Snapshot decoding, ordering and deduplication

### Common Pi message rules

- Assistant content item `type="toolCall"` with non-empty `id` and `name` is a child call; object `arguments` becomes input.
- `role="toolResult"` with non-empty `toolCallId` is a child result. Text content blocks are concatenated; valid empty content still emits an empty result so the step can settle.
- `isError` marks that child step failed but does not alone fail the whole run.
- Assistant `model` supplies actual-model evidence; the first non-empty observed model wins for that run.
- Intermediate assistant prose/thinking is not emitted into the outer transcript. The last assistant text is retained only as the run's final `summary`.
- Unknown roles/content, malformed IDs and unsupported fields are ignored independently; one malformed run cannot suppress valid sibling runs.

### Dedupe and orphan handling

Each outer tracker keeps per-run emitted call/result identities.

- Duplicate accumulated snapshots do not emit duplicates.
- A result observed before its matching call is held per run and emitted only after the call appears.
- The same inner `toolCallId` used by two parallel runs yields two different parent-scoped runtime IDs.
- Final snapshots pass through the same deduper, so recovery cannot duplicate update-time events.
- A partial snapshot that omits a previously observed result does not delete or decrement state.
- An emitted child call without a matching result is **not** given a fabricated ToolResult at parent termination. Live and replay UI derive its terminal step status from the containing normalized run: run `failed` → step FAILED; run `canceled` → step CANCELED/STOPPED; run `unknown`, `completed` or any other terminal state without result → step UNKNOWN. Thus no spinner remains and no child output/success is invented.

### Parallel ordering

Parallel updates can contain newly advanced messages for multiple results.

- Preserve message order inside each run.
- When multiple runs advance in one snapshot, use valid message timestamps to order newly discovered boundaries.
- Missing/equal timestamps fall back deterministically to input run index then message index.
- Exact wall-clock interleaving is not invented when the source snapshot cannot prove it.

### Official result identity

- Input mode and input slot are authoritative; later `details.mode` must match when present, otherwise that snapshot is ignored rather than remapping the card.
- Official result values may enrich `agentSource`, `model`, status and summary but do not reorder or replace the input task/agent identity.
- Parallel placeholders with `exitCode=-1` remain waiting until activity.
- Official successful in-progress results may temporarily expose `exitCode=0`; update-time completion must therefore also require terminal stop evidence, not `exitCode=0` alone.
- Final `result.details.results[]` is authoritative for terminal exit status.

## Status semantics

### Per-run status

| Status | Meaning |
|---|---|
| `waiting` | Declared by the invocation but no child activity has proved it started yet. |
| `running` | Initial single/current chain run, or any run with observed child messages/tool activity and no terminal evidence. |
| `completed` | Final exit code is zero without error/aborted, or an update has terminal assistant stop reason `stop` / `length`. |
| `failed` | Non-zero final exit, `stopReason=error`, structured run failure, or outer error attributable to the run. |
| `canceled` | Whole turn aborted or flat-single reports `stopReason=aborted`. |
| `skipped` | A chain ended because an earlier run failed and this later declared run never started. |
| `unknown` | The outer tool ended but available structured evidence cannot honestly assign this run success, failure, cancellation or chain skip. This is terminal and never shows a spinner. |

Additional rules:

- `stopReason=toolUse` is running, not completed.
- Official result `stopReason=aborted` follows the official extension's failure contract and maps to `failed`; only whole-turn abort cleanup maps it to `canceled` when no authoritative end is received.
- Flat-single/dev-kit `stopReason=aborted` maps to `canceled` because that contract explicitly represents parent-requested child termination.
- A child tool result with `isError=true` fails only that step; the run may recover.
- Missing model, source, output or error message degrades only that field.
- `stderr` is not persisted or displayed as a run field in this round.

### Deterministic finalization with incomplete evidence

At outer end, every run must become terminal according to these rules. The project-agent auto-decline rule above is applied before generic incomplete-evidence fallbacks.

1. **Single/flat-single with no usable final details/envelope:** outer `isError=false` → run `completed`; outer `isError=true` → run `failed`, because the sole extension tool outcome is the only available terminal evidence.
2. **Single/flat-single with a usable envelope but partial terminal fields:** explicit terminal stop/exit evidence wins; otherwise outer `isError=false` → run `unknown`, outer `isError=true` → run `failed`. Missing/malformed `exitCode` and final `exitCode=-1` without terminal stop evidence both follow this rule.
3. **Grouped outer success with no usable final details:** every non-terminal run → `unknown`; aggregate → `unknown`.
4. **Grouped outer error with no usable final details:** every non-terminal run → `unknown`; aggregate → `failed` because the outer tool itself failed, without inventing which child failed.
5. **Partial final parallel details:** runs with terminal evidence use that evidence; any remaining non-terminal/missing run → `unknown`. Presence of any `unknown` makes a successful outer aggregate `unknown`; outer error remains `failed`.
6. **Partial final chain details:** if a represented run is definitively failed, later never-started declared runs → `skipped`; otherwise any missing/non-terminal declared run → `unknown`. A known chain failure keeps aggregate `failed`; otherwise unknown evidence yields aggregate `unknown` unless outer error requires `failed`.
7. **Grouped final `exitCode=-1`:** use explicit terminal `stopReason` when available; otherwise that run → `unknown`.
8. These fallback transitions are persisted in the final `SubagentDone` snapshot, so replay cannot reconstruct waiting/running states after an end frame.

### Aggregate outer status

#### Single / flat-single

- mirror the sole run: `running`, `completed`, `failed`, `canceled` or terminal `unknown`.

#### Parallel

- any non-terminal run while outer tool is active: `running`;
- all completed: `completed`;
- terminal mixture of completed and failed with no unknown runs: `partial`;
- all failed: `failed`;
- any terminal `unknown` after a successful outer end: `unknown`;
- outer error with incomplete/unknown child evidence: `failed`;
- user abort with any non-terminal run: `canceled`, even if some siblings already completed.

#### Chain

- current/future work remains: `running`;
- all completed: `completed`;
- one run fails: `failed`; untouched later runs become `skipped`;
- incomplete terminal evidence without a known failure: `unknown` after outer success, `failed` after outer error;
- user abort: `canceled`; non-terminal runs become `canceled`, not `skipped`.

Outer `ToolResult.isError` remains available as fallback when structured terminal evidence is missing. For grouped cards, normalized state is authoritative when it contains complete per-run terminal evidence.

## Runtime, wire and chat-service contracts

### Runtime events

Reuse existing event kinds. No Pi-specific event kind is added.

- `ToolCall` gains optional `SubagentRunID`.
- `ToolResult` gains optional `SubagentRunID`.
- `SubagentInfo` gains optional `Mode` and `Runs` full snapshot.
- Existing top-level fields and `SubagentModel` event remain for legacy Claude/single behavior.
- Pi multi-run model changes may travel in the full `SubagentProgress` snapshot; they do not need one global `SubagentModel` value that would lose run identity.

### Remote wire

`internal/pkg/agentruntime/event_wire.go` round-trips every new optional field.

- Old events with omitted fields continue decoding.
- Local and agentred execution produce equivalent grouping.
- Wire tests cover ToolCall, ToolResult and Subagent snapshots with at least two runs.

### Persistent blocks

- `NestedToolUseBlock` and `NestedToolResultBlock` gain optional `SubagentRunID` and stay `ToUI` only.
- `SubagentStateBlock` gains optional `Mode` and `Runs`.
- `ChatBlock` / wire DTOs expose optional `subagentRunId` and multi-run state.
- No database schema migration is required; all changes are compatible optional fields inside `blocks_json`.
- Old code may ignore new JSON fields; old stored blocks without them remain readable.

### Handler merge rules

- When a Subagent event carries `runs`, chat service replaces the stored `Runs` slice with the incoming full snapshot.
- Existing legacy top-level merge semantics remain unchanged.
- Frontend store follows the same rule: `runs` is replaced as one full snapshot; omitted `runs` does not clear existing runs.
- Cancellation cleanup updates non-terminal run statuses as well as the outer status.

### History projection

1. A matching state block proves Pi AgentSpawn classification only when it carries the new normalized `Mode/Runs` evidence and the original tool name still passes the Pi `subagent` name gate. A legacy `SubagentStateBlock` alone is not sufficient because it also represents existing `local_bash` background activity.
2. Existing legacy Claude `Agent`/`Task` replay continues through its current canonical input recognition. New normalized Pi blocks build `canonical.agent.spawn` from persisted state plus outer input without broadening global name matching.
3. Nested blocks keep `ParentToolCallID` and `SubagentRunID` and are grouped under the matching run.
4. Name-only historical tools without state remain raw; guard tests must continue proving `canonical.FromToolUse("mcp__x__subagent", ...)` is not recognized solely by name.
5. Missing/unknown `SubagentRunID` is never silently dropped: single-run cards attach it to the sole steps list; grouped cards keep it in an outer fallback STEPS area rather than assigning it to an incorrect run.

## AgentSpawnCard UI

The existing `frontend/src/components/agentre/canonical-tool/agent-spawn/card.tsx` remains the only production card.

### Legacy and normalized single layout

- Legacy Claude data (`runs` absent) keeps the current layout and behavior.
- A normalized single run uses the same visual structure: task prompt, steps and summary.
- If `agent` is absent, display the existing generic Agent label rather than inventing an identity.
- Optional `profile` is a separate dynamic badge; it is not displayed as the agent name.
- Observed model overrides requested model; absent model produces no placeholder.

### Parallel / chain grouped layout

Header concept:

```text
Agents · Parallel · 1/3                                  RUNNING
Agents · Chain · 2/4                                     RUNNING
```

- Progress is terminal run count over total, not “runs that emitted any message.”
- Header does not show one aggregate model because sibling runs may use different models.
- Expanded card contains a `TASKS` section with one ordered run group per invocation slot.

Run group concept:

```text
1  scout [project] · Inspect authentication models [haiku-4-5] [DONE]
   STEPS
     read · internal/model/user.go                         DONE
   OUTPUT
     Found the relevant authentication models.

2  reviewer [user] · Review failure paths [sonnet-4-5] [RUNNING]
   STEPS
     bash · go test ./internal/...                         RUNNING

3  planner · Prepare the migration plan                   [WAITING]
```

Each group shows only known fields:

- index/chain step;
- agent name or generic label;
- translated official source badge when known;
- dynamic profile badge when known;
- task;
- actual/requested model when known;
- explicit status text;
- its own nested STEPS;
- final OUTPUT or sanitized error when terminal.

### Expansion behavior

- Outer card keeps the existing transcript-controlled expand/collapse behavior.
- Inside an expanded grouped card, a run auto-expands on first activity unless the user manually changed that run's state.
- `running` and `failed` groups default expanded.
- `waiting` and `skipped` groups default collapsed.
- User collapse wins over later progress; updates do not repeatedly reopen the group.
- A completed group keeps the user's last expansion state rather than collapsing automatically.

### Summary and fallback steps

- Per-run OUTPUT comes from that child's final assistant text; absent output uses the existing empty-result treatment.
- Outer SUMMARY remains the official/custom tool's top-level result text and is not synthesized from run output when already supplied.
- Unknown-run nested blocks, if any, appear in an outer fallback STEPS area so no persisted tool content disappears.

### Status presentation

- Add visible/i18n labels for `PARTIAL`, `WAITING`, `SKIPPED`, `UNKNOWN` and grouped mode/section copy as required.
- `partial` uses a warning presentation, not success or full-error presentation; `unknown` uses a neutral explicit UNKNOWN presentation and is terminal.
- `failed` uses error presentation; `canceled` uses existing stopped/canceled semantics.
- Status is always conveyed by text, never color alone.
- A normalized child call with no result stops spinning when its containing run becomes terminal: FAILED for failed run, CANCELED/STOPPED for canceled run, otherwise UNKNOWN. Its result detail area stays empty because no ToolResult was observed.

### Accessibility and i18n

- Every new static label, status, section title, mode label, source label and aria string uses `t(...)` and is added to both `en` and `zh-CN` locales.
- Agent names, profiles, models, task prompts, tool inputs/results and outputs are dynamic data and are not translated.
- Run toggles are keyboard accessible and expose expanded state and a meaningful accessible name.
- Existing shadcn/design-system components and color tokens are reused; no native form control or new color token is introduced.

### Controls

- Stop continues to abort the whole Pi turn.
- No per-run stop/pause/retry control is introduced.
- Official parallel's internal concurrency limit is not reimplemented or exposed by Agentre.

## Security, privacy and logging

### UI/persistence and outer-context boundary

- Child arguments/results are data already present in Pi RPC and are persisted only through the existing assistant `blocks_json` UI boundary.
- Nested blocks remain `ToUI`; Agentre never appends raw child messages or projected nested blocks to the next outer model request.
- The extension-authored top-level `content[].text` remains the ordinary outer `ToolResult` and continues to enter parent tool context exactly as before this feature.
- `stderr`, usage cost and raw extension details are not copied wholesale into state blocks.
- Error display uses structured `errorMessage` or existing outer result text; credentials are not specially inferred from arbitrary stderr.

### Runtime and daemon logging

The focused privacy fix covers every existing producer that can expose these new structured child details:

1. `internal/pkg/agentruntime/runtimes/piagent/session.go` raw-frame debug sink must stop logging complete RPC frames. It may log only parsed event/response kind, safe correlation IDs and byte count; malformed frames get length/parse-failure metadata, not raw bytes.
2. `internal/pkg/agentruntime/runtimes/piagent/runtime.go` failure diagnostics must not log `FinalErrorFrame`, full `FinalErrorMessage` or `StderrTail`. It may log safe event type, stop reason, error class/code and byte counts.
3. `internal/daemon/handlers/runtime.go` must not serialize any content-bearing event into operational logs. This includes `ToolCall`, `ToolResult`, `SubagentStarted`, `SubagentProgress` and `SubagentDone`; forwarding still uses the complete wire payload.
4. daemon run-result logging must not print full `stopErrMsg`; retain only safe error code/category and non-content metadata. Autonomous fanout follows the same redaction rules as foreground fanout.
5. `internal/pkg/agentruntime/runtimes/remote/runtime.go` unknown-session and unmarshal-failure branches must log raw byte count/safe event kind only, never `frame.Event`; run-result completion must not log full `StopErrMsg`.
6. `internal/service/chat_svc/chat.go` runtime `ErrorEvent`, promoted `RunResult.StopErr`, finalize/session-status and `failTurn` logs must not use `zap.Error`/full strings for untrusted runtime errors. They retain safe error class/code, IDs and message byte count while the full error continues to the existing user-visible stream and persisted `ErrorText` behavior.
7. Equivalent local and remote error paths share one redaction contract; moving payload from daemon to desktop must not reintroduce it into another log sink.

Allowed log metadata is limited to values such as:

```text
session ID / provider session ID
event or response kind
safe tool/event type
outer tool-call ID or safe short identifier
whether a parent/run ID exists
sequence/count/status
payload byte count
safe error code/category
```

The following must not be logged at any level:

```text
raw Pi RPC frame / remote frame.Event
final error frame or full runtime error message
stderr tail / stopErrMsg / StopErrMsg
ToolCall.Input
ToolResult.Content / Meta
task/prompt
run summary/errorMessage
child assistant output
file contents
terminal output
```

Regression tests must inject distinct sentinels through raw frames, final-error diagnostics, stderr/stopErrMsg, remote unknown/malformed event branches, local/remote ToolCall/ToolResult, SubagentStarted/Progress/Done task/summary/error fields, chat-service ErrorEvent promotion and finalization. Captured Pi runtime, remote runtime, chat-service and daemon logs must contain none of them while parsing, wire forwarding, user-visible errors and persisted `ErrorText` remain lossless.

## Failure and independent degradation

- Unsupported start input: ordinary tool card from the beginning.
- Supported start, missing all structured details: AgentSpawnCard remains, STEPS stays empty, outer summary/result still settles it.
- One malformed official result: ignore that result's malformed fields; valid siblings continue.
- Missing model: only model badge is absent.
- Missing source/profile: only that badge is absent.
- Missing child output: OUTPUT uses empty-result state without changing completion.
- Observed child result with empty text: emit an empty ToolResult so the known result settles normally.
- No child result observed before parent termination: do not synthesize a result; renderer uses the containing run's terminal fallback status and leaves result detail empty.
- Missing/partial final details: apply the deterministic finalization table above; unresolved single or grouped evidence becomes terminal `unknown` where attribution is impossible rather than fabricated success/failure, and no permanent running state remains.
- Result-before-call: hold until call arrives; never emit orphan at transcript top level.
- Mismatched official `details.mode`: ignore that snapshot; do not change decoder or reorder runs.
- Remote peer omits new optional run field: single behavior remains usable; grouped unknown steps use fallback grouping rather than disappearing.

## Out of scope

- Modifying Pi official, dev-kit or any third-party subagent extension.
- Automatically supporting arbitrary custom details paths such as `details.children[].history`; a new decoder is required for a new envelope.
- Giving dev-kit one outer-card parallel/chain mode; its current contract is one child per tool call, so multiple dev-kit calls remain multiple single cards.
- Streaming child stdout chunks, terminal PTY frames, intermediate assistant prose or thinking.
- Displaying or aggregating child usage, cost, context tokens, turn count or duration.
- Showing stderr verbatim.
- Per-run stop/pause/retry, recursive dispatch UI or background Pi child management.
- Interactive rendering/approval of arbitrary Pi `extension_ui_request`; this round only sends deterministic `cancelled:true` responses so dialog methods cannot deadlock.
- A Pi-specific card, HTTP API, database migration or new Wails method.
- Broadening canonical matching outside Pi runtime.
- Changing Pi scanner/frame-size limits, native session handling, compaction, image, steer or context-window behavior.

## Documentation synchronization

Implementation must update stale producer/contract documentation in the same scoped change:

- `docs/agent-backend.md`: Pi runtime now interprets structured subagent update/final snapshots in the stateful drain boundary rather than a pure translator alone.
- `internal/pkg/agentruntime/event.go`: document `ParentToolCallID` plus optional `SubagentRunID` grouping and the payload privacy boundary.
- `internal/pkg/agentruntime/runner.go`: document multi-run `SubagentInfo` and aggregate statuses.
- Any generated Wails TypeScript bindings changed by Go DTOs are refreshed through the repository's generation command, not hand-maintained as a parallel contract.

No unrelated contributor-doc cleanup is included.

## TDD and test seams

Strict Red → Green → Refactor applies independently at each producer boundary.

| Seam | Required failing behavior spec before implementation |
|---|---|
| `pkg/piagent` RPC boundary | update `partialResult` is currently absent; final `result.details` is currently unrecoverable; malformed raw details leave outer text behavior intact; blocking extension UI dialogs receive one matching `cancelled:true` response through the serialized writer while fire-and-forget requests receive none. |
| Name gate + invocation/envelope selection pure functions | a non-matching name invokes zero classifiers/decoders (spy/counter/panic seam); valid mixed-case/namespaced `Vendor__SubAgent` reaches classification; official single/parallel/chain and inactive empty arrays accepted; profile-flat/dev-kit accepted including blank optionals; `{agent,task}` locks from first unambiguous matching-mode envelope; dual/mismatched envelopes stay pending; poison/ambiguous/control inputs rejected. |
| Shared snapshot tracker | cumulative updates dedupe; final fills missing events; per-run identities isolate equal inner IDs; result-before-call held; timestamps/fallback ordering deterministic. |
| Official status tracker | running `exitCode=0` not prematurely completed; parallel mixed result becomes partial; chain failure marks later runs skipped; official aborted maps failed; whole-turn abort maps canceled; missing/partial final evidence yields deterministic unknown/failed terminal snapshots. |
| Flat-single tracker | dev-kit-style `exitCode=-1` runs live; observed model overrides requested model; error/aborted/final output map correctly. |
| Runtime drain | outer ToolCall precedes SubagentStarted; nested events precede progress; Done precedes outer result; unsupported candidates remain ordinary. |
| Runtime wire | `SubagentRunID`, mode and two-run snapshots round-trip local ↔ daemon. |
| chat handlers/state blocks | full runs replace atomically; nested blocks stay ToUI; cancellation updates runs; old single state remains compatible. |
| history projection | normalized Pi state + original name gate rebuilds AgentSpawn and groups runs; name-only history remains raw; legacy `local_bash` SubagentStateBlock 不被重分类；missing run IDs are not dropped. |
| frontend store/renderer | runs replace rather than corrupt shallow merge; children group by parent then run; no duplicate top-level cards. |
| AgentSpawnCard | legacy single unchanged; normalized single profile/model; parallel/chain groups, statuses, output, expansion rules and accessible labels; unmatched child calls stop spinning from containing-run terminal status without synthetic ToolResult. |
| Pi/remote/chat-service/daemon logging | sentinel raw-frame/frame.Event/final-error/stderr/stopErrMsg/task/input/result/meta/summary/error values are absent from all operational logs while parsing, forwarding and user-visible/persisted error behavior remain lossless. |
| i18n/typecheck | static keys covered in both locales; generated models/typecheck pass. |

Repository unit-test discipline remains unchanged: service tests inject mocks and do not open a real DB; repository tests use sqlmock where applicable.

## Acceptance criteria

### A1 — RPC update and final details survive transport

Given a scripted Pi RPC stream sends `tool_execution_update.partialResult.details` followed by `tool_execution_end.result.details`;<br>
When `pkg/piagent` decodes it;<br>
Then runtime receives both raw structures, while the existing outer result text and error behavior remain unchanged.

### A2 — Unsupported name/input combinations remain raw

Given `delegate_task` 等名称不包含 `subagent` 的工具携带完全合法的 official/flat-single 输入，或 `stop_subagent`、`subagent_status`、`mcp__x__subagent`、`SubAgent` 等通过名称门槛的工具携带 malformed、control-only、ambiguous 或 unsupported 输入;<br>
When start frames are translated;<br>
Then selector-level spy/counter 证明前一类名称调用 invocation classifier 和 envelope decoders 的次数均为零；已知字段 poison error、超过 8 项的 parallel、多个 active mode 及其它 unsupported 输入不得降级到另一协议；两类都不创建 `SubagentStateBlock`，并使用普通 RawToolCard path。

### A3 — Official single streams steps exactly once

Given a mixed-case/namespaced tool name such as `Vendor__SubAgent` carries an official `{agent, task, tasks: [], chain: []}` invocation and repeated cumulative `details.results[0].messages` snapshots;<br>
When the first unambiguous official snapshot arrives, followed by a child tool call/result and then a stray flat `details.messages` snapshot;<br>
Then case-insensitive literal containment passes the name gate, empty inactive arrays do not block single mode, the pending envelope locks to official and never switches, one AgentSpawnCard/run appears immediately, the step is RUNNING before the result, settles afterward, and neither repeated update nor final snapshot duplicates it;<br>
And an authoritative official run with `stopReason=aborted` is FAILED rather than CANCELED unless whole-turn no-end cleanup owns the abort;<br>
And Given a supported `agentScope=project|both` invocation with confirmation enabled emits `extension_ui_request` method `confirm`;<br>
Then Agentre sends one matching serialized `cancelled:true` response, never auto-approves, and terminal empty official `results` settles all declared runs and the outer card as CANCELED instead of hanging.

### A4 — Official parallel keeps runs isolated

Given three official parallel tasks whose updates interleave and two children reuse the same inner tool-call ID;<br>
When the snapshots are processed;<br>
Then one outer card contains three ordered run groups, each step remains under its own run, runtime IDs do not collide, models/output do not cross runs, and progress counts terminal runs only.

### A5 — Official parallel reports partial completion honestly

Given a terminal parallel result with at least one successful and one failed run;<br>
When the outer card settles;<br>
Then successful and failed run statuses remain visible and the outer status is PARTIAL, not DONE and not an undifferentiated full ERROR.

### A6 — Official chain exposes sequence and skipped work

Given a three-step chain whose second run fails before the third starts;<br>
When final details arrive;<br>
Then the first run is completed, the second failed, the third skipped, the outer status failed, and the third contains no invented tools/model/output;<br>
And a valid chain longer than eight steps is accepted without applying the parallel-only cap.

### A7 — Generic flat-single supports dev-kit shape

Given a `{task, profile, model?, thinking?, cwd?}` invocation (including blank optional model/thinking/cwd), or a custom `{agent, task}` invocation whose first unambiguous snapshot is flat `details.messages`, with `exitCode=-1` while running;<br>
When child messages arrive and a later stray official `details.results` snapshot follows;<br>
Then flat profile input locks immediately, agent-single pending input locks from the flat envelope, neither switches afterward, the normal single AgentSpawnCard displays profile separately from agent identity, streams nested tools, and uses the first observed assistant model over the requested alias;<br>
And flat final success/error map to COMPLETED/FAILED while `stopReason=aborted` maps explicitly to CANCELED.

### A8 — Final snapshot recovers missed updates

Given no update frame is delivered but final official or flat-single details contain a complete child tool call/result history;<br>
When the end frame is processed;<br>
Then the missing steps are emitted once before SubagentDone and outer ToolResult.

### A9 — Independent malformed fields degrade independently

Given a supported multi-run invocation where one result lacks a model, another has malformed messages and a third is valid;<br>
When the snapshot is processed;<br>
Then the valid run continues updating, missing model only removes that badge, malformed messages do not fail the outer turn, and no synthetic values are displayed;<br>
And Given an agent-single pending snapshot exposes both valid top-level `details.results` and `details.messages`, or exposes `details.results` with `details.mode=parallel|chain`;<br>
Then that ambiguous/mismatched snapshot emits no child events and does not lock the envelope, while a later unambiguous official-single or flat snapshot may lock it exactly once.

### A10 — Incomplete final evidence settles deterministically

Given single/flat final details contain usable messages but no valid terminal exit/stop evidence;<br>
When SubagentDone is produced;<br>
Then outer success persists the sole run and aggregate as terminal UNKNOWN, while outer error persists them as FAILED; any previously emitted child call without a matching result also stops spinning and displays UNKNOWN/FAILED from that run without a synthetic result;<br>
And Given a grouped outer success ends with no usable final details, or a partial final snapshot leaves declared runs without terminal evidence;<br>
Then every unresolved run is persisted as terminal UNKNOWN and a successful outer aggregate is UNKNOWN rather than fabricated DONE/PARTIAL;<br>
And Given the same grouped incomplete evidence with outer `isError=true`;<br>
Then unresolved runs remain UNKNOWN while the aggregate is FAILED;<br>
And Given a partial chain contains a definitive failed run;<br>
Then later never-started declared runs are SKIPPED, while final `exitCode=-1` without terminal stop evidence is UNKNOWN.

### A11 — Abort leaves no running replay state

Given single, parallel or chain runs are active when the whole Pi turn is aborted without a tool end frame;<br>
When existing cleanup executes and the session is reloaded;<br>
Then the outer card is canceled, every previously non-terminal run is canceled, terminal runs retain their status, unmatched child calls display CANCELED/STOPPED from their containing run without synthetic results, and no spinner remains.

### A12 — Persistence and replay preserve grouping

Given a completed grouped card with nested steps and per-run models/output has been persisted;<br>
When the user reloads the session;<br>
Then mode, run order, statuses, badges, output and step grouping match the live view, while a historical name-only `*subagent*` tool without normalized state remains raw.

### A13 — Nested child data never enters outer model context

Given persisted nested tool calls/results contain distinctive sentinel content and the extension's outer ToolResult contains its normal summary text;<br>
When the next outer LLM request is assembled;<br>
Then raw child messages and projected `ToUI` nested blocks are excluded while remaining available to history projection, but the pre-existing extension-authored outer ToolResult summary remains in tool context exactly once.

### A14 — Wire preserves grouping and all Pi/runtime logs redact payloads

Given raw Pi RPC frames, final-error diagnostics, stderr/stopErrMsg, remote unknown/malformed `frame.Event`, ToolCall/ToolResult events, SubagentStarted/Progress/Done snapshots and chat-service runtime ErrorEvent/finalization carry distinct sentinel values through local and remote execution;<br>
When parsing, wire forwarding, user-visible/persisted error handling and all operational logging complete;<br>
Then runtime/receiving side and existing UI/`ErrorText` retain the required data, but Pi runtime, remote runtime, chat-service and daemon logs contain none of the raw frame/event, full error message/frame, stderr/stopErrMsg, task, input, result, meta, summary, error or sentinel payloads.

### A15 — Legacy single and background Bash behavior do not regress

Given existing Claude Code single-subagent fixtures with no `mode/runs` fields and a legacy `local_bash` background state block;<br>
When rendered live and via replay;<br>
Then the Claude card's current task, model, progress, STEPS, SUMMARY, stop behavior and accessibility continue unchanged, while `local_bash` is not reclassified as an AgentSpawnCard merely because a SubagentStateBlock exists.

### A16 — Grouped UI is usable and accessible

Given parallel/chain state with waiting, running, completed, failed, skipped and unknown runs;<br>
When the user expands the outer card and toggles run groups by mouse or keyboard;<br>
Then activity auto-opens untouched runs, manual collapse is respected across updates, every status including UNKNOWN is conveyed by translated text, dynamic agent/profile/model/task/output are not translated, and both locale coverage and TypeScript typecheck pass.

## Verification plan

Real-application evidence lives under:

```text
e2e/scratch/2026-08-05-pi-subagent-live-steps/
```

`report.md` must be created from `docs/references/verification-report-template.md` before starting the app. The deterministic fake Pi RPC source must exercise at least:

1. official parallel with two interleaved child tool calls, distinct models, one delayed result and final replay;
2. official chain failure with a skipped later step;
3. flat-single/dev-kit-shaped update and completion;
4. unsupported `*subagent*` input remaining a RawToolCard;
5. grouped successful outer completion with missing structured final details settling visibly as UNKNOWN, including an unmatched child call that no longer spins;
6. official project-agent confirmation receiving deterministic cancellation and settling the card as CANCELED without a hung turn.

The report's single Verdict table lists A1–A16 exactly once and uses exactly the repository's three verdict values: `holds` when decisive evidence passes, `does not hold` when decisive evidence fails, and `not observed` when the criterion was not reached or evidence is insufficient. Mechanical and real-app checks follow the same vocabulary; the report must not invent a fourth value or describe red evidence as green. Decisive UI screenshots belong inline; wire/log privacy uses pasted command output with sentinel-absence evidence rather than terminal screenshots.

## Open questions

None.
