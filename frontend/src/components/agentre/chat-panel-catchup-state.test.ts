import { beforeEach, describe, expect, it } from "vitest";

import {
  __resetCatchUpStateForTesting,
  clearCatchUp,
  getCatchUp,
  recordCatchUp,
} from "./chat-panel-catchup-state";

// 补齐摘要 = 「用户还没看见的那批补齐内容」。它决定转录区底部那枚跳转控件出不出、
// 上面写几条 —— 所以「空补齐不留摘要」「多次补齐累加」「待决策取最新快照」这三条
// 都是可判别行为:任何一条写错,用户要么看到一枚数字骗人的控件,要么根本看不到。
describe("transcript catch-up summary", () => {
  beforeEach(() => {
    __resetCatchUpStateForTesting();
  });

  it("Given a catch-up replayed notifications, When it lands, Then the session carries the new-item count and the pending-decision count", () => {
    recordCatchUp(42, 12, 1);

    expect(getCatchUp(42)).toEqual({ newItems: 12, pendingDecisions: 1 });
  });

  it("Given a second catch-up before the user looked, Then new items accumulate and pending takes the latest snapshot", () => {
    recordCatchUp(42, 12, 1);
    recordCatchUp(42, 3, 0);

    expect(getCatchUp(42)).toEqual({ newItems: 15, pendingDecisions: 0 });
  });

  it("Given a catch-up that replayed nothing, Then no summary is recorded (an empty catch-up must not float a control)", () => {
    recordCatchUp(42, 0, 2);

    expect(getCatchUp(42)).toBeNull();
  });

  it("Given another session caught up, Then this session is untouched", () => {
    recordCatchUp(7, 5, 0);

    expect(getCatchUp(42)).toBeNull();
    expect(getCatchUp(7)).toEqual({ newItems: 5, pendingDecisions: 0 });
  });

  it("Given the user jumped to the latest position, When the summary is cleared, Then nothing is left behind", () => {
    recordCatchUp(42, 12, 1);

    clearCatchUp(42);

    expect(getCatchUp(42)).toBeNull();
  });

  // 快照引用必须稳定:useSyncExternalStore 每次渲染都读一次 getSnapshot,
  // 每次返回新对象会被 React 判成「外部 store 一直在变」而无限重渲。
  it("Given the summary did not change, When it is read twice, Then the same snapshot reference comes back", () => {
    recordCatchUp(42, 12, 1);

    expect(getCatchUp(42)).toBe(getCatchUp(42));
  });
});
