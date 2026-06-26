# 编排 S7 — 独立顶级页(IA/导航重构,Run 迁出 chat tab)Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把「Agent 编排」从内嵌 chat 页升级成**独立顶级板块**:新增 `/orchestration`(+ `/orchestration/:runId` 深链)路由 + 左导航栏入口 + `OrchestrationPage` 壳(RunList 侧栏 + 主区:选中 Run 渲 `OrchestrationRun`,无选中渲起步态);**把 Run 彻底迁出 chat tab**(删 `chat-tabs-store` 的 `run` 变体 + `chat-panel-host`/`use-tabs-view`/`chat-page` 相关分支),通知点击改 `navigate("/orchestration/:runId")`。这是用户最初要的「编排独立出来」。

**Architecture:** 选中 Run 用**路由参数** `/orchestration/:runId`(spec §4),不再用本地态/tab——通知器、深链、侧栏选择统一走 `navigate`。`OrchestrationPage` 经 `<Outlet>` 渲染(与 ChatPage/IssuesPage 同级);它内联 `RunList`(侧栏,已是 `{activeRunId,onSelect}` 干净组件)+ `OrchestrationRun`(主区,已存在,S4–S6 内容全在其内)。`run` tab 变体**一步删除不留兼容**(agentre 未发布、无深链历史需保)。AppRail 在代码里**本已是单一 `<aside>` rail**(3 套 rail 是设计稿历史问题,非代码),故「rail 收敛」无代码改动,仅加一项 nav。

**Tech Stack:** React 19 + react-router(MemoryRouter)+ TypeScript + Vitest。纯前端。

## Global Constraints

- **严格 TDD:Red → Green → Refactor。** 每 Task 先写失败测试 → 跑看正确失败 → 最小实现 → 跑过 →(门控)提交。
- **依赖 S4–S6 已落地**:`OrchestrationRun`(index.tsx)是 S4–S6 改过的最终形态;S7 只搬家、不动其内部。执行顺序:S4 → S5/S5b → S6 → S3 → 本 plan(roadmap S7 排最后)。
- **`run` tab 变体一步删除、不留深链兼容**(项目未发布,见 [[project_release_status]]):`TabKind` 去掉 `{kind:"run"}`、删 `openRun`、删 `chat-panel-host`/`use-tabs-view` 的 run 分支、删 `chat-page` 的 RunList 块。
- **只动本切片**:`App.tsx`、新增 `orchestration/orchestration-page.tsx`、`orchestration` barrel/`components/agentre/index.ts`、`stores/chat-tabs-store.ts`、`chat-tabs/chat-panel-host.tsx`、`chat-tabs/use-tabs-view.ts`、`chat-page.tsx`、`orch-notifier.tsx` + i18n + 各测试。**禁止** 顺手重构 OrchestrationRun 内部 / 改其它页。
- **i18n**:`nav.orchestration` 双语键;无其它新可见文案(RunList/OrchestrationRun 文案已存在)。
- **图标**:导航用 tabler `topology-star-3`(spec §4 建议),`import topologyStar3Icon from "@iconify-icons/tabler/topology-star-3"`。
- **共享分支 develop/wyz**:提交带 pathspec;**Commit 门控**。
- **测试命令**:聚焦 `cd frontend && pnpm test -- <path>`;收尾 `make test-frontend` + `make lint`。

---

## File Structure

- `frontend/src/components/agentre/orchestration/orchestration-page.tsx` — **新**。板块壳:`[RunList 侧栏 | 主区]`;主区按 `:runId` 渲 `OrchestrationRun` 或起步态。
- `frontend/src/components/agentre/index.ts` — barrel 导出 `OrchestrationPage`。
- `frontend/src/App.tsx` — `navItems` 加编排项;`<Routes>` 加 `/orchestration` + `/orchestration/:runId`;`pageBreadcrumbKeys` + 面包屑前缀兜底。
- `frontend/src/stores/chat-tabs-store.ts` — 删 `TabKind` 的 `run` 变体 + `openRun`。
- `frontend/src/components/agentre/chat-tabs/chat-panel-host.tsx` — 删 `kind==="run"` 分支 + `HostedOrchestrationRun` + `OrchestrationRun` import。
- `frontend/src/components/agentre/chat-tabs/use-tabs-view.ts` — 删 `kind==="run"` 分支。
- `frontend/src/components/agentre/chat-page.tsx` — 删编排 `AgentPanelSection + RunList` 块 + 相关 import。
- `frontend/src/components/agentre/orch-notifier.tsx` — `openRun` → `navigate("/orchestration/:runId")`。
- `frontend/src/i18n/locales/{zh-CN,en}/common.json` — `nav.orchestration`。
- 测试:新增 `orchestration/__tests__/orchestration-page.test.tsx`;改 `stores/__tests__/chat-tabs-store.test.ts`、`orch-notifier` 测试(若有)。

---

## Task 1: `OrchestrationPage` 壳 + 路由 + 导航入口(先把目的地建出来)

**Files:**
- Create: `frontend/src/components/agentre/orchestration/orchestration-page.tsx`
- Modify: `frontend/src/components/agentre/index.ts`(barrel)
- Modify: `frontend/src/App.tsx`(nav + route + breadcrumb)
- Modify: `frontend/src/i18n/locales/{zh-CN,en}/common.json`(nav.orchestration)
- Test: `frontend/src/components/agentre/orchestration/__tests__/orchestration-page.test.tsx`

**Interfaces:**
- Produces:`export function OrchestrationPage(): JSX.Element`(读 `useParams().runId`;侧栏 `RunList` + 主区 `OrchestrationRun`/起步态)。

- [ ] **Step 1: 写失败测试**

`orchestration-page.test.tsx`:
```tsx
import { render, screen } from "@testing-library/react";
import { MemoryRouter, Routes, Route } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("../../../../wailsjs/go/app/App", () => ({
  RunLoad: vi.fn().mockResolvedValue({ run: { id: 1, goal: "G", status: "running", leaderAgentId: 2 }, tasks: [] }),
  ListChatAgents: vi.fn().mockResolvedValue({ agents: [] }),
}));
vi.mock("../../../../hooks/use-chat-agents", () => ({
  useChatAgents: () => ({ agents: [], loading: false, error: null, reload: vi.fn() }),
}));

import { useOrchRunListStore } from "../../../../stores/orch-run-list-store";
import { OrchestrationPage } from "../orchestration-page";

const renderAt = (path: string) =>
  render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="/orchestration" element={<OrchestrationPage />} />
        <Route path="/orchestration/:runId" element={<OrchestrationPage />} />
      </Routes>
    </MemoryRouter>,
  );

beforeEach(() => {
  useOrchRunListStore.setState({ runs: [] });
});

describe("OrchestrationPage", () => {
  it("无选中 Run:渲染 RunList(起步 CTA)+ 起步主区", () => {
    renderAt("/orchestration");
    expect(screen.getByTestId("run-onboarding-cta")).toBeInTheDocument();
    expect(screen.getByTestId("orchestration-empty-main")).toBeInTheDocument();
  });

  it("带 :runId:主区渲染 OrchestrationRun", () => {
    useOrchRunListStore.setState({ runs: [{ id: 1, goal: "G", status: "running" } as never] });
    renderAt("/orchestration/1");
    expect(screen.getByTestId("orchestration-run")).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: 跑确认失败**

Run: `cd frontend && pnpm test -- src/components/agentre/orchestration/__tests__/orchestration-page.test.tsx`
Expected: FAIL — 模块不存在。

- [ ] **Step 3: 实现 `orchestration-page.tsx`**

```tsx
import * as React from "react";
import { useNavigate, useParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { useOrchRunListStore } from "../../../stores/orch-run-list-store";
import { OrchestrationRun } from ".";
import { RunList } from "./run-list";

export function OrchestrationPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { runId: runIdParam } = useParams();
  const runId = runIdParam ? Number(runIdParam) : null;
  const runs = useOrchRunListStore((s) => s.runs);
  const goal = runId ? (runs.find((r) => r.id === runId)?.goal ?? "") : "";

  return (
    <div className="flex min-h-0 min-w-0 flex-1">
      {/* 左:Run 侧栏(仅 Run, 不混会话) */}
      <aside className="flex w-72 shrink-0 flex-col overflow-y-auto border-r border-border bg-sidebar">
        <RunList
          activeRunId={runId ?? undefined}
          onSelect={(id) => navigate(`/orchestration/${id}`)}
        />
      </aside>

      {/* 主区:选中 Run 渲 OrchestrationRun, 否则起步态(完整总览=S8) */}
      {runId && Number.isFinite(runId) ? (
        <div className="flex min-h-0 min-w-0 flex-1">
          <OrchestrationRun runId={runId} title={goal} />
        </div>
      ) : (
        <div
          data-testid="orchestration-empty-main"
          className="flex flex-1 items-center justify-center p-8 text-center text-sm text-muted-foreground"
        >
          {t("orchestration.onboarding.cta")}
        </div>
      )}
    </div>
  );
}
```
> 主区起步态先用一句 onboarding 文案占位;**完整总览页(统计/进行中卡片/最近完成)= S8**,不在本切片。

- [ ] **Step 4: barrel 导出**

`components/agentre/index.ts` 加(挨着既有页导出):
```ts
export { OrchestrationPage } from "./orchestration/orchestration-page";
```

- [ ] **Step 5: App.tsx 加 nav + route + breadcrumb + i18n**

`App.tsx`:
- import 图标:`import topologyStar3Icon from "@iconify-icons/tabler/topology-star-3";`
- import 页:把 `OrchestrationPage` 加进 `@/components/agentre` 的解构 import。
- `navItems` 在 chat 之后插入:
  ```ts
  { path: "/orchestration", labelKey: "nav.orchestration", icon: topologyStar3Icon },
  ```
- `pageBreadcrumbKeys` 加 `"/orchestration": "nav.orchestration"`。
- 面包屑前缀兜底:`/orchestration/:id` 不精确命中 → 把 `const breadcrumbKey = pageBreadcrumbKeys[location.pathname];` 改为:
  ```ts
  const breadcrumbKey =
    pageBreadcrumbKeys[location.pathname] ??
    (location.pathname.startsWith("/orchestration") ? "nav.orchestration" : undefined);
  ```
- `<Routes>` 在 `/chat` 后加两条:
  ```tsx
  <Route path="/orchestration" element={<OrchestrationPage />} />
  <Route path="/orchestration/:runId" element={<OrchestrationPage />} />
  ```
- i18n:`zh-CN` `nav.orchestration` = `"编排"`;`en` = `"Orchestration"`。

> `isNavItemActive` 已 `startsWith(`${itemPath}/`)`,`/orchestration/123` 高亮编排项,无需改。`getKnownPaths` 迭代 navItems → `/orchestration` 自动纳入 lastPath 持久化白名单。

- [ ] **Step 6: 跑 OrchestrationPage 测试 + App 相关测试**

Run: `cd frontend && pnpm test -- src/components/agentre/orchestration/__tests__/orchestration-page.test.tsx src/__tests__/i18n.test.ts`
Expected: PASS。

- [ ] **Step 7:(门控)提交**

```bash
git commit frontend/src/components/agentre/orchestration/orchestration-page.tsx \
  frontend/src/components/agentre/index.ts frontend/src/App.tsx \
  frontend/src/components/agentre/orchestration/__tests__/orchestration-page.test.tsx \
  frontend/src/i18n/locales/zh-CN/common.json frontend/src/i18n/locales/en/common.json \
  -m "✨ orch:独立顶级页 /orchestration + 导航入口 + OrchestrationPage 壳"
```

---

## Task 2: 迁移 Run 的旧入口 —— chat-page RunList 删除 + 通知器改 navigate

**Files:**
- Modify: `frontend/src/components/agentre/chat-page.tsx`(删编排 RunList 块)
- Modify: `frontend/src/components/agentre/orch-notifier.tsx`(openRun → navigate)
- Test: `orch-notifier` 既有测试(若有)

**Interfaces:**
- 消费方从 `openRun` 切到 `navigate("/orchestration/:runId")`。

- [ ] **Step 1: 删 chat-page 的编排 RunList 块**

`chat-page.tsx`:删除侧栏顶部整块:
```tsx
          {/* 编排 Run 列表 — 固定在侧栏顶部 */}
          <div className="mb-2">
            <AgentPanelSection label={t("orchestration.section")} />
            <RunList activeRunId={...} onSelect={...} />
          </div>
```
删除随之 unused 的 import:`RunList`、`useOrchRunListStore`(若仅此处用)、以及 `useChatTabsStore.getState().openRun` 用法。`AgentPanelSection` 若别处仍用则保留。**用 tsc/eslint no-unused 兜底找干净。**

- [ ] **Step 2: 改 orch-notifier 用 navigate**

`orch-notifier.tsx`:`useNavigate()`(组件在 `<MemoryRouter>` 内,可用);把两处
```ts
onClick: () => useChatTabsStore.getState().openRun(runId, goal),
```
改为
```ts
onClick: () => navigate(`/orchestration/${runId}`),
```
删 `useChatTabsStore` import(若仅此处用)。

- [ ] **Step 3: 跑相关测试(确认编译 + 通知器)**

Run: `cd frontend && pnpm test -- src/components/agentre/__tests__/orch-notifier.test.tsx`(若存在)+ `pnpm test -- src/components/agentre/chat-page` 相关。
Expected: PASS(或编译通过)。

- [ ] **Step 4:(门控)提交**

```bash
git commit frontend/src/components/agentre/chat-page.tsx frontend/src/components/agentre/orch-notifier.tsx \
  -m "♻️ orch:Run 入口迁出 chat 侧栏/通知器改 navigate(/orchestration/:runId)"
```

---

## Task 3: 删 `run` tab 变体(store + host + tabs-view)

**Files:**
- Modify: `frontend/src/stores/chat-tabs-store.ts`(删 `run` 变体 + `openRun`)
- Modify: `frontend/src/components/agentre/chat-tabs/chat-panel-host.tsx`(删 run 分支 + `HostedOrchestrationRun`)
- Modify: `frontend/src/components/agentre/chat-tabs/use-tabs-view.ts`(删 run 分支)
- Test: `stores/__tests__/chat-tabs-store.test.ts`(删 openRun describe)

- [ ] **Step 1: 改测试(删 openRun 用例)= Red**

`chat-tabs-store.test.ts`:删除 `describe("chat-tabs-store · openRun", …)` 整块(L557–~582)。这会让后续实现删 `openRun` 后测试仍绿;为先验证「删除是安全的」,先跑全量 store 测试确认 openRun 是唯一引用点已在 Task 2 清除。

- [ ] **Step 2: 删 chat-panel-host 的 run 分支**

`chat-panel-host.tsx`:删 `:9` `import { OrchestrationRun } from "../orchestration";`;删 `:105-110` 的 `) : t.meta.kind === "run" ? ( <HostedOrchestrationRun .../> ` 分支(并回正三元结构);删 `:259-277` 的 `HostedOrchestrationRun` 组件定义。

- [ ] **Step 3: 删 use-tabs-view 的 run 分支**

`use-tabs-view.ts:60`:删 `if (tab.meta.kind === "run") { … }` 分支(其 title 派生逻辑)。

- [ ] **Step 4: 删 store 的 run 变体 + openRun**

`chat-tabs-store.ts`:`TabKind` 去掉 `| { kind: "run"; runId: number; title: string }`;`Actions` 去掉 `openRun`;删 `openRun` 实现(`:131-148`)。`chat-tabs-persistence.ts` 未序列化 run 变体(已核:只 session/terminal),无需改。

- [ ] **Step 5: 跑 + tsc 全绿(确认无残留 kind==="run")**

Run: `cd frontend && pnpm test -- src/stores/__tests__/chat-tabs-store.test.ts && pnpm exec tsc --noEmit`
Expected: PASS + 0 类型错误(若 tsc 报某处仍引用 `kind:"run"`/`openRun`,补删)。

- [ ] **Step 6:(门控)提交**

```bash
git commit frontend/src/stores/chat-tabs-store.ts \
  frontend/src/components/agentre/chat-tabs/chat-panel-host.tsx \
  frontend/src/components/agentre/chat-tabs/use-tabs-view.ts \
  frontend/src/stores/__tests__/chat-tabs-store.test.ts \
  -m "🔥 orch:删 chat tab 的 run 变体(Run 已迁独立页, 无深链兼容)"
```

---

## Task 4: 全量校验

- [ ] **Step 1: 前端全量 + tsc + lint(看真 exit code)**

```bash
cd frontend && pnpm test
cd frontend && pnpm exec tsc --noEmit
cd /Users/codfrm/Code/agentre/agentre && make lint
```
Expected: 全绿;无 `kind:"run"`/`openRun`/`HostedOrchestrationRun` 残留;`i18next/no-literal-string` 不报。

- [ ] **Step 2: `make test-frontend`(wails generate + vitest)**

Run: `make test-frontend`
Expected: 绿。

- [ ] **Step 3:(门控)如有遗漏修复的提交,带 pathspec 单独提交。**

---

## Final verification (after all tasks)

- [ ] `make test-frontend` + `make lint` 全绿。
- [ ] **真机 `make dev` 手验(本切片重点)**:左导航出现「编排」项 → 点进 `/orchestration` → 左 RunList + 右起步态;点一个 Run → URL 变 `/orchestration/:id`、主区渲结构图/任务板/钻入(S4–S6 成果);编排通知点击 → 跳 `/orchestration/:id`;chat 页侧栏**不再有** Run 列表、也无法再开 run tab。对照设计稿 `K0q5q`/`Zk6xc`(总览壳)与 §4 IA。
- [ ] 切到 /chat /projects 等,确认 chat tab 持久化/流式不受影响(run 变体删除未波及 session/terminal)。

## Self-review notes(写计划时已核对)

1. **Spec coverage(§4 / §5 item1-3,5,8 / roadmap S7)**:`/orchestration`(+`:runId` 深链)路由 + nav 入口 → Task 1;`OrchestrationPage` 壳(RunList 侧栏 + 主区 master-detail)→ Task 1;RunList 仅 Run、不混会话 → 复用既有 RunList(本就仅 Run);Run 迁出 chat tab(chat-page/host/tabs-view/store)→ Task 2/3;**AppRail 收敛**→ 代码本是单 rail,仅加 nav 项(设计稿 3-rail 是历史,非代码)。✅
2. **划界(留后续切片)**:**完整总览页(统计/进行中卡/最近完成)= S8**,本切片主区无选中时仅起步文案占位;awaiting-user 审批 = S9。
3. **选中态用路由参数**:通知器/深链/侧栏统一 `navigate("/orchestration/:runId")`,避免散落本地态;`isNavItemActive`/breadcrumb 已处理 `/orchestration/:id`(前缀)。
4. **一步删 run 变体、无兼容层**:项目未发布(见 [[project_release_status]]),无历史深链需保;`chat-tabs-persistence` 未序列化 run 变体,旧持久化无残留 run tab。
5. **删除 blast radius 已全覆盖**:`openRun` 消费点 = chat-page(Task 2)+ orch-notifier(Task 2);`kind==="run"` = chat-panel-host(Task 3)+ use-tabs-view(Task 3);测试 = chat-tabs-store.test(Task 3)。用 `tsc --noEmit` 兜残留(Task 3 Step 5 / Task 4)。
6. **依赖**:`OrchestrationRun`(主区)是 S4–S6 后形态;本切片只搬不改其内部。须最后执行。
7. **Placeholder/类型**:无 TODO;`OrchestrationPage`、路由路径、`nav.orchestration`、testid(`orchestration-empty-main`/`orchestration-run`/`run-onboarding-cta`/`run-row-{id}`)实现与测试一致。
