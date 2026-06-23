import { render } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("../../../../wailsjs/runtime/runtime", () => ({
  EventsOn: vi.fn(),
  EventsOff: vi.fn(),
}));
vi.mock("../../../../wailsjs/go/app/App", () => ({
  GroupList: vi.fn(),
  ListChatAgents: vi.fn(),
  ProjectListSessions: vi.fn(),
}));
vi.mock("@/hooks/use-project-tree", () => ({
  ensureProjectTreeLoaded: vi.fn(),
  isProjectTreeCacheLoaded: () => false,
}));

import { useChatAgentsStore, type AgentSlim } from "@/stores/chat-agents-store";
import {
  useGroupListStore,
  type GroupListItem,
} from "@/stores/group-list-store";
import { useGroupStore, type GroupDetail } from "@/stores/group-store";
import { useProjectSessionsStore } from "@/stores/project-sessions-store";
import { useSessionStatusStore } from "@/stores/session-status-store";

import { EventsOn } from "../../../../wailsjs/runtime/runtime";
import { GroupEventsHost } from "../group-events-host";

type Handler = (payload: unknown) => void;

function mountHostAndGetHandler(): Handler {
  const handlers = new Map<string, Handler>();
  (EventsOn as ReturnType<typeof vi.fn>).mockImplementation(
    (evt: string, h: Handler) => {
      handlers.set(evt, h);
      return () => {};
    },
  );
  render(<GroupEventsHost />);
  const h = handlers.get("groups:run_state");
  expect(h, "GroupEventsHost 必须订阅全局 groups:run_state 频道").toBeDefined();
  return h as Handler;
}

// 回归(侧栏 running 不亮): member_run_state / run_status 原先只有打开的群页消费
// (且不写 session-status-store),侧栏的群行与成员 backing session 行拿不到运行态。
// GroupEventsHost 常驻订阅全局频道,翻译到侧栏依赖的三个 store。
describe("GroupEventsHost", () => {
  beforeEach(() => {
    // 见 sidebar-reload.test: spyOn 复用已存在的 spy 会漏调用计数, 每例先 restore。
    vi.restoreAllMocks();
    useGroupListStore.getState().__reset();
    useGroupListStore.setState({
      groups: [
        { id: 5, title: "队", runStatus: "waiting_user", pinned: false },
      ] as unknown as GroupListItem[],
      loading: false,
      error: null,
    });
    useGroupStore.setState({ details: new Map() });
    useGroupStore.getState().setDetail(5, {
      group: { id: 5, title: "队", runStatus: "waiting_user", roundCount: 0 },
      members: [{ id: 1, runState: "idle", backingSessionID: 11 }],
      messages: [],
    } as unknown as GroupDetail);
    useSessionStatusStore.getState().__reset();
    useChatAgentsStore.getState().__reset();
    useProjectSessionsStore.getState().__reset();
  });

  function seedSidebarAgents(...sessionIds: number[]) {
    useChatAgentsStore.setState({
      agents: [
        {
          id: 1,
          name: "Eng",
          sessions: [],
          sessionIds,
        } as unknown as AgentSlim,
      ],
      loading: false,
      error: null,
    });
  }

  it("patches group-list-store and group-store on run_status", () => {
    const handler = mountHostAndGetHandler();
    handler({ kind: "run_status", groupID: 5, runStatus: "running" });

    expect(useGroupListStore.getState().groups[0].runStatus).toBe("running");
    expect(useGroupStore.getState().details.get(5)?.group?.runStatus).toBe(
      "running",
    );
  });

  it("flips the backing session row to running on member_run_state", () => {
    const handler = mountHostAndGetHandler();
    handler({
      kind: "member_run_state",
      groupID: 5,
      memberID: 1,
      runState: "running",
      backingSessionID: 11,
    });

    expect(useSessionStatusStore.getState().statuses.get(11)?.agentStatus).toBe(
      "running",
    );
    // roster 同步(群页未打开时 store 里有缓存也要保持新鲜)。
    expect(useGroupStore.getState().details.get(5)?.members[0].runState).toBe(
      "running",
    );
  });

  it("downgrades running to idle on member_run_state idle", () => {
    useSessionStatusStore.getState().upsert(11, {
      agentStatus: "running",
      needsAttention: false,
    });
    const handler = mountHostAndGetHandler();
    handler({
      kind: "member_run_state",
      groupID: 5,
      memberID: 1,
      runState: "idle",
      backingSessionID: 11,
    });

    expect(useSessionStatusStore.getState().statuses.get(11)?.agentStatus).toBe(
      "idle",
    );
  });

  it("does not downgrade waiting/error on member_run_state idle", () => {
    // 成员 turn 中可能翻 waiting(等审批): 调度器层面的 idle 不得覆盖,
    // 否则侧栏丢掉「需要你处理」的黄点。
    useSessionStatusStore.getState().upsert(11, {
      agentStatus: "waiting",
      needsAttention: true,
    });
    const handler = mountHostAndGetHandler();
    handler({
      kind: "member_run_state",
      groupID: 5,
      memberID: 1,
      runState: "idle",
      backingSessionID: 11,
    });

    const v = useSessionStatusStore.getState().statuses.get(11);
    expect(v?.agentStatus).toBe("waiting");
    expect(v?.needsAttention).toBe(true);
  });

  it("preserves needsAttention/permissionMode when flipping to running", () => {
    useSessionStatusStore.getState().upsert(11, {
      agentStatus: "idle",
      needsAttention: true,
      permissionMode: "plan",
    });
    const handler = mountHostAndGetHandler();
    handler({
      kind: "member_run_state",
      groupID: 5,
      memberID: 1,
      runState: "running",
      backingSessionID: 11,
    });

    const v = useSessionStatusStore.getState().statuses.get(11);
    expect(v?.agentStatus).toBe("running");
    expect(v?.needsAttention).toBe(true);
    expect(v?.permissionMode).toBe("plan");
  });

  it("ignores member_run_state without a backing session id", () => {
    const handler = mountHostAndGetHandler();
    handler({
      kind: "member_run_state",
      groupID: 5,
      memberID: 1,
      runState: "running",
      backingSessionID: 0,
    });
    expect(useSessionStatusStore.getState().statuses.size).toBe(0);
  });

  // 回归(成员被 @ 后会话不进左栏 / 不亮 running): backing session 是被 @ 那轮才
  // 惰性新建的, 而群成员轮不经过 ChatPanel.onSidebarShouldReload —— 左栏数据源
  // chat-agents-store 没人 reload, 这一行永远进不了列表(行不在, running 也无处挂)。
  // GroupEventsHost 收到 running 时若该 backing session 还没被左栏收录, 应补一次 reload。
  it("reloads the sidebar when a running member's backing session is not listed yet", () => {
    seedSidebarAgents(99); // 左栏只知道 99, 不知道新建的 11
    const chatReload = vi
      .spyOn(useChatAgentsStore.getState(), "reload")
      .mockResolvedValue();

    const handler = mountHostAndGetHandler();
    handler({
      kind: "member_run_state",
      groupID: 5,
      memberID: 1,
      runState: "running",
      backingSessionID: 11,
    });

    expect(chatReload).toHaveBeenCalledTimes(1);
  });

  it("does not reload the sidebar when the backing session is already listed", () => {
    seedSidebarAgents(11); // 左栏已收录 11
    const chatReload = vi
      .spyOn(useChatAgentsStore.getState(), "reload")
      .mockResolvedValue();

    const handler = mountHostAndGetHandler();
    handler({
      kind: "member_run_state",
      groupID: 5,
      memberID: 1,
      runState: "running",
      backingSessionID: 11,
    });

    expect(chatReload).not.toHaveBeenCalled();
  });
});
