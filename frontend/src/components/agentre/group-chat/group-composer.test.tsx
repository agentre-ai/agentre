import { render, screen, fireEvent } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";

import { GroupComposer } from "./group-composer";

import type { app } from "../../../../wailsjs/go/models";

type GroupMemberItem = app.GroupMemberItem;

// 成员名（动态内容）从 memberName 解析：id 7→Bob, 8→Bobby, 4→"Code Reviewer"。
const members = [
  { id: 7, agentID: 70, backingSessionID: 0, role: "member", status: "active" },
  { id: 8, agentID: 80, backingSessionID: 0, role: "member", status: "active" },
  { id: 4, agentID: 40, backingSessionID: 0, role: "member", status: "active" },
] as unknown as GroupMemberItem[];

const NAMES: Record<number, string> = {
  7: "Bob",
  8: "Bobby",
  4: "Code Reviewer",
};
const memberName = (id: number) => NAMES[id] ?? `#${id}`;

function typeAndSend(text: string) {
  const onSend = vi.fn();
  render(
    <GroupComposer members={members} memberName={memberName} onSend={onSend} />,
  );
  const textarea = screen.getByRole("textbox");
  fireEvent.change(textarea, { target: { value: text } });
  fireEvent.click(screen.getByRole("button", { name: /Send|发送/ }));
  return onSend;
}

describe("GroupComposer recipient derivation", () => {
  it("does NOT route Bob (substring) when only @Bobby is in the text", () => {
    const onSend = typeAndSend("hi @Bobby please review");
    expect(onSend).toHaveBeenCalledWith({
      text: "hi @Bobby please review",
      recipientMemberIDs: [8], // Bobby only; never 7 (Bob).
    });
  });

  it("routes a multi-word member name @Code Reviewer correctly", () => {
    const onSend = typeAndSend("ping @Code Reviewer now");
    expect(onSend).toHaveBeenCalledWith({
      text: "ping @Code Reviewer now",
      recipientMemberIDs: [4],
    });
  });

  it("derives recipients from the final text, not from prior autocomplete picks", () => {
    // text mentions both Bob and Bobby → both routed, de-duped & order-stable.
    const onSend = typeAndSend("@Bob and @Bobby go");
    expect(onSend).toHaveBeenCalledWith({
      text: "@Bob and @Bobby go",
      recipientMemberIDs: [7, 8],
    });
  });
});
