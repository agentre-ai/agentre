# 新建会话选择 LLM 供应商（替换会话级模型切换）

<!-- File: docs/specs/2026-08-09-new-session-provider-select.md -->

> Status: Approved（决策 2 的「不可事后修改」与决策 7 已被
> [2026-08-10 已有会话切换 LLM 供应商](./2026-08-10-session-provider-switch.md) 取代，
> 见下方逐条标注；其余部分仍然有效）
> Owner: chat experience / backend
> Last updated: 2026-08-09

**Objective:** 新建会话（composer 无会话态）把原来"选模型"的 pill 改成"选 LLM 供应商"：列出与该 agent 后端兼容的供应商，选中后该会话自首轮起用所选供应商的默认模型；同时彻底移除 2026-08-06 会话级模型切换（#26）引入的 `model_override` 全部代码与数据。本特性未发版，直接清理基线迁移，不留历史包袱。

**Hard invariants:**

1. **无 provider_key 的会话行为完全不变**：仍按现状解析（`agent_backend.LLMProviderKey → llm_provider.Model`；CLI 后端未绑供应商时回退 CLI 自身登录态）。
2. **provider 选择只影响供应商**：agent / backend / cli_path / reasoning_effort / permission mode / cwd 一律不变；选择不写回 agent 绑定（不污染其它会话）。
3. **`chat_messages.model` 保留**：仍是每轮实际运行模型的观测字段（transcript 展示），它不是 #26 模型切换的一部分。
4. **新 UI 文案全部走 i18n**：新增 key 同时覆盖 `zh-CN/common.json` 与 `en/common.json`；`i18n.test.ts` 校验通过。控件用 shadcn `Popover`，不引入原生 `<select>`。
5. **远端 agentred 执行同样生效**：会话级 provider 选择随 wire 透传，daemon 按 key 从自己的配置自解。
6. ~~**已有会话不再有模型/供应商切换器，也不显示只读供应商徽标**（用户决策：最干净，观测靠 transcript 每消息 `model` 字段）。~~ **被 [2026-08-10 规格](./2026-08-10-session-provider-switch.md) 取代**：已有会话在 composer 常显供应商选择器（不可切时 disabled + tooltip），切换自下一轮生效。本规格其余不变量仍然有效。

## Problem

1. **新建对话的 pill 选的是"模型"而不是"供应商"。** 用户在对话页新建会话时期望"选择 LLM 供应商"，实际弹层列的是已绑定供应商的 `/v1/models`（已绑）或自由输入模型 id（未绑）。供应商本身在 Agent 层绑定（`agent_backends.llm_provider_key`），对话页无法选（用户报告）。
2. **#26 会话级模型切换与"选供应商"诉求重叠，且未发版。** `model_override` / `SetChatSessionModel` / 模型偏离提示 / 四后端模型覆盖 / 远端 wire 是一套复杂机制，其能力被"新建会话选供应商 + 用供应商默认模型"覆盖。用户决定（B）整体移除，避免留死代码。
3. **`model_override` 若保留会在迁移历史里留下"建→删"两步。** 迁移机制为 gormigrate 按 ID 记录、无 checksum（`migrations/migrations.go` 用 `gormigrate.DefaultOptions`）；迁移历史已在上个 commit 压平为 20260808 基线，`model_override` 仅存在于 `migrations/202608080006_chat.go` 一处。未发版应直接改基线清理，而不是追加 DROP patch。

## Actors and user stories

1. 作为即将开始新会话的用户，我希望在发第一条消息前选好 LLM 供应商，以便首轮及整个会话都用该供应商的默认模型。
2. 作为为 CLI 登录后端（未绑供应商）起新会话的用户，我希望同样能选一个 agentre 供应商接管该会话。
3. 作为远端 `agentred` 会话用户，我希望新建时选的供应商在远端同样生效。
4. 作为会话所选供应商后来被删除/停用的用户，我希望会话仍可继续（回退 agent 绑定）并看到明确提示，而不是卡死。

## Design decisions

| # | Decision | Basis and rejected option |
|---|---|---|
| 1 | **整体移除 #26 会话级模型切换**：删 `model_override` 列、`SetChatSessionModel`、`SendRequest.ModelOverride`、模型偏离提示、四后端 `ModelOverride` 消费、远端 wire `ModelOverride` | 用户决策 B：未发版清理干净；能力被本特性覆盖。拒绝保留 `model_override` 与供应商选择并存——两套选择器复杂且无必要 |
| 2 | **新增会话级 `provider_key`（text，`''`=跟随 agent 绑定）**，新建会话随首条消息落库，~~**不可事后修改**~~ → **可事后修改**（「不可事后修改」这一条被 [2026-08-10 规格](./2026-08-10-session-provider-switch.md) 决策 1 取代：新增单列更新 `UpdateProviderKey` + `chat_svc.SetChatSessionProvider`，切换自下一轮生效；本行其余部分不变） | 会话需要一个稳定的供应商归属，类比被移除的 `model_override` 但粒度更粗。拒绝写回 agent 绑定——会全局污染其它会话 |
| 3 | **供应商解析：会话 `provider_key` > agent 绑定**，在 `resolveAgentBackend` 同一解析点生效 | 现状 `prov` 由 `be.LLMProviderKey` 解析（`internal/service/chat_svc/chat.go`）；插一层会话优先即可。拒绝为会话 clone 改写共享实体 |
| 4 | **新建会话 pill = 供应商选择器，只列与后端兼容的供应商**：builtin→全部；claudecode→anthropic；codex→openai-response；piagent→anthropic/openai-chat/openai-response；openclaw 不渲染 | 兼容性是后端硬约束（`ProviderTypeMatch`，`internal/model/entity/agent_backend_entity/kinds.go`），列不兼容项必然失败。拒绝列全部再报错 |
| 5 | **未绑供应商（CLI 登录态）的新建会话也显示供应商选择器**（用户 Q1） | 让 CLI 后端的新会话可选 agentre 供应商接管；不选则保持 CLI 登录态。拒绝保留自由输入模型 id——模型选择在新建会话消失 |
| 6 | **模型 = 所选供应商默认模型（`provider.Model`）**，新建会话不再有模型选择 | 用户 Option C："选完就用该供应商的默认模型（最简，不涉及模型选择）" |
| 7 | ~~**已有会话不渲染任何切换器，也不显示只读供应商徽标**（用户决策 b）~~ **被 [2026-08-10 规格](./2026-08-10-session-provider-switch.md) 决策 10 取代**：已有会话渲染同一颗 pill，不可切换时 `disabled` + tooltip 说明原因 | 原基础（用户决策 b：最干净，观测靠 transcript 每消息 `model`）已被推翻——隐藏让用户以为功能消失 |
| 8 | **会话 `provider_key` 指向的供应商被删/停用 → 回退 agent 绑定 + transcript 一条持久 notice**（复用既有 `NoticeBlock` 渲染，不是被删的模型偏离提示） | 用户 Q3；避免会话永久卡死（`ChatAgentNotChattable` 会把会话堵死）。notice 每次回退都追加，与 #26 偏离提示"每次发生都提示"先例一致 |
| 9 | **远端透传**：remote `RunRequest.LLMProviderKey` 改为 effectiveProviderKey（会话 `provider_key` 优先），daemon 按 key 自解 | 现状已按 `LLMProviderKey` 透传（`chat.go` remote 路径），替换为 effective 即可；daemon 自家 `ProviderLookup` 与 gateway 不变 |
| 10 | **迁移做法 1（改基线）**：直接改 `migrations/202608080006_chat.go`，删 `model_override` 列、加 `provider_key` 列 | 未发版 + 迁移历史已压平 + gormigrate 无 checksum；用户豁免"不修改既有迁移"硬规则；本地 dev 库需删除重建。拒绝追加 DROP patch——留"建→删"不干净 |
| 11 | **`chat_messages.model` 保留** | 每轮实际模型是观测字段，非 #26 切换的一部分；删除后 transcript 看不到每轮用的模型 |

## 数据与状态

- `chat_sessions`：`model_override` 删除，新增 `provider_key TEXT NOT NULL DEFAULT ''`（在基线迁移 `202608080006_chat.go` 内直接改，见决策 10；本地 dev 库需重建）。
- `chat_entity.Session`：删 `ModelOverride`，加 `ProviderKey`（gorm column `provider_key`）。
- `chat_svc.SendRequest.ProviderKey string`：仅新建会话生效，随首条消息与 Session 一同 Create 落库；已有会话在 `Send` 里仍忽略该字段——改供应商走 [2026-08-10 规格](./2026-08-10-session-provider-switch.md) 的 `SetChatSessionProvider`（原文「不允许事后改，无 Setter」已被取代）。
- `agentruntime.RunRequest`：删 `ModelOverride`；remote 路径 `LLMProviderKey` 改为 effectiveProviderKey。
- 前端：删 `useModelPill` 的模型列表 / 自由输入 / `SetChatSessionModel` 调用；新建会话改为供应商选择器；`ChatSessionDetail` 移除 `ModelOverride` / `ProviderDefaultModel` / `LLMProviderKey`——三者仅被旧 pill 消费（已核实 session 级 `llmProviderKey` 唯一消费者是 chat-panel 的 ModelPill）；`LLMProviderType` 保留（仍有 Usage token 语义）。

## 会话创建与解析流程

- **新建会话（Send）**：`SendRequest.ProviderKey` 非空时校验——供应商必须存在、`IsActive()` 且与后端 kind 兼容（`ProviderTypeMatch`），否则 Send 报错（复用不可对话的错误语义，不在落库后才发现）。校验通过后与 Session 一起 Create 落库。
- **已有会话**：解析 `effectiveProviderKey = firstNonEmpty(sess.ProviderKey, be.LLMProviderKey)`（「无任何切换器」已被 [2026-08-10 规格](./2026-08-10-session-provider-switch.md) 取代：composer 常显选择器，切换自下一轮生效）。
- **本地**：在 `resolveAgentBackend` 同一解析点按 effectiveProviderKey 解析 `prov`；builtin 仍要求有可用供应商（否则不可对话，走既有 `NewSessionChatGuard`）。
- **远端**：wire 透传 effectiveProviderKey；daemon 从自家配置按 key 解析；daemon 缺该 key / 非 active → 回退 agent 绑定并回传信号，桌面端据此追加 notice（与本地 Q3 一致）。
- **回退（Q3）**：会话 `provider_key` 指向的供应商缺失或非 active → 本轮改用 agent 绑定解析，transcript 追加一条持久 notice：「所选供应商 X 不可用，已回退 agent 绑定」。`provider_key` 不清除（供应商恢复后自动回到会话所选）。

## UI（新建会话供应商选择器）

- 摆放：composer 动作行原 ModelPill 位置；后端为 builtin / claudecode / codex / piagent 时渲染；openclaw 不渲染（不消费 agentre provider）。（原文的「**仅 `sessionId===0`（新建会话）**」已被 [2026-08-10 规格](./2026-08-10-session-provider-switch.md) 决策 10 取代：已有会话同样渲染，不可切换时 disabled + tooltip。）
- 形态：沿用 pill + `Popover` 模式（`h-6 rounded-md border px-2 text-2xs font-medium` + caret，图标用供应商/立方体图标，供应商名等宽字体截断）。
- 弹层：头部"供应商"标题；列表第一项"跟随 agent 绑定"（未选时该项高亮；选中具体供应商后点击可清回；未绑 agent 时该项语义为"不选，走 CLI 登录态"）；其下兼容供应商列表（名称 + 类型 + 默认模型 `provider.Model`），当前选中项高亮（`primary-soft` + 实心点徽标）。
- 未选时 pill 标签：agent 已绑 → 显示 agent 绑定供应商名；未绑 → "选择供应商"占位文案。
- 状态：供应商列表加载中 → pill 禁用；拉取失败 → 弹层底部错误行（沿用既有 `errorMessage` 样式）。
- 可访问性：pill 有 `aria-label` / `title`（含当前供应商名），列表项 `aria-selected`，键盘可触发。
- 新建会话的瞬态选择随首条 `Send` 透传（`SendRequest.ProviderKey`）；发消息前可自由改。

## 移除清单（#26 全部）

- **后端**：`model_override` 列 / entity 字段 / repo（`UpdateModelOverride` + Omit 清单）/ mock；`SetChatSessionModel`（app 绑定 + svc）；`SendRequest.ModelOverride` + `normalizeModelOverride`；`modelOverrideSwitchable` / `modelOverrideForBackend`；模型偏离提示（`modelDeviationNotice`、encode/decode、`ChatBlock.selectedModel/actualModel`）；`agentruntime.RunRequest.ModelOverride` + 四 runtime 消费（builtin / codex / piagent / claudecode 的 launchedModel evict-respawn）+ remote wire `RunParams.ModelOverride` + daemon handler；`ChatSessionDetail.ModelOverride` / `ProviderDefaultModel`。
- **前端**：ModelPill 的模型列表 / 自由输入分支；`useModelPill`；chat-panel 的已有会话 pill 渲染与模型瞬态传递；`SetChatSessionModel` 绑定；`modelPill.*` i18n key。
- **测试**：以上对应的单元 / 组件测试删除或重写。
- **文档**：`docs/specs/2026-08-06-session-model-switch.md` 标记 Superseded；`docs/agent-backend.md` 中 `model_override` 相关段落更新。

## Out of scope

- ~~**已有会话改供应商/模型**：B 明确禁止；会话建好后不可换（换需新建会话）。~~ 改**供应商**已由 [2026-08-10 规格](./2026-08-10-session-provider-switch.md) 实现；改**模型**仍不在范围内（会话固定用所选供应商的默认模型）。
- **回退时自动清除 `provider_key`**：保留，供应商恢复后自动回到会话所选。
- **openclaw 供应商选择**：openclaw 不消费 agentre provider。
- **模型列表 / 自由输入模型 id UI**：新建会话不再有模型概念。

## Testing decisions

| Seam | 验证内容 | Prior art |
|---|---|---|
| 实体 / 迁移 | `Session.ProviderKey` 默认 `''`、空值可过 `Check`；其余既有迁移未被改动（除基线豁免外）。基线 schema（含 `provider_key`、不含 `model_override`）由真实 app 启动 + 运行时 e2e 验证（PRAGMA），不设迁移 SQL 单测 | `session_test.go` |
| `chat_svc` 单元（mockgen repo mock，不连 DB） | 新建 Send 带 `ProviderKey` 落库并校验（不存在 / inactive / 不兼容 → 错误）；已有会话 `provider_key` 优先于 agent 绑定；无 `provider_key` → agent 绑定；供应商缺失回退 agent 绑定 + notice；不再产生模型偏离提示 | `chat_test.go` 现有 runTurn / 权限模式测试 |
| 远端 wire | 透传 effectiveProviderKey；daemon 缺 key → 回退 + 回传信号 | `wire_test.go` + `daemon` handler 测试 |
| 前端（vitest） | 新建会话供应商选择器只列兼容供应商；未绑也显示；openclaw 不渲染；已有会话无 pill；瞬态选择随 Send 透传；i18n key 覆盖 | `model-pill.test.tsx` 改造 |
| 无法自动化的 | CLI 后端绑定供应商后的真实模型解析（回归靠现有 eval 套件） | `pkg/codex/behavior_eval_test.go` |

## Open questions

<!-- 批准前必须为空 -->
