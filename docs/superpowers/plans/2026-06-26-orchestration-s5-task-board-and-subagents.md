# 编排 S5/S5b — 任务板计数+分组 & CLI 子代理(零后端 derive)Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把任务板对齐设计稿 §51:① 头部 `done/total` 计数(只数 orch 派发 call);② 按 agent 分组,同 agent 的多次派发调用作为缩进 `#1/#2` per-call 子行(可点,钻入留 S6);③(S5b)在父 call 行下挂**默认折叠**的 `▸ +N 子代理` 只读子行,并在结构图父节点挂 `+N 子代理` 合并徽标——子代理数据**纯前端从 session transcript derive `tool_use.subagent`(kind==="local_agent")**,零后端。

**Architecture:** S5 全部从 `detail.tasks` 现成数据算,改 `task-board.tsx` 一个文件。S5b 新增一条只读数据链:`subagent-data.ts`(纯 derive 函数)+ `orch-subagents-store.ts`(按 `sessionId` 懒加载 `LoadChatSession` 并缓存 derive 结果)+ `use-run-subagents.ts`(hook:遍历 run 的 task sessionId 触发加载,给出 `forSession`/`countForAgent`),由 `task-board.tsx`(折叠子行)与 `structure-graph.tsx`(节点 `+N` 徽标,**接 S4 的 NodeCard**)共同消费。

**Tech Stack:** React 19 + TypeScript + zustand + Tailwind v4 + Vitest + Testing Library + react-i18next。

## ⚠️ 落地前必读:S5b 数据流权衡(留给 review 的拍板点)

`+N 子代理` 的计数必须在**折叠态就可见**(设计稿 `▸ +8 子代理`)+ 结构图徽标也要 —— 即**不展开也得知道 N**。N 只能从该 task 的 session transcript 数 `tool_use.subagent` 帧得到,故必须 `LoadChatSession(sessionId)`。

- **本 plan 采用的默认方案:懒加载 + 缓存。** 打开 Run 时 `use-run-subagents` 对每个 task 的 `sessionId` 触发一次 `LoadChatSession`,结果按 `sessionId` 缓存在 `orch-subagents-store`;计数**异步出现**(加载完成才显 `+N`,加载中不显)。读多写零、不碰 `orch_svc`,真零后端。
- **代价(明确):** 一个 N-task 的 Run 打开时会发 N 次 `LoadChatSession`(各拉一份完整 transcript)。典型 Run(≤12 task)可接受;**超大 Run 会偏重**。
- **若你认为代价不可接受**:更稳的是给 `TaskDTO` 加一个 `subagentCount`(后端 O(1) 数一下),前端直接读——**但这违背 spec「零后端」**。这是个**取舍**,本 plan 先按零后端懒加载落地,review 时你可改判走小后端。

> 余下两个小 review 点见末尾 Self-review。

## Global Constraints

- **严格 TDD:Red → Green → Refactor。** 每 Task 先写失败测试 → 跑看正确失败 → 最小实现 → 跑过 →(门控)提交。
- **依赖 S4 已落地。** S5b 的结构图 `+N` 徽标挂在 **S4 重写后的 `NodeCard`** 上(`GraphNode.calls`/`isTopLevel`/`isMerged` 渲染已就位);执行顺序 S4 → 本 plan,本 plan 的 `structure-graph.tsx` 片段以 S4 后的代码为基线。
- **只动本切片文件**:`task-board.tsx` / 新增 `subagent-data.ts` / `orch-subagents-store.ts` / `use-run-subagents.ts` / `structure-graph.tsx`(仅加徽标)/ 两个 `common.json` + 对应测试。**禁止** drive-by 改 graph-data/feed/run-header/index/run-list。
- **本切片不做(划界):** per-call **钻入**(点 `#1` 进该 session)= **S6**——本切片 per-call 子行点击仍走 `onSelectTask(agentId)`,折叠子行只读不可钻入;`5/12` 是否排除 Leader 根 task 见 Self-review 复核。
- **i18n:** 新文案走 `t(...)` + 双语 `common.json`;新键挂 `orchestration.board.*`(已存在)与新增 `orchestration.subagent.*`。`i18next/no-literal-string` 拦硬编码中文。
- **共享分支 develop/wyz**:提交**永远带 pathspec**(`git commit <files>`),禁止裸 `git commit`。**Commit 步骤受用户门控**。
- **store 测试隔离**:新 store 提供 `__reset()`,测试 `beforeEach` 调用(对齐 `orch-run-store`);组件测试 per-file `vi.mock` wailsjs(`LoadChatSession`),勿加全局 alias。
- **测试命令**:聚焦 `cd frontend && pnpm test -- <path>`;收尾 `make test-frontend` + `make lint`(注意 `| tail` 吞 make 退出码)。

---

## File Structure

- `frontend/src/components/agentre/orchestration/task-board.tsx` — S5:头部计数 + agent 分组 per-call 子行;S5b:call 行下折叠 `▸ +N 子代理` 子行。
- `frontend/src/components/agentre/orchestration/subagent-data.ts` — **新**。纯函数 `deriveSubagents(messages)` → `SubagentLite[]`(只收 `kind==="local_agent"`)。无 React。
- `frontend/src/stores/orch-subagents-store.ts` — **新**。zustand:`bySession: Map<sessionId, SubagentLite[]>` + `ensureLoaded(sessionId)`(懒调 `LoadChatSession` + derive + 缓存)+ `__reset()`。
- `frontend/src/components/agentre/orchestration/use-run-subagents.ts` — **新**。hook:effect 里对 `detail.tasks` 的每个 `sessionId` `ensureLoaded`;返回 `{ forSession(sid), countForAgent(agentId) }`。
- `frontend/src/components/agentre/orchestration/structure-graph.tsx` — S5b:`NodeCard` 头部加 `+N 子代理` 徽标(接 S4)。
- `frontend/src/i18n/locales/{zh-CN,en}/common.json` — `orchestration.board.progress` + `orchestration.subagent.{badge,autoMerge,noTask,expand,collapse}`。
- 测试:`__tests__/task-board.test.tsx`、新增 `__tests__/subagent-data.test.ts`、`stores/__tests__/orch-subagents-store.test.ts`(或就近)、`__tests__/structure-graph.test.tsx`。

---

## Task 1: 任务板头部 `done/total` 计数徽标(S5)

**Files:**
- Modify: `task-board.tsx:139-161`(在 tab 控件行旁/上方插入计数)
- Test: `__tests__/task-board.test.tsx`

**Interfaces:**
- Produces: testid `board-progress`,文案 `t("orchestration.board.progress", { done, total })`。`total = tasks.length`,`done = tasks.filter(status==="done").length`。

- [ ] **Step 1: 写失败测试**

```ts
it("头部 board-progress 显示 done/total(完成数/任务数)", () => {
  const tasks = [
    makeTask(1, 2, { status: "done" }),
    makeTask(2, 3, { status: "done", parentTaskId: 1 }),
    makeTask(3, 3, { status: "running", parentTaskId: 1 }),
  ];
  render(
    <TaskBoard detail={makeDetail(tasks)} selectedAgentId={null} onSelectTask={vi.fn()} />,
  );
  expect(screen.getByTestId("board-progress")).toHaveTextContent("2");
  expect(screen.getByTestId("board-progress")).toHaveTextContent("3");
});
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd frontend && pnpm test -- src/components/agentre/orchestration/__tests__/task-board.test.tsx`
Expected: FAIL — `board-progress` 不存在。

- [ ] **Step 3: 实现计数**

在 `TaskBoard` body 内、`return` 前加:
```tsx
  const doneCount = React.useMemo(
    () => tasks.filter((tk) => tk.status === "done").length,
    [tasks],
  );
```
把 `:142` 的 tab 控件容器 `<div className="flex shrink-0 items-center gap-1 border-b border-border p-2">` 内,在两个 Button **之前**插入计数徽标:
```tsx
        <span
          data-testid="board-progress"
          className="mr-1 shrink-0 rounded bg-muted px-1.5 py-0.5 text-xs font-medium text-muted-foreground tabular-nums"
        >
          {t("orchestration.board.progress", {
            done: doneCount,
            total: tasks.length,
          })}
        </span>
```

- [ ] **Step 4: 加 i18n 键(zh + en)**

`zh-CN` `orchestration.board` 内加:`"progress": "{{done}}/{{total}} 任务"`;`en` 内加:`"progress": "{{done}}/{{total}} tasks"`。

- [ ] **Step 5: 跑测试通过**

Run: `cd frontend && pnpm test -- src/components/agentre/orchestration/__tests__/task-board.test.tsx src/__tests__/i18n.test.ts`
Expected: PASS。

- [ ] **Step 6:(门控)提交**

```bash
git commit frontend/src/components/agentre/orchestration/task-board.tsx \
  frontend/src/components/agentre/orchestration/__tests__/task-board.test.tsx \
  frontend/src/i18n/locales/zh-CN/common.json frontend/src/i18n/locales/en/common.json \
  -m "✨ orch 任务板:头部 done/total 计数徽标"
```

---

## Task 2: 任务板按 agent 分组 + 多调用 per-call 子行(S5)

**Files:**
- Modify: `task-board.tsx:181-226`(任务清单 `<ul>` 整段)
- Test: `__tests__/task-board.test.tsx`

**Interfaces:**
- Produces:
  - 单调用 agent:仍渲染 `board-task-{taskId}` 单行(向后兼容既有测试)。
  - 多调用 agent:渲染 agent 分组头 `board-agent-{agentId}`(名称 + `t("orchestration.graph.sessionsCount",{count})`),其下每 call 一行 `board-task-{taskId}`(缩进 + `#{callSeq}` + brief),点击仍 `onSelectTask(agentId)`。

- [ ] **Step 1: 写失败测试**

```ts
it("同 agent 多次调用 → 分组头 + 每次调用一条 per-call 行", () => {
  const tasks = [
    makeTask(1, 2, { status: "running" }), // Leader 单调用
    makeTask(2, 3, { status: "running", parentTaskId: 1, callSeq: 1, sessionId: 501 }),
    makeTask(3, 3, { status: "done", parentTaskId: 1, callSeq: 2, sessionId: 502 }),
  ];
  render(
    <TaskBoard detail={makeDetail(tasks)} selectedAgentId={null} onSelectTask={vi.fn()} />,
  );
  // agent 3 多调用 → 分组头 + 两条 per-call 行
  expect(screen.getByTestId("board-agent-3")).toBeInTheDocument();
  expect(screen.getByTestId("board-task-2")).toBeInTheDocument();
  expect(screen.getByTestId("board-task-3")).toBeInTheDocument();
  // agent 2 单调用 → 无分组头
  expect(screen.queryByTestId("board-agent-2")).not.toBeInTheDocument();
  expect(screen.getByTestId("board-task-1")).toBeInTheDocument();
});
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd frontend && pnpm test -- src/components/agentre/orchestration/__tests__/task-board.test.tsx`
Expected: FAIL — `board-agent-3` 不存在(当前是扁平列表)。

- [ ] **Step 3: 实现 agent 分组**

在 `TaskBoard` body 内加分组计算(保持 task 首次出现顺序):
```tsx
  // 按 agent 分组,保留 task 首次出现顺序;agent 内按 callSeq 升序。
  const agentGroups = React.useMemo(() => {
    const order: number[] = [];
    const byAgent = new Map<number, app.TaskDTO[]>();
    for (const tk of tasks) {
      if (!byAgent.has(tk.agentId)) {
        byAgent.set(tk.agentId, []);
        order.push(tk.agentId);
      }
      byAgent.get(tk.agentId)!.push(tk);
    }
    return order.map((agentId) => ({
      agentId,
      tasks: byAgent
        .get(agentId)!
        .slice()
        .sort((a, b) => a.callSeq - b.callSeq || a.id - b.id),
    }));
  }, [tasks]);
```
把 `:181-226` 的 `<ul>...{tasks.map(...)}...</ul>` 整段替换为按组渲染。单调用组渲染单行(沿用现有行的视觉:`#seq`/状态点/agentName/brief/`onSelectTask(agentId)`/`isSelected`);多调用组先渲染分组头,再渲染缩进 per-call 行:

```tsx
              <ul className="flex flex-col gap-0">
                {agentGroups.map((group) => {
                  const agentName =
                    agentNameMap.get(group.agentId) ?? `#${group.agentId}`;
                  const isSelected = group.agentId === selectedAgentId;
                  const multi = group.tasks.length >= 2;

                  const renderRow = (task: app.TaskDTO, indented: boolean) => {
                    const seq = task.callSeq > 0 ? task.callSeq : task.id;
                    return (
                      <li key={task.id}>
                        <button
                          type="button"
                          data-testid={`board-task-${task.id}`}
                          onClick={() => onSelectTask(task.agentId)}
                          className={cn(
                            "flex w-full items-center gap-2 px-3 py-2 text-left text-xs transition-colors hover:bg-muted/50",
                            indented && "pl-8",
                            isSelected && "bg-muted",
                          )}
                        >
                          <span className="shrink-0 text-muted-foreground/60">
                            #{seq}
                          </span>
                          <span
                            className={cn(
                              "h-1.5 w-1.5 shrink-0 rounded-full",
                              statusDotClass(task.status),
                            )}
                          />
                          {!indented && (
                            <span className="shrink-0 font-medium text-foreground">
                              {agentName}
                            </span>
                          )}
                          {task.brief && (
                            <span className="min-w-0 flex-1 truncate text-muted-foreground">
                              {task.brief}
                            </span>
                          )}
                        </button>
                      </li>
                    );
                  };

                  if (!multi) {
                    return renderRow(group.tasks[0], false);
                  }
                  return (
                    <li key={`agent-${group.agentId}`}>
                      <div
                        data-testid={`board-agent-${group.agentId}`}
                        className="flex items-center gap-2 px-3 pt-2 pb-1 text-xs font-medium text-foreground"
                      >
                        <span>{agentName}</span>
                        <span className="text-muted-foreground">
                          {t("orchestration.graph.sessionsCount", {
                            count: group.tasks.length,
                          })}
                        </span>
                      </div>
                      <ul className="flex flex-col gap-0">
                        {group.tasks.map((task) => renderRow(task, true))}
                      </ul>
                    </li>
                  );
                })}
              </ul>
```

> 复用 S4 引入的 `orchestration.graph.sessionsCount`(`N 会话`)做分组头计数,避免重复键。

- [ ] **Step 4: 跑测试通过(含既有「扁平」用例)**

Run: `cd frontend && pnpm test -- src/components/agentre/orchestration/__tests__/task-board.test.tsx`
Expected: PASS。既有「为每个任务渲染 board-task-${id}」用例(2 个不同 agent 各 1 task)→ 都是单调用组,仍渲染 `board-task-1/2`,通过。

- [ ] **Step 5:(门控)提交**

```bash
git commit frontend/src/components/agentre/orchestration/task-board.tsx \
  frontend/src/components/agentre/orchestration/__tests__/task-board.test.tsx \
  -m "✨ orch 任务板:按 agent 分组 + 多调用 per-call 缩进子行"
```

---

## Task 3: `deriveSubagents` 纯函数(S5b 数据层)

**Files:**
- Create: `frontend/src/components/agentre/orchestration/subagent-data.ts`
- Test: `frontend/src/components/agentre/orchestration/__tests__/subagent-data.test.ts`

**Interfaces:**
- Produces:
  ```ts
  export interface SubagentLite {
    toolUseId: string;
    role: string;       // subagentType || taskDescription
    description: string;
    status: "running" | "completed" | "failed";
  }
  export function deriveSubagents(messages: chat_svc.ChatMessage[]): SubagentLite[]
  ```
  只收 `block.type==="tool_use"` 且 `block.subagent.kind==="local_agent"`(CLI 子代理 / Task 工具);按 `toolUseId` dedupe。**排除** `local_bash`(那是后台 bash,归后台任务面板)。

- [ ] **Step 1: 写失败测试**

```ts
import { describe, it, expect } from "vitest";
import { deriveSubagents } from "../subagent-data";
import type { chat_svc } from "../../../../../wailsjs/go/models";

const msg = (blocks: unknown[]): chat_svc.ChatMessage =>
  ({ blocks } as unknown as chat_svc.ChatMessage);

describe("deriveSubagents", () => {
  it("只收 kind==='local_agent' 的 tool_use.subagent, 排除 local_bash", () => {
    const subs = deriveSubagents([
      msg([
        { type: "tool_use", toolUseId: "a", subagent: { kind: "local_agent", subagentType: "用例生成器", taskDescription: "写用例", status: "completed" } },
        { type: "tool_use", toolUseId: "b", subagent: { kind: "local_bash", status: "running" } }, // 后台 bash, 排除
        { type: "text", text: "hi" }, // 非 tool_use
      ]),
    ]);
    expect(subs).toHaveLength(1);
    expect(subs[0].toolUseId).toBe("a");
    expect(subs[0].role).toBe("用例生成器");
    expect(subs[0].status).toBe("completed");
  });

  it("按 toolUseId dedupe(后出现覆盖)", () => {
    const subs = deriveSubagents([
      msg([{ type: "tool_use", toolUseId: "x", subagent: { kind: "local_agent", status: "running" } }]),
      msg([{ type: "tool_use", toolUseId: "x", subagent: { kind: "local_agent", status: "completed" } }]),
    ]);
    expect(subs).toHaveLength(1);
    expect(subs[0].status).toBe("completed");
  });

  it("canceled 归 failed; 缺 status 归 running", () => {
    const subs = deriveSubagents([
      msg([
        { type: "tool_use", toolUseId: "c", subagent: { kind: "local_agent", status: "canceled" } },
        { type: "tool_use", toolUseId: "d", subagent: { kind: "local_agent" } },
      ]),
    ]);
    expect(subs.find((s) => s.toolUseId === "c")!.status).toBe("failed");
    expect(subs.find((s) => s.toolUseId === "d")!.status).toBe("running");
  });
});
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd frontend && pnpm test -- src/components/agentre/orchestration/__tests__/subagent-data.test.ts`
Expected: FAIL — 模块不存在。

- [ ] **Step 3: 实现 `subagent-data.ts`**

```ts
import type { chat_svc } from "../../../../wailsjs/go/models";

export interface SubagentLite {
  toolUseId: string;
  role: string;
  description: string;
  status: "running" | "completed" | "failed";
}

type VisitableBlock = {
  type?: string;
  toolUseId?: string;
  subagent?: chat_svc.ChatBlockSubagent;
};

// deriveSubagents 从一个 session 的历史消息里收 CLI 子代理(Task 工具)。
// 与后台任务面板的 deriveBackgroundTasks 对称:那边收 local_bash、排除 subagent;
// 这边收 local_agent(CLI 子代理)、排除 local_bash。按 toolUseId dedupe(后覆盖)。
export function deriveSubagents(
  messages: chat_svc.ChatMessage[],
): SubagentLite[] {
  const byId = new Map<string, SubagentLite>();
  for (const m of messages) {
    for (const b of m.blocks ?? []) {
      const block = b as unknown as VisitableBlock;
      if (block.type !== "tool_use") continue;
      const sa = block.subagent;
      const id = block.toolUseId;
      if (!sa || !id) continue;
      if (sa.kind !== "local_agent") continue;
      byId.set(id, {
        toolUseId: id,
        role: sa.subagentType || sa.taskDescription || "",
        description: sa.taskDescription || "",
        status: mapStatus(sa.status),
      });
    }
  }
  return [...byId.values()];
}

function mapStatus(raw: string | undefined): SubagentLite["status"] {
  if (raw === "completed") return "completed";
  if (raw === "failed" || raw === "canceled") return "failed";
  return "running";
}
```

- [ ] **Step 4: 跑测试通过**

Run: `cd frontend && pnpm test -- src/components/agentre/orchestration/__tests__/subagent-data.test.ts`
Expected: PASS。

- [ ] **Step 5:(门控)提交**

```bash
git commit frontend/src/components/agentre/orchestration/subagent-data.ts \
  frontend/src/components/agentre/orchestration/__tests__/subagent-data.test.ts \
  -m "✨ orch:deriveSubagents 从 transcript 收 CLI 子代理(local_agent)"
```

---

## Task 4: `orch-subagents-store` 懒加载缓存 + `useRunSubagents` hook(S5b 数据流)

**Files:**
- Create: `frontend/src/stores/orch-subagents-store.ts`
- Create: `frontend/src/components/agentre/orchestration/use-run-subagents.ts`
- Test: `frontend/src/stores/__tests__/orch-subagents-store.test.ts`

**Interfaces:**
- Produces:
  ```ts
  // store
  export const useOrchSubagentsStore: Store<{
    bySession: Map<number, SubagentLite[]>;
    loading: Set<number>;
    ensureLoaded: (sessionId: number) => void;
    __reset: () => void;
  }>;
  // hook
  export function useRunSubagents(detail: app.RunDetailDTO): {
    forSession: (sessionId: number) => SubagentLite[];
    countForAgent: (agentId: number) => number;
  };
  ```

- [ ] **Step 1: 写失败测试(store)**

```ts
import { describe, it, expect, vi, beforeEach } from "vitest";

const loadMock = vi.fn();
vi.mock("../../../wailsjs/go/app/App", () => ({
  LoadChatSession: (...a: unknown[]) => loadMock(...a),
}));

import { useOrchSubagentsStore } from "../orch-subagents-store";

beforeEach(() => {
  useOrchSubagentsStore.getState().__reset();
  loadMock.mockReset();
});

describe("orch-subagents-store", () => {
  it("ensureLoaded 调 LoadChatSession 并缓存 derive 结果", async () => {
    loadMock.mockResolvedValue({
      messages: [
        { blocks: [{ type: "tool_use", toolUseId: "a", subagent: { kind: "local_agent", status: "completed" } }] },
      ],
    });
    useOrchSubagentsStore.getState().ensureLoaded(501);
    // 等微任务队列 flush
    await vi.waitFor(() =>
      expect(useOrchSubagentsStore.getState().bySession.get(501)).toHaveLength(1),
    );
    expect(loadMock).toHaveBeenCalledTimes(1);
  });

  it("同 sessionId 不重复加载(已缓存或加载中)", async () => {
    loadMock.mockResolvedValue({ messages: [] });
    useOrchSubagentsStore.getState().ensureLoaded(7);
    useOrchSubagentsStore.getState().ensureLoaded(7);
    await vi.waitFor(() =>
      expect(useOrchSubagentsStore.getState().bySession.has(7)).toBe(true),
    );
    expect(loadMock).toHaveBeenCalledTimes(1);
  });

  it("sessionId=0 不加载", () => {
    useOrchSubagentsStore.getState().ensureLoaded(0);
    expect(loadMock).not.toHaveBeenCalled();
  });
});
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd frontend && pnpm test -- src/stores/__tests__/orch-subagents-store.test.ts`
Expected: FAIL — 模块不存在。

- [ ] **Step 3: 实现 store**

```ts
import { create } from "zustand";
import { LoadChatSession } from "../../wailsjs/go/app/App";
import {
  deriveSubagents,
  type SubagentLite,
} from "../components/agentre/orchestration/subagent-data";

type State = {
  bySession: Map<number, SubagentLite[]>;
  loading: Set<number>;
  ensureLoaded: (sessionId: number) => void;
  __reset: () => void;
};

// orch-subagents-store:按 sessionId 懒加载该 session 的 CLI 子代理。
// 计数需折叠态可见(设计稿 `+N 子代理`),只能 LoadChatSession 数 transcript。
// 读多写零、不碰 orch_svc;同 sessionId 只加载一次(in-flight 去重)。
export const useOrchSubagentsStore = create<State>((set, get) => ({
  bySession: new Map(),
  loading: new Set(),
  ensureLoaded: (sessionId) => {
    if (!sessionId) return;
    const { bySession, loading } = get();
    if (bySession.has(sessionId) || loading.has(sessionId)) return;
    set((s) => {
      const ld = new Set(s.loading);
      ld.add(sessionId);
      return { loading: ld };
    });
    void LoadChatSession({ sessionId } as never)
      .then((resp) => {
        const subs = deriveSubagents(resp?.messages ?? []);
        set((s) => {
          const next = new Map(s.bySession);
          next.set(sessionId, subs);
          const ld = new Set(s.loading);
          ld.delete(sessionId);
          return { bySession: next, loading: ld };
        });
      })
      .catch(() => {
        set((s) => {
          const ld = new Set(s.loading);
          ld.delete(sessionId);
          return { loading: ld };
        });
      });
  },
  __reset: () => set({ bySession: new Map(), loading: new Set() }),
}));
```

- [ ] **Step 4: 跑 store 测试通过**

Run: `cd frontend && pnpm test -- src/stores/__tests__/orch-subagents-store.test.ts`
Expected: PASS。

- [ ] **Step 5: 实现 `use-run-subagents.ts` hook(无独立单测,由 Task 5/6 组件测试覆盖)**

```ts
import * as React from "react";
import type { app } from "../../../../wailsjs/go/models";
import { useOrchSubagentsStore } from "../../../stores/orch-subagents-store";
import type { SubagentLite } from "./subagent-data";

// useRunSubagents:对 run 的每个 task session 触发懒加载,给任务板/结构图共享读。
export function useRunSubagents(detail: app.RunDetailDTO): {
  forSession: (sessionId: number) => SubagentLite[];
  countForAgent: (agentId: number) => number;
} {
  const tasks = React.useMemo(() => detail.tasks ?? [], [detail.tasks]);
  const bySession = useOrchSubagentsStore((s) => s.bySession);
  const ensureLoaded = useOrchSubagentsStore((s) => s.ensureLoaded);

  React.useEffect(() => {
    for (const t of tasks) {
      if (t.sessionId) ensureLoaded(t.sessionId);
    }
  }, [tasks, ensureLoaded]);

  return React.useMemo(() => {
    const forSession = (sessionId: number) => bySession.get(sessionId) ?? [];
    const countForAgent = (agentId: number) =>
      tasks
        .filter((t) => t.agentId === agentId)
        .reduce((n, t) => n + (bySession.get(t.sessionId)?.length ?? 0), 0);
    return { forSession, countForAgent };
  }, [bySession, tasks]);
}
```

- [ ] **Step 6:(门控)提交**

```bash
git commit frontend/src/stores/orch-subagents-store.ts \
  frontend/src/stores/__tests__/orch-subagents-store.test.ts \
  frontend/src/components/agentre/orchestration/use-run-subagents.ts \
  -m "✨ orch:子代理懒加载缓存 store + useRunSubagents hook(零后端)"
```

---

## Task 5: 任务板 call 行下「▸ +N 子代理」默认折叠子行(S5b)

**Files:**
- Modify: `task-board.tsx`(`renderRow` 内,行下挂折叠块;引入 `useRunSubagents`)
- Test: `__tests__/task-board.test.tsx`(补 `LoadChatSession` mock)

**Interfaces:**
- Consumes: `useRunSubagents(detail)`、`orch-subagents-store`(测试需 mock `LoadChatSession`)。
- Produces:`board-subagents-{taskId}`(折叠触发,文案 `+N 子代理`),展开后 `board-subagent-{taskId}-{i}` 只读子行。`N===0` 时不渲染。

- [ ] **Step 1: 给 task-board 测试补 `LoadChatSession` mock + store reset**

`task-board.test.tsx` 顶部 `vi.mock("../../../../../wailsjs/go/app/App", …)` 的对象里加一项:
```ts
  LoadChatSession: vi.fn().mockResolvedValue({
    messages: [
      { blocks: [{ type: "tool_use", toolUseId: "s1", subagent: { kind: "local_agent", subagentType: "用例生成器", status: "completed" } }] },
    ],
  }),
```
`beforeEach` 里加:`useOrchSubagentsStore.getState().__reset();`(import 之)。

- [ ] **Step 2: 写失败测试**

```ts
it("有 CLI 子代理的 call 行下挂折叠 board-subagents-{taskId}, 点开展开只读子行", async () => {
  const tasks = [
    makeTask(1, 2, { status: "running" }),
    makeTask(2, 3, { status: "running", parentTaskId: 1, sessionId: 501 }),
  ];
  render(
    <TaskBoard detail={makeDetail(tasks)} selectedAgentId={null} onSelectTask={vi.fn()} />,
  );
  // 懒加载完成后折叠行出现(+1 子代理)
  const toggle = await screen.findByTestId("board-subagents-2");
  expect(toggle).toHaveTextContent("1");
  // 默认折叠:子行不在
  expect(screen.queryByTestId("board-subagent-2-0")).not.toBeInTheDocument();
  // 点开 → 子行出现
  fireEvent.click(toggle);
  expect(screen.getByTestId("board-subagent-2-0")).toBeInTheDocument();
});
```

- [ ] **Step 3: 跑测试确认失败**

Run: `cd frontend && pnpm test -- src/components/agentre/orchestration/__tests__/task-board.test.tsx`
Expected: FAIL — `board-subagents-2` 不存在。

- [ ] **Step 4: 实现折叠子行**

`TaskBoard` 顶部加 `const subagents = useRunSubagents(detail);`(import `useRunSubagents`)。新增折叠态:
```tsx
  const [expandedSub, setExpandedSub] = React.useState<Set<number>>(
    () => new Set(),
  );
  const toggleSub = (taskId: number) =>
    setExpandedSub((prev) => {
      const next = new Set(prev);
      next.has(taskId) ? next.delete(taskId) : next.add(taskId);
      return next;
    });
```
在 `renderRow` 返回的 `<li>` 内、`</button>` 之后(仍在该 `<li>`)插入折叠块:
```tsx
                          {(() => {
                            const subs = subagents.forSession(task.sessionId);
                            if (subs.length === 0) return null;
                            const open = expandedSub.has(task.id);
                            return (
                              <div className={cn(indented ? "pl-12" : "pl-8")}>
                                <button
                                  type="button"
                                  data-testid={`board-subagents-${task.id}`}
                                  onClick={() => toggleSub(task.id)}
                                  aria-expanded={open}
                                  className="flex items-center gap-1 px-3 py-1 text-xs text-subtle-foreground hover:text-muted-foreground"
                                >
                                  <span>{open ? "▾" : "▸"}</span>
                                  <span>
                                    {t("orchestration.subagent.badge", {
                                      count: subs.length,
                                    })}
                                  </span>
                                  <span className="text-muted-foreground/60">
                                    · {t("orchestration.subagent.autoMerge")}
                                  </span>
                                </button>
                                {open && (
                                  <ul className="flex flex-col gap-0.5 pb-1">
                                    {subs.map((sa, i) => (
                                      <li
                                        key={sa.toolUseId}
                                        data-testid={`board-subagent-${task.id}-${i}`}
                                        className="flex items-center gap-2 px-3 py-1 pl-8 text-xs text-muted-foreground"
                                      >
                                        <span className="truncate">
                                          {sa.role || sa.description || sa.toolUseId}
                                        </span>
                                      </li>
                                    ))}
                                  </ul>
                                )}
                              </div>
                            );
                          })()}
```
> 注意:`renderRow` 现有结构是 `<li><button>…</button></li>`;把折叠块放进 `<li>`、`<button>` 之后(子代理触发是独立 button,不嵌在 task button 里——避免按钮嵌套非法)。需把 `renderRow` 的 `<li>` 包裹改为同时容纳 task button + 折叠块。

- [ ] **Step 5: 跑测试通过**

Run: `cd frontend && pnpm test -- src/components/agentre/orchestration/__tests__/task-board.test.tsx`
Expected: PASS(含既有用例:无 sessionId / 无子代理的行不渲染折叠块)。

- [ ] **Step 6: 加 i18n 键(Task 7 统一加,可先占位跑)** — 见 Task 7;此处测试用 testid + count 断言,不依赖文案。

- [ ] **Step 7:(门控)提交**

```bash
git commit frontend/src/components/agentre/orchestration/task-board.tsx \
  frontend/src/components/agentre/orchestration/__tests__/task-board.test.tsx \
  -m "✨ orch 任务板:call 行下 ▸ +N 子代理 默认折叠只读子行(零后端 derive)"
```

---

## Task 6: 结构图节点 `+N 子代理` 合并徽标(S5b · 接 S4)

**Files:**
- Modify: `structure-graph.tsx`(S4 后的 `StructureGraph` 传 count 进 `NodeCard`;`NodeCard` 头部加徽标)
- Test: `__tests__/structure-graph.test.tsx`(补 `LoadChatSession` mock + store reset)

**Interfaces:**
- Consumes: `useRunSubagents(detail).countForAgent(agentId)`。
- Produces:`node-{agentId}-subagents` 徽标(`+N 子代理`),`N>0` 才渲染。

- [ ] **Step 1: 补 mock + 写失败测试**

`structure-graph.test.tsx`:`vi.mock` 的 App 对象加 `LoadChatSession: vi.fn().mockResolvedValue({ messages: [{ blocks: [{ type:"tool_use", toolUseId:"s", subagent:{ kind:"local_agent", status:"running" } }] }] })`;`beforeEach` 加 `useOrchSubagentsStore.getState().__reset()`。新增:
```ts
it("agent 的 task session 有 CLI 子代理 → 节点挂 +N 子代理 徽标", async () => {
  const detail = makeDetail({
    runStatus: "running",
    tasks: [makeTask(1, 2, "running", 0, 0), makeTask(2, 3, "running", 1, 700)],
  });
  render(<StructureGraph detail={detail} onSelectNode={vi.fn()} />);
  const badge = await screen.findByTestId("node-3-subagents");
  expect(badge).toHaveTextContent("1");
});
```

- [ ] **Step 2: 跑确认失败**

Run: `cd frontend && pnpm test -- src/components/agentre/orchestration/__tests__/structure-graph.test.tsx`
Expected: FAIL — `node-3-subagents` 不存在。

- [ ] **Step 3: 实现徽标**

`StructureGraph` 内加 `const subagents = useRunSubagents(detail);`,把 `subagentCount` 透传进 `NodeTree`→`NodeCard`(新增可选 prop `subagentCount: number`,在 `NodeTree` 渲染处用 `subagents.countForAgent(node.agentId)` 求值)。`NodeCard` 头部(皇冠/×N 之后)加:
```tsx
        {subagentCount > 0 && (
          <span
            data-testid={`node-${node.agentId}-subagents`}
            className="shrink-0 rounded bg-secondary px-1.5 py-0.5 text-xs text-muted-foreground"
          >
            {t("orchestration.subagent.badge", { count: subagentCount })}
          </span>
        )}
```
> `secondary`/`muted-foreground` 中性色,与 agent 着色节点、琥珀 ask 徽标区分(对齐设计稿 §9:`git-merge` 中性徽标)。

- [ ] **Step 4: 跑测试通过(含既有 4+2 用例)**

Run: `cd frontend && pnpm test -- src/components/agentre/orchestration/__tests__/structure-graph.test.tsx`
Expected: PASS。既有用例的 task `sessionId=0`(或 mock 返回空)→ count 0 → 不渲染徽标,不影响。

> ⚠ 既有 S4 用例若 `makeTask` 默认 `sessionId=0`,`ensureLoaded(0)` 被跳过,无徽标,稳。死锁/completed 用例的 task 若带 `sessionId` 会触发 mock 返回 1 个子代理 → 多出徽标但不影响其断言(断言的是 banner/ring)。

- [ ] **Step 5:(门控)提交**

```bash
git commit frontend/src/components/agentre/orchestration/structure-graph.tsx \
  frontend/src/components/agentre/orchestration/__tests__/structure-graph.test.tsx \
  -m "✨ orch 结构图:节点 +N 子代理 合并徽标(useRunSubagents)"
```

---

## Task 7: i18n `orchestration.subagent.*` + 全量校验

**Files:**
- Modify: `frontend/src/i18n/locales/{zh-CN,en}/common.json`

- [ ] **Step 1: 加 zh-CN 键**

`orchestration` 下新增 `subagent` 对象:
```json
    "subagent": {
      "badge": "+{{count}} 子代理",
      "autoMerge": "auto-merge",
      "noTask": "无独立任务",
      "expand": "展开子代理",
      "collapse": "收起子代理"
    }
```

- [ ] **Step 2: 加 en 键(同结构)**

```json
    "subagent": {
      "badge": "+{{count}} subagents",
      "autoMerge": "auto-merge",
      "noTask": "no task",
      "expand": "Expand subagents",
      "collapse": "Collapse subagents"
    }
```

- [ ] **Step 3: i18n + 编排目录全量测试**

Run: `cd frontend && pnpm test -- src/__tests__/i18n.test.ts src/components/agentre/orchestration src/stores/__tests__/orch-subagents-store.test.ts`
Expected: PASS。

- [ ] **Step 4: 全量前端 + lint(看真 exit code)**

```bash
cd frontend && pnpm test
cd /Users/codfrm/Code/agentre/agentre && make lint
```
Expected: 全绿;`i18next/no-literal-string` 不报新组件(`▸`/`▾`/`#`/`·` 非中文,放心;若 lint 抱怨,把 `▸/▾` 也走 `t("orchestration.subagent.expand/collapse")` 的 aria-label 并保留符号字面量,符号非中文一般放行)。

- [ ] **Step 5:(门控)提交**

```bash
git commit frontend/src/i18n/locales/zh-CN/common.json frontend/src/i18n/locales/en/common.json \
  -m "🌐 orch:子代理徽标/折叠 i18n 双语键(orchestration.subagent.*)"
```

---

## Final verification (after all tasks)

- [ ] `cd frontend && pnpm test -- src/components/agentre/orchestration src/stores/__tests__/orch-subagents-store.test.ts` — 全绿。
- [ ] `make test-frontend` + `make lint` — 全绿。
- [ ] 人工对照设计稿 `iBqBl`/`r0NpeW`:任务板头部 `5/12`、验签助手分组 `#1/#2`、用例生成器 `▸ +8 子代理 · auto-merge` 折叠行、结构图 后端工程师 `+3`/用例生成器 `+8` 中性徽标。**真机**:跑一个真起过 CLI 子代理(Task 工具)的 Run 目检 derive 是否数对(可并入 S7 验证)。

## Self-review notes(写计划时已核对)

1. **Spec coverage(§51/§5.7b/§9)**:`5/12` 计数 → Task 1;agent 分组 + per-call `#1/#2` → Task 2;CLI 子代理折叠子行(`▸ +N · auto-merge`)→ Task 5;结构图 `+N` 中性徽标 → Task 6;零后端 derive(`local_agent`)→ Task 3/4。✅
2. **数据流权衡**:已在顶部「必读」显式拍板=懒加载缓存(代价 N 次 `LoadChatSession`),并给出小后端备选——**留你 review 改判**。
3. **`5/12` 是否含 Leader 根**:本 plan `total=tasks.length`(含 Leader 根 task)。spec「只数 orch 派发 call」可能想**排除**根 task(根代表 Run 目标本身)。**留 review**:若要排除,Task 1 改 `total = tasks.filter(t => t.parentTaskId !== 0).length`、`done` 同口径。
4. **依赖**:Task 6 改的 `structure-graph.tsx` 以 **S4 后**的 `NodeCard` 为基线(`subagentCount` prop 加在 S4 的头部渲染之后)。执行顺序须 S4 → 本 plan。
5. **划界**:per-call 钻入=S6(本切片 per-call 行/折叠子行均不钻入,点击走 `onSelectTask(agentId)`/折叠只读)。`noTask`/`expand`/`collapse` 键已备(`noTask` 供「无独立任务」标,本切片折叠行已显 `auto-merge`,`noTask` 留 S6/精修用,先建键不渲染——若 lint/i18n 测试报「未使用键」再删)。
6. **Placeholder/类型**:无 TODO;`SubagentLite`/`useRunSubagents`/store 形状在 Task 3/4 定义、Task 5/6 消费,名字逐字对齐;testid `board-progress`/`board-agent-{id}`/`board-subagents-{taskId}`/`board-subagent-{taskId}-{i}`/`node-{agentId}-subagents` 实现与测试一致。
7. **按钮嵌套**:子代理折叠触发是**独立 `<button>`**、放在 task `<button>` 之外(同一 `<li>`),避免非法嵌套(Task 5 Step 4 已强调)。
