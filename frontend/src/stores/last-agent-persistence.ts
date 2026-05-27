// localStorage 持久化:命令面板上次选过的 agentId。
// 下次 ⌘N 打开时,把这个 agent 提到组首(member 组首),让"再开一次"是 1 次回车。
//
// 单一全局值 —— 不按 project 拆分:用户的肌肉记忆是"哪个 agent",
// 不是"在哪个项目下用哪个 agent"。

const KEY = "agentre.commandPalette.lastAgentId";

export function readLastAgentId(): number | null {
  try {
    const raw = localStorage.getItem(KEY);
    if (!raw) return null;
    const n = Number(raw);
    if (!Number.isFinite(n) || n <= 0) return null;
    return n;
  } catch {
    return null;
  }
}

export function writeLastAgentId(id: number): void {
  if (!Number.isFinite(id) || id <= 0) return;
  try {
    localStorage.setItem(KEY, String(id));
  } catch {
    /* ignore quota errors */
  }
}

export function clearLastAgentId(): void {
  try {
    localStorage.removeItem(KEY);
  } catch {
    /* ignore */
  }
}
