# AskUserQuestion 失效终态 + 失败可诊断性 设计

- 日期:2026-06-24
- 范围:仅 `agentre/`(桌面端 Wails 应用)
- 状态:已批准设计,待转实施计划

## 背景与问题

会话 `sess-1174`(claudecode 后端)出现:用户点击 AskUserQuestion 交互卡的「提交回复」时报
**「提交失败,请稍后再试」**,之后无法继续。

### 根因(已查实)

1. 「提交失败,请稍后再试」是前端**通用兜底文案**(`canonical-tool/user-ask/card.tsx`
   `handleSubmit` catch 分支:`err instanceof Error ? err.message : t("canonical.userAsk.errors.submitFailed")`)。
   Wails 调用失败时 reject 的不是 `Error` 实例,所以**任何**后端 `AnswerUserQuestion` 失败都会落到这句兜底
   —— 文案本身不携带具体信息。

2. 后端 `chat_svc.AnswerUserQuestion`(`ask_user_question.go`)→ claudecode
   `Runtime.SubmitAnswer`(`runtimes/claudecode/control.go`)在以下情况返回错误:
   - `r.cache.Get(sessionKey)` 拿不到 active → `ErrNoActiveTurn`(**turn 已结束,runner 已从 cache 移除**)
   - `takeAskWaiter(requestID)` 为 nil → "no waiting AskUserQuestion"(waiter 已消费 / 已被清)
   - `RespondToControl` 自身失败

   turn 结束会触发 `cache.Remove → claudeActive.Close()`,`Close()` 直接
   `askWaiters = nil`(`runtimes/claudecode/active.go`)清掉所有待答问题。

3. **死卡**:AskUserQuestion 活卡的状态只有 `answered` / `skipped` 两种终态
   (`card.tsx`:`isLocked = isAnswered || isSkipped || submitting`)。turn 结束 / 用户边问边发消息越过后,
   卡片既非 answered 也非 skipped,仍可点击,但提交必然失败。

4. **可诊断性缺口**:`AnswerUserQuestion` / `SubmitAnswer` 整条失败路径**零日志**,所以运行日志里查不到该错误。

5. 附带问题:未答的 AskUserQuestion 活卡**从不落库**(turn 未经答题路径 finalize),reload 后无任何记录。

### 决策(已与用户确认)

- 死卡表示法:**失效终态 + 持久化**(根因修复,覆盖 reload / turn 结束)。
- 范围:**仅 AskUserQuestion**;ToolPermission CLI 审批卡同病但本次不做。

## 设计原则

完全复刻仓库已有的 `expired` 先例,接进同一套模式(OCP,低风险):
- `handlers.MarkRunningSubagentsCancelled(finalBlocks)`:finalize 时把 running subagent 标 `canceled`(`handlers/subagent.go`)。
- `chatSvc.takeToolApprovals(sessionID)`:finalize 时把 pending 审批 block 标 `expired`(`tool_approval.go`)。
- 前端 `toolApproval.status.expired`(「已过期」)已有失效态视觉。

## A. 数据模型 —— 新增互斥终态 `Expired`

在 `answered` / `skipped` 之外新增第三个互斥终态 `expired`,沿这条链透传:

| 层 | 文件 | 改动 |
|---|---|---|
| 持久化 block | `internal/service/chat_svc/blocks/user_ask.go` | `UserAskBlock` 加 `Expired bool` `json:"expired,omitempty"` |
| canonical | `internal/pkg/agentruntime/canonical/user_ask.go` | `UserAsk` 加 `Expired bool` `json:"expired,omitempty"` |
| 运行时事件 | `internal/pkg/agentruntime/event.go` | `UserAskResolved` 加 `Expired bool` |
| wire DTO | `internal/service/chat_svc/types.go` | `ChatBlockAskUserQuestion` 加 `Expired bool` `json:"expired,omitempty"` |
| 投影 | `internal/service/chat_svc/ask_user_question.go` | `askUserQuestionBlockToChatBlock` 把 `b.Expired` 灌进 wire DTO + `canonical.UserAsk` |
| 前端 DTO | `frontend/src/components/agentre/canonical-tool/types.ts` | `UserAskDTO.expired?: boolean` |

终态互斥不变量:`expired` 仅在 `!answered && !skipped` 时被置位。

## B. 产端根因修复 —— turn finalize 时标记 + 持久化 + live emit

### B1. finalBlocks 标记函数(与 `MarkRunningSubagentsCancelled` 同形)

新增 `internal/service/chat_svc/handlers/user_ask.go`:

```go
// MarkUnansweredUserAsksExpired finalize 时把仍未答/未跳过的 AskUserQuestion
// block 标 expired(对齐 MarkRunningSubagentsCancelled / takeToolApprovals)。
// 返回被标记的 requestID 列表,供调用方 emit live 锁定 patch。
func MarkUnansweredUserAsksExpired(finalBlocks []cagoblocks.ContentBlock) []string {
    var expired []string
    for _, b := range finalBlocks {
        ua, ok := b.(*blocks.UserAskBlock)
        if !ok || ua.Answered || ua.Skipped || ua.Expired {
            continue
        }
        ua.Expired = true
        expired = append(expired, ua.RequestID)
    }
    return expired
}
```

### B2. 接入 finalize 路径

`internal/service/chat_svc/chat.go`,`acc.Finalize()` 之后、`SetBlocks(finalBlocks)` 之前
(`MarkRunningSubagentsCancelled` 调用点附近):

```go
expiredAsks := handlers.MarkUnansweredUserAsksExpired(finalBlocks)
```

- 标记随 `assistantMsg.SetBlocks(finalBlocks)` 落库 → **reload 后展示失效态**(同时根治"未答活卡不落库"问题)。
- 对 `expiredAsks` 每个 requestID emit 一条 `StreamAskUserQuestion`(expired patch,复用
  `UserAskResolvedHandler` 的 emit 形状:`{kind: "ask_user_question", requestId, askUserQuestion: blkPtr}`,
  `blkPtr.Expired==true`)→ **当前在屏活卡不用 reload 立即锁定**。

这覆盖 `ErrNoActiveTurn`(turn 已 idle 后点提交)那一类。

边界:`expiredAsks` 为空时不 emit、不产生额外副作用;abort 路径同样适用(turn 真正结束即应失效未答卡)。

## C. 前端兜底 —— 失效渲染 + 提交失败即锁卡

`frontend/src/components/agentre/canonical-tool/user-ask/card.tsx`:

- `const isExpired = !!payload?.expired;` 并入 `isLocked = isAnswered || isSkipped || isExpired || submitting`。
- 失效态渲染独立灰态卡片,标题/状态显示「提问已失效」(对齐 `toolApproval.status.expired` 视觉),不渲染提交按钮。
- `handleSubmit` catch 分支:不再无脑显示通用兜底,改为可读文案
  **「提问已失效:会话已结束或已被新消息跳过」**,并把该卡本地置为失效(后续提交被 `isLocked` 拦住),
  防止反复点击反复失败。这覆盖**边问边发消息**(steer 越过、turn 仍在跑、产端难即时探知)那一类残余。

## D. 补日志(可诊断性)

- `chat_svc.AnswerUserQuestion`(`ask_user_question.go`)每个 error 分支补
  `logger.Ctx(ctx).Warn("chat_svc.AnswerUserQuestion: <reason>", zap.Int64("sessionId", req.SessionID),
  zap.String("requestId", req.RequestID), zap.Error(err))`。
- claudecode `Runtime.SubmitAnswer`(`runtimes/claudecode/control.go`)失败分支补
  `logger.Ctx(ctx).Warn("claudecode runtime: SubmitAnswer ...", zap.Int64("sessionID", ...),
  zap.String("requestID", ...), zap.Error(err))`(对齐该文件既有 `runtime.go` 日志前缀风格)。

## 数据流

```
turn 结束
  └─ acc.Finalize() → finalBlocks
       ├─ MarkRunningSubagentsCancelled(finalBlocks)        [既有]
       ├─ expiredAsks = MarkUnansweredUserAsksExpired(...)   [新增]
       ├─ assistantMsg.SetBlocks(finalBlocks) → 落库(expired:true 持久化)
       └─ for reqID in expiredAsks: emit StreamAskUserQuestion(expired) → 前端 markAskUserQuestionAnswered 合并 → 活卡锁定

reload / history 回放
  └─ UserAskBlock(expired:true) → askUserQuestionBlockToChatBlock → wire + canonical(expired) → UserAskCard 失效态

用户在失效卡上点提交(残余路径,如 steer 越过)
  └─ AnswerUserQuestion 失败 → catch → 明确文案 + 本地锁卡(不再反复失败)
       └─ 后端各失败分支 Warn 日志(可诊断)
```

## 测试(TDD:先红后绿)

### Go 单测
- `handlers/user_ask_test.go`:`MarkUnansweredUserAsksExpired` 表驱动
  —— 未答→`expired=true` 且返回 requestID;`answered` / `skipped` / 已 `expired` → 不动、不返回;非 UserAskBlock 跳过。
- finalize 集成层(chat_svc 既有 turn 测试套路):断言未答 AskUserQuestion 落库带 `expired:true`,
  且对该 requestID emit 了 expired patch。
- `AnswerUserQuestion` 各 error 分支断言返回对应错误(日志副作用不强测)。

### 前端 vitest
- `card.test.tsx`:expired payload 渲染失效态且无提交按钮 / 不可提交;submit 失败时锁卡 + 明确文案。
- i18n:`frontend/src/i18n/locales/{zh-CN,en}/common.json` 在 `canonical.userAsk` 下补
  `expired` 标题/状态文案与 `errors.submitFailed` 的失效文案;过 `frontend/src/__tests__/i18n.test.ts`。

## 不做(YAGNI / 范围纪律)

- ToolPermission CLI 审批卡(`permWaiter`)同病,本次**不做**,留后续单独处理。
- 不改动 subagent / toolApproval 既有路径。
- 不碰与本任务无关的文件(如工作区已有的 `orchestration/graph-data.ts` 改动不纳入本次提交)。
