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
const ECMASCRIPT_DATE_MAX_TIMESTAMP = 8_640_000_000_000_000;
const TIMESTAMP_RESERVATION_HEADROOM = 1_000_000;
const FIRST_TIMESTAMP_WITHOUT_RESERVATION_HEADROOM =
  ECMASCRIPT_DATE_MAX_TIMESTAMP - TIMESTAMP_RESERVATION_HEADROOM + 1;

beforeEach(() => {
  vi.restoreAllMocks();
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

  it("Given persisted timestamps across scopes and a rolled-back clock, when submission order is reserved after reconstruction, then every reservation stays above persisted history and prior reservations", () => {
    localStorage.setItem(
      LOCAL_COMMAND_HISTORY_STORAGE_KEY,
      JSON.stringify({
        version: 1,
        scopes: {
          [deriveLocalCommandHistoryScopeKey(localRepo)]: [
            { command: "pnpm test", lastUsedAt: 900 },
          ],
          [deriveLocalCommandHistoryScopeKey(remoteRepo)]: [
            { command: "make deploy", lastUsedAt: 1_200 },
          ],
        },
      }),
    );
    vi.spyOn(Date, "now").mockReturnValue(500);

    const reconstructed = createLocalCommandHistoryStore({
      storage: localStorage,
    });
    const firstReservation = reconstructed.reserveLastUsedAt();
    vi.mocked(Date.now).mockReturnValue(100);

    expect(firstReservation).toBe(1_201);
    expect(reconstructed.reserveLastUsedAt()).toBe(1_202);
  });

  it("Given repeated reservations and a newer explicit record, when the clock does not advance, then reservations are cross-call monotonic and the accepted record advances their floor", () => {
    vi.spyOn(Date, "now").mockReturnValue(100);
    const store = createLocalCommandHistoryStore({ storage: localStorage });

    expect(store.reserveLastUsedAt()).toBe(101);
    expect(store.reserveLastUsedAt()).toBe(102);

    store.record(localRepo, "new floor", 500);
    expect(store.reserveLastUsedAt()).toBe(501);

    store.record(localRepo, "new floor", 400);
    expect(store.list(localRepo)).toEqual([
      { command: "new floor", lastUsedAt: 500 },
    ]);
    expect(store.reserveLastUsedAt()).toBe(502);
    expect(Array.from({ length: 4 }, () => store.reserveLastUsedAt())).toEqual([
      503, 504, 505, 506,
    ]);
  });

  it.each([
    ["the ECMAScript Date ceiling", ECMASCRIPT_DATE_MAX_TIMESTAMP],
    [
      "the first timestamp without one-million-reservation headroom",
      FIRST_TIMESTAMP_WITHOUT_RESERVATION_HEADROOM,
    ],
    ["the safe-integer ceiling", Number.MAX_SAFE_INTEGER],
    ["an unsafe integer", Number.MAX_SAFE_INTEGER + 1],
    ["a negative integer", -1],
    ["a fractional number", 100.5],
  ])(
    "Given %s as an explicit timestamp, when a command is recorded, then a valid monotonic timestamp is persisted without poisoning later reservations",
    (_label, invalidTimestamp) => {
      vi.spyOn(Date, "now").mockReturnValue(100);
      const store = createLocalCommandHistoryStore({ storage: localStorage });
      store.record(localRepo, "older valid command", 100);

      store.record(localRepo, "invalid timestamp command", invalidTimestamp);

      expect(store.list(localRepo)).toEqual([
        { command: "invalid timestamp command", lastUsedAt: 101 },
        { command: "older valid command", lastUsedAt: 100 },
      ]);
      expect([
        store.reserveLastUsedAt(),
        store.reserveLastUsedAt(),
        store.reserveLastUsedAt(),
      ]).toEqual([102, 103, 104]);
      expect(
        createLocalCommandHistoryStore({ storage: localStorage }).list(
          localRepo,
        ),
      ).toEqual(store.list(localRepo));
    },
  );

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

  it("Given 101 commands submitted oldest to newest, when records settle newest to oldest, then the submission-MRU keeps the newest 100", () => {
    const store = createLocalCommandHistoryStore({ storage: localStorage });
    const submissions = Array.from({ length: 101 }, (_, index) => ({
      command: `command-${index}`,
      lastUsedAt: index + 1,
    }));

    for (const { command, lastUsedAt } of [...submissions].reverse()) {
      store.record(localRepo, command, lastUsedAt);
    }

    const entries = store.list(localRepo);
    const commands = entries.map(({ command }) => command);
    expect
      .soft({
        newestRetained: commands.includes("command-100"),
        oldestRetained: commands.includes("command-0"),
      })
      .toEqual({ newestRetained: true, oldestRetained: false });
    expect(entries).toEqual([...submissions.slice(1)].reverse());
    expect(
      createLocalCommandHistoryStore({ storage: localStorage }).list(localRepo),
    ).toEqual(entries);
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
    ["the exact ECMAScript Date ceiling", ECMASCRIPT_DATE_MAX_TIMESTAMP],
    [
      "the first timestamp without one-million-reservation headroom",
      FIRST_TIMESTAMP_WITHOUT_RESERVATION_HEADROOM,
    ],
  ])(
    "Given persisted history at %s, when reconstructed and used again, then it is rejected before multiple reservations can poison new storage",
    (_label, poisonTimestamp) => {
      localStorage.setItem(
        LOCAL_COMMAND_HISTORY_STORAGE_KEY,
        JSON.stringify({
          version: 1,
          scopes: {
            [deriveLocalCommandHistoryScopeKey(localRepo)]: [
              { command: "must not partially survive", lastUsedAt: 40 },
            ],
            [deriveLocalCommandHistoryScopeKey(remoteRepo)]: [
              { command: "poisoned", lastUsedAt: poisonTimestamp },
            ],
          },
        }),
      );
      vi.spyOn(Date, "now").mockReturnValue(100);

      const store = createLocalCommandHistoryStore({ storage: localStorage });

      expect(store.list(localRepo)).toEqual([]);
      expect(store.list(remoteRepo)).toEqual([]);
      const reservations = Array.from({ length: 3 }, () =>
        store.reserveLastUsedAt(),
      );
      expect(reservations).toEqual([101, 102, 103]);

      store.record(localRepo, "safe command", reservations[2]);
      const reconstructed = createLocalCommandHistoryStore({
        storage: localStorage,
      });
      expect(reconstructed.list(localRepo)).toEqual([
        { command: "safe command", lastUsedAt: 103 },
      ]);
      expect(reconstructed.reserveLastUsedAt()).toBe(104);
    },
  );

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
    [
      "a safe-integer ceiling timestamp beside otherwise valid history",
      JSON.stringify({
        version: 1,
        scopes: {
          [deriveLocalCommandHistoryScopeKey(localRepo)]: [
            { command: "must not partially survive", lastUsedAt: 40 },
          ],
          [deriveLocalCommandHistoryScopeKey(remoteRepo)]: [
            { command: "poisoned", lastUsedAt: Number.MAX_SAFE_INTEGER },
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
