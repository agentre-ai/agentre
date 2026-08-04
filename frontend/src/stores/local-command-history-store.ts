import type {
  LocalCommandHistoryEntry,
  LocalCommandHistoryScope,
} from "@/components/agentre/chat-input/local-command-history/types";

export const LOCAL_COMMAND_HISTORY_STORAGE_KEY = "agentre.localCommandHistory";

const LOCAL_COMMAND_HISTORY_VERSION = 1;
const MAX_ENTRIES_PER_SCOPE = 100;

type PersistedLocalCommandHistory = {
  version: typeof LOCAL_COMMAND_HISTORY_VERSION;
  scopes: Record<string, LocalCommandHistoryEntry[]>;
};

export type LocalCommandHistoryStorage = Pick<Storage, "getItem" | "setItem">;

export type LocalCommandHistoryStore = {
  list(scope: LocalCommandHistoryScope): LocalCommandHistoryEntry[];
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

function isHistoryEntry(value: unknown): value is LocalCommandHistoryEntry {
  if (!value || typeof value !== "object") return false;
  const entry = value as Partial<LocalCommandHistoryEntry>;
  return (
    typeof entry.command === "string" &&
    entry.command.length > 0 &&
    typeof entry.lastUsedAt === "number" &&
    Number.isFinite(entry.lastUsedAt)
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

  return {
    list(scope) {
      const key = deriveLocalCommandHistoryScopeKey(scope);
      return (history.scopes[key] ?? []).map(({ command, lastUsedAt }) => ({
        command,
        lastUsedAt,
      }));
    },
    record(scope, command, lastUsedAt = Date.now()) {
      if (!command) return;
      const key = deriveLocalCommandHistoryScopeKey(scope);
      const usedAt = Number.isFinite(lastUsedAt) ? lastUsedAt : Date.now();
      const entries = history.scopes[key] ?? [];
      const existingEntry = entries.find((entry) => entry.command === command);
      if (existingEntry && existingEntry.lastUsedAt >= usedAt) return;

      history.scopes[key] = [
        { command, lastUsedAt: usedAt },
        ...entries.filter((entry) => entry.command !== command),
      ].slice(0, MAX_ENTRIES_PER_SCOPE);
      writePersistedHistory(storage, history);
    },
    clear(scope) {
      delete history.scopes[deriveLocalCommandHistoryScopeKey(scope)];
      writePersistedHistory(storage, history);
    },
  };
}

export const localCommandHistoryStore = createLocalCommandHistoryStore();
