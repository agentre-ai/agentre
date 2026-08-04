export type LocalCommandHistoryScope = {
  readonly deviceId: string;
  readonly cwd: string;
};

export type LocalCommandHistoryEntry = {
  readonly command: string;
  readonly lastUsedAt: number;
};

export const LOCAL_COMMAND_HISTORY_STORAGE_KEY = "agentre.localCommandHistory";

const LOCAL_COMMAND_HISTORY_VERSION = 1;
const MAX_ENTRIES_PER_SCOPE = 100;
const MAX_ECMASCRIPT_DATE_TIMESTAMP = 8_640_000_000_000_000;
// Persisted timestamps use a ceiling one million ticks below ECMAScript Date's.
// The decoder, explicit writes, and reservations enforce this same ceiling;
// once reached, reservation fails closed so existing MRU stays reconstructable.
const TIMESTAMP_RESERVATION_HEADROOM = 1_000_000;
const MAX_TIMESTAMP_RESERVATION_SEED =
  MAX_ECMASCRIPT_DATE_TIMESTAMP - TIMESTAMP_RESERVATION_HEADROOM;

type PersistedLocalCommandHistory = {
  version: typeof LOCAL_COMMAND_HISTORY_VERSION;
  scopes: Record<string, LocalCommandHistoryEntry[]>;
};

export type LocalCommandHistoryStorage = Pick<Storage, "getItem" | "setItem">;

export type LocalCommandHistoryStore = {
  list(scope: LocalCommandHistoryScope): LocalCommandHistoryEntry[];
  reserveLastUsedAt(): number;
  record(
    scope: LocalCommandHistoryScope,
    command: string,
    lastUsedAt?: number,
  ): void;
  clear(scope: LocalCommandHistoryScope): void;
};

type CreateLocalCommandHistoryStoreOptions = {
  storage?: LocalCommandHistoryStorage | null;
};

function browserStorage(): LocalCommandHistoryStorage | null {
  if (typeof window === "undefined") return null;
  try {
    return window.localStorage;
  } catch {
    return null;
  }
}

function encodeScopeKey({ deviceId, cwd }: LocalCommandHistoryScope): string {
  const deviceScope = deviceId ? ["remote", deviceId] : ["local"];
  const cwdScope = cwd ? ["cwd", cwd] : ["default"];
  return JSON.stringify([deviceScope, cwdScope]);
}

function isHistoryScopeKey(scopeKey: string): boolean {
  let value: unknown;
  try {
    value = JSON.parse(scopeKey);
  } catch {
    return false;
  }
  if (!Array.isArray(value) || value.length !== 2) return false;

  const [deviceScope, cwdScope] = value;
  if (!Array.isArray(deviceScope) || !Array.isArray(cwdScope)) return false;

  const isLocal = deviceScope.length === 1 && deviceScope[0] === "local";
  const isRemote =
    deviceScope.length === 2 &&
    deviceScope[0] === "remote" &&
    typeof deviceScope[1] === "string" &&
    deviceScope[1].length > 0;
  const isDefault = cwdScope.length === 1 && cwdScope[0] === "default";
  const isCwd =
    cwdScope.length === 2 &&
    cwdScope[0] === "cwd" &&
    typeof cwdScope[1] === "string" &&
    cwdScope[1].length > 0;
  if ((!isLocal && !isRemote) || (!isDefault && !isCwd)) return false;

  return (
    encodeScopeKey({
      deviceId: isRemote ? (deviceScope[1] as string) : "",
      cwd: isCwd ? (cwdScope[1] as string) : "",
    }) === scopeKey
  );
}

function isValidHistoryTimestamp(value: unknown): value is number {
  return (
    typeof value === "number" &&
    Number.isSafeInteger(value) &&
    value >= 0 &&
    value <= MAX_ECMASCRIPT_DATE_TIMESTAMP
  );
}

function hasTimestampReservationHeadroom(value: unknown): value is number {
  return (
    isValidHistoryTimestamp(value) && value <= MAX_TIMESTAMP_RESERVATION_SEED
  );
}

function ensurePersistableHistoryTimestamp(value: number): number {
  if (!hasTimestampReservationHeadroom(value)) {
    throw new RangeError("Local command history timestamp budget exhausted");
  }
  return value;
}

function reservationClockTimestamp(): number {
  const now = Date.now();
  return hasTimestampReservationHeadroom(now) ? now : 0;
}

function isHistoryEntry(value: unknown): value is LocalCommandHistoryEntry {
  if (!value || typeof value !== "object") return false;
  const entry = value as Partial<LocalCommandHistoryEntry>;
  return (
    typeof entry.command === "string" &&
    entry.command.length > 0 &&
    hasTimestampReservationHeadroom(entry.lastUsedAt)
  );
}

function normalizeEntries(
  entries: readonly LocalCommandHistoryEntry[],
): LocalCommandHistoryEntry[] {
  const commands = new Set<string>();
  return [...entries]
    .sort((left, right) => right.lastUsedAt - left.lastUsedAt)
    .filter(({ command }) => {
      if (commands.has(command)) return false;
      commands.add(command);
      return true;
    })
    .slice(0, MAX_ENTRIES_PER_SCOPE)
    .map(({ command, lastUsedAt }) => ({ command, lastUsedAt }));
}

function decodePersistedHistory(
  raw: string | null,
): PersistedLocalCommandHistory {
  const empty: PersistedLocalCommandHistory = {
    version: LOCAL_COMMAND_HISTORY_VERSION,
    scopes: {},
  };
  if (!raw) return empty;

  let value: unknown;
  try {
    value = JSON.parse(raw);
  } catch {
    return empty;
  }
  if (!value || typeof value !== "object") return empty;

  const persisted = value as Partial<PersistedLocalCommandHistory>;
  if (
    persisted.version !== LOCAL_COMMAND_HISTORY_VERSION ||
    !persisted.scopes ||
    typeof persisted.scopes !== "object" ||
    Array.isArray(persisted.scopes)
  ) {
    return empty;
  }

  const scopes = Object.create(null) as Record<
    string,
    LocalCommandHistoryEntry[]
  >;
  for (const [scopeKey, entries] of Object.entries(persisted.scopes)) {
    if (
      !isHistoryScopeKey(scopeKey) ||
      !Array.isArray(entries) ||
      !entries.every(isHistoryEntry)
    ) {
      return empty;
    }
    scopes[scopeKey] = normalizeEntries(entries);
  }
  return { version: LOCAL_COMMAND_HISTORY_VERSION, scopes };
}

function maximumLastUsedAt(history: PersistedLocalCommandHistory): number {
  let maximum = Number.NEGATIVE_INFINITY;
  for (const entries of Object.values(history.scopes)) {
    for (const { lastUsedAt } of entries) {
      maximum = Math.max(maximum, lastUsedAt);
    }
  }
  return maximum;
}

function readPersistedHistory(
  storage: LocalCommandHistoryStorage | null,
): PersistedLocalCommandHistory {
  if (!storage) return decodePersistedHistory(null);
  try {
    return decodePersistedHistory(
      storage.getItem(LOCAL_COMMAND_HISTORY_STORAGE_KEY),
    );
  } catch {
    return decodePersistedHistory(null);
  }
}

function writePersistedHistory(
  storage: LocalCommandHistoryStorage | null,
  history: PersistedLocalCommandHistory,
): void {
  if (!storage) return;
  try {
    storage.setItem(LOCAL_COMMAND_HISTORY_STORAGE_KEY, JSON.stringify(history));
  } catch {
    // Quota, private-mode, and serialization failures intentionally stay in memory.
  }
}

export function deriveLocalCommandHistoryScopeKey(
  scope: LocalCommandHistoryScope,
): string {
  return encodeScopeKey(scope);
}

export function createLocalCommandHistoryStore(
  options: CreateLocalCommandHistoryStoreOptions = {},
): LocalCommandHistoryStore {
  const storage =
    "storage" in options ? (options.storage ?? null) : browserStorage();
  const history = readPersistedHistory(storage);
  let lastReservedAt = Math.max(
    reservationClockTimestamp(),
    maximumLastUsedAt(history),
  );
  // Clear barriers stay in memory because in-flight frontend promises do not survive restart.
  const clearBarriers = new Map<string, number>();
  const reserveLastUsedAt = () => {
    const reservation = ensurePersistableHistoryTimestamp(
      Math.max(lastReservedAt, reservationClockTimestamp()) + 1,
    );
    lastReservedAt = reservation;
    return reservation;
  };

  return {
    list(scope) {
      const key = deriveLocalCommandHistoryScopeKey(scope);
      return (history.scopes[key] ?? []).map(({ command, lastUsedAt }) => ({
        command,
        lastUsedAt,
      }));
    },
    reserveLastUsedAt,
    record(scope, command, lastUsedAt = Date.now()) {
      if (!command) return;
      const key = deriveLocalCommandHistoryScopeKey(scope);
      const usedAt = ensurePersistableHistoryTimestamp(
        hasTimestampReservationHeadroom(lastUsedAt)
          ? lastUsedAt
          : reserveLastUsedAt(),
      );
      const clearBarrier = clearBarriers.get(key);
      if (clearBarrier !== undefined && usedAt <= clearBarrier) return;

      const entries = history.scopes[key] ?? [];
      const existingEntry = entries.find((entry) => entry.command === command);
      if (existingEntry && existingEntry.lastUsedAt >= usedAt) return;

      history.scopes[key] = normalizeEntries([
        { command, lastUsedAt: usedAt },
        ...entries,
      ]);
      lastReservedAt = Math.max(lastReservedAt, usedAt);
      writePersistedHistory(storage, history);
    },
    clear(scope) {
      const key = deriveLocalCommandHistoryScopeKey(scope);
      delete history.scopes[key];
      clearBarriers.set(key, lastReservedAt);
      writePersistedHistory(storage, history);
    },
  };
}

export const localCommandHistoryStore = createLocalCommandHistoryStore();
