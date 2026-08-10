# 供应商 notice 的两处显示缺陷

<!-- File: docs/specs/2026-08-10-provider-notice-fixes.md -->

> Status: Approved
> Owner: chat experience
> Last updated: 2026-08-10

**Objective:** 让供应商切换 notice 在 transcript 里读得懂——显示供应商名而不是 UUID，并且不再
在它上方误画一条「后台任务完成 · 已自动续跑」横幅。

**Hard invariant:** 不改变供应商切换本身的任何行为（校验、落库、生效时机、网关路由、子进程
重启），只改 notice 的负载内容与 transcript 的行判定；真实的自主续轮横幅必须照常出现。

## Problem

1. **切换 notice 显示原始 provider key（UUID）。** 用户在 pill 上选的是供应商名，transcript 里
   回看到的却是 `Switched to provider 36a04495-dfe9-40ef-a3c5-2b62468db6b1 from here`。渲染处
   直接把 key 插进文案（`frontend/src/components/agentre/transcript-row-view.tsx:716-718`），
   而负载里本来就只有 key（`internal/service/chat_svc/session_provider.go:191`）。既有的供应商
   回退 notice 同源同形（`transcript-row-view.tsx:721-723`）。真机证据：
   `e2e/scratch/2026-08-10-session-provider-switch/report.md` 的 R1 与截图 `v4-switch-notice.png`。
2. **切换 notice 会误触发「后台任务完成 · 已自动续跑」横幅。** 自主续轮的判定是启发式的——
   「assistant 消息且紧邻前一条不是 user」（`frontend/src/components/agentre/chat.tsx:1319-1327`）。
   切换 notice 是一条独立的 assistant 消息，插在上一轮回复之后，正好构成 `assistant→assistant`，
   于是被判成自主续轮。规格从未要求这条横幅，用户会以为后台真的跑了一轮任务。同一份报告的 R2
   给出了对照组：未切换过的会话没有这条横幅。

## Actors and user stories

1. 作为回看 transcript 的用户，我希望看到「改用了哪个供应商」的名字，以便和我在 pill 上选的对上。
2. 作为切换过供应商的用户，我不希望 transcript 里出现我没做过的「后台任务自动续跑」提示。

## Design decisions

| # | Decision | Basis and rejected option |
|---|---|---|
| 1 | **notice 负载增加供应商显示名**，前端优先用它、为空时回退到 key | 名字只有产出 notice 的后端手里有（前端的供应商列表可能还没拉、或该供应商已被删）。拒绝前端按 key 查名——列表是异步且可能缺项，会出现「有时有名有时是 UUID」的不稳定文案 |
| 2 | **回退 notice 一并带名**，查不到供应商时保持显示 key | 与切换 notice 同源同形，同一次改动覆盖；回退的典型诱因就是供应商被删，此时没有名字可显示，key 是唯一能给的信息 |
| 3 | **修正自主续轮的行判定：只含 notice 块的消息不参与 user/assistant 相邻关系**，既不自己算自主轮，也不打断它前后两条消息的判定 | 改的是判定本身的不完备（它假设 assistant 消息只有「轮回复」一种，而现在有第三种：系统 notice 行），不是在消费端给生产端打补丁。拒绝「不再用独立消息承载切换 notice」——切换发生在轮之外，它本就没有可挂靠的轮，且该形状是上一轮规格的决策 9 |

## notice 负载与渲染

切换 notice 与回退 notice 的结构化负载都增加一个供应商显示名字段；后端在产出 notice 时按当前
解析到的供应商实体填入，取不到（供应商已删）时留空。

前端渲染这两类 notice 时，显示名非空就用它，为空则回退到 key，文案本身不变。切换回「跟随
agent 绑定」时没有供应商，沿用既有的专用文案，不受影响。

## 自主续轮横幅的判定

一条消息若其内容块全部是 notice，则视为系统提示行：它自己永远不被判为自主续轮，且在判断其它
消息的「紧邻前一条」时被跳过——即它前后的真实轮之间的相邻关系保持不变。

真实的自主续轮（CLI 后台任务完成后自主跑的一轮）必须照常显示横幅，包括「切换 notice 之后紧
接着发生一次自主续轮」这种叠加情形。

## Out of scope

- 供应商切换的任何行为（校验 / 落库 / 生效时机 / 网关路由 / 子进程重启）。
- 上一轮遗留的其它未观察项（远端 agentred、轮进行中切换）。
- 自主续轮判定改用显式标记（本轮只让 notice 行透明；把启发式换成后端下发的显式字段是更大的改动，没有当前需求驱动）。

## Testing decisions

| Seam | 验证内容 | Prior art |
|---|---|---|
| `chat_svc` 单元（mockgen repo mock，不连 DB） | 切换 notice 负载带供应商显示名；供应商取不到时留空；回退 notice 同样 | `session_provider` 与 `chat_internal_test.go` 既有 notice 用例 |
| 前端（vitest） | 有名时渲染名、无名时回退 key；只含 notice 块的消息不产生自主续轮横幅，且不改变其前后消息的判定；真实自主续轮仍出横幅 | `transcript-row-view-notice.test.tsx`、`chat.tsx` 的 autonomousIds 既有用例 |

**无法自动化的部分**：真机上「切换后 transcript 读起来是否真的对得上 pill 里选的名字」由收尾的
运行时验证补一张截图，并入上一轮的 `e2e/scratch/2026-08-10-session-provider-switch/report.md`。

## Links

- 上一轮规格：[2026-08-10 已有会话切换 LLM 供应商](./2026-08-10-session-provider-switch.md)（本轮修的是它交付后运行时暴露的两个显示问题，不改它的任何已批准行为）
- 真机证据与两个缺陷的原始记录：`e2e/scratch/2026-08-10-session-provider-switch/report.md` 的 R1 / R2（本地，未入库）
- 设计评审用的 mockup：`.dev-kit/artifacts/2026-08-10-provider-notice-fixes/mockups/notice-fixes.html`（本地，未入库；notice 与横幅按 `transcript-row-view.tsx` / `auto-trigger-banner.tsx` 的真实结构还原，token 取自 `frontend/src/styles/globals.css`）。约束性结论以本文正文为准，图仅为佐证。

## Open questions

无。
