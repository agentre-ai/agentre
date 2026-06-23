import { create } from "zustand";

// 进入意图:browse=打开后停在浏览/预览态;create=打开后直接进空白编辑器。
// 内部的 view/editor/delete-confirm 细分态是 WorkflowManagerDialog 的本地状态,
// store 只负责"开/关 + 初次进入意图"。
type WorkflowManagerIntent = "browse" | "create";

type State = {
  open: boolean;
  intent: WorkflowManagerIntent;
};

type Actions = {
  openBrowse: () => void;
  openCreate: () => void;
  close: () => void;
};

// UI-only store(无数据):流程数据仍由 useWorkflows 拥有。放 store 是为了让命令面板
// source / 建群弹窗 link 等无父子关系的入口都能打开同一个根挂载的弹窗,免 prop drilling。
export const useWorkflowManagerStore = create<State & Actions>((set) => ({
  open: false,
  intent: "browse",
  openBrowse: () => set({ open: true, intent: "browse" }),
  openCreate: () => set({ open: true, intent: "create" }),
  close: () => set({ open: false, intent: "browse" }),
}));
