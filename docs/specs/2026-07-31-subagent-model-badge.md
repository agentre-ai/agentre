# subagent 卡片展示调用模型与头部瘦身

状态：待批准<br>
创建：2026-07-31<br>
更新：2026-07-31

## 问题与目标

**目标：让 claude 后端派遣的每一个 subagent 在 AgentSpawnCard 上直接显示它实际使用的模型，并把头部让给身份信息——用量数字下沉到展开区。**

**硬不变量：主 agent 的模型、用量与上下文进度不得被 subagent 的模型或用量污染。** `parent_tool_use_id != ""` 的帧属于独立 Anthropic 会话，现有隔离规则（`pkg/claudecode/stream.go:256-262`、`session.go:623-626`）不得因本改动回退。

### 问题

1. **卡片头部没有模型，却塞满了数字。** `frontend/src/components/agentre/canonical-tool/agent-spawn/card.tsx:198-282` 渲染 `taskDescription` / `subagentType` / `toolUses` / `totalTokens` / 状态胶囊，唯独没有模型。除 description 外全部是 `shrink-0`，所以 description 先被截断、其余元素继续顶出卡片边界（卡片 `overflow-hidden`）。卡片最大宽 720px（`--container-measure: 45rem`，`frontend/src/styles/globals.css:227`），窗口最小宽 860px（`main.go:32`）扣掉 68px 导航栏（`chrome.tsx:60`）与会话侧栏后下限约 520px；在 520px 下现状的 description 完全消失、右侧顶格。
2. **canonical 层把已有的模型数据丢掉了。** `canonical.AgentSpawn`（`internal/pkg/agentruntime/canonical/agent_spawn.go:5-16`）没有模型字段；`AgentSpawnFromInput`（`from_tool_use.go:82-95`）只取 `description` / `subagent_type` / `prompt`，显式丢弃 `input["model"]`。本机 `~/.claude/projects/**/*.jsonl` 的 1549 次 `Agent`/`Task` 调用中有 1145 次（73.9%）带 `model` 键（`sonnet` 839 / `opus` 158 / `haiku` 148），其余 389 次不带。
3. **解码层不消费 subagent 帧的真实模型。** `pkg/claudecode/stream.go:109-111` 注释写明「普通 assistant 帧的 Anthropic `message.model` 我们暂不消费」，`rawMessage`（`stream.go:195-201`）确实没有该字段。真 CLI 2.1.220 抓帧证明这条信息一直在流里：`.dev-kit/artifacts/2026-07-31-subagent-model-badge/cli-2.1.220-subagent-model.jsonl` 第 12 帧为 `type:"assistant"` + `parent_tool_use_id:"toolu_018GjyED…"` + `message.model:"claude-haiku-4-5-20251001"`，同次抓帧第 7 帧的 `Agent` tool_use input 为 `model:"haiku"`——「调用方写的别名」与「实际解析出的完整模型 id」同时可得。
4. **累计态没有承载位置。** `blocks.SubagentStateBlock`（`internal/service/chat_svc/blocks/subagent_state.go`）与 `agentruntime.SubagentInfo`（`runner.go:105-116`）都没有模型字段，即使解出模型也无处存放，重开会话无法 replay。
5. **运行中的耗时被丢弃，卡片缺少活信号。** `SubagentProgressHandler`（`internal/service/chat_svc/handlers/subagent.go:83-90`）只抄 `TotalTokens` / `LastToolName` / `ToolUses`，不抄 `DurationMs`；`DurationMs` 只在 `SubagentDoneHandler` 才被写入。而 `task_progress` 帧本身带该值——同次抓帧第 11 帧为 `usage:{total_tokens:12056, tool_uses:1, duration_ms:1959}`。结果是运行中的状态胶囊只显示裸的 `RUNNING`，没有跳动的耗时；数字一旦下沉，运行中的卡片将没有任何活信号。
6. **`last_tool_name` 存了但从不展示。** 同一帧带 `last_tool_name:"Bash"`，`SubagentStateBlock.LastToolName` 也存了，但卡片从未渲染。本规格不引入它，仅在此记录为已知的未用数据。

### 基线事实

- `cd frontend && pnpm test --run`：197 文件 / 1665 测试全绿（2026-07-31）。
- `make test-backend`：退出码 2，唯一失败为 `pkg/codex` 的 `TestExecAppServerRunner_EnvShebangCanFindInterpreterNextToBinary`（`process_test.go:36`，`signal: killed`）。单独重跑为 `ok 2.146s`，属全量并跑时 3 秒硬超时的既有 flake，**先于本改动存在，不在本规格范围内**。

## 参与者与用户故事

- **Agentre 用户**：在对话流里扫一眼就知道某次子代理派遣跑的是哪个模型（判断能力档次与成本），并且在历史记录里不再被一排数字挤掉任务描述。
- **claude runtime / translator**：从 CLI 帧中取出模型，以不污染主 agent 的方式向上游透传。
- **chat service**：把模型并入 subagent 累计态、持久化，并在后台子代理的跨轮场景写回发起消息。
- **前端 AgentSpawnCard**：在头部展示身份（角色 + 模型）与状态，把用量数字收进展开区。

## 用户流程

### 派遣瞬间

1. 主 agent 调用 `Agent` / `Task` 工具，卡片出现。
2. 工具入参带 `model` 时，头部在角色芯片右侧多出一枚**描边模型徽标**，显示别名（如 `haiku`）；角色芯片是实心的，两者靠底色区分。
3. 工具入参不带 `model` 时，不渲染模型徽标，头部与今天一致。

### 子代理运行中

1. 头部形态为：`Agent · <任务描述> [角色] [模型] ⋯ ⚒<工具数> · <tokens> [RUNNING · 耗时]`。
2. 工具数与 tokens 以**无文案的极简形式**呈现（次级前景色，只有数字，不带「个工具」/「tok」字样）。
3. 状态胶囊上的耗时随 `task_progress` 帧跳动。
4. 子代理首帧内部 assistant 到达后，模型徽标由别名改显实际模型的归一化短名（`claude-haiku-4-5-20251001` → `haiku-4-5`）；此后同一子代理再出现其它模型的内部帧不改写。
5. 横向空间不足时，先截断任务描述，再截断极简进度；状态胶囊始终完整。

### 子代理完成后

1. 极简进度消失，头部只剩 `Agent · <任务描述> [角色] [模型] ⋯ [DONE · 耗时]`。
2. 完整的工具数、tokens、耗时与**未经裁剪的模型原值**出现在展开区顶部的 meta 行。

### 缺失与降级

1. 模型两种来源都没有值时，不渲染模型徽标，其余部分不变。
2. `subagent_type` 缺失但模型存在时，只渲染模型徽标。
3. 用量数字为零时，展开区 meta 行只显示有值的项。

### 历史与后台子代理

1. 重开会话或刷新后，模型随 `blocks_json` replay，不依赖运行时内存。
2. 以 `run_in_background` 派遣的子代理在会话空闲态产出内部帧时，其模型跨轮写回发起消息，重开会话仍可见。

## 范围

- claude 后端（本地执行与 agentred 远端执行两条路径）。**远端路径的覆盖以既有管道为限**：模型事件本身随 `internal/pkg/agentruntime/event_wire.go` 过远端 wire，per-turn 场景成立；但后台子代理的空闲活动通道（`SubagentActivitySource`）本就从未被 daemon 转发（`internal/daemon/` 无相关引用），因此 **A9 只在本地执行下成立**，远端补齐该通道不在本轮范围内。
- 前台子代理与后台（`run_in_background`）子代理。
- AgentSpawnCard 的头部布局调整、展开区新增 meta 行，及其持久化 / replay。
- 运行中耗时的补齐（问题 5），作为数字下沉的必要配套。

## 非目标

- **codex / piagent 后端**：canonical 层新增字段对它们是可选空值，本轮不为其接入数据来源。
- **展示 `last_tool_name`**（问题 6）：数据已在块里，但本轮不引入，避免与极简进度的展示位置打架。
- **子步骤级模型**：展开区 STEPS 里每个子调用不显示模型。
- **成本估算 / 价格换算**。
- **主 agent 模型展示的任何改动**。
- **去掉头部的「Agent」字样**：评估过（可再省约 44px），用户于 2026-07-31 选择保留。
- **后台任务面板**：不新增模型列。
- **本次抓帧中出现的 `rate_limit_event` / `hook_started` / `hook_response` / `thinking_tokens` 等帧类型的处理**。

## 行为要求

### R1 — 工具入参中的模型别名即时可见

`Agent` / `Task` 工具入参含非空 `model` 时，该值必须在派遣那一刻随 canonical 投影进入卡片，不等待子代理产出任何内部帧。入参无该键时不得伪造、不得从 `subagent_type` 或父会话模型推导。

### R2 — 子代理内部帧的实际模型覆盖别名

`parent_tool_use_id` 非空的 assistant 帧携带非空 `message.model` 时，该值成为这次派遣的模型，覆盖 R1 的别名。「实际执行」胜过「调用意图」。

一个例外：CLI 为 API 错误合成的帧其 `message.model` 是占位符而非真实模型，不得据此记录模型。判定以该帧的权威标志（`isApiErrorMessage`）为准，不以占位符字符串的值为准——占位符取值可能随 CLI 版本变化，而 R3 的 first-wins 意味着一旦记错就永久留在持久化数据里。

### R3 — 同一子代理只认第一个实际模型

R2 的覆盖对同一次派遣只发生一次；模型一经记录，后续内部帧不再改写。

### R4 — 部分更新不得清空既有累计态

承载模型的事件与流事件只更新模型字段；`toolUses` / `totalTokens` / `durationMs` / `lastToolName` / `status` 在后端累计态、前端 store 与持久化 JSON 三处都不得因此被清零或改写。

### R5 — 隔离不变量不回退

subagent 帧的模型与用量不得写入主 agent 的模型、用量或上下文进度；`EventInit` / `EventDone` 的 `Model` 语义保持不变。

### R6 — 持久化与 replay

模型随 subagent 累计态落入消息 `blocks_json`；重开会话后展示与流式期间一致。后台子代理在空闲活动轮取得的模型跨轮写回发起消息。

### R7 — 模型以独立描边徽标展示

模型渲染为角色芯片右侧的独立徽标，视觉上与实心角色芯片区分（描边、次级前景色）。徽标文本只做两项裁剪：剥去 `claude-` 前缀、剥去结尾的 8 位日期段；其余字符原样保留。未经裁剪的完整原值在展开区 meta 行可见。

横向空间充足时徽标完整显示裁剪后的全文，不做与宽度无关的固定截断；空间不足时它按 R11 的让位次序参与收缩并以省略号截断（省略号必须真实呈现，不得齐字中断）。经网关接入的第三方模型名可能很长，徽标不得因此把状态胶囊挤出可视区。

### R8 — 用量数字下沉到展开区

完整的工具数、tokens、耗时与模型原值移入展开区顶部的一行 meta，位于 TASK PROMPT 之上。零值项不显示。

### R9 — 运行中保留极简进度，完成后消失

子代理处于运行态时，头部在状态胶囊左侧显示极简进度（工具数与 tokens，纯数字、无文案、次级前景色）。非运行态不渲染该元素。

### R10 — 运行中的耗时必须跳动

`task_progress` 帧携带的 `duration_ms` 必须并入 subagent 累计态，使运行中的状态胶囊显示耗时并随进度更新。这是数字下沉后头部仍有活信号的前提。

### R11 — 溢出时的让位次序

横向空间不足时的牺牲次序固定为：任务描述 → 极简进度 → 其它。状态胶囊在任何宽度下保持完整，不被裁切。

## 状态、数据与接口

- **模型的两个来源分居两层，与既有的静态 / 运行时分工一致**：入参别名走 canonical 静态投影（与 `taskDescription` / `subagentType` / `prompt` 同层），实际模型走 subagent 运行时累计态（与 `toolUses` / `totalTokens` / `status` 同层）。前端 `readSpawn`（`card.tsx:39-54`）现成的「运行时字段非零则覆盖静态字段」合并语义直接适用，不引入新的合并机制。
- **解码单点**：`parseAssistantContentWithUsage`（`pkg/claudecode/session.go:1071`）是 `Session.parseLine` 与 `frameDecoder.decodeLine` 两条解码路径共用的助手函数，模型解析只在此一处发生，两条路径自动一致。
- **事件形态**：模型以独立事件类型透传，不复用 `SubagentProgress`（理由见实现决策 3）。该事件同样需经 `internal/pkg/agentruntime/event_wire.go` 的远端 wire 编解码，远端 daemon 执行的会话行为与本地一致。
- **跨轮写回**：复用既有 `chat_repo.SubagentProgress` 补丁结构与 `PatchSubagentProgress`（`internal/repository/chat_repo/message.go:260`）的「零值字段跳过」语义，不新增仓储方法。R10 的 `DurationMs` 已在该结构中，无需扩展。
- **展示层裁剪只在前端**：后端存原值，短名由前端纯函数计算。
- **新增静态文案**：展开区 meta 行的字段标签属静态 UI copy，须走 `t(...)` 并同时更新 `zh-CN` 与 `en` 两份 locale。模型名、角色名本身是动态数据，不翻译。

## 持久化数据变化

| 变化 | 影响级别 | 影响既有数据 | 迁移与回滚 |
| --- | --- | --- | --- |
| `subagent_state` 块新增可选模型字段（写入消息 `blocks_json`） | compatible for future writes | 既有消息不批量改写；旧块缺该字段 → 不渲染模型徽标 | 无 schema 迁移。回滚旧代码后新字段被忽略，块仍可正常反序列化 |
| 运行中的 `duration_ms` 开始写入 `subagent_state` 块（R10） | compatible for future writes | 既有已完成块的 `duration_ms` 已由 Done 路径写入，不受影响 | 无迁移。回滚后运行中不再写入，完成时仍写 |

- **不可逆部分：本次没有。** 不删除、不改写任何既有块。
- **无数据库 schema 迁移**：累计态序列化进 `blocks_json`，新增可选字段不触及表结构，不需要追加 `migrationList()` 条目。

## 安全、隐私、兼容性与可访问性

- **安全与隐私**：模型名不是凭证，不新增任何外发数据；不因本改动增加 prompt / 工具结果的落盘。
- **兼容性**：以真 CLI 2.1.220 抓帧为基线。老 CLI 不发 `message.model` 或工具入参不带 `model` 时走降级路径，不报错、不阻断。
- **可访问性**：模型是可见文本，天然进入无障碍树，不依赖颜色或图标单独编码。极简进度去掉了「个工具」/「tok」**可见**文案，展开区 meta 行的带标签完整值承担完整语义；同时极简进度元素本身须带无障碍名（不可见，仅供屏幕阅读器），使折叠态下两个裸数字也可被理解。该无障碍名属静态 UI copy，走 `t(...)` 且两份 locale 同步，并须正确处理单复数。不得使用 `title` 之类会在悬停时**可见**地把文案带回来的属性——那会推翻 R9 对该元素「纯数字、无文案」的定义。
- **视觉**：模型徽标使用既有 `border-strong` 描边与 `muted-foreground` 前景，极简进度使用 `subtle-foreground`，均为既有令牌，不新增颜色令牌。徽标沿用 `text-meta` + `font-mono`，与 `docs/DESIGN.md` 中「模型名用 `font-mono`」一致。

## 实现决策

1. **模型用独立描边徽标，不与角色芯片合并。** 先评估过「融进角色芯片」（`general-purpose · opus` 单芯片，省约 34px）；独立徽标多占约 56px，但角色与模型是两个可独立缺失的维度，合并芯片在只有其一时需要特殊拼接逻辑，且两段不同语义挤在同一底色里可读性更差。用户于 2026-07-31 在 mockup 对比后选定独立徽标。
2. **用量数字下沉到展开区，头部只留身份与状态。** 头部横向预算是实测约束（问题 1）。下沉后完成态的头部显著变短——而历史记录里绝大多数卡片是完成态；运行态因为新增模型徽标，长度与现状基本持平，收益主要体现在完成态与 720px 下终于能完整显示任务描述。拒绝「全部下沉、运行中什么都不留」，因为那会让运行中的卡片失去数字类活信号（见决策 4）。
3. **模型用独立事件类型透传，拒绝复用 `SubagentProgress`。** `SubagentProgressHandler`（`handlers/subagent.go:83-90`）对 `TotalTokens` / `LastToolName` / `ToolUses` 是无条件赋值，塞一个只带模型的 `SubagentInfo` 会把已累计的进度清零；前端 `mergeSubagentMeta`（`chat-streams-store.ts:728-746`）做浅合并，而 `SubagentStateBlock.Status` 的 JSON 标签没有 `omitempty`，部分载荷会把状态覆盖成空串。改造既有 handler 为「非零才赋值」虽可行，但会把语义不同的两个来源挤进同一事件，且要同时改动前后端的合并语义，风险大于新增一个事件类型（OCP：以新类型扩展而非修改既有分支）。
4. **补齐运行中的 `duration_ms`（R10）属本轮范围，不算无关改动。** 它是数字下沉的必要配套：不补，运行中的卡片将没有任何跳动元素。数据来源（`task_progress.usage.duration_ms`）与目标字段（`SubagentStateBlock.DurationMs`）都已存在。

   **范围延伸（2026-07-31 收尾复审后经用户裁决追加）**：`TotalTokens` / `ToolUses` / `DurationMs` 三者出自同一个 CLI `usage` 对象，而该对象是值类型、无存在性区分——缺 `usage` 的帧解码成全零，无条件赋值会把已累计值抹回 0。仓库层 `PatchSubagentProgressInBlocksJSON` 早已按此跳过零值，handler 层却没有，同一字段两条写路径策略相反。因此三个字段在 `SubagentProgressHandler` 与 `SubagentDoneHandler` 两处统一采用「零值不覆盖」。拒绝只守 `DurationMs`：留一个字段有守卫会让后来者误以为另两个是故意不守。`LastToolName` 不在 `usage` 对象内，本轮不动，其两条写路径的策略差异记为待办。
5. **运行中的极简进度保留工具数与 tokens 两个数字，不改用 `last_tool_name`。** 评估过用当前工具名（`⚒ Bash`）替代数字——更短且更能说明「在干什么」，数据也已在块里；用户于 2026-07-31 选择保留数字形式。`last_tool_name` 的展示留作独立议题（非目标）。
6. **解析放在两条解码路径共用的 `parseAssistantContentWithUsage`，拒绝各写一遍。** 该仓库已有「两个 decoder 的 assistant 分支成对维护」的负担（`stream.go:242-247` 与 `session.go:613-621` 的 API 错误帧处理），共用助手函数是既有的正确接缝。
7. **实际模型覆盖入参别名，而非相反。** 别名是调用意图（`haiku`），实际帧是执行事实（`claude-haiku-4-5-20251001`），本特性回答的是「跑的是什么」。代价是徽标文本在子代理首帧到达时变化一次；这一次性精化优于长期显示可能与实际不符的别名。
8. **同一子代理只认第一个实际模型（first-wins）。** CLI 会在子代理内部用小模型做压缩 / 摘要，last-wins 会让卡片最终显示成内部辅助模型。与既有 usage 的 zero-clobber 守卫（`stream.go:256-262`）同一思路。
9. **短名裁剪只剥 `claude-` 前缀与结尾 8 位日期段，其余原样。** 经网关接入的第三方模型名是任意串（GLM / openrouter 等），进一步「美化」可能把它改坏。裁剪放前端纯函数，后端存原值。
10. **溢出让位次序固定为「描述 → 极简进度 → 其它」（R11）。** 状态胶囊是卡片最重要的一眼信息，不能被裁；极简进度是补充信号，可截断。实现上极简进度允许收缩，胶囊保持不收缩。
11. **后台子代理复用既有 `PatchSubagentProgress` 跨轮补丁，不新增仓储方法。** 该方法已具备「零值字段跳过 + per-session 读改写锁」语义（`message.go:212-268`）。
12. **不新增数据库迁移。** 累计态序列化进 `blocks_json`，新增可选字段无需触及 schema。

明确拒绝：

- 从 `subagent_type` 或 agent 定义文件推断模型（agentre 不读 `~/.claude/agents/*.md`，推断会显示与实际不符的值）。
- 用父会话模型给缺失项兜底（继承关系不是事实，会把「不知道」伪装成「知道」）。
- 在展开区 STEPS 的每个子调用上显示模型。
- 为 codex / piagent 后端在本轮补数据来源。

## 测试接缝

- **`pkg/claudecode` 解码出口**：以扩展后的 `testdata/stream_subagent.jsonl` fixture（按真 CLI 2.1.220 抓帧补 `message.model`）驱动，断言 subagent 帧产出携带模型的事件、主 agent 帧不产出该事件、缺 `model` 的老帧不产出该事件。两条解码路径都覆盖。
- **`chat_svc` 事件 → 累计态 / DTO**：断言模型进入 `SubagentStateBlock` 与 `canonical.AgentSpawn` 投影；断言 first-wins（R3）；断言模型事件不清空进度字段（R4）；断言 `task_progress` 的 `duration_ms` 进入累计态（R10）。
- **`canonical.AgentSpawnFromInput` 纯函数**：断言入参 `model` 被提取、缺失时不产出该字段。
- **`chat_repo` JSON 改写纯函数**：断言模型经 `PatchSubagentProgressInBlocksJSON` 就地写入命中块、其余字段不动（后台子代理跨轮路径）。
- **前端卡片渲染 + 短名纯函数**：断言模型徽标渲染与运行时值覆盖静态别名；无模型时不渲染徽标（R7）；运行态显示极简进度、完成态不显示（R9）；工具数 / tokens / 耗时 / 模型原值出现在展开区 meta 行（R8）；短名裁剪规则含第三方模型名原样保留（R9 裁剪部分）。
- **非自动化、以复核代替**：520px 与 720px 下的实际观感与让位次序（R11），以 mockup 与真机目测确认，不写像素级断言。

## 验收标准

### A1（R1）入参别名在派遣瞬间可见

Given 主 agent 发出的 `Agent` 工具调用入参含 `model: "haiku"`、`subagent_type: "general-purpose"`；<br>
When 该 tool_use 帧到达、子代理尚未产出任何内部帧；<br>
Then 头部在角色芯片右侧渲染描边模型徽标，内容为 `haiku`。

### A2（R2、R7）实际模型到达后覆盖并归一化

Given A1 的卡片已显示别名徽标；<br>
When 子代理首帧内部 assistant（`parent_tool_use_id` 指向该派遣）携带 `message.model: "claude-haiku-4-5-20251001"`；<br>
Then 徽标改显 `haiku-4-5`，展开区 meta 行显示完整原值 `claude-haiku-4-5-20251001`。

### A3（R2）无别名也能显示

Given `Agent` 工具入参不含 `model` 键；<br>
When 子代理首帧内部 assistant 携带 `message.model: "claude-opus-5"`；<br>
Then 徽标显示 `opus-5`。

### A4（R7）两种来源都缺时不渲染徽标

Given 工具入参不含 `model`，且子代理未产出任何带 `message.model` 的内部帧；<br>
When 卡片渲染；<br>
Then 头部不出现模型徽标、不出现占位符或空槽，其余元素位置与今天一致。

### A5（R3）内部辅助模型不改写已记录模型

Given 某次派遣已记录 `claude-opus-5`；<br>
When 同一 `parent_tool_use_id` 之后到达一帧 `message.model: "claude-haiku-4-5-20251001"`；<br>
Then 记录与展示仍为 `opus-5`。

### A6（R4）模型更新不清空进度

Given 卡片累计态已有「3 个工具 / 14.5K tok / RUNNING」；<br>
When 模型更新事件到达；<br>
Then 工具数、token 数与运行状态在前端 store、后端累计态与持久化 JSON 三处均保持原值。

### A7（R5）主 agent 不被污染

Given 一轮里主 agent 用 `claude-opus-5`、其派遣的子代理用 `claude-haiku-4-5-20251001`；<br>
When 该轮结束；<br>
Then 助手消息记录的模型与用量仍取自主 agent 帧，上下文进度不出现骤降。

### A8（R6）replay 后模型仍在

Given 某次派遣的模型已在流式期间记录；<br>
When 用户切走再切回该会话（走历史 replay 而非实时流）；<br>
Then 徽标展示与流式期间一致。

### A9（R6）后台子代理跨轮记录模型

Given 一个以 `run_in_background` 派遣的子代理，其发起轮已收尾、会话处于空闲态；<br>
When 该子代理在空闲活动轮产出携带 `message.model` 的内部帧；<br>
Then 模型写回发起消息的 `subagent_state` 块，重开会话后可见，且该块其它字段不被改写。

### A10（R8、R9）数字随状态在头部与展开区之间分工

Given 一次派遣的累计态为「3 个工具 / 14.5K tok / 7.8s」；<br>
When 它处于运行态；<br>
Then 头部在状态胶囊左侧显示纯数字形式的工具数与 tokens（不含「个工具」「tok」字样），且展开区 meta 行同时可见带标签的完整值；<br>
And When 它转为完成态；<br>
Then 头部不再显示该极简进度，完整数值只在展开区 meta 行。

### A11（R10）运行中的耗时跳动

Given 一次派遣处于运行态；<br>
When `task_progress` 帧携带 `duration_ms: 1959` 到达；<br>
Then 该值进入累计态，状态胶囊显示为 `RUNNING · 2.0s`，并随后续 progress 帧更新。

### A12（R8）零值项不占位

Given 一次派遣完成时 tokens 为 0；<br>
When 用户展开卡片；<br>
Then meta 行只列出有值的项，不出现 `0 tok` 之类的空占位。

## 开放问题

无。

## 参考

- UI 方案对比 mockup（本地产物，不入 Git）：
  - `.dev-kit/artifacts/2026-07-31-subagent-model-badge/mockups/placement.html`——模型摆放位置的四种方案（融进角色芯片 / 归入用量组 / 独立徽标 / 只在展开区）。
  - `.dev-kit/artifacts/2026-07-31-subagent-model-badge/mockups/slim-header.html`——头部瘦身后的四种活信号方案，宽度取实测区间 720px（卡片最大宽）与 520px（窗口最小宽扣掉导航栏与会话侧栏后的下限）。
  两份都使用 `frontend/src/styles/globals.css` 的真实令牌。决定性内容已写入 R7–R11 与实现决策 1、2、5，本规格不依赖打开这些文件即可读懂。
- 真 CLI 2.1.220 抓帧（本地产物，不入 Git）：`.dev-kit/artifacts/2026-07-31-subagent-model-badge/cli-2.1.220-subagent-model.jsonl`。
- `docs/DESIGN.md` §排版：模型名属 `font-mono` 一类。
- `docs/agent-backend.md`：agent 后端接入与 translator / capability 约定。
