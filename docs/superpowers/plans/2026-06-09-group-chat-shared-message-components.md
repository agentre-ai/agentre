# 群聊 / 单聊共享消息组件 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 抽出 `MessageRow` 气泡外壳、`MessageCopyButton` 复制动作、`useStickToBottom` 跟随滚动三个共享接缝,让群聊穿过它们渲染,从而获得复制按钮、规范头像、自动跟随滚动,且这几类问题今后改一处即生效。

**Architecture:** 新增 3 个内聚单元(2 个新文件 + 1 个新 hook),单聊 `ChatMessage` 重构为组合 `MessageRow`(渲染像素级不变,现有测试当回归闸门),群聊 `group-transcript` / `index` 接线复用。不碰单聊 `chat-panel.tsx` 的滚动逻辑,不引入群聊虚拟化,不动后端。

**Tech Stack:** React 19 + TypeScript,Vitest + @testing-library/react,Tailwind v4(`cn` = `twMerge`),`react-i18next`。所有命令在 `agentre/frontend/` 下运行。

参照 spec:`docs/superpowers/specs/2026-06-09-group-chat-shared-message-components-design.md`

---

## File Structure

| 文件 | 角色 |
| --- | --- |
| `frontend/src/hooks/use-stick-to-bottom.ts`(新) | 通用跟随滚动 hook + 纯函数 `isAtBottom` |
| `frontend/src/hooks/__tests__/use-stick-to-bottom.test.ts`(新) | hook 单测 |
| `frontend/src/components/agentre/message-row.tsx`(新) | `MessageRow` 气泡外壳 + `MessageCopyButton` |
| `frontend/src/components/agentre/message-row.test.tsx`(新) | 外壳 + 复制按钮单测 |
| `frontend/src/components/agentre/chat.tsx`(改) | `ChatMessage` 组合 `MessageRow`;`AssistantMessageActions` 复用 `MessageCopyButton`;删未用 `Copy` import |
| `frontend/src/components/agentre/group-chat/group-transcript.tsx`(改) | agent/user 行改用 `MessageRow` + `MessageCopyButton` |
| `frontend/src/components/agentre/group-chat/group-transcript.test.tsx`(新) | 群聊外壳/复制单测 |
| `frontend/src/components/agentre/group-chat/index.tsx`(改) | 滚动 `<section>` 接 `useStickToBottom` + 回到底部按钮 |
| `frontend/src/components/agentre/group-chat/group-chat.test.tsx`(改) | 加跟随滚动 / 回到底部回归 |

**已确认的既有事实(无需新增):**
- i18n 键已存在:`common.copy`("Copy"/"复制")、`common.copied`("Copied"/"已复制")、`common.copyFailed`、`chat.actions.copyOutput|copyOutputDone|copyOutputFailed`、`chatPanel.scroll.backToBottom`("Back to bottom"/"回到底部")。**本计划不新增 i18n 键。**
- `cn` = `twMerge(clsx(...))`,故 `size="md"` + `className="size-7 rounded-lg text-[11px]"` 去重后等于 `size-7 rounded-lg text-[11px]`,与单聊现状一致。
- `copyTextWithToast(text, { successTitle, errorTitle? })` 在 `@/lib/clipboard-toast`。
- 测试里 i18n 默认英文(现有 group 测试断言 `Running`/`Host`)。

---

## Task 1: `useStickToBottom` hook + 纯函数 `isAtBottom`

**Files:**
- Create: `frontend/src/hooks/use-stick-to-bottom.ts`
- Test: `frontend/src/hooks/__tests__/use-stick-to-bottom.test.ts`

- [ ] **Step 1: 写失败测试**

Create `frontend/src/hooks/__tests__/use-stick-to-bottom.test.ts`:

```ts
import { act, render } from "@testing-library/react";
import * as React from "react";
import { describe, expect, it } from "vitest";

import { isAtBottom, useStickToBottom } from "../use-stick-to-bottom";

describe("isAtBottom", () => {
  it("贴底(在容差内)判为 true", () => {
    expect(
      isAtBottom({ scrollTop: 480, scrollHeight: 1000, clientHeight: 500 }),
    ).toBe(true); // 1000-480-500 = 20 <= 32
  });

  it("远离底部判为 false", () => {
    expect(
      isAtBottom({ scrollTop: 0, scrollHeight: 1000, clientHeight: 500 }),
    ).toBe(false); // 1000-0-500 = 500 > 32
  });
});

// 测试组件：把 hook 的 ref/onScroll 挂到一个 div，并暴露 atBottom / scrollToBottom。
function Harness({ dep }: { dep: number }) {
  const { ref, atBottom, scrollToBottom, onScroll } = useStickToBottom(dep);
  return (
    <div>
      <div
        data-testid="scroller"
        ref={ref as React.Ref<HTMLDivElement>}
        onScroll={onScroll}
      />
      <span data-testid="at-bottom">{String(atBottom)}</span>
      <button type="button" onClick={scrollToBottom}>
        to-bottom
      </button>
    </div>
  );
}

function setDims(
  el: HTMLElement,
  dims: { scrollTop: number; scrollHeight: number; clientHeight: number },
) {
  Object.defineProperty(el, "scrollHeight", {
    configurable: true,
    value: dims.scrollHeight,
  });
  Object.defineProperty(el, "clientHeight", {
    configurable: true,
    value: dims.clientHeight,
  });
  el.scrollTop = dims.scrollTop;
}

describe("useStickToBottom", () => {
  it("用户上滚后 atBottom 变 false；scrollToBottom 把它拉回底部", () => {
    const { getByTestId, getByText } = render(<Harness dep={0} />);
    const scroller = getByTestId("scroller");
    setDims(scroller, { scrollTop: 0, scrollHeight: 1000, clientHeight: 500 });

    act(() => {
      scroller.dispatchEvent(new Event("scroll"));
    });
    expect(getByTestId("at-bottom").textContent).toBe("false");

    act(() => {
      getByText("to-bottom").click();
    });
    expect(getByTestId("at-bottom").textContent).toBe("true");
    expect(scroller.scrollTop).toBe(1000); // 被拉到 scrollHeight
  });

  it("贴底时 dep 变化(新消息追加)自动滚到底", () => {
    const { getByTestId, rerender } = render(<Harness dep={0} />);
    const scroller = getByTestId("scroller");
    setDims(scroller, { scrollTop: 500, scrollHeight: 1000, clientHeight: 500 });

    // 内容增高后 dep 翻新
    setDims(scroller, { scrollTop: 500, scrollHeight: 2000, clientHeight: 500 });
    act(() => {
      rerender(<Harness dep={1} />);
    });
    expect(scroller.scrollTop).toBe(2000);
  });
});
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd frontend && pnpm test -- src/hooks/__tests__/use-stick-to-bottom.test.ts`
Expected: FAIL —— `isAtBottom`/`useStickToBottom` 未定义(模块不存在)。

- [ ] **Step 3: 写最小实现**

Create `frontend/src/hooks/use-stick-to-bottom.ts`:

```ts
import * as React from "react";

// 贴底容差：与单聊 chat-panel 的 32px 保持一致。
export const STICK_TO_BOTTOM_TOLERANCE_PX = 32;

// isAtBottom：纯函数，判断滚动容器是否在底部容差内。抽出来便于单测。
export function isAtBottom(
  metrics: { scrollTop: number; scrollHeight: number; clientHeight: number },
  tolerance: number = STICK_TO_BOTTOM_TOLERANCE_PX,
): boolean {
  return (
    metrics.scrollHeight - metrics.scrollTop - metrics.clientHeight <= tolerance
  );
}

// useStickToBottom：容器 ref 驱动的「自动跟随滚动」。与虚拟化无关。
// - onScroll：挂到滚动容器，实时更新 atBottom；
// - dep 变化(消息追加)时若先前贴底则滚到底；用户上滚后不抢滚；
// - scrollToBottom：供「回到底部」按钮主动拉回。
export function useStickToBottom(dep: unknown) {
  const ref = React.useRef<HTMLElement | null>(null);
  const atBottomRef = React.useRef(true);
  const [atBottom, setAtBottom] = React.useState(true);

  const onScroll = React.useCallback(() => {
    const el = ref.current;
    if (!el) return;
    const next = isAtBottom(el);
    atBottomRef.current = next;
    setAtBottom(next);
  }, []);

  const scrollToBottom = React.useCallback(() => {
    const el = ref.current;
    if (!el) return;
    el.scrollTop = el.scrollHeight;
    atBottomRef.current = true;
    setAtBottom(true);
  }, []);

  React.useLayoutEffect(() => {
    if (!atBottomRef.current) return;
    const el = ref.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [dep]);

  return { ref, atBottom, scrollToBottom, onScroll };
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `cd frontend && pnpm test -- src/hooks/__tests__/use-stick-to-bottom.test.ts`
Expected: PASS(4 个用例全绿)。

- [ ] **Step 5: 提交**

```bash
cd /Users/codfrm/Code/agentre/agentre
git add frontend/src/hooks/use-stick-to-bottom.ts frontend/src/hooks/__tests__/use-stick-to-bottom.test.ts
git commit -m "✨ add useStickToBottom hook for auto-follow scroll"
```

---

## Task 2: `MessageRow` 气泡外壳 + `MessageCopyButton`

**Files:**
- Create: `frontend/src/components/agentre/message-row.tsx`
- Test: `frontend/src/components/agentre/message-row.test.tsx`

- [ ] **Step 1: 写失败测试**

Create `frontend/src/components/agentre/message-row.test.tsx`:

```tsx
import { render, screen, fireEvent } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { MessageRow, MessageCopyButton } from "./message-row";

// 隔离剪贴板副作用：只断言 copyTextWithToast 被以正文调用。
const copySpy = vi.fn();
vi.mock("@/lib/clipboard-toast", () => ({
  copyTextWithToast: (text: string, opts: unknown) => {
    copySpy(text, opts);
    return Promise.resolve(true);
  },
}));

describe("MessageRow", () => {
  it("内置彩色头像用规范尺寸 size-7(锁住一致性)", () => {
    render(
      <MessageRow avatarName="后端" avatarColor="agent-2" name="后端">
        正文
      </MessageRow>,
    );
    const avatar = screen.getByLabelText("后端");
    expect(avatar.className).toContain("size-7");
    expect(screen.getByText("正文")).toBeInTheDocument();
  });

  it("传入 avatar 槽时用自定义头像、不渲染内置头像", () => {
    render(
      <MessageRow
        avatar={<span aria-label="me-pill">我</span>}
        avatarName="后端"
        name={null}
      >
        正文
      </MessageRow>,
    );
    expect(screen.getByLabelText("me-pill")).toBeInTheDocument();
    expect(screen.queryByLabelText("后端")).toBeNull();
  });

  it("footer 槽被渲染", () => {
    render(
      <MessageRow avatarName="后端" name="后端" footer={<span>页脚</span>}>
        正文
      </MessageRow>,
    );
    expect(screen.getByText("页脚")).toBeInTheDocument();
  });
});

describe("MessageCopyButton", () => {
  it("点击以正文文本调用 copyTextWithToast", () => {
    copySpy.mockClear();
    render(<MessageCopyButton text="hello world" />);
    fireEvent.click(screen.getByRole("button"));
    expect(copySpy).toHaveBeenCalledWith("hello world", expect.any(Object));
  });

  it("正文为空时不渲染", () => {
    const { container } = render(<MessageCopyButton text="" />);
    expect(container.querySelector("button")).toBeNull();
  });
});
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd frontend && pnpm test -- src/components/agentre/message-row.test.tsx`
Expected: FAIL —— `./message-row` 模块不存在。

- [ ] **Step 3: 写最小实现**

Create `frontend/src/components/agentre/message-row.tsx`:

```tsx
import * as React from "react";
import { Copy } from "lucide-react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import { copyTextWithToast } from "@/lib/clipboard-toast";
import { cn } from "@/lib/utils";

import { AgentAvatar } from "./primitives";
import type { AgentColor } from "./types";

// MESSAGE_AVATAR_CLASS：单聊/群聊统一的彩色头像尺寸(以单聊为准)。抽成常量，
// 杜绝两处各写 size-6 / size-7 的漂移。size="md" 的 size-8 被 twMerge 去重成 size-7。
export const MESSAGE_AVATAR_CLASS = "size-7 rounded-lg text-[11px]";

type MessageRowProps = Omit<React.ComponentProps<"article">, "children"> & {
  /** 逃生口：自定义头像节点(如单聊 user 的「我」灰胶囊)。传了就不渲染内置头像。 */
  avatar?: React.ReactNode;
  avatarName?: string;
  avatarColor?: AgentColor;
  avatarInitials?: string;
  /** 名字行；传 null 时不显名(单聊 user 行)。 */
  name?: React.ReactNode;
  /** 名字行右侧附加内容：时间 / 群聊「仅 X 收到」灰字。 */
  headerExtra?: React.ReactNode;
  /** 动作行 / token 行的挂载点(复制按钮)。 */
  footer?: React.ReactNode;
  children: React.ReactNode;
};

// MessageRow：单条消息的布局骨架(头像列 + 内容列)。纯展示，不取数据、不决定业务。
// 单聊 ChatMessage 与群聊 transcript 共用，保证头像尺寸/布局一致，并给群聊提供 footer 槽。
function MessageRow({
  avatar,
  avatarName = "",
  avatarColor = "agent-1",
  avatarInitials,
  name,
  headerExtra,
  footer,
  children,
  className,
  ...props
}: MessageRowProps) {
  const showHeader = name != null || headerExtra != null;
  return (
    <article className={cn("flex gap-3 text-sm", className)} {...props}>
      {avatar ?? (
        <AgentAvatar
          name={avatarName}
          initials={avatarInitials}
          color={avatarColor}
          size="md"
          className={MESSAGE_AVATAR_CLASS}
        />
      )}
      <div className="flex min-w-0 max-w-[720px] flex-1 flex-col gap-1">
        {showHeader ? (
          <div className="flex items-center gap-2">
            {name != null ? <span className="font-semibold">{name}</span> : null}
            {headerExtra}
          </div>
        ) : null}
        <div
          data-selectable-text="true"
          className="flex flex-col gap-2 leading-[1.55]"
        >
          {children}
        </div>
        {footer ? (
          <div className="mt-1 flex flex-wrap items-center gap-1.5 font-mono text-[10px] text-subtle-foreground">
            {footer}
          </div>
        ) : null}
      </div>
    </article>
  );
}

type MessageCopyButtonProps = {
  text: string;
  /** 可见文案，默认 common.copy。 */
  label?: string;
  /** aria-label，默认同可见文案。 */
  ariaLabel?: string;
  /** 复制成功 toast 标题，默认 common.copied。 */
  successTitle?: string;
  /** 复制失败 toast 标题，默认走 copyTextWithToast 内置的 common.copyFailed。 */
  errorTitle?: string;
};

// MessageCopyButton：通用「复制消息正文」按钮。text 为空时不渲染。
function MessageCopyButton({
  text,
  label,
  ariaLabel,
  successTitle,
  errorTitle,
}: MessageCopyButtonProps) {
  const { t } = useTranslation();
  if (text.length === 0) return null;
  const visible = label ?? t("common.copy");
  async function handleCopy() {
    await copyTextWithToast(text, {
      successTitle: successTitle ?? t("common.copied"),
      errorTitle,
    });
  }
  return (
    <Button
      type="button"
      variant="ghost"
      size="xs"
      className="h-5 gap-1 px-1.5 text-[10px] text-muted-foreground"
      aria-label={ariaLabel ?? visible}
      onClick={() => void handleCopy()}
    >
      <Copy data-icon="inline-start" aria-hidden="true" />
      {visible}
    </Button>
  );
}

export { MessageRow, MessageCopyButton };
```

- [ ] **Step 4: 跑测试确认通过**

Run: `cd frontend && pnpm test -- src/components/agentre/message-row.test.tsx`
Expected: PASS(5 个用例全绿)。

- [ ] **Step 5: 提交**

```bash
cd /Users/codfrm/Code/agentre/agentre
git add frontend/src/components/agentre/message-row.tsx frontend/src/components/agentre/message-row.test.tsx
git commit -m "✨ add shared MessageRow shell + MessageCopyButton"
```

---

## Task 3: 单聊 `ChatMessage` 组合 `MessageRow`(回归闸门:渲染等价)

**Files:**
- Modify: `frontend/src/components/agentre/chat.tsx`(`ChatMessage` 函数 88-143;`AssistantMessageActions` 复制段 356-368;`Copy` import 第 8 行)

> 这是 Refactor 阶段:不新增行为,现有 `chat.test.tsx` / `chat-panel.test.tsx` / `chat-streams-host.test.tsx` 是回归闸门。

- [ ] **Step 1: 先跑基线,确认现有测试全绿**

Run: `cd frontend && pnpm test -- src/components/agentre/__tests__/chat.test.tsx src/components/agentre/__tests__/chat-panel.test.tsx src/components/agentre/__tests__/chat-streams-host.test.tsx`
Expected: PASS(改动前的基线)。

- [ ] **Step 2: 在 import 区加入 MessageRow / MessageCopyButton**

在 `chat.tsx` 第 41 行 `import { AgentAvatar } from "./primitives";` 之后(import 分组内)加入:

```tsx
import { MessageRow, MessageCopyButton } from "./message-row";
```

- [ ] **Step 3: 重写 `ChatMessage` 为组合 `MessageRow`**

把 `chat.tsx` 第 88-143 行的整个 `ChatMessage` 函数体替换为:

```tsx
function ChatMessage({
  author,
  avatarColor = "agent-1",
  children,
  className,
  initials,
  meta,
  time,
  variant = "assistant",
  ...props
}: ChatMessageProps) {
  const { t } = useTranslation();
  const isUser = variant === "user";
  return (
    <MessageRow
      className={className}
      avatar={
        isUser ? (
          <span
            aria-label={t("chat.message.me")}
            role="img"
            className="inline-flex size-7 shrink-0 items-center justify-center rounded-lg bg-muted text-[11px] font-semibold text-muted-foreground"
          >
            {t("chat.message.me")}
          </span>
        ) : undefined
      }
      avatarName={author}
      avatarInitials={initials}
      avatarColor={avatarColor}
      name={isUser ? null : author}
      headerExtra={
        <span className="font-mono text-[10px] text-muted-foreground">
          {time}
        </span>
      }
      footer={meta}
      {...props}
    >
      {children}
    </MessageRow>
  );
}
```

> 等价性说明:assistant 头像走 MessageRow 内置 `AgentAvatar size="md" className="size-7 rounded-lg text-[11px]"`,twMerge 后与原 `size="md" + "size-7 text-[11px]"` 一致;user 头像走 avatar 槽,与原「我」胶囊逐字相同;header 因 `headerExtra`(时间)恒在而恒渲染;footer 复用原 meta 容器同款 class。

- [ ] **Step 4: `AssistantMessageActions` 的复制段改用 `MessageCopyButton`**

把 `chat.tsx` 第 356-368 行(`{hasCopyableOutput ? (<Button ...>...<Copy/>{t("common.copy")}</Button>) : null}` 整段)替换为:

```tsx
      <MessageCopyButton
        text={copyText}
        label={t("common.copy")}
        ariaLabel={t("chat.actions.copyOutput")}
        successTitle={t("chat.actions.copyOutputDone")}
        errorTitle={t("chat.actions.copyOutputFailed")}
      />
```

随后删除 `AssistantMessageActions` 里已不再使用的 `hasCopyableOutput` 局部变量(第 332 行 `const hasCopyableOutput = copyText.length > 0;`)及其内部 `handleCopy`(第 334-340 行)——这些逻辑已收敛进 `MessageCopyButton`。

> 行为等价:`MessageCopyButton` 在 `text === ""` 时返回 null,替代原 `hasCopyableOutput` 守卫;label/aria/toast 文案全部透传原 key,toast 文案不变。

- [ ] **Step 5: 删除 `chat.tsx` 不再使用的 `Copy` import**

`Copy` 现仅由刚替换掉的那段使用。从第 4-18 行的 lucide-react import 列表里删除第 8 行的 `Copy,`(若 lint 报 `copyTextWithToast` 未用也一并清理——本任务不应再直接引用它)。

- [ ] **Step 6: 跑 lint + 回归测试确认全绿**

Run: `cd frontend && pnpm exec eslint src/components/agentre/chat.tsx && pnpm test -- src/components/agentre/__tests__/chat.test.tsx src/components/agentre/__tests__/chat-panel.test.tsx src/components/agentre/__tests__/chat-streams-host.test.tsx`
Expected: lint 无 error(无 unused `Copy`/`copyTextWithToast`);测试 PASS(与 Step 1 基线一致)。

- [ ] **Step 7: 提交**

```bash
cd /Users/codfrm/Code/agentre/agentre
git add frontend/src/components/agentre/chat.tsx
git commit -m "♻️ single chat ChatMessage composes shared MessageRow/MessageCopyButton"
```

---

## Task 4: 群聊 `group-transcript` 改用 `MessageRow` + 复制按钮

**Files:**
- Modify: `frontend/src/components/agentre/group-chat/group-transcript.tsx`(第 93-122 行 agent/user 行)
- Test: `frontend/src/components/agentre/group-chat/group-transcript.test.tsx`(新)

- [ ] **Step 1: 写失败测试**

Create `frontend/src/components/agentre/group-chat/group-transcript.test.tsx`:

```tsx
import { render, screen, fireEvent } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { GroupTranscript } from "./group-transcript";

import type { app } from "../../../../wailsjs/go/models";

const copySpy = vi.fn();
vi.mock("@/lib/clipboard-toast", () => ({
  copyTextWithToast: (text: string, opts: unknown) => {
    copySpy(text, opts);
    return Promise.resolve(true);
  },
}));

const roster = [
  { id: 1, agentID: 2, role: "host", status: "active", backingSessionID: 11 },
] as unknown as app.GroupMemberItem[];

function msg(over: Partial<app.GroupMessageItem>): app.GroupMessageItem {
  return {
    id: 1,
    seq: 1,
    senderKind: "agent",
    senderMemberID: 1,
    recipientMemberIDs: [],
    toUser: false,
    content: "hello from agent",
    createtime: 0,
    ...over,
  } as app.GroupMessageItem;
}

const memberName = (id: number) => (id === 1 ? "后端" : `#${id}`);

describe("GroupTranscript", () => {
  it("agent 消息渲染规范尺寸头像(size-7)", () => {
    render(
      <GroupTranscript
        messages={[msg({})]}
        roster={roster}
        memberName={memberName}
      />,
    );
    const avatar = screen.getByLabelText("后端");
    expect(avatar.className).toContain("size-7");
  });

  it("agent 消息有复制按钮，点击以正文调用 copyTextWithToast", () => {
    copySpy.mockClear();
    render(
      <GroupTranscript
        messages={[msg({ content: "hello from agent" })]}
        roster={roster}
        memberName={memberName}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: /Copy|复制/ }));
    expect(copySpy).toHaveBeenCalledWith("hello from agent", expect.any(Object));
  });

  it("system 行不渲染复制按钮", () => {
    render(
      <GroupTranscript
        messages={[msg({ senderKind: "system", content: "X 加入了群聊" })]}
        roster={roster}
        memberName={memberName}
      />,
    );
    expect(screen.queryByRole("button", { name: /Copy|复制/ })).toBeNull();
  });
});
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd frontend && pnpm test -- src/components/agentre/group-chat/group-transcript.test.tsx`
Expected: FAIL —— 复制按钮不存在 / 头像 className 仍是 `size-6`。

- [ ] **Step 3: 改 `group-transcript.tsx`**

第 6 行 import 区在 `import { AgentAvatar } from "../primitives";` 之后加入:

```tsx
import { MessageRow, MessageCopyButton } from "../message-row";
```

把第 93-122 行(agent/user 行的 `return (<div key={msg.id} className="flex gap-3">...</div>)` 整块)替换为:

```tsx
        return (
          <MessageRow
            key={msg.id}
            avatarName={displayName}
            avatarColor={color}
            name={displayName}
            headerExtra={
              directed ? (
                <span className="text-2xs text-muted-foreground">
                  {t("group.onlyXReceived", { name: firstRecipientName })}
                </span>
              ) : null
            }
            footer={<MessageCopyButton text={msg.content} />}
          >
            <div className="whitespace-pre-wrap break-words text-sm leading-relaxed text-foreground">
              {renderBody(msg.content)}
            </div>
          </MessageRow>
        );
```

> `AgentAvatar` import 现仅 system 行外已不直接使用——检查 system 行(第 60-72)未用 `AgentAvatar`,故删除第 6 行 `AgentAvatar` import 以免 lint unused。`cn` 若也变为未使用则一并删除(system 行第 68 行用的是字面 class,不经 `cn`;agent 行原 `cn` 调用已随替换移除)→ 删除 `import { cn } from "@/lib/utils";`。

- [ ] **Step 4: 跑测试 + lint 确认通过**

Run: `cd frontend && pnpm exec eslint src/components/agentre/group-chat/group-transcript.tsx && pnpm test -- src/components/agentre/group-chat/group-transcript.test.tsx`
Expected: lint 无 unused import error;测试 PASS(3 用例)。

- [ ] **Step 5: 提交**

```bash
cd /Users/codfrm/Code/agentre/agentre
git add frontend/src/components/agentre/group-chat/group-transcript.tsx frontend/src/components/agentre/group-chat/group-transcript.test.tsx
git commit -m "✨ group transcript reuses MessageRow + adds copy button"
```

---

## Task 5: 群聊 `index` 接 `useStickToBottom` + 回到底部按钮

**Files:**
- Modify: `frontend/src/components/agentre/group-chat/index.tsx`(第 200-207 行的滚动 `<section>`)
- Test: `frontend/src/components/agentre/group-chat/group-chat.test.tsx`(追加用例)

- [ ] **Step 1: 写失败测试**

在 `frontend/src/components/agentre/group-chat/group-chat.test.tsx` 顶部 import 区补上(若尚无):

```tsx
import { act } from "@testing-library/react";
```

并在 `describe("GroupChat", ...)` 内末尾追加:

```tsx
  it("非贴底时显示「回到底部」按钮，点击拉回底部", () => {
    render(<GroupChat groupId={5} />);
    const scroller = screen.getByTestId("group-scroll");
    Object.defineProperty(scroller, "scrollHeight", {
      configurable: true,
      value: 1000,
    });
    Object.defineProperty(scroller, "clientHeight", {
      configurable: true,
      value: 500,
    });
    scroller.scrollTop = 0;

    // 初始贴底，无按钮
    expect(
      screen.queryByRole("button", { name: /Back to bottom|回到底部/ }),
    ).toBeNull();

    act(() => {
      scroller.dispatchEvent(new Event("scroll"));
    });

    const btn = screen.getByRole("button", { name: /Back to bottom|回到底部/ });
    fireEvent.click(btn);
    expect(scroller.scrollTop).toBe(1000);
  });
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd frontend && pnpm test -- src/components/agentre/group-chat/group-chat.test.tsx`
Expected: FAIL —— 找不到 `group-scroll` testid / 无回到底部按钮。

- [ ] **Step 3: 改 `index.tsx`**

在第 1-2 行 import 区(`import * as React` 之后)加入图标与 hook:

```tsx
import { ArrowDown, Pause, Play, Square } from "lucide-react";
```
(替换原第 2 行的 `import { Pause, Play, Square } from "lucide-react";`)

并在第 10 行 `import { useGroup } from "../../../hooks/use-group";` 之后加入:

```tsx
import { useStickToBottom } from "../../../hooks/use-stick-to-bottom";
```

在 `GroupChat` 组件体内、`const messages = ...`(第 50-53 行)之后加入 hook(以 `messages.length` 为 dep,新消息追加即跟随):

```tsx
  const { ref: scrollRef, atBottom, scrollToBottom, onScroll } =
    useStickToBottom(messages.length);
```

把第 200-207 行的 `<section>` 块替换为(加 ref / onScroll / testid / relative 定位 + 条件按钮):

```tsx
          <section
            ref={scrollRef as React.Ref<HTMLElement>}
            onScroll={onScroll}
            data-testid="group-scroll"
            className="relative min-h-0 flex-1 overflow-auto px-7 py-5"
          >
            <GroupTranscript
              messages={messages}
              roster={members}
              memberName={memberName}
              renderBody={renderMessageBody}
            />
            {!atBottom ? (
              <Button
                type="button"
                variant="outline"
                size="icon-sm"
                aria-label={t("chatPanel.scroll.backToBottom")}
                title={t("chatPanel.scroll.backToBottom")}
                onClick={scrollToBottom}
                className="sticky bottom-4 z-20 ml-auto flex rounded-full bg-background shadow-md hover:shadow-lg dark:bg-background animate-in fade-in slide-in-from-bottom-1 duration-200 ease-out motion-reduce:animate-none"
              >
                <ArrowDown data-icon="only" aria-hidden="true" />
              </Button>
            ) : null}
          </section>
```

> `Button` 已在第 6 行 import,无需新增。复用既有 `chatPanel.scroll.backToBottom` 文案键,不新增 i18n。

- [ ] **Step 4: 跑测试 + lint 确认通过**

Run: `cd frontend && pnpm exec eslint src/components/agentre/group-chat/index.tsx && pnpm test -- src/components/agentre/group-chat/group-chat.test.tsx`
Expected: lint 干净;测试 PASS(含原有用例 + 新用例)。

- [ ] **Step 5: 提交**

```bash
cd /Users/codfrm/Code/agentre/agentre
git add frontend/src/components/agentre/group-chat/index.tsx frontend/src/components/agentre/group-chat/group-chat.test.tsx
git commit -m "✨ group chat auto-follow scroll + back-to-bottom button"
```

---

## Task 6: 全量校验

- [ ] **Step 1: 前端测试 + lint 全绿**

Run: `cd frontend && pnpm test -- src/components/agentre src/hooks/__tests__/use-stick-to-bottom.test.ts && pnpm exec eslint src`
Expected: 全 PASS;lint 无 error(特别确认 `chat.tsx` / `group-transcript.tsx` 无 unused import,i18n no-literal-string 无新增违例)。

- [ ] **Step 2: i18n 键覆盖测试**

Run: `cd frontend && pnpm test -- src/__tests__/i18n.test.ts`
Expected: PASS(本计划未新增键,应天然通过;此步是防回退确认)。

---

## Self-Review 记录

- **Spec 覆盖**:复制按钮(Task 2 `MessageCopyButton` + Task 3/4 接入)✓;头像一致(Task 2 `MESSAGE_AVATAR_CLASS` + Task 4 断言 size-7)✓;自动跟随滚动(Task 1 hook + Task 5 接入)✓;气泡外壳复用(Task 2 `MessageRow` + Task 3 单聊组合 + Task 4 群聊组合)✓;非目标(不动 chat-panel 滚动 / 不引虚拟化 / 不动后端)均未出现在任务中 ✓。
- **占位符**:无 TBD/TODO;每个代码步给出完整代码与精确行号。
- **类型一致**:`useStickToBottom` 返回 `{ ref, atBottom, scrollToBottom, onScroll }` 在 Task 1 定义、Task 5 消费一致;`MessageRow` / `MessageCopyButton` props 在 Task 2 定义、Task 3/4 消费一致;i18n 键全部为既有键。
