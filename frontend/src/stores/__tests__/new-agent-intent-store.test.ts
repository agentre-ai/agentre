import { beforeEach, describe, expect, it, vi } from "vitest";

import {
  consumeNewAgentDialogIntent,
  requestNewAgentDialog,
  subscribeNewAgentIntent,
} from "../new-agent-intent-store";

describe("new-agent intent store", () => {
  beforeEach(() => {
    consumeNewAgentDialogIntent();
  });

  it("Given no pending intent, when requested and consumed, then it is cleared", () => {
    expect(consumeNewAgentDialogIntent()).toBe(false);

    requestNewAgentDialog();

    expect(consumeNewAgentDialogIntent()).toBe(true);
    expect(consumeNewAgentDialogIntent()).toBe(false);
  });

  it("Given a subscriber, when the intent is requested twice, then each request is observable and consumption stays idempotent", () => {
    const listener = vi.fn();
    const unsubscribe = subscribeNewAgentIntent(listener);

    requestNewAgentDialog();
    requestNewAgentDialog();

    expect(listener).toHaveBeenCalledTimes(2);
    expect(consumeNewAgentDialogIntent()).toBe(true);
    expect(consumeNewAgentDialogIntent()).toBe(false);

    unsubscribe();
    requestNewAgentDialog();
    expect(listener).toHaveBeenCalledTimes(2);
    expect(consumeNewAgentDialogIntent()).toBe(true);
  });
});
