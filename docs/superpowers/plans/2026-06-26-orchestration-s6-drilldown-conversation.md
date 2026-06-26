# 编排 S6 — 钻入会话(右栏「任务板 ⇄ 会话」只读 + 对它说)Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把编排 Run 的钻入做出来:结构图节点 / 任务板 per-call 行点击 → 选中某次调用的 **session**,Run 右栏从「任务板」二态切换成「会话」面板(复用既有 `ChatTranscript` 渲染该 session 的**只读** transcript + 顶部返回任务板 + 底部「对它说」`RunSpeak(sessionId, text)`)。因「节点=调用=唯一 session」(§5.7a),钻入直接拿 `task.sessionId` 读该会话,**无需「一 agent 多会话」切换器**。

**Architecture:** 选中态从 `selectedAgentId` 升级为 `selectedSessionId`。Run 外壳 `index.tsx` 持有它并在右栏做二态:`selectedSessionId===null` 渲 `<TaskBoard>`,否则渲新增的 `<ConversationPanel>`。`ConversationPanel` 复用 `ChatTranscript`(省略所有 `live*`/`onRerun`/`onEdit` → 只读)。session 的原始 messages 走**扩展后的 `orch-subagents-store`**(S5b 已对每个 task session `LoadChatSession`;这里顺手缓存 `messagesBySession`,避免二次加载)。「对它说」调 `RunSpeak(sessionId, text)`(后端 `orch.go:144` 已支持按 session 干预),发完重载该 session 快照。

**Tech Stack:** React 19 + TypeScript + zustand + Tailwind v4 + Vitest + Testing Library + react-i18next。纯前端;`ChatTranscript`(`./chat`)、`RunSpeak`、`LoadChatSession` 均已存在。

## Global Constraints

- **严格 TDD:Red → Green → Refactor。** 每 Task 先写失败测试 → 跑看正确失败 → 最小实现 → 跑过 →(门控)提交。
- **依赖 S4 + S5/S5b 已落地。** 本切片改的 `structure-graph.tsx`(选中契约)、`task-board.tsx`(per-call 行 + 折叠子代理)、`orch-subagents-store.ts` 都以 **S4/S5b 后**的代码为基线;执行顺序 S4 → S5/S5b → 本 plan。
- **只动本切片文件**:`index.tsx` / 新增 `conversation-panel.tsx` / `orch-subagents-store.ts`(扩 messages 缓存)/ `structure-graph.tsx`(选中契约)/ `task-board.tsx`(选中契约 + 删 DrilldownPanel)/ 两个 `common.json` + 对应测试。**禁止** drive-by 改 graph-data/feed/run-header。
- **本切片不做(划界):**
  - **内联审批 批准/拒绝 = S9**(`TaskAwaitingUser`/`ApprovalGateway` 后端零调用点,见 spec §9)。会话面板**不**渲染批准/拒绝块。
  - **钻入面板的实时流**:本切片是**只读快照 + 发言后重载**;把活跃 session 的流式 chunk 实时灌进钻入面板**不做**(留后续;Run 列表/详情的整体实时仍由 `OrchEventsHost` 维持)。
  - peer ask/reply 渲染 = **S3**;独立顶级页 = **S7**。
- **i18n:** 新文案走 `t(...)` + 双语;新键挂 `orchestration.conversation.*`。`i18next/no-literal-string` 拦硬编码中文。
- **共享分支 develop/wyz**:提交**永远带 pathspec**;**Commit 步骤受用户门控**。
- **store 测试隔离**:`orch-subagents-store` 已有 `__reset()`,测试 `beforeEach` 调用;组件测试 per-file `vi.mock` wailsjs(`LoadChatSession`/`RunSpeak`),勿加全局 alias。
- **测试命令**:聚焦 `cd frontend && pnpm test -- <path>`;收尾 `make test-frontend` + `make lint`(`| tail` 吞退出码注意)。

---

## File Structure

- `frontend/src/stores/orch-subagents-store.ts` — 扩 `messagesBySession: Map<sessionId, ChatMessage[]>` + `messagesFor(sessionId)`;`ensureLoaded` 顺手缓存原始 messages(一次加载两用)。
- `frontend/src/components/agentre/orchestration/conversation-panel.tsx` — **新**。右栏「会话」态:返回任务板按钮 + `<ChatTranscript>`(只读)+「对它说」输入(`RunSpeak`)。
- `frontend/src/components/agentre/orchestration/index.tsx` — 选中态 `selectedSessionId`;右栏二态切换;给 graph/board 传 `onSelectSession`。
- `frontend/src/components/agentre/orchestration/structure-graph.tsx` — 选中契约 `onSelectNode(agentId)` → `onSelectSession(sessionId)`(单调用节点 / 顶层分组 per-call 子行发各自 session)。
- `frontend/src/components/agentre/orchestration/task-board.tsx` — 选中契约 `onSelectTask(agentId)` → `onSelectSession(sessionId)`(per-call 行发各自 session);删除内嵌 `DrilldownPanel`(会话上移到外壳右栏)。
- `frontend/src/i18n/locales/{zh-CN,en}/common.json` — `orchestration.conversation.{backToBoard,speakPlaceholder,speakSend,title}`。
- 测试:新增 `__tests__/conversation-panel.test.tsx`;改 `__tests__/structure-graph.test.tsx`、`__tests__/task-board.test.tsx`、`__tests__/index.test.tsx`、`stores/__tests__/orch-subagents-store.test.ts`。

---

## Task 1: `orch-subagents-store` 顺手缓存原始 messages(一次加载两用)

**Files:**
- Modify: `frontend/src/stores/orch-subagents-store.ts`
- Test: `frontend/src/stores/__tests__/orch-subagents-store.test.ts`

**Interfaces:**
- Produces(扩展,不破坏 S5b 既有):
  ```ts
  messagesBySession: Map<number, chat_svc.ChatMessage[]>;
  messagesFor: (sessionId: number) => chat_svc.ChatMessage[];
  // ensureLoaded 不变签名,内部同时写 bySession(subagents)+ messagesBySession(原始)
  ```

- [ ] **Step 1: 写失败测试**

`orch-subagents-store.test.ts` 追加:
```ts
it("ensureLoaded 同时缓存原始 messages, messagesFor 可取", async () => {
  loadMock.mockResolvedValue({
    messages: [{ id: 1, blocks: [{ type: "text", text: "hi" }] }],
  });
  useOrchSubagentsStore.getState().ensureLoaded(801);
  await vi.waitFor(() =>
    expect(useOrchSubagentsStore.getState().messagesFor(801)).toHaveLength(1),
  );
});

it("未加载的 session messagesFor 返回空数组", () => {
  expect(useOrchSubagentsStore.getState().messagesFor(999)).toEqual([]);
});
```

- [ ] **Step 2: 跑确认失败**

Run: `cd frontend && pnpm test -- src/stores/__tests__/orch-subagents-store.test.ts`
Expected: FAIL — `messagesFor` 不存在。

- [ ] **Step 3: 扩展 store**

`orch-subagents-store.ts`:`State` 加 `messagesBySession` + `messagesFor`;初值 `messagesBySession: new Map()`;`ensureLoaded` 的 `.then` 内同时写两 map;`__reset` 也清 `messagesBySession`。顶部 import 加 `import type { chat_svc } from "../../wailsjs/go/models";`。`.then` 改为:
```ts
      .then((resp) => {
        const messages = resp?.messages ?? [];
        const subs = deriveSubagents(messages);
        set((s) => {
          const nextSubs = new Map(s.bySession);
          nextSubs.set(sessionId, subs);
          const nextMsgs = new Map(s.messagesBySession);
          nextMsgs.set(sessionId, messages);
          const ld = new Set(s.loading);
          ld.delete(sessionId);
          return {
            bySession: nextSubs,
            messagesBySession: nextMsgs,
            loading: ld,
          };
        });
      })
```
加 getter:
```ts
  messagesFor: (sessionId) => get().messagesBySession.get(sessionId) ?? [],
```
`__reset`:`set({ bySession: new Map(), messagesBySession: new Map(), loading: new Set() })`。

- [ ] **Step 4: 跑通过(含 S5b 既有用例)**

Run: `cd frontend && pnpm test -- src/stores/__tests__/orch-subagents-store.test.ts`
Expected: PASS。

- [ ] **Step 5:(门控)提交**

```bash
git commit frontend/src/stores/orch-subagents-store.ts \
  frontend/src/stores/__tests__/orch-subagents-store.test.ts \
  -m "✨ orch:子代理 store 顺手缓存原始 messages(钻入会话复用,免二次加载)"
```

---

## Task 2: `ConversationPanel` 只读会话面板 + 对它说

**Files:**
- Create: `frontend/src/components/agentre/orchestration/conversation-panel.tsx`
- Test: `frontend/src/components/agentre/orchestration/__tests__/conversation-panel.test.tsx`

**Interfaces:**
- Consumes:`ChatTranscript`(`../chat`)、`RunSpeak`(wailsjs)、`useOrchSubagentsStore`(`messagesFor` + `ensureLoaded`)、`AgentColor`(`../types`)。
- Produces:
  ```ts
  export function ConversationPanel(props: {
    sessionId: number;
    agentName: string;
    agentColor: AgentColor;
    onBack: () => void;
  }): JSX.Element
  ```
  testid:`conversation-panel`、返回按钮 `conversation-back`、对它说输入 `conversation-speak-input`、发送 `conversation-speak-send`。

- [ ] **Step 1: 写失败测试**

```tsx
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const runSpeak = vi.fn().mockResolvedValue(undefined);
const loadSession = vi.fn();
vi.mock("../../../../../wailsjs/go/app/App", () => ({
  RunSpeak: (...a: unknown[]) => runSpeak(...a),
  LoadChatSession: (...a: unknown[]) => loadSession(...a),
  ListChatAgents: vi.fn().mockResolvedValue({ agents: [] }),
}));
// ChatTranscript 是重组件,这里 stub 成可断言消息数的轻量占位
vi.mock("../../chat", () => ({
  ChatTranscript: ({ messages }: { messages: unknown[] }) => (
    <div data-testid="stub-transcript">{messages.length}</div>
  ),
}));

import { useOrchSubagentsStore } from "../../../../stores/orch-subagents-store";
import { ConversationPanel } from "../conversation-panel";

beforeEach(() => {
  useOrchSubagentsStore.getState().__reset();
  runSpeak.mockClear();
  loadSession.mockReset();
});

describe("ConversationPanel", () => {
  it("加载该 session 并把 messages 喂给 ChatTranscript", async () => {
    loadSession.mockResolvedValue({
      messages: [{ id: 1, blocks: [] }, { id: 2, blocks: [] }],
    });
    render(
      <ConversationPanel sessionId={701} agentName="后端" agentColor="agent-2" onBack={vi.fn()} />,
    );
    await waitFor(() =>
      expect(screen.getByTestId("stub-transcript")).toHaveTextContent("2"),
    );
  });

  it("返回按钮调 onBack", () => {
    loadSession.mockResolvedValue({ messages: [] });
    const onBack = vi.fn();
    render(
      <ConversationPanel sessionId={701} agentName="后端" agentColor="agent-2" onBack={onBack} />,
    );
    fireEvent.click(screen.getByTestId("conversation-back"));
    expect(onBack).toHaveBeenCalled();
  });

  it("对它说 → RunSpeak(sessionId, text) 并清空输入", async () => {
    loadSession.mockResolvedValue({ messages: [] });
    render(
      <ConversationPanel sessionId={701} agentName="后端" agentColor="agent-2" onBack={vi.fn()} />,
    );
    const input = screen.getByTestId("conversation-speak-input") as HTMLTextAreaElement;
    fireEvent.change(input, { target: { value: "改用 sqlmock" } });
    fireEvent.click(screen.getByTestId("conversation-speak-send"));
    await waitFor(() => expect(runSpeak).toHaveBeenCalledWith(701, "改用 sqlmock"));
  });
});
```

- [ ] **Step 2: 跑确认失败**

Run: `cd frontend && pnpm test -- src/components/agentre/orchestration/__tests__/conversation-panel.test.tsx`
Expected: FAIL — 模块不存在。

- [ ] **Step 3: 实现 `conversation-panel.tsx`**

```tsx
import * as React from "react";
import { useTranslation } from "react-i18next";
import { ArrowLeft, SendHorizontal } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { ChatTranscript } from "../chat";
import type { AgentColor } from "../types";
import { useOrchSubagentsStore } from "../../../stores/orch-subagents-store";
import { RunSpeak } from "../../../../wailsjs/go/app/App";

export function ConversationPanel({
  sessionId,
  agentName,
  agentColor,
  onBack,
}: {
  sessionId: number;
  agentName: string;
  agentColor: AgentColor;
  onBack: () => void;
}) {
  const { t } = useTranslation();
  const ensureLoaded = useOrchSubagentsStore((s) => s.ensureLoaded);
  const messages = useOrchSubagentsStore((s) => s.messagesBySession.get(sessionId));
  const [draft, setDraft] = React.useState("");
  const [sending, setSending] = React.useState(false);
  const scrollRef = React.useRef<HTMLDivElement | null>(null);

  React.useEffect(() => {
    if (sessionId) ensureLoaded(sessionId);
  }, [sessionId, ensureLoaded]);

  const speak = async () => {
    const text = draft.trim();
    if (!text || sending) return;
    setSending(true);
    try {
      await RunSpeak(sessionId, text);
      setDraft("");
    } finally {
      setSending(false);
    }
  };

  return (
    <div data-testid="conversation-panel" className="flex h-full flex-col">
      {/* 头部:返回 + agent 名 */}
      <div className="flex shrink-0 items-center gap-2 border-b border-border p-2">
        <Button
          data-testid="conversation-back"
          variant="ghost"
          size="sm"
          className="h-7 px-2 text-xs"
          onClick={onBack}
        >
          <ArrowLeft className="size-3.5" />
          {t("orchestration.conversation.backToBoard")}
        </Button>
        <span className="truncate text-sm font-medium text-foreground">
          {agentName}
        </span>
      </div>

      {/* 只读 transcript:省略所有 live*/onRerun/onEdit → 只读 */}
      <div ref={scrollRef} className="min-h-0 flex-1 overflow-y-auto">
        <ChatTranscript
          agentName={agentName}
          agentColor={agentColor}
          sessionId={sessionId}
          messages={messages ?? []}
          scrollElement={scrollRef.current}
          virtualize
          active
        />
      </div>

      {/* 对它说 */}
      <div className="flex shrink-0 items-end gap-2 border-t border-border p-2">
        <Textarea
          data-testid="conversation-speak-input"
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          placeholder={t("orchestration.conversation.speakPlaceholder")}
          rows={1}
          className="min-h-8 resize-none text-xs"
        />
        <Button
          data-testid="conversation-speak-send"
          size="sm"
          className="h-8 shrink-0 px-2"
          disabled={!draft.trim() || sending}
          onClick={() => void speak()}
          aria-label={t("orchestration.conversation.speakSend")}
        >
          <SendHorizontal className="size-3.5" />
        </Button>
      </div>
    </div>
  );
}
```

> 真实 `ChatTranscript` 在生产渲染;测试里被 stub 成轻量占位(Step 1 的 `vi.mock("../../chat")`),只断言消息数/交互,不拉起整套 transcript 依赖。

- [ ] **Step 4: 跑通过**

Run: `cd frontend && pnpm test -- src/components/agentre/orchestration/__tests__/conversation-panel.test.tsx`
Expected: PASS。

- [ ] **Step 5:(门控)提交**

```bash
git commit frontend/src/components/agentre/orchestration/conversation-panel.tsx \
  frontend/src/components/agentre/orchestration/__tests__/conversation-panel.test.tsx \
  -m "✨ orch:钻入会话只读面板(复用 ChatTranscript)+ 对它说 RunSpeak"
```

---

## Task 3: 选中契约升级为 sessionId(structure-graph + task-board)

**Files:**
- Modify: `structure-graph.tsx`(`onSelectNode` → `onSelectSession`)
- Modify: `task-board.tsx`(`onSelectTask(agentId)` → `onSelectSession(sessionId)`;`selectedAgentId` → `selectedSessionId`;**删除 `DrilldownPanel`**)
- Test: 改 `__tests__/structure-graph.test.tsx`、`__tests__/task-board.test.tsx`

**Interfaces:**
- Produces:
  - `StructureGraph` prop:`onSelectSession: (sessionId: number) => void`。单调用节点点击发该 task `sessionId`;顶层分组 per-call 子行(S4 的 `node-{id}-call-{taskId}`)点击发各自 `call.sessionId`;合并 `×N` 节点点击发其首个 call 的 `sessionId`(图上不细分,细分在板)。
  - `TaskBoard` props:`selectedSessionId: number | null`、`onSelectSession: (sessionId: number) => void`。行点击发该 task `sessionId`;高亮按 `task.sessionId === selectedSessionId`。

- [ ] **Step 1: 改 structure-graph 测试(选中契约)**

`structure-graph.test.tsx`:把既有「点击节点 node-3 调 onSelectNode(3)」改名/改断言为按 session;该用例的 `makeTask(2, 3, "running", 1)` 默认 `sessionId=0`,改为带 sessionId:
```ts
it("点击单调用节点 → onSelectSession(该 task sessionId)", () => {
  const onSelectSession = vi.fn();
  const detail = makeDetail({
    runStatus: "running",
    tasks: [makeTask(1, 2, "running"), makeTask(2, 3, "running", 1, 555)],
  });
  render(<StructureGraph detail={detail} onSelectSession={onSelectSession} />);
  fireEvent.click(screen.getByTestId("node-3"));
  expect(onSelectSession).toHaveBeenCalledWith(555);
});
```
全文件把 `onSelectNode={vi.fn()}` 改为 `onSelectSession={vi.fn()}`,`onSelectNode={onSelectNode}` 同理。

- [ ] **Step 2: 跑确认失败**

Run: `cd frontend && pnpm test -- src/components/agentre/orchestration/__tests__/structure-graph.test.tsx`
Expected: FAIL — `StructureGraph` 还是 `onSelectNode`。

- [ ] **Step 3: 改 structure-graph 实现**

`StructureGraph` 与 `NodeTree`/`NodeCard` 的 `onSelectNode: (agentId)=>void` 改为 `onSelectSession: (sessionId)=>void`。`NodeCard` 点击解析:
- 整卡 `onClick`:`node.calls[0] ? onSelectSession(node.calls[0].sessionId) : undefined`(单调用 / 合并节点默认取首 call)。
- 顶层分组子行(S4 的 `<li data-testid={node-{id}-call-{taskId}}>`)改为可点 `<button>` 包裹,`onClick={(e)=>{ e.stopPropagation(); onSelectSession(call.sessionId); }}`(stopPropagation 防冒泡到整卡)。
> 死锁 `deadlockAgentIds`/选中环等内部仍按 agentId 算,不受影响。

- [ ] **Step 4: 跑 structure-graph 通过**

Run: `cd frontend && pnpm test -- src/components/agentre/orchestration/__tests__/structure-graph.test.tsx`
Expected: PASS。

- [ ] **Step 5: 改 task-board 测试(选中契约 + 删 Drilldown)**

`task-board.test.tsx`:
- 把所有 `selectedAgentId={null}` → `selectedSessionId={null}`,`onSelectTask={...}` → `onSelectSession={...}`。
- 「点击任务行调用 onSelectTask(agentId)」改为:
```ts
it("点击任务行调用 onSelectSession(该 task sessionId)", () => {
  const onSelectSession = vi.fn();
  const tasks = [makeTask(1, 2, { sessionId: 11 }), makeTask(2, 3, { parentTaskId: 1, sessionId: 22 })];
  render(<TaskBoard detail={makeDetail(tasks)} selectedSessionId={null} onSelectSession={onSelectSession} />);
  fireEvent.click(screen.getByTestId("board-task-2"));
  expect(onSelectSession).toHaveBeenCalledWith(22);
});
```
- 删除/不再断言 `board-drilldown`(若有相关用例)。

- [ ] **Step 6: 跑确认失败**

Run: `cd frontend && pnpm test -- src/components/agentre/orchestration/__tests__/task-board.test.tsx`
Expected: FAIL。

- [ ] **Step 7: 改 task-board 实现**

- props 改 `selectedSessionId: number | null` + `onSelectSession: (sessionId: number) => void`。
- `renderRow` 的 `onClick={() => onSelectSession(task.sessionId)}`;`isSelected` 改 `task.sessionId === selectedSessionId`(逐行算,移出 group 级)。
- **删除 `DrilldownPanel` 组件定义** + 其在 tasks tab 顶部的渲染块(`selectedAgentId !== null && <DrilldownPanel .../>`),会话上移到外壳右栏(Task 4)。`selectedAgentTasks`/`selectedAgentName` 等只服务 Drilldown 的派生也一并删。

- [ ] **Step 8: 跑 task-board 通过**

Run: `cd frontend && pnpm test -- src/components/agentre/orchestration/__tests__/task-board.test.tsx`
Expected: PASS。

- [ ] **Step 9:(门控)提交**

```bash
git commit frontend/src/components/agentre/orchestration/structure-graph.tsx \
  frontend/src/components/agentre/orchestration/task-board.tsx \
  frontend/src/components/agentre/orchestration/__tests__/structure-graph.test.tsx \
  frontend/src/components/agentre/orchestration/__tests__/task-board.test.tsx \
  -m "♻️ orch:钻入选中契约 agentId→sessionId(图/板)+ 删任务板内嵌 Drilldown"
```

---

## Task 4: Run 外壳右栏二态切换(任务板 ⇄ 会话)

**Files:**
- Modify: `frontend/src/components/agentre/orchestration/index.tsx`
- Test: `frontend/src/components/agentre/orchestration/__tests__/index.test.tsx`

**Interfaces:**
- Consumes:`ConversationPanel`、`useChatAgents`(算 agentName/color)、`detail.tasks`(sessionId → agentId)。

- [ ] **Step 1: 写失败测试**

`index.test.tsx` 追加(按既有 mock 习惯;`useChatAgents` 给含 agent 3 的列表):
```ts
it("选中 session 后右栏切到 ConversationPanel, 返回回到任务板", async () => {
  // 渲染 OrchestrationRun, store 注入含 task(agent3, sessionId 900)的 detail
  // 点结构图节点 → conversation-panel 出现;点 conversation-back → 回 board
  // (依赖既有 index.test 的 store/detail 注入工具;断言 testid 切换)
});
```
> 若 `index.test.tsx` 既有工具不便直接点节点,退化为:直接断言「`selectedSessionId` 非空时渲 `conversation-panel`、为空时渲 `board-tab-tasks`」——可把右栏二态抽成可单测的子函数,或用 `findByTestId` 走完整交互。以能跑出 Red 为准。

- [ ] **Step 2: 跑确认失败**

Run: `cd frontend && pnpm test -- src/components/agentre/orchestration/__tests__/index.test.tsx`
Expected: FAIL — 无 `conversation-panel`。

- [ ] **Step 3: 改 index.tsx**

- `selectedAgentId` 状态改为 `selectedSessionId: number | null`。
- `StructureGraph onSelectSession={setSelectedSessionId}`;`ActivityFeed` 不变。
- 右栏改二态:
```tsx
          <aside className="w-80 shrink-0 border-l border-border">
            {selectedSessionId === null ? (
              <TaskBoard
                detail={detail}
                selectedSessionId={selectedSessionId}
                onSelectSession={setSelectedSessionId}
              />
            ) : (
              <ConversationPanel
                sessionId={selectedSessionId}
                agentName={selAgentName}
                agentColor={selAgentColor}
                onBack={() => setSelectedSessionId(null)}
              />
            )}
          </aside>
```
- 算选中 session 的 agent:
```tsx
  const { agents } = useChatAgents();
  const selTask = (detail?.tasks ?? []).find((t) => t.sessionId === selectedSessionId);
  const selAgent = agents.find((a) => a.id === selTask?.agentId);
  const selAgentName = selAgent?.name ?? (selTask ? `#${selTask.agentId}` : "");
  const selAgentColor = (selAgent?.avatarColor as AgentColor) ?? "agent-1";
```
> `detail` 在 `!detail||!detail.run` 早返回分支之外引用要做空安全(上面用 `detail?.`)。import `useChatAgents`、`AgentColor`、`ConversationPanel`。
- Run 切换时重置选中:`React.useEffect(() => setSelectedSessionId(null), [runId]);`(避免跨 Run 残留)。

- [ ] **Step 4: 跑 index 通过**

Run: `cd frontend && pnpm test -- src/components/agentre/orchestration/__tests__/index.test.tsx`
Expected: PASS。

- [ ] **Step 5:(门控)提交**

```bash
git commit frontend/src/components/agentre/orchestration/index.tsx \
  frontend/src/components/agentre/orchestration/__tests__/index.test.tsx \
  -m "✨ orch Run 外壳:右栏任务板 ⇄ 会话 二态切换(钻入选中 session)"
```

---

## Task 5: i18n `orchestration.conversation.*` + 全量校验

**Files:**
- Modify: `frontend/src/i18n/locales/{zh-CN,en}/common.json`

- [ ] **Step 1: 加 zh-CN 键**

`orchestration` 下新增:
```json
    "conversation": {
      "title": "会话",
      "backToBoard": "返回任务板",
      "speakPlaceholder": "对它说…(发送给该会话)",
      "speakSend": "发送"
    }
```

- [ ] **Step 2: 加 en 键**

```json
    "conversation": {
      "title": "Conversation",
      "backToBoard": "Back to board",
      "speakPlaceholder": "Speak to it… (sends to this session)",
      "speakSend": "Send"
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
Expected: 全绿(重点确认没改坏 index/structure-graph/task-board 既有用例 + `ChatTranscript` 真实渲染不被本改动牵连)。

- [ ] **Step 5:(门控)提交**

```bash
git commit frontend/src/i18n/locales/zh-CN/common.json frontend/src/i18n/locales/en/common.json \
  -m "🌐 orch:钻入会话 i18n 双语键(orchestration.conversation.*)"
```

---

## Final verification (after all tasks)

- [ ] `cd frontend && pnpm test -- src/components/agentre/orchestration src/stores/__tests__/orch-subagents-store.test.ts` — 全绿。
- [ ] `make test-frontend` + `make lint` — 全绿。
- [ ] 人工对照设计稿 `x4ZEP`/`RUK6J`(节点钻入):点节点/任务行 → 右栏切会话面板(transcript + 返回任务板 + 对它说);点返回 → 回任务板。**真机**:跑一个真 Run,钻入某 agent 会话目检 transcript 渲染 + 对它说能 RunSpeak(可并入 S7 验证)。

## Self-review notes(写计划时已核对)

1. **Spec coverage(§5 item9 / table row10 / §5.7a)**:右栏「任务板 ⇄ 会话」二态 → Task 4;只读 transcript 复用 `ChatTranscript` → Task 2;对它说 `RunSpeak(sessionId)` → Task 2(后端已支持按 session,`orch.go:144`);节点=调用=唯一 session、钻入直接 `task.sessionId`、无需会话切换器 → Task 3 选中契约。✅
2. **划界**:内联审批批准/拒绝 = **S9**(后端零调用点);钻入面板实时流 = 不做(只读快照 + 发言后重载);peer ask/reply = **S3**。均在 Global Constraints 标注。
3. **复用而非新造**:transcript 走既有 `ChatTranscript`(省 `live*`/`onRerun`/`onEdit` 即只读);session 加载复用 S5b 的 `orch-subagents-store`(Task 1 顺手缓存 messages,免二次 `LoadChatSession`)。
4. **依赖顺序**:Task 3 改的 graph/board 以 S4/S5b 后代码为基线(`node-{id}-call-{taskId}` 子行来自 S4;per-call 行/折叠子代理来自 S5b);执行须 S4 → S5/S5b → 本 plan。
5. **风险/未决(留 review)**:
   - **发言后刷新**:本 plan 发完 `RunSpeak` 只清输入,不主动重载 transcript(下次 `ensureLoaded` 已缓存不会重拉)。若要发完即见新消息,需在 speak 成功后强制重载该 session(给 store 加 `reload(sessionId)` 绕过缓存)——**留 review**,本切片先不做(读多写少、钻入重开即新)。
   - **合并 `×N` 节点点击**取首 call session(图不细分);若希望合并节点点击改为「展开任务板对应组」而非进会话,Task 3 Step 3 微调。
6. **Placeholder/类型/testid**:无 TODO;`ConversationPanel` props、`onSelectSession`、`selectedSessionId`、`messagesFor` 名字跨 Task 一致;testid `conversation-panel`/`conversation-back`/`conversation-speak-input|send`、`board-task-{id}`、`node-{id}`/`node-{id}-call-{taskId}` 实现与测试对齐。
7. **按钮嵌套**:顶层分组 per-call 子行改成可点 `<button>` 后,注意它原在 `NodeCard` 的整卡 `<button>` 内 → **HTML 不允许 button 嵌 button**。Task 3 Step 3 需把 `NodeCard` 根从 `<button>` 改为 `<div role="button" tabIndex=0 onClick + onKeyDown>`(或把整卡点击区与子行并列、不嵌套),并补 `focus-visible:ring`(DESIGN.md §无障碍)。**这是 Task 3 的实现要点,务必处理**。
