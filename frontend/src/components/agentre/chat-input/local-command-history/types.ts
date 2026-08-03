export type LocalCommandHistoryScope = {
  readonly deviceId: string;
  readonly cwd: string;
};

export type LocalCommandHistoryEntry = {
  readonly command: string;
  readonly lastUsedAt: number;
};

export type LocalCommandHistoryMenuState = {
  readonly open: boolean;
  readonly anchorRect: {
    readonly left: number;
    readonly top: number;
    readonly bottom: number;
  } | null;
  readonly items: LocalCommandHistoryEntry[];
  readonly selectedIndex: number;
  readonly query: string;
};
