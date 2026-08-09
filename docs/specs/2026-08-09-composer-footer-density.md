# Composer 底栏密度：三档容器降级 + 配额计量器重做

> Status: Draft
> Owner: chat experience / frontend
> Last updated: 2026-08-09（第 2 版：运行时验证后的两处修订 —— 决策 4 补「取整到 1000 即进 M」、新增决策 10 与「e2e fake runtime 的能力补齐」一节）

**Objective:** 让会话输入框底栏在任意 chat panel 宽度下都保持**单行、行高恒定**，同时把 Claude 订阅配额（5h / 7d）从"一行同色的纯文本 + 原生 `title`"改造成**双窗口独立取色 + 可键盘触达的 HoverCard 面板**，并修正上下文计量器的进位缺陷，使 `1000k` 这个字符串在任何输入下都不再出现（1M 上下文窗口读作 `1M`）。

**Hard invariants:**

1. **底栏永远单行。** 任何 chat panel 宽度下，底栏都不因内容折行而变高；降级靠隐藏 / 收缩元素，不靠 `flex-wrap`。
2. **信息不因降级而丢失。** 上下文的绝对 token 数（`120.5k / 1M`）与两个配额窗口的百分比数字，在**每一档**都直接可见；被降级隐藏的只有文字标签、前缀与教学性提示，且其语义在 HoverCard / tooltip 中仍可取得。
3. **`UsageState.reason` 的既有分支行为零回归。** `undefined` / 空 reason / `no_credentials` 仍整块不渲染；`ok` 与 stale 仍显示数字；`auth_expired` / `device_offline` / 无 stale 的 `network` 仍显示灰态占位。
4. **HoverCard 不抢输入焦点。** 触发区可被 Tab 聚焦并因此展开面板，但展开动作不得把焦点从正在编辑的输入框移走，也不得吞掉 Enter 发送。
5. **新增与改写的 UI 文案全部走 i18n**，`zh-CN` 与 `en` 双语，由 `src/__tests__/i18n.test.ts` 守卫；不硬编码中文。
6. **动画克制且 reduced-motion 安全**，遵循 `docs/design.md` §8：HoverCard 走 Radix `data-state` + `tw-animate-css`，不引 Framer Motion。
7. **表单控件仍统一走 shadcn `@/components/ui/*`**（本轮用既有的 `hover-card.tsx`，不新造弹层原语）。

## Problem

1. **底栏在窄宽度下没有降级策略，靠文本折行"自己解决"。** `frontend/src/components/agentre/chat.tsx:836` 的底栏是 `<div className="flex items-center gap-2">`——没有 `flex-wrap`，7 个子项没有任何一个带 `shrink-0` / `truncate` / `min-w-0`（唯一的 `min-w-0 flex-1` 是 `chat.tsx:874` 用来把右侧顶开的 spacer）。常驻内容为：图片按钮 + `↵ 发送 · ⇧↵ 换行` + 权限模式 Pill + 模型 Pill + QuotaMeter + ContextMeter + 发送键，约 700px 起步。宽度不足时子项内部文字折行，底栏行高跳变。用户报告："现在的对话框底部塞的东西越来越多了，窄屏幕展示一些内容被换行了"。
2. **`formatTokens` 在 1M 上下文窗口下输出 `1000k`。** `chat.tsx:274-278` 只有两档：`n < 1000` 原样返回，否则一律换算成 `k`（`v >= 100` 时取整）。本项目已在使用 1M 上下文的模型配置，`ContextMeter` 因此渲染成 `1000k / 1000k`。用户直接指出："1000k -> 1M"。
3. **配额整行只有一个色调，由 `Math.max(5h, 7d)` 驱动。** `chat.tsx:346-355` 算出 `peak` 后把 `tone` 套在最外层 div 上。结果：5h 95% / 7d 20% 时两个数字**一起变红**，用户既看不出是哪个窗口告急，也无从判断该等 3 小时还是等 4 天。
4. **唯一能回答"还有多久重置"的信息藏在原生 `title` 里。** 重置倒计时（`chat.tsx:392-399`）与 Sonnet / Opus 7d 拆分（`chat.tsx:400-405`）只进 `describeQuotaTitle`，经 `title=` 属性暴露。原生 `title` 需悬停约 1 秒、无法用键盘触达、无法着色、多行 `\n` 与用于缩进的两个前导空格在各平台渲染不一致。仓库其实已有 shadcn `hover-card.tsx` 与 `tooltip.tsx`。
5. **两个相邻计量器视觉语言不一致，图标失去区分作用。** ContextMeter 有真实进度条（`chat.tsx:445-459`），QuotaMeter 是纯文本，两者却都用 `Gauge` 图标（`chat.tsx:373` 与 `chat.tsx:441`）；后果更重的配额（耗尽 = 硬停）反而没有任何量感。
6. **百分比语义不明。** `chat.quota.aria` = "Claude Code 配额 5h {{five}}% 7d {{seven}}%"、`chat.quota.title.ok` = "Claude Code 配额 · {{device}}"，UI 里没有任何一处交代 43% 是**已用**还是**剩余**。后端 `ccoauth` 读的是 utilization（已用），该语义从未表达给用户。
7. **`cc-usage-store` 的"同值短路"从未生效。** `frontend/src/stores/cc-usage-store.ts:32-48` 的 `shallowSameState` 把 `if (a.fetchedAtMs !== b.fetchedAtMs) return false;`（第 35 行）排在所有百分比比较**之前**，而 `internal/service/cc_usage_svc/manager.go:114 / 122 / 128` 每次 probe 都写入新的 `FetchedAtMs`。因此该函数在 60s 周期下恒返回 `false`，文件头部注释声称的"避免 60s tick 把数字 round 成同一个百分比却让所有订阅者集体 re-render"从未兑现。

## Actors and user stories

1. 作为**在窄 chat panel（侧栏 + 右侧面板同时展开）里工作的桌面用户**，我要底栏始终保持单行，这样输入区不会因为一次宽度变化而跳动、遮挡正文。
2. 作为**使用 1M 上下文模型的用户**，我要上下文余量以 `1M` 而非 `1000k` 表达，这样一眼就能读出量级。
3. 作为**接近 5 小时配额上限的用户**，我要一眼看出是哪个窗口告急、还有多久重置，这样能决定是等一会儿还是换设备 / 换模型继续。
4. 作为**Opus 用量吃紧的订阅用户**，我要能看到 Sonnet / Opus 各自的 7 天占用，这样知道真正的瓶颈在哪个模型上。
5. 作为**键盘用户**，我要用 Tab 就能读到配额详情，而不是只能靠鼠标悬停一个原生 tooltip。

## Design decisions

| # | Decision | Basis and rejected option |
|---|---|---|
| 1 | 降级用 **`@container` 按 composer 自身宽度**触发，不用视口 media query | chat panel 的实际宽度取决于侧栏 / 右侧面板开合，视口宽度读不到它。仓库已有先例 `frontend/src/components/ui/field.tsx:47` 的 `@container/field-group`，Tailwind 4.3（`frontend/package.json:72`）原生支持。Rejected: 视口断点——同一窗口宽度下 panel 可窄可宽，必然误判；Rejected: `flex-wrap`——用户抱怨的正是"变高"，允许折行等于把 bug 正式化 |
| 2 | 牺牲顺序固定为 **①快捷键提示 → ②「上下文」文字标签 → ③`5h`/`7d` 前缀 + 进度条 96px→40px → ④权限模式 Pill 退成纯图标** | 用户裁定（①②③ 为其明确勾选项，④ 为极窄兜底的裁定）。依据是信息密度递减：快捷键提示是一次性教学文案，标签与前缀可由图标 + HoverCard 补偿，进度条是数字的冗余表达。Rejected: 极窄档允许横向溢出 / 滚动——把"看不见"从可控降级变成不可控；Rejected: 极窄档改双行——与 Hard invariant 1 直接冲突 |
| 3 | 上下文的**绝对 token 数任何一档都保留** | 用户明确未授权牺牲它——它是判断"还能聊多久"的唯一定量依据，百分比无法替代（60% of 1M 与 60% of 200k 决策不同）。Rejected: 极窄档把 token 数移入 HoverCard——它是底栏里被看得最频繁的数字，代价高于收益的 ~74px |
| 4 | `formatTokens` 补 **M 档**：`≥1e6` 时 `<10M` 保一位小数、`≥10M` 取整；**且 k 档一旦四舍五入到 1000 就改走 M 档** | 与现有 k 档（`>=100` 取整、否则一位小数）同构，读者不需要学两套规则。进位补充项是 2026-08-09 运行时验证后的用户裁定：若只按量级分档，`999_999` 会落在 k 档并渲染成 `1000k`——正是本轮要消灭的字符串，只是换了触发条件。Rejected: 一律一位小数——`10.0M` 冗余；Rejected: 仅在 max 侧换算——同一行内 `120.5k / 1M` 两侧单位不一致反而更难读（分子分母量级本就常差三个数量级，单位不同是正常的科学计数惯例）；Rejected: 容忍 `1000k` 边界——用户明确要求消灭该字符串 |
| 10 | **e2e fake runtime 补两项能力**：`RunResult.ContextWindow` 上报一个 1M 上下文窗口、`Capabilities()` 增加 `CapSetPermission` | 2026-08-09 运行时验证发现四条验收项无法观测，根因全在 fake 而非实现：`chat-panel.tsx:1246` 的 `max = session.contextWindow`，而 fake 的 `Model: "e2e-fake-model"` 不在 `llmcatalog` 里、`RunResult.ContextWindow` 为 0 → `ContextMeter` 整块不渲染；`isModeSwitchable = caps.has("set_permission_mode")`（`chat-panel.tsx:1234`）而 fake 未声明该 cap → `PermissionModePill` 不渲染。补上后这四条即可在真实应用里观测。`chat.go:3943` 已有 `result.ContextWindow > 0 → sess.ContextWindow` 的落库路径，无需新接缝。Rejected: 把 fake 的 model 改成 `llmcatalog` 里的真实 model id——会把"用哪个真实模型"这一无关语义引进 fake，且 `runtime_test.go:50` 正断言该字符串；Rejected: 接受这四条永久 not observed——用户裁定先补 fake 再交付 |
| 5 | 配额**两个窗口各自独立取色**，删除 `Math.max` 驱动的整行同色 | 直接解决 Problem 3。阈值沿用现状（≥90% `status-error`、≥75% `status-waiting`、否则 `muted-foreground`），不引入新阈值。Rejected: 保留整行同色、仅在 HoverCard 里区分——底栏正是"一眼判断"的场景，把区分推迟到悬停等于没解决 |
| 6 | 原生 `title` → shadcn **HoverCard**，触发区可聚焦 | 直接解决 Problem 4。仓库已有 `frontend/src/components/ui/hover-card.tsx`。Rejected: `tooltip.tsx`——本轮面板含进度条、分组与脚注，超出 tooltip 的纯文本定位；Rejected: `popover.tsx`——Popover 会抢焦点，与 Hard invariant 4 冲突 |
| 7 | 配额图标 `Gauge` → **`Timer`** | 直接解决 Problem 5：两个计量器不再同图标，且 `Timer` 表达"按时间窗口配给"这一配额本质。上下文保留 `Gauge`。Rejected: 给配额也加两条迷你进度条以对齐上下文——每条至少 40px，与本轮"底栏太挤"的主诉求直接对冲；量感改由面板内的进度条承载 |
| 8 | HoverCard 脚注写明"百分比为**已用**比例" | 直接解决 Problem 6，且放在面板而非底栏，不占用稀缺的横向空间。Rejected: 在底栏文案里写「已用 43%」——每个窗口多约 30px，且四档降级下最先被牺牲，等于没写 |
| 9 | `shallowSameState` 的 `fetchedAtMs` 比较**降级为兜底**：先比 reason / stale / 各百分比 / 各 resets_at，全同则判同、忽略 `fetchedAtMs` 差异 | 用户裁定本轮一并修。HoverCard 让 QuotaMeter 的渲染成本上升（多出 4 条进度条与分组），让 60s tick 在数值未变时不再触发 re-render 的收益随之变大。Rejected: 另开一轮——它与本轮改的是同一个功能域的同一条渲染链路，拆开反而使两轮都要碰 `QuotaMeter` |

## 底栏分档行为

底栏容器声明为具名容器（`composer`），四档按容器 inline-size 生效，档与档之间只增删 / 收缩元素，**任何一档都不允许换行**。断点取自本轮 mockup 的实测拖拽结果：

- **wide（> 640px）**：全部元素展开，与今天的宽屏形态一致。
- **mid（≤ 640px）**：隐藏快捷键提示（`chat.composer.shortcuts.send` / `.edit`）；隐藏上下文计量器的「上下文」文字标签。图标、`120.5k / 1M`、进度条、百分比全部保留。
- **narrow（≤ 480px）**：在 mid 基础上，隐藏配额的 `5h ` / `7d ` 前缀（渲染成 `43% · 18%`）；上下文进度条由 96px 收窄到 40px。两个窗口的百分比与上下文的绝对 token 数仍直接可见。
- **ultra-narrow（≤ 380px）**：在 narrow 基础上，权限模式 Pill 隐藏其文字标签，退成「图标 + 下拉箭头」。该 Pill 的模式语义由其既有图标配色与既有 `title` / Popover 承载，不新增补偿机制。

被隐藏的元素一律从可访问性树中一并移除（用 `hidden` 语义而非仅视觉隐藏），避免屏幕阅读器读到用户看不见的文本；其语义补偿见下节与决策 2。

底栏所有子项都必须显式不可收缩或显式可截断，杜绝"靠内部文字折行来适配"的隐式行为——这是 Problem 1 的根因，也是 Hard invariant 1 的实现前提。

## 配额计量器

**底栏形态。** 图标改为 `Timer`。两个窗口的百分比各自按自身取值着色：≥90% 取 `status-error`、≥75% 取 `status-waiting`、否则取 `muted-foreground`。分隔点与前缀保持 `subtle-foreground`。整块不再有统一 tone。

**触发区。** 底栏的配额块本身即 HoverCard 触发区：可被 Tab 聚焦，聚焦或悬停时展开面板，并在 hover / focus 时有可见的背景与描边反馈，以提示"这里还有内容"（现状没有任何 affordance）。展开不移动焦点、不拦截键盘输入。

**面板内容。** 面板标题为「Claude 订阅用量」并在右侧标注设备名（`local` 或远端设备名）。主体按顺序列出：

- **5 小时窗口**：名称、重置倒计时、百分比、一条按百分比着色的进度条。
- **7 天窗口**：同上。
- **Sonnet 7 天 / Opus 7 天**：仅在后端返回了对应字段时出现，视觉上从属于 7 天窗口（缩进 + 左侧分隔线）。

倒计时沿用既有 `formatResetIn` 的 `XdYh` / `Xh` / `Xm` 形式与既有 `chat.quota.resetRemaining` 文案；`resets_at` 缺失时该行不显示倒计时，而不是显示空括号。

**脚注与异常态。** 面板底部固定一行脚注说明百分比为**已用**比例。当 `reason` 为异常态时，脚注替换为该状态的说明并着 `status-waiting` 色：429 退避中 / 网络错误（两者均附"显示上次值"）、OAuth 已过期（含"请在 {{device}} 上运行 `claude /login`"）、设备离线。这些文案沿用既有 `chat.quota.title.*` 键的语义，从原生 `title` 迁入面板。

**渲染分支。** 与现状完全一致，不变：`undefined` / 空 `reason` / `no_credentials` 整块不渲染；`ok` 或 stale 显示数字；`auth_expired` / `device_offline` / 无 stale 的 `network` 显示灰态占位（`—%`）。灰态占位下 HoverCard 仍可展开，面板只显示脚注说明的异常态。

## 上下文计量器

`formatTokens` 增加 M 档，规则与既有 k 档同构：小于 1000 原样；`[1e3, 1e6)` 走 k（商 ≥100 取整、否则一位小数）；`≥1e6` 走 M（商 ≥10 取整、否则一位小数，整数时省掉 `.0`）。**此外，k 档的取整结果一旦达到 1000，改按 M 档渲染**——`1000k` 这个字符串在任何输入下都不得出现。据此 `1_000_000 → 1M`、`1_200_000 → 1.2M`、`10_000_000 → 10M`、`999_000 → 999k`、`999_999 → 1M`、`120_500 → 121k`。分子与分母各自独立换算，允许出现 `121k / 1M`。

其余行为不变：阈值配色、`role="progressbar"` 与 `aria-valuenow` 语义、`chat.context.aria` 的完整 `used / max` 播报均保持现状——分档隐藏的只有视觉标签，无障碍播报不降级。

## 用量 store 的同值短路

`shallowSameState` 调整比较次序与语义：先比较 `reason`、`stale`、两个主百分比、Sonnet / Opus 百分比与全部 `resets_at`；仅当这些全部相同时判为"同值"并跳过 Map 重建，此时忽略 `fetchedAtMs` 的差异。`fetchedAtMs` 不再作为否决条件，但仍随新状态一并写入 store，供需要"数据有多新"的调用方读取。可观察结果：数值未变化的 60s tick 不再触发 QuotaMeter 重渲染；数值一旦变化仍立即反映。

## 无障碍

配额触发区保留既有 `chat.quota.aria` 的完整播报（含两个窗口的数值与设备名），不因分档隐藏前缀而降级。HoverCard 面板由 Radix 关联到触发区，可用 Esc 关闭。所有新增可见文案双语落地。

## e2e fake runtime 的能力补齐

`e2e/` 的 fake runtime 需要多提供两样东西，否则本轮四条可观测要求在真实应用里根本没有渲染对象：

- **上报一个 1M 的上下文窗口**，使 `ContextMeter` 在 e2e 会话中真实渲染，且分母恰好命中 M 档。
- **声明 `set_permission_mode` 能力**，使 `PermissionModePill` 在 e2e 会话中真实渲染，从而能观测 ≤380 档的纯图标降级。

这两项只影响 `-tags e2e` 构建下的 fake，不改任何生产运行时的行为，也不改 `chat_svc` 既有的上下文窗口优先级（`session.ContextWindow` > `provider.ContextWindow` > `llmcatalog` 查表）。fake 回显文本、`ReplyPrefix`、`ProviderSessionID` 与 `Model` 字符串保持不变，既有 e2e 断言不受影响。

## Out of scope

- **不改后端 `cc_usage_svc` / `ccoauth` 的任何契约、字段或 60s probe 周期。** 本轮只消费既有 `UsageState`。
- **不给 `cc_usage_svc` 加 e2e fake 接缝。** 配额数值在验证时以浏览器侧合成 `cc_usage:update` 推送驱动，走的仍是生产订阅链路；为它专门造一套 fake 凭证/HTTP 桩超出本轮价值。
- **不做配额告警 / 通知 / 自动切换设备。** 面板只呈现，不干预运行。
- **不改权限模式 Pill 与模型 Pill 自身的交互、Popover 内容或可切换性**，只在 ultra-narrow 档隐藏权限 Pill 的文字标签。
- **不做配额的历史曲线 / 趋势。**
- **不动 `AGENTS.md` 第 4 条意义上的无关文件**：`describeQuotaTitle` 被 HoverCard 取代后随其删除，属于本轮生产者变更；除此之外不做顺带重构、重命名或格式化。

## Testing decisions

| Seam | What it verifies | Prior art |
|---|---|---|
| `formatTokens` 纯函数单测（vitest） | 进位边界表：`999` / `12.3k` / `121k` / `999k` / `999_999 → 1M` / `1M` / `1.2M` / `10M`，含 k↔M 两侧临界与「取整到 1000 即进 M」 | 无（函数当前无直接单测）；`frontend/src/components/agentre/__tests__/chat.test.tsx` 为同文件测试宿主 |
| fake runtime 的能力单测（Go） | fake 上报的上下文窗口与 `set_permission_mode` 能力 | `internal/pkg/agentruntime/runtimes/fake/runtime_test.go` 已有 `Capabilities` / `RunResult` 断言 |
| Playwright e2e 真实宽度截图（`e2e/`） | 四档降级在真实浏览器下的最终布局：改变 chat panel 宽度后底栏**行高不变**、各档该显示 / 该隐藏的元素符合预期。这是唯一能真正验证"不折行"的手段——jsdom 不实现 `@container`，vitest 无法断言实际隐藏 | `e2e/README.md` 的 Playwright + fake-runtime harness；`docs/verification.md` 的证据留存约定 |

用户裁定测试范围为「e2e 为主 + 仅补 `formatTokens` 边界表」，因此 **QuotaMeter 的状态矩阵（双窗口独立配色、HoverCard 各分支内容、各 `reason` 分支）不新增自动化覆盖**，改由收尾时的源码复审 + 真实应用运行观察承担。

配额数值在 e2e 里由**浏览器侧合成的 `cc_usage:update` 推送**驱动（走生产订阅链路 `EventsOn → cc-usage-store → useCCUsage`），因为 e2e 环境没有 Claude OAuth 凭证、`cc_usage_svc` 只会给 `no_credentials`（该状态下 QuotaMeter 整块不渲染）。合成的只有数据来源，渲染、CSS 容器查询与 Radix 行为均为真。

分档宽度在 e2e 里**直接给定 composer `<form>` 的宽度**，而非拖窗口：本 app 的侧栏在窄视口会自动收起，实测视口 1280 → composer 664、视口 1000 → composer 944（非单调），靠改视口够不到 ≤380 档。容器查询读的就是该元素的 inline-size，因此仍是真实浏览器跑真实 CSS。

**必须一并更新的既有测试（属于变更维护，不计入上述新增覆盖）**：`frontend/src/components/agentre/__tests__/chat.test.tsx:1585-1612` 现断言 `toHaveAttribute("title", expect.stringContaining("resets in 40m"))`（断言在 1605-1608 行）。原生 `title` 被 HoverCard 取代后该断言必然失败，需改为面板内容断言；同组其余用例（不渲染分支、数字渲染、stale、`auth_expired` 占位）对应 Hard invariant 3，应保持通过而不被削弱。

## Open questions

无。

## Links

- Mockup（本地，不入 Git）：`.dev-kit/artifacts/2026-08-09-composer-footer-density/mockups/footer.html` —— 四档实拍、明暗双主题、HoverCard 面板、before/after 折行对比；断点 640 / 480 / 380 由其可拖拽容器实测得出。
- `docs/design.md` §8（motion）、§10（reduced-motion）、色彩 token 表
- `docs/frontend.md`（shadcn `@/components/ui/*` 与 i18n 强制约定）
