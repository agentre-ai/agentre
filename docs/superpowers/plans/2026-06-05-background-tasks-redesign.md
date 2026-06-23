# 后台任务面板重设计(对齐设计稿 + 真 CLI 校正)Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让「后台任务」胶囊/弹层与设计稿 `agentry.pen` 帧 `w3qQYz`(Agent Chat — 后台任务 · A 头部胶囊弹层)一致,并为 `run_in_background` Bash 工具卡补一个「后台运行 · <task_id>」内联标识。

**Architecture:** 纯前端改动。面板只收 `local_bash`(run_in_background bash),排除所有 subagent(`local_agent`)。运行中 + 已完成都展示;「清理已完成」把对应 `toolUseId` 写进一个 localStorage 持久化的 Zustand store,`deriveBackgroundTasks` 据此过滤,重载后仍生效。`task_id` 已经端到端透传到前端 `subagent.taskId`,直接展示真实不可读串(不合成 bg1)。

**Tech Stack:** React 19 + TypeScript + Vite + Tailwind v4 + Zustand 5(`persist` middleware)+ react-i18next + Vitest。

---

## 背景:真 CLI 校正(2.1.165 实测,务必先读)

实测 `claude --output-format stream-json` 帧(capture 在 `/tmp/cc-capture/{bg,sub}.jsonl`):

- **run_in_background Bash**:Bash tool_use 的 `input.run_in_background === true`;CLI 发 `task_started{task_type:"local_bash", description}` → `task_updated` → `task_notification{status:"completed", output_file:".../tasks/<id>.output", summary:"Background command \"…\" completed (exit code 0)"}`。完成后 CLI 自主跑一轮(第 2 个 `result` 帧)。
- **subagent(Agent 工具)**:发 `task_started{task_type:"local_agent", subagent_type}` → `task_progress` → `task_notification{status:"completed", output_file:""}`。**前台 subagent(parent 等待)发的帧与后台完全相同,无 `run_in_background` 入参,无法区分前台/后台。**
- **`task_id` 是不可读串**(`b3875slp0` / `a1e8e0e8a87c06e81`),设计稿里的 `bg1` 是 mock。

**两条产品决策(用户已拍板):**
1. 面板**只收 `local_bash`**,完全排除 subagent。
2. **直接展示真实 `task_id`**,不合成友好编号。

**已验证的数据通路(无需改后端):**
- `rawFrame.task_id` → `SubagentMeta.TaskID`(session.go:893)→ `SubagentInfo.TaskID`(translator.go:319 区域)→ `SubagentStateBlock.TaskID` → `ChatBlockSubagent.TaskID`(json `taskId`,chat.go:730 / types.go:316;round-trip 测试 chat_internal_test.go:177/198)。前端 `ChatBlockSubagent.taskId` 已存在。
- `.subagent`(`kind:"local_bash"`)只挂在 run_in_background Bash + subagent 的 tool_use 块上,普通 Read/Edit/Bash 无 `.subagent`。

**范围内**:胶囊+弹层对齐、内联 Bash 卡「后台运行」标识、清理已完成(持久化)、i18n、测试。
**范围外(本次不做)**:自主轮 assistant 消息上的「自动」徽标(`AutoTriggerBanner` 已标记自主轮,徽标是冗余 polish);后端持久化清理(localStorage 已满足「重载不复现」);停止/查看 output;远端转发。

---

## File Structure

| 文件 | 责任 | 动作 |
| --- | --- | --- |
| `frontend/src/components/agentre/background-tasks/types.ts` | `BackgroundTask` 类型 | Modify:加 `taskId` |
| `frontend/src/components/agentre/background-tasks/derive.ts` | 从消息/live 块 derive | Modify:只收 local_bash、加 taskId、过滤已清理 |
| `frontend/src/components/agentre/background-tasks/derive.test.ts` | derive 单测 | Modify:改 subagent 用例、加 taskId/cleared 用例 |
| `frontend/src/stores/cleared-background-tasks-store.ts` | 已清理 toolUseId 的持久化集合 | **Create** |
| `frontend/src/stores/__tests__/cleared-background-tasks-store.test.ts` | store 单测 | **Create** |
| `frontend/src/components/agentre/background-tasks/background-tasks-popover.tsx` | 弹层 UI | Modify:头部 badge + 清理已完成 + 行 chrome + taskId |
| `frontend/src/components/agentre/background-tasks/background-tasks-chip.tsx` | 头部胶囊 | Modify:透传 `onClearCompleted` |
| `frontend/src/components/agentre/background-tasks/background-tasks-chip.test.tsx` | 胶囊/弹层单测 | Modify:对齐新结构 + 清理交互 |
| `frontend/src/components/agentre/chat-panel.tsx` | 面板消费方 | Modify:过滤已清理 + 接 store + 传 `onClearCompleted` |
| `frontend/src/components/agentre/canonical-tool/raw/card.tsx` | Bash/兜底工具卡 | Modify:run_in_background 时加「后台运行 · id」pill |
| `frontend/src/components/agentre/canonical-tool/raw/card.test.tsx` | 卡片单测 | Modify/Create:后台运行 pill 用例 |
| `frontend/src/i18n/locales/{zh-CN,en}/common.json` | 文案 | Modify:`clearCompleted` + `canonical.raw.backgroundRunning` |

---

## Task 1: derive — 只收 local_bash + 透传 taskId + 过滤已清理

**Files:**
- Modify: `frontend/src/components/agentre/background-tasks/types.ts`
- Modify: `frontend/src/components/agentre/background-tasks/derive.ts`
- Test: `frontend/src/components/agentre/background-tasks/derive.test.ts`

- [ ] **Step 1: 写失败测试(改既有 + 新增)**

在 `derive.test.ts` 里:把所有断言「local_agent / subagent 出现在结果」的用例改成断言它们被排除;新增 taskId + cleared 用例。新增/替换以下用例(保留文件已有 import 与 `makeMessage`/`makeBlock` helper;若 helper 名不同按现有为准):

```ts
it("excludes local_agent subagents — only run_in_background bash is shown", () => {
  const messages = [
    makeMessage(1000, [
      makeBlock("tool_use", "tu-bash", { kind: "local_bash", status: "running", taskDescription: "sleep 5" }),
      makeBlock("tool_use", "tu-agent", { kind: "local_agent", status: "running", taskDescription: "Explore" }),
    ]),
  ];
  const tasks = deriveBackgroundTasks(messages, []);
  expect(tasks.map((t) => t.toolUseId)).toEqual(["tu-bash"]);
  expect(tasks[0].kind).toBe("local_bash");
});

it("carries the real task_id through to BackgroundTask.taskId", () => {
  const messages = [
    makeMessage(1000, [
      makeBlock("tool_use", "tu-bash", { kind: "local_bash", status: "running", taskId: "b3875slp0" }),
    ]),
  ];
  const tasks = deriveBackgroundTasks(messages, []);
  expect(tasks[0].taskId).toBe("b3875slp0");
});

it("filters out cleared toolUseIds", () => {
  const messages = [
    makeMessage(1000, [
      makeBlock("tool_use", "tu-a", { kind: "local_bash", status: "completed" }),
      makeBlock("tool_use", "tu-b", { kind: "local_bash", status: "running" }),
    ]),
  ];
  const tasks = deriveBackgroundTasks(messages, [], new Set(["tu-a"]));
  expect(tasks.map((t) => t.toolUseId)).toEqual(["tu-b"]);
});
```

> `makeBlock(type, toolUseId, subagent)` 需产出 `{ type, toolUseId, subagent }` 形状(对齐 `VisitableBlock`)。若现有 helper 不接受 `subagent`,在此文件内补一个本地 helper:
> ```ts
> const makeBlock = (type: string, toolUseId: string, subagent: Record<string, unknown>) =>
>   ({ type, toolUseId, subagent }) as unknown as chat_svc.ChatBlock;
> const makeMessage = (createtime: number, blocks: chat_svc.ChatBlock[]) =>
>   ({ createtime, blocks }) as unknown as chat_svc.ChatMessage;
> ```

- [ ] **Step 2: 跑测试看失败**

Run: `cd frontend && pnpm test -- src/components/agentre/background-tasks/derive.test.ts`
Expected: FAIL — 新用例里 `tu-agent` 仍出现 / `taskId` 为 undefined / cleared 未过滤。

- [ ] **Step 3: 改类型**

`types.ts` 的 `BackgroundTask` 加字段(放在 `description` 后):

```ts
export interface BackgroundTask {
  toolUseId: string;
  taskId?: string; // 真实 CLI task_id(不可读串,如 b3875slp0)— 直接展示
  kind: BackgroundTaskKind;
  description: string;
  status: BackgroundTaskStatus;
  startedAt?: number;
  durationMs?: number;
  summary?: string;
}
```

- [ ] **Step 4: 改 derive**

`derive.ts`:签名加第 3 参 `clearedToolUseIds`;`visit` 里**只收 local_bash**、跳过已清理、写入 `taskId`。完整替换 `deriveBackgroundTasks` + `visit`:

```ts
export function deriveBackgroundTasks(
  messages: chat_svc.ChatMessage[],
  liveBlocks: ChatBlockData[],
  clearedToolUseIds?: ReadonlySet<string>,
): BackgroundTask[] {
  const byId = new Map<string, BackgroundTask>();

  const visit = (block: VisitableBlock | undefined, startedAt?: number) => {
    if (!block || block.type !== "tool_use") return;
    const sa = block.subagent;
    const toolUseId = block.toolUseId;
    if (!toolUseId || !sa) return;
    // 只收 run_in_background bash;subagent(local_agent)整体排除(真 CLI 无法区分
    // 前台/后台 subagent,产品决策只展示真正后台的 bash 任务)。
    if (sa.kind !== "local_bash") return;
    if (clearedToolUseIds?.has(toolUseId)) return;
    const prev = byId.get(toolUseId);
    byId.set(toolUseId, {
      toolUseId,
      taskId: sa.taskId || prev?.taskId,
      kind: "local_bash",
      description: sa.taskDescription ?? prev?.description ?? "",
      status: mapStatus(sa.status),
      startedAt: startedAt ?? prev?.startedAt,
      durationMs:
        (typeof sa.durationMs === "number" ? sa.durationMs : undefined) ??
        prev?.durationMs,
      summary: (sa.summary || undefined) ?? prev?.summary,
    });
  };

  for (const m of messages) {
    for (const b of m.blocks ?? [])
      visit(b as unknown as VisitableBlock, m.createtime);
  }
  for (const b of liveBlocks) visit(b);

  return [...byId.values()];
}
```

> 删除现在用不到的 `mapKind`(local_bash 写死)。`VisitableBlock` 加 `taskId?: string`? 不需要 —— `taskId` 在 `chat_svc.ChatBlockSubagent` 上,`sa.taskId` 已可读。保留 `mapStatus` 不变。

- [ ] **Step 5: 跑测试看通过**

Run: `cd frontend && pnpm test -- src/components/agentre/background-tasks/derive.test.ts`
Expected: PASS(全部用例)。

- [ ] **Step 6: Commit**

```bash
git add frontend/src/components/agentre/background-tasks/types.ts \
        frontend/src/components/agentre/background-tasks/derive.ts \
        frontend/src/components/agentre/background-tasks/derive.test.ts
git commit -m "♻️ background-tasks: derive 只收 local_bash + 透传 taskId + 支持过滤已清理"
```

---

## Task 2: 已清理任务持久化 store

**Files:**
- Create: `frontend/src/stores/cleared-background-tasks-store.ts`
- Test: `frontend/src/stores/__tests__/cleared-background-tasks-store.test.ts`

- [ ] **Step 1: 写失败测试**

```ts
import { beforeEach, describe, expect, it } from "vitest";

import { useClearedBackgroundTasksStore } from "../cleared-background-tasks-store";

describe("cleared-background-tasks-store", () => {
  beforeEach(() => {
    useClearedBackgroundTasksStore.setState({ cleared: {} });
    localStorage.clear();
  });

  it("records cleared toolUseIds per session", () => {
    useClearedBackgroundTasksStore.getState().clearCompleted(7, ["a", "b"]);
    expect(useClearedBackgroundTasksStore.getState().cleared[7]).toEqual(["a", "b"]);
  });

  it("merges + dedupes on repeated clears", () => {
    const { clearCompleted } = useClearedBackgroundTasksStore.getState();
    clearCompleted(7, ["a"]);
    clearCompleted(7, ["a", "c"]);
    expect(useClearedBackgroundTasksStore.getState().cleared[7]).toEqual(["a", "c"]);
  });

  it("keeps sessions independent", () => {
    const { clearCompleted } = useClearedBackgroundTasksStore.getState();
    clearCompleted(7, ["a"]);
    clearCompleted(8, ["b"]);
    expect(useClearedBackgroundTasksStore.getState().cleared[7]).toEqual(["a"]);
    expect(useClearedBackgroundTasksStore.getState().cleared[8]).toEqual(["b"]);
  });

  it("ignores empty clears", () => {
    useClearedBackgroundTasksStore.getState().clearCompleted(7, []);
    expect(useClearedBackgroundTasksStore.getState().cleared[7]).toBeUndefined();
  });
});
```

- [ ] **Step 2: 跑测试看失败**

Run: `cd frontend && pnpm test -- src/stores/__tests__/cleared-background-tasks-store.test.ts`
Expected: FAIL — 模块不存在。

- [ ] **Step 3: 写实现**

`cleared-background-tasks-store.ts`(对齐 `chat-sidebar-store.ts` 的 persist 写法):

```ts
import { create } from "zustand";
import { persist } from "zustand/middleware";

type ClearedBackgroundTasksState = {
  // sessionId -> 已清理(dismiss)的 toolUseId 列表
  cleared: Record<number, string[]>;
  clearCompleted: (sessionId: number, toolUseIds: string[]) => void;
};

export const useClearedBackgroundTasksStore =
  create<ClearedBackgroundTasksState>()(
    persist(
      (set) => ({
        cleared: {},
        clearCompleted: (sessionId, toolUseIds) => {
          if (toolUseIds.length === 0) return;
          set((state) => {
            const prev = state.cleared[sessionId] ?? [];
            const merged = [...new Set([...prev, ...toolUseIds])];
            return { cleared: { ...state.cleared, [sessionId]: merged } };
          });
        },
      }),
      { name: "cleared-background-tasks" },
    ),
  );
```

- [ ] **Step 4: 跑测试看通过**

Run: `cd frontend && pnpm test -- src/stores/__tests__/cleared-background-tasks-store.test.ts`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add frontend/src/stores/cleared-background-tasks-store.ts \
        frontend/src/stores/__tests__/cleared-background-tasks-store.test.ts
git commit -m "✨ background-tasks: 已清理任务的 localStorage 持久化 store"
```

---

## Task 3: i18n 文案

**Files:**
- Modify: `frontend/src/i18n/locales/zh-CN/common.json`
- Modify: `frontend/src/i18n/locales/en/common.json`

- [ ] **Step 1: 加 key(两个 locale 都加,保持结构一致)**

`zh-CN/common.json` 的 `chatPanel.backgroundTasks` 对象加:

```json
"clearCompleted": "清理已完成"
```

`zh-CN/common.json` 的 `canonical.raw` 对象加:

```json
"backgroundRunning": "后台运行"
```

`en/common.json` 对应:

```json
"clearCompleted": "Clear completed"
```
```json
"backgroundRunning": "Background"
```

- [ ] **Step 2: 跑 i18n 覆盖测试**

Run: `cd frontend && pnpm test -- src/__tests__/i18n.test.ts`
Expected: PASS(两 locale key 对齐;无缺失)。

- [ ] **Step 3: Commit**

```bash
git add frontend/src/i18n/locales/zh-CN/common.json frontend/src/i18n/locales/en/common.json
git commit -m "🌐 background-tasks: 清理已完成 + 后台运行 文案"
```

---

## Task 4: 弹层重设计(头部 badge + 清理已完成 + 行 chrome + taskId)

**Files:**
- Modify: `frontend/src/components/agentre/background-tasks/background-tasks-popover.tsx`
- Test: `frontend/src/components/agentre/background-tasks/background-tasks-chip.test.tsx`(弹层经胶囊渲染,测试在此文件)

设计稿对齐点(帧 `iNSW3`):
- 头部 `popHeader`:标题「后台任务」+ 绿色 badge「{N} 运行中」(运行中计数)+ 右侧「清理已完成」(仅当有已完成/失败任务时可点)。
- 行 `TaskRow`:左侧 24×24 圆角彩色图标容器(bash:浅蓝底 `#eef4fa` + 终端图标 `#3b6896`)+ 描述 + meta「bash · {taskId} · {elapsed}」+ 右侧状态 pill(运行中 绿 / 已完成 灰 / 失败 红)。

- [ ] **Step 1: 写失败测试**

在 `background-tasks-chip.test.tsx` 末尾追加(沿用文件已有的 render helper / `renderChip(tasks)`;若没有,用 `render(<BackgroundTasksChip tasks={tasks} onClearCompleted={onClear} />)` 并 `userEvent.click` 打开 popover):

```ts
it("shows the running-count badge and the task_id in the row", async () => {
  const onClear = vi.fn();
  render(
    <BackgroundTasksChip
      tasks={[
        { toolUseId: "tu1", taskId: "b3875slp0", kind: "local_bash", description: "sleep 5", status: "running", startedAt: Date.now() },
        { toolUseId: "tu2", taskId: "c9xyz", kind: "local_bash", description: "build", status: "completed", durationMs: 20000 },
      ]}
      onClearCompleted={onClear}
    />,
  );
  await userEvent.click(screen.getByRole("button", { name: /后台任务|background/i }));
  expect(screen.getByText("b3875slp0")).toBeInTheDocument();
  // 运行中计数 badge = 1
  expect(screen.getByText(/1 运行中|1 running/)).toBeInTheDocument();
});

it("clears completed tasks via 清理已完成", async () => {
  const onClear = vi.fn();
  render(
    <BackgroundTasksChip
      tasks={[
        { toolUseId: "tu1", taskId: "id1", kind: "local_bash", description: "sleep", status: "running", startedAt: Date.now() },
        { toolUseId: "tu2", taskId: "id2", kind: "local_bash", description: "build", status: "completed", durationMs: 1000 },
      ]}
      onClearCompleted={onClear}
    />,
  );
  await userEvent.click(screen.getByRole("button", { name: /后台任务|background/i }));
  await userEvent.click(screen.getByRole("button", { name: /清理已完成|clear completed/i }));
  expect(onClear).toHaveBeenCalledTimes(1);
});
```

> 文件顶部确保 import 了 `userEvent`(`@testing-library/user-event`)和 `vi`。胶囊触发按钮的 aria-label 来自 `t("chatPanel.backgroundTasks.aria")`=「后台任务」。

- [ ] **Step 2: 跑测试看失败**

Run: `cd frontend && pnpm test -- src/components/agentre/background-tasks/background-tasks-chip.test.tsx`
Expected: FAIL — `onClearCompleted` 未支持 / 无「清理已完成」按钮 / 行无 taskId。

- [ ] **Step 3: 改弹层实现**

`background-tasks-popover.tsx` —— 加 `onClearCompleted` prop、头部 badge + 清理按钮、行 chrome + taskId。完整替换 `BackgroundTasksPopoverContent`(保留文件内 `formatElapsed` 与 `StatusPill` 不变):

```tsx
type BackgroundTasksPopoverContentProps = {
  tasks: BackgroundTask[];
  onClearCompleted?: () => void;
};

export function BackgroundTasksPopoverContent({
  tasks,
  onClearCompleted,
}: BackgroundTasksPopoverContentProps) {
  const { t } = useTranslation();

  const [now, setNow] = React.useState(() => Date.now());
  const hasLiveElapsed = tasks.some(
    (task) => task.status === "running" && task.startedAt != null,
  );
  React.useEffect(() => {
    if (!hasLiveElapsed) return;
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, [hasLiveElapsed]);

  const runningCount = tasks.filter((tk) => tk.status === "running").length;
  const hasCompleted = tasks.some(
    (tk) => tk.status === "completed" || tk.status === "failed",
  );

  return (
    <div className="flex min-w-[260px] max-w-[400px] flex-col gap-2">
      {/* 头部:标题 + 运行中计数 badge + 清理已完成 */}
      <div className="flex items-center gap-2">
        <span className="text-xs font-semibold text-foreground">
          {t("chatPanel.backgroundTasks.title")}
        </span>
        {runningCount > 0 && (
          <span className="inline-flex items-center gap-1 rounded-full bg-emerald-50 px-1.5 py-0.5 font-mono text-[10px] font-medium text-emerald-700 dark:bg-emerald-500/15 dark:text-emerald-400">
            <span className="inline-block size-1.5 rounded-full bg-emerald-500" aria-hidden="true" />
            {t("chatPanel.backgroundTasks.chip", { count: runningCount })}
          </span>
        )}
        <span className="min-w-0 flex-1" />
        {hasCompleted && onClearCompleted && (
          <button
            type="button"
            onClick={onClearCompleted}
            className="shrink-0 text-[11px] text-muted-foreground transition-colors hover:text-foreground"
          >
            {t("chatPanel.backgroundTasks.clearCompleted")}
          </button>
        )}
      </div>
      {tasks.length === 0 ? (
        <p className="text-xs text-muted-foreground">
          {t("chatPanel.backgroundTasks.empty")}
        </p>
      ) : (
        <ul className="flex flex-col gap-1.5">
          {tasks.map((task) => {
            let elapsedLabel: string | undefined;
            if (task.status === "running" && task.startedAt != null) {
              elapsedLabel = formatElapsed(now - task.startedAt);
            } else if (
              (task.status === "completed" || task.status === "failed") &&
              task.durationMs != null &&
              task.durationMs > 0
            ) {
              elapsedLabel = formatElapsed(task.durationMs);
            }

            return (
              <li key={task.toolUseId} className="flex items-start gap-2.5">
                {/* 圆角彩色图标容器(bash) */}
                <span
                  className="mt-0.5 inline-flex size-6 shrink-0 items-center justify-center rounded-md bg-sky-50 text-sky-700 dark:bg-sky-500/15 dark:text-sky-300"
                  aria-hidden="true"
                >
                  <Terminal className="size-3.5" />
                </span>
                <div className="min-w-0 flex-1">
                  <p className="break-words text-xs leading-snug text-foreground">
                    {task.description || " "}
                  </p>
                  <div className="mt-0.5 flex items-center gap-1.5">
                    <span className="font-mono text-[10px] text-muted-foreground">
                      {t("chatPanel.backgroundTasks.bash")}
                    </span>
                    {task.taskId && (
                      <>
                        <span className="font-mono text-[10px] text-muted-foreground/50">·</span>
                        <span className="font-mono text-[10px] text-muted-foreground" data-testid="task-id">
                          {task.taskId}
                        </span>
                      </>
                    )}
                    <StatusPill status={task.status} />
                    {elapsedLabel != null && (
                      <span
                        className="ml-auto font-mono text-[10px] tabular-nums text-muted-foreground"
                        data-testid="elapsed"
                      >
                        {elapsedLabel}
                      </span>
                    )}
                  </div>
                  {task.summary && (
                    <p className="mt-0.5 break-words text-[10px] text-muted-foreground">
                      {task.summary}
                    </p>
                  )}
                </div>
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}
```

> `Bot` 图标 import 可删(面板不再有 subagent 行);保留 `Terminal`。若 lint 报 `Bot` 未使用,从第 1 行 import 移除。

- [ ] **Step 4: 跑测试看通过**

Run: `cd frontend && pnpm test -- src/components/agentre/background-tasks/background-tasks-chip.test.tsx`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/agentre/background-tasks/background-tasks-popover.tsx \
        frontend/src/components/agentre/background-tasks/background-tasks-chip.test.tsx
git commit -m "💄 background-tasks: 弹层对齐设计稿(运行中 badge + 清理已完成 + 行 chrome + taskId)"
```

---

## Task 5: 胶囊透传 onClearCompleted

**Files:**
- Modify: `frontend/src/components/agentre/background-tasks/background-tasks-chip.tsx`

- [ ] **Step 1: 改胶囊 props,透传回调到弹层**

`background-tasks-chip.tsx`:

```tsx
type BackgroundTasksChipProps = {
  tasks: BackgroundTask[];
  onClearCompleted?: () => void;
};

export function BackgroundTasksChip({ tasks, onClearCompleted }: BackgroundTasksChipProps) {
```

并把 `<BackgroundTasksPopoverContent tasks={tasks} />` 改为:

```tsx
<BackgroundTasksPopoverContent tasks={tasks} onClearCompleted={onClearCompleted} />
```

- [ ] **Step 2: 跑相关测试看通过**

Run: `cd frontend && pnpm test -- src/components/agentre/background-tasks/`
Expected: PASS(Task 4 的清理用例此时全绿)。

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/agentre/background-tasks/background-tasks-chip.tsx
git commit -m "♻️ background-tasks: 胶囊透传 onClearCompleted 到弹层"
```

---

## Task 6: chat-panel 接 store(过滤已清理 + 触发清理)

**Files:**
- Modify: `frontend/src/components/agentre/chat-panel.tsx`

- [ ] **Step 1: 引入 store + 过滤已清理 + 组装 onClearCompleted**

顶部 import 加:

```tsx
import { useClearedBackgroundTasksStore } from "@/stores/cleared-background-tasks-store";
```

模块级常量(文件顶部,组件外)加一个稳定空数组,避免 selector 每次新引用:

```tsx
const EMPTY_CLEARED: string[] = [];
```

把现有 `backgroundTasks` 的 `useMemo`(约 552-555 行)替换为:

```tsx
const clearedList = useClearedBackgroundTasksStore((s) =>
  sessionId != null ? (s.cleared[sessionId] ?? EMPTY_CLEARED) : EMPTY_CLEARED,
);
const clearedSet = React.useMemo(() => new Set(clearedList), [clearedList]);
const backgroundTasks = React.useMemo(
  () =>
    deriveBackgroundTasks(messages, currentStream?.liveBlocks ?? [], clearedSet),
  [messages, currentStream?.liveBlocks, clearedSet],
);
const clearCompletedTasks = useClearedBackgroundTasksStore((s) => s.clearCompleted);
const handleClearCompleted = React.useCallback(() => {
  if (sessionId == null) return;
  const doneIds = backgroundTasks
    .filter((tk) => tk.status === "completed" || tk.status === "failed")
    .map((tk) => tk.toolUseId);
  clearCompletedTasks(sessionId, doneIds);
}, [sessionId, backgroundTasks, clearCompletedTasks]);
```

> `sessionId` 的类型按 chat-panel 现状(`number | undefined`/`null`);用 `!= null` 守卫即可。若 `sessionId` 实际命名不同(如 `currentSessionId`),按现有变量名替换。

把渲染处(约 1247 行)改为:

```tsx
<BackgroundTasksChip tasks={backgroundTasks} onClearCompleted={handleClearCompleted} />
```

- [ ] **Step 2: 类型检查 + 跑面板相关测试**

Run: `cd frontend && pnpm tsc --noEmit`
Expected: 无新增类型错误。

Run: `cd frontend && pnpm test -- src/components/agentre/chat-panel`
Expected: PASS(若存在 chat-panel 测试;无则跳过此条,靠 tsc + 下面整体跑)。

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/agentre/chat-panel.tsx
git commit -m "✨ background-tasks: chat-panel 接清理 store(过滤已清理 + 清理已完成持久化)"
```

---

## Task 7: 内联「后台运行 · task_id」标识(Bash 卡)

**Files:**
- Modify: `frontend/src/components/agentre/canonical-tool/raw/card.tsx`
- Test: `frontend/src/components/agentre/canonical-tool/raw/card.test.tsx`(无则 Create)

设计稿对齐点(帧 `WoItS` BgTaskCard 的 `BgPill`):run_in_background Bash 卡头部多一个胶囊「后台运行 · {task_id}」(运行中 loader 图标;完成后保持标识)。数据:`input.run_in_background === true` 判定;`task_id` 取 `toolBlock.subagent?.taskId`。

- [ ] **Step 1: 写失败测试**

`raw/card.test.tsx`(若文件不存在则创建;参考同目录其他卡片测试的 render 包装 / i18n provider 写法):

```tsx
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { RawToolCard } from "./card";
// 注意:RawToolCard 用了 useTranslation,测试需包到项目既有的 i18n test wrapper。
// 若仓库有 `renderWithI18n` helper 用它;否则在 setup 里已全局初始化 i18n。

const bashBlock = (extra: Record<string, unknown>) =>
  ({
    type: "tool_use",
    toolName: "Bash",
    toolUseId: "tu1",
    toolInput: { command: "sleep 5", run_in_background: true },
    ...extra,
  }) as never;

describe("RawToolCard — background bash", () => {
  it("shows 后台运行 + task_id pill when run_in_background", () => {
    render(
      <RawToolCard
        toolBlock={bashBlock({ subagent: { kind: "local_bash", taskId: "b3875slp0", status: "running" } })}
        resultBlock={undefined}
        cwd="/tmp"
        sessionId={1}
      />,
    );
    expect(screen.getByText(/后台运行|Background/)).toBeInTheDocument();
    expect(screen.getByText("b3875slp0")).toBeInTheDocument();
  });

  it("does NOT show the pill for a normal foreground bash", () => {
    render(
      <RawToolCard
        toolBlock={
          ({ type: "tool_use", toolName: "Bash", toolUseId: "tu2", toolInput: { command: "ls" } }) as never
        }
        resultBlock={undefined}
        cwd="/tmp"
        sessionId={1}
      />,
    );
    expect(screen.queryByText(/后台运行|Background/)).not.toBeInTheDocument();
  });
});
```

- [ ] **Step 2: 跑测试看失败**

Run: `cd frontend && pnpm test -- src/components/agentre/canonical-tool/raw/card.test.tsx`
Expected: FAIL — 无「后台运行」pill。

- [ ] **Step 3: 改 card.tsx**

在 `RawToolCard` 顶部(`const input = ...` 之后)加判定:

```tsx
  const isBackground = input?.run_in_background === true;
  const bgTaskId =
    (toolBlock as ChatBlockData).subagent?.taskId ?? undefined;
```

import 加 `Loader` 风格图标 —— 复用已 import 的 `LoaderCircle`。在头部按钮内、`allowedBadge` 之前(`<span className="min-w-0 flex-1" />` 之后)插入 pill:

```tsx
        {isBackground && (
          <span
            data-testid="bg-running-pill"
            className="inline-flex shrink-0 items-center gap-1 rounded-full border border-border bg-muted px-1.5 py-0.5 text-[9px] font-medium text-muted-foreground"
          >
            <LoaderCircle
              className={cn("size-2.5", !hasResult && "animate-spin")}
              aria-hidden="true"
            />
            {t("canonical.raw.backgroundRunning")}
            {bgTaskId && (
              <>
                <span className="opacity-50">·</span>
                <span className="font-mono">{bgTaskId}</span>
              </>
            )}
          </span>
        )}
```

> `ChatBlockData` 已在第 13 行 import。`subagent` 字段在 `ChatBlockData` 上(`Omit<chat_svc.ChatBlock,...>` 含 `subagent`)。`LoaderCircle` 已在第 1-10 行 import。

- [ ] **Step 4: 跑测试看通过**

Run: `cd frontend && pnpm test -- src/components/agentre/canonical-tool/raw/card.test.tsx`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/agentre/canonical-tool/raw/card.tsx \
        frontend/src/components/agentre/canonical-tool/raw/card.test.tsx
git commit -m "✨ tool-card: run_in_background Bash 卡补「后台运行 · task_id」内联标识"
```

---

## Task 8: 整体校验

- [ ] **Step 1: 本特性相关测试 + lint + tsc**

> ⚠️ 当前 `develop/wyz` 工作树有**无关 WIP**(agent-backends / llm-providers / group-chat 的源码与测试都是脏的)。**全量 `pnpm test` 可能因这些 WIP 红**,与本特性无关。所以这里只跑本特性触及的目录,别用全量结果下"全绿"结论;无关 WIP 文件**绝不**纳入本特性 commit。

Run: `cd frontend && pnpm test -- src/components/agentre/background-tasks/ src/stores/__tests__/cleared-background-tasks-store.test.ts src/components/agentre/canonical-tool/raw/ src/__tests__/i18n.test.ts`
Expected: 全绿(Task 1 改动会让任何依赖「subagent 进面板」的旧用例失败 —— 那些是预期需要更新的,确保已在 Task 1/4 改完)。

Run: `cd frontend && pnpm tsc --noEmit`
Expected: 0 error(若报错只该来自本特性文件;无关 WIP 引入的既有错误需甄别)。

Run: `cd frontend && pnpm lint`
Expected: 本特性文件 0 error(尤其 `i18next/no-literal-string` —— 所有静态文案走了 `t(...)`,task_id/description/summary 是动态值不进 t)。

- [ ] **Step 2: 人工冒烟(可选,GUI)**

`make dev`,用 claude-code 后端发「用 run_in_background 跑 sleep 20,然后告诉我」→ 头部胶囊出现「1 运行中」→ 点开弹层见 bash 行 + 真实 task_id + 运行中 pill;assistant 消息里的 Bash 卡有「后台运行 · <id>」;20s 后自动续轮 + AutoTriggerBanner;弹层任务转「已完成」→ 点「清理已完成」消失;刷新会话仍不复现。

- [ ] **Step 3: 最终 commit(若 lint/tsc 有零碎修整)**

> 仅 add 本特性文件,**禁止 `git add -A`/`-am`**(会扫进无关 WIP)。逐一列出本特性触及文件:

```bash
git add frontend/src/components/agentre/background-tasks/ \
        frontend/src/stores/cleared-background-tasks-store.ts \
        frontend/src/stores/__tests__/cleared-background-tasks-store.test.ts \
        frontend/src/components/agentre/chat-panel.tsx \
        frontend/src/components/agentre/canonical-tool/raw/ \
        frontend/src/i18n/locales/zh-CN/common.json \
        frontend/src/i18n/locales/en/common.json
git commit -m "✅ background-tasks: 重设计整体校验通过(test/lint/tsc 绿)"
```

---

## Self-Review 核对

- **Spec 覆盖**:只收 local_bash(T1)✓;真实 task_id 展示(T1 derive + T4 行 + T7 卡)✓;运行中 badge(T4)✓;清理已完成 + 持久化(T2/T4/T6)✓;行 chrome 对齐(T4)✓;内联后台运行标识(T7)✓;i18n(T3)✓。
- **类型一致**:`BackgroundTask.taskId`(T1)在 T4/T7 一致引用;`clearCompleted(sessionId:number, toolUseIds:string[])`(T2)在 T6 一致调用;`onClearCompleted?: () => void`(T4 popover ← T5 chip ← T6 chat-panel)签名一致。
- **无占位**:每步含真实代码/命令/预期。
- **范围**:未碰后端、未碰无关文件;subagent 内联 AgentSpawnCard 不动(面板与它解耦)。
</content>
</invoke>
