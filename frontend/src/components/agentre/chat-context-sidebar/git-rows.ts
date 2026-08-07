import type { workspace_fs_svc } from "@/../wailsjs/go/models";

type Change = workspace_fs_svc.ChangeView;

/**
 * GitRow 是 Git 模式一行的显示模型。Git 变动天然跨目录且路径深，所以这里是扁平
 * 列表而不是树（设计决策 11）：`name` 主显、永不截断，`dir` 是右侧灰色的目录
 * 后缀、从头截断，根目录下的文件后缀为空串。
 */
export type GitRow = {
  /** 完整相对路径，行的 key 与「打开文件」的入参。 */
  path: string;
  name: string;
  dir: string;
  /** 后端的五类状态之一：modified / added / deleted / renamed / untracked。 */
  status: string;
  /** 仅 status==="renamed" 时非空。 */
  oldPath: string;
  added: number;
  deleted: number;
  binary: boolean;
};

/**
 * deriveGitRows 把后端的变动清单拆成行模型并按完整路径排序 —— 后端「先跟踪、
 * 后未跟踪」的产出顺序会把新建文件全都挤到末尾，与同目录的兄弟文件分开。
 */
export function deriveGitRows(changes: Change[] | null | undefined): GitRow[] {
  if (!changes) return [];
  return changes
    .map((change) => {
      const cut = change.path.lastIndexOf("/");
      return {
        path: change.path,
        name: cut < 0 ? change.path : change.path.slice(cut + 1),
        dir: cut < 0 ? "" : change.path.slice(0, cut),
        status: change.status,
        oldPath: change.oldPath ?? "",
        added: change.added ?? 0,
        deleted: change.deleted ?? 0,
        binary: Boolean(change.binary),
      };
    })
    .sort((a, b) => a.path.localeCompare(b.path));
}
