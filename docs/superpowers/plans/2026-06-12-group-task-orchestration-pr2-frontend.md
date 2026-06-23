# 群任务卡编排 PR2(前端)实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 落地 spec `docs/superpowers/specs/2026-06-11-group-task-orchestration-design.md` §8 的 PR2 前端:任务卡气泡(`taskEvent != ''` 的群消息渲染为卡片)、roster 第三个「任务」tab(open 计数 badge + 分组列表 + 锚定/跳转)、`task_updated` 事件驱动的状态实时回写、i18n 双语。

**Architecture:** 任务实体随 `GroupLoad` 的 `GroupDetailResponse.tasks` 下行,实时更新走既有 per-group 事件频道 `group:event:<id>` 的 `{kind:"task_updated", task:{…}}` 载荷 → store `upsertTask`。任务卡气泡**不读消息快照、读 store 里的实时任务实体**(`taskById`),历史卡的状态 pill 自然随翻转。群 transcript 无虚拟化,锚定用原生 `data-message-id` + `scrollIntoView`。

**Tech Stack:** React 19 / TypeScript / zustand / react-i18next / shadcn(`@/components/ui/*`)/ Tailwind v4 / Vitest + @testing-library/react。纯前端改动,不碰 Go。

---

## 后端接缝(PR1 已落地,wailsjs 已生成,直接用)

- `GroupLoad(groupId) → app.GroupDetailResponse{ group, members, messages, tasks }`(`frontend/wailsjs/go/models.ts:936`)。
- `app.GroupTaskItem` 字段:`id, taskNo, title, brief, creatorMemberID, assigneeMemberID, status, result, parentTaskNo, createtime, updatetime`(models.ts:826)。
- `status` 枚举:`"open" | "done" | "canceled"`(注意 canceled 单 l)。`parentTaskNo` 为 0 表示无回指,>0 是**群内编号 #N**。
- `app.GroupMessageItem` 新增 `taskID`(0=非任务消息)、`taskEvent`(`"" | "created" | "completed" | "canceled"`)。
- 事件频道 `group:event:<groupId>`(use-group.ts 已订阅),新增载荷 `{kind:"task_updated", task:<GroupTaskItem 同构>}`,任务**创建与状态翻转都发**;任务事件消息本身仍走 `{kind:"message", message:{…taskEvent}}`。
- 后端有同构断言测试钉死 `GroupTaskEvent ↔ app.GroupTaskItem`,前端直接复用 `app.GroupTaskItem` 类型。

## 文件结构

| 文件 | 动作 | 职责 |
| ---- | ---- | ---- |
| `frontend/src/stores/group-store.ts` | Modify | 新增 `upsertTask`(按 id 原位替换/追加) |
| `frontend/src/stores/__tests__/group-store.test.ts` | Modify | upsertTask 行为测试 |
| `frontend/src/hooks/use-group.ts` | Modify | `GroupLiveEvent` 加 `task` 字段 + `task_updated` 分支 |
| `frontend/src/hooks/use-group.test.ts` | Modify | task_updated 事件测试 |
| `frontend/src/components/agentre/group-chat/group-task-card.tsx` | Create | 任务卡气泡(头部条+pill 三态+回指+@chip+meta) |
| `frontend/src/components/agentre/group-chat/group-task-card.test.tsx` | Create | 卡片三态/回指/chip 跳转测试 |
| `frontend/src/components/agentre/group-chat/mention-text.tsx` | Modify | 导出 `MentionChip`(卡片复用,样式单一来源) |
| `frontend/src/components/agentre/group-chat/group-transcript.tsx` | Modify | 任务消息分支+连续派活并排+`data-message-id` |
| `frontend/src/components/agentre/group-chat/group-transcript.test.tsx` | Create | transcript 任务卡渲染/并排/兜底/翻转测试 |
| `frontend/src/components/agentre/group-chat/group-task-list.tsx` | Create | 任务 tab 列表(分组排序/行/空态) |
| `frontend/src/components/agentre/group-chat/group-task-list.test.tsx` | Create | 列表分组/点击行为测试 |
| `frontend/src/components/agentre/group-chat/group-roster.tsx` | Modify | 第三个「任务」tab + open 计数 badge |
| `frontend/src/components/agentre/group-chat/index.tsx` | Modify | tasks 派生 map、`scrollToMessage` 锚定、props 接线 |
| `frontend/src/components/agentre/group-chat/group-chat.test.tsx` | Modify | 集成测试(卡片渲染/任务 tab/锚定/跳转) |
| `frontend/src/i18n/locales/{zh-CN,en}/common.json` | Modify | `group.tabs.tasks` + `group.task.*` |

**不做**(spec 其他 PR / Out of scope):流程库 UI(PR3)、e2e(PR4)、`group_create`(PR5)、群 transcript 虚拟化、任务卡编辑。

## 执行前置

在隔离 worktree 执行(superpowers:using-git-worktrees),基于 `develop/group` 开分支 `feature/group-task-pr2`。本仓 worktree 坑(memory 已踩过):

```bash
git worktree add ../agentre-pr2 -b feature/group-task-pr2 develop/group
cd ../agentre-pr2
mkdir -p frontend/dist && touch frontend/dist/.gitkeep   # go:embed 占位
GOWORK=off make generate                                  # wailsjs 是 gitignore 生成物
cd frontend && pnpm install
```

测试一律 `cd frontend && pnpm test -- <path>`(focused)或 `pnpm test`(全量);git 写操作需关 sandbox。

---

### Task 1: group-store 新增 `upsertTask`

**Files:**
- Modify: `frontend/src/stores/group-store.ts`
- Test: `frontend/src/stores/__tests__/group-store.test.ts`

- [x] **Step 1: 写失败的测试**

在 `group-store.test.ts` 末尾追加(沿用文件里既有的 `detailWith` 风格,新写一个带 tasks 的 helper):

```ts
function detailWithTasks(tasks: unknown[]): GroupDetail {
  return {
    group: { id: 5, title: "队", runStatus: "running", roundCount: 0 },
    members: [],
    messages: [],
    tasks,
  } as unknown as GroupDetail;
}

describe("group-store upsertTask", () => {
  beforeEach(() => {
    useGroupStore.setState({ details: new Map() });
  });

  it("Given 群详情已加载, When 收到新任务, Then 追加到 tasks 末尾", () => {
    useGroupStore.getState().setDetail(5, detailWithTasks([]));
    useGroupStore.getState().upsertTask(5, {
      id: 9,
      taskNo: 1,
      status: "open",
    } as never);
    const tasks = useGroupStore.getState().details.get(5)?.tasks;
    expect(tasks).toHaveLength(1);
    expect(tasks?.[0].id).toBe(9);
  });

  it("Given 任务已存在, When 收到同 id 更新, Then 原位替换(状态翻转)", () => {
    useGroupStore
      .getState()
      .setDetail(
        5,
        detailWithTasks([
          { id: 9, taskNo: 1, status: "open" },
          { id: 10, taskNo: 2, status: "open" },
        ]),
      );
    useGroupStore.getState().upsertTask(5, {
      id: 9,
      taskNo: 1,
      status: "done",
      result: "改了 settings.tsx",
    } as never);
    const tasks = useGroupStore.getState().details.get(5)?.tasks;
    expect(tasks).toHaveLength(2);
    expect(tasks?.[0].status).toBe("done");
    expect(tasks?.[1].status).toBe("open");
  });

  it("Given 群详情未加载, When upsertTask, Then no-op 不崩", () => {
    useGroupStore.getState().upsertTask(404, { id: 9 } as never);
    expect(useGroupStore.getState().details.get(404)).toBeUndefined();
  });

  it("Given 缓存的旧详情没有 tasks 字段, When upsertTask, Then 当空数组起步", () => {
    useGroupStore.getState().setDetail(5, detailWith([]));
    useGroupStore.getState().upsertTask(5, { id: 9, taskNo: 1 } as never);
    expect(useGroupStore.getState().details.get(5)?.tasks).toHaveLength(1);
  });
});
```

- [x] **Step 2: 跑测试看它失败**

```bash
cd frontend && pnpm test -- src/stores/__tests__/group-store.test.ts
```

预期:FAIL,`upsertTask is not a function`。

- [x] **Step 3: 最小实现**

`group-store.ts` 的 `GroupActions` 类型里、`patchMemberRunState` 声明之后追加:

```ts
  // upsertTask 落一条任务卡:已存在(按 id)则原位替换 —— task_updated 事件既送
  // 新建也送状态翻转,upsert 让两者共用一条路径;群详情未加载时丢弃(打开群时
  // GroupLoad 会带全量 tasks)。
  upsertTask: (groupId: number, task: app.GroupTaskItem) => void;
```

store 实现里、`patchMemberRunState` 实现之后追加:

```ts
  upsertTask: (groupId, task) =>
    set((state) => {
      const cur = state.details.get(groupId);
      if (!cur) return state;
      const tasks = cur.tasks ?? [];
      const idx = tasks.findIndex((t) => t.id === task.id);
      const nextTasks =
        idx < 0 ? [...tasks, task] : tasks.map((t, i) => (i === idx ? task : t));
      const next = new Map(state.details);
      next.set(groupId, { ...cur, tasks: nextTasks });
      return { details: next };
    }),
```

- [x] **Step 4: 跑测试看它通过**

```bash
cd frontend && pnpm test -- src/stores/__tests__/group-store.test.ts
```

预期:PASS(原有 patchMemberRunState 用例也仍绿)。

- [x] **Step 5: Commit**

```bash
git add frontend/src/stores/group-store.ts frontend/src/stores/__tests__/group-store.test.ts
git commit -m "✨ group: store 增 upsertTask(任务新建/状态翻转共用 upsert 落点)"
```

---

### Task 2: use-group 订阅 `task_updated`

**Files:**
- Modify: `frontend/src/hooks/use-group.ts`
- Test: `frontend/src/hooks/use-group.test.ts`

- [x] **Step 1: 写失败的测试**

`use-group.test.ts` 已有 handler-capture 模式(`EventsOn.mockImplementation` 抓回调),在 describe 末尾追加:

```ts
  it("upserts a task on a task_updated event (创建 + 状态翻转)", async () => {
    let handler: ((p: unknown) => void) | undefined;
    (EventsOn as ReturnType<typeof vi.fn>).mockImplementation(
      (_e: string, h: (p: unknown) => void) => {
        handler = h;
        return () => {};
      },
    );
    const { result } = renderHook(() => useGroup(5));
    await waitFor(() => expect(result.current.loading).toBe(false));

    handler?.({
      kind: "task_updated",
      task: {
        id: 9,
        taskNo: 1,
        title: "重构设置页",
        brief: "按设计稿",
        creatorMemberID: 1,
        assigneeMemberID: 2,
        status: "open",
        result: "",
        parentTaskNo: 0,
      },
    });
    await waitFor(() => expect(result.current.detail?.tasks).toHaveLength(1));

    handler?.({
      kind: "task_updated",
      task: {
        id: 9,
        taskNo: 1,
        title: "重构设置页",
        brief: "按设计稿",
        creatorMemberID: 1,
        assigneeMemberID: 2,
        status: "done",
        result: "改完自测通过",
        parentTaskNo: 0,
      },
    });
    await waitFor(() =>
      expect(result.current.detail?.tasks?.[0].status).toBe("done"),
    );
  });
```

- [x] **Step 2: 跑测试看它失败**

```bash
cd frontend && pnpm test -- src/hooks/use-group.test.ts
```

预期:FAIL,第一处 `waitFor` 超时(`tasks` 一直是 undefined —— 事件没人处理)。

- [x] **Step 3: 最小实现**

`use-group.ts` 三处改动:

1. `GroupLiveEvent` 类型加一行(`runState?: string;` 之后):

```ts
  // task_updated 事件:任务创建或状态翻转,载荷与 app.GroupTaskItem 同构(后端有断言钉死)。
  task?: app.GroupTaskItem;
```

2. 取 store action(`patchRunStatus` 之后):

```ts
  const upsertTask = useGroupStore((s) => s.upsertTask);
```

3. 事件分支(`run_status` 分支之后)+ effect 依赖数组补 `upsertTask`:

```ts
      if (payload.kind === "task_updated" && payload.task) {
        upsertTask(groupId, payload.task);
      }
```

- [x] **Step 4: 跑测试看它通过**

```bash
cd frontend && pnpm test -- src/hooks/use-group.test.ts
```

预期:PASS(原有 4 个用例仍绿)。

- [x] **Step 5: Commit**

```bash
git add frontend/src/hooks/use-group.ts frontend/src/hooks/use-group.test.ts
git commit -m "✨ group: use-group 订阅 task_updated 事件 → store.upsertTask"
```

---

### Task 3: 任务卡气泡组件 `GroupTaskCard`

**Files:**
- Create: `frontend/src/components/agentre/group-chat/group-task-card.tsx`
- Modify: `frontend/src/components/agentre/group-chat/mention-text.tsx`(导出 `MentionChip`)
- Modify: `frontend/src/i18n/locales/zh-CN/common.json` + `frontend/src/i18n/locales/en/common.json`
- Test: `frontend/src/components/agentre/group-chat/group-task-card.test.tsx`

视觉规格(spec §8 + agentry.pen 帧「Group 任务卡组件 — Dark」):卡体 = 头部条(clipboard-list 图标 + `#N` mono 序号 + 标题 + 状态 pill:open=amber「进行中」/done=green「已完成」/canceled=gray「已取消」)+ 体部(brief 或 result + 「指派给/交付给 @成员」chip + 创建者·时间 meta)。**pill 读实时任务实体的 status,不读消息快照** —— 历史 created 卡随 task_updated 翻转。

- [x] **Step 1: 写失败的测试**

新建 `group-task-card.test.tsx`:

```tsx
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { app } from "../../../../wailsjs/go/models";

import { GroupTaskCard } from "./group-task-card";

const NAMES: Record<number, string> = { 1: "主持人", 2: "前端" };
const memberName = (id: number) => NAMES[id] ?? `#${id}`;

function task(overrides: Partial<app.GroupTaskItem> = {}): app.GroupTaskItem {
  return {
    id: 9,
    taskNo: 3,
    title: "重构设置页",
    brief: "按设计稿重构,验收:vitest 全绿",
    creatorMemberID: 1,
    assigneeMemberID: 2,
    status: "open",
    result: "",
    parentTaskNo: 0,
    createtime: 1000,
    updatetime: 1000,
    ...overrides,
  } as app.GroupTaskItem;
}

describe("GroupTaskCard", () => {
  it("created 卡:序号/标题/brief/进行中 pill/指派给 @assignee", () => {
    const onJumpMember = vi.fn();
    render(
      <GroupTaskCard
        task={task()}
        taskEvent="created"
        messageId={42}
        memberName={memberName}
        onJumpMember={onJumpMember}
      />,
    );
    expect(screen.getByText("#3")).toBeInTheDocument();
    expect(screen.getByText("重构设置页")).toBeInTheDocument();
    expect(screen.getByText(/按设计稿重构/)).toBeInTheDocument();
    expect(screen.getByText(/In progress|进行中/)).toBeInTheDocument();
    expect(screen.getByText(/Assigned to|指派给/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "@前端" }));
    expect(onJumpMember).toHaveBeenCalledWith(2);
  });

  it("completed 卡:展示 result(非 brief)/已完成 pill/交付给 @creator", () => {
    const onJumpMember = vi.fn();
    render(
      <GroupTaskCard
        task={task({ status: "done", result: "改了 settings.tsx,自测通过" })}
        taskEvent="completed"
        messageId={43}
        memberName={memberName}
        onJumpMember={onJumpMember}
      />,
    );
    expect(screen.getByText(/改了 settings\.tsx/)).toBeInTheDocument();
    expect(screen.queryByText(/按设计稿重构/)).toBeNull();
    expect(screen.getByText(/Done|已完成/)).toBeInTheDocument();
    expect(screen.getByText(/Delivered to|交付给/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "@主持人" }));
    expect(onJumpMember).toHaveBeenCalledWith(1);
  });

  it("canceled 卡:已取消 pill", () => {
    render(
      <GroupTaskCard
        task={task({ status: "canceled" })}
        taskEvent="canceled"
        messageId={44}
        memberName={memberName}
        onJumpMember={vi.fn()}
      />,
    );
    expect(screen.getByText(/Canceled|已取消/)).toBeInTheDocument();
  });

  it("parentTaskNo>0 渲染「↳ 验证 #N」回指,点击回调父任务编号", () => {
    const onJumpTaskNo = vi.fn();
    render(
      <GroupTaskCard
        task={task({ parentTaskNo: 1 })}
        taskEvent="created"
        messageId={45}
        memberName={memberName}
        onJumpMember={vi.fn()}
        onJumpTaskNo={onJumpTaskNo}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: /Verifies #1|验证 #1/ }));
    expect(onJumpTaskNo).toHaveBeenCalledWith(1);
  });

  it("状态 pill 跟实时任务实体走:rerender 翻转 status 后 pill 变化", () => {
    const { rerender } = render(
      <GroupTaskCard
        task={task()}
        taskEvent="created"
        messageId={46}
        memberName={memberName}
        onJumpMember={vi.fn()}
      />,
    );
    expect(screen.getByText(/In progress|进行中/)).toBeInTheDocument();
    rerender(
      <GroupTaskCard
        task={task({ status: "done", result: "ok" })}
        taskEvent="created"
        messageId={46}
        memberName={memberName}
        onJumpMember={vi.fn()}
      />,
    );
    expect(screen.getByText(/Done|已完成/)).toBeInTheDocument();
    // created 卡体仍是 brief(交付物在 completed 卡上)。
    expect(screen.getByText(/按设计稿重构/)).toBeInTheDocument();
  });
});
```

- [x] **Step 2: 跑测试看它失败**

```bash
cd frontend && pnpm test -- src/components/agentre/group-chat/group-task-card.test.tsx
```

预期:FAIL,模块 `./group-task-card` 不存在。

- [x] **Step 3: 导出 MentionChip**

`mention-text.tsx` 末行导出列表改为:

```ts
export { MentionChip, MentionText, mentionMarkdownDecorator };
```

- [x] **Step 4: i18n key(两份 locale 同步加)**

`zh-CN/common.json` 的 `group` 对象内(与 `tabs`/`roster` 平级)新增:

```json
"task": {
  "status": {
    "open": "进行中",
    "done": "已完成",
    "canceled": "已取消"
  },
  "assignedTo": "指派给",
  "deliveredTo": "交付给",
  "verifies": "验证 #{{no}}"
}
```

`en/common.json` 对应:

```json
"task": {
  "status": {
    "open": "In progress",
    "done": "Done",
    "canceled": "Canceled"
  },
  "assignedTo": "Assigned to",
  "deliveredTo": "Delivered to",
  "verifies": "Verifies #{{no}}"
}
```

- [x] **Step 5: 实现组件**

新建 `group-task-card.tsx`:

```tsx
import { ClipboardList, CornerDownRight } from "lucide-react";
import { useTranslation } from "react-i18next";

import { Badge } from "@/components/ui/badge";
import { relativeTime } from "@/lib/relative-time";
import { cn } from "@/lib/utils";

import { MentionChip } from "./mention-text";

import type { app } from "../../../../wailsjs/go/models";

type GroupTaskItem = app.GroupTaskItem;

// 任务状态 → i18n key。未知值兜底 open,绝不让 t() 落空(与 RUN_STATUS_KEY 同手法)。
const STATUS_KEY: Record<string, string> = {
  open: "open",
  done: "done",
  canceled: "canceled",
};

// 状态 pill 三态配色(spec §8:进行中=amber/已完成=green/已取消=gray),暗色靠
// dark: 变体(设计稿「Group 任务卡组件 — Dark」帧)。
const STATUS_PILL_CLASS: Record<string, string> = {
  open: "bg-amber-500/15 text-amber-600 dark:text-amber-400",
  done: "bg-green-500/15 text-green-600 dark:text-green-400",
  canceled: "bg-muted text-muted-foreground",
};

export type GroupTaskCardProps = {
  /** 实时任务实体(store 的 tasks,状态随 task_updated 翻转),不是消息快照。 */
  task: GroupTaskItem;
  /** 该消息的任务事件(created/completed/canceled),决定卡体展示 brief 还是 result。 */
  taskEvent: string;
  /** 所属消息 id:卡片自带 data-message-id 供任务 tab/回指链接锚定。 */
  messageId: number;
  /** member id → 显示名(动态内容,由父层解析,不进 t())。 */
  memberName: (memberId: number) => string;
  /** @chip 点击跳成员 backing session(复用 mention chip 行为)。 */
  onJumpMember: (memberId: number) => void;
  /** 「↳ 验证 #N」回指点击,锚定父任务的派活卡。 */
  onJumpTaskNo?: (taskNo: number) => void;
};

// GroupTaskCard:任务事件消息的卡片体(taskEvent != "" 的 group_message)。纯展示,
// 不取数据 —— 任务实体/跳转都由父层注入。created/canceled 展示 brief 指向 assignee,
// completed 展示交付物 result 指向 creator(任务回到建卡人手里,spec §5)。
function GroupTaskCard({
  task,
  taskEvent,
  messageId,
  memberName,
  onJumpMember,
  onJumpTaskNo,
}: GroupTaskCardProps) {
  const { t } = useTranslation();
  const statusKey = STATUS_KEY[task.status] ?? "open";
  const isCompleted = taskEvent === "completed";
  const body = isCompleted ? task.result : task.brief;
  const targetMemberId = isCompleted
    ? task.creatorMemberID
    : task.assigneeMemberID;

  return (
    <div
      data-message-id={messageId}
      data-testid="group-task-card"
      className="min-w-64 max-w-md flex-1 basis-72 rounded-lg border border-border bg-card"
    >
      <div className="flex items-center gap-2 border-b border-border px-3 py-2">
        <ClipboardList
          className="size-3.5 shrink-0 text-muted-foreground"
          aria-hidden="true"
        />
        <span className="shrink-0 font-mono text-xs text-muted-foreground">
          #{task.taskNo}
        </span>
        {/* 标题是动态内容,原样渲染不进 t()。 */}
        <span
          className="min-w-0 flex-1 truncate text-sm font-medium"
          title={task.title}
        >
          {task.title}
        </span>
        <Badge
          variant="secondary"
          className={cn("shrink-0", STATUS_PILL_CLASS[statusKey])}
        >
          {t(`group.task.status.${statusKey}`)}
        </Badge>
      </div>
      <div className="flex flex-col gap-1.5 px-3 py-2">
        {task.parentTaskNo > 0 ? (
          <button
            type="button"
            onClick={() => onJumpTaskNo?.(task.parentTaskNo)}
            className="flex w-fit items-center gap-1 text-xs text-primary hover:underline"
          >
            <CornerDownRight className="size-3" aria-hidden="true" />
            {t("group.task.verifies", { no: task.parentTaskNo })}
          </button>
        ) : null}
        {body ? (
          <div className="whitespace-pre-wrap break-words text-sm leading-relaxed text-foreground">
            {body}
          </div>
        ) : null}
        <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
          <span>
            {t(
              isCompleted ? "group.task.deliveredTo" : "group.task.assignedTo",
            )}
          </span>
          <MentionChip
            memberId={targetMemberId}
            name={memberName(targetMemberId)}
            onJump={onJumpMember}
          />
        </div>
        <div className="text-2xs text-muted-foreground">
          {memberName(task.creatorMemberID)} · {relativeTime(task.createtime)}
        </div>
      </div>
    </div>
  );
}

export { GroupTaskCard };
```

- [x] **Step 6: 跑测试看它通过**

```bash
cd frontend && pnpm test -- src/components/agentre/group-chat/group-task-card.test.tsx
```

预期:PASS。再跑 i18n 校验:

```bash
cd frontend && pnpm test -- src/__tests__/i18n.test.ts
```

预期:PASS(新 key 双语齐全)。

- [x] **Step 7: Commit**

```bash
git add frontend/src/components/agentre/group-chat/group-task-card.tsx \
        frontend/src/components/agentre/group-chat/group-task-card.test.tsx \
        frontend/src/components/agentre/group-chat/mention-text.tsx \
        frontend/src/i18n/locales/zh-CN/common.json frontend/src/i18n/locales/en/common.json
git commit -m "✨ group: 任务卡气泡组件 GroupTaskCard(三态 pill/回指/@chip/i18n)"
```

---

### Task 4: transcript 渲染任务卡(含连续派活并排)

**Files:**
- Modify: `frontend/src/components/agentre/group-chat/group-transcript.tsx`
- Test: `frontend/src/components/agentre/group-chat/group-transcript.test.tsx`(新建)

行为:`taskEvent != ""` 且非 system 的消息渲染为 `GroupTaskCard`(外层仍是 MessageRow,保留发送者头像/名字行,spec §8);**同发送者的连续任务消息聚合进同一行**,卡片 flex-wrap 并排(并行派活两张紧凑卡并排);任务实体缺失时兜底纯文本(消息正文自带任务抬头,信息不丢);普通 user/agent 行加 `data-message-id`(MessageRow 把多余 props spread 到 `<article>`)。

- [x] **Step 1: 写失败的测试**

新建 `group-transcript.test.tsx`:

```tsx
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { app } from "../../../../wailsjs/go/models";

import { GroupTranscript } from "./group-transcript";

type Msg = app.GroupMessageItem;
type Task = app.GroupTaskItem;

const NAMES: Record<number, string> = { 1: "主持人", 2: "前端" };
const memberName = (id: number) => (id === 0 ? "用户" : (NAMES[id] ?? `#${id}`));

const roster = [
  { id: 1, agentID: 2, role: "host", status: "active" },
  { id: 2, agentID: 3, role: "member", status: "active" },
] as unknown as app.GroupMemberItem[];

function msg(overrides: Partial<Msg>): Msg {
  return {
    id: 1,
    seq: 1,
    senderKind: "agent",
    senderMemberID: 1,
    recipientMemberIDs: [],
    toUser: false,
    content: "",
    taskID: 0,
    taskEvent: "",
    createtime: 0,
    ...overrides,
  } as unknown as Msg;
}

function task(overrides: Partial<Task>): Task {
  return {
    id: 9,
    taskNo: 1,
    title: "重构设置页",
    brief: "按设计稿",
    creatorMemberID: 1,
    assigneeMemberID: 2,
    status: "open",
    result: "",
    parentTaskNo: 0,
    createtime: 0,
    updatetime: 0,
    ...overrides,
  } as unknown as Task;
}

function renderTranscript(messages: Msg[], tasks: Task[]) {
  const taskById = new Map(tasks.map((tk) => [tk.id, tk]));
  return render(
    <GroupTranscript
      messages={messages}
      roster={roster}
      memberName={memberName}
      taskById={taskById}
      onJumpMember={vi.fn()}
    />,
  );
}

describe("GroupTranscript 任务卡", () => {
  it("taskEvent != '' 的消息渲染任务卡而非正文", () => {
    renderTranscript(
      [
        msg({
          id: 11,
          taskEvent: "created",
          taskID: 9,
          content: "(来自 主持人 的任务 #1) 重构设置页:按设计稿",
        }),
      ],
      [task({})],
    );
    expect(screen.getByTestId("group-task-card")).toBeInTheDocument();
    expect(screen.getByText("重构设置页")).toBeInTheDocument();
    // 原始消息正文(任务抬头)不再直出。
    expect(screen.queryByText(/来自 主持人 的任务/)).toBeNull();
  });

  it("同发送者连续两条 created 聚合为一行(一个头像行,两张卡并排)", () => {
    const { container } = renderTranscript(
      [
        msg({ id: 11, taskEvent: "created", taskID: 9 }),
        msg({ id: 12, taskEvent: "created", taskID: 10 }),
      ],
      [task({}), task({ id: 10, taskNo: 2, title: "e2e 验证", parentTaskNo: 1 })],
    );
    const cards = screen.getAllByTestId("group-task-card");
    expect(cards).toHaveLength(2);
    // 两张卡同处一个 MessageRow(article)。
    expect(cards[0].closest("article")).toBe(cards[1].closest("article"));
    // 发送者名字行只出现一次。
    expect(container.querySelectorAll("article")).toHaveLength(1);
  });

  it("发送者不同的任务消息不聚合", () => {
    const { container } = renderTranscript(
      [
        msg({ id: 11, taskEvent: "created", taskID: 9, senderMemberID: 1 }),
        msg({ id: 12, taskEvent: "completed", taskID: 9, senderMemberID: 2 }),
      ],
      [task({ status: "done", result: "改完了" })],
    );
    expect(container.querySelectorAll("article")).toHaveLength(2);
  });

  it("任务实体缺失时兜底渲染原文", () => {
    renderTranscript(
      [
        msg({
          id: 11,
          taskEvent: "created",
          taskID: 404,
          content: "(来自 主持人 的任务 #7) 神秘任务",
        }),
      ],
      [],
    );
    expect(screen.queryByTestId("group-task-card")).toBeNull();
    expect(screen.getByText(/神秘任务/)).toBeInTheDocument();
  });

  it("历史 created 卡的状态 pill 随实时任务实体翻转", () => {
    const messages = [msg({ id: 11, taskEvent: "created", taskID: 9 })];
    const { rerender } = renderTranscript(messages, [task({})]);
    expect(screen.getByText(/In progress|进行中/)).toBeInTheDocument();

    const flipped = new Map([[9, task({ status: "done", result: "ok" })]]);
    rerender(
      <GroupTranscript
        messages={messages}
        roster={roster}
        memberName={memberName}
        taskById={flipped}
        onJumpMember={vi.fn()}
      />,
    );
    expect(screen.getByText(/Done|已完成/)).toBeInTheDocument();
  });

  it("普通消息行携带 data-message-id(锚定用)", () => {
    const { container } = renderTranscript(
      [msg({ id: 21, content: "普通发言" })],
      [],
    );
    expect(
      container.querySelector('article[data-message-id="21"]'),
    ).not.toBeNull();
  });
});
```

- [x] **Step 2: 跑测试看它失败**

```bash
cd frontend && pnpm test -- src/components/agentre/group-chat/group-transcript.test.tsx
```

预期:FAIL —— `taskById` 不是合法 prop / 任务消息按普通正文渲染(`getByTestId("group-task-card")` 找不到)。

- [x] **Step 3: 实现**

`group-transcript.tsx` 改动:

1. 顶部 import 增加:

```ts
import { GroupTaskCard } from "./group-task-card";
```

类型别名增加:

```ts
type GroupTaskItem = app.GroupTaskItem;
```

2. Props 扩展(`GroupTranscriptProps` 增加三个可选字段):

```ts
  /** taskID → 实时任务实体(状态随 task_updated 翻转);任务事件消息据此渲染卡片。 */
  taskById?: Map<number, GroupTaskItem>;
  /** 任务卡 @chip 跳成员会话。 */
  onJumpMember?: (memberId: number) => void;
  /** 任务卡「↳ 验证 #N」回指锚定。 */
  onJumpTaskNo?: (taskNo: number) => void;
```

3. 文件内(组件外)加聚合纯函数:

```ts
// 渲染项:普通消息一条一行;同发送者的连续任务事件消息聚合成一行,并行派活的
// 多张卡在同一头像行内 flex-wrap 并排(spec §8)。
type RenderItem =
  | { kind: "single"; msg: GroupMessageItem }
  | { kind: "taskGroup"; msgs: GroupMessageItem[] };

function groupTaskMessages(messages: GroupMessageItem[]): RenderItem[] {
  const items: RenderItem[] = [];
  for (const msg of messages) {
    // Boolean() 同时挡住 ""/undefined(旧测试 fixture 可能缺字段)。
    const isTask = Boolean(msg.taskEvent) && msg.senderKind !== "system";
    const last = items[items.length - 1];
    if (
      isTask &&
      last?.kind === "taskGroup" &&
      last.msgs[0].senderKind === msg.senderKind &&
      last.msgs[0].senderMemberID === msg.senderMemberID
    ) {
      last.msgs.push(msg);
      continue;
    }
    items.push(
      isTask ? { kind: "taskGroup", msgs: [msg] } : { kind: "single", msg },
    );
  }
  return items;
}
```

4. 组件签名解构新 props;渲染主体从 `messages.map((msg) => …)` 改为 `groupTaskMessages(messages).map((item) => …)`:

```tsx
      {groupTaskMessages(messages).map((item) => {
        if (item.kind === "taskGroup") {
          const first = item.msgs[0];
          const isUser = first.senderKind === "user";
          const displayName = isUser
            ? t("group.you")
            : memberName(first.senderMemberID);
          const color = isUser
            ? "neutral"
            : agentColorForMember(first.senderMemberID);
          return (
            <MessageRow
              key={first.id}
              avatarName={displayName}
              avatarColor={color}
              name={displayName}
            >
              <div className="flex flex-wrap gap-2">
                {item.msgs.map((m) => {
                  const task = taskById?.get(m.taskID);
                  if (!task) {
                    // 任务实体缺失(LoadGroup/task_updated 正常必有):兜底纯文本,
                    // 消息正文自带任务抬头,信息不丢。
                    return (
                      <React.Fragment key={m.id}>
                        {renderBody(m.content)}
                      </React.Fragment>
                    );
                  }
                  return (
                    <GroupTaskCard
                      key={m.id}
                      task={task}
                      taskEvent={m.taskEvent}
                      messageId={m.id}
                      memberName={memberName}
                      onJumpMember={onJumpMember ?? (() => {})}
                      onJumpTaskNo={onJumpTaskNo}
                    />
                  );
                })}
              </div>
            </MessageRow>
          );
        }

        const msg = item.msg;
        // ……以下是原有 system / user-agent 分支,整体保留;唯一改动:
        // user/agent 的 <MessageRow> 增加 data-message-id={msg.id}(spread 到 article)。
      })}
```

原有 user/agent 分支的 `<MessageRow key={msg.id} …>` 加一个属性:

```tsx
            data-message-id={msg.id}
```

- [x] **Step 4: 跑测试看它通过**

```bash
cd frontend && pnpm test -- src/components/agentre/group-chat/group-transcript.test.tsx src/components/agentre/group-chat/group-chat.test.tsx
```

预期:两个文件都 PASS(group-chat.test 的旧消息 fixture 没有 taskEvent 字段,`Boolean(undefined)`=false 走普通分支,不受影响)。

- [x] **Step 5: Commit**

```bash
git add frontend/src/components/agentre/group-chat/group-transcript.tsx \
        frontend/src/components/agentre/group-chat/group-transcript.test.tsx
git commit -m "✨ group: transcript 任务事件消息渲染任务卡(连续派活并排/实体缺失兜底/行锚点)"
```

---

### Task 5: 任务 tab 列表组件 `GroupTaskList`

**Files:**
- Create: `frontend/src/components/agentre/group-chat/group-task-list.tsx`
- Modify: `frontend/src/i18n/locales/zh-CN/common.json` + `frontend/src/i18n/locales/en/common.json`
- Test: `frontend/src/components/agentre/group-chat/group-task-list.test.tsx`

行为(spec §8):「进行中」置顶、「已完成」(done+canceled)分组在下,组内按 `#N` 升序(与卡片故事线一致);行 = 状态点/勾 + `#N` + 标题 + 副行(assignee · 回指 · 时间)+ assignee 小头像 + chevron;**点行 = 锚定 transcript 任务卡,行尾 chevron = 跳 assignee 会话**(两个独立 button 并排,不嵌套交互元素)。

- [x] **Step 1: 写失败的测试**

新建 `group-task-list.test.tsx`:

```tsx
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { app } from "../../../../wailsjs/go/models";

import { GroupTaskList } from "./group-task-list";

type Task = app.GroupTaskItem;

const NAMES: Record<number, string> = { 1: "主持人", 2: "前端" };
const memberName = (id: number) => NAMES[id] ?? `#${id}`;

function task(overrides: Partial<Task>): Task {
  return {
    id: 9,
    taskNo: 1,
    title: "重构设置页",
    brief: "按设计稿",
    creatorMemberID: 1,
    assigneeMemberID: 2,
    status: "open",
    result: "",
    parentTaskNo: 0,
    createtime: 0,
    updatetime: 0,
    ...overrides,
  } as unknown as Task;
}

describe("GroupTaskList", () => {
  it("进行中置顶、已结束(done/canceled)在下,组内按 #N 升序", () => {
    render(
      <GroupTaskList
        tasks={[
          task({ id: 12, taskNo: 4, status: "open", title: "代码审查" }),
          task({ id: 10, taskNo: 2, status: "done", title: "实现功能" }),
          task({ id: 11, taskNo: 3, status: "canceled", title: "废弃任务" }),
          task({ id: 9, taskNo: 1, status: "open", title: "重构设置页" }),
        ]}
        memberName={memberName}
        onAnchorTask={vi.fn()}
        onOpenMember={vi.fn()}
      />,
    );
    const nos = screen.getAllByText(/^#\d+$/).map((el) => el.textContent);
    expect(nos).toEqual(["#1", "#4", "#2", "#3"]);
  });

  it("空列表渲染空态文案", () => {
    render(
      <GroupTaskList
        tasks={[]}
        memberName={memberName}
        onAnchorTask={vi.fn()}
        onOpenMember={vi.fn()}
      />,
    );
    expect(screen.getByText(/No tasks yet|暂无任务/)).toBeInTheDocument();
  });

  it("副行展示 assignee 与回指编号", () => {
    render(
      <GroupTaskList
        tasks={[task({ taskNo: 2, parentTaskNo: 1 })]}
        memberName={memberName}
        onAnchorTask={vi.fn()}
        onOpenMember={vi.fn()}
      />,
    );
    // 副行是单个文本块「前端 · ↳#1 · now」,合并断言避免命中头像等其他节点。
    expect(screen.getByText(/前端 · ↳#1/)).toBeInTheDocument();
  });

  it("点行回调 onAnchorTask,点行尾 › 回调 onOpenMember(assignee) 且不触发锚定", () => {
    const onAnchorTask = vi.fn();
    const onOpenMember = vi.fn();
    render(
      <GroupTaskList
        tasks={[task({})]}
        memberName={memberName}
        onAnchorTask={onAnchorTask}
        onOpenMember={onOpenMember}
      />,
    );
    fireEvent.click(screen.getByText("重构设置页"));
    expect(onAnchorTask).toHaveBeenCalledWith(
      expect.objectContaining({ id: 9 }),
    );

    fireEvent.click(
      screen.getByRole("button", { name: /Open assignee|打开执行成员/ }),
    );
    expect(onOpenMember).toHaveBeenCalledWith(2);
    expect(onAnchorTask).toHaveBeenCalledTimes(1);
  });
});
```

- [x] **Step 2: 跑测试看它失败**

```bash
cd frontend && pnpm test -- src/components/agentre/group-chat/group-task-list.test.tsx
```

预期:FAIL,模块不存在。

- [x] **Step 3: i18n key(两份 locale 同步加)**

`zh-CN/common.json` 的 `group.tabs` 增加 `"tasks": "任务"`;`group.task`(Task 3 已建)内追加:

```json
"sectionOpen": "进行中",
"sectionClosed": "已完成",
"empty": "暂无任务",
"openAssignee": "打开执行成员"
```

`en/common.json`:`group.tabs` 增加 `"tasks": "Tasks"`;`group.task` 内追加:

```json
"sectionOpen": "In progress",
"sectionClosed": "Completed",
"empty": "No tasks yet",
"openAssignee": "Open assignee"
```

- [x] **Step 4: 实现组件**

新建 `group-task-list.tsx`:

```tsx
import { Check, ChevronRight, X } from "lucide-react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import { relativeTime } from "@/lib/relative-time";

import { AgentAvatar } from "../primitives";

import { agentColorForMember } from "./group-transcript";

import type { app } from "../../../../wailsjs/go/models";

type GroupTaskItem = app.GroupTaskItem;

export type GroupTaskListProps = {
  tasks: GroupTaskItem[];
  /** member id → 显示名(动态内容,父层解析,不进 t())。 */
  memberName: (memberId: number) => string;
  /** 点行:transcript 锚定到该任务的派活卡。 */
  onAnchorTask: (task: GroupTaskItem) => void;
  /** 行尾 ›:跳 assignee 的成员会话。 */
  onOpenMember: (memberId: number) => void;
};

// 状态图标:open=amber 点(进行中),done=绿勾,canceled=灰叉 —— 与卡片 pill 同色系。
function statusIcon(status: string) {
  if (status === "done") {
    return (
      <Check
        className="size-3.5 shrink-0 text-green-600 dark:text-green-400"
        aria-hidden="true"
      />
    );
  }
  if (status === "canceled") {
    return <X className="size-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />;
  }
  return (
    <span
      aria-hidden="true"
      className="mx-1 size-1.5 shrink-0 rounded-full bg-amber-500"
    />
  );
}

function TaskRow({
  task,
  memberName,
  onAnchorTask,
  onOpenMember,
}: {
  task: GroupTaskItem;
  memberName: (memberId: number) => string;
  onAnchorTask: (task: GroupTaskItem) => void;
  onOpenMember: (memberId: number) => void;
}) {
  const { t } = useTranslation();
  const assigneeName = memberName(task.assigneeMemberID);
  // 主体与行尾是两个并排 button(交互元素不嵌套):点主体锚定,点 › 跳会话。
  return (
    <div className="flex items-center gap-1">
      <button
        type="button"
        onClick={() => onAnchorTask(task)}
        className="flex min-w-0 flex-1 items-center gap-2 rounded-md px-2 py-1.5 text-left hover:bg-accent"
      >
        {statusIcon(task.status)}
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-1.5">
            <span className="shrink-0 font-mono text-2xs text-muted-foreground">
              #{task.taskNo}
            </span>
            {/* 标题/成员名是动态内容,原样渲染。 */}
            <span className="min-w-0 flex-1 truncate text-sm text-foreground">
              {task.title}
            </span>
          </div>
          <div className="truncate text-2xs text-muted-foreground">
            {assigneeName}
            {task.parentTaskNo > 0 ? ` · ↳#${task.parentTaskNo}` : ""}
            {` · ${relativeTime(task.createtime)}`}
          </div>
        </div>
        <AgentAvatar
          name={assigneeName}
          color={agentColorForMember(task.assigneeMemberID)}
          size="sm"
        />
      </button>
      <Button
        type="button"
        variant="ghost"
        size="icon-sm"
        aria-label={t("group.task.openAssignee")}
        onClick={() => onOpenMember(task.assigneeMemberID)}
      >
        <ChevronRight data-icon="only" aria-hidden="true" />
      </Button>
    </div>
  );
}

// GroupTaskList:roster「任务」tab 的内容。「进行中」置顶、已结束分组在下,
// 组内按 #N 升序(与 transcript 卡片故事线一致)。
function GroupTaskList({
  tasks,
  memberName,
  onAnchorTask,
  onOpenMember,
}: GroupTaskListProps) {
  const { t } = useTranslation();
  const open = tasks
    .filter((tk) => tk.status === "open")
    .sort((a, b) => a.taskNo - b.taskNo);
  const closed = tasks
    .filter((tk) => tk.status !== "open")
    .sort((a, b) => a.taskNo - b.taskNo);

  if (tasks.length === 0) {
    return (
      <div className="p-4 text-center text-xs text-muted-foreground">
        {t("group.task.empty")}
      </div>
    );
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-auto p-2">
      {open.length > 0 ? (
        <>
          <div className="px-2 pb-1 pt-2 text-2xs font-semibold uppercase tracking-wide text-muted-foreground">
            {t("group.task.sectionOpen")}
          </div>
          {open.map((tk) => (
            <TaskRow
              key={tk.id}
              task={tk}
              memberName={memberName}
              onAnchorTask={onAnchorTask}
              onOpenMember={onOpenMember}
            />
          ))}
        </>
      ) : null}
      {closed.length > 0 ? (
        <>
          <div className="px-2 pb-1 pt-3 text-2xs font-semibold uppercase tracking-wide text-muted-foreground">
            {t("group.task.sectionClosed")}
          </div>
          {closed.map((tk) => (
            <TaskRow
              key={tk.id}
              task={tk}
              memberName={memberName}
              onAnchorTask={onAnchorTask}
              onOpenMember={onOpenMember}
            />
          ))}
        </>
      ) : null}
    </div>
  );
}

export { GroupTaskList };
```

- [x] **Step 5: 跑测试看它通过**

```bash
cd frontend && pnpm test -- src/components/agentre/group-chat/group-task-list.test.tsx src/__tests__/i18n.test.ts
```

预期:PASS。

- [x] **Step 6: Commit**

```bash
git add frontend/src/components/agentre/group-chat/group-task-list.tsx \
        frontend/src/components/agentre/group-chat/group-task-list.test.tsx \
        frontend/src/i18n/locales/zh-CN/common.json frontend/src/i18n/locales/en/common.json
git commit -m "✨ group: 任务 tab 列表组件 GroupTaskList(进行中置顶/已结束分组/锚定+跳转)"
```

---

### Task 6: roster 第三 tab + index 接线(锚定/跳转/实时数据)

**Files:**
- Modify: `frontend/src/components/agentre/group-chat/group-roster.tsx`
- Modify: `frontend/src/components/agentre/group-chat/index.tsx`
- Test: `frontend/src/components/agentre/group-chat/group-chat.test.tsx`

- [x] **Step 1: 写失败的集成测试**

`group-chat.test.tsx` 改动:

1. mock 的 `useGroup` detail 里,`messages` 数组追加一条任务消息、新增 `tasks` 字段(加在 `messages` 之后):

```ts
        {
          id: 4,
          seq: 4,
          senderKind: "agent",
          senderMemberID: 1,
          recipientMemberIDs: [2],
          toUser: false,
          content: "(来自 后端 的任务 #1) 重构设置页:按设计稿",
          taskID: 9,
          taskEvent: "created",
          createtime: 0,
        },
      ],
      tasks: [
        {
          id: 9,
          taskNo: 1,
          title: "重构设置页",
          brief: "按设计稿",
          creatorMemberID: 1,
          assigneeMemberID: 2,
          status: "open",
          result: "",
          parentTaskNo: 0,
          createtime: 0,
          updatetime: 0,
        },
      ],
```

2. describe 末尾追加用例:

```tsx
  it("任务事件消息渲染为任务卡(标题可见,原文抬头不直出)", () => {
    renderGroupChat();
    expect(screen.getByTestId("group-task-card")).toBeInTheDocument();
    expect(screen.queryByText(/来自 后端 的任务/)).toBeNull();
  });

  it("roster 任务 tab:badge 显示 open 计数,点开列出任务行", () => {
    renderGroupChat();
    const tasksTab = screen.getByRole("button", { name: /Tasks|任务/ });
    expect(tasksTab.textContent).toContain("1"); // open 计数 badge
    fireEvent.click(tasksTab);
    // 「进行中」此时出现两处:transcript 卡片的 pill + 任务列表的分组标题。
    expect(
      screen.getAllByText(/In progress|进行中/).length,
    ).toBeGreaterThanOrEqual(2);
    // 行主体(#1 + 标题)与 transcript 卡片同时在 DOM —— 标题出现两次。
    expect(screen.getAllByText("重构设置页").length).toBeGreaterThanOrEqual(2);
  });

  it("点任务行,transcript 锚定到对应任务卡(scrollIntoView)", () => {
    const spy = vi.fn();
    Element.prototype.scrollIntoView = spy; // jsdom 没有 scrollIntoView
    renderGroupChat();
    fireEvent.click(screen.getByRole("button", { name: /Tasks|任务/ }));
    // 点列表行主体(行内 #1 文本属于行 button)。
    const rowNo = screen
      .getAllByText("#1")
      .find((el) => el.closest("button")?.textContent?.includes("前端"));
    if (!rowNo) throw new Error("task list row #1 not found");
    fireEvent.click(rowNo);
    expect(spy).toHaveBeenCalled();
  });

  it("点任务行尾 ›,打开 assignee 的成员会话 tab", () => {
    renderGroupChat();
    fireEvent.click(screen.getByRole("button", { name: /Tasks|任务/ }));
    fireEvent.click(
      screen.getByRole("button", { name: /Open assignee|打开执行成员/ }),
    );
    const state = useChatTabsStore.getState();
    const active = state.tabs.find((tab) => tab.id === state.activeTabId);
    expect(active?.meta).toEqual({
      kind: "groupSession",
      groupId: 5,
      sessionId: 12,
      title: "前端",
    });
  });
```

注意:mock 任务的 `assigneeMemberID: 2` 对应 mock 成员 `id:2 → agentID:3 → "前端" → backingSessionID:12`,与既有「点成员行开 tab」用例同口径。

- [x] **Step 2: 跑测试看它失败**

```bash
cd frontend && pnpm test -- src/components/agentre/group-chat/group-chat.test.tsx
```

预期:旧用例仍绿(transcript 兜底 Boolean(taskEvent));新 4 个用例 FAIL(无任务 tab / 卡片虽渲染但 `taskById` 还没接线 —— `getByTestId("group-task-card")` 找不到)。

- [x] **Step 3: 实现 roster 第三 tab**

`group-roster.tsx` 改动:

1. import 增加:

```ts
import { Badge } from "@/components/ui/badge";

import { GroupTaskList } from "./group-task-list";
```

类型别名增加 `type GroupTaskItem = app.GroupTaskItem;`,tab 类型改为:

```ts
type RosterTab = "members" | "tasks" | "settings";
```

2. `GroupRosterProps` 增加:

```ts
  /** 群任务卡(实时,LoadGroup + task_updated 驱动)。 */
  tasks: GroupTaskItem[];
  /** 任务行点击:transcript 锚定到该任务的派活卡。 */
  onAnchorTask: (task: GroupTaskItem) => void;
  /** 任务行尾 ›:按 member id 跳成员会话(复用 openMemberById)。 */
  onOpenMemberById: (memberId: number) => void;
```

3. 组件内(`hosts`/`regulars` 旁)算 open 计数:

```ts
  const openTaskCount = tasks.filter((tk) => tk.status === "open").length;
```

4. tab 条:在「成员」与「设置」按钮之间插入(顺序:成员/任务/设置):

```tsx
        <Button
          type="button"
          variant={tab === "tasks" ? "secondary" : "ghost"}
          size="sm"
          className="flex-1"
          onClick={() => setTab("tasks")}
        >
          {t("group.tabs.tasks")}
          {openTaskCount > 0 ? (
            <Badge
              variant="secondary"
              className="ml-1 h-4 min-w-4 px-1 font-mono text-2xs"
            >
              {openTaskCount}
            </Badge>
          ) : null}
        </Button>
```

5. 主体三分支:原 `{tab === "members" ? (…) : (…settings…)}` 改为:

```tsx
      {tab === "members" ? (
        /* 原 members 分支原样保留 */
      ) : tab === "tasks" ? (
        <GroupTaskList
          tasks={tasks}
          memberName={memberName}
          onAnchorTask={onAnchorTask}
          onOpenMember={onOpenMemberById}
        />
      ) : (
        /* 原 settings 分支原样保留 */
      )}
```

- [x] **Step 4: 实现 index 接线**

`group-chat/index.tsx` 改动(全部在 `GroupChat` 组件内):

1. `messages` memo 之后增加派生数据:

```tsx
  const tasks = React.useMemo(() => detail?.tasks ?? [], [detail?.tasks]);

  // taskById:taskID → 实时任务实体,transcript 卡片按它取状态(随 task_updated 翻转)。
  const taskById = React.useMemo(() => {
    const map = new Map<number, app.GroupTaskItem>();
    for (const tk of tasks) map.set(tk.id, tk);
    return map;
  }, [tasks]);

  // createdMessageIdByTaskNo:#N → 派活卡(created 消息)的消息 id。任务 tab 点行
  // 与卡片回指链接都锚到这条消息。
  const createdMessageIdByTaskNo = React.useMemo(() => {
    const idByTaskId = new Map<number, number>();
    for (const m of messages) {
      if (m.taskEvent === "created" && !idByTaskId.has(m.taskID)) {
        idByTaskId.set(m.taskID, m.id);
      }
    }
    const map = new Map<number, number>();
    for (const tk of tasks) {
      const mid = idByTaskId.get(tk.id);
      if (mid != null) map.set(tk.taskNo, mid);
    }
    return map;
  }, [messages, tasks]);
```

2. `openMemberById` 之后增加锚定函数:

```tsx
  // scrollToMessage:群 transcript 无虚拟化,原生 DOM 锚定即可(行/卡带 data-message-id)。
  const scrollToMessage = React.useCallback(
    (messageId: number) => {
      const el = scrollRef.current?.querySelector(
        `[data-message-id="${messageId}"]`,
      );
      el?.scrollIntoView({ block: "center" });
    },
    [scrollRef],
  );

  const anchorTaskNo = React.useCallback(
    (taskNo: number) => {
      const mid = createdMessageIdByTaskNo.get(taskNo);
      if (mid != null) scrollToMessage(mid);
    },
    [createdMessageIdByTaskNo, scrollToMessage],
  );
```

3. `<GroupTranscript …>` 增加 props:

```tsx
              taskById={taskById}
              onJumpMember={openMemberById}
              onJumpTaskNo={anchorTaskNo}
```

4. `<GroupRoster …>` 增加 props:

```tsx
        tasks={tasks}
        onAnchorTask={(tk) => anchorTaskNo(tk.taskNo)}
        onOpenMemberById={openMemberById}
```

- [x] **Step 5: 跑测试看它通过**

```bash
cd frontend && pnpm test -- src/components/agentre/group-chat/
```

预期:group-chat / group-transcript / group-task-card / group-task-list / mention-text 全部 PASS。

- [x] **Step 6: Commit**

```bash
git add frontend/src/components/agentre/group-chat/group-roster.tsx \
        frontend/src/components/agentre/group-chat/index.tsx \
        frontend/src/components/agentre/group-chat/group-chat.test.tsx
git commit -m "✨ group: roster 任务 tab(open 计数 badge)+ transcript 锚定/回指/跳转接线"
```

---

### Task 7: 全量验证 + 收尾

- [x] **Step 1: 全量前端测试**

```bash
cd frontend && pnpm test
```

预期:全绿。若有「组件间接 import wailsjs runtime」型失败(memory 规则:per-file `vi.mock` + `importActual` override,**不加全局 alias**),按既有测试文件的 mock 模式补到对应测试文件,不动全局配置。

- [x] **Step 2: lint(含 i18next/no-literal-string 检查)**

```bash
GOWORK=off make lint
```

预期:0 issues。本 PR 全部静态文案已走 `t(...)`;动态内容(标题/成员名/`#N`/`group-{id}`)原样渲染是惯例,不会命中规则。

- [x] **Step 3: 后端测试冒烟(确认没碰 Go)**

```bash
git diff develop/group --stat -- ':!frontend'
```

预期:除本计划文件外无 Go/后端文件变更(无须跑 `make test-backend`;若意外有,回查并移除)。

- [x] **Step 4: 勾选计划 + 提交**

把本计划文件中完成的 checkbox 勾上:

```bash
git add docs/superpowers/plans/2026-06-12-group-task-orchestration-pr2-frontend.md
git commit -m "📝 plan: 勾选 PR2 全部任务(实施完成)"
```

- [x] **Step 5: 真机验证提示(人工,不阻塞合并)**

`make dev` 起 app → 建群派活(或用既有群)→ 核对:任务卡气泡三态、并行派活并排、任务 tab badge/分组、点行锚定、`↳ 验证 #N` 回指、@chip 跳成员会话、暗色模式。设计稿对照 `~/Desktop/agentry.pen` 帧「Group Chat — 任务卡编排 · Light」「Group 任务卡组件 — Dark」。

---

## Spec 覆盖对照(自查)

| spec §8 要求 | 任务 |
| ---- | ---- |
| 任务卡气泡(头像行保留/头部条/pill 三态/体部 brief·result/指派给·交付给 chip/创建者·时间 meta) | Task 3、4 |
| 并行派活两卡并排 + `↳ 验证 #N` 回指链接 | Task 3、4 |
| @chip 跳 assignee backing session | Task 3、6(复用 `MentionChip`+`openMemberById`) |
| 状态实时回写(store `taskId → status`,`task_updated` 驱动,历史卡 pill 翻转) | Task 1、2(store/事件)+ 3、4(卡片读实时实体) |
| 任务 tab(第三 tab/open 计数 badge/进行中置顶/行构成/点行锚定/行尾跳会话) | Task 5、6 |
| 群头部不加新元素 | 全程未触碰群头部 |
| i18n 双语 + shadcn 控件 | Task 3、5(key)+ i18n.test 自动校验 |
