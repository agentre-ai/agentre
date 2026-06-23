# 群聊 / 单聊共享消息组件 — 设计

- 日期：2026-06-09
- 状态：设计已确认，待出实现计划
- 范围：前端（`agentre/frontend`）

## 背景与问题

群聊对话界面（`components/agentre/group-chat/`）和单聊对话界面
（`components/agentre/chat.tsx` + `chat-panel.tsx`）是**两套各自独立**的消息渲染实现，
没有复用。由此带来四个症状，根因是同一件事——单聊把这些能力和自己的「气泡外壳 /
滚动容器」长在一起，没有可复用的接缝，群聊只能另起炉灶：

| 关注点 | 单聊 | 群聊 | 根因 |
| --- | --- | --- | --- |
| 消息渲染 | `ChatMessage`（头像+名字+时间+`meta` 插槽+正文） | `group-transcript.tsx` 手写一套 `<div>` | 两套独立实现 |
| 复制按钮 | 经 `meta` 插槽挂 `AssistantMessageActions`（含 Copy） | 无 `meta` 插槽 → 无处可挂 | 渲染路径上没有动作行 |
| 头像 | `AgentAvatar size="md"` 覆写成 `size-7 rounded-lg` | `AgentAvatar size="sm"`（`size-6 rounded-md`） | 各写各的，差 1px + 圆角不一致 |
| 自动跟随滚动 | `chat-panel.tsx` 一整套（at-bottom / follow-on-append / 回到底部按钮） | `index.tsx` 只有一个 `overflow-auto` | 滚动行为没抽出来 |

## 目标

抽出**少量、内聚、单一职责**的共享接缝，让群聊穿过它们渲染，从而：

1. 群聊获得复制按钮、规范头像、自动跟随滚动；
2. 这几类问题今后**改一处即生效**，不再两边各修一遍。

非目标（明确不做）：

- 不重构单聊 `chat-panel.tsx` 那套滚动逻辑（tab 快照 / 虚拟化锚点还原）——它现在
  没坏，且耦合虚拟化，触碰回归风险高；新滚动 hook 仅群聊采用，单聊以后可增量迁移。
- 不让群聊复用单聊的 `ChatTranscript`（虚拟化 / 流式目标 / compact 折叠 / 工具块 /
  plan / rerun-edit 一整套单聊专属机器）——群聊只读纯文本数据模型不该背上整台机器。
- 不引入群聊虚拟化（消息量不大，YAGNI）。
- 不动群聊后端 / 数据模型 / 系统行 + 定向消息语义。

## 决策记录

- **复用程度**：抽公共接缝，两套 transcript 保留（非完全合并，非就地修 bug）。
- **滚动迁移**：新建通用 hook，仅群聊采用；单聊 `chat-panel.tsx` 不动。

## 设计

### 共享单元 ①：`MessageRow` — 消息气泡外壳

- 位置：新文件 `components/agentre/message-row.tsx`。
- 职责：纯展示，只负责「一行消息」的布局骨架——头像列 + 内容列（名字行 / 正文 /
  footer）。不决定业务、不取数据。
- 槽位（示意，最终 props 由实现计划敲定）：

  ```ts
  type MessageRowProps = {
    avatar?: React.ReactNode;     // 逃生口：单聊 user 的「我」灰胶囊从这里传
    avatarName?: string;          // 未传 avatar 时，内置渲染彩色 AgentAvatar
    avatarColor?: AgentColor;
    avatarInitials?: string;
    name?: React.ReactNode;       // 名字；传 null 时不显名（单聊 user 行）
    headerExtra?: React.ReactNode;// 时间 / 群聊「仅 X 收到」灰字
    footer?: React.ReactNode;     // 动作行 / token 行的挂载点（复制按钮）
    children: React.ReactNode;    // 正文
    className?: string;
  };
  ```

- 关键：内置**唯一一份**彩色头像渲染
  `<AgentAvatar size="md" className="size-7 rounded-lg text-[11px]" />`（以单聊为准）。
  群聊不再各写各的 → 头像尺寸 / 圆角**由构造保证一致**，「尺寸不对」消失。
- `footer` 槽让群聊**第一次有了挂动作的地方**。
- 不进 `MessageRow` 的特例：单聊 user 的「我」灰胶囊（走 `avatar` 槽逃生口）；群聊系统
  行（居中胶囊，仍留在 `group-transcript.tsx`）。

### 共享单元 ②：`MessageCopyButton` — 通用复制动作

- 位置：同 `message-row.tsx`（消息展示套件内聚一处）。
- API：`{ text: string; label?: string; ariaLabel?: string }`，内部用现成的
  `@/lib/clipboard-toast` 的 `copyTextWithToast`。
- 接入：
  - 单聊 `AssistantMessageActions` 里那段内联 Copy 改为调用它（行为不变）。
  - 群聊 `group-transcript.tsx` 的 agent / user 文本行 footer 调用它（系统行不给）。
- i18n：文案复用既有 `common.copy`，不新增硬编码中文；如需 aria-label，复用或新增
  `group.*` key 并同步 `zh-CN` / `en` 两份 locale。

### 共享单元 ③：`useStickToBottom` — 通用自动跟随滚动

- 位置：新文件 `hooks/use-stick-to-bottom.ts`；at-bottom 数学拆成纯函数便于单测。
- 行为（与虚拟化无关，纯容器 ref 驱动）：
  - 监听容器 scroll，按 ~32px 容差判定 `atBottom`；
  - 内容追加（依赖项变化）时，若先前贴底则滚到底；用户上滚后不抢滚；
  - 暴露 `atBottom` / `scrollToBottom`，供「回到底部」按钮。
- 接入：群聊 `index.tsx` 的 `overflow-auto` `<section>` 接上，并加一个小的「回到底部」
  按钮（仅在 `!atBottom` 时出现）。

### 两侧接线

- **群聊** `group-transcript.tsx`：agent / user 消息行改写为
  `<MessageRow avatarName={...} avatarColor={...} name={...} headerExtra={定向提示}
  footer={<MessageCopyButton text={msg.content} />}>{renderBody(content)}</MessageRow>`；
  系统行保持原样。`index.tsx` 接 `useStickToBottom` + 回到底部按钮。
- **单聊** `chat.tsx`：`ChatMessage` 重构为**组合 `MessageRow`**；
  `AssistantMessageActions` 的 copy 改为 `MessageCopyButton`。
  **渲染结果保持像素级不变**——这是「以后改一处」的兑现点：气泡外壳今后只改
  `MessageRow`。

## 测试计划（严格 TDD：Red → Green，逐个先写测试看它为正确的原因失败）

1. `hooks/__tests__/use-stick-to-bottom.test.ts`：at-bottom 纯函数；hook 行为（追加滚到
   底 / 非贴底不抢滚 / 回到底部按钮态切换）。
2. `components/agentre/message-row.test.tsx`：渲染头像 / 名字 / footer；断言彩色头像用的
   是 `size-7`（锁住一致性，防回退）。
3. `components/agentre/message-copy-button.test.tsx`：点击 → `copyTextWithToast` 被以正文
   文本调用。
4. 扩 `components/agentre/group-chat/group-chat.test.tsx`：agent 消息存在复制按钮且点击真
   复制；头像走规范尺寸；系统行无复制按钮。
5. 单聊回归闸门：`ChatMessage` 重构后，`__tests__/chat-panel.test.tsx`、
   `__tests__/chat-streams-host.test.tsx` 全绿（无新行为，纯结构等价）。

## 影响面 / 风险

- 触碰单聊 `chat.tsx` 的 `ChatMessage` / `AssistantMessageActions`——以「渲染等价」为约
  束，靠现有单聊测试当回归闸门控制风险。
- 新增 3 个文件 + 各自测试；群聊两文件接线；不触后端、不动迁移。
- 不在同一改动里夹带任何无关重构 / 格式化（遵守 CLAUDE.md 范围纪律）。

## 文件清单（预计）

- 新增：`components/agentre/message-row.tsx`（`MessageRow` + `MessageCopyButton`）
- 新增：`hooks/use-stick-to-bottom.ts`
- 新增测试：`message-row.test.tsx`、`message-copy-button.test.tsx`、
  `use-stick-to-bottom.test.ts`
- 改动：`chat.tsx`（`ChatMessage` / `AssistantMessageActions` 组合共享件）
- 改动：`group-chat/group-transcript.tsx`、`group-chat/index.tsx`（接线）
- 可能改动：`i18n/locales/{zh-CN,en}/common.json`（若新增 aria key）
