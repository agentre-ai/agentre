import { describe, expect, it } from "vitest";
import {
  computeRunStats,
  formatDuration,
  inProgressRuns,
  recentDoneRuns,
  runProgress,
} from "../overview-data";

const NOW = 1_700_000_000_000;
const HOUR = 3_600_000;
const DAY = 24 * HOUR;

function run(
  over: Partial<{
    id: number;
    status: string;
    createtime: number;
    updatetime: number;
  }>,
) {
  return {
    id: 1,
    goal: "g",
    leaderAgentId: 0,
    status: "running",
    projectId: 0,
    flowId: 0,
    flowContent: "",
    rootTaskId: 0,
    createtime: NOW,
    updatetime: NOW,
    ...over,
  } as never;
}

describe("computeRunStats", () => {
  it("空列表返回全零统计", () => {
    expect(computeRunStats([], NOW)).toEqual({
      running: 0,
      waiting: 0,
      doneThisWeek: 0,
      avgDurationMs: 0,
    });
  });

  it("统计 running / waiting(paused) / 本周完成 / 平均时长", () => {
    const runs = [
      run({ id: 1, status: "running" }),
      run({ id: 2, status: "paused" }),
      run({
        id: 3,
        status: "done",
        createtime: NOW - 2 * HOUR,
        updatetime: NOW - 1 * HOUR,
      }),
      run({
        id: 4,
        status: "done",
        createtime: NOW - 4 * HOUR,
        updatetime: NOW - 1 * HOUR,
      }),
      // 8 天前完成 → 不计入本周完成
      run({
        id: 5,
        status: "done",
        createtime: NOW - 9 * DAY,
        updatetime: NOW - 8 * DAY,
      }),
    ];
    const s = computeRunStats(runs, NOW);
    expect(s.running).toBe(1);
    expect(s.waiting).toBe(1);
    expect(s.doneThisWeek).toBe(2);
    // 三个 done 的时长: 1h, 3h, 1d → 平均 (1+3+24)/3 h = 28/3 h
    expect(s.avgDurationMs).toBe(Math.round((HOUR + 3 * HOUR + DAY) / 3));
  });

  it("无 done 时平均时长为 0", () => {
    expect(
      computeRunStats([run({ status: "running" })], NOW).avgDurationMs,
    ).toBe(0);
  });
});

describe("inProgressRuns / recentDoneRuns", () => {
  it("inProgressRuns 空列表返回空数组", () => {
    expect(inProgressRuns([])).toEqual([]);
  });

  it("recentDoneRuns 空列表返回空数组", () => {
    expect(recentDoneRuns([], 5)).toEqual([]);
  });

  it("inProgressRuns 只取 running 并按 updatetime 倒序", () => {
    const runs = [
      run({ id: 1, status: "running", updatetime: NOW - 3 * HOUR }),
      run({ id: 2, status: "done", updatetime: NOW }),
      run({ id: 3, status: "running", updatetime: NOW - 1 * HOUR }),
    ];
    expect(inProgressRuns(runs).map((r) => r.id)).toEqual([3, 1]);
  });

  it("recentDoneRuns 只取 done、倒序、限量", () => {
    const runs = [
      run({ id: 1, status: "done", updatetime: NOW - 3 * HOUR }),
      run({ id: 2, status: "done", updatetime: NOW - 1 * HOUR }),
      run({ id: 3, status: "running", updatetime: NOW }),
    ];
    expect(recentDoneRuns(runs, 1).map((r) => r.id)).toEqual([2]);
  });
});

describe("runProgress", () => {
  it("done = status==='done' 计数, total = 长度", () => {
    const tasks = [
      { status: "done" },
      { status: "running" },
      { status: "done" },
    ] as never[];
    expect(runProgress(tasks)).toEqual({ done: 2, total: 3 });
  });
  it("undefined → 0/0", () => {
    expect(runProgress(undefined)).toEqual({ done: 0, total: 0 });
  });
});

describe("formatDuration", () => {
  it("分/时/天 分段", () => {
    expect(formatDuration(38 * 60_000)).toBe("38m");
    expect(formatDuration(2 * HOUR)).toBe("2h");
    expect(formatDuration(1 * DAY)).toBe("1d");
    expect(formatDuration(0)).toBe("");
  });

  it("负数输入返回空字符串", () => {
    expect(formatDuration(-1000)).toBe("");
  });

  it("亚分钟非零时长返回 '0m'(当前行为特征测试)", () => {
    // 30 秒 < 1 分钟: Math.floor(30000/60000)=0 → "0m"; 记录当前行为,勿改实现
    expect(formatDuration(30_000)).toBe("0m");
  });
});
