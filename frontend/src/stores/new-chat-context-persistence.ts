import type { ProjectContext } from "./new-chat-context-store";

// localStorage 持久化：用户在命令面板 ContextBar 里手动选过的项目。
// 下次 ⌘N 打开时，如果没有 project-page 注入的 context，就用这个作为默认。
//
// 不持久化"无项目"状态 —— 用户主动选「无项目」时直接清这条记录，
// 下次默认仍是「无项目」。

const KEY = "agentre.commandPalette.lastContext";

export type LastContext = ProjectContext;

export function readLastContext(): LastContext | null {
  try {
    const raw = localStorage.getItem(KEY);
    if (!raw) return null;
    const v = JSON.parse(raw) as Partial<LastContext>;
    if (
      typeof v.projectID !== "number" ||
      v.projectID <= 0 ||
      typeof v.projectName !== "string"
    ) {
      return null;
    }
    return {
      projectID: v.projectID,
      projectName: v.projectName,
    };
  } catch {
    return null;
  }
}

export function writeLastContext(value: LastContext): void {
  try {
    localStorage.setItem(KEY, JSON.stringify(value));
  } catch {
    /* ignore quota errors */
  }
}

export function clearLastContext(): void {
  try {
    localStorage.removeItem(KEY);
  } catch {
    /* ignore */
  }
}
