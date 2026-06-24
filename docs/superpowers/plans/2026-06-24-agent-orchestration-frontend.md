# Agent 编排能力基座 — 前端 实现计划（plan-1b）

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 plan-1 的 headless 编排引擎建 React 前端：创建 Run 弹窗 + 结构图/活动流双视图 + runHeader 干预条 + 任务看板 + 节点钻入 + 生命周期/边缘态 + 大图缩放 + i18n + 桌面通知，落地 `agentre.pen` 14 帧（spec §8）。

**Architecture:** 新前端域 `frontend/src/components/agentre/orchestration/`（与 `group-chat/` 平级）+ 两个 zustand store（`orch-run-list-store` 侧栏列表、`orch-run-store` Run 详情 + 事件实时归并）。视图模型由**纯函数 transform**（`graph-data.ts` / `feed-data.ts`：`Task[]` → 节点/边/feed）承载——可单测、与渲染解耦。双视图共用外壳，runHeader 内分段控件切换。经 Wails 绑定（plan-1 的 `RunCreate/RunList/RunLoad/RunPause/RunResume/RunStop/RunSpeak` + `orch:run:*` 事件）与后端通信；不加 HTTP-style API。

**Tech Stack:** React 19、TypeScript、Vite、Tailwind v4、shadcn `@/components/ui/*`、zustand、react-i18next、Wails 绑定（`frontend/wailsjs`）、Vitest、Playwright e2e。

**依赖：** plan-1（后端引擎）已完成并合并——本计划消费其 Wails 绑定与事件。

## Global Constraints

- **新可见 UI 文案全部走 i18n**：`react-i18next` 的 `t(...)` + `frontend/src/i18n/locales/{zh-CN,en}/common.json` 双语；**禁硬编码中文**（`i18next/no-literal-string` 会拦 JSX）；不翻译 agent/用户/terminal/markdown 等动态内容；`frontend/src/__tests__/i18n.test.ts` 校验 key 覆盖与双语一致。
- **表单控件一律 shadcn `@/components/ui/*`**；禁原生 `<select>`。
- **前后端只走 Wails 绑定**（`frontend/wailsjs/go/app/App`）；不加 HTTP-style app API。
- **测试 mock 规则**：组件（间接）import `wailsjs/runtime` 或 `wailsjs/go/app/App` → 渲染它的测试**必须 per-file `vi.mock`**（`importActual` + override），**不要加全局 vite alias**（会破坏 `App.test`/`foundation.test` 的 `importActual`）；判断哪些测试要补 mock 要跑全量 vitest，不是 focused。见 [[reference_frontend_wails_runtime_test_mock]]。
- **pnpm 是 source of truth**（非 npm）；`make generate` 刷新 `frontend/wailsjs/` 后再用新绑定。
- **设计稿 = `agentre.pen` 14 帧**（dark）；本计划实现**行为 + 结构 + 数据→视图映射**，像素级布局/配色对齐对应帧。主题变量走 `$primary`/`$status-*`/`$agent-1..10`（light/dark 双值）。
- **状态胶囊只用客观生命周期**（待开始/运行中/等待中/等待你/完成/已暂停/已取消；技术崩溃才 error）；语义结果（评审通过/驳回…）是 `Result` 自由文本叙述，**不入状态枚举**；**不做"返工次数"指标**。
- **TDD**：纯函数（transform/store reducer）先写 vitest 失败用例再实现；组件写渲染断言。

---

## 后端契约（plan-1 产出，本计划消费）

- 绑定：`RunCreate(req)→RunDetailDTO`、`RunList()→RunItemDTO[]`、`RunLoad(id)→RunDetailDTO`、`RunPause/RunResume/RunStop(id)→void`、`RunSpeak(sessionId, message)→void`。
- DTO：`RunItemDTO{id,goal,leaderAgentId,status,projectId,createtime,updatetime}`、`TaskDTO{id,runId,agentId,sessionId,parentTaskId,kind,status,brief,result,callSeq}`、`RunDetailDTO{run,tasks}`。
- 事件（`wailsjs/runtime` 的 `EventsOn`）：`orch:run:done`、`orch:run:paused`、`orch:run:resumed`、`orch:run:stopped`、`orch:run:deadlock{runId,cycle}`，以及 **Task 1 新增** `orch:run:updated{runId}`（任务态变化时）。
- Task 状态字符串：`pending`/`running`/`awaiting-children`/`awaiting-user`/`done`/`canceled`/`paused`/`error`；Run 状态：`pending`/`running`/`paused`/`done`/`stopped`。

---

## File Structure（本计划新建/修改）

**新建（前端）：**
- `frontend/src/stores/orch-run-list-store.ts` — 侧栏 Run 列表。
- `frontend/src/stores/orch-run-store.ts` — Run 详情缓存 + 事件归并。
- `frontend/src/components/agentre/orchestration/graph-data.ts` — `Task[]`→结构图节点/边/树规模（纯函数）。
- `frontend/src/components/agentre/orchestration/feed-data.ts` — `Task[]`→活动流条目（纯函数）。
- `frontend/src/components/agentre/orchestration/index.tsx` — 编排 Run 外壳（RunList ｜ 中视图 ｜ 任务看板）。
- `frontend/src/components/agentre/orchestration/run-new-dialog.tsx` — 创建 Run 弹窗。
- `frontend/src/components/agentre/orchestration/run-list.tsx` — 左侧 Run 列表。
- `frontend/src/components/agentre/orchestration/run-header.tsx` — 速览 + 干预 + 视图切换。
- `frontend/src/components/agentre/orchestration/structure-graph.tsx` — 结构图画布（含生命周期/边缘态 + 大图）。
- `frontend/src/components/agentre/orchestration/activity-feed.tsx` — 活动流视图。
- `frontend/src/components/agentre/orchestration/task-board.tsx` — 右栏任务看板 + 产出物 tab + 节点钻入 transcript。
- `frontend/src/components/agentre/orchestration/__tests__/*` — vitest。
- `frontend/src/i18n/locales/{zh-CN,en}/common.json` — 加 `orchestration.*`（修改）。

**修改：**
- `internal/service/orch_svc/*`（Task 1：新增 `orch:run:updated` 事件，少量后端改动）。
- `frontend/src/stores/chat-tabs-store.ts` — `TabKind` 加 `run`。
- `frontend/src/components/agentre/chat-tabs/chat-panel-host.tsx` — 路由 `run`。
- `frontend/src/App.tsx` / `AppRail` — 「编排」区段导航 + Run 事件订阅。

---

## Task 1: 后端补 `orch:run:updated` 事件（前端实时刷新依赖）

**Files:**
- Modify: `internal/service/orch_svc/{dispatch,complete,send,control,finish}.go`（任务态变化处 emit）
- Modify: `internal/service/orch_svc/orch.go`（事件名常量 + `emitRunUpdated(runID)` helper）
- Test: `internal/service/orch_svc/event_test.go`

**Interfaces:**
- Produces: `(s *orchSvc) emitRunUpdated(ctx, runID int64)` → `s.emit.Emit(ctx, "orch:run:updated", map[string]any{"runId": runID})`；在 `Dispatch`/`watchCompletion`（done/error）/`Send`/`reportToParent`（父态翻转）处调用。

- [ ] **Step 1: 写失败测试** `event_test.go`（dispatch 后应 emit `orch:run:updated`）

```go
package orch_svc_test

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
	"github.com/agentre-ai/agentre/internal/repository/orch_repo/mock_orch_repo"
	"github.com/agentre-ai/agentre/internal/service/orch_svc"
	"github.com/agentre-ai/agentre/internal/service/orch_svc/mock_orch_svc"
)

func TestDispatch_EmitsRunUpdated(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	chat := mock_orch_svc.NewMockChatGateway(ctrl)
	agents := mock_orch_svc.NewMockAgentLookup(ctrl)
	tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
	emit := mock_orch_svc.NewMockEmitter(ctrl)
	orch_svc.Default().RegisterDeps(chat, agents, nil, tasks, nil, emit)

	tasks.EXPECT().FindBySession(gomock.Any(), int64(500)).Return(&orch_entity.Task{ID: 9, RunID: 100, SessionID: 500, Status: orch_entity.TaskRunning}, nil)
	agents.EXPECT().FindByName(gomock.Any(), "李").Return(&agent_entity.Agent{ID: 3, Name: "李"}, nil)
	tasks.EXPECT().CountByRunAgent(gomock.Any(), int64(100), int64(3)).Return(int64(0), nil)
	chat.EXPECT().EnsureOrchSession(gomock.Any(), gomock.Any()).Return(int64(600), nil)
	tasks.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil)
	tasks.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	chat.EXPECT().SendAndForget(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	chat.EXPECT().ObserveTurn(gomock.Any()).Return(make(<-chan orch_svc.TurnDone), func() {}).AnyTimes()
	emit.EXPECT().Emit(gomock.Any(), "orch:run:updated", gomock.Any()).MinTimes(1)

	Convey("dispatch 触发 orch:run:updated", t, func() {
		_, err := orch_svc.Default().Dispatch(context.Background(), 500, "李", "做X", false)
		So(err, ShouldBeNil)
	})
}
```

- [ ] **Step 2: 跑测试看它失败** → `go test ./internal/service/orch_svc/ -run TestDispatch_EmitsRunUpdated`（FAIL：未 emit）。

- [ ] **Step 3: 实现** — `orch.go` 加 helper，在 `dispatch.go`（建 child 后）、`complete.go`（done/error 后）、`send.go`（重开后）、`finish.go`（已 emit run:done，非根也 emitRunUpdated）调用：

```go
func (s *orchSvc) emitRunUpdated(ctx context.Context, runID int64) {
	if s.emit != nil {
		s.emit.Emit(ctx, "orch:run:updated", map[string]any{"runId": runID})
	}
}
```

- [ ] **Step 4: 跑测试通过 + 全包回归 + Commit**

```bash
cd /Users/codfrm/Code/agentre/agentre
go test ./internal/service/orch_svc/...
git add internal/service/orch_svc/ && git commit -m "✨ orch_svc: emit orch:run:updated(前端实时刷新)"
```

---

## Task 2: 刷新 Wails 绑定 + 编排事件名模块

**Files:**
- Generate: `frontend/wailsjs/go/app/App.{d.ts,js}` + `models.ts`（`make generate`）
- Create: `frontend/src/components/agentre/orchestration/events.ts`
- Test: `frontend/src/components/agentre/orchestration/__tests__/events.test.ts`

**Interfaces:**
- Produces: `ORCH_EVENTS = { updated:"orch:run:updated", done:"orch:run:done", paused:"orch:run:paused", resumed:"orch:run:resumed", stopped:"orch:run:stopped", deadlock:"orch:run:deadlock" } as const`。

- [ ] **Step 1: 刷新绑定**

Run: `cd /Users/codfrm/Code/agentre/agentre && make generate`
Expected: `App.d.ts` 出现 `RunCreate`/`RunList`/`RunLoad`/`RunPause`/`RunResume`/`RunStop`/`RunSpeak`，`models.ts` 出现 `app.RunItemDTO`/`app.TaskDTO`/`app.RunDetailDTO`。

- [ ] **Step 2: 写 `events.ts` + 测试**

```ts
// events.ts
export const ORCH_EVENTS = {
  updated: "orch:run:updated",
  done: "orch:run:done",
  paused: "orch:run:paused",
  resumed: "orch:run:resumed",
  stopped: "orch:run:stopped",
  deadlock: "orch:run:deadlock",
} as const;
export type OrchEventName = (typeof ORCH_EVENTS)[keyof typeof ORCH_EVENTS];
```

```ts
// __tests__/events.test.ts
import { describe, it, expect } from "vitest";
import { ORCH_EVENTS } from "../events";
describe("ORCH_EVENTS", () => {
  it("与后端事件名一致", () => {
    expect(ORCH_EVENTS.updated).toBe("orch:run:updated");
    expect(Object.values(ORCH_EVENTS)).toContain("orch:run:deadlock");
  });
});
```

- [ ] **Step 3: 跑测试 + Commit**

```bash
cd frontend && pnpm test -- src/components/agentre/orchestration/__tests__/events.test.ts
cd /Users/codfrm/Code/agentre/agentre && git add frontend/ && git commit -m "✨ orch 前端: 刷新绑定 + 编排事件名模块"
```

---

## Task 3: `orch-run-list-store`（侧栏 Run 列表）

**Files:**
- Create: `frontend/src/stores/orch-run-list-store.ts`
- Test: `frontend/src/stores/__tests__/orch-run-list-store.test.ts`

**Interfaces:**
- Consumes: `RunList` 绑定（per-file `vi.mock`）。
- Produces: zustand store `useOrchRunListStore`：`{ runs: app.RunItemDTO[]; loading: boolean; load(): Promise<void>; upsert(run): void; __reset(): void }`（mirror `group-list-store.ts`）。

- [ ] **Step 1: 写失败测试**（`vi.mock` 掉 `RunList`，断言 `load()` 填充 `runs`）

```ts
import { describe, it, expect, vi, beforeEach } from "vitest";
vi.mock("../../../wailsjs/go/app/App", () => ({
  RunList: vi.fn().mockResolvedValue([{ id: 1, goal: "做登录页", status: "running" }]),
}));
import { useOrchRunListStore } from "../orch-run-list-store";

describe("orch-run-list-store", () => {
  beforeEach(() => useOrchRunListStore.getState().__reset());
  it("load 填充 runs", async () => {
    await useOrchRunListStore.getState().load();
    expect(useOrchRunListStore.getState().runs).toHaveLength(1);
    expect(useOrchRunListStore.getState().runs[0].goal).toBe("做登录页");
  });
});
```

- [ ] **Step 2: 跑测试看它失败** → FAIL（store 不存在）。

- [ ] **Step 3: 实现**（mirror `group-list-store.ts` 的 zustand + `__reset`）

```ts
import { create } from "zustand";
import { RunList } from "../../wailsjs/go/app/App";
import type { app } from "../../wailsjs/go/models";

interface OrchRunListState {
  runs: app.RunItemDTO[];
  loading: boolean;
  load: () => Promise<void>;
  upsert: (run: app.RunItemDTO) => void;
  __reset: () => void;
}

export const useOrchRunListStore = create<OrchRunListState>((set, get) => ({
  runs: [],
  loading: false,
  async load() {
    set({ loading: true });
    try {
      set({ runs: (await RunList()) ?? [], loading: false });
    } catch {
      set({ loading: false });
    }
  },
  upsert(run) {
    const rest = get().runs.filter((r) => r.id !== run.id);
    set({ runs: [run, ...rest] });
  },
  __reset: () => set({ runs: [], loading: false }),
}));
```

- [ ] **Step 4: 跑测试 + Commit**

```bash
cd frontend && pnpm test -- src/stores/__tests__/orch-run-list-store.test.ts
cd /Users/codfrm/Code/agentre/agentre && git add frontend/ && git commit -m "✨ orch 前端: run-list-store"
```

---

## Task 4: `orch-run-store`（Run 详情 + 事件归并）

**Files:**
- Create: `frontend/src/stores/orch-run-store.ts`
- Test: `frontend/src/stores/__tests__/orch-run-store.test.ts`

**Interfaces:**
- Consumes: `RunLoad` 绑定。
- Produces: `useOrchRunStore`：`{ details: Map<number, app.RunDetailDTO>; loadRun(id): Promise<void>; onRunEvent(name, payload): void; deadlocks: Map<number, number[]>; __reset() }`。`onRunEvent` 对 `updated/done/paused/...` 重新 `loadRun`（简单可靠）；`deadlock` 记 `cycle` 供高亮。

- [ ] **Step 1: 写失败测试**（loadRun 填充 + deadlock 事件记 cycle）

```ts
import { describe, it, expect, vi, beforeEach } from "vitest";
vi.mock("../../../wailsjs/go/app/App", () => ({
  RunLoad: vi.fn().mockResolvedValue({ run: { id: 1, status: "running" }, tasks: [{ id: 9, runId: 1, status: "running", parentTaskId: 0 }] }),
}));
import { useOrchRunStore } from "../orch-run-store";

describe("orch-run-store", () => {
  beforeEach(() => useOrchRunStore.getState().__reset());
  it("loadRun 填充详情", async () => {
    await useOrchRunStore.getState().loadRun(1);
    expect(useOrchRunStore.getState().details.get(1)?.tasks).toHaveLength(1);
  });
  it("deadlock 事件记录环", () => {
    useOrchRunStore.getState().onRunEvent("orch:run:deadlock", { runId: 1, cycle: [700, 800] });
    expect(useOrchRunStore.getState().deadlocks.get(1)).toEqual([700, 800]);
  });
});
```

- [ ] **Step 2: 跑测试看它失败** → FAIL。

- [ ] **Step 3: 实现**

```ts
import { create } from "zustand";
import { RunLoad } from "../../wailsjs/go/app/App";
import type { app } from "../../wailsjs/go/models";
import { ORCH_EVENTS } from "../components/agentre/orchestration/events";

interface OrchRunState {
  details: Map<number, app.RunDetailDTO>;
  deadlocks: Map<number, number[]>;
  loadRun: (id: number) => Promise<void>;
  onRunEvent: (name: string, payload: { runId: number; cycle?: number[] }) => void;
  __reset: () => void;
}

export const useOrchRunStore = create<OrchRunState>((set, get) => ({
  details: new Map(),
  deadlocks: new Map(),
  async loadRun(id) {
    const d = await RunLoad(id);
    const m = new Map(get().details);
    m.set(id, d);
    set({ details: m });
  },
  onRunEvent(name, payload) {
    if (!payload?.runId) return;
    if (name === ORCH_EVENTS.deadlock && payload.cycle) {
      const dm = new Map(get().deadlocks);
      dm.set(payload.runId, payload.cycle);
      set({ deadlocks: dm });
    }
    // 任何 run 事件 → 重新拉详情(简单可靠;数据量小)。
    void get().loadRun(payload.runId);
  },
  __reset: () => set({ details: new Map(), deadlocks: new Map() }),
}));
```

- [ ] **Step 4: 跑测试 + Commit**

```bash
cd frontend && pnpm test -- src/stores/__tests__/orch-run-store.test.ts
cd /Users/codfrm/Code/agentre/agentre && git add frontend/ && git commit -m "✨ orch 前端: run-store(详情+事件归并)"
```

---

## Task 5: `graph-data.ts` — 结构图视图模型（纯函数，核心逻辑）

**Files:**
- Create: `frontend/src/components/agentre/orchestration/graph-data.ts`
- Test: `frontend/src/components/agentre/orchestration/__tests__/graph-data.test.ts`

**Interfaces:**
- Produces（纯函数，无 wails import → 无需 mock）：
  - `type GraphNode = { agentId: number; tasks: TaskLite[]; status: NodeStatus; isLeader: boolean }`。
  - `type GraphEdge = { from: number; to: number; kind: "dispatch" | "report"; }`（节点=agent，边连 agent）。
  - `type NodeStatus = "running"|"waiting"|"waiting-user"|"done"|"error"|"idle"`（卡级聚合：任一 `awaiting-user`→`waiting-user`；任一 `running`→`running`；任一 `error`→`error`；全 `done`→`done`；`awaiting-children`/ask 等待→`waiting`）。
  - `type TreeStats = { nodes: number; subagents: number; depth: number }`。
  - `buildGraph(detail: RunDetailDTO): { nodes: GraphNode[]; edges: GraphEdge[]; stats: TreeStats }`。
  - `lifecycle(detail): "empty"|"running"|"completed"|"paused"|"stopped"`（驱动空/起步态、完成态、暂停态外壳）。

> spec §8 已定：**节点=agent**、dispatch 边连卡头、卡内任务行、卡级聚合状态；**等待态归并**为「等待中（其他 agent）」vs「等待你（awaiting-user）」。

- [ ] **Step 1: 写失败测试**（聚合状态 + 边 + 树规模 + lifecycle）

```ts
import { describe, it, expect } from "vitest";
import { buildGraph, lifecycle } from "../graph-data";

const detail = (tasks: any[], runStatus = "running") => ({ run: { id: 1, leaderAgentId: 2, status: runStatus }, tasks });

describe("graph-data", () => {
  it("节点按 agent 聚合, 边来自 dispatch 父子", () => {
    const g = buildGraph(detail([
      { id: 1, agentId: 2, parentTaskId: 0, kind: "dispatch", status: "running" }, // Leader 根
      { id: 2, agentId: 3, parentTaskId: 1, kind: "dispatch", status: "running" },
      { id: 3, agentId: 3, parentTaskId: 1, kind: "dispatch", status: "done" },    // 同 agent 第二任务 → 同节点
    ]) as any);
    expect(g.nodes).toHaveLength(2);                         // agent 2 + agent 3
    const a3 = g.nodes.find((n) => n.agentId === 3)!;
    expect(a3.tasks).toHaveLength(2);                        // 卡内两任务行
    expect(a3.status).toBe("running");                       // 聚合: 有 running
    expect(g.edges).toContainEqual({ from: 2, to: 3, kind: "dispatch" });
    expect(g.stats.nodes).toBe(2);
  });
  it("awaiting-user 聚合为 waiting-user(等待你)", () => {
    const g = buildGraph(detail([{ id: 1, agentId: 2, parentTaskId: 0, status: "running" }, { id: 2, agentId: 4, parentTaskId: 1, status: "awaiting-user" }]) as any);
    expect(g.nodes.find((n) => n.agentId === 4)!.status).toBe("waiting-user");
  });
  it("lifecycle: 只有 Leader 根任务时为 empty", () => {
    expect(lifecycle(detail([{ id: 1, agentId: 2, parentTaskId: 0, status: "running" }]) as any)).toBe("empty");
    expect(lifecycle(detail([], "done") as any)).toBe("completed");
  });
});
```

- [ ] **Step 2: 跑测试看它失败** → FAIL。

- [ ] **Step 3: 实现 `graph-data.ts`**（纯函数；聚合规则按 spec §8）

```ts
import type { app } from "../../../wailsjs/go/models";

export type TaskLite = app.TaskDTO;
export type NodeStatus = "running" | "waiting" | "waiting-user" | "done" | "error" | "idle";
export interface GraphNode { agentId: number; tasks: TaskLite[]; status: NodeStatus; isLeader: boolean; }
export interface GraphEdge { from: number; to: number; kind: "dispatch" | "report"; }
export interface TreeStats { nodes: number; subagents: number; depth: number; }

function aggregate(tasks: TaskLite[]): NodeStatus {
  const s = new Set(tasks.map((t) => t.status));
  if (s.has("error")) return "error";
  if (s.has("awaiting-user")) return "waiting-user";
  if (s.has("running")) return "running";
  if (s.has("awaiting-children")) return "waiting";
  if (tasks.length && tasks.every((t) => t.status === "done")) return "done";
  return "idle";
}

export function buildGraph(detail: app.RunDetailDTO): { nodes: GraphNode[]; edges: GraphEdge[]; stats: TreeStats } {
  const tasks = detail.tasks ?? [];
  const leaderAgent = detail.run.leaderAgentId;
  const byAgent = new Map<number, TaskLite[]>();
  for (const t of tasks) {
    if (!byAgent.has(t.agentId)) byAgent.set(t.agentId, []);
    byAgent.get(t.agentId)!.push(t);
  }
  const nodes: GraphNode[] = [...byAgent.entries()].map(([agentId, ts]) => ({
    agentId, tasks: ts, status: aggregate(ts), isLeader: agentId === leaderAgent,
  }));
  // dispatch 边: 子任务的 parent agent → 子任务 agent(去重到 agent 级)。
  const taskById = new Map(tasks.map((t) => [t.id, t]));
  const seen = new Set<string>();
  const edges: GraphEdge[] = [];
  for (const t of tasks) {
    if (!t.parentTaskId) continue;
    const parent = taskById.get(t.parentTaskId);
    if (!parent || parent.agentId === t.agentId) continue;
    const key = `${parent.agentId}->${t.agentId}`;
    if (seen.has(key)) continue;
    seen.add(key);
    edges.push({ from: parent.agentId, to: t.agentId, kind: "dispatch" });
  }
  // 树深: 沿 parentTaskId 最长链。
  const depthOf = (id: number, guard = 0): number => {
    const t = taskById.get(id);
    if (!t || !t.parentTaskId || guard > 64) return 0;
    return 1 + depthOf(t.parentTaskId, guard + 1);
  };
  const depth = tasks.reduce((m, t) => Math.max(m, depthOf(t.id)), 0);
  return { nodes, edges, stats: { nodes: nodes.length, subagents: tasks.length, depth } };
}

export function lifecycle(detail: app.RunDetailDTO): "empty" | "running" | "completed" | "paused" | "stopped" {
  const st = detail.run.status;
  if (st === "done") return "completed";
  if (st === "paused") return "paused";
  if (st === "stopped") return "stopped";
  const tasks = detail.tasks ?? [];
  if (tasks.length <= 1) return "empty"; // 只有 Leader 根任务 → 起步态
  return "running";
}
```

- [ ] **Step 4: 跑测试 + Commit**

```bash
cd frontend && pnpm test -- src/components/agentre/orchestration/__tests__/graph-data.test.ts
cd /Users/codfrm/Code/agentre/agentre && git add frontend/ && git commit -m "✨ orch 前端: graph-data 结构图视图模型(纯函数)"
```

---

## Task 6: `feed-data.ts` — 活动流视图模型（纯函数）

**Files:**
- Create: `frontend/src/components/agentre/orchestration/feed-data.ts`
- Test: `__tests__/feed-data.test.ts`

**Interfaces:**
- Produces: `type FeedItem = { id: string; kind: "dispatch"|"report"|"finish"|"blocked"|"ask"; agentId: number; text: string; ts: number }`；`buildFeed(detail): FeedItem[]`（按 `createtime`/`updatetime` 排序；dispatch→一条；done 且有 `result`→report 一条带语义结果文本；`error`→blocked/技术中断条）。语义结果直接作叙述文本（印证"结果是自由文本"）。

- [ ] **Step 1: 写失败测试**

```ts
import { describe, it, expect } from "vitest";
import { buildFeed } from "../feed-data";
describe("feed-data", () => {
  it("dispatch + 完成报告各成一条, 按时间排", () => {
    const items = buildFeed({ run: { id: 1, leaderAgentId: 2, status: "running" }, tasks: [
      { id: 2, agentId: 3, parentTaskId: 1, kind: "dispatch", status: "done", brief: "做X", result: "已完成X", createtime: 100, updatetime: 200 },
    ] } as any);
    expect(items.map((i) => i.kind)).toContain("dispatch");
    expect(items.map((i) => i.kind)).toContain("report");
    expect(items.find((i) => i.kind === "report")!.text).toContain("已完成X");
  });
});
```

- [ ] **Step 2: 跑测试看它失败** → FAIL。

- [ ] **Step 3: 实现 `feed-data.ts`**

```ts
import type { app } from "../../../wailsjs/go/models";

export interface FeedItem { id: string; kind: "dispatch" | "report" | "finish" | "blocked" | "ask"; agentId: number; text: string; ts: number; }

export function buildFeed(detail: app.RunDetailDTO): FeedItem[] {
  const items: FeedItem[] = [];
  for (const t of detail.tasks ?? []) {
    if (t.parentTaskId) {
      items.push({ id: `d${t.id}`, kind: "dispatch", agentId: t.agentId, text: t.brief, ts: t.createtime ?? 0 });
    }
    if (t.status === "done" && (t.result ?? "").trim()) {
      items.push({ id: `r${t.id}`, kind: "report", agentId: t.agentId, text: t.result, ts: t.updatetime ?? 0 });
    }
    if (t.status === "error") {
      items.push({ id: `e${t.id}`, kind: "blocked", agentId: t.agentId, text: t.result || "技术中断", ts: t.updatetime ?? 0 });
    }
  }
  return items.sort((a, b) => a.ts - b.ts);
}
```

- [ ] **Step 4: 跑测试 + Commit**

```bash
cd frontend && pnpm test -- src/components/agentre/orchestration/__tests__/feed-data.test.ts
cd /Users/codfrm/Code/agentre/agentre && git add frontend/ && git commit -m "✨ orch 前端: feed-data 活动流视图模型(纯函数)"
```

---

## Task 7: `TabKind "run"` + ChatPanelHost 路由 + Rail「编排」区段

**Files:**
- Modify: `frontend/src/stores/chat-tabs-store.ts`（`TabKind` 加 `run`）
- Modify: `frontend/src/components/agentre/chat-tabs/chat-panel-host.tsx`（路由 `run`→`<OrchestrationRun>`）
- Modify: `frontend/src/App.tsx` / `AppRail`（「编排」区段 + Run 事件订阅）
- Test: `frontend/src/components/agentre/chat-tabs/__tests__/chat-panel-host.test.tsx`（已存在则追加用例）

**Interfaces:**
- Consumes: `useChatTabsStore`、`OrchestrationRun`（Task 8 外壳，先放占位组件）。
- Produces: `TabKind` 增 `| { kind: "run"; runId: number; title: string }`；`openRunTab(runId, title)` helper。

- [ ] **Step 1: 写失败测试**（host 在 `kind:"run"` 时渲染编排外壳的 testid）

```tsx
// per-file mock wailsjs（参考现有 chat-panel-host.test 的 mock 块）
import { render, screen } from "@testing-library/react";
// ...mock runtime + App...
import { ChatPanelHost } from "../chat-panel-host";
import { useChatTabsStore } from "../../../../stores/chat-tabs-store";
it("kind:run 渲染编排外壳", () => {
  useChatTabsStore.setState({ tabs: [{ id: "r1", kind: "run", runId: 1, title: "做登录页" }] as any, activeTabId: "r1" });
  render(<ChatPanelHost />);
  expect(screen.getByTestId("orchestration-run")).toBeTruthy();
});
```

- [ ] **Step 2: 跑测试看它失败** → FAIL（无 run 分支）。

- [ ] **Step 3: 实现** — 先建**最小 stub 外壳** `frontend/src/components/agentre/orchestration/index.tsx`（Task 13 充实），让本 task 可编译/测试：

```tsx
// index.tsx (stub; Task 13 拼装 RunList｜中视图｜task-board)
export function OrchestrationRun({ runId, title }: { runId: number; title: string }) {
  return <div data-testid="orchestration-run" data-run-id={runId} aria-label={title} />;
}
```

`chat-tabs-store.ts` 的 `TabKind` 加 `run` 变体；`chat-panel-host.tsx` 加：

```tsx
import { OrchestrationRun } from "../orchestration";
// ...在 tab.kind switch 里:
case "run":
  return <OrchestrationRun key={tab.id} runId={tab.runId} title={tab.title} />;
```

`AppRail`：加「编排」区段项（lucide `waypoints` 图标，i18n `t("orchestration.section")`），点击进编排区（列出 Run）。`App.tsx`：挂 `useEffect` 订阅 `ORCH_EVENTS.*` → `useOrchRunStore.getState().onRunEvent` + `useOrchRunListStore.getState().load()`。

- [ ] **Step 4: 跑测试 + Commit**

```bash
cd frontend && pnpm test -- src/components/agentre/chat-tabs
cd /Users/codfrm/Code/agentre/agentre && git add frontend/ && git commit -m "✨ orch 前端: run tab 类型 + panel 路由 + Rail 编排区段 + 事件订阅"
```

---

## Task 8: 创建 Run 弹窗 `run-new-dialog.tsx`（帧 `创建编排 Run 弹窗`）

**Files:**
- Create: `frontend/src/components/agentre/orchestration/run-new-dialog.tsx`
- Test: `__tests__/run-new-dialog.test.tsx`

> `index.tsx` 外壳 stub 已在 Task 7 建，Task 13 充实；本 task 只做创建弹窗。

**Interfaces:**
- Consumes: `RunCreate`、`ListChatAgents`（取 Leader/团队候选 + 各 agent 危险操作姿态）、workflow 列表绑定（流程库；plan-2 重定位后为 `WorkflowList`）、shadcn `Dialog/Input/Textarea/Select/Button`。
- Produces: `RunNewDialog({open,onOpenChange})`：选 Leader、写目标、选/写编排流程（流程库 ｜ 临时写 ｜ 留空）、可选限定团队（**标注各 agent「危险操作自动放行/需审批」姿态**，spec §3.2/§8）。提交 → `RunCreate(req)` → 打开该 Run 的 tab。

- [ ] **Step 1: 写失败测试**（填目标 + 选 Leader → 点创建 → 调 `RunCreate`）

```tsx
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { vi } from "vitest";
const RunCreate = vi.fn().mockResolvedValue({ run: { id: 7 }, tasks: [] });
vi.mock("../../../../wailsjs/go/app/App", () => ({ RunCreate, ListChatAgents: vi.fn().mockResolvedValue([{ id: 2, name: "架构师" }]), WorkflowList: vi.fn().mockResolvedValue([]) }));
import { RunNewDialog } from "../run-new-dialog";
it("提交调 RunCreate", async () => {
  render(<RunNewDialog open onOpenChange={() => {}} />);
  fireEvent.change(await screen.findByTestId("run-goal"), { target: { value: "做登录页" } });
  fireEvent.click(screen.getByTestId("run-create"));
  await waitFor(() => expect(RunCreate).toHaveBeenCalled());
});
```

- [ ] **Step 2: 跑测试看它失败** → FAIL。

- [ ] **Step 3: 实现**（mirror `group-new-dialog.tsx` 结构；所有文案 `t("orchestration.new.*")`；布局对齐帧 `创建编排 Run 弹窗`）。关键骨架：

```tsx
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogBody, DialogFooter } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Select, SelectTrigger, SelectValue, SelectContent, SelectItem } from "@/components/ui/select";
import { useTranslation } from "react-i18next";
import { RunCreate, ListChatAgents, WorkflowList } from "../../../../wailsjs/go/app/App";
// ...useState: goal, leaderId, flowMode("library"|"adhoc"|"none"), flowId, flowContent, allowedIds...
// 提交: const d = await RunCreate({ goal, leaderAgentId: leaderId, flowId, flowContent, allowedAgentIds: allowedIds });
//        openRunTab(d.run.id, goal); onOpenChange(false);
// 团队选择项旁渲染各 agent 的「危险操作: 自动放行/需审批」徽标(来自 agent 后端权限配置)。
```

- [ ] **Step 4: 跑测试 + Commit**

```bash
cd frontend && pnpm test -- src/components/agentre/orchestration/__tests__/run-new-dialog.test.tsx
cd /Users/codfrm/Code/agentre/agentre && git add frontend/ && git commit -m "✨ orch 前端: 创建 Run 弹窗(Leader/目标/流程/限定团队+危险操作姿态)"
```

---

## Task 9: `run-list.tsx`（左侧 Run 列表 + 无 Run 首次态）

**Files:**
- Create: `frontend/src/components/agentre/orchestration/run-list.tsx`
- Test: `__tests__/run-list.test.tsx`

**Interfaces:**
- Consumes: `useOrchRunListStore`、`RunNewDialog`。
- Produces: `RunList({activeRunId,onSelect})`：列 Run（标题 + 客观状态点 + 时间）；空列表 → onboarding（`创建你的第一个编排 Run` CTA + 三步说明，帧 `编排 Run — 无 Run 首次态`）；顶部「新建编排 Run」按钮开弹窗。

- [ ] **Step 1: 写失败测试**（空列表显示 onboarding CTA；非空列出 Run）

```tsx
// mock useOrchRunListStore 返回 runs=[] → 断言 CTA testid "run-onboarding-cta"
// runs=[{id:1,goal:"X"}] → 断言出现 "X"
```

- [ ] **Step 2-4: 实现 + 测试 + Commit**（布局对齐帧；文案 i18n）

```bash
git commit -m "✨ orch 前端: run-list 侧栏 + 无 Run 首次态 onboarding"
```

---

## Task 10: `run-header.tsx`（速览 + 干预 + 视图切换）

**Files:**
- Create: `frontend/src/components/agentre/orchestration/run-header.tsx`
- Test: `__tests__/run-header.test.tsx`

**Interfaces:**
- Consumes: `RunPause/RunResume/RunStop`、`buildGraph`（取 stats + 客观状态计数）。
- Produces: `RunHeader({detail, view, onView})`：标题 + `结构图/活动流` 分段控件 + `自主运行中`/`已完成`/`已暂停` 状态 + 客观计数（运行/等待你/完成）+ 树规模（节点 N·子agent M·深度 D）+ 累计 token/时长（占位：后端暂未统计则显示 `—`）+ `暂停/继续`/`硬停止`；**软阈值预警**（树规模/时长越线 → 非阻塞黄条，不自动封顶，spec §4）。`等待你` 琥珀高亮。

- [ ] **Step 1: 写失败测试**（暂停按钮调 `RunPause`；视图分段控件触发 `onView`；`等待你` 计数渲染）

- [ ] **Step 2-4: 实现 + 测试 + Commit**（布局对齐帧 runHeader；**不设"返工次数"指标**）

```bash
git commit -m "✨ orch 前端: runHeader(速览+计数+树规模+暂停/硬停止+视图切换+软阈值预警)"
```

---

## Task 11: `structure-graph.tsx`（结构图 + 生命周期/边缘态 + 大图）

**Files:**
- Create: `frontend/src/components/agentre/orchestration/structure-graph.tsx`
- Test: `__tests__/structure-graph.test.tsx`

**Interfaces:**
- Consumes: `buildGraph`/`lifecycle`、`useOrchRunStore.deadlocks`。
- Produces: `StructureGraph({detail, onSelectNode})`：按 `lifecycle` 切外壳——`empty`（仅 Leader 节点 + 居中引导）/`running`（节点卡 + 三类边 + 图例）/`completed`（节点全绿 + 顶部"✓ Run 完成"横幅 + 交付小结）/`paused`（灰化 + 暂停横幅）；节点卡=agent（皇冠标 Leader、卡内任务行、`awaiting-user` 琥珀边 + 内联 `批准/拒绝`、`error` 红边）；**ask 死锁**（`deadlocks` 命中）→ 环上节点红边 + 顶部红横幅"去裁决"；**大图**（节点多）→ 紧凑 chip + 折叠分支 `▶N` + 右下小地图 + 顶部图工具条（缩放/适应/折叠/过滤/聚焦）。点节点 → `onSelectNode(agentId)`。

- [ ] **Step 1: 写失败测试**（completed → 渲染"Run 完成"横幅；deadlock → 渲染裁决横幅；empty → 引导文案；点节点触发回调）

- [ ] **Step 2-4: 实现 + 测试 + Commit**（边色：dispatch=`$primary`/report=`$status-running`/ask=`$agent-7`，落地用真 dash；像素对齐帧 `结构图`/`空·起步`/`完成·审计`/`ask 死锁`/`暂停·错误`/`大图`）

```bash
git commit -m "✨ orch 前端: structure-graph(节点/边/生命周期态/死锁高亮/大图)"
```

---

## Task 12: `activity-feed.tsx`（活动流 + 阻塞条 + 对Leader说）

**Files:**
- Create: `frontend/src/components/agentre/orchestration/activity-feed.tsx`
- Test: `__tests__/activity-feed.test.tsx`

**Interfaces:**
- Consumes: `buildFeed`、`RunSpeak`（对 Leader 说）。
- Produces: `ActivityFeed({detail})`：中栏按时间排 feed（dispatch/report/finish/blocked/ask，语义结果作叙述文本）；顶部**阻塞确认条**（有 `awaiting-user` 任务时 → `批准/拒绝`，按分支阻塞）；底部「对 Leader 说」输入条 → `RunSpeak(rootSessionId, msg)`；同态随 Run 状态改写顶部条（死锁红条/完成绿条/暂停条，帧 `活动流 · *`）。

- [ ] **Step 1: 写失败测试**（feed 渲染 dispatch+report 文本；底部输入提交调 `RunSpeak`）

- [ ] **Step 2-4: 实现 + 测试 + Commit**

```bash
git commit -m "✨ orch 前端: activity-feed(时间线+阻塞条+对Leader说+同态)"
```

---

## Task 13: `task-board.tsx` + 节点钻入（看板/产出物 tab + transcript）

**Files:**
- Create: `frontend/src/components/agentre/orchestration/task-board.tsx`
- Modify: `frontend/src/components/agentre/orchestration/index.tsx`（拼装 RunList｜中视图｜task-board；selectedNode 状态）
- Test: `__tests__/task-board.test.tsx`

**Interfaces:**
- Consumes: `detail.tasks`、`RunSpeak`、现有聊天 transcript 组件（复用 `ChatPanel` 的只读 transcript + block 级虚拟化）。
- Produces: `TaskBoard({detail, selectedAgentId, onSelectTask})`：tab `任务看板 ｜ 产出物`；任务看板=以任务为中心清单（`#`/agent 头像/任务名/客观状态，子agent 缩进）；**节点钻入**——选中某 agent/任务 → 右栏从看板切成**该会话 transcript**（消息/工具调用/插话）+ 底部「对它说」(`RunSpeak(sessionId,…)`)；若该任务 `awaiting-user` → transcript 内联**审批块**（`批准/拒绝`）。`产出物` tab = Run 内 `Refs` 汇总（占位：plan-1 `Refs` 为 JSON，先列引用，结构化展开后续）。

- [ ] **Step 1: 写失败测试**（看板列任务；选中任务 → 出现 transcript 区 + 对它说输入）

- [ ] **Step 2-4: 实现 + 测试 + Commit**（帧 `结构图`右栏 + `节点钻入`）

```bash
git commit -m "✨ orch 前端: task-board(看板/产出物 tab)+ 节点钻入 transcript + 对它说"
```

---

## Task 14: i18n keys `orchestration.*`（双语）

**Files:**
- Modify: `frontend/src/i18n/locales/zh-CN/common.json`
- Modify: `frontend/src/i18n/locales/en/common.json`
- Test: `frontend/src/__tests__/i18n.test.ts`（既有，跑通即可）

**Interfaces:**
- Produces: `orchestration` 命名空间，覆盖 Task 8-13 用到的全部 `t(...)` key（section/new.*/list.*/header.*/graph.*/feed.*/board.*/status.*/controls.*/onboarding.*/deadlock.*/completed.*）。

- [ ] **Step 1: 跑 i18n.test 看失败**（实现组件时用了未登记的 key → 覆盖测试失败）

Run: `cd frontend && pnpm test -- src/__tests__/i18n.test.ts`
Expected: FAIL（缺 key / 双语不一致）。

- [ ] **Step 2: 补齐 `orchestration.*` 双语 key**（zh-CN 中文、en 英文；两文件 key 结构完全一致）。例：

```jsonc
// zh-CN/common.json 片段
"orchestration": {
  "section": "编排",
  "new": { "title": "创建编排 Run", "leader": "Leader", "goal": "目标", "flow": "编排流程", "create": "创建" },
  "header": { "running": "自主运行中", "completed": "已完成", "paused": "已暂停", "waitingYou": "等待你", "treeSize": "节点 {{nodes}} · 子agent {{subagents}} · 深度 {{depth}}" },
  "controls": { "pause": "暂停", "resume": "继续", "stop": "硬停止" },
  "graph": { "view": "结构图", "feedView": "活动流", "empty": "Leader 规划完会自动派发子任务", "completedBanner": "✓ Run 完成", "deadlock": "检测到 ask 等待环 · 去裁决" },
  "onboarding": { "cta": "创建你的第一个编排 Run" },
  "approve": "批准", "reject": "拒绝", "speakToLeader": "对 Leader 说"
}
```

- [ ] **Step 3: 跑 i18n.test 通过 + Commit**

```bash
cd frontend && pnpm test -- src/__tests__/i18n.test.ts
cd /Users/codfrm/Code/agentre/agentre && git add frontend/ && git commit -m "🌐 orch 前端: orchestration i18n 双语 key"
```

---

## Task 15: 桌面通知（等待你 / Run 完成）

**Files:**
- Modify: `frontend/src/App.tsx` 或通知 host（订阅 orch 事件 → 通知）
- Test: `__tests__/orch-notifications.test.tsx`

**Interfaces:**
- Consumes: `ORCH_EVENTS`、现有 `PushNotification`/`NotificationCard`（复用，见 [[reference_agentry_pen_design_file]]）。
- Produces: `orch:run:done` → 完成通知；任务转 `awaiting-user`（经 `orch:run:updated` reload 后检测）→ `等待你` 通知（点击跳该 Run tab）。复用现有应用内通知卡，不新建机制。

- [ ] **Step 1: 写失败测试**（emit `orch:run:done` → 出现完成通知卡）

- [ ] **Step 2-4: 实现 + 测试 + Commit**

```bash
git commit -m "✨ orch 前端: 桌面通知(等待你/Run 完成,复用 PushNotification)"
```

---

## Task 16: e2e — 经 UI 跑一条编排（扩展 plan-1 Task 18）

**Files:**
- Create/Modify: `e2e/tests/orchestration-ui.spec.ts`
- Test: `make e2e`

**Interfaces:**
- Consumes: 真 Wails + fake runtime（fake leader dispatch→子完成→finish，plan-1 Task 18 已建 fake 接缝）。
- Produces: 断言——点「新建编排 Run」→ 填目标 + 选 Leader → 创建 → 结构图出现 Leader 节点 →（fake 驱动 dispatch→回报→finish）→ 节点变绿 + "✓ Run 完成"横幅；DB 孪生：`orchestration_runs.status='done'`；左栏 Run 列表含该 Run；普通会话侧栏**不含**编排子会话。

- [ ] **Step 1: 写 spec**（Playwright；selector 用 testid：`run-goal`/`run-create`/`orchestration-run`/`graph.completedBanner`）

- [ ] **Step 2: 跑 e2e（先 `make e2e-scratch` 调通再收进 tests）+ Commit**

```bash
cd /Users/codfrm/Code/agentre/agentre && make e2e
git add e2e/ && git commit -m "✅ e2e: 编排 Run 经 UI 创建→结构图→完成态 + DB 孪生"
```

---

## 收尾校验

- [ ] `cd frontend && pnpm test`（全量 vitest）→ 全绿（含 i18n.test、App.test、foundation.test 未被新 mock 破坏）。
- [ ] `make lint`（ESLint：`i18next/no-literal-string` 无违例 + golangci）→ 干净。
- [ ] `make e2e` → 编排 UI spec 绿。
- [ ] 真机手验（不阻塞）：起一个真 Run（真 claude Leader）走结构图/活动流/钻入/暂停。

## 交付边界

- **含**：编排 Run 全部前端（14 帧）+ 双视图 + 干预 + 看板/钻入 + 大图 + i18n + 通知 + UI e2e + 后端 `orch:run:updated` 事件。
- **不含**：plan-2（删 group）；产出物（Blackboard）结构化展开（先占位列 `Refs`）；light 主题（设计稿待办）。
