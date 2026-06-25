import { describe, it, expect, beforeEach } from "vitest";
import { useChatTabsStore } from "../chat-tabs-store";

describe("attachTerminal", () => {
  beforeEach(() =>
    useChatTabsStore.setState({ tabs: [], activeTabId: undefined }),
  );
  it("creates an active attach terminal tab bound to the same terminalId", () => {
    useChatTabsStore
      .getState()
      .attachTerminal({ terminalId: "t1", command: "go test" });
    const st = useChatTabsStore.getState();
    const tab = st.tabs.at(-1)!;
    expect(tab.meta).toMatchObject({
      kind: "terminal",
      terminalId: "t1",
      attach: true,
    });
    expect(st.activeTabId).toBe(tab.id);
  });
});
