# 编排 S4 — 结构图节点模型(×N 合并 + 顶层 agent 分组)Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把结构图的节点模型对齐设计稿 §9 最终拍板:同一 subagent 的多次调用在图上合并成一个 `×N` 节点;Leader 直属(顶层)agent 的多次对话用「`<name> · N 会话`」分组容器逐条展示;`calls[]` 携带 per-call 的 `sessionId/callSeq/brief/status`,为后续任务板(S5)与钻入(S6)铺路。

**Architecture:** 改动只在前端两个文件 + 一个 i18n。`graph-data.ts` 的 `buildGraph` 仍按 `agentId` 聚合成一个节点(沿用现有「agent-as-node」),但每个 `GraphNode` 新增三件事:`callCount`(该 agent 的派发调用数 = `×N`)、`isTopLevel`(其某个 task 的父 task 属于 Leader → 直属顶层)、`calls[]`(per-call 投影)。`structure-graph.tsx` 据此分三种渲染:单次调用(现状)、非顶层多次(合并 `×N` 徽标、不列子行)、顶层多次(分组容器 + 逐条 `会话` 子行)。边/树深/死锁/横幅/皇冠全部不动。

**Tech Stack:** React 19 + TypeScript + Tailwind v4 + Vitest + Testing Library + react-i18next。纯前端,零后端改动(数据已齐:`TaskDTO.callSeq/sessionId/parentTaskId`、`RunItemDTO.rootTaskId/leaderAgentId`)。

## Global Constraints

- **严格 TDD:Red → Green → Refactor。** 每个 Task 先写失败测试 → 跑一次看它**因正确原因失败** → 最小实现 → 跑过 → (按需)提交。
- **只动本切片文件**:`graph-data.ts` / `structure-graph.tsx` / 两个 `common.json` + 对应测试。**禁止** drive-by 重构 / 改名扫荡 / 顺手格式化 / 动 task-board / feed / run-header / store。看到无关脏数据**只上报、不顺手修**。
- **本切片不做的(明确划界,留给后续切片):**
  - **`+N 子代理` CLI 徽标 = S5b**(零后端、从 session transcript derive `tool_use.subagent`)。本切片**不**渲染它。
  - **per-call 钻入(点 `会话#1` → 该 session)= S6**。本切片 `onSelectNode(agentId)` 契约**不变**,顶层分组的子行是展示性的,点击仍冒泡到整卡选中 agent。
  - **任务板 `5/12` 计数 + per-call `#1/#2` 行 = S5**。本切片只改结构图。
- **i18n:** 新增可见文案一律走 `t(...)`,**同步** `frontend/src/i18n/locales/zh-CN/common.json` 与 `en/common.json`;`i18next/no-literal-string` 会拦截 JSX 里的硬编码中文。新键挂在既有 `orchestration.graph.*` 命名空间下(已有 `empty/completedBanner/pausedBanner/stoppedBanner/deadlock/leaderCrown`)。
- **共享分支 develop/wyz 有并发会话 + 共享 index**:提交**永远带 pathspec**(`git commit <files>`),禁止裸 `git commit`(会卷进别人 staged 的改动)。
- **Commit 步骤受用户门控**:仅当用户明确要求提交时执行 Commit 步骤;否则把改动留在工作树并报告。
- **测试命令**:聚焦用 `cd frontend && pnpm test -- src/components/agentre/orchestration/__tests__/<file>`;收尾用 `make test-frontend` + `make lint`(lint 会先 `generate`)。注意 `make … | tail` 会吞 make 退出码,看真 exit code。

---

## File Structure

- `frontend/src/components/agentre/orchestration/graph-data.ts` — 数据层。`GraphNode` 扩展 + `buildGraph` 计算 `callCount/isTopLevel/calls`。**唯一消费者是 `structure-graph.tsx` + 其测试**(已确认无其它 import),故可安全把 `GraphNode.tasks` 换成 `calls`。
- `frontend/src/components/agentre/orchestration/__tests__/graph-data.test.ts` — 数据层单测,既有断言 `.tasks` 改 `.calls` + 新增模型断言。
- `frontend/src/components/agentre/orchestration/structure-graph.tsx` — 视图层。`NodeCard` 三态渲染。
- `frontend/src/components/agentre/orchestration/__tests__/structure-graph.test.tsx` — 视图层单测,新增 ×N / 分组容器断言。
- `frontend/src/i18n/locales/{zh-CN,en}/common.json` — `orchestration.graph.callCount/sessionsCount/callLabel` 三个新键。

---

## Task 1: `graph-data.ts` — 节点模型扩展(callCount + isTopLevel + calls)

**Files:**
- Modify: `frontend/src/components/agentre/orchestration/graph-data.ts:11-26`(类型)、`:28-37`(aggregate)、`:39-87`(buildGraph)
- Test: `frontend/src/components/agentre/orchestration/__tests__/graph-data.test.ts`

**Interfaces:**
- Produces(后续 Task 2 / S5 / S6 依赖这些精确名字与类型):
  ```ts
  export interface GraphCall {
    taskId: number;
    sessionId: number;
    callSeq: number;
    brief: string;
    status: NodeStatus;
  }
  export interface GraphNode {
    agentId: number;
    isLeader: boolean;
    isTopLevel: boolean;   // 其某个 task 的父 task.agentId === leaderAgentId
    status: NodeStatus;    // 对 calls.status 聚合
    callCount: number;     // 该 agent 的派发调用数(= ×N)
    calls: GraphCall[];    // 按 callSeq 升序;per-call 投影
  }
  // GraphEdge / TreeStats / buildGraph / lifecycle 签名不变
  ```
- `GraphNode.tasks` 字段**删除**(替换为 `calls`)。

- [ ] **Step 1: 写失败测试 — 非顶层 subagent 多调用合并为单节点 callCount=N**

在 `graph-data.test.ts` 的 `describe("graph-data", …)` 内追加:

```ts
it("非顶层 subagent 多次 dispatch → 单节点, callCount=N, isTopLevel=false", () => {
  // Leader(2) → 后端(3) → 验签助手(4) 被 dispatch 两次
  const g = buildGraph(
    detail([
      { id: 1, agentId: 2, parentTaskId: 0, status: "running" }, // Leader 根
      { id: 2, agentId: 3, parentTaskId: 1, status: "running" }, // 后端(顶层)
      { id: 3, agentId: 4, parentTaskId: 2, status: "running", callSeq: 1 }, // 验签助手 #1
      { id: 4, agentId: 4, parentTaskId: 2, status: "done", callSeq: 2 }, // 验签助手 #2
    ]),
  );
  const helper = g.nodes.find((n) => n.agentId === 4)!;
  expect(helper.callCount).toBe(2); // ×2 合并
  expect(helper.isTopLevel).toBe(false); // 父是 后端(3), 不是 Leader
  expect(helper.calls).toHaveLength(2);
});
```

- [ ] **Step 2: 跑测试,确认因正确原因失败**

Run: `cd frontend && pnpm test -- src/components/agentre/orchestration/__tests__/graph-data.test.ts`
Expected: FAIL — `callCount`/`isTopLevel`/`calls` 在 `GraphNode` 上不存在(或 undefined),断言不通过。

- [ ] **Step 3: 写失败测试 — 顶层 agent 多对话 isTopLevel=true 且 calls 携带 sessionId/callSeq**

继续追加:

```ts
it("顶层 agent(父=Leader) 多次 dispatch → isTopLevel=true, calls 携带 sessionId/callSeq/brief", () => {
  // Leader(2) → 前端(3) 派发两次不同对话
  const g = buildGraph(
    detail([
      { id: 1, agentId: 2, parentTaskId: 0, status: "running" },
      {
        id: 2, agentId: 3, parentTaskId: 1, status: "running",
        callSeq: 1, sessionId: 501, brief: "支付表单",
      },
      {
        id: 3, agentId: 3, parentTaskId: 1, status: "done",
        callSeq: 2, sessionId: 502, brief: "退款流程",
      },
    ]),
  );
  const fe = g.nodes.find((n) => n.agentId === 3)!;
  expect(fe.isTopLevel).toBe(true);
  expect(fe.callCount).toBe(2);
  // calls 按 callSeq 升序, 携带 per-call 身份
  expect(fe.calls.map((c) => c.sessionId)).toEqual([501, 502]);
  expect(fe.calls.map((c) => c.callSeq)).toEqual([1, 2]);
  expect(fe.calls[0].brief).toBe("支付表单");
  expect(fe.calls[0].status).toBe("running");
  expect(fe.calls[1].status).toBe("done");
});
```

- [ ] **Step 4: 跑测试,确认失败**

Run: `cd frontend && pnpm test -- src/components/agentre/orchestration/__tests__/graph-data.test.ts`
Expected: FAIL — 同上,新字段缺失。

- [ ] **Step 5: 重写 `graph-data.ts` 实现新模型**

把 `:4-37` 的类型 + `aggregate` 与 `:39-87` 的 `buildGraph` 整体替换为:

```ts
import type { app } from "../../../../wailsjs/go/models";

export type TaskLite = app.TaskDTO;
export type NodeStatus =
  | "running"
  | "waiting"
  | "waiting-user"
  | "done"
  | "error"
  | "idle";

export interface GraphCall {
  taskId: number;
  sessionId: number;
  callSeq: number;
  brief: string;
  status: NodeStatus;
}
export interface GraphNode {
  agentId: number;
  isLeader: boolean;
  isTopLevel: boolean;
  status: NodeStatus;
  callCount: number;
  calls: GraphCall[];
}
export interface GraphEdge {
  from: number;
  to: number;
  kind: "dispatch" | "report";
}
export interface TreeStats {
  nodes: number;
  subagents: number;
  depth: number;
}

// 单个 task 状态 → NodeStatus(per-call)。
function taskStatus(status: string): NodeStatus {
  switch (status) {
    case "awaiting-user":
      return "waiting-user";
    case "error":
      return "error";
    case "running":
      return "running";
    case "awaiting-children":
      return "waiting";
    case "done":
      return "done";
    default:
      return "idle";
  }
}

// 聚合多个 per-call 状态成节点状态。优先级:
// 等待你(awaiting-user)优先于 error——等待你是唯一真正阻塞用户的状态,
// 不能被技术崩溃(会回报父 agent 重派)掩盖。
function aggregate(statuses: NodeStatus[]): NodeStatus {
  const s = new Set(statuses);
  if (s.has("waiting-user")) return "waiting-user";
  if (s.has("error")) return "error";
  if (s.has("running")) return "running";
  if (s.has("waiting")) return "waiting";
  if (statuses.length && statuses.every((x) => x === "done")) return "done";
  return "idle";
}

export function buildGraph(detail: app.RunDetailDTO): {
  nodes: GraphNode[];
  edges: GraphEdge[];
  stats: TreeStats;
} {
  const tasks = detail.tasks ?? [];
  const leaderAgent = detail.run?.leaderAgentId;
  const taskById = new Map(tasks.map((t) => [t.id, t]));

  // 按 agent 聚合(agent-as-node):同 subagent 多调用 = 同一节点。
  const byAgent = new Map<number, TaskLite[]>();
  for (const t of tasks) {
    if (!byAgent.has(t.agentId)) byAgent.set(t.agentId, []);
    byAgent.get(t.agentId)!.push(t);
  }

  const nodes: GraphNode[] = [...byAgent.entries()].map(([agentId, ts]) => {
    const calls: GraphCall[] = ts
      .map((t) => ({
        taskId: t.id,
        sessionId: t.sessionId,
        callSeq: t.callSeq,
        brief: t.brief,
        status: taskStatus(t.status),
      }))
      .sort((a, b) => a.callSeq - b.callSeq || a.taskId - b.taskId);
    // 顶层 = 其某个 task 的父 task 由 Leader 派发(父 task.agentId === leaderAgentId)。
    // 用「父 task 的 agent」判定,不依赖 rootTaskId 是否填充,既有测试夹具(无 rootTaskId)也成立。
    const isTopLevel = ts.some(
      (t) => taskById.get(t.parentTaskId)?.agentId === leaderAgent,
    );
    return {
      agentId,
      isLeader: agentId === leaderAgent,
      isTopLevel,
      status: aggregate(calls.map((c) => c.status)),
      callCount: calls.length,
      calls,
    };
  });

  // dispatch 边:子任务的 parent agent → 子任务 agent(去重到 agent 级)。
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

  // 树深:沿 parentTaskId 最长链。
  const depthOf = (id: number, guard = 0): number => {
    const t = taskById.get(id);
    if (!t || !t.parentTaskId || guard > 64) return 0;
    return 1 + depthOf(t.parentTaskId, guard + 1);
  };
  const depth = tasks.reduce((m, t) => Math.max(m, depthOf(t.id)), 0);

  // subagents = 唯一子agent 节点数(排除 Leader),与 runHeader「子agent M」标签一致。
  return {
    nodes,
    edges,
    stats: {
      nodes: nodes.length,
      subagents: Math.max(0, nodes.length - 1),
      depth,
    },
  };
}

export function lifecycle(
  detail: app.RunDetailDTO,
): "empty" | "running" | "completed" | "paused" | "stopped" {
  const st = detail.run?.status;
  if (st === "done") return "completed";
  if (st === "paused") return "paused";
  if (st === "stopped") return "stopped";
  const tasks = detail.tasks ?? [];
  if (tasks.length <= 1) return "empty";
  return "running";
}
```

- [ ] **Step 6: 更新既有断言 `.tasks` → `.calls`(同文件,在范围内)**

`graph-data.test.ts` 既有用例 `"节点按 agent 聚合, 边来自 dispatch 父子"`:把
```ts
    expect(a3.tasks).toHaveLength(2); // 卡内两任务行
```
改为
```ts
    expect(a3.calls).toHaveLength(2); // 同 agent 两次调用合并进一节点
    expect(a3.callCount).toBe(2);
```
该 `describe` 内其余用例(`awaiting-user`/`error`/`subagents`/`depth`/`lifecycle`/容错)断言的是 `.status`/`.stats`/`lifecycle`,**无需改**。全文搜索确认没有其它 `.tasks` 残留。

- [ ] **Step 7: 跑数据层测试,全绿**

Run: `cd frontend && pnpm test -- src/components/agentre/orchestration/__tests__/graph-data.test.ts`
Expected: PASS(含新增 2 个 + 既有全部)。

- [ ] **Step 8: (门控)提交**

```bash
git commit frontend/src/components/agentre/orchestration/graph-data.ts \
  frontend/src/components/agentre/orchestration/__tests__/graph-data.test.ts \
  -m "✨ orch graph-data:节点模型加 callCount/isTopLevel/calls(×N 合并 + 顶层标记)"
```

---

## Task 2: `structure-graph.tsx` — ×N 合并徽标 + 顶层 agent 分组容器

**Files:**
- Modify: `frontend/src/components/agentre/orchestration/structure-graph.tsx:30-108`(`NodeCard`)、`:1-12`(imports)
- Test: `frontend/src/components/agentre/orchestration/__tests__/structure-graph.test.tsx`

**Interfaces:**
- Consumes: `GraphNode`(含 `callCount/isTopLevel/calls`,Task 1 产出)、`GraphCall`。
- 渲染契约(Task 3 i18n + 后续 S6 依赖这些 testid):
  - 节点根:`data-testid="node-{agentId}"`(不变,整卡仍 `onClick → onSelectNode(agentId)`)。
  - 合并徽标:`data-testid="node-{agentId}-multi"`,文案 `t("orchestration.graph.callCount", { count })`(=`×N`)。**非顶层且 callCount≥2** 时出现。
  - 分组子行:`data-testid="node-{agentId}-call-{taskId}"`。**顶层且 callCount≥2** 时,每个 call 一行。
  - 头部计数后缀:顶层分组时名字后接 `t("orchestration.graph.sessionsCount", { count })`(=`N 会话`)。

- [ ] **Step 1: 写失败测试 — 非顶层 subagent 多调用渲染 ×N 合并徽标、不列子行**

`structure-graph.test.tsx` 的 `describe("StructureGraph", …)` 内追加:

```ts
it("非顶层 subagent 多次调用 → node 显示 ×N 合并徽标, 不逐条列 call 子行", () => {
  const detail = makeDetail({
    runStatus: "running",
    tasks: [
      makeTask(1, 2, "running", 0, 0), // Leader
      makeTask(2, 3, "running", 1, 0), // 后端(顶层)
      makeTask(3, 4, "running", 2, 410), // 验签助手 #1, 父=后端(3)
      makeTask(4, 4, "done", 2, 411), // 验签助手 #2
    ],
  });

  render(<StructureGraph detail={detail} onSelectNode={vi.fn()} />);

  // 合并徽标可见、文案含 ×2
  expect(screen.getByTestId("node-4-multi")).toHaveTextContent("×2");
  // 合并节点不列 per-call 子行(那是任务板的事)
  expect(screen.queryByTestId("node-4-call-3")).not.toBeInTheDocument();
  expect(screen.queryByTestId("node-4-call-4")).not.toBeInTheDocument();
});
```

> 说明:`makeTask(id, agentId, status, parentTaskId, sessionId)` 已是既有工厂签名(默认 `kind:"dispatch"`、`callSeq:0`),`×N` 看的是该 agent 的 task 条数,callSeq 为 0 不影响计数。

- [ ] **Step 2: 跑测试,确认因正确原因失败**

Run: `cd frontend && pnpm test -- src/components/agentre/orchestration/__tests__/structure-graph.test.tsx`
Expected: FAIL — `node-4-multi` 不存在(当前 `NodeCard` 把每个 task 渲成 `<li>` 子行、无合并徽标)。

- [ ] **Step 3: 写失败测试 — 顶层 agent 多对话渲染分组容器 + 逐条会话子行**

继续追加:

```ts
it("顶层 agent 多次对话 → 分组容器, 头部 N 会话 + 每次调用一条子行", () => {
  const detail = makeDetail({
    runStatus: "running",
    tasks: [
      makeTask(1, 2, "running", 0, 0), // Leader
      makeTask(2, 3, "running", 1, 501), // 前端 会话#1, 父=Leader 根(1)
      makeTask(3, 3, "done", 1, 502), // 前端 会话#2
    ],
  });

  render(<StructureGraph detail={detail} onSelectNode={vi.fn()} />);

  // 头部「2 会话」分组标记(i18n 未 mock → t() 回显 key, 用 count 插值断言)
  expect(screen.getByTestId("node-3")).toHaveTextContent("2");
  // 两条 per-call 子行
  expect(screen.getByTestId("node-3-call-2")).toBeInTheDocument();
  expect(screen.getByTestId("node-3-call-3")).toBeInTheDocument();
  // 顶层分组不显示 ×N 合并徽标(分组 ≠ 合并)
  expect(screen.queryByTestId("node-3-multi")).not.toBeInTheDocument();
});
```

> 注:本测试文件未 mock `react-i18next`,`t(key,{count})` 回显 `key`(不插值)。故断言 `node-3-call-{taskId}` 这类 **testid**,不断言中文文案;`toHaveTextContent("2")` 命中的是 callSeq/序号渲染(见 Step 4 用 `{call.callSeq || i+1}` 直出数字),稳。

- [ ] **Step 4: 跑测试,确认失败**

Run: `cd frontend && pnpm test -- src/components/agentre/orchestration/__tests__/structure-graph.test.tsx`
Expected: FAIL — `node-3-call-2` 不存在。

- [ ] **Step 5: 重写 `NodeCard`(structure-graph.tsx)实现三态**

更新 `:1-12` 的 import,加 `GraphCall` 与 `StatusDot` 已有;把 `:30-108` 的 `NodeCard` 整体替换为:

```tsx
// 单个 agent 节点卡片:三态
//  ① callCount<=1            → 单行 brief(现状)
//  ② !isTopLevel && >=2      → 合并 ×N 徽标, 不列子行(子代理保持图干净)
//  ③ isTopLevel && >=2       → 分组容器: 头部「N 会话」+ 每次调用一条只读子行
function NodeCard({
  node,
  agentName,
  agentColor,
  agentAvatarIcon,
  agentAvatarDataUrl,
  hasDeadlock,
  leaderLabel,
  onClick,
}: {
  node: GraphNode;
  agentName: string;
  agentColor: AgentColor;
  agentAvatarIcon?: string;
  agentAvatarDataUrl?: string;
  hasDeadlock: boolean;
  leaderLabel: string;
  onClick: () => void;
}) {
  const { t } = useTranslation();
  const isMerged = !node.isLeader && !node.isTopLevel && node.callCount >= 2;
  const isGroup = node.isTopLevel && node.callCount >= 2;

  const callDot = (status: GraphCall["status"]) =>
    status === "waiting-user" || status === "waiting"
      ? "waiting"
      : (status as AgentStatus) in { running: 1, idle: 1, error: 1, done: 1 }
        ? (status === "done"
            ? "idle"
            : (status as AgentStatus))
        : "idle";

  return (
    <button
      type="button"
      data-testid={`node-${node.agentId}`}
      onClick={onClick}
      className={cn(
        "flex w-52 cursor-pointer flex-col gap-2 rounded-lg border-2 bg-card p-3 text-left shadow-sm transition-shadow hover:shadow-md",
        hasDeadlock
          ? "border-destructive ring-2 ring-destructive/40"
          : nodeBorderClass(node.status),
      )}
    >
      {/* 头部: 头像 + 名称 + (N 会话 / 皇冠 / ×N) */}
      <div className="flex items-center gap-2">
        <AgentAvatar
          name={agentName}
          color={agentColor}
          size="sm"
          avatarIcon={agentAvatarIcon}
          avatarDataUrl={agentAvatarDataUrl}
        />
        <span className="flex-1 truncate text-sm font-medium text-foreground">
          {agentName}
          {isGroup && (
            <span className="ml-1 text-xs font-normal text-muted-foreground">
              · {t("orchestration.graph.sessionsCount", { count: node.callCount })}
            </span>
          )}
        </span>
        {isMerged && (
          <span
            data-testid={`node-${node.agentId}-multi`}
            className="shrink-0 rounded bg-muted px-1.5 py-0.5 text-xs font-medium text-muted-foreground"
          >
            {t("orchestration.graph.callCount", { count: node.callCount })}
          </span>
        )}
        {node.isLeader && (
          <Crown
            aria-label={leaderLabel}
            className="size-3.5 shrink-0 text-status-waiting"
          />
        )}
      </div>

      {/* 分组容器:每次调用一条只读子行(钻入留给 S6) */}
      {isGroup ? (
        <ul className="flex flex-col gap-1">
          {node.calls.map((call, i) => (
            <li
              key={call.taskId}
              data-testid={`node-${node.agentId}-call-${call.taskId}`}
              className="flex items-center gap-1.5"
            >
              <StatusDot status={callDot(call.status)} size="xs" />
              <span className="shrink-0 text-xs tabular-nums text-subtle-foreground">
                {t("orchestration.graph.callLabel", {
                  seq: call.callSeq || i + 1,
                })}
              </span>
              <span className="truncate text-xs text-muted-foreground">
                {call.brief || `#${call.taskId}`}
              </span>
            </li>
          ))}
        </ul>
      ) : (
        // 单次调用:单行 brief(合并 ×N 节点不列子行)
        !isMerged &&
        node.calls.length > 0 && (
          <ul className="flex flex-col gap-1">
            {node.calls.map((call) => (
              <li key={call.taskId} className="flex items-center gap-1.5">
                <StatusDot status={callDot(call.status)} size="xs" />
                <span className="truncate text-xs text-muted-foreground">
                  {call.brief || `#${call.taskId}`}
                </span>
              </li>
            ))}
          </ul>
        )
      )}
    </button>
  );
}
```

并把 `:12` 的 import 改为同时带 `GraphCall`:
```ts
import type { GraphCall, GraphNode, NodeStatus } from "./graph-data";
```

> 说明:`callDot` 把 `done` 折成 `idle`(灰)、`waiting-user`/`waiting` 折成 `waiting`(琥珀),与原 `StatusDot` 的有限 `AgentStatus` 域对齐,避免传入非法状态。`tabular-nums` 让 `会话#1/#2` 对齐。

- [ ] **Step 6: 跑视图层测试,全绿**

Run: `cd frontend && pnpm test -- src/components/agentre/orchestration/__tests__/structure-graph.test.tsx`
Expected: PASS(新增 2 个 + 既有 4 个:completed banner / deadlock ring / empty / 点击 onSelectNode)。

> 既有「点击节点 node-3 调 onSelectNode(3)」用例:其 detail 是 Leader(2)+子(3) 各一 task → 子(3) `callCount=1`,走单行分支,整卡 onClick 不变 → 仍通过。

- [ ] **Step 7: (门控)提交**

```bash
git commit frontend/src/components/agentre/orchestration/structure-graph.tsx \
  frontend/src/components/agentre/orchestration/__tests__/structure-graph.test.tsx \
  -m "✨ orch 结构图:子代理多调用合并 ×N + 顶层 agent 多对话分组容器"
```

---

## Task 3: i18n 三键 + 全量校验

**Files:**
- Modify: `frontend/src/i18n/locales/zh-CN/common.json`(`orchestration.graph`)
- Modify: `frontend/src/i18n/locales/en/common.json`(`orchestration.graph`)

**Interfaces:**
- Consumes: Task 2 用到的三个键 `orchestration.graph.callCount/sessionsCount/callLabel`。

- [ ] **Step 1: 写失败测试 — i18n 静态键 + 双语覆盖**

`frontend/src/__tests__/i18n.test.ts` 已自动校验 `t("...")` 静态键存在且 zh/en 对齐。先确认它会因缺键失败:

Run: `cd frontend && pnpm test -- src/__tests__/i18n.test.ts`
Expected: FAIL — `orchestration.graph.callCount` / `sessionsCount` / `callLabel` 未定义(static key 扫描报缺失)。

> 若该测试不是静态扫描而无法直接捕获,改为依赖 Task 2 的组件测试作为 Red:Task 2 测试在缺键时虽不崩(t 回显 key),但 `toHaveTextContent("×2")` 会失败——以 Task 2 Step 6 为该键的真实 Red。

- [ ] **Step 2: 加 zh-CN 键**

`zh-CN/common.json` 的 `orchestration.graph` 对象内(已有 `leaderCrown` 等),追加:

```json
    "callCount": "×{{count}}",
    "sessionsCount": "{{count}} 会话",
    "callLabel": "会话#{{seq}}"
```

- [ ] **Step 3: 加 en 键(同结构)**

`en/common.json` 的 `orchestration.graph` 内追加:

```json
    "callCount": "×{{count}}",
    "sessionsCount": "{{count}} sessions",
    "callLabel": "Session #{{seq}}"
```

- [ ] **Step 4: 跑 i18n + 两个组件测试,全绿**

Run: `cd frontend && pnpm test -- src/__tests__/i18n.test.ts src/components/agentre/orchestration/__tests__/graph-data.test.ts src/components/agentre/orchestration/__tests__/structure-graph.test.tsx`
Expected: PASS。

- [ ] **Step 5: 全量前端校验 + lint(看真 exit code)**

```bash
cd frontend && pnpm test
```
Expected: 全绿(确认没改坏 index/feed/run-header 等同目录测试)。

```bash
cd /Users/codfrm/Code/agentre/agentre && make lint
```
Expected: 0 error(重点:`i18next/no-literal-string` 不报 structure-graph,`tsc` 不报 GraphNode 类型,prettier/eslint 干净)。

- [ ] **Step 6: (门控)提交**

```bash
git commit frontend/src/i18n/locales/zh-CN/common.json \
  frontend/src/i18n/locales/en/common.json \
  -m "🌐 orch 结构图:×N/N 会话/会话# i18n 双语键"
```

---

## Final verification (after all tasks)

- [ ] `cd frontend && pnpm test -- src/components/agentre/orchestration` — 编排目录全绿。
- [ ] `make test-frontend` — wails generate + 全量 vitest 绿。
- [ ] `make lint` — golangci-lint(无 go 改动应秒过)+ 前端 eslint/tsc/prettier 绿。
- [ ] 人工对照设计稿 `iBqBl`/`r0NpeW`(结构图主屏):验签助手 `×2` 合并、前端「· 2 会话」分组 + 两条子行、Leader 皇冠、死锁红框仍在。**真机 GUI 验证**:本切片落地后建议起 `make dev` 跑一个有「同 agent 多调用」的 Run 目检(可留到 S7 搬家后连同独立页一起验)。

## Self-review notes(写计划时已核对)

1. **Spec coverage(§9 最终模型)**:
   - 「同 subagent 多调用合并 ×N」→ Task 2 `isMerged` 分支 + Task 1 `callCount`。✅
   - 「顶层 agent 多对话分组容器(N 会话)」→ Task 2 `isGroup` 分支 + Task 1 `isTopLevel`/`calls`。✅
   - 「per-call 明细(独立 session/钻入)落任务板」→ `calls[]` 已携带 `sessionId/callSeq`,供 **S5/S6** 消费;本切片图上分组子行只读、不钻入。✅(划界清楚)
   - 「`send` 续做不新增节点」→ `send` 不创建 task(后端确认仅 `TaskKindDispatch` 建 task),按 agent 聚合天然成立,无需特判。✅
   - 「边按 parentTaskId 任意深度 / 非-Leader 派发」→ 边逻辑原样保留(已支持)。✅
2. **明确不在本切片(防 scope 蔓延)**:`+N 子代理` CLI 徽标=S5b;per-call 钻入=S6;任务板 5/12=S5。三处在 Global Constraints + Interfaces 标注。
3. **Placeholder 扫描**:无 TODO/TBD;每个 code step 给了完整代码。
4. **类型一致性**:`GraphNode.{agentId,isLeader,isTopLevel,status,callCount,calls}` 在 Task 1 定义、Task 2 消费,名字逐字对齐;`GraphCall.{taskId,sessionId,callSeq,brief,status}` 同。testid `node-{id}` / `node-{id}-multi` / `node-{id}-call-{taskId}` 在 Task 2 实现与测试一致。
5. **风险**:`GraphNode.tasks → calls` 是破坏性改名,已确认唯一消费者是 `structure-graph.tsx` + 其测试(无其它 import),范围内可控。i18n 测试若非静态扫描,Red 以 Task 2 组件测试兜底(Step 1 已说明)。
6. **未决(留给 review)**:顶层分组「竖排子行」在窄主区可能与设计稿的横向布局有出入(spec §5.7a 自承横向裁剪未解);本切片按竖排子行落地(最稳),布局精修可在 S7 搬进独立页有更宽主区后再调。
