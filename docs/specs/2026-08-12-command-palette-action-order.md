# 命令面板操作分组后置

> Status: Approved
> Owner: chat experience / frontend
> Last updated: 2026-08-12

**Objective:** 让用户进入命令模式后优先看到当前页面可执行的高频新建对话命令，并把低频管理操作统一放在命令结果之后。

**Hard invariant:** 本轮只改变命令模式下 source 分组的展示与键盘遍历顺序；搜索匹配、组内评分排序、命令内容、禁用状态、执行结果、上下文切换、面板关闭和默认会话搜索模式均不得改变。

## Problem

1. **低频管理操作抢占命令模式首项。** 当前命令 source 注册顺序中，`newAgentSource` 位于 `newChatSource` 与 `newProjectChatSource` 之前；命令模式未输入筛选词时，面板因此先显示「操作 / New agent」，再显示当前页面更常用的「新建对话」或「项目新对话」。用户明确要求“把操作放到后面去”，并已批准本轮本地 mockup 中「新建对话 → 操作」的信息层级。

## Actors and user stories

1. 作为使用快捷键打开命令模式的桌面用户，我希望新建对话命令排在管理操作之前，以便打开面板后可以更快地用方向键或回车选择高频任务。
2. 作为仍需创建 Agent 的用户，我希望「New agent」继续保留在同一命令面板中，只是位于主要命令之后，以便能力不丢失且仍可通过搜索快速定位。

## Design decisions

| # | Decision | Basis and rejected option |
|---|---|---|
| 1 | 命令模式中，先展示当前路由激活的新建对话 source，再展示「操作」source | 用户已批准 mockup 的「高频任务优先、低频管理操作后置」方向。Rejected: 保持「操作」在首位——继续让低频操作占据首个键盘选择位置 |
| 2 | 后置规则同时覆盖普通聊天路由和项目路由 | 两个路由分别激活「新建对话」与「项目新对话」，但都属于同一高频用户意图；只调整其中一个会产生同一面板在不同页面优先级不一致的问题。Rejected: 仅调整普通聊天路由——项目页仍保留原有问题 |
| 3 | 使用现有 source 顺序表达优先级，不新增固定区、折叠区、分隔容器或配置项 | 当前面板已经按 active source 的顺序渲染分组，需求只需改变既有优先级。Rejected: 为操作新增底部固定区域或「更多」折叠——引入新的布局与交互概念，但当前没有隐藏或固定操作的需求 |
| 4 | 搜索后仍按 source 分组展示，不把所有命令跨 source 混排 | 本轮目标是默认信息层级与键盘顺序，不是重做搜索模型。Rejected: 按全局 score 混排——会改变搜索结果契约并扩大范围 |

## 命令模式展示与交互

当用户在普通聊天相关页面进入命令模式且没有输入筛选词时，命令面板先显示「新建对话」分组及其 Agent 命令，随后显示「操作」分组及「New agent」。初始键盘选中项和向下遍历顺序随可见 DOM 顺序变化，因此首个可选项属于「新建对话」，而「New agent」仍可继续向下选择或点击执行。

当用户在项目页面进入命令模式时，命令面板先显示当前项目作用域下的「项目新对话」分组，包括既有成员、其它 Agent、不可用状态与上下文表现；「操作」分组位于这些项目新对话结果之后。项目上下文的 Tab 切换、非成员禁用规则以及当前项目变化后的重新加载行为保持不变。

当用户输入查询时，各 source 继续使用既有评分与最多 50 条的组内排序。只要新建对话 source 和操作 source 都有匹配结果，新建对话 source 的匹配分组仍在操作匹配分组之前；若前一个 source 没有匹配结果，则不渲染空分组，后续匹配分组自然上移。

「New agent」命令的标题、描述、图标、搜索关键词、执行行为与可访问名称均保持不变。选择后仍关闭命令面板、请求打开新建 Agent 对话框并导航到组织架构页面。

## 状态、异常与兼容性

本轮不引入新状态或异步请求。某个新建对话 source 正在加载、为空或因当前路由不激活时，面板继续采用既有加载、空结果和 source 过滤行为；操作分组不因前序 source 不可用而被隐藏或禁用。

默认的会话搜索模式不包含「New agent」操作 source，因此其聊天会话与导航分组顺序保持原样。已有快捷键提示、Esc 关闭、回车执行、上下文栏和亮色/暗色视觉样式不变。

## 无障碍

分组后置不得移除任何命令或改变可访问名称。键盘用户按方向键时应按新的视觉顺序遍历，视觉顺序与 DOM 顺序一致；不通过 CSS `order` 制造屏幕阅读器顺序与视觉顺序分离的问题。现有焦点高亮、循环导航与禁用项跳过规则保持不变。

## Out of scope

- 不新增、删除、重命名或重写任何命令。
- 不改变 source 内部的评分、排序、置顶、最近选择或最多渲染条数。
- 不将「操作」固定在 footer，也不新增「更多操作」折叠或独立弹层。
- 不调整命令面板尺寸、颜色、间距、搜索栏、上下文栏或 footer 提示。
- 不修改默认会话搜索模式的聊天会话与导航分组顺序。
- 不处理当前工作区中与命令面板无关的未提交改动。

## Testing decisions

| Seam | What it verifies | Prior art |
|---|---|---|
| 命令面板 focused component test（普通聊天路由） | Given 新建对话与 New agent 都可见，When 进入命令模式，Then「新建对话」分组与首个 Agent 命令出现在「操作 / New agent」之前 | `frontend/src/components/agentre/command-palette/command-palette.test.tsx` 已覆盖命令模式、路由互斥 source 与键盘提示 |
| 命令面板 focused component test（项目路由边界） | Given 项目新对话与 New agent 都可见，When 在项目路由进入命令模式，Then「项目新对话」结果出现在「操作 / New agent」之前，且项目上下文仍可见 | 同文件已有 `/projects` source 互斥与 ContextBar 用例 |
| 既有 focused component suite | 默认模式、查询过滤、不可对话、非成员禁用、Tab 上下文切换、关闭与执行行为不回归 | `frontend/src/components/agentre/command-palette/command-palette.test.tsx` |

该变更的视觉结果已由本地 mockup 确认；实现收尾时以 focused component suite 与源码复审覆盖，不需要为单纯的分组顺序变化新增真实应用 e2e 场景。

## Relevant links

- 本地 mockup（设计证据，不入 Git）：`.dev-kit/artifacts/2026-08-12-command-palette-action-order/mockups/`
- 命令面板实现：`frontend/src/components/agentre/command-palette/command-palette.tsx`
- 命令面板测试：`frontend/src/components/agentre/command-palette/command-palette.test.tsx`

## Open questions

无。
