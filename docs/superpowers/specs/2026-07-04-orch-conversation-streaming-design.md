# 编排对话流式化 + 完整输入框 — 设计

日期:2026-07-04 · 分支:develop/wyz · 范围:agentre 桌面端前端

## 背景与问题

编排页右栏的 `ConversationPanel` 和普通对话页,**消息渲染层是同一个组件**(都用 `chat.tsx` 的 `ChatTranscript`),但外围是两套独立实现,尤其数据流完全不同:

- 普通对话页:`ChatPanel` 把 `chat-streams-store` 的实时流(`liveDelta/liveBlocks/liveThinking`)作为 live* props 喂给 `ChatTranscript`;全局 `ChatStreamsHost` 挂 `EventsOn(chat:stream:*)` 持续收流;发送走 `SendChatMessage` + 乐观插入 + `openStream`。
- 编排 `ConversationPanel`:只从 `orch-subagents-store.messagesBySession` 读**持久快照**(`LoadChatSession`),显式省略所有 live* props(`conversation-panel.tsx:208` 注释「read-only transcript」);发送走 `RunSpeak` + 整体 `reload`。输入框是裸 `Textarea`,无贴图/无权限模式。

结果:编排对话**没有逐字流式**(agent 干活时看不到增量,消息在 reload 后整条突然出现),输入能力也是精简版。用户希望编排对话「像普通对话一样」:**流式 + 完整输入框**。

## 可行性结论(已在代码中核实)

整个特性**不需要动后端**,纯前端接线:

1. **流是按 session 维度发的,编排轮次全都发了起轮广播。** 编排所有起轮路径 —— `create.go:94`(Leader kickoff)、`scheduler.go:84`(**dispatch 派活 = agent 真正干活那轮**)、`complete.go:91`(子任务回报父)、`ask.go:85`(peer ask)、`control.go:84`(用户 Speak)—— 都经 `orch_adapter.go:58 SendAndForget → chat_svc.Send{EmitTurnStartedBypass:true}`。`chat_svc/types.go:564` 注释明说该 flag「表示本轮由非查看者发起(编排子轮经调度)」。→ 后端已在 `AutonomousStreamName(sessionId)` 广播每一种编排轮次的「起轮」,前端订阅即可捕获 agent 干活的实时流。
2. **`ChatStreamsHost` 是全局、与页面无关的订阅器。** 任何 session 只要被 `openStream` 进 `chat-streams-store`,host 就自动挂 `EventsOn` 收流写 store。编排面板复用它,无需新增订阅宿主。
3. **`LoadChatSession` == `chat_svc.Chat().LoadSession`(同一方法)。** 返回的 `LoadSessionResponse.Session`(`ChatSessionDetail`)**已带 `activeStream`**(中途进来重挂用)+ `backendType` / `supportedModes` / `contextWindow`(ChatComposer 需要的元数据全在)。`orch-subagents-store` 现在只是把 `resp.session` 丢弃了没读。
4. **`ChatComposer` 是受控组件、已可移植。** `onSubmit` 回调返回 `{text, images}`,权限模式经 `permissionModeSlot` 注入,backendType/图片支持是普通 props;已在 `queued-messages-bar.tsx` 等 `chat-panel` 之外复用。
5. **`ChatTranscript` 是纯渲染组件**,不内部订阅任何 store;live 数据必须由外部作为 props 喂入。

## 架构决策

- **复用** `ChatTranscript`(显示)+ `ChatComposer`(输入)。
- **新增隔离 hook**(不碰 `ChatPanel`,聊天页零回归风险),按单一职责拆分(ISP):
  - `useLiveTranscript(sessionId)` — viewer:重挂 activeStream + 捕获自发轮 + 暴露 live overlay + finish→reload。
  - `useConversationSender(sessionId)` — sender:`submit({text,images})` + 会话元(backendType / 权限模式 / 上下文用量)。
  - `useLiveConversation(sessionId)` = 组合两者,供 `ConversationPanel` 用。
  - **Leader footer 只依赖 sender**(它不显示 transcript,避免重复订阅同一条流)。
- `ChatPanel` 迁移到同一套 hook(统一引擎)**留作可选 follow-up,本次不做**。

## 数据流

| 关注点 | 来源 / 机制 |
|---|---|
| 持久消息 + 会话元(activeStream / backendType / supportedModes / contextWindow) | `orch-subagents-store`,**扩一个 `detailBySession: Map<sessionId, ChatSessionDetail>`** 存 `resp.session`(现被丢弃);一次 `LoadChatSession` 全拿 |
| live overlay(liveDelta / liveBlocks / liveThinking / liveRetry / liveCompacting) | `chat-streams-store`,由全局 `ChatStreamsHost` 自动写入 —— 不改 |
| 发送 | `SendChatMessage({sessionId, text, images, permissionMode})` → `openStream(resp.stream)`;会话 busy(`ChatSendInFlight`)→ steer/enqueue(与聊天页一致) |
| 中途重挂 | 打开面板时读 `session.activeStream`,非空 → `openStream({sessionId, name, assistantMessageId, streamStartedAt})` |
| 捕获 agent 自发轮 | `EventsOn(AutonomousStreamName(sessionId))` → 起轮广播帧 → `openStream`(照搬 `chat-panel.tsx:496-546` 的 StreamAutonomousStarted pattern) |
| 收尾对账 | `finishStream`(done/error)触发 → `orch-subagents-store.reload(sessionId)`,持久消息替换 live overlay |

**背景会话不累积 live overlay**:未打开的会话不订阅其流;打开时用 `activeStream` 重挂到进行中的轮,已结束的看持久消息即可。与聊天页语义一致,不过度工程。

## Hook 契约

`useLiveTranscript(sessionId)` 返回:
```
{ messages, live: { liveDelta, liveThinking, liveBlocks, liveRetry, streaming, liveCompacting, liveTargetId } }
```
职责:①mount 时 `ensureLoaded` + 读 detail;`activeStream` 非空则 `openStream` 重挂。②`EventsOn(AutonomousStreamName(sessionId))` → 起轮帧 → `openStream`;unmount `EventsOff`。③selector 读本 session 的 `LiveStream`。④监听 finish → `reload`。

`useConversationSender(sessionId)` 返回:
```
{ submit({text, images}), sending, session: { backendType, supportsImageInput, contextUsage, supportedModes, permissionMode }, setPermissionMode }
```
职责:`submit` → `SendChatMessage` → `openStream(resp.stream)`;busy → enqueue steer;暴露 composer 所需会话元。

依赖:`chat-streams-store`(openStream + selector)、`orch-subagents-store`(messages/detail/reload)、`SendChatMessage` RPC、`EventsOn/EventsOff`、`StreamName/AutonomousStreamName` helper。

## 组件改动

**`ConversationPanel`**(壳保持:who-row / 状态点 / awaiting callout / 返回按钮)
- body 的 `ChatTranscript`:从「只传 messages」→ 追加 `useLiveConversation` 的 live* props(逐字流式)。
- footer:裸 `Textarea`+按钮 → `ChatComposer`(贴图 + `permissionModeSlot`,claudecode 时注入),接 `hook.submit`。
- 删掉本地 `speak()`/`RunSpeak` 路径,统一走 `hook.submit`(→ `SendChatMessage`,与 `RunSpeak` 都命中 `chat_svc.Send`,行为等价且直接拿到 stream)。

**Leader footer(`orchestration/index.tsx`)**
- 裸 `<input>` → `ChatComposer`,用 `useConversationSender(leaderSessionId)`。
- 提交后 `setSelectedSessionId(leaderSessionId)` → 右栏切到 Leader 的 `ConversationPanel` 看流式。
- 保留死锁「介入」聚焦行为;注意 ChatComposer 比单行高,footer 高度会涨,用紧凑排布。

## 明确不做 / 待定

- **edit/rerun**:v1 **不开**。在编排 runtime 正驱动的会话里编辑/重跑会 fork 一个编排引擎「拥有」的轮次,可能与调度打架。transcript 维持不传 `onEdit/onRerun`。留 follow-up(要开建议加「会话 busy / run 活跃时禁用」守卫)。
- **ChatPanel 迁到共享 hook**(统一引擎)—— 本次不做。
- **远端/daemon 会话**的 `AutonomousStreamName` 是否跨 WS 桥转发 —— **需验证点**,若不转发则远端编排会话仍无流式(桌面本地不受影响)。

## 测试(TDD:先红后绿)

- **hook 单测(vitest)**:①重挂(`LoadChatSession` 返回 activeStream → 调 `openStream`);②自发轮(模拟 `AutonomousStreamName` 事件 → `openStream`);③live overlay 透出(store 有 LiveStream → hook 返回 live* props);④submit(调 `SendChatMessage` 带 `{sessionId,text,images}` + `openStream(resp.stream)`;busy → enqueue);⑤finish → `reload`。
- **`ConversationPanel`**:transcript 带 live props 渲染;composer 提交路径;awaiting callout 不回归。
- **Leader footer**:ChatComposer 提交 → `SendChatMessage` + `setSelectedSessionId(leaderSessionId)`。
- 遵守前端 wails runtime mock 规则(引 wailsjs runtime 的组件按文件 `vi.mock(importActual+override)`,不加全局 alias)。
- 新文案进 `zh-CN` + `en` 双 locale,尽量复用现有 key;收尾跑全量 `pnpm test` + `tsc` + `eslint` 看真 exit code。

## 分层与依赖(合规检查)

- 纯前端;不新增 Wails 绑定,不加 HTTP-style app API。
- 复用既有 store(`chat-streams-store` / `orch-subagents-store`)与既有 RPC(`SendChatMessage` / `LoadChatSession`);`orch-subagents-store` 仅新增 `detailBySession` 字段。
- 新 hook 依赖 store + RPC + Wails events,不反向依赖组件;`ConversationPanel` / Leader footer 通过 hook 消费,符合高内聚低耦合。
