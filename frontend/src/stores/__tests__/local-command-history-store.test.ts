import { beforeEach, describe, expect, it, vi } from "vitest";

import {
  LOCAL_COMMAND_HISTORY_STORAGE_KEY,
  createLocalCommandHistoryStore,
  deriveLocalCommandHistoryScopeKey,
  type LocalCommandHistoryScope,
} from "../local-command-history-store";

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
const MAX_TIMESTAMP_RESERVATION_SEED =
  ECMASCRIPT_DATE_MAX_TIMESTAMP - TIMESTAMP_RESERVATION_HEADROOM;
const FIRST_TIMESTAMP_WITHOUT_RESERVATION_HEADROOM =
  MAX_TIMESTAMP_RESERVATION_SEED + 1;

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

  it("Given history reaches the exact accepted timestamp boundary, when it is reserved and recorded, then crossing fails closed and the existing MRU reconstructs unchanged", () => {
    vi.spyOn(Date, "now").mockReturnValue(MAX_TIMESTAMP_RESERVATION_SEED - 1);
    const store = createLocalCommandHistoryStore({ storage: localStorage });
    store.record(
      localRepo,
      "older command",
      MAX_TIMESTAMP_RESERVATION_SEED - 2,
    );

    const boundaryReservation = store.reserveLastUsedAt();
    expect(boundaryReservation).toBe(MAX_TIMESTAMP_RESERVATION_SEED);
    store.record(localRepo, "boundary command", boundaryReservation);
    const persistedAtBoundary = localStorage.getItem(
      LOCAL_COMMAND_HISTORY_STORAGE_KEY,
    );

    expect(() => store.reserveLastUsedAt()).toThrow(
      "Local command history timestamp budget exhausted",
    );
    expect(() =>
      store.record(
        localRepo,
        "must not fabricate newer recency",
        FIRST_TIMESTAMP_WITHOUT_RESERVATION_HEADROOM,
      ),
    ).toThrow("Local command history timestamp budget exhausted");
    expect(localStorage.getItem(LOCAL_COMMAND_HISTORY_STORAGE_KEY)).toBe(
      persistedAtBoundary,
    );

    const expected = [
      {
        command: "boundary command",
        lastUsedAt: MAX_TIMESTAMP_RESERVATION_SEED,
      },
      {
        command: "older command",
        lastUsedAt: MAX_TIMESTAMP_RESERVATION_SEED - 2,
      },
    ];
    expect(store.list(localRepo)).toEqual(expected);
    expect(
      createLocalCommandHistoryStore({ storage: localStorage }).list(localRepo),
    ).toEqual(expected);
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

  it("Given thousands of unseen scopes and no outstanding reservations, when each is cleared, then no lasting barrier rejects a later explicit record", () => {
    vi.spyOn(Date, "now").mockReturnValue(100);
    const store = createLocalCommandHistoryStore({ storage: null });
    const unseenScopes = Array.from({ length: 2_000 }, (_, index) => ({
      deviceId: `dynamic-device-${index}`,
      cwd: `/dynamic/cwd/${index}`,
    }));

    for (const scope of unseenScopes) store.clear(scope);
    for (const [index, scope] of unseenScopes.entries()) {
      store.record(scope, `command-${index}`, 0);
    }

    expect(
      unseenScopes.every(
        (scope, index) => store.list(scope)[0]?.command === `command-${index}`,
      ),
    ).toBe(true);
  });

  it("Given a tracked pre-clear reservation, when it records after clear, then it stays deleted and consuming it removes the obsolete barrier", () => {
    vi.spyOn(Date, "now").mockReturnValue(100);
    const store = createLocalCommandHistoryStore({ storage: localStorage });
    const preClear = store.reserveLastUsedAt();

    store.clear(localRepo);
    store.record(localRepo, "private command", preClear);

    expect(store.list(localRepo)).toEqual([]);

    store.record(localRepo, "allowed after settlement", preClear);
    expect(store.list(localRepo)).toEqual([
      { command: "allowed after settlement", lastUsedAt: preClear },
    ]);
  });

  it("Given two pre-clear reservations, when one records another scope, then the barrier remains until the other is released", () => {
    vi.spyOn(Date, "now").mockReturnValue(100);
    const store = createLocalCommandHistoryStore({ storage: localStorage });
    const firstPending = store.reserveLastUsedAt();
    const secondPending = store.reserveLastUsedAt();

    store.clear(localRepo);
    store.record(remoteRepo, "remote command", firstPending);
    store.record(localRepo, "still private", firstPending);

    expect(store.list(localRepo)).toEqual([]);
    expect(store.list(remoteRepo)).toEqual([
      { command: "remote command", lastUsedAt: firstPending },
    ]);

    store.releaseLastUsedAt(secondPending);
    store.record(localRepo, "allowed after both settle", firstPending);
    expect(store.list(localRepo)).toEqual([
      { command: "allowed after both settle", lastUsedAt: firstPending },
    ]);
  });

  it("Given a tracked reservation becomes an older duplicate, when its record is a no-op, then it still releases the last protected barrier", () => {
    vi.spyOn(Date, "now").mockReturnValue(100);
    const store = createLocalCommandHistoryStore({ storage: localStorage });
    const olderDuplicate = store.reserveLastUsedAt();
    store.record(localRepo, "duplicate command", olderDuplicate + 1);

    store.clear(remoteRepo);
    store.record(localRepo, "duplicate command", olderDuplicate);
    store.record(remoteRepo, "allowed after no-op", olderDuplicate);

    expect(store.list(localRepo)).toEqual([
      {
        command: "duplicate command",
        lastUsedAt: olderDuplicate + 1,
      },
    ]);
    expect(store.list(remoteRepo)).toEqual([
      { command: "allowed after no-op", lastUsedAt: olderDuplicate },
    ]);
  });

  it("Given a reserved submission is rejected after clear, when its timestamp is released repeatedly, then the barrier is pruned idempotently", () => {
    vi.spyOn(Date, "now").mockReturnValue(100);
    const store = createLocalCommandHistoryStore({ storage: localStorage });
    const rejectedSubmission = store.reserveLastUsedAt();

    store.clear(localRepo);
    store.releaseLastUsedAt(rejectedSubmission);
    store.releaseLastUsedAt(rejectedSubmission);
    store.record(localRepo, "allowed after rejection", rejectedSubmission);

    expect(store.list(localRepo)).toEqual([
      {
        command: "allowed after rejection",
        lastUsedAt: rejectedSubmission,
      },
    ]);
  });

  it("Given a pre-clear reservation remains pending, when a post-clear reservation records, then only the newer command is accepted", () => {
    vi.spyOn(Date, "now").mockReturnValue(100);
    const store = createLocalCommandHistoryStore({ storage: localStorage });
    const preClear = store.reserveLastUsedAt();

    store.clear(localRepo);
    const postClear = store.reserveLastUsedAt();
    store.record(localRepo, "new command", postClear);
    store.record(localRepo, "private command", preClear);

    expect(store.list(localRepo)).toEqual([
      { command: "new command", lastUsedAt: postClear },
    ]);
  });

  it("Given a scope was cleared behind a pending reservation, when a direct record omits its timestamp, then it is reserved after the barrier while the tracked pre-clear record stays deleted", () => {
    vi.spyOn(Date, "now").mockReturnValue(100);
    const store = createLocalCommandHistoryStore({ storage: localStorage });
    const preClear = store.reserveLastUsedAt();

    store.clear(localRepo);
    store.record(localRepo, "new command");
    store.record(localRepo, "private command", preClear);

    expect(store.list(localRepo)).toEqual([
      { command: "new command", lastUsedAt: preClear + 1 },
    ]);
  });

  it("Given reserved submissions settle after repeated scope clears, when records arrive out of order, then pre-clear commands stay deleted while post-clear and other-scope commands persist", () => {
    vi.spyOn(Date, "now").mockReturnValue(100);
    const setItem = vi.fn(localStorage.setItem.bind(localStorage));
    const store = createLocalCommandHistoryStore({
      storage: {
        getItem: localStorage.getItem.bind(localStorage),
        setItem,
      },
    });
    const preClearLocal = store.reserveLastUsedAt();
    const preClearRemote = store.reserveLastUsedAt();

    store.clear(localRepo);
    const persistedAfterClear = localStorage.getItem(
      LOCAL_COMMAND_HISTORY_STORAGE_KEY,
    );
    const writesAfterClear = setItem.mock.calls.length;
    store.record(localRepo, "private command", preClearLocal);

    expect(store.list(localRepo)).toEqual([]);
    expect(setItem).toHaveBeenCalledTimes(writesAfterClear);
    expect(localStorage.getItem(LOCAL_COMMAND_HISTORY_STORAGE_KEY)).toBe(
      persistedAfterClear,
    );

    store.record(remoteRepo, "remote command", preClearRemote);
    const postClearLocal = store.reserveLastUsedAt();
    store.record(localRepo, "new command", postClearLocal);
    const preSecondClearLocal = store.reserveLastUsedAt();
    store.clear(localRepo);
    store.record(localRepo, "second private command", preSecondClearLocal);
    const postSecondClearLocal = store.reserveLastUsedAt();
    store.record(localRepo, "newest command", postSecondClearLocal);

    expect(store.list(localRepo)).toEqual([
      { command: "newest command", lastUsedAt: postSecondClearLocal },
    ]);
    expect(store.list(remoteRepo)).toEqual([
      { command: "remote command", lastUsedAt: preClearRemote },
    ]);
    expect(
      JSON.parse(localStorage.getItem(LOCAL_COMMAND_HISTORY_STORAGE_KEY)!),
    ).toEqual({
      version: 1,
      scopes: {
        [deriveLocalCommandHistoryScopeKey(localRepo)]: [
          { command: "newest command", lastUsedAt: postSecondClearLocal },
        ],
        [deriveLocalCommandHistoryScopeKey(remoteRepo)]: [
          { command: "remote command", lastUsedAt: preClearRemote },
        ],
      },
    });
  });
});

describe("local command history mutation subscriptions", () => {
  it("Given two listeners, when accepted records and repeated clears mutate scopes, then notifications are synchronous while no-op records and unsubscribed listeners stay silent", () => {
    vi.spyOn(Date, "now").mockReturnValue(0);
    const store = createLocalCommandHistoryStore({ storage: localStorage });
    const localScopeKey = deriveLocalCommandHistoryScopeKey(localRepo);
    const remoteScopeKey = deriveLocalCommandHistoryScopeKey(remoteRepo);
    const observedEntries: Array<{
      type: "record" | "clear";
      scopeKey: string;
      commands: string[];
    }> = [];
    const firstListener = vi.fn(
      (mutation: { type: "record" | "clear"; scopeKey: string }) => {
        const scope =
          mutation.scopeKey === localScopeKey ? localRepo : remoteRepo;
        observedEntries.push({
          ...mutation,
          commands: store.list(scope).map(({ command }) => command),
        });
      },
    );
    const secondListener = vi.fn();
    const unsubscribeFirst = store.subscribe(firstListener);
    const unsubscribeSecond = store.subscribe(secondListener);

    store.record(localRepo, "pnpm test", 10);
    store.record(localRepo, "pnpm test", 5);
    store.record(localRepo, "", 15);
    store.record(remoteRepo, "make deploy", 20);
    store.clear(localRepo);
    store.clear(localRepo);

    expect(observedEntries).toEqual([
      {
        type: "record",
        scopeKey: localScopeKey,
        commands: ["pnpm test"],
      },
      {
        type: "record",
        scopeKey: remoteScopeKey,
        commands: ["make deploy"],
      },
      { type: "clear", scopeKey: localScopeKey, commands: [] },
      { type: "clear", scopeKey: localScopeKey, commands: [] },
    ]);
    expect(secondListener).toHaveBeenCalledTimes(4);

    unsubscribeFirst();
    store.record(localRepo, "git status", 30);
    expect(firstListener).toHaveBeenCalledTimes(4);
    expect(secondListener).toHaveBeenCalledTimes(5);

    unsubscribeSecond();
    store.clear(remoteRepo);
    expect(secondListener).toHaveBeenCalledTimes(5);
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
