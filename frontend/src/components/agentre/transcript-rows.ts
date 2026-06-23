// transcript-rows: chat transcript 的「block → 渲染项 → 虚拟行」纯函数层,零 React 依赖。
// renderMessageBlocks 的配对状态机抽取到这里,让行级虚拟化(每个 RenderItem 一个
// 虚拟行)能在不碰 JSX 的前提下单测配对 / 合并 / skip / FIFO / 归集 / key 稳定性。
import type { ChatBlockData } from "@/stores/chat-streams-store";
import type { chat_svc } from "../../../wailsjs/go/models";

// isSubagentCanonical 替代旧 isSubagentTool(name) — name-based 检测改为读
// canonical.kind。translator 在 emit 时已经把 Task/Agent/collabAgent 工具识别成
// canonical.agentSpawn,这里直接 dispatch。
export function isSubagentCanonical(block: {
  canonical?: { kind?: string };
}): boolean {
  return block.canonical?.kind === "agent.spawn";
}

// isAskUserQuestionToolName 旧 tool-summary.ts 同名;此处过滤掉 AskUserQuestion 类工具的
// tool_use 块,避免与 ask_user_question 独立 block 渲染的 UserAskCard 重复出卡。
export function isAskUserQuestionToolName(
  toolName: string | undefined,
): boolean {
  if (!toolName) return false;
  const name = toolName.toLowerCase();
  return name === "askuserquestion" || name === "ask_user_question";
}

export function isRenderablePlanBlock(block: ChatBlockData): boolean {
  const canonical = block.canonical;
  if (canonical?.kind !== "plan.update" || !canonical.planUpdate) return false;
  const actions = canonical.planUpdate.actions ?? [];
  if (actions.length > 0) return true;
  const text = canonical.planUpdate.text ?? block.text ?? "";
  const steps = canonical.planUpdate.steps ?? [];
  return text.trim().length > 0 && steps.length === 0;
}

export type RenderItem =
  // streaming=true 标记这是「流式途中正在生长」的文本项 —— 用 StreamingMarkdown
  // 增量渲染(已定稿 block memo 跳过、只重解析活跃尾巴);持久化文本仍走整段 MarkdownText。
  | { text: string; type: "text"; streaming?: boolean }
  | { block: ChatBlockData; type: "plan" }
  | {
      block: ChatBlockData;
      startedAt?: number;
      streaming: boolean;
      type: "thinking";
    }
  | {
      // permissionBlock 仅在审批通过后由配对逻辑挂上,渲染时透传给 ToolInvocationCard。
      permissionBlock?: ChatBlockData;
      resultBlock?: ChatBlockData;
      toolBlock?: ChatBlockData;
      // childBlocks 仅 canonical.agent.spawn 需要(parent-child 归集),其它工具留空。
      childBlocks?: ChatBlockData[];
      type: "tool";
    }
  | {
      block: ChatBlockData;
      type: "image";
    }
  | {
      block: ChatBlockData;
      // _consumed 标记此条审批已被 merge 到某条 tool_use 卡上,buildRenderItems 返回前
      // 会被过滤掉。未 resolved / resolved-denied 的审批不会被标记,保留为独立卡。
      _consumed?: boolean;
      type: "tool_permission_request";
    }
  | { block: ChatBlockData; type: "tool_approval" }
  | { block: ChatBlockData; type: "unknown" }
  | { block: ChatBlockData; type: "compact_boundary" };

// VisibleRenderItem = 过滤掉已 merge 审批后的渲染项 + 预计算的 uiStateKey。
// uiStateKey 进 TranscriptUIStateContext 持久化卡片折叠态,格式与旧
// renderMessageBlocks 字节级一致:message:${messageId}:${type}:${identity ?? visibleIdx}。
export type VisibleRenderItem = RenderItem & { uiStateKey: string };

export type BuildRenderItemsArgs = {
  messageId: number;
  blocks?: ChatBlockData[];
  /** 本轮仍在生长的尾巴文本,合并进末尾 text 项并标记 streaming。 */
  liveTail?: string;
  /** 流式中累积的 thinking 增量,合成一张排在 liveBlocks 之前的 thinking 卡。 */
  liveThinking?: string;
  liveThinkingStartedAt?: number | null;
  // 本轮 turn 已"冻结但还没持久化"的块(text / tool_use / tool_result),由
  // chat-streams-store 维护。和 persisted blocks 拼成一个完整顺序 —— 关键:
  // 流式途中遇到 tool_use 时,store 会把当下的 liveDelta 先冻成 text block 推
  // 到 liveBlocks 尾,所以真实顺序就是 [persisted..., ...liveBlocks, liveDelta]。
  liveBlocks?: ChatBlockData[];
};

export function buildRenderItems({
  messageId,
  blocks = [],
  liveTail = "",
  liveThinking = "",
  liveThinkingStartedAt,
  liveBlocks = [],
}: BuildRenderItemsArgs): VisibleRenderItem[] {
  // 预扫一遍把 subagent 内部 block 归集到外层 Agent.tool_use_id;
  // 主流程遇到 parentToolUseId 非空就 skip,避免被同级渲染。
  const childrenByParent = new Map<string, ChatBlockData[]>();
  const collectChildren = (b: ChatBlockData) => {
    if (!b.parentToolUseId) return;
    const arr = childrenByParent.get(b.parentToolUseId) ?? [];
    arr.push(b);
    childrenByParent.set(b.parentToolUseId, arr);
  };
  blocks.forEach(collectChildren);
  liveBlocks.forEach(collectChildren);

  const items: RenderItem[] = [];
  const pendingToolIndexes = new Map<string, number>();
  const pendingAnonymousToolIndexes: number[] = [];
  // SKIPPED_TOOL_INDEX 给 AskUserQuestion 的 tool_use 占位用:tool_use 本身不入 items,
  // 但要让后续的 tool_result 在 pendingToolIndexes 里查到这个哨兵,从而一同 skip。
  const SKIPPED_TOOL_INDEX = -1;
  // pendingPermsByTool 按 toolName 维护"已审批通过、还在等匹配 tool_use"的 perm RenderItem
  // 下标 (FIFO)。匹配到 tool_use 时把 perm 标记 _consumed,merge 到那条 tool item。
  // 这是协议上唯一可行的关联方式 —— ChatBlockToolPermission 没有 toolUseId 字段,
  // can_use_tool control_request 也不携带未来的 tool_use_id。
  const pendingPermsByTool = new Map<string, number[]>();

  function appendText(text: string, streaming = false) {
    if (!text) return;
    const last = items.at(-1);
    if (last?.type === "text") {
      last.text += text;
      // 与前一个已冻结的 text 段合并后,整段都按流式尾巴处理 ——
      // StreamingMarkdown 会把已冻结的前缀也切成 memo 命中的定稿块,只重解析真尾巴。
      if (streaming) last.streaming = true;
      return;
    }
    items.push({ text, type: "text", streaming });
  }

  const consumeBlock = (b: ChatBlockData) => {
    // subagent 内部 block 已经被归集到父 AgentSpawnCard 的 childBlocks,不再同级渲染。
    if (b.parentToolUseId) return;
    switch (b.type) {
      case "text":
        appendText(b.text ?? "");
        break;
      case "thinking":
        items.push({ block: b, streaming: false, type: "thinking" });
        break;
      case "image":
        items.push({ block: b, type: "image" });
        break;
      case "plan":
        // Most plan.update blocks are progress data for TaskProgressBar only.
        // Actionable plan blocks carry canonical.actions and need the shared
        // PlanCard in the transcript.
        if (isRenderablePlanBlock(b)) {
          items.push({ block: b, type: "plan" });
        }
        break;
      case "tool_use": {
        // AskUserQuestion 类工具的 tool_use 不渲染独立卡 —— ask_user_question block
        // 已经把交互界面接管掉。占位 SKIPPED_TOOL_INDEX 让后续 tool_result 也 skip。
        if (isAskUserQuestionToolName(b.toolName)) {
          if (b.toolUseId)
            pendingToolIndexes.set(b.toolUseId, SKIPPED_TOOL_INDEX);
          break;
        }
        // ExitPlanMode 同理 —— PlanApproveCard(plan.approve_request canonical)已经
        // 承担"批准执行计划"的完整渲染,后续 CLI 真正调用 ExitPlanMode 冒出的 tool_use
        // 是协议余响,再渲染一张卡只会和 PlanApproveCard 视觉重复。break 前不入
        // pendingPermsByTool 队列也意味着 PlanApproveCard 不会被 merge 隐藏。
        if (b.toolName === "ExitPlanMode") {
          if (b.toolUseId)
            pendingToolIndexes.set(b.toolUseId, SKIPPED_TOOL_INDEX);
          break;
        }
        if (isSubagentCanonical(b)) {
          // canonical.agent.spawn — 走 CanonicalToolRouter → AgentSpawnCard,childBlocks
          // 由 parent-child 归集传过去(AgentSpawnCard 内部渲染 STEPS 段)。
          const item: RenderItem = {
            childBlocks: b.toolUseId
              ? (childrenByParent.get(b.toolUseId) ?? [])
              : [],
            toolBlock: b,
            type: "tool",
          };
          items.push(item);
          if (b.toolUseId) {
            pendingToolIndexes.set(b.toolUseId, items.length - 1);
          }
          break;
        }
        const item: RenderItem = { toolBlock: b, type: "tool" };
        // 配对消费一条审批 RenderItem —— 找最早未消费且同 toolName 的 allowed 审批。
        if (b.toolName) {
          const queue = pendingPermsByTool.get(b.toolName);
          if (queue && queue.length > 0) {
            const permIdx = queue.shift()!;
            const permItem = items[permIdx];
            if (permItem?.type === "tool_permission_request") {
              permItem._consumed = true;
              item.permissionBlock = permItem.block;
            }
          }
        }
        items.push(item);
        if (b.toolUseId) {
          pendingToolIndexes.set(b.toolUseId, items.length - 1);
        } else {
          pendingAnonymousToolIndexes.push(items.length - 1);
        }
        break;
      }
      case "tool_result": {
        const toolIndex = b.toolUseId
          ? pendingToolIndexes.get(b.toolUseId)
          : pendingAnonymousToolIndexes.pop();
        // AskUserQuestion 的 tool_result 命中 SKIPPED_TOOL_INDEX 哨兵,直接丢弃。
        if (toolIndex === SKIPPED_TOOL_INDEX) {
          if (b.toolUseId) pendingToolIndexes.delete(b.toolUseId);
          break;
        }
        const item =
          typeof toolIndex === "number" ? items[toolIndex] : undefined;

        if (item?.type === "tool") {
          item.resultBlock = b;
          if (b.toolUseId) pendingToolIndexes.delete(b.toolUseId);
        }
        // 孤儿 tool_result:没有配对 tool_use(AskUserQuestion 历史数据 / 后端漏过滤
        // 的 PostToolUse 等都会走到这里),直接丢,不要 push 一条没有 toolBlock 的
        // 幽灵 tool 卡(toolName 会回退到默认 "tool" 把答案文本暴露出来)。
        break;
      }
      case "ask_user_question":
        // ask_user_question 走 CanonicalToolRouter — block.canonical (UserAsk)
        // 已由后端 live + replay 双路径填好,UserAskCard 直接消费。
        items.push({ toolBlock: b, type: "tool" });
        break;
      case "tool_permission_request": {
        // tool_permission_request 渲染走 CanonicalToolRouter —— ExitPlanMode
        // → canonical.plan.approve_request → PlanApproveCard;其它工具
        // → canonical.tool.permission → ToolPermissionCard。两条 canonical 都由后端
        // dispatcher_emitter + replay 填好。RenderItem.type 保留 "tool_permission_request"
        // 让 merge 到下方同 toolName tool_use 卡的逻辑可识别。
        items.push({ block: b, type: "tool_permission_request" });
        const idx = items.length - 1;
        const perm = b.toolPermission;
        // 只有 resolved + allowed 才参与 merge:未决态用户还要操作、denied 没有下游 tool_use。
        if (perm?.resolved && perm.allowed && perm.toolName) {
          const queue = pendingPermsByTool.get(perm.toolName) ?? [];
          queue.push(idx);
          pendingPermsByTool.set(perm.toolName, queue);
        }
        break;
      }
      case "tool_approval":
        // 内置写工具审批卡:不走 CanonicalToolRouter,直接按 block.type 路由到
        // ToolApprovalCard(transcript-row-view)。持久化/overlay 与 live 两路都到这里 ——
        // block.toolApproval.status 自身就是 truth(后端 finalize 已把悬空 pending 落成
        // expired),前端不按会话活跃度推断。
        items.push({ block: b, type: "tool_approval" });
        break;
      case "compact_boundary":
        // CLI 通报上下文已压缩 (manual /compact 或 auto)。在 transcript 中嵌一条
        // 分隔卡片;最后一条 compact_boundary 之前的所有内容会被 ChatTranscript 顶层
        // 折叠成"查看历史"按钮。
        items.push({ block: b, type: "compact_boundary" });
        break;
      default:
        items.push({ block: b, type: "unknown" });
        break;
    }
  };
  blocks.forEach(consumeBlock);

  // 合成 thinking 必须排在本轮 liveBlocks(tool_use/tool_result/已冻结 text)之前 —
  // Anthropic 协议里 thinking 永远在 turn 开头,store 也是单一 liveThinking 字段不穿插。
  // 摆错位置会出现「思考 14s 还在转,但工具卡已经在它上方」的视觉错乱。
  // streaming 判定:本轮一旦冒出任何非思考的输出(tool_use 进 liveBlocks 或文本开始流到
  // liveTail),思考阶段就结束;只看 liveTail 会漏掉「思考完→直接发 tool」那一帧,徽标
  // 一直 pulse、计时定格。
  if (liveThinking) {
    items.push({
      block: { text: liveThinking, type: "thinking" } as ChatBlockData,
      startedAt: liveThinkingStartedAt ?? undefined,
      streaming: !liveTail && liveBlocks.length === 0,
      type: "thinking",
    });
  }
  liveBlocks.forEach(consumeBlock);
  // liveTail 是本轮仍在生长的尾巴文本 —— 标记 streaming,走 StreamingMarkdown 增量渲染。
  appendText(liveTail, true);

  // 被 merge 到下方 tool_use 卡的审批 RenderItem 不再独立渲染。
  return items
    .filter(
      (item) => !(item.type === "tool_permission_request" && item._consumed),
    )
    .map((item, idx) =>
      Object.assign(item, {
        uiStateKey: itemUIStateKey(messageId, item, idx),
      }),
    );
}

// itemUIStateKey 的 type 段沿用旧 renderMessageBlocks 渲染层的命名
// (tool_permission_request 项历史上写作 "permission"),identity 优先块身份、
// 回退 visible 下标 —— 与旧实现字节级一致,卡片折叠态零迁移。
function itemUIStateKey(
  messageId: number,
  item: RenderItem,
  visibleIdx: number,
): string {
  const type =
    item.type === "tool_permission_request" ? "permission" : item.type;
  const block =
    item.type === "tool"
      ? item.toolBlock
      : item.type === "text"
        ? undefined
        : item.block;
  const identity = stableBlockIdentity(block) ?? visibleIdx;
  return `message:${messageId}:${type}:${identity}`;
}

// ─── 行模型 ──────────────────────────────────────────────────────────────────
// 一行 = 一个 RenderItem + 消息分片标志。chrome(头像/名字/时间戳/AutoTriggerBanner
// /meta footer/indicator/error)由渲染层按 isFirstOfMessage/isLastOfMessage 挂在
// 行内,不单独成行 —— 纯文本消息恰好一行,DOM 形态与 message 级虚拟化几乎一致。

// placeholder:blocks 为空(占位 assistant)或全部被 skip 的消息仍要渲染消息 chrome
// (头像行 + typing indicator 落点),发射一个无内容行。
export type TranscriptRowItem =
  | VisibleRenderItem
  | { type: "placeholder"; uiStateKey?: undefined };

export type TranscriptRow = {
  /** 虚拟器 getItemKey + 测量缓存键。复用 uiStateKey(含 messageId,item 级唯一)。 */
  key: string;
  messageId: number;
  /** 行渲染需要的消息引用(角色/时间戳/meta tokens/errorText)。 */
  message: chat_svc.ChatMessage;
  item: TranscriptRowItem;
  /** 首行渲染头像 + 名字 + 时间戳(以及 autonomous banner)。 */
  isFirstOfMessage: boolean;
  /** 末行渲染 footer(meta/copy/edit)+ RetryNotice + TypingIndicator + ErrorCard。 */
  isLastOfMessage: boolean;
  autonomous: boolean;
};

export type TranscriptRowsResult = {
  rows: TranscriptRow[];
  /** messageId → 该消息首行下标;scrollToMessage / anchor 回退用。 */
  firstRowIndexByMessageId: Map<number, number>;
  /** 行 key → 下标;行级 anchor 精确恢复用。 */
  rowIndexByKey: Map<string, number>;
};

export type BuildTranscriptRowsArgs = {
  displayMessages: chat_svc.ChatMessage[];
  autonomousIds: ReadonlySet<number>;
  liveTargetId?: number | null;
  liveTail?: string;
  liveThinking?: string;
  liveThinkingStartedAt?: number | null;
  liveBlocks?: ChatBlockData[];
  /**
   * 实例级行缓存(WeakMap,键是消息对象)。persisted 消息的 blocks 引用稳定 →
   * 缓存命中返回同一 row 对象数组 → 行组件 React.memo 恒命中;reload 换对象引用
   * 自然失效。live 消息(liveTargetId)每 chunk 内容都在变,绕过缓存现场重建。
   */
  cache?: WeakMap<chat_svc.ChatMessage, TranscriptRow[]>;
};

function buildMessageRows(
  m: chat_svc.ChatMessage,
  autonomous: boolean,
  live?: {
    liveTail?: string;
    liveThinking?: string;
    liveThinkingStartedAt?: number | null;
    liveBlocks?: ChatBlockData[];
  },
): TranscriptRow[] {
  const items = buildRenderItems({
    messageId: m.id,
    blocks: m.blocks ?? undefined,
    liveTail: live?.liveTail,
    liveThinking: live?.liveThinking,
    liveThinkingStartedAt: live?.liveThinkingStartedAt,
    liveBlocks: live?.liveBlocks,
  });
  if (items.length === 0) {
    return [
      {
        autonomous,
        isFirstOfMessage: true,
        isLastOfMessage: true,
        item: { type: "placeholder" },
        key: `message:${m.id}:placeholder`,
        message: m,
        messageId: m.id,
      },
    ];
  }
  return items.map((item, idx) => ({
    autonomous,
    isFirstOfMessage: idx === 0,
    isLastOfMessage: idx === items.length - 1,
    item,
    // uiStateKey 含 messageId 且在消息内 item 级唯一(identity 或 visible 下标),
    // 直接复用为行 key —— 它在「流式形态 → 落库形态」之间逐项相等(同一套
    // buildRenderItems 输入序列),turn 落定不会整列 remount。
    key: item.uiStateKey,
    message: m,
    messageId: m.id,
  }));
}

export function buildTranscriptRows({
  displayMessages,
  autonomousIds,
  liveTargetId,
  liveTail,
  liveThinking,
  liveThinkingStartedAt,
  liveBlocks,
  cache,
}: BuildTranscriptRowsArgs): TranscriptRowsResult {
  const rows: TranscriptRow[] = [];
  const firstRowIndexByMessageId = new Map<number, number>();
  const rowIndexByKey = new Map<string, number>();

  for (const m of displayMessages) {
    const autonomous = autonomousIds.has(m.id);
    const isLive = liveTargetId != null && m.id === liveTargetId;
    let messageRows: TranscriptRow[];
    if (isLive) {
      // live 消息每 chunk 重建,不读不写缓存。
      messageRows = buildMessageRows(m, autonomous, {
        liveBlocks,
        liveTail,
        liveThinking,
        liveThinkingStartedAt,
      });
    } else {
      const cached = cache?.get(m);
      // autonomous 取决于前一条消息,与消息对象自身无关 —— 缓存命中但标志变了
      // (极端:上游裁剪了前面的消息而对象引用未变)就重建,避免 banner 错挂。
      if (cached && cached[0]?.autonomous === autonomous) {
        messageRows = cached;
      } else {
        messageRows = buildMessageRows(m, autonomous);
        cache?.set(m, messageRows);
      }
    }
    firstRowIndexByMessageId.set(m.id, rows.length);
    for (const row of messageRows) {
      rowIndexByKey.set(row.key, rows.length);
      rows.push(row);
    }
  }

  return { firstRowIndexByMessageId, rowIndexByKey, rows };
}

// estimateRowSize:按 item 类型估行高,供虚拟器 estimateSize 用。text/placeholder
// 维持 132(与 message 级虚拟化的整消息估高一致 —— 纯文本消息恰好一行,含 chrome);
// 真实高度由 measureElement 动态测量覆盖,估值只影响冷跳收敛速度。
export function estimateRowSize(row: TranscriptRow): number {
  switch (row.item.type) {
    case "text":
    case "placeholder":
      return 132;
    case "image":
      return 160;
    case "thinking":
      return 40;
    case "compact_boundary":
      return 48;
    default:
      // tool / plan / tool_permission_request / unknown:折叠态卡片。
      return 48;
  }
}

export function stableBlockIdentity(block?: ChatBlockData): string | undefined {
  if (!block) return undefined;
  if (block.toolUseId) return `tool:${block.toolUseId}`;
  if (block.toolPermission?.requestId) {
    return `permission:${block.toolPermission.requestId}`;
  }
  if (block.askUserQuestion?.requestId) {
    return `ask:${block.askUserQuestion.requestId}`;
  }
  if (block.toolApproval?.requestId) {
    return `tool-approval:${block.toolApproval.requestId}`;
  }
  const canonical = (block as { canonical?: unknown }).canonical;
  if (!canonical || typeof canonical !== "object") return undefined;
  const c = canonical as {
    planApprove?: { requestId?: string };
    toolPermission?: { requestId?: string };
    userAsk?: { requestId?: string };
  };
  if (c.planApprove?.requestId) return `plan:${c.planApprove.requestId}`;
  if (c.toolPermission?.requestId) {
    return `permission:${c.toolPermission.requestId}`;
  }
  if (c.userAsk?.requestId) return `ask:${c.userAsk.requestId}`;
  return undefined;
}
