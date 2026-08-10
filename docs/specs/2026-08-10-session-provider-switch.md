# 已有会话切换 LLM 供应商（并让会话级供应商真正决定上游）

<!-- File: docs/specs/2026-08-10-session-provider-switch.md -->

> Status: Draft
> Owner: chat experience / backend
> Last updated: 2026-08-10

**Objective:** 用户能在**已有会话**的 composer 里换 LLM 供应商，自下一轮生效、正在跑的轮不受影响；同时把会话级 `provider_key` 贯穿到所有后端的真实执行路径——今天它在 claudecode / codex（本地与远端）上只改了 `--model`，并不决定请求实际打到哪个供应商。

**Hard invariants:**

1. **`provider_key` 为空的会话行为完全不变**：仍按 `agent_backend.LLMProviderKey → llm_provider` 解析；CLI 后端未绑供应商时走 CLI 自身登录态。
2. **切换只换供应商**：agent / backend / cli_path / reasoning_effort / permission mode / cwd / 执行目标钉住关系一律不变；选择不写回 agent 绑定，不污染其它会话。
3. **不打断正在进行的轮**：切换在轮中允许，但当前轮已 spawn 的子进程不被 evict、不被重启、不改写；新供应商自下一轮生效。
4. **不可切换时禁用而非隐藏**（用户决策）：选择器在所有会话状态下都渲染，不满足切换条件时 `disabled` 并通过 tooltip 说明原因。
5. **新 UI 文案全部走 i18n**：新增 key 同时覆盖 `zh-CN/common.json` 与 `en/common.json`，`i18n.test.ts` 通过；控件复用既有 shadcn `Popover` pill，不引入原生 `<select>`。
6. **远端 agentred 执行同样生效**：切换后的供应商随 wire 透传，daemon 按 key 自解，并同样贯穿到 daemon 侧网关路由。
7. **供应商密钥不跨机器**：`APIKey` / `BaseURL` 始终由执行侧（desktop 本机或 daemon 本机）从自己的配置解析，wire 只过 key。

## Problem

1. **已有会话无法换供应商。** 供应商选择器只在 `sessionId === 0` 渲染（`frontend/src/components/agentre/chat-panel.tsx:2848`），首条消息落库后即消失；后端也没有 Setter——`docs/specs/2026-08-09-new-session-provider-select.md` 决策 2 明确「不可事后修改」。用户报告：「在对话输出时，选择供应商怎么没了」，并指出被移除的 #26 曾承诺「除非是用登录态，否则可以在对话中切换」。
2. **会话级供应商在 claudecode / codex 上不决定真实上游。** 网关 token 的路由信息来自 agent 绑定：`signChatTokenFor` 调 `IssueToken(ctx, be, 0)`（`internal/service/chat_svc/chat.go:4575`），entry 里 `MainProviderKey = b.LLMProviderKey`（`internal/pkg/httpgateway/token_registry.go:97`）；转发时按该 entry 取 `BaseURL` / `APIKey`，并把请求体的 model **重写成该 provider 的 model**（`internal/pkg/httpgateway/llmforward.go:75-97`）。会话选的供应商只进到 `req.Provider` → CLI 的 `--model`（`internal/pkg/agentruntime/runtimes/claudecode/session.go:236`），随后被网关改回去。远端 daemon 同构：`ensureSessionToken(ctx, em.rid, &be)`（`internal/daemon/handlers/runtime.go:384`、`1443`）。结果是 #33 已发布的「新建会话选供应商」在这两个后端上就是无效的。
3. **CLI 登录态后端上会话供应商完全无法接管。** `BuildClaudeCodeEnv` 只在 `b.LLMProviderKey != ""` 时才设 `ANTHROPIC_BASE_URL` / `ANTHROPIC_AUTH_TOKEN`（`internal/pkg/agentruntime/clienv.go:48-53`），codex 更早一步——`shouldSignChatGateway` 对未绑 provider 的 codex 直接返回 false（`internal/service/chat_svc/chat.go:4505-4513`），连 token 都不签。两处门控读的都是 agent 绑定，所以 `2026-08-09-new-session-provider-select.md` 决策 5 承诺的「未绑 agent 的新建会话也能选一个 agentre 供应商接管该会话」在这两个后端上从未兑现。
4. **会话选了不同供应商后，头部展示与用量语义按 agent 绑定算。** `LoadSession` 用 `be.LLMProviderKey` 解析 prov（`internal/service/chat_svc/chat.go:573-575`），据此填 `LLMProviderType`（前端据它决定 Anthropic 系 / OpenAI 系的 prompt token 叠加口径）与 `ContextWindow`（`chat.go:583-586`）。会话供应商与 agent 绑定不同类型 / 不同上下文窗口时，用量条和上下文占比显示错误。
5. **「复制启动命令」按 agent 绑定拼。** 该入口独立解析 `be.LLMProviderKey → prov` 并据此签 token（`internal/service/chat_svc/chat.go:674-693`），复制出来的命令用的是 agent 绑定的供应商，与会话实际执行的不一致。

问题 2–5 是同一个根因的四个面：**会话级供应商没有一个统一的解析口径，各消费点各自读 `be.LLMProviderKey`**。

## Actors and user stories

1. 作为对话中的用户，我希望换一个 LLM 供应商继续这段对话，以便同一段上下文换个模型/厂商接着跑，而不必新建会话重述背景。
2. 作为 CLI 登录态（agent 未绑供应商）会话的用户，我希望能中途切到一个 agentre 供应商接管，也能再切回登录态。
3. 作为选了会话级供应商的用户，我希望请求真的打到我选的那个供应商，且头部上下文用量按它计算。
4. 作为远端 `agentred` 会话的用户，我希望切换在远端同样生效。
5. 作为切换过供应商的用户，我希望回看 transcript 时能看出从哪一条开始换的供应商。

## Design decisions

| # | Decision | Basis and rejected option |
|---|---|---|
| 1 | **`chat_sessions.provider_key` 从「落库后不可改」放开为可改**，新增单列更新 `UpdateProviderKey` + `chat_svc.SetChatSessionProvider` + Wails 绑定 | 用户要求对话中可切；单列更新与既有 `UpdatePermissionMode` / `UpdateExecDaemon` 同形（`internal/repository/chat_repo/session.go:409-427`）。拒绝走全量 `Update`——会把并发轮写入的状态字段一起盖掉 |
| 2 | **引入唯一的有效供应商解析口 `effectiveProviderKey = firstNonEmpty(sess.ProviderKey, be.LLMProviderKey)`，所有消费点改读它**：turn 解析、网关 token 路由、CLI env / config 门控、LoadSession 展示、复制启动命令、远端 wire | 问题 2–5 是同一根因的四个面；`chat.go:3722` 已在 remote 路径用了这个表达式，把它提成共用解析口而不是再抄三份。拒绝在各消费点分别加 `if sess.ProviderKey != ""` 补丁——正是当前缺陷的成因 |
| 3 | **网关 token 的身份不变、路由可变**：`TokenEntry` 的供应商来源改为签发时传入的 effective key，并新增「按 token 更新其 provider key」的能力；切换时更新既有 entry，不重签 token | token 是**会话级常驻**且首轮就烤进子进程 env，跨轮复用（`chat.go:4557-4562`）；重签会让在跑的子进程手里的 token 失效（正是被修过的 401 事故）。拒绝每轮重签——同一事故的回归 |
| 4 | **供应商变化触发子进程 evict + 重 spawn，比对键从 model 扩展为 (effectiveProviderKey, model)** | claudecode 已有 `launchedModel != claudeEffectiveModel(req)` 的 evict 先例（`internal/pkg/agentruntime/runtimes/claudecode/runtime.go:564`），codex 有等价的 `modelChanged`（`runtimes/codex/runtime.go:113-118`）；但两个不同供应商可以配同一个 model id，只比 model 会漏掉换供应商。拒绝无条件 respawn——会让不换供应商的普通续轮也白重启 |
| 5 | **piagent 不需要新增 evict 逻辑** | piagent 不使用 `CLISessionPool`，每轮按 `req.Provider` 重新装配 provider 扩展与 `--model agentre-<key>/<model>`（`runtimes/piagent/session.go:335-337`），换供应商下一轮自然生效 |
| 6 | **CLI env / config 的网关门控改按 effective provider 判定**：claudecode 的 `ANTHROPIC_BASE_URL` / `ANTHROPIC_AUTH_TOKEN` 与 codex 的 `shouldSignChatGateway` + `BuildCodexConfig` 都以「本轮是否有 effective provider」为准 | 兑现 #33 决策 5 的既有承诺（问题 3），也是登录态双向切换的前提。拒绝只在 backend 已绑时才允许会话切换——会把用户决策「登录态双向可切」砍掉 |
| 7 | **登录态会话双向可切**（用户决策）：未绑 agent 的会话可选一个 agentre 供应商接管，也可选回「跟随 agent 绑定」回到 CLI 登录态 | 用户决策；修好决策 6 后技术上对称，上下文靠 `--resume` 不丢。拒绝 #26 的「登录态已有会话灰显」——那是在没有会话级供应商概念时的限制，现已不成立 |
| 8 | **自下一轮生效，轮中允许切**（用户决策） | 沿用 #26 已定语义；当前轮的子进程已用旧供应商 spawn，改写它等于打断用户正在看的输出。拒绝「立即生效」——需要中断并重启在跑的轮，代价与风险都不成立；拒绝「轮中禁用」——长轮期间无法预设下一轮 |
| 9 | **切换成功后向 transcript 追加一条持久 notice**（用户决策），复用既有 `NoticeBlock` 与 providerFallback 同一套渲染（`chat.go:718-762`） | 用户决策；两个供应商配同一 model 时，逐条消息的 `model` 字段看不出分界。拒绝不留痕——回看时无法定位切换点 |
| 10 | **选择器在已有会话同样渲染，不满足条件时 `disabled` + tooltip 说明**（用户决策），取代 #33 决策 7 的「已有会话不渲染任何切换器」 | 用户决策：隐藏让用户以为功能消失（本轮起因就是这个报告）。禁用态覆盖：openclaw 后端、无兼容供应商、供应商列表加载中、列表拉取失败 |
| 11 | **切换时复用新建会话那套校验**（存在 / `IsActive` / `ProviderTypeMatch`），不通过则拒绝写库并原样报错，会话保持原供应商 | 与 `validateNewSessionProvider`（`chat.go:1723-1743`）同一口径，避免出现「写进去了但下一轮必然失败」的会话。拒绝先落库后校验 |
| 12 | **远端切换沿用「wire 只过 key、daemon 自解」**：每轮 run 已透传 effectiveProviderKey（`chat.go:3722`），daemon 侧把同样的 effective key 用到它自己的网关 token 路由上 | 硬不变量 7；daemon 侧已有按 effective key 解析 provider 的分支（`internal/daemon/handlers/runtime.go:349-376`），缺的只是网关 token 那一段。拒绝把 desktop 的 provider 实体下发给 daemon——密钥越线 |
| 13 | **`docs/specs/2026-08-09-new-session-provider-select.md` 的决策 7 与「不可事后改」标注为被本规格取代**，不删除原文件 | 该规格其余部分（新建会话选择器、回退 notice、迁移）仍然有效，只有这两条被推翻。拒绝整篇 Superseded——会连带作废仍生效的决策 |

## 有效供应商解析（唯一口径）

会话任意一轮的有效供应商 = `sess.ProviderKey` 非空时取它，否则取 `be.LLMProviderKey`；两者都空表示「无供应商」，即 CLI 自身登录态（builtin 后端此时不可对话，走既有 `NewSessionChatGuard`）。

这个口径是**唯一**的：turn 解析、网关 token 签发与路由、CLI 子进程 env / config 的网关门控、`LoadSession` 的展示字段、复制启动命令、远端 wire 透传，全部读同一个解析结果。任何消费点不得再直接读 `be.LLMProviderKey` 来决定「这一轮用谁」。

会话所选供应商缺失 / 停用 / 与后端 kind 不兼容时，该轮回退 agent 绑定并追加既有的 providerFallback notice，`provider_key` 不清除（既有行为，不变）。

## 切换流程与生效时机

用户在已有会话的 composer 里打开供应商选择器并选中一项：前端调用后端切换接口，后端按决策 11 校验后写入 `chat_sessions.provider_key` 并返回成功；前端更新 pill 显示。选「跟随 agent 绑定」写入空串。

切换**不触碰正在进行的轮**：不 evict 子进程、不改写已下发的 env、不重签 token、不中断流式输出。下一轮起 turn 解析读到新的 effective key，据此解析 provider、更新该会话网关 token 的路由、并在供应商变化时 evict 重 spawn 子进程。

弹层底部常显一行说明「切换自下一轮生效，正在进行的回合不受影响」。

切换成功后向 transcript 追加一条持久 notice，说明从此处起改用哪个供应商（选回「跟随 agent 绑定」时说明改为跟随绑定 / CLI 登录态）。notice 与既有 providerFallback notice 同为结构化负载 + 前端 `t()` 渲染，不把原始 JSON 泄漏给前端。

## 网关路由与子进程重启

网关 token 在整段会话内保持同一个字符串（它已烤进子进程 env）；切换供应商改变的是该 token 在网关内的**路由目标**。签发时按 effective key 填入，切换后的下一轮把既有 token 的 provider key 更新为新的 effective key。token 找不到对应 entry / entry 的 provider 缺失或停用时，转发端点维持既有的 401 / 502 行为。

CLI 子进程在本轮 effective provider 与 spawn 时不一致时 evict 并重 spawn（claudecode / codex）：`--model`、`ANTHROPIC_BASE_URL`、codex 的 `model_provider` config 都是启动期参数，运行时改不掉。重 spawn 通过 `--resume` / `thread/resume` 续上原 CLI 会话，对话上下文不丢。登录态 ↔ 供应商两个方向都要重 spawn，因为网关相关 env 的有无本身就是启动期差异。

## 登录态与后端可用性

agent 未绑供应商的会话（CLI 登录态）可以选一个兼容的 agentre 供应商接管：该轮起签发网关 token、装配网关 env / config，请求经本机网关转发到所选供应商。选回「跟随 agent 绑定」则下一轮不装配网关 env，回到 CLI 自身登录态。

openclaw 后端不消费 agentre provider：选择器渲染为 disabled，tooltip 说明该后端不使用 agentre 供应商。

## 展示口径

`LoadSession` 返回的会话级供应商类型与上下文窗口按 effective provider 解析；`ChatSessionDetail` 增加会话当前 `providerKey` 与 agent 绑定 key 两个字段，供前端渲染 pill 标签（已选具体供应商 → 供应商名；未选 → agent 绑定供应商名；两者皆无 → 「选择供应商」占位）。运行时上报的 `session.ContextWindow` 优先级不变（既有 `resolveContextWindowWithRuntime` 顺序）。

「复制启动命令」按 effective provider 解析供应商与签发 token，使复制出的命令与该会话实际执行一致。

## UI 与禁用状态

已有会话复用新建会话那颗 pill（同一组件、同一弹层、同一位置），差异只在数据来源与禁用条件。

| 会话/后端状态 | pill | 说明 |
|---|---|---|
| builtin / claudecode / codex / piagent，有兼容供应商 | 可用 | 已选高亮 + 可清回「跟随 agent 绑定」 |
| 轮进行中 | 可用 | 弹层底部常显「自下一轮生效」 |
| openclaw 后端 | disabled | tooltip：该后端不使用 agentre 供应商 |
| 无兼容供应商 | disabled | tooltip：没有与该后端类型匹配的可用供应商 |
| 供应商列表加载中 | disabled | 既有行为 |
| 列表拉取失败 | 可用 | 弹层底部错误行（既有行为） |

pill 与列表项的 `aria-label` / `title` / `aria-selected` 与键盘可达性沿用既有实现；禁用态的原因通过 tooltip 暴露，不只靠视觉灰显。

## Out of scope

- **回合内立即切换 / 中断当前轮换供应商**（决策 8 已拒绝）。
- **会话级模型选择**：本轮仍是「用所选供应商的默认模型」，不恢复 #26 的模型列表或自由输入（`2026-08-09` 规格决策 6 不变）。
- **openclaw 供应商选择**：该后端不消费 agentre provider。
- **切换写回 agent 绑定**：硬不变量 2。
- **回退时自动清除 `provider_key`**：既有行为保留。
- **新增迁移**：`provider_key` 列已存在（基线 `migrations/202608080006_chat.go`），本轮不动 schema。

## Testing decisions

| Seam | 验证内容 | Prior art |
|---|---|---|
| `chat_svc` 单元（mockgen repo mock，不连 DB） | 切换校验（不存在 / inactive / 不兼容 → 报错且不写库）；切换成功写 `provider_key` 并产出 notice；切回空串；effective 解析（会话 > agent 绑定 > 空）；轮中切换不影响本轮已解析的 provider | `chat_test.go` 的 runTurn / 权限模式 / `chat_internal_test.go` 的 `TestPrepareTurnRun_Remote*` |
| `chat_repo` 单元（sqlmock） | `UpdateProviderKey` 只更新该列 | `session_test.go` 既有单列更新用例 |
| `httpgateway` 单元 | 按 effective key 签发的 entry 路由到该供应商；更新既有 token 的 provider key 后转发目标随之改变且 token 字符串不变；provider 缺失 / 停用维持 502 | `token_registry_test.go` / `llmforward_test.go` |
| `agentruntime` 单元 | claudecode / codex：effective provider 变化 → evict 重 spawn；未变 → 复用；claudecode env 与 codex config 在「backend 未绑但会话选了供应商」时装配网关参数，在「两者都无」时不装配 | `clienv_test.go`、`runtimes/claudecode/runtime_test.go`、`runtimes/codex` 既有 modelChanged 用例 |
| daemon handler 单元 | 远端按 effective key 解析并用于自家网关 token 路由；会话 key 缺失时回退 agent 绑定并回传 fallback 信号（既有语义不回归） | `internal/daemon/handlers` 既有 runtime 用例 |
| 前端（vitest） | 已有会话渲染 pill 并显示当前会话供应商；禁用状态表逐条（openclaw / 无兼容供应商 / 加载中）；选中调用切换接口并更新显示；i18n key 覆盖 | `model-pill.test.tsx`、`chat-panel.test.tsx` |

**无法自动化的部分**：真实 claude / codex CLI 在换供应商后重 spawn 并 `--resume` 续上原会话、请求真的打到新供应商的上游——这依赖真 CLI 与真供应商凭证。由收尾阶段的真机运行观测覆盖：开启 Debug Logging 后确认重 spawn 发生、网关转发目标为新供应商、对话上下文未丢，按 `docs/verification.md` 在 `e2e/scratch/2026-08-10-session-provider-switch/` 留证据。

## Links

- 前序规格：[2026-08-09 新建会话选择 LLM 供应商](./2026-08-09-new-session-provider-select.md)（其决策 7「已有会话不渲染任何切换器」与决策 2「不可事后修改」被本规格取代）
- 历史规格：[2026-08-06 会话内切换 LLM 模型](./2026-08-06-session-model-switch.md)（已 Superseded，本规格不恢复其模型选择能力）

## Open questions

无。
