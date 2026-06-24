export const ORCH_EVENTS = {
  updated: "orch:run:updated",
  done: "orch:run:done",
  paused: "orch:run:paused",
  resumed: "orch:run:resumed",
  stopped: "orch:run:stopped",
  deadlock: "orch:run:deadlock",
} as const;

export type OrchEventName = (typeof ORCH_EVENTS)[keyof typeof ORCH_EVENTS];
