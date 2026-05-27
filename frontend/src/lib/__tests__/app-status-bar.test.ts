import { describe, expect, it } from "vitest";

import { deriveAppStatusBarState } from "../app-status-bar";

describe("deriveAppStatusBarState", () => {
  it("counts actual agents and running sessions", () => {
    const state = deriveAppStatusBarState(
      [
        {
          sessions: [
            { id: 1, status: "running" },
            { id: 2, status: "idle" },
          ],
        },
        { sessions: [{ id: 3, status: "running" }] },
      ],
      new Map(),
      new Map(),
      new Map(),
    );

    expect(state).toMatchObject({
      agentSummary: "2 agents · 2 running",
      attentionSummary: null,
      indicatorStatus: "running",
    });
  });

  it("uses live status overlays before stale list snapshots", () => {
    const state = deriveAppStatusBarState(
      [{ sessions: [{ id: 1, status: "idle" }] }],
      new Map([[1, { agentStatus: "running", needsAttention: false }]]),
      new Map(),
      new Map(),
    );

    expect(state.agentSummary).toBe("1 agent · 1 running");
    expect(state.indicatorStatus).toBe("running");
  });

  it("surfaces approvals and unread sessions as an orange attention summary", () => {
    const state = deriveAppStatusBarState(
      [
        {
          sessions: [
            { id: 1, status: "waiting", needsAttention: true },
            { id: 2, status: "idle", lastMessageAt: 200, lastReadAt: 100 },
            { id: 3, status: "running", lastMessageAt: 300, lastReadAt: 0 },
          ],
        },
      ],
      new Map(),
      new Map(),
      new Map(),
    );

    expect(state.agentSummary).toBe("1 agent · 1 running");
    expect(state.attentionSummary).toBe("1 approval · 1 unread");
    expect(state.indicatorStatus).toBe("waiting");
  });

  it("uses read overrides when deciding whether a session is unread", () => {
    const state = deriveAppStatusBarState(
      [{ sessions: [{ id: 1, status: "idle", lastMessageAt: 200 }] }],
      new Map(),
      new Map(),
      new Map([[1, 200]]),
    );

    expect(state.attentionSummary).toBeNull();
    expect(state.indicatorStatus).toBe("idle");
  });
});
