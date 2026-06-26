# `/new` 斜杠命令 — 设计

## 背景与目标

聊天输入框已有斜杠命令系统(`/compact`、`/goal`)。本特性新增 `/new`:在输入框输入
`/new` 回车,**直接开一个全新的会话 tab 并跳转过去**,沿用当前会话的 agent / 项目配置,
但不带任何历史消息;**不动当前会话**(不发消息、不压缩、不修改)。

经确认的产品决策:
- 「相同的全新对话」= **空白新对话(同配置)**,不复制历史消息。
- 新对话继承 **agent + 项目**(沿用 `openNewSession(projectId, agentId, workMode)`;
  `workMode` 为历史遗留字段,所有现存调用方都传 `""` 且无人读取,故同样传 `""`)。
- 触发方式 = **输入 `/new` 回车执行**(与 `/compact` 一致的 `literal_text` + Enter 拦截),
  不是菜单选中即执行。

## 架构

**纯前端特性**,无 Go / 后端改动。复用既有的 `useChatTabsStore.openNewSession` —— 它已经
「新开一个 `kind:"new"` 占位 tab 并设为 active」,真正的 DB 会话在新 tab 首发消息时才惰性创建
(与现有「+ / ⌘N 新建会话」完全一致)。

### 数据流

```
用户在输入框输入 /new 回车
  → chat-input 编辑器 clearContent,调 onSubmit("/new")
  → chat-panel onSubmit 拦截 isExactNewCommand(text)
      derive agentId   = session?.agentId ?? newSessionAgent?.id ?? 0
      derive projectId = session?.projectId ?? newSessionContext?.projectId ?? 0
      useChatTabsStore.getState().openNewSession(projectId, agentId, "")
      return  // 不走 doSend / SendChatMessage / CompactChatSession
  → 新 "new" tab 入列并 setActive → ChatPanelHost 渲染并跳转过去
  → 当前会话完全不受影响
```

## 组件改动

### 1. `slash-commands/registry.ts`
新增第三个命令项,对**所有非空 backend** 可用(纯前端 tab 操作,与 backend 无关):

```ts
{
  name: "new",
  label: "/new",
  description: i18n.t("slashCommands.new.description"),
  resolve(backend) {
    return backend ? { kind: "literal_text", text: "/new" } : null;
  },
}
```

### 2. `chat-panel.tsx` —— `onSubmit` 拦截
- 新增 `isExactNewCommand(text)` 辅助函数(与 `isExactCompactCommand` 同款,匹配
  trim 后恰为 `/new`)。
- 在 `onSubmit` **靠前位置**拦截(在 editing guard 之后、goal/compact 之前),保证在
  已有会话面板与未首发的新 tab 两种情形下都生效。
- 派生 agentId / projectId 后调 `openNewSession(projectId, agentId, "")` 并 `return`。

### 3. i18n
`slashCommands.new.description` 同步加到 `zh-CN` 与 `en` 的 `common.json`。
- zh-CN: `开一个相同配置的全新对话`
- en: `Start a new chat with the same setup`

## 边界与决策

- **流式中**:`/new` 照常生效(开的是另一个 tab,不碰当前会话),不像 `/compact` 需要等
  turn 结束。
- **无法解析出 agent**(纯空态、既无 session 又无 newSessionAgent):no-op 直接 `return`,
  绝不把 `/new` 当普通消息发出去。实际上空态不渲染 composer,此分支基本走不到,仅作兜底。
- **附带图片**:`/new` 是元命令,忽略图片直接开 tab(composer 照常清空);不为此单独加
  拒绝提示。

## 测试(TDD,Red→Green)

1. `slash-commands/__tests__/registry.test.ts`
   - `/new` 在 claudecode / codex / piagent(以及任意非空 backend)可用,
     `resolve` 返回 `literal_text` 且 text 为 `/new`。
2. `components/agentre/__tests__/chat-panel.test.tsx`(沿用现有 `/compact` 测试 harness)
   - 输入恰为 `/new` 时:`useChatTabsStore` 新增一个 `kind:"new"` tab,其 `agentId` /
     `projectId` 取自当前 session,且该 tab 被设为 active。
   - **不**调用 `SendChatMessage` / `CompactChatSession`(当前会话不受影响)。

## 实施清单

- [ ] registry.test.ts 加 `/new` 用例(Red)
- [ ] registry.ts 加 `/new` 项(Green)
- [ ] chat-panel.test.tsx 加 `/new` 行为用例(Red)
- [ ] chat-panel.tsx 加 `isExactNewCommand` + onSubmit 拦截(Green)
- [ ] zh-CN / en common.json 加 `slashCommands.new.description`
- [ ] 收尾:`make lint` + 全量 `pnpm test` 看真 exit code
