# 会话内切换 LLM 模型

> Status: Draft
> Owner: chat experience / backend
> Last updated: 2026-08-06

**Objective:** 用户能在会话对话框（composer）里为**当前会话**选择一个不同的 LLM 模型：选择持久化到该会话，自下一轮起生效，覆盖四个可切换后端（builtin / claudecode / codex / piagent）以及远端 `agentred` 执行；若后端静默回退到所选之外的模型，会话里出现一条提示。新建会话在首发消息前也能预选模型。

**Hard invariants:**

1. **无 override 的会话行为完全不变：** `chat_sessions.model_override` 为空时，每轮仍按现状解析（`agent_backend.LLMProviderKey → llm_provider.Model`，claudecode 未绑 provider 时回退 `DefaultModel`）。
2. **override 只影响模型，不影响绑定：** provider / agent / backend / cli_path / reasoning_effort / permission mode 一律不变。
3. **`chat_messages.Model` 仍是实际运行模型：** 每轮记录的实际模型语义不回归；codex 后端需把该值修正为"线程实际模型"（见决策 9）。
4. **新 UI 文案全部走 i18n：** 新增 key 同时覆盖 `zh-CN/common.json` 与 `en/common.json`；`i18n.test.ts` 校验通过。不得硬编码中文。
5. **控件用 shadcn：** 模型选择用 `Popover`（沿用 `PermissionModePill` 模式），不引入原生 `<select>`。
6. **claudecode 缓存复用不被破坏：** 模型未变时 LRU 命中仍复用子进程；模型变化时按 `ReasoningEffort` 既有先例 evict + respawn。
7. **未绑 provider 的已有会话绝不提供切换：** 该状态下选择器灰显不可交互，不发起任何 `--model` 变更或 respawn。

## Problem

1. **会话级无法选择模型。** 模型每轮由 `chat_svc.runTurn` 从 `be.LLMProviderKey → prov.Model` 解析（`internal/service/chat_svc/chat.go` 拼 `RunRequest{Provider: prov}`）；`chat_sessions` 只有 `ProviderSessionID`，无任何模型覆盖字段。用户在对话框里没有换模型入口，只能去改 agent 绑定的供应商默认。
2. **各后端把模型绑定在"启动时刻"，机制不同。** claudecode 在 spawn 时 `--model` 下发且子进程常驻 LRU 缓存；codex 每轮新起 app-server 进程、模型锁在线程上（`thread/resume` 返回线程模型）；piagent 每轮 `--model` 下发、按 `--session` 恢复；builtin 进程内每轮读取。一个 override 必须在**每个 runtime 的模型解析点**统一生效，claudecode 还要处理"模型变了 → 逐出重spawn"。
3. **远端执行隐藏 provider。** `req.Provider` 在远端被置 nil、daemon 用 `LLMProviderKey` 自解（`internal/daemon/handlers/runtime.go`），override 必须随 `wire.RunParams` 过线并在 daemon 侧应用，否则远端会话换模型失效。
4. **后端可能静默回退模型。** 实测 codex（0.146.0）对 provider 不认识的模型 id 在 `thread/resume` 上静默沿用线程原模型（无报错）；这类偏离若无提示会让用户误以为切换成功。

## Actors and user stories

1. 作为会话中的用户，我希望在 composer 里给当前会话换个模型，以便剩余对话（自下一轮起）用新模型，不需要动 agent 配置。
2. 作为即将开始新会话的用户，我希望在发第一条消息前就选好模型，以便首轮就用上。
3. 作为远端 `agentred` 会话用户，我希望同样的切换在远端也生效。
4. 作为选了不被后端支持的模型的用户，我希望看到"未生效"提示，而不是被静默误导。

## Design decisions

| # | Decision | Basis and rejected option |
|---|---|---|
| 1 | **会话级持久覆盖**：`chat_sessions.model_override`（text，`''`=跟随供应商默认），一次设置持续生效、跨重启保留 | 贴合"在对话框切换模型"语义；最简心智。拒绝"单轮一次性"——每条都要选、心智重；拒绝"两者都要"——复杂，超出当前需求 |
| 2 | **override 优先于 provider 默认**：每轮 `RunRequest.ModelOverride` 非空则用它，空则走现有 provider 默认解析 | 各 runtime 已消费 `req.Provider.Model`，在每处解析点 `firstNonEmpty(override, providerModel, backendDefault)` 即可，claudecode/piagent 的既有模型映射（`--model`、`agentre-<key>/<model>`）保持。拒绝"clone 并改写 `req.Provider.Model`"——改共享实体、远端路径无法用 |
| 3 | **composer 动作行放 ModelPill**，与 `PermissionModePill` 并排 | 两个会话级开关聚在一起、延续 `permissionModeSlot` 模式，改动面最小。拒绝放头部 toolbar——44px 单行已挤（mockup 对比见 artifacts） |
| 4 | **新建会话首发前也能预选**：composer 在无会话时显示同款 pill，瞬态选择随 `SendRequest.ModelOverride` 透传，`send()` 在 `chat_repo.Session().Create` 时一并落库 | 开聊前定好模型是高频场景；首轮即用上。拒绝"首发后用默认再切"——体验断层 |
| 5 | **回合后偏离提示**：每轮结束后，若 override 非空且 `RunResult.Model` 非空且 ≠ override，会话里出现一条持久提示"所选模型 X 未生效，实际使用 Y" | 兜住 codex/piagent 静默回退。拒绝"不提示"——误导用户；拒绝"切换前校验"——需要真实 turn 才知道，不可廉价做 |
| 6 | **v1 覆盖四个有实测依据的后端 + 远端透传；openclaw 为 follow-up** | builtin/claudecode/codex/piagent 均已实测可切换；openclaw 模型要走 `agent` 方法 + `operator.admin` scope，且无真机 Gateway 验证，v1 硬塞有返工风险 |
| 7 | **模型列表源分两种**：已绑 provider → 该 provider 的 `/v1/models`（复用 `ListLLMModels`）；未绑 provider 的新建会话 → 弹层内**自由输入模型 id**（无结构化列表） | 已绑时 `/v1/models` 是跨后端一致的源；未绑时没有 provider 可列，自由输入最诚实，非法 id 由 CLI spawn 时报错。拒绝各 CLI 自有模型目录（`claude model list`/`pi --list-models`）——三种格式不一致、要加 per-backend 发现逻辑，破坏单一模型源 |
| 8 | **选择器按「绑定 × 会话状态」enable/disable，而不是隐藏**：已绑 provider → 新建/已有会话都可选可切；未绑 provider → 新建会话可选（自由输入，作为 spawn 时 `--model`，走 CLI 自身登录/配置）、已有会话灰显不可切（tooltip 说明"未绑定供应商，已有会话无法切换模型"） | 未绑已有会话技术上也能 resume 换模型，但**刻意不支持**——没有可靠列表/校验时避免误配，记为产品决定。拒绝"未绑一律不显示"——丢掉新建会话的可用能力；拒绝"未绑已有会话也可切"——用户明确排除 |
| 9 | **codex 的 `RunResult.Model` 改为上报线程实际模型（`sess.Model()`），而非启动请求模型** | 决策 5 的偏离提示依赖"实际模型"信号；无 override 时两者同值，行为不回归。拒绝保持现状——偏离提示对 codex 失明 |

## 数据与状态

- `chat_sessions.model_override`（text，`not null default ''`）；追加新 migration 到 `migrationList()` 末尾，不改既有 migration。
- `agentruntime.RunRequest.ModelOverride string`（`omitempty`）；各 runtime 模型解析点优先读它。
- `remote` wire：`RunParams.ModelOverride string`（`json:"modelOverride,omitempty"`）；daemon 侧组装 `RunRequest` 时填入。
- `chat_svc.SendRequest.ModelOverride string`（新建会话专用，随首条消息落库；已有会话忽略，走 `SetSessionModel`）。
- 新增 Wails 绑定 `SetChatSessionModel(sessionID, model)` → `chat_svc.SetSessionModel`：写入 `model_override`；空串 = 清空覆盖回默认。
- `ChatSessionDetail` 增加 `ModelOverride`（本会话覆盖值，`''`=默认）与 `ProviderDefaultModel`（本会话 agent 的生效默认模型，服务端解析，供 pill 显示"当前默认"），沿用 `LLMProviderType` 的解析位置。

## 模型解析与各后端生效

- **统一规则：** `effectiveModel = firstNonEmpty(req.ModelOverride, req.Provider.Model, backendDefaultForKind)`；override 的取值是 provider 模型 id，在**现有 prov.Model 被消费的同一个位置**替换，因此 claudecode 的 `--model`、piagent 的 `agentre-<key>/<model>` 映射、codex 的 `thread/resume {model}`、builtin 的 `coding.WithModel` 全部沿用既有映射，不新造映射。
- **未绑 provider（登录态）路径：** override 是**裸 CLI 模型 id**，直接在 spawn 处作为 `--model` 下发（claudecode 走 `DefaultModel` 同一条 `--model` 通道；codex/piagent 同理走 CLI 登录/自身配置），不经过 agentre 网关、无 provider 映射。该路径只在**新建会话**产生（未绑已有会话被决策 8 禁用）。
- **claudecode**：`claudeActive` 记录 `launchedModel`；`acquireSession` 在 `cur.launchedModel != effectiveModel` 时 evict + 以 `--resume <ProviderSessionID> --model <effectiveModel>` 重spawn（镜像现有 `launchedEffort` 判断；`--model` 在 resume 上生效已在真实 CLI 2.1.222 实测：`glm-5.2` 会话以 `--model claude-haiku-4-5` resume，`system.init` 上报 `claude-haiku-4-5`）。
- **codex**：`buildLaunchSpec.spec.model = effectiveModel` → `thread/resume {model}`；每轮新进程的首次 resume 即生效（真实 CLI 0.146.0 实测：对 `gpt-5.6-sol` 线程 resume 传 `gpt-5.5` 返回 `gpt-5.5`）。`RunResult.Model` 取 `sess.Model()`（线程实际模型）。
- **piagent**：`providerRunConfig` 以 effectiveModel 构建 `--model`（真实 `pi` 实测：`gpt-5.6-sol` 会话以 `--model local-llm/gpt-5.6-terra` 恢复，`get_state` 上报 `gpt-5.6-terra`）。
- **builtin**：进程内每轮读取，`coding.WithModel(effectiveModel)`，历史从 `chat_messages` 重建，无子进程/缓存问题。

## 切换流程

- **已有会话**：用户点 composer 的 ModelPill → popover 列模型（含置顶"跟随供应商默认"）→ 选择 → `SetChatSessionModel` 落库 → 下一轮 `runTurn` 填 `req.ModelOverride` → 各 runtime 生效（claudecode 视模型变化 evict+respawn）。
- **新建会话（已绑）**：无会话时 composer 显示同款 pill，瞬态选择随首条 `Send` 透传，`send()` 在会话创建事务内写 `model_override`，首轮即用。
- **新建会话（未绑）**：pill 可用，弹层内自由输入模型 id（或选"跟随默认"），同样随首条 `Send` 落库，作为 spawn 时 `--model` 走 CLI 自身登录。
- **未绑已有会话**：pill 灰显不可交互（tooltip 说明原因），不发起任何切换。
- **远端会话**：同一 `SetChatSessionModel` 落库 → `runTurn` 填 `req.ModelOverride` → `remote.Runtime` 序列化进 `RunParams.ModelOverride` → daemon 组装 `RunRequest` 时回填 → 远端 runtime 同规则生效。
- **切换时机**：允许在回合运行中切换，但**自下一轮生效**（当前轮已以旧模型 spawn）；popover 底部常显"切换从下一轮开始生效，正在进行的回合不受影响"。

## UI（composer ModelPill）

- 摆放：composer 动作行，`PermissionModePill` 旁边（`permissionModeSlot` 同一行；模型 pill 紧随其后）。
- 形态：复用 `PermissionModePill` 的 pill + `Popover` 模式（`h-6 rounded-md border px-2 text-2xs font-medium` + caret），图标用方框/模型图标，模型名等宽字体截断。
- 弹层：头部"模型"标题 + provider 名；列表第一项"跟随供应商默认"（override 非空时点击清空；override 为空时该项高亮）；其下模型列表（名称 + 右侧 `contextWindow`），当前生效项高亮（`primary-soft` + 实心点徽标）；底部常显"切换从下一轮开始生效…"。
- 状态：模型列表加载中 → pill 禁用；provider 离线 → 弹层底部错误行（沿用 permission pill 的 `errorMessage` 样式）；override 已设 → pill 高亮 active 态并可选回默认；**未绑 provider + 已有会话 → pill 灰显（`disabled:opacity-60` + `cursor-not-allowed`），tooltip 说明"未绑定供应商，已有会话无法切换模型"，不渲染弹层交互**；**未绑 provider + 新建会话 → pill 可用，弹层第一项"跟随默认" + 一个模型 id 自由输入框（回车/提交）**。
- 可访问性：pill 有 `aria-label`/`title`（含当前模型名），列表项 `aria-selected`/`aria-disabled`，键盘可触发，镜像 permission pill。

## 偏离提示（决策 5）

- 触发条件：该轮 override 非空、`RunResult.Model` 非空、两者不等，且后端实际运行了其它模型（builtin 恒等不触发；claudecode/piagent 以实际模型上报；codex 以线程实际模型上报，见决策 9）。已绑与未绑新建会话都适用；未绑已有会话被禁用、无此路径。
- 形态：在会话对话流里追加一条**持久**提示记录（复用 cago `blocks.NoticeBlock` 概念投射为 ChatBlock；前端 transcript 新增 notice 渲染分支），内容含"所选模型 X"与"实际使用 Y"两个等宽模型名。
- 幂等：每次偏离发生都提示（用户据此知道该后端不接受该模型，可换一个或回默认）；未偏离不提示。

## Out of scope

- **openclaw 切换**：记为 follow-up，需真机 Gateway 验证"已存在会话上换模型是否生效"（admin scope + `agent` 方法）。
- **单轮一次性模型**（决策 1 拒绝项）与 **provider 切换**（本特性只在该 agent 已绑定的 provider 内换模型）。
- **各 CLI 自有模型目录作为列表源**（决策 7 拒绝项）；模型质量/排序不做。
- **偏离的自动"回滚到默认"**：只提示，不自动改 override。

## Testing decisions

| Seam | 验证内容 | Prior art |
|---|---|---|
| 实体/migration | `model_override` 默认 `''`、空值可过 `Check`、append 到 `migrationList()` 末尾 | 既有 migration + entity 测试 |
| `chat_svc` 单元（mockgen repo mock，不连 DB） | `runTurn` 填 `req.ModelOverride`（来自 `sess.ModelOverride`）；`SetSessionModel` 持久化/清空/校验；新建会话 `SendRequest.ModelOverride` 落库；偏离提示在 override≠`RunResult.Model` 时发出、相等/为空时不发 | `chat_test.go` 现有 runTurn/权限模式测试 |
| 各 runtime 单元（fake session / sqlmock 无关） | builtin/codex/piagent/claudecode 的 effectiveModel 解析（override > provider > 默认）；claudecode 模型变化 evict+respawn、未变复用 | 各 `runtime_test.go`；claudecode `launchedEffort` 测试先例 |
| 远端 wire | `RunParams.ModelOverride` 编解码 round-trip；daemon handler 组装 `req.ModelOverride` | `wire_test.go` + `daemon/runtime_imports_test.go` |
| 前端（vitest） | ModelPill 渲染态（默认/已覆盖/加载中/错误行）、**未绑已有会话灰显 disable + tooltip**、**未绑新建会话自由输入**、popover 交互、新建会话瞬态传递；i18n key 覆盖 | `permission-mode` 相关测试 + `i18n.test.ts` |
| 无法自动化的 | 各 CLI 对"换模型 resume"的真实行为（本次调研已用真实 claude/codex/pi 无 token 实测，见 Problem/设计决策依据）；后续回归靠 codex/claude 后端 eval 套件 | `docs/codex-backend-eval.md` |

## Open questions

<!-- 批准前必须为空 -->
