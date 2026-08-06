import { beforeEach, describe, expect, it } from "vitest";

import {
  __resetCatchUpStateForTesting,
  clearCatchUp,
  getCatchUp,
  openCatchUpWindow,
  recordCatchUp,
  registerTranscriptRowCounter,
} from "./chat-panel-catchup-state";

// 补齐摘要 = 「用户还没看见的那批补齐内容」。它决定转录区底部那枚跳转控件出不出、
// 上面写几条 —— 所以「空补齐不留摘要」「多次补齐累加」「待决策取最新快照」这三条
// 都是可判别行为:任何一条写错,用户要么看到一枚数字骗人的控件,要么根本看不到。
//
// 而那个数必须是**转录行**数:daemon 每个 agentruntime 事件落一行日志、重连后逐条
// 重放,一条长回复能重放上千条通知而只多出一两行。下面每条用例都刻意让重放的通知
// 条数与行数不同量级,拿通知条数充数的实现在这里全红。
describe("transcript catch-up summary", () => {
  beforeEach(() => {
    __resetCatchUpStateForTesting();
  });

  /** 装一个会话的转录行数取数口(生产里是挂载中的 ChatPanel)。 */
  function transcriptOf(sessionId: number, initialRows: number) {
    const state = { rows: initialRows };
    const unregister = registerTranscriptRowCounter(
      sessionId,
      () => state.rows,
    );
    return {
      grewBy(rows: number) {
        state.rows += rows;
      },
      shrankTo(rows: number) {
        state.rows = rows;
      },
      unregister,
    };
  }

  it("Given a catch-up replayed a thousand notifications into twelve new rows, When it lands, Then the session carries the row count and the pending-decision count", () => {
    const transcript = transcriptOf(42, 30);
    openCatchUpWindow(42);
    transcript.grewBy(12);

    recordCatchUp(42, 1_206, 1);

    expect(getCatchUp(42)).toEqual({ newRows: 12, pendingDecisions: 1 });
    transcript.unregister();
  });

  it("Given a second catch-up before the user looked, Then new rows accumulate and pending takes the latest snapshot", () => {
    const transcript = transcriptOf(42, 30);
    openCatchUpWindow(42);
    transcript.grewBy(12);
    recordCatchUp(42, 900, 1);

    openCatchUpWindow(42);
    transcript.grewBy(3);
    recordCatchUp(42, 40, 0);

    expect(getCatchUp(42)).toEqual({ newRows: 15, pendingDecisions: 0 });
    transcript.unregister();
  });

  // 补齐把上千条 delta 全追加进了还在流的那一行:行数没变,内容确实多了。摘要照留
  // (控件要浮出),只是没有条数可报 —— 不许拿重放的通知条数顶上。
  it("Given a catch-up that only grew an existing row, Then a summary is still recorded with zero new rows", () => {
    const transcript = transcriptOf(42, 30);
    openCatchUpWindow(42);

    recordCatchUp(42, 1_200, 0);

    expect(getCatchUp(42)).toEqual({ newRows: 0, pendingDecisions: 0 });
    transcript.unregister();
  });

  // turn 在断连期间跑完:补齐落定那一刻在流的行已被摘掉、reload 还没回来,行数会
  // 比基准还少。这只是数不出来,不是「负的新内容」。
  it("Given the transcript momentarily shrank across the window, Then the count floors at zero instead of going negative", () => {
    const transcript = transcriptOf(42, 30);
    openCatchUpWindow(42);
    transcript.shrankTo(28);

    recordCatchUp(42, 1_200, 0);

    expect(getCatchUp(42)).toEqual({ newRows: 0, pendingDecisions: 0 });
    transcript.unregister();
  });

  it("Given a catch-up that replayed nothing, Then no summary is recorded (an empty catch-up must not float a control)", () => {
    const transcript = transcriptOf(42, 30);
    openCatchUpWindow(42);
    transcript.grewBy(4);

    recordCatchUp(42, 0, 2);

    expect(getCatchUp(42)).toBeNull();
    transcript.unregister();
  });

  // 这个 tab 根本没打开(没人能数行)时补齐照样要留摘要:用户下次打开就落在底部,
  // 控件随即销账,而丢掉这一发会让「刚才那次补齐」彻底无声。
  it("Given no transcript is mounted to count rows, Then the catch-up is still recorded with zero new rows", () => {
    openCatchUpWindow(42);

    recordCatchUp(42, 1_200, 1);

    expect(getCatchUp(42)).toEqual({ newRows: 0, pendingDecisions: 1 });
  });

  // 一次断连里连接态可能反复跳(reconnecting → lost),窗口只认第一发 ——
  // 重取基准会把已经补进来的那一段算丢。
  it("Given the channel bounces again inside the same outage, Then the baseline stays at the first drop", () => {
    const transcript = transcriptOf(42, 30);
    openCatchUpWindow(42);
    transcript.grewBy(5);
    openCatchUpWindow(42);
    transcript.grewBy(2);

    recordCatchUp(42, 800, 0);

    expect(getCatchUp(42)).toEqual({ newRows: 7, pendingDecisions: 0 });
    transcript.unregister();
  });

  it("Given another session caught up, Then this session is untouched", () => {
    const transcript = transcriptOf(7, 10);
    openCatchUpWindow(7);
    transcript.grewBy(5);

    recordCatchUp(7, 500, 0);

    expect(getCatchUp(42)).toBeNull();
    expect(getCatchUp(7)).toEqual({ newRows: 5, pendingDecisions: 0 });
    transcript.unregister();
  });

  it("Given the user jumped to the latest position, When the summary is cleared, Then nothing is left behind", () => {
    const transcript = transcriptOf(42, 30);
    openCatchUpWindow(42);
    transcript.grewBy(12);
    recordCatchUp(42, 900, 1);

    clearCatchUp(42);

    expect(getCatchUp(42)).toBeNull();
    transcript.unregister();
  });

  // 会话换 tab 重挂时新面板先注册、旧面板后卸载。注销必须只摘自己那一份,
  // 否则活着的那个面板从此数不出行,补齐一律报 0。
  it("Given a new transcript registered before the old one unmounted, When the old one unregisters, Then the live counter survives", () => {
    const stale = transcriptOf(42, 30);
    const live = transcriptOf(42, 100);
    stale.unregister();

    openCatchUpWindow(42);
    live.grewBy(6);
    recordCatchUp(42, 700, 0);

    expect(getCatchUp(42)).toEqual({ newRows: 6, pendingDecisions: 0 });
    live.unregister();
  });

  // 快照引用必须稳定:useSyncExternalStore 每次渲染都读一次 getSnapshot,
  // 每次返回新对象会被 React 判成「外部 store 一直在变」而无限重渲。
  it("Given the summary did not change, When it is read twice, Then the same snapshot reference comes back", () => {
    const transcript = transcriptOf(42, 30);
    openCatchUpWindow(42);
    transcript.grewBy(12);
    recordCatchUp(42, 900, 1);

    expect(getCatchUp(42)).toBe(getCatchUp(42));
    transcript.unregister();
  });
});
