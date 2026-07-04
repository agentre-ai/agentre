# 编排对话流式化 + 完整输入框 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让编排页的对话(右栏 `ConversationPanel` + 底部 Leader footer)像普通对话页一样实时流式,并把裸输入框换成完整的 `ChatComposer`(贴图 + 权限模式)。

**Architecture:** 纯前端、后端零改。复用已有的 `ChatTranscript`(显示)、`ChatComposer`(输入)、`useChatSession`(canonical 会话加载器,自带 `activeStream` 重挂)、全局 `ChatStreamsHost`(把任何 `openStream` 的流写进 `chat-streams-store`)。新增一个隔离的发送原语 `useComposerSend` 和一个编排对话 hook `useLiveConversation`(viewer + sender 组合),**不碰 `ChatPanel`**。Leader footer 只用 sender 原语(不显示 transcript,发送后自动把右栏切到 Leader 的 `ConversationPanel` 看流)。

**Tech Stack:** React 19 + TypeScript, zustand, Wails events(`EventsOn`)、vitest + Testing Library, react-i18next。

## Global Constraints

- 前端 IPC 只走 Wails 绑定(`frontend/wailsjs`),**不加 HTTP-style app API**。
- 表单控件一律用 shadcn `@/components/ui/*`,**禁止裸 `<select>`**。
- 新增可见 UI 文案必须走 i18n:`react-i18next` 的 `t(...)` + 同步 `frontend/src/i18n/locales/{zh-CN,en}/common.json`;`i18next/no-literal-string` 会拦截 JSX 里的硬编码中文。
- 引(间接)import wailsjs runtime 的组件,其测试**必须按文件 `vi.mock("../../../wailsjs/runtime/runtime", …)`(importActual + override)**,不加全局 vite alias。
- 提交在共享分支 `develop/wyz`:**永远 `git commit <files>` 带 pathspec**,不裸 `git commit`(会卷入他人 staged 改动)。gitmoji 提交。
- 收尾 gate 必须跑到真 exit code:`cd frontend && pnpm test`(全量)+ `pnpm tsc --noEmit` + `pnpm lint`;`make … | tail` 会吞退出码。
- TDD:每个 hook/组件改动**先写失败测试、跑一次看它按预期失败,再实现**。
- 严禁在本次 diff 里夹带无关重构/格式化/import 重排。当前工作区已有他人改动(`run-new-dialog.tsx` 及其测试)—— 不要动它们。

## 关键复用点(实现前先读这些行)

- `frontend/src/components/agentre/chat-panel.tsx`
  - `doSend`(1154–1217):`SendRequest` 形状 + optimistic 插入 + `openStream`。
  - `onAutonomousEvent` + `useChatStream("chat:autonomous:"+sessionId, …)`(504–551):自发轮捕获 → `openStream`。
  - caps/权限模式(835–905):`useBackendCapabilities` / `caps.has("set_permission_mode")` / `caps.has("image_input")` / `usePermissionMode({...})` / `permissionModeMeta`。
  - `<ChatComposer …>` 用法(1986–2040):`permissionModeSlot`(`PermissionModePill`)/`onSubmit`/`contextUsage`/`supportsImageInput`。
  - `doEnqueue`(1431–):`EnqueueChatMessage({sessionId,text})` busy 兜底。
- `frontend/src/hooks/use-chat-session.ts`:返回 `{ session, messages, setMessages, error, reload }`;内部已在 `session.activeStream` 非空时 `openStream` 重挂(97–135)。
- `frontend/src/stores/chat-streams-store.ts`:`openStream({name,sessionId,assistantMessageId,streamStartedAt})`;LiveStream = `s.streams.get(sessionId)`(字段:`liveDelta/liveThinking/liveBlocks/liveRetry/streamStartedAt/liveCompacting`)。
- `frontend/src/components/agentre/chat.tsx`:`ChatTranscript` 的 live* props(`liveDelta/liveThinking/liveTargetId/liveBlocks/liveRetry/liveStreamStartedAt/streaming/liveCompacting`);`ChatComposer` / `ChatComposerSubmit` / `ChatImageAttachment`。
- `frontend/src/stores/session-status-store.ts`:`markSessionRunning(sessionId)`;每 session 的 `doneTick`(turn 结束自增)。
- `frontend/src/components/agentre/permission-mode.tsx`(实际路径以 import 为准):`usePermissionMode` / `PermissionModePill`。

## File Structure

- **Create** `frontend/src/hooks/use-composer-send.ts` — 发送原语:caps + 权限模式 + `submit({text,images})`(SendChatMessage + openStream + busy→enqueue)。viewer 无关,可被 footer 单独用。
- **Create** `frontend/src/hooks/use-composer-send.test.ts` — 单测。
- **Create** `frontend/src/hooks/use-live-conversation.ts` — 编排对话 hook:`useChatSession` + 自发轮 watch + live overlay + doneTick→reload + 组合 `useComposerSend`(带 optimistic)。
- **Create** `frontend/src/hooks/use-live-conversation.test.ts` — 单测。
- **Modify** `frontend/src/components/agentre/orchestration/conversation-panel.tsx` — transcript 接 live props;footer 换 `ChatComposer`;删本地 `speak()`/`RunSpeak`。
- **Modify** `frontend/src/components/agentre/orchestration/__tests__/conversation-panel.test.tsx`(若不存在则 Create)。
- **Modify** `frontend/src/components/agentre/orchestration/index.tsx` — Leader footer 换 `ChatComposer` + `useComposerSend` + 发送后 `setSelectedSessionId(leaderSessionId)`。
- **Modify/Create** `frontend/src/components/agentre/orchestration/__tests__/orchestration-run.test.tsx`(Leader footer 行为)。
- **Modify** `frontend/src/i18n/locales/zh-CN/common.json` + `frontend/src/i18n/locales/en/common.json` — 新文案。

---

### Task 1: `useComposerSend` 发送原语

**Files:**
- Create: `frontend/src/hooks/use-composer-send.ts`
- Test: `frontend/src/hooks/use-composer-send.test.ts`

**Interfaces:**
- Consumes: `SendChatMessage` / `EnqueueChatMessage`(`wailsjs/go/app/App`)、`chat_svc.SendRequest`(`wailsjs/go/models`)、`useChatStreamsStore.openStream`、`markSessionRunning`(session-status-store)、`useBackendCapabilities`、`usePermissionMode`。
- Produces:
  ```ts
  type ComposerSendResult = { sessionId: number; userMessageId: number; assistantMessageId: number };
  type UseComposerSend = {
    submit: (msg: { text: string; images?: ChatImageAttachment[] }) => Promise<ComposerSendResult | null>;
    sending: boolean;
    error: string | null;
    // composer 支撑
    backendType: string;
    isModeSwitchable: boolean;
    supportsImageInput: boolean;
    permissionMode: ReturnType<typeof usePermissionMode>;
    permissionModeMeta: { allowedModes: string[]; defaultMode: string; switchableDuringTurn: boolean; order: string[] };
  };
  function useComposerSend(args: {
    sessionId: number;
    agentId: number;
    backendType: string;
    /** turn 进行中(有 live stream)时 submit 走 enqueue 而非新起 turn */
    isRunning: boolean;
    /** optimistic 插入回调;footer 无 transcript 时省略 */
    onOptimistic?: (r: ComposerSendResult, text: string, images: ChatImageAttachment[]) => void;
    /** 权限模式初值(来自 session detail;footer 可省) */
    initialMode?: string;
    initialModeAtLaunch?: string;
    hasActiveSession?: boolean;
  }): UseComposerSend;
  ```

- [ ] **Step 1: 写失败测试**

`frontend/src/hooks/use-composer-send.test.ts`:
```ts
import { renderHook, act } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach } from "vitest";

const sendMock = vi.fn();
const enqueueMock = vi.fn();
const openStreamMock = vi.fn();
const markRunningMock = vi.fn();

vi.mock("../../wailsjs/go/app/App", () => ({
  SendChatMessage: (...a: unknown[]) => sendMock(...a),
  EnqueueChatMessage: (...a: unknown[]) => enqueueMock(...a),
}));
vi.mock("../../wailsjs/go/models", () => ({
  chat_svc: { SendRequest: { createFrom: (x: unknown) => x } },
}));
vi.mock("@/stores/chat-streams-store", () => ({
  useChatStreamsStore: Object.assign(
    (sel: (s: unknown) => unknown) => sel({ openStream: openStreamMock }),
    { getState: () => ({ openStream: openStreamMock }) },
  ),
}));
vi.mock("@/stores/session-status-store", () => ({
  markSessionRunning: (...a: unknown[]) => markRunningMock(...a),
}));
vi.mock("@/hooks/use-backend-capabilities", () => ({
  useBackendCapabilities: () => ({
    caps: new Set(["set_permission_mode", "image_input"]),
  }),
}));
vi.mock("@/components/agentre/permission-mode", () => ({
  usePermissionMode: () => ({ mode: "default", setMode: vi.fn() }),
  PermissionModePill: () => null,
}));

import { useComposerSend } from "./use-composer-send";

describe("useComposerSend", () => {
  beforeEach(() => {
    sendMock.mockReset();
    enqueueMock.mockReset();
    openStreamMock.mockReset();
    markRunningMock.mockReset();
  });

  it("idle 时 submit 走 SendChatMessage + openStream + optimistic", async () => {
    sendMock.mockResolvedValue({
      sessionId: 7, userMessageId: 100, assistantMessageId: 101, stream: "chat:stream:7:101",
    });
    const onOptimistic = vi.fn();
    const { result } = renderHook(() =>
      useComposerSend({ sessionId: 7, agentId: 3, backendType: "claudecode", isRunning: false, onOptimistic }),
    );
    await act(async () => {
      await result.current.submit({ text: "hi" });
    });
    expect(sendMock).toHaveBeenCalledWith(
      expect.objectContaining({ sessionId: 7, agentId: 3, text: "hi", permissionMode: "default" }),
    );
    expect(openStreamMock).toHaveBeenCalledWith(
      expect.objectContaining({ name: "chat:stream:7:101", sessionId: 7, assistantMessageId: 101 }),
    );
    expect(markRunningMock).toHaveBeenCalledWith(7);
    expect(onOptimistic).toHaveBeenCalled();
    expect(enqueueMock).not.toHaveBeenCalled();
  });

  it("isRunning 时 submit 走 EnqueueChatMessage,不新起 turn", async () => {
    enqueueMock.mockResolvedValue({ queuedId: "q1" });
    const { result } = renderHook(() =>
      useComposerSend({ sessionId: 7, agentId: 3, backendType: "claudecode", isRunning: true }),
    );
    await act(async () => {
      await result.current.submit({ text: "later" });
    });
    expect(enqueueMock).toHaveBeenCalledWith({ sessionId: 7, text: "later" });
    expect(sendMock).not.toHaveBeenCalled();
  });

  it("空文本 submit 直接忽略", async () => {
    const { result } = renderHook(() =>
      useComposerSend({ sessionId: 7, agentId: 3, backendType: "claudecode", isRunning: false }),
    );
    await act(async () => {
      await result.current.submit({ text: "   " });
    });
    expect(sendMock).not.toHaveBeenCalled();
    expect(enqueueMock).not.toHaveBeenCalled();
  });
});
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd frontend && pnpm test -- src/hooks/use-composer-send.test.ts`
Expected: FAIL —— `Failed to resolve import "./use-composer-send"` / `useComposerSend is not a function`。

- [ ] **Step 3: 写实现**

`frontend/src/hooks/use-composer-send.ts`:
```ts
import * as React from "react";
import { SendChatMessage, EnqueueChatMessage } from "../../wailsjs/go/app/App";
import { chat_svc } from "../../wailsjs/go/models";
import type { ChatImageAttachment } from "@/components/agentre/chat";
import { useChatStreamsStore } from "@/stores/chat-streams-store";
import { markSessionRunning } from "@/stores/session-status-store";
import { useBackendCapabilities } from "@/hooks/use-backend-capabilities";
import { usePermissionMode } from "@/components/agentre/permission-mode";

export type ComposerSendResult = {
  sessionId: number;
  userMessageId: number;
  assistantMessageId: number;
};

const EMPTY_META = {
  allowedModes: [] as string[],
  defaultMode: "",
  switchableDuringTurn: false,
  order: [] as string[],
};

export function useComposerSend(args: {
  sessionId: number;
  agentId: number;
  backendType: string;
  isRunning: boolean;
  onOptimistic?: (
    r: ComposerSendResult,
    text: string,
    images: ChatImageAttachment[],
  ) => void;
  initialMode?: string;
  initialModeAtLaunch?: string;
  hasActiveSession?: boolean;
}) {
  const {
    sessionId,
    agentId,
    backendType,
    isRunning,
    onOptimistic,
    initialMode,
    initialModeAtLaunch,
    hasActiveSession,
  } = args;
  const openStream = useChatStreamsStore((s) => s.openStream);
  const { caps } = useBackendCapabilities(
    sessionId > 0 ? undefined : backendType,
  );
  const isModeSwitchable = !!caps?.has("set_permission_mode");
  const supportsImageInput = !!caps?.has("image_input");
  const permissionModeMeta = caps?.permissionModeMeta ?? EMPTY_META;
  const permissionMode = usePermissionMode({
    sessionId: isModeSwitchable && sessionId > 0 ? sessionId : undefined,
    permissionModeMeta,
    runtimeKey: backendType,
    initialMode,
    initialModeAtLaunch,
    hasActiveSession: hasActiveSession ?? false,
  });

  const [sending, setSending] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);

  const submit = React.useCallback(
    async (msg: { text: string; images?: ChatImageAttachment[] }) => {
      const text = msg.text.trim();
      const images = msg.images ?? [];
      if (!text || sending) return null;
      setSending(true);
      setError(null);
      try {
        if (isRunning) {
          await EnqueueChatMessage({ sessionId, text });
          return null;
        }
        const payload: Record<string, unknown> = {
          sessionId,
          agentId,
          text,
          projectId: 0,
          permissionMode: isModeSwitchable ? permissionMode.mode : "",
        };
        if (images.length > 0) {
          payload.images = images.map((i) => ({ name: i.name, dataUrl: i.dataUrl }));
        }
        const resp = await SendChatMessage(
          chat_svc.SendRequest.createFrom(payload),
        );
        const r: ComposerSendResult = {
          sessionId: resp.sessionId,
          userMessageId: resp.userMessageId,
          assistantMessageId: resp.assistantMessageId,
        };
        markSessionRunning(resp.sessionId);
        openStream({
          name: resp.stream,
          sessionId: resp.sessionId,
          assistantMessageId: resp.assistantMessageId,
          streamStartedAt: Date.now(),
        });
        onOptimistic?.(r, text, images);
        return r;
      } catch (e: unknown) {
        setError(e instanceof Error ? e.message : String(e));
        return null;
      } finally {
        setSending(false);
      }
    },
    [
      sessionId,
      agentId,
      isRunning,
      isModeSwitchable,
      permissionMode,
      openStream,
      onOptimistic,
      sending,
    ],
  );

  return {
    submit,
    sending,
    error,
    backendType,
    isModeSwitchable,
    supportsImageInput,
    permissionMode,
    permissionModeMeta,
  };
}
```
> 注:`useBackendCapabilities` 返回的 `caps` 上挂有 `permissionModeMeta`(见 chat-panel.tsx:898)。若 TS 类型未暴露该字段,按 chat-panel 同样方式访问(`caps?.permissionModeMeta`)。`Date.now()` 在前端可用(仅 workflow 脚本禁用)。

- [ ] **Step 4: 跑测试确认通过**

Run: `cd frontend && pnpm test -- src/hooks/use-composer-send.test.ts`
Expected: PASS(3 passed)。

- [ ] **Step 5: 提交**

```bash
git add frontend/src/hooks/use-composer-send.ts frontend/src/hooks/use-composer-send.test.ts
git commit frontend/src/hooks/use-composer-send.ts frontend/src/hooks/use-composer-send.test.ts \
  -m "✨ orchestration: useComposerSend 发送原语(SendChatMessage+openStream+busy enqueue+权限模式)"
```

---

### Task 2: `useLiveConversation` 编排对话 hook

**Files:**
- Create: `frontend/src/hooks/use-live-conversation.ts`
- Test: `frontend/src/hooks/use-live-conversation.test.ts`

**Interfaces:**
- Consumes: Task 1 的 `useComposerSend`;`useChatSession`(`{session,messages,setMessages,reload}`);`useChatStream`;`useChatStreamsStore`(LiveStream selector);`useSessionStatusStore`(`doneTick`);optimistic helpers `optimisticUser` / `optimisticAssistantPlaceholder`(与 chat-panel 同源 import)。
- Produces:
  ```ts
  function useLiveConversation(sessionId: number, agentId: number): {
    messages: chat_svc.ChatMessage[];
    live: {
      liveDelta: string; liveThinking: string; liveBlocks: unknown[];
      liveRetry: unknown | null; liveStreamStartedAt: number;
      streaming: boolean; liveCompacting: boolean;
    };
    submit: UseComposerSend["submit"];
    sending: boolean;
    isModeSwitchable: boolean;
    supportsImageInput: boolean;
    permissionMode: UseComposerSend["permissionMode"];
    permissionModeMeta: UseComposerSend["permissionModeMeta"];
    backendType: string;
    contextUsage: { used: number; max: number };
  }
  ```

- [ ] **Step 1: 写失败测试**

`frontend/src/hooks/use-live-conversation.test.ts`(聚焦 hook 的编排特有行为:自发轮 openStream、live overlay 透出、doneTick→reload):
```ts
import { renderHook, act } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach } from "vitest";

const openStreamMock = vi.fn();
const reloadMock = vi.fn();
let autonomousHandler: ((ev: unknown) => void) | null = null;
let liveStream: unknown = null;
let doneTick = 0;

vi.mock("@/hooks/use-chat-session", () => ({
  useChatSession: () => ({
    session: { backendType: "claudecode", contextWindow: 0, permissionMode: "default" },
    messages: [{ id: 1, role: "user" }],
    setMessages: vi.fn(),
    reload: reloadMock,
    error: null,
  }),
}));
vi.mock("@/hooks/use-chat-stream", () => ({
  useChatStream: (_name: string | null, handler: (ev: unknown) => void) => {
    autonomousHandler = handler;
  },
}));
vi.mock("@/stores/chat-streams-store", () => ({
  useChatStreamsStore: Object.assign(
    (sel: (s: unknown) => unknown) =>
      sel({ streams: new Map(liveStream ? [[7, liveStream]] : []), openStream: openStreamMock }),
    { getState: () => ({ openStream: openStreamMock }) },
  ),
}));
vi.mock("@/stores/session-status-store", () => ({
  markSessionRunning: vi.fn(),
  useSessionStatusStore: (sel: (s: unknown) => unknown) =>
    sel({ statuses: new Map([[7, { doneTick }]]) }),
}));
vi.mock("./use-composer-send", () => ({
  useComposerSend: () => ({
    submit: vi.fn(), sending: false, error: null, backendType: "claudecode",
    isModeSwitchable: true, supportsImageInput: true,
    permissionMode: { mode: "default" }, permissionModeMeta: { order: [] },
  }),
}));

import { useLiveConversation } from "./use-live-conversation";

describe("useLiveConversation", () => {
  beforeEach(() => {
    openStreamMock.mockReset();
    reloadMock.mockReset();
    autonomousHandler = null;
    liveStream = null;
    doneTick = 0;
  });

  it("收到 autonomous_started 帧 → openStream", () => {
    renderHook(() => useLiveConversation(7, 3));
    act(() => {
      autonomousHandler?.({
        kind: "autonomous_started",
        stream: "chat:stream:7:200",
        assistantMessage: { id: 200, role: "assistant" },
      });
    });
    expect(openStreamMock).toHaveBeenCalledWith(
      expect.objectContaining({ name: "chat:stream:7:200", sessionId: 7, assistantMessageId: 200 }),
    );
  });

  it("chat-streams-store 有 LiveStream 时把 live overlay 透出", () => {
    liveStream = {
      liveDelta: "partial", liveThinking: "", liveBlocks: [], liveRetry: null,
      streamStartedAt: 123, liveCompacting: false,
    };
    const { result } = renderHook(() => useLiveConversation(7, 3));
    expect(result.current.live.liveDelta).toBe("partial");
    expect(result.current.live.streaming).toBe(true);
  });

  it("doneTick 自增 → 调 reload 对账持久消息", () => {
    const { rerender } = renderHook(() => useLiveConversation(7, 3));
    reloadMock.mockClear();
    doneTick = 1;
    rerender();
    expect(reloadMock).toHaveBeenCalled();
  });
});
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd frontend && pnpm test -- src/hooks/use-live-conversation.test.ts`
Expected: FAIL —— import 无法解析。

- [ ] **Step 3: 写实现**

`frontend/src/hooks/use-live-conversation.ts`:
```ts
import * as React from "react";
import type { chat_svc, ChatStreamEvent } from "../../wailsjs/go/models";
import { useChatSession } from "@/hooks/use-chat-session";
import { useChatStream } from "@/hooks/use-chat-stream";
import { useChatStreamsStore } from "@/stores/chat-streams-store";
import {
  markSessionRunning,
  useSessionStatusStore,
} from "@/stores/session-status-store";
import {
  optimisticUser,
  optimisticAssistantPlaceholder,
} from "@/components/agentre/chat"; // 与 chat-panel 同源;若实际导出在别处,按其 import 路径
import { useComposerSend } from "./use-composer-send";

const EMPTY_LIVE = {
  liveDelta: "",
  liveThinking: "",
  liveBlocks: [] as unknown[],
  liveRetry: null as unknown,
  liveStreamStartedAt: 0,
  streaming: false,
  liveCompacting: false,
};

export function useLiveConversation(sessionId: number, agentId: number) {
  const { session, messages, setMessages, reload } = useChatSession(sessionId);
  const openStream = useChatStreamsStore((s) => s.openStream);

  // ── live overlay ──
  const stream = useChatStreamsStore((s) =>
    sessionId ? (s.streams.get(sessionId) ?? null) : null,
  );
  const live = stream
    ? {
        liveDelta: stream.liveDelta,
        liveThinking: stream.liveThinking,
        liveBlocks: stream.liveBlocks,
        liveRetry: stream.liveRetry,
        liveStreamStartedAt: stream.streamStartedAt,
        streaming: true,
        liveCompacting: stream.liveCompacting,
      }
    : EMPTY_LIVE;

  // ── 自发轮捕获(dispatch 派活 / Leader kickoff / peer ask / 用户 speak) ──
  const onAutonomous = React.useCallback(
    (ev: ChatStreamEvent) => {
      if (ev.kind === "subagent_activity_started") {
        if (!ev.stream || !ev.launchMessageId) return;
        openStream({
          name: ev.stream,
          sessionId,
          assistantMessageId: ev.launchMessageId,
          streamStartedAt: Date.now(),
        });
        return;
      }
      if (ev.kind !== "autonomous_started" || !ev.assistantMessage || !ev.stream) return;
      const amsg = ev.assistantMessage;
      markSessionRunning(sessionId);
      openStream({
        name: ev.stream,
        sessionId,
        assistantMessageId: amsg.id,
        streamStartedAt: Date.now(),
      });
      setMessages((prev) =>
        prev.some((m) => m.id === amsg.id) ? prev : [...prev, amsg],
      );
    },
    [sessionId, openStream, setMessages],
  );
  useChatStream(sessionId ? `chat:autonomous:${sessionId}` : null, onAutonomous);

  // ── turn 结束 → reload 持久消息对账 ──
  const doneTick = useSessionStatusStore((s) =>
    sessionId ? (s.statuses.get(sessionId)?.doneTick ?? 0) : 0,
  );
  const lastSeenDoneTick = React.useRef(doneTick);
  React.useEffect(() => {
    if (doneTick !== lastSeenDoneTick.current) {
      lastSeenDoneTick.current = doneTick;
      reload();
    }
  }, [doneTick, reload]);

  // ── sender(带 optimistic 插入) ──
  const onOptimistic = React.useCallback(
    (r: { sessionId: number; userMessageId: number; assistantMessageId: number }, text: string, images) => {
      setMessages((prev) => [
        ...prev,
        optimisticUser(r.userMessageId, r.sessionId, text, images),
        optimisticAssistantPlaceholder(r.assistantMessageId, r.sessionId),
      ]);
    },
    [setMessages],
  );
  const sender = useComposerSend({
    sessionId,
    agentId,
    backendType: session?.backendType ?? "",
    isRunning: live.streaming,
    onOptimistic,
    initialMode: session?.permissionMode,
    initialModeAtLaunch: session?.permissionModeAtLaunch,
    hasActiveSession: messages.length > 0,
  });

  const contextUsage = {
    used: 0,
    max: session?.contextWindow ?? 0,
  };

  return {
    messages,
    live,
    submit: sender.submit,
    sending: sender.sending,
    isModeSwitchable: sender.isModeSwitchable,
    supportsImageInput: sender.supportsImageInput,
    permissionMode: sender.permissionMode,
    permissionModeMeta: sender.permissionModeMeta,
    backendType: sender.backendType,
    contextUsage,
  };
}
```
> 注:`contextUsage.used` 先给 0(v1 不做进度条精算);要精算可后续复用 chat-panel 的 `computeComposerContextUsage`。若 `optimisticUser/optimisticAssistantPlaceholder` 的真实导出路径不是 `chat`,按 chat-panel.tsx 顶部的 import 改。

- [ ] **Step 4: 跑测试确认通过**

Run: `cd frontend && pnpm test -- src/hooks/use-live-conversation.test.ts`
Expected: PASS(3 passed)。

- [ ] **Step 5: 提交**

```bash
git add frontend/src/hooks/use-live-conversation.ts frontend/src/hooks/use-live-conversation.test.ts
git commit frontend/src/hooks/use-live-conversation.ts frontend/src/hooks/use-live-conversation.test.ts \
  -m "✨ orchestration: useLiveConversation(useChatSession+自发轮 watch+live overlay+doneTick reload)"
```

---

### Task 3: `ConversationPanel` 接 live props + 换 `ChatComposer`

**Files:**
- Modify: `frontend/src/components/agentre/orchestration/conversation-panel.tsx`
- Test: `frontend/src/components/agentre/orchestration/__tests__/conversation-panel.test.tsx`(不存在则 Create)

**Interfaces:**
- Consumes: Task 2 的 `useLiveConversation(sessionId, agentId)`;`ChatComposer`/`ChatComposerSubmit`(`../chat`);`PermissionModePill`(`../permission-mode`)。
- Produces: 无新导出(组件内部改造)。行为:transcript 逐字流式 + 输入框为 ChatComposer。

- [ ] **Step 1: 写失败测试**

`__tests__/conversation-panel.test.tsx`(遵守 wails runtime per-file mock;mock `useLiveConversation` 与 `../chat` 的 `ChatTranscript`/`ChatComposer` 以隔离):
```tsx
import { render, screen } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";

vi.mock("../../../../wailsjs/runtime/runtime", () => ({
  EventsOn: () => () => {}, EventsOff: () => {}, EventsEmit: () => {},
}));
vi.mock("../../../../wailsjs/go/app/App", () => ({ RunSpeak: vi.fn() }));

const liveConv = {
  messages: [{ id: 1, role: "assistant" }],
  live: { liveDelta: "streaming-text", liveThinking: "", liveBlocks: [], liveRetry: null, liveStreamStartedAt: 1, streaming: true, liveCompacting: false },
  submit: vi.fn(), sending: false, isModeSwitchable: false, supportsImageInput: true,
  permissionMode: { mode: "default" }, permissionModeMeta: { order: [] },
  backendType: "claudecode", contextUsage: { used: 0, max: 0 },
};
vi.mock("@/hooks/use-live-conversation", () => ({ useLiveConversation: () => liveConv }));
// 用真 ChatTranscript 会太重;断言 live prop 透传即可 —— stub 出来读 props
vi.mock("../../chat", () => ({
  ChatTranscript: (p: { liveDelta?: string }) => <div data-testid="tx" data-live={p.liveDelta} />,
  ChatComposer: (p: { onSubmit?: (m: unknown) => void }) => (
    <button data-testid="composer" onClick={() => p.onSubmit?.({ text: "hello" })}>send</button>
  ),
}));

import { ConversationPanel } from "../conversation-panel";

describe("ConversationPanel streaming", () => {
  it("把 live overlay 透传给 ChatTranscript", () => {
    render(
      <ConversationPanel sessionId={7} agentName="Alice" agentColor="agent-1" onBack={() => {}} runId={1} agentId={3} />,
    );
    expect(screen.getByTestId("tx").getAttribute("data-live")).toBe("streaming-text");
  });

  it("ChatComposer onSubmit → hook.submit", () => {
    render(
      <ConversationPanel sessionId={7} agentName="Alice" agentColor="agent-1" onBack={() => {}} runId={1} agentId={3} />,
    );
    screen.getByTestId("composer").click();
    expect(liveConv.submit).toHaveBeenCalledWith(expect.objectContaining({ text: "hello" }));
  });
});
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd frontend && pnpm test -- src/components/agentre/orchestration/__tests__/conversation-panel.test.tsx`
Expected: FAIL —— 现组件不读 `useLiveConversation`、无 `ChatComposer`(`data-live` 为空 / 无 composer testid)。

- [ ] **Step 3: 改实现**

改 `conversation-panel.tsx`:
1. 顶部 import 增:`useLiveConversation`、`ChatComposer`、`ChatComposerSubmit`、`PermissionModePill`;删 `useOrchSubagentsStore`、`RunSpeak`、`Textarea`、本地 `draft/sending/speak` 状态。保留 `useOrchRunStore`(awaiting/status)、`AgentAvatar/StatusDot`、header/awaiting callout。
2. 组件体替换数据来源:
```tsx
const {
  messages, live, submit, sending, isModeSwitchable, supportsImageInput,
  permissionMode, permissionModeMeta, backendType, contextUsage,
} = useLiveConversation(sessionId, agentId ?? 0);
```
   删掉 `ensureLoaded/reload/messagesBySession/EMPTY_MESSAGES` 与 `React.useEffect(ensureLoaded)`。`isAwaiting/agentTaskCount/agentStatus/agentTaskStatus/statusLabelKey` 保持不变(仍读 `useOrchRunStore`)。
3. transcript 追加 live props:
```tsx
<ChatTranscript
  agentName={agentName}
  agentColor={agentColor}
  sessionId={sessionId}
  messages={messages}
  scrollElement={scrollEl}
  virtualize
  active
  liveDelta={live.liveDelta}
  liveThinking={live.liveThinking}
  liveBlocks={live.liveBlocks}
  liveRetry={live.liveRetry}
  liveStreamStartedAt={live.liveStreamStartedAt}
  streaming={live.streaming}
  liveCompacting={live.liveCompacting}
/>
```
4. footer 整块替换为 ChatComposer:
```tsx
<div className="shrink-0 border-t border-border bg-card px-3 py-2.5">
  <ChatComposer
    placeholder={t("orchestration.conversation.speakPlaceholder")}
    backendType={backendType}
    supportsImageInput={supportsImageInput}
    contextUsage={contextUsage}
    permissionModeSlot={
      isModeSwitchable ? (
        <PermissionModePill
          mode={permissionMode.mode}
          modes={permissionModeMeta.order}
          onSelect={permissionMode.setMode}
          errorMessage={permissionMode.error}
          runtimeKey={backendType}
        />
      ) : null
    }
    onShiftTab={isModeSwitchable ? permissionMode.cycleMode : undefined}
    onSubmit={(m: ChatComposerSubmit | string) => {
      const msg = typeof m === "string" ? { text: m } : m;
      void submit(msg);
    }}
  />
</div>
```
   `sending` 可用于禁用态(ChatComposer 内部有提交态,不强制)。删掉旧 `data-testid="conversation-speak-input/send"` 的 Textarea+Button 块。

- [ ] **Step 4: 跑测试确认通过**

Run: `cd frontend && pnpm test -- src/components/agentre/orchestration/__tests__/conversation-panel.test.tsx`
Expected: PASS(2 passed)。

- [ ] **Step 5: 提交**

```bash
git add frontend/src/components/agentre/orchestration/conversation-panel.tsx \
        frontend/src/components/agentre/orchestration/__tests__/conversation-panel.test.tsx
git commit frontend/src/components/agentre/orchestration/conversation-panel.tsx \
           frontend/src/components/agentre/orchestration/__tests__/conversation-panel.test.tsx \
  -m "✨ orchestration: ConversationPanel 流式化(live overlay 透传 ChatTranscript + 换 ChatComposer)"
```

---

### Task 4: Leader footer 换 `ChatComposer` + 发送后自动开右栏 Leader 面板

**Files:**
- Modify: `frontend/src/components/agentre/orchestration/index.tsx`
- Test: `frontend/src/components/agentre/orchestration/__tests__/orchestration-run.test.tsx`(不存在则 Create)

**Interfaces:**
- Consumes: Task 1 的 `useComposerSend`(sender-only,不订阅流);`ChatComposer`;`PermissionModePill`;`useChatAgents`(拿 leader 的 backendType);现有 `leaderSessionId` 派生。
- Produces:无新导出;行为:footer 是 ChatComposer,提交后 `setSelectedSessionId(leaderSessionId)`。

- [ ] **Step 1: 写失败测试**

`__tests__/orchestration-run.test.tsx`(mock 掉重子组件,聚焦 footer 提交路径):
```tsx
import { render, screen } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";

vi.mock("../../../../wailsjs/runtime/runtime", () => ({
  EventsOn: () => () => {}, EventsOff: () => {}, EventsEmit: () => {},
}));
vi.mock("../../../../wailsjs/go/app/App", () => ({ RunResume: vi.fn(), RunSpeak: vi.fn() }));

const submitMock = vi.fn();
vi.mock("@/hooks/use-composer-send", () => ({
  useComposerSend: () => ({
    submit: submitMock, sending: false, error: null, backendType: "claudecode",
    isModeSwitchable: false, supportsImageInput: true,
    permissionMode: { mode: "default" }, permissionModeMeta: { order: [] },
  }),
}));
vi.mock("../../chat", () => ({
  ChatComposer: (p: { onSubmit?: (m: unknown) => void }) => (
    <button data-testid="leader-composer" onClick={() => p.onSubmit?.({ text: "to leader" })}>send</button>
  ),
}));
// 其余重子组件(StructureGraph/TaskBoard/ConversationPanel/RunFlowOverlay 等)按需 stub
// 使 orchestration-run 能在 detail 就绪下渲染出 footer。detail 走 useOrchRunStore mock。

// … provide useOrchRunStore mock returning a detail with run.leaderAgentId + a leader task with sessionId>0 …

import { OrchestrationRun } from "../index";

describe("Leader footer streaming", () => {
  it("footer ChatComposer 提交 → useComposerSend.submit + 切右栏到 Leader 会话", () => {
    render(<OrchestrationRun runId={1} title="R" />);
    screen.getByTestId("leader-composer").click();
    expect(submitMock).toHaveBeenCalledWith(expect.objectContaining({ text: "to leader" }));
    // 断言右栏切到 Leader:ConversationPanel 出现(用其 data-testid="conversation-panel")
    expect(screen.queryByTestId("conversation-panel")).not.toBeNull();
  });
});
```
> 注:`OrchestrationRun` 依赖较多子组件与 `useOrchRunStore`。测试里把 `StructureGraph/TaskBoard/RunFlowOverlay/ActivityFeed/RunHeader/ToggleBar` stub 成占位,并 mock `useOrchRunStore` 返回含 `run.leaderAgentId` + 一条 leader task(`sessionId>0`)的 detail;`ConversationPanel` 保留真组件或 stub 成带 `data-testid="conversation-panel"` 的占位(用于断言右栏切换)。参考同目录既有测试的 mock 写法。

- [ ] **Step 2: 跑测试确认失败**

Run: `cd frontend && pnpm test -- src/components/agentre/orchestration/__tests__/orchestration-run.test.tsx`
Expected: FAIL —— 现 footer 是裸 `<input>`,无 `leader-composer`,提交也不切右栏。

- [ ] **Step 3: 改实现**

改 `index.tsx`:
1. import 增:`useComposerSend`、`ChatComposer`、`ChatComposerSubmit`、`PermissionModePill`;删 footer 用的 `MessageSquare/SendHorizontal` 裸 input 相关(如不再用)、`leaderMsg` state、`handleLeaderSend/handleLeaderKeyDown`、`RunSpeak`(若无其它调用)。
2. 派生 leader backendType:
```tsx
const leaderAgent = agents.find((a) => a.id === detail?.run?.leaderAgentId);
const leaderSender = useComposerSend({
  sessionId: leaderSessionId ?? 0,
  agentId: detail?.run?.leaderAgentId ?? 0,
  backendType: (leaderAgent?.backendType as string) ?? "",
  isRunning: false, // footer 不订阅流;busy 时后端 Send 会拒并由 sender 吞成 error(v1 可接受)
});
```
   > `isRunning: false`:footer 无 live overlay 可判 busy;若目标正跑,`SendChatMessage` 返回 `ChatSendInFlight`,`useComposerSend` 现会吞成 `error`。v1 可接受(用户改由右栏面板续发);如需 steer,follow-up 再把 footer 也接 live 判断。
3. footer JSX 换成 ChatComposer + 提交后切右栏:
```tsx
<div data-testid="orch-footer" className="shrink-0 border-t border-border bg-card px-5 py-3">
  <ChatComposer
    placeholder={t("orchestration.run.speakLeaderPlaceholder")}
    backendType={leaderSender.backendType}
    supportsImageInput={leaderSender.supportsImageInput}
    permissionModeSlot={
      leaderSender.isModeSwitchable ? (
        <PermissionModePill
          mode={leaderSender.permissionMode.mode}
          modes={leaderSender.permissionModeMeta.order}
          onSelect={leaderSender.permissionMode.setMode}
          runtimeKey={leaderSender.backendType}
        />
      ) : null
    }
    onSubmit={(m: ChatComposerSubmit | string) => {
      if (!leaderSessionId) return;
      const msg = typeof m === "string" ? { text: m } : m;
      void leaderSender.submit(msg).then(() => {
        setSelectedSessionId(leaderSessionId); // 切右栏到 Leader 面板看流
      });
    }}
  />
</div>
```
   保留死锁 banner 的「介入」按钮:原先 `footerInputRef.current?.focus()` 改为 `setSelectedSessionId(leaderSessionId)`(把用户带到 Leader 对话)或保留一个可 focus 的元素;简化为切到 Leader 面板即可,删掉 `footerInputRef`。

- [ ] **Step 4: 跑测试确认通过**

Run: `cd frontend && pnpm test -- src/components/agentre/orchestration/__tests__/orchestration-run.test.tsx`
Expected: PASS(1 passed)。

- [ ] **Step 5: 提交**

```bash
git add frontend/src/components/agentre/orchestration/index.tsx \
        frontend/src/components/agentre/orchestration/__tests__/orchestration-run.test.tsx
git commit frontend/src/components/agentre/orchestration/index.tsx \
           frontend/src/components/agentre/orchestration/__tests__/orchestration-run.test.tsx \
  -m "✨ orchestration: Leader footer 换 ChatComposer + 发送后自动开右栏 Leader 会话看流"
```

---

### Task 5: i18n 文案补齐 + 全量 gate

**Files:**
- Modify: `frontend/src/i18n/locales/zh-CN/common.json`
- Modify: `frontend/src/i18n/locales/en/common.json`

**Interfaces:**
- Consumes:前面 Task 用到的 key(`orchestration.conversation.speakPlaceholder` 已存在;`orchestration.run.speakLeaderPlaceholder` 已存在)。本 Task 只补**新增**的 key(若上面步骤引入了新文案,如 composer 内的空态/错误提示)。

- [ ] **Step 1: 找出新增 key**

Run: `cd frontend && pnpm test -- src/__tests__/i18n.test.ts`
Expected:若无新增 key 则 PASS;若前面引入了未登记的 `t("…")`,这里会报 missing key —— 记下来。

- [ ] **Step 2: 补 key(如有)**

把缺失 key 同步进 `zh-CN/common.json` 与 `en/common.json`(两边都要,值分别中/英)。复用既有 `orchestration.*` 命名前缀。若无新增 key,跳过。

- [ ] **Step 3: 跑 i18n 测试确认通过**

Run: `cd frontend && pnpm test -- src/__tests__/i18n.test.ts`
Expected: PASS。

- [ ] **Step 4: 全量 gate(真 exit code)**

```bash
cd frontend && pnpm test            # 全量 vitest(看总 passed / 0 failed)
cd frontend && pnpm exec tsc --noEmit
cd frontend && pnpm lint            # ESLint(含 i18next/no-literal-string)
```
Expected:三条全绿。若 `conversation-panel.test.tsx` 旧断言(`conversation-speak-input` 等)因改造失效,更新为新结构;若有引 wailsjs runtime 的组件测试因缺 mock 报错,按 per-file `vi.mock` 规则补。

- [ ] **Step 5: 提交(如有改动)**

```bash
git add frontend/src/i18n/locales/zh-CN/common.json frontend/src/i18n/locales/en/common.json
git commit frontend/src/i18n/locales/zh-CN/common.json frontend/src/i18n/locales/en/common.json \
  -m "🌐 orchestration: 编排对话流式化新增文案(zh-CN/en)"
```

---

## 收尾:手动冒烟(可选,建议)

`make dev` 起应用,开一个编排 Run:
1. 点某 agent 节点进右栏 ConversationPanel → agent 干活时应**逐字流式**(不再 reload 才整条出现)。
2. ConversationPanel 输入框是 ChatComposer(claudecode 后端可见权限模式 pill、可贴图),发送后本会话续轮流式。
3. 底部 Leader footer 是 ChatComposer,发送后右栏自动切到 Leader 会话并看到流式。
4. 切走再切回 Run,进行中的轮应能**中途重挂**继续流(靠 `useChatSession` 的 activeStream)。

## Self-Review 记录

- **Spec 覆盖**:①流式展示 → Task 2+3(live overlay 透传);②完整输入框 → Task 3+4(ChatComposer + 权限模式 + 贴图);③两处都改 → Task 3(Panel)+ Task 4(footer);④Leader 复用右栏 → Task 4(submit 后 `setSelectedSessionId`);⑤自发轮捕获(agent 干活流式)→ Task 2(`chat:autonomous` watch);⑥中途重挂 → 复用 `useChatSession`;⑦finish→reload → Task 2(doneTick);⑧edit/rerun 不做 → 未传 `onEdit/onRerun`,符合 spec。
- **偏离 spec 并已在计划顶部说明**:持久消息源由「扩 orch-subagents-store」改为「复用 `useChatSession`」(重挂逻辑白拿,更 DRY;orch-subagents-store 不动仍供 TaskBoard)。
- **Placeholder 扫描**:无 TBD;`contextUsage.used=0` 是有意的 v1 简化(已注明),非占位。
- **类型一致**:`useComposerSend` 的 `submit/sending/permissionMode/permissionModeMeta/isModeSwitchable/supportsImageInput/backendType` 在 Task 3/4 消费处名字一致;`openStream({name,sessionId,assistantMessageId,streamStartedAt})` 全程一致;`live.*` 字段与 `ChatTranscript` 的 `liveDelta/liveThinking/liveBlocks/liveRetry/liveStreamStartedAt/streaming/liveCompacting` 对齐。
- **待执行期确认的软点**(非阻塞):`optimisticUser/optimisticAssistantPlaceholder` 与 `usePermissionMode/PermissionModePill/useBackendCapabilities` 的**真实 import 路径**以 chat-panel.tsx 顶部为准;`caps.permissionModeMeta` 的 TS 类型可能需按 chat-panel 同样方式访问。
- **远端 daemon**:`chat:autonomous` 是否跨 WS 桥转发到远端编排会话 —— 需在真机验证(spec 已标注),桌面本地不受影响。
