import { create } from "zustand";

import type { app } from "../../wailsjs/go/models";

// GroupDetail 是 Wails 生成的 app.GroupDetailResponse 的「纯数据形态」——去掉自动
// 注入的 convertValues 方法,方便用对象 spread 在 store 内拼装(patchRunStatus /
// appendMessage 都返回新对象)。Wails 实际下行的 GroupDetailResponse 实例(含
// convertValues)结构性满足这个类型,因此 setDetail(GroupLoad 的返回)同样接受。
// 这与 chat-streams-store 的 ChatBlockData = Omit<chat_svc.ChatBlock,...> 同一手法。
export type GroupDetail = Omit<app.GroupDetailResponse, "convertValues">;

// 群聊详情按 groupId 落在一个全局 Map。放全局 store(而不是面板内部 state)的原因
// 与 chat-streams-store 一致:用户切走面板时组件 unmount,自管 state 会被销毁,
// 但后端仍会通过 group:event:<id> 推 message/run_status;集中存放让重新挂载时
// 直接读到既有详情,并让 live 事件有处可落。
type GroupState = {
  details: Map<number, GroupDetail>;
};

type GroupActions = {
  // setDetail 用 GroupLoad 拉回的全量详情覆盖该 group 的缓存。
  setDetail: (groupId: number, detail: GroupDetail) => void;
  // appendMessage 把一条 live 消息追加到 messages 末尾;按 id 去重,因为同一条
  // 消息可能既走 reload 落库、又走 group:event 实时推上来。
  appendMessage: (groupId: number, message: app.GroupMessageItem) => void;
  // patchRunStatus 只改 group.runStatus,保留其余字段不动。
  patchRunStatus: (groupId: number, runStatus: string) => void;
  // patchMember 用于 backing session 懒创建后回填成员信息。
  patchMember: (groupId: number, member: app.GroupMemberItem) => void;
  // patchMemberRunState 只改某成员的 runState(运行态: running/idle),与 patchMember
  // 的成员身份回填分开,避免彼此覆盖字段。后端 member_run_state 事件驱动。
  patchMemberRunState: (
    groupId: number,
    memberId: number,
    runState: string,
  ) => void;
  // upsertTask 落一条任务卡:已存在(按 id)则原位替换 —— task_updated 事件既送
  // 新建也送状态翻转,upsert 让两者共用一条路径;群详情未加载时丢弃(打开群时
  // GroupLoad 会带全量 tasks)。
  upsertTask: (groupId: number, task: app.GroupTaskItem) => void;
};

export const useGroupStore = create<GroupState & GroupActions>((set) => ({
  details: new Map(),

  setDetail: (groupId, detail) =>
    set((state) => {
      const next = new Map(state.details);
      next.set(groupId, detail);
      return { details: next };
    }),

  appendMessage: (groupId, message) =>
    set((state) => {
      const cur = state.details.get(groupId);
      if (!cur) return state;
      // de-dupe by id (a message may arrive both via reload and via live event)
      if (cur.messages.some((m) => m.id === message.id)) return state;
      const next = new Map(state.details);
      next.set(groupId, { ...cur, messages: [...cur.messages, message] });
      return { details: next };
    }),

  patchRunStatus: (groupId, runStatus) =>
    set((state) => {
      const cur = state.details.get(groupId);
      if (!cur || !cur.group) return state;
      const next = new Map(state.details);
      next.set(groupId, { ...cur, group: { ...cur.group, runStatus } });
      return { details: next };
    }),

  patchMember: (groupId, member) =>
    set((state) => {
      const cur = state.details.get(groupId);
      if (!cur) return state;
      const idx = cur.members.findIndex((m) => m.id === member.id);
      if (idx < 0) return state;
      const members = cur.members.slice();
      members[idx] = { ...members[idx], ...member };
      const next = new Map(state.details);
      next.set(groupId, { ...cur, members });
      return { details: next };
    }),

  patchMemberRunState: (groupId, memberId, runState) =>
    set((state) => {
      const cur = state.details.get(groupId);
      if (!cur) return state;
      const idx = cur.members.findIndex((m) => m.id === memberId);
      if (idx < 0) return state;
      // 同值短路:避免无意义重建 Map 触发额外重渲染。
      if (cur.members[idx].runState === runState) return state;
      const members = cur.members.slice();
      members[idx] = { ...members[idx], runState };
      const next = new Map(state.details);
      next.set(groupId, { ...cur, members });
      return { details: next };
    }),

  upsertTask: (groupId, task) =>
    set((state) => {
      const cur = state.details.get(groupId);
      if (!cur) return state;
      const tasks = cur.tasks ?? [];
      const idx = tasks.findIndex((t) => t.id === task.id);
      const nextTasks =
        idx < 0
          ? [...tasks, task]
          : tasks.map((t, i) => (i === idx ? task : t));
      const next = new Map(state.details);
      next.set(groupId, { ...cur, tasks: nextTasks });
      return { details: next };
    }),
}));
