import { describe, expect, it } from "vitest";

import { isResolvedAskState } from "./ask-event-state";

describe("isResolvedAskState", () => {
  it("answered → resolved (route to merge-by-requestId)", () => {
    expect(isResolvedAskState({ answered: true })).toBe(true);
  });

  it("skipped → resolved", () => {
    expect(isResolvedAskState({ skipped: true })).toBe(true);
  });

  // 回归:turn finalize 的失效锁定 patch（answered=false && skipped=false &&
  // expired=true）必须算终态,才会走 markAskUserQuestionAnswered 合并、翻转在屏
  // 等待卡;漏掉 expired 会走 appendLiveAskUserQuestion 追加重复卡且不翻转。
  it("expired → resolved (regression: finalize lock patch must merge, not append)", () => {
    expect(isResolvedAskState({ expired: true })).toBe(true);
  });

  it("none set (still waiting) → not resolved (append new live card)", () => {
    expect(isResolvedAskState({})).toBe(false);
  });
});
