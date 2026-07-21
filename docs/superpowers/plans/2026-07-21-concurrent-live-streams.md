# 并发 LiveStream:一个会话可同时持有多条流

> 根因 B 的修复计划。根因 A(startup 看门狗错杀健康子进程)已单独修完,见
> `internal/pkg/agentruntime/runtimes/claudecode/{active,autoturn,runtime}.go` +
> `TestRun_UserTurnQueuedBehindAutonomousTurnIsNotStartupKilled`。

## 问题

`frontend/src/stores/chat-streams-store.ts` 的 `streams: Map<number, LiveStream>`
**按 sessionId 单槽位**,`openStream` 无条件覆盖整条 entry。但一个会话在同一时刻
合法地可以有多条流:

| 流 | 入口 | 绑定的 assistant 消息 |
| --- | --- | --- |
| 用户轮 | `doSend` → `SendChatMessage.stream` | 本轮新建的 assistant 行 |
| 自主续轮 | `chat:autonomous:<sid>` 的 `autonomous_started` | 后端新建的 assistant 行 |
| 后台 subagent 活动轮 | 同上的 `subagent_activity_started` | **已存在**的发起消息 |

后两者由后台任务/后台 subagent 驱动,与用户操作无关,随时可能与用户轮重叠。

覆盖的两个后果:
1. 被覆盖那条流已经流到屏幕、尚未落库的 `liveDelta` / `liveBlocks` / `liveThinking`
   全部丢失 → transcript 回退到持久化态(稀疏 checkpoint)= 用户看到的「已输出内容
   清空回退」。
2. `ChatStreamsHost` 的 `<StreamSubscriber key={sessionId}>` 随之换 `streamName`,
   旧流被 `EventsOff` → 它后续的 chunk/tool/done 事件**无人接收**,该轮再也不会
   收敛(要靠下一次 reload 兜)。

复现:`chat-panel.test.tsx` 的 `T31 自主续轮流式中再发消息`(当前 RED)。

## 目标

一个会话可并存多条 LiveStream,按 `assistantMessageId` 区分;每条独立累积内容、
独立订阅、独立收尾。transcript 按消息各取各的 live 内容渲染。

## 数据结构

```ts
// State
streams: Map<number, Map<number, LiveStream>>;   // sessionId → (assistantMessageId → LiveStream)
```

嵌套而非 `Map<string, LiveStream>` 复合键:会话级 selector(`s.streams.get(sessionId)`)
保持 O(1) 且引用稳定 —— 别的会话在流不会让本 panel 重渲染(沿用
`b760d75` useShallow 收窄过度订阅的思路)。

所有 action 从 `(sessionId, ...)` 改为 `(sessionId, assistantMessageId, ...)`。

导出选择器辅助(供组件用,避免各处手写两层取值):
- `sessionStreamMap(state, sessionId): Map<number, LiveStream> | null`
- `primaryStream(state, sessionId): LiveStream | null` —— 最近一次 `openStream` 的那条,
  供 composer / toolbar 的会话级读数(`liveUsage` / `liveContextWindow` / `liveCompacting`
  / `streaming` / `canStop`)。用 `streamStartedAt` 最大者。
- `hasSessionStream(state, sessionId): boolean`

## transcript 契约

`buildTranscriptRows` 现在只认单个 `liveTargetId` + 一组 `liveTail/liveThinking/liveBlocks`。
改为按消息查表:

```ts
export type LiveContent = {
  liveTail?: string;
  liveThinking?: string;
  liveThinkingStartedAt?: number | null;
  liveBlocks?: ChatBlockData[];
};
// BuildTranscriptRowsArgs
liveByMessageId?: ReadonlyMap<number, LiveContent>;   // 取代 liveTargetId + 三个单值
```

`isLive` 判定从 `m.id === liveTargetId` 改为 `liveByMessageId.has(m.id)`。
`ChatTranscript`(chat.tsx)同步换 props。

> 注意 `reference_chattranscript_livetargetid_contract` 记的坑:复用 ChatTranscript
> 必须传对 live 目标,否则整块不流式。改成 map 之后这个坑变成「必须传全 map」,
> 在 chat.tsx 的 props 注释里写清楚。

## 任务拆分

- [ ] T1 store:`streams` 改嵌套 Map,全部 action 加 `assistantMessageId` 形参;
      新增三个选择器辅助。`openStream` 只 set 自己那条,不动同会话其它流。
      `closeStream` / `finishStream` 只删自己那条。
      更新 `stores/__tests__/chat-streams-store.test.ts`。
- [ ] T2 `chat-streams-host.tsx`:`streamKeys` 编码进 `assistantMessageId`,
      `<StreamSubscriber key={`${sessionId}:${assistantMessageId}`}>`,
      `handleEvent(sessionId, assistantMessageId, ev)`。更新两个 host 测试。
- [ ] T3 transcript:`transcript-rows.ts` 换 `liveByMessageId`,`chat.tsx` 换 props
      并把 `isLiveTail` 判定改成查表。更新 transcript-rows / chat 测试。
- [ ] T4 `chat-panel.tsx`:`currentStream` → `primaryStream`;新增 `liveByMessageId`
      useMemo 喂给 ChatTranscript;`streaming` / `canStop` 改用 `hasSessionStream`。
      T31 转 GREEN。
- [ ] T5 零散消费方:`hooks/use-chat-session.ts`(3 处)、`stores/chat-agents-store.ts`、
      `canonical-tool/plan/card.tsx`。
- [ ] T6 全量门禁:`pnpm test` + `tsc` + `eslint`(看真 exit code),`make test-backend`。
- [ ] T7 e2e:另一路 agent 在加 fake runtime 自主轮接缝 + scratch spec,合流后跑
      `make e2e-scratch` 验证真 app 里不再回退。

## 不做

- 不动后端 `chat_svc` 的自主轮落库逻辑(它本身是对的)。
- 不动 `liveUsage` / `liveContextWindow` 的会话级语义 —— 它们服务 composer 进度条,
  取 primary 流即可,不需要 per-message。
- 不顺手改无关文件(`pkg/codex/*` 的既有改动不是本任务的)。
