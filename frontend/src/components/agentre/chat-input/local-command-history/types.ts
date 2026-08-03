export type LocalCommandHistoryScope = {
  readonly deviceId: string;
  readonly cwd: string;
};

export type LocalCommandHistoryEntry = {
  readonly command: string;
  readonly lastUsedAt: number;
};
