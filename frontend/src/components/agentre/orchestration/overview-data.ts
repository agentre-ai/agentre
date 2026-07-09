import type { app } from "../../../../wailsjs/go/models";

export type RunStats = {
  running: number;
  waiting: number;
  doneThisWeek: number;
  avgDurationMs: number;
};

const WEEK_MS = 7 * 24 * 60 * 60 * 1000;

// 从 Run 列表客户端 derive 概要统计。createtime/updatetime 为毫秒(UnixMilli)。
export function computeRunStats(runs: app.RunItemDTO[], now: number): RunStats {
  let running = 0;
  let waiting = 0;
  let doneThisWeek = 0;
  let durSum = 0;
  let durCount = 0;
  for (const r of runs) {
    if (r.status === "running") running++;
    else if (r.status === "paused") waiting++;
    if (r.status === "done") {
      if (now - r.updatetime <= WEEK_MS) doneThisWeek++;
      const dur = r.updatetime - r.createtime;
      if (dur > 0) {
        durSum += dur;
        durCount++;
      }
    }
  }
  return {
    running,
    waiting,
    doneThisWeek,
    avgDurationMs: durCount > 0 ? Math.round(durSum / durCount) : 0,
  };
}

// 进行中(running)的 Run,按 updatetime 倒序。
export function inProgressRuns(runs: app.RunItemDTO[]): app.RunItemDTO[] {
  return runs
    .filter((r) => r.status === "running")
    .slice()
    .sort((a, b) => b.updatetime - a.updatetime);
}

// 最近完成(done)的 Run,按 updatetime 倒序,取前 limit。
export function recentDoneRuns(
  runs: app.RunItemDTO[],
  limit = 5,
): app.RunItemDTO[] {
  return runs
    .filter((r) => r.status === "done")
    .slice()
    .sort((a, b) => b.updatetime - a.updatetime)
    .slice(0, limit);
}

// Run 进度:done/total。镜像 task-board.tsx(done=status==="done" 计数,total=tasks.length)。
export function runProgress(tasks: app.DispatchDTO[] | undefined): {
  done: number;
  total: number;
} {
  const t = tasks ?? [];
  return {
    done: t.filter((x) => x.status === "done").length,
    total: t.length,
  };
}

// 时长格式化:ms → "38m"/"2h"/"1d";ms<=0 → ""。
export function formatDuration(ms: number): string {
  if (ms <= 0) return "";
  const min = Math.floor(ms / 60000);
  if (min < 60) return `${min}m`;
  const hr = Math.floor(min / 60);
  if (hr < 24) return `${hr}h`;
  const day = Math.floor(hr / 24);
  return `${day}d`;
}
