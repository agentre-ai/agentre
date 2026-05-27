import type { ChatTab } from "./chat-tabs-store";

export const CHAT_TABS_STORAGE_KEY = "agentre.chatTabs";

type PersistedV1 = {
  v: 1;
  tabs: Array<{
    id: string;
    sessionId: number;
    isPinned: boolean;
    pinAt: number;
    openedAt: number;
  }>;
  activeTabId: string | null;
};

function getStorage(): Storage | null {
  if (typeof window === "undefined") return null;
  try {
    return window.localStorage;
  } catch {
    return null;
  }
}

export function writePersistedTabs(
  tabs: ChatTab[],
  activeTabId: string | null,
): void {
  const storage = getStorage();
  if (!storage) return;
  const persisted: PersistedV1 = {
    v: 1,
    tabs: tabs
      .filter((t) => t.meta.kind === "session" && !t.isPreview)
      .map((t) => ({
        id: t.id,
        sessionId: (t.meta as { kind: "session"; sessionId: number }).sessionId,
        isPinned: t.isPinned,
        pinAt: t.pinAt,
        openedAt: t.openedAt,
      })),
    activeTabId,
  };
  try {
    storage.setItem(CHAT_TABS_STORAGE_KEY, JSON.stringify(persisted));
  } catch {
    /* quota / private mode */
  }
}

export function readPersistedTabs(): {
  tabs: ChatTab[];
  activeTabId: string | null;
} | null {
  const storage = getStorage();
  if (!storage) return null;
  let raw: string | null;
  try {
    raw = storage.getItem(CHAT_TABS_STORAGE_KEY);
  } catch {
    return null;
  }
  if (!raw) return null;
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    return null;
  }
  if (
    !parsed ||
    typeof parsed !== "object" ||
    (parsed as { v: number }).v !== 1
  ) {
    return null;
  }
  const p = parsed as PersistedV1;
  return {
    tabs: p.tabs.map((r) => ({
      id: r.id,
      meta: { kind: "session" as const, sessionId: r.sessionId },
      isPreview: false,
      isPinned: r.isPinned,
      pinAt: r.pinAt,
      openedAt: r.openedAt,
    })),
    activeTabId: p.activeTabId,
  };
}
