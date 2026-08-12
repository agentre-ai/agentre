// frontend/src/components/agentre/peer/peer-transcript.ts
//
// Peer Tab 的转录归约（R19 / R8）：把对端桌面端经 peer_svc 推来的 canonical 事件
// 帧（wire EventFrame 同形，带 fingerprint / sessionId / seq）归约成本地可渲染的
// ChatMessage[] + 挂起的待决策列表。桌面端自己的聊天页用 chat_svc.ChatMessage 渲染，
// 这里复用同一批块形状（text / thinking / tool_use / tool_result），因此 Peer Tab
// 直接喂给 ChatTranscript。ask_user_question / tool_permission_request 不进转录卡，
// 归约成 PeerDecision 由 Peer Panel 自绘可操作控件（它们要走到 peer 绑定，而不是
// 本地会话的 AnswerUserQuestion / AnswerToolApproval）。
//
// 事件 → 消息的边界规则：
//   - user_message → 新开一条 user 消息（带来源标识，R21）。
//   - text_delta / thinking_delta / tool_use_start / tool_result → 累计进当前
//     assistant 消息（无则新建）。
//   - done / error → 关闭当前 assistant 消息（下一条 assistant 事件另起一条）。
//   - 其余未知 kind → 落成 raw 文本块（R8：不识别也不丢弃）。

export const PEER_EVENT_KIND = {
  TEXT_DELTA: "text_delta",
  THINKING_DELTA: "thinking_delta",
  USER_MESSAGE: "user_message",
  TOOL_USE_START: "tool_use_start",
  TOOL_USE_END: "tool_use_end",
  TOOL_RESULT: "tool_result",
  ERROR: "error",
  DONE: "done",
  ASK_USER_QUESTION: "ask_user_question",
  ASK_USER_QUESTION_ANSWERED: "ask_user_question_answered",
  TOOL_PERMISSION_REQUEST: "tool_permission_request",
  TOOL_PERMISSION_RESOLVED: "tool_permission_resolved",
} as const;

export type PeerEventFrame = {
  fingerprint: string;
  sessionId: number;
  seq?: number;
  event: { kind: string } & Record<string, unknown>;
};

export type PeerTextBlock = { type: "text"; text: string };
export type PeerThinkingBlock = { type: "thinking"; text: string };
export type PeerToolUseBlock = {
  type: "tool_use";
  toolUseId: string;
  toolName: string;
  toolInput?: Record<string, unknown>;
};
export type PeerToolResultBlock = {
  type: "tool_result";
  toolUseId: string;
  text: string;
  isError?: boolean;
};
export type PeerRawBlock = { type: "raw"; text: string };
export type PeerBlock =
  | PeerTextBlock
  | PeerThinkingBlock
  | PeerToolUseBlock
  | PeerToolResultBlock
  | PeerRawBlock;

export type PeerChatMessage = {
  id: number;
  role: "user" | "assistant";
  blocks: PeerBlock[];
  seq: number;
  createtime: number;
  errorText?: string;
  sourceDevice?: string;
  sourceDeviceName?: string;
};

export type PeerAskQuestion = {
  id?: string;
  question: string;
  header: string;
  multiSelect?: boolean;
  isOther?: boolean;
  isSecret?: boolean;
  options: { label: string; description: string; preview?: string }[];
};

export type PeerDecision =
  | {
      kind: "ask";
      requestId: string;
      questions: PeerAskQuestion[];
      answered?: boolean;
      skipped?: boolean;
    }
  | {
      kind: "permission";
      requestId: string;
      toolName: string;
      toolCallId: string;
      input?: Record<string, unknown>;
      resolved?: boolean;
      allowed?: boolean;
    };

export type PeerTranscriptState = {
  messages: PeerChatMessage[];
  decisions: PeerDecision[];
  cursor: number;
  nextId: number;
  lifecycle: string;
  waitingForInput: boolean;
};

export const createPeerTranscript = (): PeerTranscriptState => ({
  messages: [],
  decisions: [],
  cursor: 0,
  nextId: 1,
  lifecycle: "idle",
  waitingForInput: false,
});

export function reducePeerEvent(
  state: PeerTranscriptState,
  frame: PeerEventFrame,
): PeerTranscriptState {
  // 按 seq 去重：pull 补齐与实时推送可能重复投递同一帧（attach 到拉平之间落库的
  // 帧会被两路都带出来），已归约过的帧（seq ≤ 游标）直接丢弃。
  if (frame.seq != null && frame.seq > 0 && frame.seq <= state.cursor) {
    return state;
  }
  const kind = frame.event?.kind;
  const seq = frame.seq ?? 0;
  const cursor = Math.max(state.cursor, seq);
  const base = { ...state, cursor };

  switch (kind) {
    case PEER_EVENT_KIND.USER_MESSAGE: {
      const text = String(frame.event.text ?? "");
      const msg: PeerChatMessage = {
        id: base.nextId,
        role: "user",
        blocks: [{ type: "text", text }],
        seq,
        createtime: seq,
        sourceDevice: (frame.event.sourceDevice as string) || undefined,
        sourceDeviceName: (frame.event.sourceDeviceName as string) || undefined,
      };
      return {
        ...base,
        nextId: base.nextId + 1,
        messages: [...base.messages, msg],
        waitingForInput: false,
      };
    }
    case PEER_EVENT_KIND.TEXT_DELTA: {
      const text = String(frame.event.text ?? "");
      return appendToAssistant(base, (m) => appendText(m, text));
    }
    case PEER_EVENT_KIND.THINKING_DELTA: {
      const text = String(frame.event.text ?? "");
      return appendToAssistant(base, (m) => appendThinking(m, text));
    }
    case PEER_EVENT_KIND.TOOL_USE_START: {
      const block: PeerToolUseBlock = {
        type: "tool_use",
        toolUseId: String(frame.event.id ?? ""),
        toolName: String(frame.event.name ?? ""),
        toolInput:
          typeof frame.event.input === "object" && frame.event.input !== null
            ? (frame.event.input as Record<string, unknown>)
            : undefined,
      };
      return appendToAssistant(base, (m) => ({
        ...m,
        blocks: [...m.blocks, block],
      }));
    }
    case PEER_EVENT_KIND.TOOL_USE_END: {
      return base;
    }
    case PEER_EVENT_KIND.TOOL_RESULT: {
      const toolCallId = String(frame.event.toolCallId ?? "");
      const text = String(frame.event.content ?? "");
      const isError = Boolean(frame.event.isError);
      return {
        ...base,
        messages: appendToolResult(base.messages, toolCallId, text, isError),
      };
    }
    case PEER_EVENT_KIND.ERROR: {
      const errText = String(frame.event.message ?? "");
      return {
        ...base,
        messages: closeLastAssistantWithError(base.messages, errText),
        waitingForInput: false,
      };
    }
    case PEER_EVENT_KIND.DONE: {
      return { ...base, waitingForInput: false };
    }
    case PEER_EVENT_KIND.ASK_USER_QUESTION: {
      const requestId = String(frame.event.requestId ?? "");
      const questions = (frame.event.questions ?? []) as PeerAskQuestion[];
      return {
        ...base,
        decisions: upsertAskDecision(base.decisions, requestId, questions),
        waitingForInput: true,
      };
    }
    case PEER_EVENT_KIND.ASK_USER_QUESTION_ANSWERED: {
      const requestId = String(frame.event.requestId ?? "");
      return {
        ...base,
        decisions: markAskAnswered(
          base.decisions,
          requestId,
          Boolean(frame.event.skipped),
        ),
        waitingForInput: false,
      };
    }
    case PEER_EVENT_KIND.TOOL_PERMISSION_REQUEST: {
      const requestId = String(frame.event.requestId ?? "");
      const decision: PeerDecision = {
        kind: "permission",
        requestId,
        toolName: String(frame.event.toolName ?? ""),
        toolCallId: String(frame.event.toolCallId ?? ""),
        input:
          typeof frame.event.input === "object" && frame.event.input !== null
            ? (frame.event.input as Record<string, unknown>)
            : undefined,
      };
      return {
        ...base,
        decisions: upsertPermissionDecision(base.decisions, decision),
        waitingForInput: true,
      };
    }
    case PEER_EVENT_KIND.TOOL_PERMISSION_RESOLVED: {
      const requestId = String(frame.event.requestId ?? "");
      return {
        ...base,
        decisions: markPermissionResolved(
          base.decisions,
          requestId,
          Boolean(frame.event.allowed),
        ),
        waitingForInput: false,
      };
    }
    default: {
      // R8：不识别的块落成原始形态，不丢弃。
      return appendRaw(base, frame);
    }
  }
}

// reducePeerPullPage 把一页 journaled 历史喂给同一归约器。每条 notification 的
// Params 是「不含 seq」的帧原样，须把日志行自己的 seq 盖上去（与浏览器同一约定）。
export function reducePeerPullPage(
  state: PeerTranscriptState,
  notifications: Array<{
    seq: number;
    params: { sessionId: number; event: unknown };
  }>,
): PeerTranscriptState {
  let next = state;
  for (const n of notifications ?? []) {
    const raw = n.params as unknown as {
      sessionId?: number;
      event?: { kind: string };
    };
    const frame: PeerEventFrame = {
      fingerprint: "",
      sessionId: raw?.sessionId ?? 0,
      seq: n.seq,
      event: raw?.event ?? { kind: "" },
    };
    next = reducePeerEvent(next, frame);
  }
  return next;
}

// appendToAssistant 把一条消息变换应用到「当前 assistant 消息」上；没有当前
// assistant 消息时新开一条（id 用 state.nextId，块从空开始）。
function appendToAssistant(
  state: PeerTranscriptState,
  mutate: (m: PeerChatMessage) => PeerChatMessage,
): PeerTranscriptState {
  const last = state.messages.at(-1);
  if (last && last.role === "assistant") {
    const messages = [...state.messages];
    messages[messages.length - 1] = mutate(last);
    return { ...state, messages };
  }
  const fresh: PeerChatMessage = {
    id: state.nextId,
    role: "assistant",
    blocks: [],
    seq: state.cursor,
    createtime: state.cursor,
  };
  return {
    ...state,
    nextId: state.nextId + 1,
    messages: [...state.messages, mutate(fresh)],
  };
}

function appendText(m: PeerChatMessage, text: string): PeerChatMessage {
  const blocks = [...m.blocks];
  const last = blocks.at(-1);
  if (last && last.type === "text") {
    blocks[blocks.length - 1] = {
      ...last,
      text: (last as PeerTextBlock).text + text,
    };
  } else {
    blocks.push({ type: "text", text });
  }
  return { ...m, blocks };
}

function appendThinking(m: PeerChatMessage, text: string): PeerChatMessage {
  const blocks = [...m.blocks];
  const last = blocks.at(-1);
  if (last && last.type === "thinking") {
    blocks[blocks.length - 1] = {
      ...last,
      text: (last as PeerThinkingBlock).text + text,
    };
  } else {
    blocks.push({ type: "thinking", text });
  }
  return { ...m, blocks };
}

function appendToolResult(
  messages: PeerChatMessage[],
  toolUseId: string,
  text: string,
  isError: boolean,
): PeerChatMessage[] {
  if (messages.length === 0) return messages;
  const last = messages[messages.length - 1];
  const idx = last.blocks.findIndex(
    (b) => b.type === "tool_use" && b.toolUseId === toolUseId,
  );
  if (idx < 0) return messages;
  const out = [...messages];
  out[out.length - 1] = {
    ...last,
    blocks: [...last.blocks, { type: "tool_result", toolUseId, text, isError }],
  };
  return out;
}

function closeLastAssistantWithError(
  messages: PeerChatMessage[],
  errText: string,
): PeerChatMessage[] {
  if (messages.length === 0) return messages;
  const last = messages[messages.length - 1];
  if (last.role !== "assistant") return messages;
  const out = [...messages];
  out[out.length - 1] = {
    ...last,
    errorText: errText,
    blocks: [...last.blocks, { type: "raw", text: errText }],
  };
  return out;
}

function appendRaw(
  state: PeerTranscriptState,
  frame: PeerEventFrame,
): PeerTranscriptState {
  const text = JSON.stringify(frame.event ?? {});
  return appendToAssistant(state, (m) => ({
    ...m,
    blocks: [...m.blocks, { type: "raw", text }],
  }));
}

function upsertAskDecision(
  decisions: PeerDecision[],
  requestId: string,
  questions: PeerAskQuestion[],
): PeerDecision[] {
  const existing = decisions.findIndex(
    (d) => d.kind === "ask" && d.requestId === requestId,
  );
  if (existing >= 0) return decisions;
  return [...decisions, { kind: "ask", requestId, questions }];
}

function markAskAnswered(
  decisions: PeerDecision[],
  requestId: string,
  skipped: boolean,
): PeerDecision[] {
  return decisions.map((d) =>
    d.kind === "ask" && d.requestId === requestId
      ? { ...d, answered: true, skipped }
      : d,
  );
}

function upsertPermissionDecision(
  decisions: PeerDecision[],
  decision: PeerDecision,
): PeerDecision[] {
  const existing = decisions.findIndex(
    (d) => d.kind === "permission" && d.requestId === decision.requestId,
  );
  if (existing >= 0) return decisions;
  return [...decisions, decision];
}

function markPermissionResolved(
  decisions: PeerDecision[],
  requestId: string,
  allowed: boolean,
): PeerDecision[] {
  return decisions.map((d) =>
    d.kind === "permission" && d.requestId === requestId
      ? { ...d, resolved: true, allowed }
      : d,
  );
}
