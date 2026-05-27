import { render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { AgentSession } from "../agent-list";
import { SessionGroup } from "../session-group";

function unreadSession(id: number): AgentSession {
  return {
    id: String(id),
    status: "waiting",
    title: `unread-${id}`,
    trailingLabel: "未读",
    attentionRank: "unread",
  };
}

function needsAttentionSession(id: number): AgentSession {
  return {
    id: String(id),
    status: "waiting",
    title: `approve-${id}`,
    trailingLabel: "审批",
    attentionRank: "needs_attention",
  };
}

function selectedSession(id: number): AgentSession {
  return {
    id: String(id),
    status: "idle",
    title: `selected-${id}`,
    trailingLabel: "1m ago",
    attentionRank: "selected",
  };
}

function ordinarySession(id: number): AgentSession {
  return {
    id: String(id),
    status: "idle",
    title: `idle-${id}`,
    trailingLabel: "5m ago",
  };
}

function queryBubble(container: HTMLElement): HTMLElement | null {
  return container.querySelector('[data-slot="agent-attention-bubble"]');
}

describe("SessionGroup attention bubble in expanded state", () => {
  it("keeps unread sessions in the attention bubble when expanded (so they can pick up ⌘N chip)", () => {
    const unread = unreadSession(1);
    const idle = ordinarySession(2);

    const { container } = render(
      <SessionGroup
        defaultExpanded
        sessions={[unread, idle]}
        attentionSessions={[unread]}
        renderHeader={() => <div data-testid="header" />}
      />,
    );

    const bubble = queryBubble(container);
    expect(bubble).not.toBeNull();
    expect(within(bubble!).getByText("unread-1")).toBeTruthy();
    // 下方常规列表通过 attentionIds 去重，所以未读不会同时出现在两处。
    expect(within(bubble!).queryByText("idle-2")).toBeNull();
    // idle 仍然在下方常规列表里出现。
    expect(screen.getByText("idle-2")).toBeTruthy();
  });

  it("still filters selected sessions out of the bubble when expanded (selected stays in the regular list at its natural position)", () => {
    const selected = selectedSession(7);
    const idle = ordinarySession(8);

    const { container } = render(
      <SessionGroup
        defaultExpanded
        sessions={[idle, selected]}
        attentionSessions={[selected]}
        renderHeader={() => <div data-testid="header" />}
      />,
    );

    // 没有任何 BubbleRank 留在 bubble 里 → bubble 元素本身不渲染。
    expect(queryBubble(container)).toBeNull();
    // selected + ordinary 都出现在下方常规列表。
    expect(screen.getByText("selected-7")).toBeTruthy();
    expect(screen.getByText("idle-8")).toBeTruthy();
  });

  it("keeps all BubbleRank entries (including unread + selected) in the bubble when collapsed", () => {
    const unread = unreadSession(1);
    const selected = selectedSession(2);
    const needs = needsAttentionSession(3);

    const { container } = render(
      <SessionGroup
        defaultExpanded={false}
        sessions={[unread, selected, needs]}
        attentionSessions={[needs, unread, selected]}
        renderHeader={() => <div data-testid="header" />}
      />,
    );

    const bubble = queryBubble(container);
    expect(bubble).not.toBeNull();
    expect(within(bubble!).getByText("unread-1")).toBeTruthy();
    expect(within(bubble!).getByText("selected-2")).toBeTruthy();
    expect(within(bubble!).getByText("approve-3")).toBeTruthy();
  });
});
