import { beforeEach, describe, expect, it, vi } from "vitest";

import {
  LOCAL_COMMAND_HISTORY_STORAGE_KEY,
  createLocalCommandHistoryStore,
  deriveLocalCommandHistoryScopeKey,
} from "../local-command-history-store";
import type { LocalCommandHistoryScope } from "@/components/agentre/chat-input/local-command-history/types";

const localRepo: LocalCommandHistoryScope = { deviceId: "", cwd: "/repo" };
const localOtherRepo: LocalCommandHistoryScope = {
  deviceId: "",
  cwd: "/other",
};
const remoteRepo: LocalCommandHistoryScope = {
  deviceId: "device-1",
  cwd: "/repo",
};

beforeEach(() => {
  localStorage.clear();
});

describe("local command history scope and persistence", () => {
  it("Given local and remote submissions in different cwd scopes, when reconstructed, then only versioned command MRU data is restored per deterministic scope", () => {
    const store = createLocalCommandHistoryStore({ storage: localStorage });

    store.record(localRepo, "pnpm test", 10);
    store.record(localOtherRepo, "go test ./...", 20);
    store.record(remoteRepo, "make deploy", 30);

    const localDefault = deriveLocalCommandHistoryScopeKey({
      deviceId: "",
      cwd: "",
    });
    const remoteDefault = deriveLocalCommandHistoryScopeKey({
      deviceId: "device-1",
      cwd: "",
    });
    expect(localDefault).not.toBe(remoteDefault);
    expect(deriveLocalCommandHistoryScopeKey(localRepo)).not.toBe(
      deriveLocalCommandHistoryScopeKey(remoteRepo),
    );
    expect(deriveLocalCommandHistoryScopeKey(localRepo)).not.toBe(
      deriveLocalCommandHistoryScopeKey(localOtherRepo),
    );

    const reconstructed = createLocalCommandHistoryStore({
      storage: localStorage,
    });
    expect(reconstructed.list(localRepo)).toEqual([
      { command: "pnpm test", lastUsedAt: 10 },
    ]);
    expect(reconstructed.list(localOtherRepo)).toEqual([
      { command: "go test ./...", lastUsedAt: 20 },
    ]);
    expect(reconstructed.list(remoteRepo)).toEqual([
      { command: "make deploy", lastUsedAt: 30 },
    ]);

    expect(
      JSON.parse(localStorage.getItem(LOCAL_COMMAND_HISTORY_STORAGE_KEY)!),
    ).toEqual({
      version: 1,
      scopes: {
        [deriveLocalCommandHistoryScopeKey(localRepo)]: [
          { command: "pnpm test", lastUsedAt: 10 },
        ],
        [deriveLocalCommandHistoryScopeKey(localOtherRepo)]: [
          { command: "go test ./...", lastUsedAt: 20 },
        ],
        [deriveLocalCommandHistoryScopeKey(remoteRepo)]: [
          { command: "make deploy", lastUsedAt: 30 },
        ],
      },
    });
  });

  it("Given exact repeats settle out of order, when an older write follows a newer one, then timestamp, MRU order, and persistence stay on the newer case-sensitive entry", () => {
    const setItem = vi.fn(localStorage.setItem.bind(localStorage));
    const store = createLocalCommandHistoryStore({
      storage: {
        getItem: localStorage.getItem.bind(localStorage),
        setItem,
      },
    });

    store.record(localRepo, "Git Status", 10);
    store.record(localRepo, "pnpm test", 20);
    store.record(localRepo, "git status", 25);
    store.record(localRepo, "Git Status", 30);

    const expectedEntries = [
      { command: "Git Status", lastUsedAt: 30 },
      { command: "git status", lastUsedAt: 25 },
      { command: "pnpm test", lastUsedAt: 20 },
    ];
    const persistedBeforeStaleWrite = localStorage.getItem(
      LOCAL_COMMAND_HISTORY_STORAGE_KEY,
    );
    const writesBeforeStaleWrite = setItem.mock.calls.length;

    store.record(localRepo, "Git Status", 5);

    expect(store.list(localRepo)).toEqual(expectedEntries);
    expect(setItem).toHaveBeenCalledTimes(writesBeforeStaleWrite);
    expect(localStorage.getItem(LOCAL_COMMAND_HISTORY_STORAGE_KEY)).toBe(
      persistedBeforeStaleWrite,
    );
    expect(
      createLocalCommandHistoryStore({ storage: localStorage }).list(localRepo),
    ).toEqual(expectedEntries);
  });

  it("Given 101 unique commands in one scope, when the last is recorded, then only the newest 100 remain in that scope", () => {
    const store = createLocalCommandHistoryStore({ storage: localStorage });

    for (let index = 0; index <= 100; index += 1) {
      store.record(localRepo, `command-${index}`, index);
    }

    const entries = store.list(localRepo);
    expect(entries).toHaveLength(100);
    expect(entries[0]).toEqual({ command: "command-100", lastUsedAt: 100 });
    expect(entries[entries.length - 1]).toEqual({
      command: "command-1",
      lastUsedAt: 1,
    });
    expect(entries.some(({ command }) => command === "command-0")).toBe(false);
  });

  it("Given two populated scopes, when one scope is cleared, then its editor history is empty and the other scope survives reconstruction", () => {
    const store = createLocalCommandHistoryStore({ storage: localStorage });
    store.record(localRepo, "pnpm test", 10);
    store.record(remoteRepo, "make deploy", 20);

    store.clear(localRepo);

    expect(store.list(localRepo)).toEqual([]);
    expect(store.list(remoteRepo)).toEqual([
      { command: "make deploy", lastUsedAt: 20 },
    ]);
    const reconstructed = createLocalCommandHistoryStore({
      storage: localStorage,
    });
    expect(reconstructed.list(localRepo)).toEqual([]);
    expect(reconstructed.list(remoteRepo)).toEqual([
      { command: "make deploy", lastUsedAt: 20 },
    ]);
  });
});

describe("local command history storage failures", () => {
  it.each([
    ["invalid JSON", "not-json"],
    ["unknown version", JSON.stringify({ version: 99, scopes: {} })],
    [
      "malformed entries",
      JSON.stringify({
        version: 1,
        scopes: {
          [deriveLocalCommandHistoryScopeKey(localRepo)]: [
            { command: "unsafe", lastUsedAt: "yesterday" },
          ],
        },
      }),
    ],
  ])(
    "Given %s, when read and followed by a valid record, then callers see empty history and storage is rebuilt",
    (_label, raw) => {
      localStorage.setItem(LOCAL_COMMAND_HISTORY_STORAGE_KEY, raw);
      const store = createLocalCommandHistoryStore({ storage: localStorage });

      expect(store.list(localRepo)).toEqual([]);
      expect(() => store.record(localRepo, "safe command", 50)).not.toThrow();

      const reconstructed = createLocalCommandHistoryStore({
        storage: localStorage,
      });
      expect(reconstructed.list(localRepo)).toEqual([
        { command: "safe command", lastUsedAt: 50 },
      ]);
    },
  );

  it("Given malformed scope metadata, when followed by a valid record, then storage is rebuilt without retaining out-of-schema history", () => {
    localStorage.setItem(
      LOCAL_COMMAND_HISTORY_STORAGE_KEY,
      JSON.stringify({
        version: 1,
        scopes: {
          "not-a-device-cwd-scope": [
            { command: "should not survive", lastUsedAt: 10 },
          ],
        },
      }),
    );
    const store = createLocalCommandHistoryStore({ storage: localStorage });

    store.record(localRepo, "safe command", 50);

    expect(
      JSON.parse(localStorage.getItem(LOCAL_COMMAND_HISTORY_STORAGE_KEY)!),
    ).toEqual({
      version: 1,
      scopes: {
        [deriveLocalCommandHistoryScopeKey(localRepo)]: [
          { command: "safe command", lastUsedAt: 50 },
        ],
      },
    });
  });

  it("Given unavailable reads and failing writes, when commands are recorded and cleared, then callers never receive an error and current-run memory remains usable", () => {
    const failingStorage = {
      getItem(): string | null {
        throw new Error("private mode");
      },
      setItem(): void {
        throw new Error("quota exceeded");
      },
    };

    expect(() =>
      createLocalCommandHistoryStore({ storage: failingStorage }),
    ).not.toThrow();
    const store = createLocalCommandHistoryStore({ storage: failingStorage });
    expect(() => store.record(localRepo, "pnpm test", 10)).not.toThrow();
    expect(store.list(localRepo)).toEqual([
      { command: "pnpm test", lastUsedAt: 10 },
    ]);
    expect(() => store.clear(localRepo)).not.toThrow();
    expect(store.list(localRepo)).toEqual([]);
  });
});
