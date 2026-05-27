import { beforeEach, describe, expect, it } from "vitest";
import {
  CHAT_TABS_STORAGE_KEY,
  readPersistedTabs,
  writePersistedTabs,
} from "../chat-tabs-persistence";
import type { ChatTab } from "../chat-tabs-store";

beforeEach(() => {
  localStorage.clear();
});

describe("chat-tabs-persistence · write", () => {
  it("写入 kind:'session' + isPinned + activeTabId, 跳过 kind:'new' 和 preview", () => {
    const tabs: ChatTab[] = [
      {
        id: "t1",
        meta: { kind: "session", sessionId: 1 },
        isPreview: false,
        isPinned: true,
        pinAt: 100,
        openedAt: 50,
      },
      {
        id: "t2",
        meta: { kind: "session", sessionId: 2 },
        isPreview: true,
        isPinned: false,
        pinAt: 0,
        openedAt: 60,
      },
      {
        id: "t3",
        meta: { kind: "new", projectId: 7, agentId: 3, workMode: "" },
        isPreview: false,
        isPinned: false,
        pinAt: 0,
        openedAt: 70,
      },
    ];
    writePersistedTabs(tabs, "t1");
    const raw = localStorage.getItem(CHAT_TABS_STORAGE_KEY);
    expect(raw).toBeTruthy();
    const parsed = JSON.parse(raw!);
    expect(parsed).toEqual({
      v: 1,
      tabs: [
        { id: "t1", sessionId: 1, isPinned: true, pinAt: 100, openedAt: 50 },
      ],
      activeTabId: "t1",
    });
  });
});

describe("chat-tabs-persistence · read", () => {
  it("读出后还原 kind:'session' 形态, 默认 isPreview=false", () => {
    localStorage.setItem(
      CHAT_TABS_STORAGE_KEY,
      JSON.stringify({
        v: 1,
        tabs: [
          { id: "t1", sessionId: 1, isPinned: true, pinAt: 100, openedAt: 50 },
        ],
        activeTabId: "t1",
      }),
    );
    const got = readPersistedTabs();
    expect(got).toEqual({
      tabs: [
        {
          id: "t1",
          meta: { kind: "session", sessionId: 1 },
          isPreview: false,
          isPinned: true,
          pinAt: 100,
          openedAt: 50,
        },
      ],
      activeTabId: "t1",
    });
  });

  it("schema 版本不匹配返回 null", () => {
    localStorage.setItem(
      CHAT_TABS_STORAGE_KEY,
      JSON.stringify({ v: 2, tabs: [] }),
    );
    expect(readPersistedTabs()).toBeNull();
  });

  it("解析失败返回 null", () => {
    localStorage.setItem(CHAT_TABS_STORAGE_KEY, "not-json");
    expect(readPersistedTabs()).toBeNull();
  });

  it("空 storage 返回 null", () => {
    expect(readPersistedTabs()).toBeNull();
  });
});
