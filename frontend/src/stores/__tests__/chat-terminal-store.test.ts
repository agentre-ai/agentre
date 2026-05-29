import { describe, it, expect, beforeEach } from "vitest";
import { useChatTerminalStore } from "../chat-terminal-store";

describe("useChatTerminalStore", () => {
  beforeEach(() => {
    useChatTerminalStore.setState({ openSessionIDs: new Set<number>() });
  });

  it("defaults to empty set", () => {
    expect(useChatTerminalStore.getState().openSessionIDs.size).toBe(0);
    expect(useChatTerminalStore.getState().isOpen(7)).toBe(false);
  });

  it("toggle(7) opens session 7", () => {
    useChatTerminalStore.getState().toggle(7);
    expect(useChatTerminalStore.getState().isOpen(7)).toBe(true);
  });

  it("toggle(7) twice closes", () => {
    useChatTerminalStore.getState().toggle(7);
    useChatTerminalStore.getState().toggle(7);
    expect(useChatTerminalStore.getState().isOpen(7)).toBe(false);
  });

  it("toggle(8) while 7 is open keeps BOTH open (tab-scoped, multi-tab coexist)", () => {
    useChatTerminalStore.getState().toggle(7);
    useChatTerminalStore.getState().toggle(8);
    expect(useChatTerminalStore.getState().isOpen(7)).toBe(true);
    expect(useChatTerminalStore.getState().isOpen(8)).toBe(true);
    expect(useChatTerminalStore.getState().openSessionIDs.size).toBe(2);
  });

  it("closeAll clears everything", () => {
    useChatTerminalStore.getState().toggle(7);
    useChatTerminalStore.getState().toggle(8);
    useChatTerminalStore.getState().closeAll();
    expect(useChatTerminalStore.getState().openSessionIDs.size).toBe(0);
  });
});
