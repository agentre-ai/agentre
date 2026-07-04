# 编排流程 DAG 设计器 Phase 2 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在流程库管理器(`workflow-manager-dialog.tsx`)里加一个表单式 DAG 设计器——左栏结构化节点表单、右上实时 DAG 图、右下实时提示词预览——让用户直接编辑 `workflows.graph`,保存后由 Phase 1 后端投影 content/outline。

**Architecture:** 纯前端。Phase 1 后端已就绪(`WorkflowCreate/Update` 接受 `graph` 并 `applyGraph` 投影;`WorkflowPreviewGraph(name, graph)` 提供实时投影;`FlowGraphView` 渲染只读 DAG)。本片新增一个**纯 graph 编辑模块**(`flow-graph-draft.ts`,无 React/无 wails,可纯单测)、一个**单节点表单**(`workflow-node-form.tsx`)、一个**三栏设计器**(`workflow-dag-designer.tsx`,内部防抖调预览绑定),并把它们接进管理器:新建/有 graph 的流程走设计器,legacy 无 graph 流程保留自由文本表单 + 「转成 DAG」入口;保存时把 `graph` 串起(`use-workflows` 的 `create/update` 增参)。

**Tech Stack:** React 19 + TypeScript + Vite + Vitest + Testing Library + Tailwind v4 + shadcn `@/components/ui/*` + react-i18next。无新依赖。

## Global Constraints

- **不引入任何新前端依赖**:节点/边编辑用自定义代码,DAG 复用既有 `FlowGraphView`,预览复用既有 `MarkdownText`。
- **frontend/wailsjs/ 是 gitignore 生成物**,本片不改 Go 绑定(`WorkflowPreviewGraph` + `graph` 字段 Phase 1 已生成),**不需要 `make generate`**。
- **所有可见 UI 文案走 `t(...)`** 并同时更新 `frontend/src/i18n/locales/zh-CN/common.json` 与 `frontend/src/i18n/locales/en/common.json`;`i18next/no-literal-string` 只在 JSX 文本/可见属性上拦 Chinese 字面量,JS 表达式里的技术字符串(节点 id、`"__none__"` sentinel)不算文案。节点 `label/brief` 是用户动态数据,绝不入 i18n。
- **软约束**:设计器只产出 graph 与投影散文,无硬门控(那是 Phase 3)。
- **DAG 单一形状**:草稿态就是 Phase 1 的 `FlowGraph`(`version/nodes/edges`),`FlowGraphView` 与 `WorkflowPreviewGraph` 都消费它;不引入第二套中间结构。
- **边的编辑方式**:每个节点用「依赖(depends on)」多选**只列本节点之前的节点**——这既是「在表单里画 sequence 边」的方式,也天然防环;「失败打回(bounce)」单选可指向任意其它节点(bounce 边被 `layoutFlowGraph` 跳过,不参与分层,故不影响 DAG)。
- **测试注意**:渲染 `MarkdownText` 的测试会间接 import `@/../wailsjs/runtime/runtime`(经 `RichLink` → `BrowserOpenURL`),必须 per-file `vi.mock` 该 runtime 模块(`importActual` + override,见 Task 4/5);import wailsjs `App` 的测试 per-file `vi.mock` App 模块。测试文件比组件深一层,`__tests__/` 下引 `App`/`runtime` 用 5 层 `../../../../../wailsjs/...`。
- **收尾 gate 看真 exit code**:`pnpm exec tsc`/`| tail` 会吞退出码,重定向到文件后 `grep "error TS"` 数真错误。
- 提交遵循 gitmoji + `git commit <files>` 带 pathspec(develop/wyz 有并发会话共享 index)。

---

### Task 1: 纯 graph 编辑模块 `flow-graph-draft.ts`

无 React、无 wails 的纯函数模块:草稿态就是 `FlowGraph`,提供节点增删改/排序、depends-on↔sequence 边互转、bounce 边设置、序列化。全部纯函数 → 纯单测。

**Files:**
- Create: `frontend/src/components/agentre/workflows/flow-graph-draft.ts`
- Test: `frontend/src/components/agentre/workflows/__tests__/flow-graph-draft.test.ts`

**Interfaces:**
- Consumes: `FlowGraph`, `FlowNode` from `../orchestration/flow-graph`(Phase 1,`{version:number; nodes:FlowNode[]; edges:FlowEdge[]}`;`FlowNode={id,label,kind,brief?,sharedFiles?}`;`FlowEdge={from,to,kind?:"bounce"}`;`FlowKind="task"|"leader"`)。
- Produces(后续任务依赖这些精确签名):
  - `emptyDraftGraph(): FlowGraph` — 单个空白 task 节点(`id:"n1"`)。
  - `nextNodeId(g: FlowGraph): string` — `"n"+ (max existing n<k> + 1)`。
  - `addNode(g: FlowGraph): FlowGraph`
  - `updateNode(g: FlowGraph, id: string, patch: Partial<Pick<FlowNode,"label"|"kind"|"brief">>): FlowGraph`
  - `removeNode(g: FlowGraph, id: string): FlowGraph`
  - `moveNode(g: FlowGraph, id: string, dir: -1 | 1): FlowGraph`
  - `earlierNodeIds(g: FlowGraph, id: string): string[]`
  - `nodeDependsOn(g: FlowGraph, id: string): string[]`
  - `setDependsOn(g: FlowGraph, id: string, deps: string[]): FlowGraph`
  - `nodeBounce(g: FlowGraph, id: string): string | null`
  - `setBounce(g: FlowGraph, id: string, target: string | null): FlowGraph`
  - `graphToJSON(g: FlowGraph): string`

- [ ] **Step 1: Write the failing test**

Create `frontend/src/components/agentre/workflows/__tests__/flow-graph-draft.test.ts`:

```ts
import { describe, expect, it } from "vitest";

import {
  addNode,
  earlierNodeIds,
  emptyDraftGraph,
  graphToJSON,
  moveNode,
  nextNodeId,
  nodeBounce,
  nodeDependsOn,
  removeNode,
  setBounce,
  setDependsOn,
  updateNode,
} from "../flow-graph-draft";

describe("flow-graph-draft", () => {
  it("emptyDraftGraph 是单个空白 task 节点", () => {
    const g = emptyDraftGraph();
    expect(g.version).toBe(1);
    expect(g.nodes).toEqual([{ id: "n1", label: "", kind: "task" }]);
    expect(g.edges).toEqual([]);
  });

  it("nextNodeId 取现有 n<k> 最大值 + 1", () => {
    expect(nextNodeId(emptyDraftGraph())).toBe("n2");
    const g = { version: 1, nodes: [{ id: "n5", label: "x", kind: "task" as const }], edges: [] };
    expect(nextNodeId(g)).toBe("n6");
  });

  it("addNode 追加一个新 id 的空白 task 节点", () => {
    const g = addNode(emptyDraftGraph());
    expect(g.nodes.map((n) => n.id)).toEqual(["n1", "n2"]);
    expect(g.nodes[1]).toEqual({ id: "n2", label: "", kind: "task" });
  });

  it("updateNode 只改目标节点的 label/kind/brief", () => {
    const g = updateNode(emptyDraftGraph(), "n1", { label: "See", kind: "leader" });
    expect(g.nodes[0]).toMatchObject({ id: "n1", label: "See", kind: "leader" });
  });

  it("removeNode 删节点并连带删除其所有边", () => {
    let g = addNode(emptyDraftGraph()); // n1, n2
    g = setDependsOn(g, "n2", ["n1"]); // edge n1->n2
    g = removeNode(g, "n1");
    expect(g.nodes.map((n) => n.id)).toEqual(["n2"]);
    expect(g.edges).toEqual([]);
  });

  it("moveNode 在 nodes[] 里换位, 越界不动", () => {
    let g = addNode(emptyDraftGraph()); // n1, n2
    g = moveNode(g, "n2", -1);
    expect(g.nodes.map((n) => n.id)).toEqual(["n2", "n1"]);
    expect(moveNode(g, "n2", -1).nodes.map((n) => n.id)).toEqual(["n2", "n1"]);
  });

  it("earlierNodeIds 只返回 nodes[] 里排在目标之前的节点", () => {
    let g = addNode(addNode(emptyDraftGraph())); // n1, n2, n3
    expect(earlierNodeIds(g, "n1")).toEqual([]);
    expect(earlierNodeIds(g, "n3")).toEqual(["n1", "n2"]);
  });

  it("setDependsOn/nodeDependsOn 往返: 依赖生成 sequence 边", () => {
    let g = addNode(addNode(emptyDraftGraph())); // n1, n2, n3
    g = setDependsOn(g, "n3", ["n1", "n2"]);
    expect(nodeDependsOn(g, "n3").sort()).toEqual(["n1", "n2"]);
    expect(g.edges).toEqual([
      { from: "n1", to: "n3" },
      { from: "n2", to: "n3" },
    ]);
    // 再次 setDependsOn 替换该节点入边, 不累加
    g = setDependsOn(g, "n3", ["n2"]);
    expect(nodeDependsOn(g, "n3")).toEqual(["n2"]);
  });

  it("setBounce/nodeBounce: 唯一 bounce 出边, null 清除", () => {
    let g = addNode(emptyDraftGraph()); // n1, n2
    g = setBounce(g, "n2", "n1");
    expect(nodeBounce(g, "n2")).toBe("n1");
    expect(g.edges).toContainEqual({ from: "n2", to: "n1", kind: "bounce" });
    g = setBounce(g, "n2", null);
    expect(nodeBounce(g, "n2")).toBeNull();
    expect(g.edges.some((e) => e.kind === "bounce")).toBe(false);
  });

  it("setDependsOn 不误删 bounce 边; setBounce 不误删 sequence 边", () => {
    let g = addNode(emptyDraftGraph()); // n1, n2
    g = setDependsOn(g, "n2", ["n1"]); // n1->n2 sequence
    g = setBounce(g, "n2", "n1"); // n2->n1 bounce
    g = setDependsOn(g, "n2", []); // 清 n2 入向 sequence
    expect(nodeBounce(g, "n2")).toBe("n1"); // bounce 仍在
    expect(nodeDependsOn(g, "n2")).toEqual([]);
  });

  it("graphToJSON 输出可被 JSON.parse", () => {
    const g = emptyDraftGraph();
    expect(JSON.parse(graphToJSON(g))).toEqual(g);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && pnpm test -- --run src/components/agentre/workflows/__tests__/flow-graph-draft.test.ts`
Expected: FAIL — `Failed to resolve import "../flow-graph-draft"` / functions not defined.

- [ ] **Step 3: Write minimal implementation**

Create `frontend/src/components/agentre/workflows/flow-graph-draft.ts`:

```ts
import type { FlowGraph, FlowNode } from "../orchestration/flow-graph";

// nextNodeId: 找现有 n<k> 的最大 k, 返回 n<k+1>。确定性(不依赖随机/时间), 便于单测。
export function nextNodeId(g: FlowGraph): string {
  let max = 0;
  for (const n of g.nodes) {
    const m = /^n(\d+)$/.exec(n.id);
    if (m) max = Math.max(max, Number(m[1]));
  }
  return `n${max + 1}`;
}

// emptyDraftGraph: 最小可编辑图 —— 单个空白 task 节点。
export function emptyDraftGraph(): FlowGraph {
  return {
    version: 1,
    nodes: [{ id: "n1", label: "", kind: "task" }],
    edges: [],
  };
}

export function addNode(g: FlowGraph): FlowGraph {
  const id = nextNodeId(g);
  const node: FlowNode = { id, label: "", kind: "task" };
  return { ...g, nodes: [...g.nodes, node] };
}

export function updateNode(
  g: FlowGraph,
  id: string,
  patch: Partial<Pick<FlowNode, "label" | "kind" | "brief">>,
): FlowGraph {
  return {
    ...g,
    nodes: g.nodes.map((n) => (n.id === id ? { ...n, ...patch } : n)),
  };
}

// removeNode: 删节点并连带删除所有以它为端点的边(sequence + bounce)。
export function removeNode(g: FlowGraph, id: string): FlowGraph {
  return {
    ...g,
    nodes: g.nodes.filter((n) => n.id !== id),
    edges: g.edges.filter((e) => e.from !== id && e.to !== id),
  };
}

export function moveNode(g: FlowGraph, id: string, dir: -1 | 1): FlowGraph {
  const i = g.nodes.findIndex((n) => n.id === id);
  const j = i + dir;
  if (i < 0 || j < 0 || j >= g.nodes.length) return g;
  const nodes = [...g.nodes];
  [nodes[i], nodes[j]] = [nodes[j], nodes[i]];
  return { ...g, nodes };
}

// earlierNodeIds: nodes[] 顺序中排在 id 之前的节点 —— depends-on 候选 + 天然防环。
export function earlierNodeIds(g: FlowGraph, id: string): string[] {
  const out: string[] = [];
  for (const n of g.nodes) {
    if (n.id === id) break;
    out.push(n.id);
  }
  return out;
}

// nodeDependsOn: 由 sequence 边(非 bounce)反推该节点的前驱集合。
export function nodeDependsOn(g: FlowGraph, id: string): string[] {
  return g.edges
    .filter((e) => e.kind !== "bounce" && e.to === id)
    .map((e) => e.from);
}

// setDependsOn: 用 deps 重建该节点的入向 sequence 边(替换), 不动 bounce 边与其它节点的边。
export function setDependsOn(
  g: FlowGraph,
  id: string,
  deps: string[],
): FlowGraph {
  const kept = g.edges.filter((e) => !(e.kind !== "bounce" && e.to === id));
  const added = deps.map((from) => ({ from, to: id }));
  return { ...g, edges: [...kept, ...added] };
}

// nodeBounce: 该节点的失败打回目标, 无则 null。
export function nodeBounce(g: FlowGraph, id: string): string | null {
  const e = g.edges.find((x) => x.kind === "bounce" && x.from === id);
  return e ? e.to : null;
}

// setBounce: 替换该节点唯一的 bounce 出边; target 为 null 则清除。
export function setBounce(
  g: FlowGraph,
  id: string,
  target: string | null,
): FlowGraph {
  const kept = g.edges.filter((e) => !(e.kind === "bounce" && e.from === id));
  const added: FlowGraph["edges"] = target
    ? [{ from: id, to: target, kind: "bounce" }]
    : [];
  return { ...g, edges: [...kept, ...added] };
}

export function graphToJSON(g: FlowGraph): string {
  return JSON.stringify(g);
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd frontend && pnpm test -- --run src/components/agentre/workflows/__tests__/flow-graph-draft.test.ts`
Expected: PASS(11 passed)。

- [ ] **Step 5: Commit**

```bash
git commit frontend/src/components/agentre/workflows/flow-graph-draft.ts \
  frontend/src/components/agentre/workflows/__tests__/flow-graph-draft.test.ts \
  -m "✨ orchestration: DAG 设计器纯 graph 编辑模块(节点增删改/依赖边/bounce)"
```

---

### Task 2: `use-workflows` 打通 graph 参数

管理器要能读回流程的 `graph` 做编辑,并在保存时把 graph 串给后端。给 hook 的投影类型加 `graph`,给 `create/update` 增一个可选 `graph` 参数(默认 `""` → 后端 `applyGraph` 空图 no-op,保留原状)。

**Files:**
- Modify: `frontend/src/hooks/use-workflows.ts`
- Test: `frontend/src/hooks/__tests__/use-workflows.test.ts`

**Interfaces:**
- Consumes: 生成绑定 `WorkflowCreate/WorkflowUpdate/WorkflowList/WorkflowDelete`(`WorkflowItem` DTO 已有 `graph: string`;`CreateWorkflowRequest/UpdateWorkflowRequest` 已有 `graph?: string`)。
- Produces:
  - `WorkflowItem`(hook 投影类型)新增 `graph: string`。
  - `create(name: string, content: string, tags: string[], outline: string[], graph?: string): Promise<void>`
  - `update(id: number, name: string, content: string, tags: string[], outline: string[], graph?: string): Promise<void>`

- [ ] **Step 1: Write the failing test**

Create `frontend/src/hooks/__tests__/use-workflows.test.ts`:

```ts
import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const appMocks = vi.hoisted(() => ({
  WorkflowList: vi.fn(),
  WorkflowCreate: vi.fn(),
  WorkflowUpdate: vi.fn(),
  WorkflowDelete: vi.fn(),
}));
vi.mock("../../../wailsjs/go/app/App", () => appMocks);

import { useWorkflows } from "../use-workflows";

describe("useWorkflows graph 打通", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    appMocks.WorkflowList.mockResolvedValue({ items: [] });
    appMocks.WorkflowCreate.mockResolvedValue({});
    appMocks.WorkflowUpdate.mockResolvedValue({});
  });

  it("投影出的 WorkflowItem 携带 graph 字段", async () => {
    appMocks.WorkflowList.mockResolvedValue({
      items: [
        {
          id: 1,
          name: "F",
          content: "c",
          tags: [],
          outline: [],
          runCount: 0,
          createtime: 0,
          updatetime: 0,
          graph: '{"version":1,"nodes":[{"id":"n1","label":"x","kind":"task"}],"edges":[]}',
        },
      ],
    });
    const { result } = renderHook(() => useWorkflows());
    await waitFor(() => expect(result.current.workflows.length).toBe(1));
    expect(result.current.workflows[0].graph).toContain('"n1"');
  });

  it("create 把 graph 传给 WorkflowCreate", async () => {
    const { result } = renderHook(() => useWorkflows());
    await waitFor(() => expect(appMocks.WorkflowList).toHaveBeenCalled());
    await act(async () => {
      await result.current.create("N", "C", [], [], '{"g":1}');
    });
    expect(appMocks.WorkflowCreate).toHaveBeenCalledWith(
      expect.objectContaining({ name: "N", graph: '{"g":1}' }),
    );
  });

  it("update 把 graph 传给 WorkflowUpdate; 省略时为空串", async () => {
    const { result } = renderHook(() => useWorkflows());
    await waitFor(() => expect(appMocks.WorkflowList).toHaveBeenCalled());
    await act(async () => {
      await result.current.update(7, "N", "C", [], []);
    });
    expect(appMocks.WorkflowUpdate).toHaveBeenCalledWith(
      expect.objectContaining({ id: 7, graph: "" }),
    );
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && pnpm test -- --run src/hooks/__tests__/use-workflows.test.ts`
Expected: FAIL — `workflows[0].graph` undefined;`WorkflowCreate` 调用不含 `graph`。

- [ ] **Step 3: Write minimal implementation**

In `frontend/src/hooks/use-workflows.ts`:

3a. Add `graph` to the projection type (after `content`):

```ts
export type WorkflowItem = {
  id: number;
  name: string;
  content: string;
  graph: string;
  tags: string[];
  outline: string[];
  runCount: number;
  createtime: number;
  updatetime: number;
};
```

3b. Map `graph` in `reload()` (inside the `.map((i) => ({ ... }))`, after `content: i.content,`):

```ts
          content: i.content,
          graph: i.graph ?? "",
```

3c. Replace `create`:

```ts
  const create = useCallback(
    async (
      name: string,
      content: string,
      tags: string[],
      outline: string[],
      graph = "",
    ) => {
      await WorkflowCreate({ name, content, tags, outline, graph });
      await reload();
    },
    [reload],
  );
```

3d. Replace `update`:

```ts
  const update = useCallback(
    async (
      id: number,
      name: string,
      content: string,
      tags: string[],
      outline: string[],
      graph = "",
    ) => {
      await WorkflowUpdate({ id, name, content, tags, outline, graph });
      await reload();
    },
    [reload],
  );
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd frontend && pnpm test -- --run src/hooks/__tests__/use-workflows.test.ts`
Expected: PASS(3 passed)。

- [ ] **Step 5: Commit**

```bash
git commit frontend/src/hooks/use-workflows.ts \
  frontend/src/hooks/__tests__/use-workflows.test.ts \
  -m "✨ orchestration: use-workflows 投影/create/update 打通 graph 参数"
```

---

### Task 3: 单节点表单 `workflow-node-form.tsx`

设计器左栏里的单个节点卡:名称 Input、类型 Select(task/leader)、任务说明 Textarea(仅 task)、依赖多选(toggle chips,只列前序节点)、失败打回 Select(其它节点)、上移/下移/删除。纯受控组件,无 wails。

**Files:**
- Create: `frontend/src/components/agentre/workflows/workflow-node-form.tsx`
- Test: `frontend/src/components/agentre/workflows/__tests__/workflow-node-form.test.tsx`
- Modify: `frontend/src/i18n/locales/zh-CN/common.json`
- Modify: `frontend/src/i18n/locales/en/common.json`

**Interfaces:**
- Consumes: `FlowNode`, `FlowKind` from `../orchestration/flow-graph`;shadcn `Button/Input/Textarea/Select*`;`workflows.editor.moveUp/moveDown/removeItem`(复用)。
- Produces:
  ```ts
  export function WorkflowNodeForm(props: {
    node: FlowNode;
    index: number;
    earlier: FlowNode[];          // depends-on 候选(前序节点)
    others: FlowNode[];           // bounce 候选(除自己外的全部节点)
    dependsOn: string[];
    bounce: string | null;
    canRemove: boolean;
    onLabelChange: (v: string) => void;
    onKindChange: (v: FlowKind) => void;
    onBriefChange: (v: string) => void;
    onToggleDependsOn: (depId: string) => void;
    onBounceChange: (target: string | null) => void;
    onMoveUp: () => void;
    onMoveDown: () => void;
    onRemove: () => void;
  }): JSX.Element
  ```
  testid 约定:`node-<id>-label` / `node-<id>-kind` / `node-<id>-brief` / `node-<id>-dep-<depId>` / `node-<id>-bounce` / `node-<id>-up` / `node-<id>-down` / `node-<id>-remove`。

- [ ] **Step 1: Add i18n keys**

In `frontend/src/i18n/locales/en/common.json`, add a `designer` object inside `workflows` (sibling of `editor`):

```json
    "designer": {
      "nodeLabel": "Node name",
      "nodeLabelPlaceholder": "e.g. Break down requirements",
      "nodeKind": "Type",
      "kindTask": "Delegated task",
      "kindLeader": "Leader step",
      "nodeBrief": "Task brief",
      "nodeBriefPlaceholder": "What to do + acceptance criteria",
      "dependsOn": "Depends on",
      "dependsOnHint": "Runs after the selected earlier steps",
      "bounce": "On failure → bounce to",
      "bounceNone": "None"
    }
```

In `frontend/src/i18n/locales/zh-CN/common.json`, add the matching block inside `workflows`:

```json
    "designer": {
      "nodeLabel": "节点名称",
      "nodeLabelPlaceholder": "如:拆解需求",
      "nodeKind": "类型",
      "kindTask": "委派任务",
      "kindLeader": "Leader 步骤",
      "nodeBrief": "任务说明",
      "nodeBriefPlaceholder": "做什么 + 验收标准",
      "dependsOn": "依赖",
      "dependsOnHint": "在所选的前序步骤之后执行",
      "bounce": "失败打回 →",
      "bounceNone": "无"
    }
```

- [ ] **Step 2: Write the failing test**

Create `frontend/src/components/agentre/workflows/__tests__/workflow-node-form.test.tsx`:

```tsx
import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import type { FlowNode } from "../../orchestration/flow-graph";
import { WorkflowNodeForm } from "../workflow-node-form";

const base: FlowNode = { id: "n2", label: "Build", kind: "task" };

function renderForm(overrides: Partial<Parameters<typeof WorkflowNodeForm>[0]> = {}) {
  const props = {
    node: base,
    index: 1,
    earlier: [{ id: "n1", label: "Plan", kind: "leader" as const }],
    others: [{ id: "n1", label: "Plan", kind: "leader" as const }],
    dependsOn: [] as string[],
    bounce: null as string | null,
    canRemove: true,
    onLabelChange: vi.fn(),
    onKindChange: vi.fn(),
    onBriefChange: vi.fn(),
    onToggleDependsOn: vi.fn(),
    onBounceChange: vi.fn(),
    onMoveUp: vi.fn(),
    onMoveDown: vi.fn(),
    onRemove: vi.fn(),
    ...overrides,
  };
  render(<WorkflowNodeForm {...props} />);
  return props;
}

describe("WorkflowNodeForm", () => {
  it("task 节点显示 brief 输入", () => {
    renderForm();
    expect(screen.getByTestId("node-n2-brief")).toBeInTheDocument();
  });

  it("leader 节点隐藏 brief 输入", () => {
    renderForm({ node: { id: "n2", label: "Wrap", kind: "leader" } });
    expect(screen.queryByTestId("node-n2-brief")).toBeNull();
  });

  it("改 label 触发 onLabelChange", () => {
    const props = renderForm();
    fireEvent.change(screen.getByTestId("node-n2-label"), {
      target: { value: "Ship" },
    });
    expect(props.onLabelChange).toHaveBeenCalledWith("Ship");
  });

  it("依赖 chip 点击触发 onToggleDependsOn", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    const props = renderForm();
    await user.click(screen.getByTestId("node-n2-dep-n1"));
    expect(props.onToggleDependsOn).toHaveBeenCalledWith("n1");
  });

  it("已选依赖 chip 标记 aria-pressed", () => {
    renderForm({ dependsOn: ["n1"] });
    expect(screen.getByTestId("node-n2-dep-n1").getAttribute("aria-pressed")).toBe("true");
  });

  it("无前序节点时不渲染依赖区", () => {
    renderForm({ earlier: [] });
    expect(screen.queryByTestId("node-n2-dep-n1")).toBeNull();
  });

  it("删除禁用当 canRemove=false", () => {
    renderForm({ canRemove: false });
    expect((screen.getByTestId("node-n2-remove") as HTMLButtonElement).disabled).toBe(true);
  });
});
```

- [ ] **Step 3: Run test to verify it fails**

Run: `cd frontend && pnpm test -- --run src/components/agentre/workflows/__tests__/workflow-node-form.test.tsx`
Expected: FAIL — `Failed to resolve import "../workflow-node-form"`。

- [ ] **Step 4: Write minimal implementation**

Create `frontend/src/components/agentre/workflows/workflow-node-form.tsx`:

```tsx
import { ArrowDown, ArrowUp, X } from "lucide-react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { cn } from "@/lib/utils";

import type { FlowKind, FlowNode } from "../orchestration/flow-graph";

// bounce「无」的哨兵值:shadcn SelectItem 不允许空串 value。
const BOUNCE_NONE = "__none__";

export function WorkflowNodeForm({
  node,
  index,
  earlier,
  others,
  dependsOn,
  bounce,
  canRemove,
  onLabelChange,
  onKindChange,
  onBriefChange,
  onToggleDependsOn,
  onBounceChange,
  onMoveUp,
  onMoveDown,
  onRemove,
}: {
  node: FlowNode;
  index: number;
  earlier: FlowNode[];
  others: FlowNode[];
  dependsOn: string[];
  bounce: string | null;
  canRemove: boolean;
  onLabelChange: (v: string) => void;
  onKindChange: (v: FlowKind) => void;
  onBriefChange: (v: string) => void;
  onToggleDependsOn: (depId: string) => void;
  onBounceChange: (target: string | null) => void;
  onMoveUp: () => void;
  onMoveDown: () => void;
  onRemove: () => void;
}) {
  const { t } = useTranslation();
  return (
    <div className="flex flex-col gap-2 rounded-md border border-border bg-card px-3 py-2.5">
      <div className="flex items-center gap-2">
        <span className="flex size-5 shrink-0 items-center justify-center rounded bg-accent text-2xs text-muted-foreground">
          {index + 1}
        </span>
        <Input
          data-testid={`node-${node.id}-label`}
          aria-label={t("workflows.designer.nodeLabel")}
          value={node.label}
          onChange={(e) => onLabelChange(e.target.value)}
          placeholder={t("workflows.designer.nodeLabelPlaceholder")}
          className="h-8 flex-1 text-xs"
        />
        <Button
          type="button"
          variant="ghost"
          size="icon-xs"
          data-testid={`node-${node.id}-up`}
          aria-label={t("workflows.editor.moveUp")}
          onClick={onMoveUp}
        >
          <ArrowUp className="size-3" aria-hidden="true" />
        </Button>
        <Button
          type="button"
          variant="ghost"
          size="icon-xs"
          data-testid={`node-${node.id}-down`}
          aria-label={t("workflows.editor.moveDown")}
          onClick={onMoveDown}
        >
          <ArrowDown className="size-3" aria-hidden="true" />
        </Button>
        <Button
          type="button"
          variant="ghost"
          size="icon-xs"
          disabled={!canRemove}
          data-testid={`node-${node.id}-remove`}
          aria-label={t("workflows.editor.removeItem")}
          onClick={onRemove}
        >
          <X className="size-3" aria-hidden="true" />
        </Button>
      </div>

      <div className="flex items-center gap-2">
        <span className="w-16 shrink-0 text-2xs text-muted-foreground">
          {t("workflows.designer.nodeKind")}
        </span>
        <Select value={node.kind} onValueChange={(v) => onKindChange(v as FlowKind)}>
          <SelectTrigger
            data-testid={`node-${node.id}-kind`}
            className="h-7 flex-1 text-2xs"
          >
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="task">{t("workflows.designer.kindTask")}</SelectItem>
            <SelectItem value="leader">{t("workflows.designer.kindLeader")}</SelectItem>
          </SelectContent>
        </Select>
      </div>

      {node.kind === "task" ? (
        <Textarea
          data-testid={`node-${node.id}-brief`}
          aria-label={t("workflows.designer.nodeBrief")}
          value={node.brief ?? ""}
          onChange={(e) => onBriefChange(e.target.value)}
          placeholder={t("workflows.designer.nodeBriefPlaceholder")}
          className="min-h-16 resize-none text-2xs"
        />
      ) : null}

      {earlier.length > 0 ? (
        <div className="flex flex-col gap-1">
          <span className="text-2xs text-muted-foreground">
            {t("workflows.designer.dependsOn")}
          </span>
          <div className="flex flex-wrap gap-1.5">
            {earlier.map((dep) => {
              const on = dependsOn.includes(dep.id);
              return (
                <button
                  key={dep.id}
                  type="button"
                  data-testid={`node-${node.id}-dep-${dep.id}`}
                  aria-pressed={on}
                  onClick={() => onToggleDependsOn(dep.id)}
                  className={cn(
                    "rounded border px-1.5 py-0.5 text-2xs",
                    on
                      ? "border-primary bg-primary-soft text-primary-text"
                      : "border-border bg-background text-muted-foreground hover:bg-accent/50",
                  )}
                >
                  {dep.label || dep.id}
                </button>
              );
            })}
          </div>
          <span className="text-2xs text-subtle-foreground">
            {t("workflows.designer.dependsOnHint")}
          </span>
        </div>
      ) : null}

      {others.length > 0 ? (
        <div className="flex items-center gap-2">
          <span className="w-16 shrink-0 text-2xs text-muted-foreground">
            {t("workflows.designer.bounce")}
          </span>
          <Select
            value={bounce ?? BOUNCE_NONE}
            onValueChange={(v) => onBounceChange(v === BOUNCE_NONE ? null : v)}
          >
            <SelectTrigger
              data-testid={`node-${node.id}-bounce`}
              className="h-7 flex-1 text-2xs"
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={BOUNCE_NONE}>
                {t("workflows.designer.bounceNone")}
              </SelectItem>
              {others.map((o) => (
                <SelectItem key={o.id} value={o.id}>
                  {o.label || o.id}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      ) : null}
    </div>
  );
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `cd frontend && pnpm test -- --run src/components/agentre/workflows/__tests__/workflow-node-form.test.tsx`
Expected: PASS(7 passed)。

- [ ] **Step 6: Verify i18n coverage still green**

Run: `cd frontend && pnpm test -- --run src/__tests__/i18n.test.ts`
Expected: PASS(both locales carry the new `workflows.designer.*` keys)。

- [ ] **Step 7: Commit**

```bash
git commit frontend/src/components/agentre/workflows/workflow-node-form.tsx \
  frontend/src/components/agentre/workflows/__tests__/workflow-node-form.test.tsx \
  frontend/src/i18n/locales/en/common.json \
  frontend/src/i18n/locales/zh-CN/common.json \
  -m "✨ orchestration: DAG 设计器单节点表单(kind/brief/依赖/bounce)"
```

---

### Task 4: 三栏设计器 `workflow-dag-designer.tsx`

组装:左栏名称 Input + 节点表单列表(`WorkflowNodeForm`)+「添加节点」;右上实时 DAG(`FlowGraphView`);右下防抖调用 `WorkflowPreviewGraph` 展示投影出的提示词(只读 `MarkdownText`)。所有 graph 变更经 `flow-graph-draft` 纯函数产生新 graph 上抛 `onGraphChange`。

**Files:**
- Create: `frontend/src/components/agentre/workflows/workflow-dag-designer.tsx`
- Test: `frontend/src/components/agentre/workflows/__tests__/workflow-dag-designer.test.tsx`
- Modify: `frontend/src/i18n/locales/zh-CN/common.json`
- Modify: `frontend/src/i18n/locales/en/common.json`

**Interfaces:**
- Consumes: Task 1 `flow-graph-draft`(`addNode/updateNode/removeNode/moveNode/earlierNodeIds/nodeDependsOn/setDependsOn/nodeBounce/setBounce/graphToJSON`);Task 3 `WorkflowNodeForm`;`FlowGraph/FlowKind` from `../orchestration/flow-graph`;`FlowGraphView` from `../orchestration/flow-graph-view`;`MarkdownText` from `../markdown-text`;`WorkflowPreviewGraph` from `../../../../wailsjs/go/app/App`;复用 `workflows.editor.name/namePlaceholder`。
- Produces:
  ```ts
  export function WorkflowDagDesigner(props: {
    name: string;
    graph: FlowGraph;
    error: string | null;
    onNameChange: (v: string) => void;
    onGraphChange: (g: FlowGraph) => void;
  }): JSX.Element
  ```
  testid:`designer-add-node` / `designer-prompt-preview`(名称 Input 复用 `workflow-name-input`)。

- [ ] **Step 1: Add i18n keys**

In `frontend/src/i18n/locales/en/common.json`, add to the `workflows.designer` object (created in Task 3):

```json
      "addNode": "Add node",
      "graphTitle": "Flow graph",
      "previewTitle": "Live prompt preview",
      "previewEmpty": "Add nodes to see the projected prompt"
```

In `frontend/src/i18n/locales/zh-CN/common.json`, add to `workflows.designer`:

```json
      "addNode": "添加节点",
      "graphTitle": "流程图",
      "previewTitle": "实时提示词预览",
      "previewEmpty": "添加节点后显示投影出的提示词"
```

- [ ] **Step 2: Write the failing test**

Create `frontend/src/components/agentre/workflows/__tests__/workflow-dag-designer.test.tsx`:

```tsx
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import * as React from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

// 设计器 → MarkdownText → RichLink 间接 import wailsjs runtime
vi.mock("../../../../../wailsjs/runtime/runtime", async () => {
  const actual = await vi.importActual<
    typeof import("../../../../../wailsjs/runtime/runtime")
  >("../../../../../wailsjs/runtime/runtime");
  return { ...actual, BrowserOpenURL: vi.fn() };
});

const appMocks = vi.hoisted(() => ({ WorkflowPreviewGraph: vi.fn() }));
vi.mock("../../../../../wailsjs/go/app/App", () => appMocks);

import type { FlowGraph } from "../../orchestration/flow-graph";
import { emptyDraftGraph } from "../flow-graph-draft";
import { WorkflowDagDesigner } from "../workflow-dag-designer";

// 受控 harness: 持有 graph/name state, 让设计器的编辑真正回写。
function Harness() {
  const [name, setName] = React.useState("Flow");
  const [graph, setGraph] = React.useState<FlowGraph>(emptyDraftGraph());
  return (
    <WorkflowDagDesigner
      name={name}
      graph={graph}
      error={null}
      onNameChange={setName}
      onGraphChange={setGraph}
    />
  );
}

describe("WorkflowDagDesigner", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    appMocks.WorkflowPreviewGraph.mockResolvedValue({
      content: "## Projected prompt",
      outline: [],
    });
  });

  it("渲染初始节点到 DAG(flow-node-n1)", () => {
    render(<Harness />);
    expect(screen.getByTestId("flow-node-n1")).toBeInTheDocument();
  });

  it("点击「添加节点」后 DAG 出现 flow-node-n2", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    render(<Harness />);
    await user.click(screen.getByTestId("designer-add-node"));
    expect(await screen.findByTestId("flow-node-n2")).toBeInTheDocument();
  });

  it("防抖后调用 WorkflowPreviewGraph 并展示投影正文", async () => {
    render(<Harness />);
    await waitFor(() =>
      expect(appMocks.WorkflowPreviewGraph).toHaveBeenCalledWith(
        expect.objectContaining({ name: "Flow" }),
      ),
    );
    expect(
      await screen.findByText("Projected prompt"),
    ).toBeInTheDocument();
  });
});
```

- [ ] **Step 3: Run test to verify it fails**

Run: `cd frontend && pnpm test -- --run src/components/agentre/workflows/__tests__/workflow-dag-designer.test.tsx`
Expected: FAIL — `Failed to resolve import "../workflow-dag-designer"`。

- [ ] **Step 4: Write minimal implementation**

Create `frontend/src/components/agentre/workflows/workflow-dag-designer.tsx`:

```tsx
import * as React from "react";
import { Plus } from "lucide-react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";

import type { FlowGraph, FlowKind } from "../orchestration/flow-graph";
import { FlowGraphView } from "../orchestration/flow-graph-view";
import { MarkdownText } from "../markdown-text";
import { WorkflowPreviewGraph } from "../../../../wailsjs/go/app/App";
import {
  addNode,
  earlierNodeIds,
  graphToJSON,
  moveNode,
  nodeBounce,
  nodeDependsOn,
  removeNode,
  setBounce,
  setDependsOn,
  updateNode,
} from "./flow-graph-draft";
import { WorkflowNodeForm } from "./workflow-node-form";

export function WorkflowDagDesigner({
  name,
  graph,
  error,
  onNameChange,
  onGraphChange,
}: {
  name: string;
  graph: FlowGraph;
  error: string | null;
  onNameChange: (v: string) => void;
  onGraphChange: (g: FlowGraph) => void;
}) {
  const { t } = useTranslation();
  const [preview, setPreview] = React.useState("");

  // graphJSON 作为依赖: graph 变化才重新预览(结构比较), 250ms 防抖。
  const graphJSON = graphToJSON(graph);
  React.useEffect(() => {
    let alive = true;
    const timer = setTimeout(() => {
      WorkflowPreviewGraph({ name, graph: graphJSON })
        .then((resp) => {
          if (alive) setPreview(resp?.content ?? "");
        })
        .catch(() => {
          if (alive) setPreview("");
        });
    }, 250);
    return () => {
      alive = false;
      clearTimeout(timer);
    };
  }, [name, graphJSON]);

  const nodeById = new Map(graph.nodes.map((n) => [n.id, n]));
  const earlierNodes = (id: string) =>
    earlierNodeIds(graph, id)
      .map((eid) => nodeById.get(eid))
      .filter((n): n is NonNullable<typeof n> => n != null);
  const otherNodes = (id: string) => graph.nodes.filter((n) => n.id !== id);

  const toggleDep = (id: string, depId: string) => {
    const cur = nodeDependsOn(graph, id);
    const next = cur.includes(depId)
      ? cur.filter((d) => d !== depId)
      : [...cur, depId];
    onGraphChange(setDependsOn(graph, id, next));
  };

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-3">
      <div className="flex min-h-0 flex-1 gap-4">
        {/* 左栏: 名称 + 节点表单 + 添加 */}
        <div className="flex w-[360px] shrink-0 flex-col gap-3 overflow-y-auto pr-1">
          <label className="flex flex-col gap-1.5 text-xs">
            <span className="font-medium text-foreground">
              {t("workflows.editor.name")}
              <span className="ml-0.5 text-destructive">*</span>
            </span>
            <Input
              data-testid="workflow-name-input"
              aria-label={t("workflows.editor.name")}
              value={name}
              onChange={(e) => onNameChange(e.target.value)}
              placeholder={t("workflows.editor.namePlaceholder")}
              className="h-9 text-xs"
            />
          </label>

          <div className="flex flex-col gap-2.5">
            {graph.nodes.map((node, i) => (
              <WorkflowNodeForm
                key={node.id}
                node={node}
                index={i}
                earlier={earlierNodes(node.id)}
                others={otherNodes(node.id)}
                dependsOn={nodeDependsOn(graph, node.id)}
                bounce={nodeBounce(graph, node.id)}
                canRemove={graph.nodes.length > 1}
                onLabelChange={(v) =>
                  onGraphChange(updateNode(graph, node.id, { label: v }))
                }
                onKindChange={(v: FlowKind) =>
                  onGraphChange(updateNode(graph, node.id, { kind: v }))
                }
                onBriefChange={(v) =>
                  onGraphChange(updateNode(graph, node.id, { brief: v }))
                }
                onToggleDependsOn={(depId) => toggleDep(node.id, depId)}
                onBounceChange={(target) =>
                  onGraphChange(setBounce(graph, node.id, target))
                }
                onMoveUp={() => onGraphChange(moveNode(graph, node.id, -1))}
                onMoveDown={() => onGraphChange(moveNode(graph, node.id, 1))}
                onRemove={() => onGraphChange(removeNode(graph, node.id))}
              />
            ))}
          </div>

          <Button
            type="button"
            variant="outline"
            size="sm"
            data-testid="designer-add-node"
            onClick={() => onGraphChange(addNode(graph))}
          >
            <Plus className="size-3.5" aria-hidden="true" />
            {t("workflows.designer.addNode")}
          </Button>
        </div>

        {/* 右栏: 上 DAG + 下实时提示词 */}
        <div className="flex min-w-0 flex-1 flex-col gap-3">
          <div className="flex flex-col gap-1.5">
            <span className="text-2xs text-subtle-foreground">
              {t("workflows.designer.graphTitle")}
            </span>
            <FlowGraphView graph={graph} />
          </div>
          <div className="flex min-h-0 flex-1 flex-col gap-1.5">
            <span className="text-2xs text-subtle-foreground">
              {t("workflows.designer.previewTitle")}
            </span>
            <div
              data-testid="designer-prompt-preview"
              className="min-h-0 flex-1 overflow-y-auto rounded-lg border border-border bg-card/40 px-3 py-2"
            >
              {preview.trim() ? (
                <MarkdownText text={preview} />
              ) : (
                <span className="text-2xs text-muted-foreground">
                  {t("workflows.designer.previewEmpty")}
                </span>
              )}
            </div>
          </div>
        </div>
      </div>

      {error ? (
        <div className="rounded-md border border-destructive bg-destructive-soft px-3 py-2 text-2xs text-destructive">
          {error}
        </div>
      ) : null}
    </div>
  );
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `cd frontend && pnpm test -- --run src/components/agentre/workflows/__tests__/workflow-dag-designer.test.tsx`
Expected: PASS(3 passed)。

- [ ] **Step 6: Verify i18n coverage still green**

Run: `cd frontend && pnpm test -- --run src/__tests__/i18n.test.ts`
Expected: PASS。

- [ ] **Step 7: Commit**

```bash
git commit frontend/src/components/agentre/workflows/workflow-dag-designer.tsx \
  frontend/src/components/agentre/workflows/__tests__/workflow-dag-designer.test.tsx \
  frontend/src/i18n/locales/en/common.json \
  frontend/src/i18n/locales/zh-CN/common.json \
  -m "✨ orchestration: 三栏 DAG 设计器(节点表单 + 实时 DAG + 实时提示词)"
```

---

### Task 5: 接进流程库管理器 `workflow-manager-dialog.tsx`

管理器加 `draftGraph` 态:新建 / 编辑有 graph 的流程 → 渲染设计器(模态加宽 ~1200px);legacy 无 graph 流程 → 保留自由文本表单 + 「转成 DAG」入口;保存时把 `graph` 串给 `use-workflows`。

**Files:**
- Modify: `frontend/src/components/agentre/workflows/workflow-manager-dialog.tsx`
- Test: `frontend/src/components/agentre/workflows/__tests__/workflow-manager-dialog.test.tsx`
- Modify: `frontend/src/i18n/locales/zh-CN/common.json`
- Modify: `frontend/src/i18n/locales/en/common.json`

**Interfaces:**
- Consumes: Task 2 `use-workflows`(`create/update` 增 `graph`;`WorkflowItem.graph`);Task 4 `WorkflowDagDesigner`;Task 1 `emptyDraftGraph/graphToJSON`;`parseFlowGraph`+`FlowGraph` from `../orchestration/flow-graph`。
- Produces: 无新导出(内部组件/状态)。新增 testid:`workflow-convert-dag`(自由文本表单里的转 DAG 按钮);设计器内 `designer-add-node` 等沿用。

> **Scope note(tags):** 设计器不编辑 `tags`。spec 把左栏 tags chips 标为「可选」,本片按 YAGNI 略去——避免把 `WorkflowEditorForm` 里的 chips 组件拆出来复制一份。已有流程带的 tags 在 `openEdit` 载入 `draftTags` 后原样透传给 `update`(不丢);设计器新建的流程 tags 为空。tags 是「给人看、不注入 AI」的展示元数据,不编辑不影响功能。

- [ ] **Step 1: Add i18n keys**

In `frontend/src/i18n/locales/en/common.json`, add to `workflows.designer`:

```json
      "convertToDag": "Convert to DAG designer",
      "convertHint": "Rebuild as a structured flow; saving replaces the free-text body with the generated one."
```

In `frontend/src/i18n/locales/zh-CN/common.json`, add to `workflows.designer`:

```json
      "convertToDag": "转成 DAG 设计器",
      "convertHint": "改用结构化流程重建;保存时会用投影正文替换当前自由文本正文。"
```

- [ ] **Step 2: Write the failing test**

Create `frontend/src/components/agentre/workflows/__tests__/workflow-manager-dialog.test.tsx`:

```tsx
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

// 管理器 → ViewPane/设计器 → MarkdownText → RichLink 间接 import wailsjs runtime
vi.mock("../../../../../wailsjs/runtime/runtime", async () => {
  const actual = await vi.importActual<
    typeof import("../../../../../wailsjs/runtime/runtime")
  >("../../../../../wailsjs/runtime/runtime");
  return { ...actual, BrowserOpenURL: vi.fn() };
});

const appMocks = vi.hoisted(() => ({
  WorkflowList: vi.fn(),
  WorkflowCreate: vi.fn(),
  WorkflowUpdate: vi.fn(),
  WorkflowDelete: vi.fn(),
  WorkflowPreviewGraph: vi.fn(),
}));
vi.mock("../../../../../wailsjs/go/app/App", () => appMocks);

import { useWorkflowManagerStore } from "../../../../stores/workflow-manager-store";
import { WorkflowManagerDialog } from "../workflow-manager-dialog";

const graphN1 = JSON.stringify({
  version: 1,
  nodes: [{ id: "n1", label: "Plan", kind: "leader" }],
  edges: [],
});

function listItem(over: Record<string, unknown> = {}) {
  return {
    id: 1,
    name: "Legacy",
    content: "free text",
    tags: [],
    outline: [],
    runCount: 0,
    createtime: 0,
    updatetime: 0,
    graph: "",
    ...over,
  };
}

describe("WorkflowManagerDialog DAG 设计器接入", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useWorkflowManagerStore.setState({ open: false, intent: "browse" });
    appMocks.WorkflowList.mockResolvedValue({ items: [] });
    appMocks.WorkflowCreate.mockResolvedValue({});
    appMocks.WorkflowUpdate.mockResolvedValue({});
    appMocks.WorkflowPreviewGraph.mockResolvedValue({ content: "", outline: [] });
  });

  it("新建流程直接进 DAG 设计器", async () => {
    useWorkflowManagerStore.getState().openCreate();
    render(<WorkflowManagerDialog />);
    expect(await screen.findByTestId("designer-add-node")).toBeInTheDocument();
  });

  it("保存设计器流程 → WorkflowCreate 带 graph(含节点)", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    useWorkflowManagerStore.getState().openCreate();
    render(<WorkflowManagerDialog />);
    fireEvent.change(await screen.findByTestId("workflow-name-input"), {
      target: { value: "My Flow" },
    });
    fireEvent.change(screen.getByTestId("node-n1-label"), {
      target: { value: "Kickoff" },
    });
    await user.click(screen.getByTestId("workflow-save-button"));
    await waitFor(() =>
      expect(appMocks.WorkflowCreate).toHaveBeenCalledWith(
        expect.objectContaining({
          name: "My Flow",
          graph: expect.stringContaining("Kickoff"),
        }),
      ),
    );
  });

  it("编辑 legacy 无 graph 流程 → 自由文本表单 + 转 DAG 入口, 点转换进设计器", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    appMocks.WorkflowList.mockResolvedValue({ items: [listItem()] });
    useWorkflowManagerStore.getState().openBrowse();
    render(<WorkflowManagerDialog />);
    await user.click(await screen.findByTestId("workflow-row-1"));
    await user.click(await screen.findByTestId("workflow-edit-button"));
    expect(await screen.findByTestId("workflow-content-input")).toBeInTheDocument();
    expect(screen.getByTestId("workflow-convert-dag")).toBeInTheDocument();
    await user.click(screen.getByTestId("workflow-convert-dag"));
    expect(await screen.findByTestId("designer-add-node")).toBeInTheDocument();
  });

  it("编辑有 graph 的流程 → 设计器载入其节点(flow-node-n1)", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    appMocks.WorkflowList.mockResolvedValue({
      items: [listItem({ id: 2, name: "DAG flow", graph: graphN1 })],
    });
    useWorkflowManagerStore.getState().openBrowse();
    render(<WorkflowManagerDialog />);
    await user.click(await screen.findByTestId("workflow-row-2"));
    await user.click(await screen.findByTestId("workflow-edit-button"));
    expect(await screen.findByTestId("flow-node-n1")).toBeInTheDocument();
  });
});
```

- [ ] **Step 3: Run test to verify it fails**

Run: `cd frontend && pnpm test -- --run src/components/agentre/workflows/__tests__/workflow-manager-dialog.test.tsx`
Expected: FAIL — `designer-add-node` / `workflow-convert-dag` 不存在(管理器尚未接入设计器)。

- [ ] **Step 4a: Add imports**

In `frontend/src/components/agentre/workflows/workflow-manager-dialog.tsx`, add after the existing `WorkflowEditorForm` import (line 18):

```tsx
import { WorkflowDagDesigner } from "./workflow-dag-designer";
import { emptyDraftGraph, graphToJSON } from "./flow-graph-draft";
import { parseFlowGraph, type FlowGraph } from "../orchestration/flow-graph";
```

- [ ] **Step 4b: Add draftGraph state**

After the `draftOutline` state (line 58), add:

```tsx
  const [draftGraph, setDraftGraph] = React.useState<FlowGraph | null>(null);
```

- [ ] **Step 4c: Seed draftGraph on create/edit**

In the `started` effect's `intent === "create"` branch (after `setDraftOutline([]);`), add:

```tsx
      setDraftGraph(emptyDraftGraph());
```

In `openCreate` (after `setDraftOutline([]);`), add:

```tsx
    setDraftGraph(emptyDraftGraph());
```

In `openEdit` (after `setDraftOutline(w.outline);`), add:

```tsx
    setDraftGraph(w.graph ? parseFlowGraph(w.graph) : null);
```

- [ ] **Step 4d: Widen canSave + submit to carry graph**

Replace the `canSave` line (line 113):

```tsx
  const canSave =
    !submitting &&
    draftName.trim().length > 0 &&
    (draftGraph
      ? draftGraph.nodes.length > 0 &&
        draftGraph.nodes.every((n) => n.label.trim().length > 0)
      : true);
```

In `submit`, replace the `if (editingId > 0) { ... } else { ... }` block (lines 119-131) with:

```tsx
      const graphStr = draftGraph ? graphToJSON(draftGraph) : "";
      if (editingId > 0) {
        await update(
          editingId,
          draftName.trim(),
          draftContent,
          draftTags,
          draftOutline,
          graphStr,
        );
        setSelectedId(editingId);
      } else {
        await create(
          draftName.trim(),
          draftContent,
          draftTags,
          draftOutline,
          graphStr,
        );
        setSelectedId(0);
      }
```

- [ ] **Step 4e: Widen the modal in designer mode**

Replace the `DialogContent` `className={cn(...)}` (lines 171-173) with:

```tsx
        className={cn(
          "flex h-[640px] max-h-[88vh] flex-col gap-0 overflow-hidden p-0",
          mode === "editor" && draftGraph
            ? "w-[1200px] max-w-[96vw]"
            : "w-[920px] max-w-[94vw]",
        )}
```

- [ ] **Step 4f: Branch editor → designer vs free-text**

Replace the `{mode === "editor" ? (<EditorPane .../>) : selected ? (...` block (lines 294-310) so the editor branch splits on `draftGraph`:

```tsx
            {mode === "editor" ? (
              draftGraph ? (
                <DesignerPane
                  editing={editingId > 0}
                  name={draftName}
                  graph={draftGraph}
                  error={formError}
                  canSave={canSave}
                  onNameChange={setDraftName}
                  onGraphChange={setDraftGraph}
                  onCancel={cancelEdit}
                  onSave={() => void submit()}
                  onKeyDown={onEditorKeyDown}
                />
              ) : (
                <EditorPane
                  editing={editingId > 0}
                  name={draftName}
                  content={draftContent}
                  tags={draftTags}
                  outline={draftOutline}
                  error={formError}
                  canSave={canSave}
                  onNameChange={setDraftName}
                  onContentChange={setDraftContent}
                  onTagsChange={setDraftTags}
                  onOutlineChange={setDraftOutline}
                  onConvertToDag={() => setDraftGraph(emptyDraftGraph())}
                  onCancel={cancelEdit}
                  onSave={() => void submit()}
                  onKeyDown={onEditorKeyDown}
                />
              )
            ) : selected ? (
```

- [ ] **Step 4g: Add onConvertToDag to EditorPane**

In `EditorPane`'s props type (the `{ editing; name; ... onKeyDown }` object type), add:

```tsx
  onConvertToDag: () => void;
```

and add `onConvertToDag,` to the destructured params list. Then, inside EditorPane's scroll body, immediately **before** `<WorkflowEditorForm`, insert the convert affordance:

```tsx
        <div className="mb-3 flex items-center justify-between gap-3 rounded-md border border-dashed border-border bg-muted/40 px-3 py-2">
          <span className="text-2xs text-muted-foreground">
            {t("workflows.designer.convertHint")}
          </span>
          <Button
            type="button"
            variant="outline"
            size="sm"
            data-testid="workflow-convert-dag"
            onClick={onConvertToDag}
          >
            {t("workflows.designer.convertToDag")}
          </Button>
        </div>
```

- [ ] **Step 4h: Add DesignerPane component**

At the end of the file (after `EditorPane`), add:

```tsx
function DesignerPane({
  editing,
  name,
  graph,
  error,
  canSave,
  onNameChange,
  onGraphChange,
  onCancel,
  onSave,
  onKeyDown,
}: {
  editing: boolean;
  name: string;
  graph: FlowGraph;
  error: string | null;
  canSave: boolean;
  onNameChange: (v: string) => void;
  onGraphChange: (g: FlowGraph) => void;
  onCancel: () => void;
  onSave: () => void;
  onKeyDown: (e: React.KeyboardEvent) => void;
}) {
  const { t } = useTranslation();
  return (
    <div className="flex min-h-0 flex-1 flex-col" onKeyDown={onKeyDown}>
      <header className="flex items-center gap-2.5 border-b border-border px-5 py-3">
        <span className="flex size-7 shrink-0 items-center justify-center rounded-md bg-primary-soft">
          <Pencil className="size-4 text-primary-text" aria-hidden="true" />
        </span>
        <h2 className="text-sm font-semibold text-foreground">
          {editing
            ? t("workflows.editor.editTitle")
            : t("workflows.editor.createTitle")}
        </h2>
      </header>
      <div className="flex min-h-0 flex-1 flex-col overflow-hidden px-5 py-4">
        <WorkflowDagDesigner
          name={name}
          graph={graph}
          error={error}
          onNameChange={onNameChange}
          onGraphChange={onGraphChange}
        />
      </div>
      <footer className="flex items-center gap-2 border-t border-border px-5 py-3">
        <span className="text-2xs text-muted-foreground">
          {t("workflows.manager.saveHint")}
        </span>
        <div className="flex-1" />
        <Button type="button" variant="outline" size="sm" onClick={onCancel}>
          {t("common.cancel")}
        </Button>
        <Button
          type="button"
          size="sm"
          disabled={!canSave}
          data-testid="workflow-save-button"
          onClick={onSave}
        >
          <Check className="size-3.5" aria-hidden="true" />
          {t("workflows.editor.save")}
        </Button>
      </footer>
    </div>
  );
}
```

(`Pencil`, `Check`, `Button`, `useTranslation` are already imported in this file.)

- [ ] **Step 5: Run test to verify it passes**

Run: `cd frontend && pnpm test -- --run src/components/agentre/workflows/__tests__/workflow-manager-dialog.test.tsx`
Expected: PASS(4 passed)。

- [ ] **Step 6: Verify i18n coverage still green**

Run: `cd frontend && pnpm test -- --run src/__tests__/i18n.test.ts`
Expected: PASS。

- [ ] **Step 7: Commit**

```bash
git commit frontend/src/components/agentre/workflows/workflow-manager-dialog.tsx \
  frontend/src/components/agentre/workflows/__tests__/workflow-manager-dialog.test.tsx \
  frontend/src/i18n/locales/en/common.json \
  frontend/src/i18n/locales/zh-CN/common.json \
  -m "✨ orchestration: 流程库管理器接入 DAG 设计器(新建/编辑/legacy 转换/保存串 graph)"
```

---

### Task 6: 收尾全量 gate

跑全套前端 gate 看真 exit code:tsc(类型)、eslint(含 `i18next/no-literal-string`)、全量 vitest(含 i18n 覆盖 + 既有 workflows/orchestration 测试无回归)。后端未改,无需 backend gate。

**Files:**
- 无新增(仅在发现回归时回到对应任务修复)。

- [ ] **Step 1: TypeScript 全量类型检查**

Run:
```bash
cd frontend && pnpm exec tsc --noEmit > /tmp/p2-tsc.txt 2>&1; grep -c "error TS" /tmp/p2-tsc.txt
```
Expected: `0`。若非 0,`cat /tmp/p2-tsc.txt` 看具体错误并回到引入错误的任务修复(常见:node-form 的 `FlowKind` 断言、designer 的 `NonNullable` 过滤器写法)。

- [ ] **Step 2: ESLint(新增/改动文件)**

Run:
```bash
cd frontend && pnpm exec eslint \
  src/components/agentre/workflows \
  src/hooks/use-workflows.ts \
  > /tmp/p2-eslint.txt 2>&1; echo "eslint_exit=$?"; tail -30 /tmp/p2-eslint.txt
```
Expected: `eslint_exit=0`,无 `i18next/no-literal-string`(节点 id / `"__none__"` sentinel 在 JS 表达式里不算 JSX 文案)、无 `no-unused-vars`。

- [ ] **Step 3: 全量 Vitest**

Run:
```bash
cd frontend && pnpm test -- --run > /tmp/p2-vitest.txt 2>&1; echo "vitest_exit=$?"; tail -25 /tmp/p2-vitest.txt
```
Expected: `vitest_exit=0`,全部测试通过(新增 4 个测试文件 + i18n 覆盖 + 既有 `run-new-dialog`/`flow-graph`/`run-flow-blueprint` 无回归)。若 `run-new-dialog.test.tsx` 报错,检查是否误改了 `WorkflowItem` 投影类型顺序影响到别处。

- [ ] **Step 4: 复述结果**

在给用户的收尾汇报里,如实写出三个 gate 的真实 exit code 与通过数;若某 gate 失败,不得标记完成——回到对应任务修复后重跑。

---

## 交付方式

沿用 Phase 1 的 Subagent-Driven:每 Task 一 implementer + 逐任务两段复审(spec 合规 + 代码质量)+ 全分支终审(最强模型),合入 develop/wyz。Task 1/2/3 偏机械(纯函数 / 小改 / 受控组件,plan 含完整代码)→ 便宜模型即可;Task 4/5 有集成判断(设计器组装 + 管理器分流)→ 标准模型;终审用最强模型。
