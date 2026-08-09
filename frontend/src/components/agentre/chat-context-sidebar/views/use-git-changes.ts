import * as React from "react";

import {
  WorkspaceFsGitBranches,
  WorkspaceFsGitChanges,
} from "@/../wailsjs/go/app/App";
import { useChatSidebarStore } from "@/stores/chat-sidebar-store";
import { useSessionStatus } from "@/stores/session-status-store";

import type { workspace_fs_svc } from "@/../wailsjs/go/models";

import { errorText } from "./panel-feedback";

/** 未提交档回答「现在还有什么没提交」，本分支档回答「相对基线一共改了什么」。 */
export type GitScope = "uncommitted" | "branch";

type Change = workspace_fs_svc.ChangeView;

export type GitChangesState =
  | { status: "loading" }
  | { status: "loaded"; view: workspace_fs_svc.GitChangesView }
  | { status: "error"; message: string };

type Params = {
  sessionId: number;
  /** 会话工作目录；空串表示没有工作目录，直接出空态、不打后端。 */
  cwd: string;
  /** 只有 Git 模式可见时才取数（决策 13：无手动刷新、也不在别的模式后台拉）。 */
  enabled: boolean;
  /**
   * 目录模式可见时为 true：目录树要叠加「未提交」档的 git 状态（served
   * requirement「目录模式的 git 状态叠加」——与 Git 页未提交档同一份数据）。
   * 这只多让本 hook 的同一个 effect 在目录模式下也触发一次取数，不会新开
   * 第二个 useGitChanges 实例，两个消费者永远只共享一次在途请求
   * （enabled 与 overlayEnabled 由 ChatContextSidebar 里的 activeTab /
   * filesMode 互斥推出，从不同时以各自的档位需要两份不同数据）。
   */
  overlayEnabled?: boolean;
};

export type GitChanges = {
  scope: GitScope;
  setScope: (scope: GitScope) => void;
  state: GitChangesState;
  /** 分支清单，供基线下拉用；只在本分支档取，失败时为 null（下拉出空提示）。 */
  branches: workspace_fs_svc.GitBranchesView | null;
  /** 本次实际比较用的基线；未提交档恒为空，本分支档为空表示推断不出默认分支。 */
  baseRef: string;
  selectBaseline: (ref: string) => void;
  /** 已加载且是 git 仓库时的变动文件数，用于模式控件角标；否则为 null。 */
  count: number | null;
  notARepo: boolean;
  reload: () => void;
  /**
   * 目录模式叠加用的「未提交」变动清单：非 git 仓库、读取失败或尚未加载时为
   * null——状态是增强信息，缺失时目录树只是不带叠加，不因此不可用。
   */
  overlayChanges: Change[] | null;
};

/**
 * useGitChanges 管 Git 模式的取数。数据是快照，在「本模式可见且无缓存」与
 * 「当前会话轮次结束」两个时机自动重拉（决策 13）——轮次结束是「文件可能变了」
 * 的唯一强信号，所以订阅 session-status-store 的 doneTick。
 */
export function useGitChanges({
  sessionId,
  cwd,
  enabled,
  overlayEnabled = false,
}: Params): GitChanges {
  const [scope, setScope] = React.useState<GitScope>("uncommitted");
  const [state, setState] = React.useState<GitChangesState>({
    status: "loading",
  });
  const [branches, setBranches] =
    React.useState<workspace_fs_svc.GitBranchesView | null>(null);
  const [reloadTick, setReloadTick] = React.useState(0);

  const persistedBaseline = useChatSidebarStore(
    (s) => s.gitBaselineBySession[sessionId] ?? "",
  );
  const setGitBaseline = useChatSidebarStore((s) => s.setGitBaseline);
  const clearGitBaseline = useChatSidebarStore((s) => s.clearGitBaseline);

  // doneTick 每次本会话轮次结束自增一次；别的会话结束不会动它。
  const doneTick = useSessionStatus(sessionId)?.doneTick ?? 0;

  // 目录模式只需要「未提交」清单做叠加，不受 Git 页当前选中档位影响；只有
  // Git 页本身可见时才用它自己切换出来的档位。两者由调用方（activeTab /
  // filesMode）互斥推出，从不会同时都要「本分支」以外的另一份数据，effect
  // 因此永远只发起一次取数。
  const active = enabled || overlayEnabled;
  const effectiveScope: GitScope = enabled ? scope : "uncommitted";
  // 请求的基线只在本分支档有意义；空串让后端用它推断出的默认基线。
  const requestedBaseRef = effectiveScope === "branch" ? persistedBaseline : "";

  // 取数代际：会话 / 档位 / 基线变化后，先前在途的响应必须丢弃，否则慢的那一次
  // 会把新快照覆盖回旧数据。两条请求各记各的代际，互不撤销。
  const changesGenRef = React.useRef(0);
  const branchesGenRef = React.useRef(0);

  // 换会话或换工作目录时先把上一个仓库的快照丢掉：模式控件的角标读的是这里的
  // 文件数，而本模式不可见时下面的取数 effect 不会跑，不清就会拿旧会话的数字。
  React.useEffect(() => {
    setBranches(null);
    setState({ status: "loading" });
  }, [sessionId, cwd]);

  React.useEffect(() => {
    if (!active || cwd === "") return;
    changesGenRef.current += 1;
    const gen = changesGenRef.current;
    setState({ status: "loading" });
    WorkspaceFsGitChanges(sessionId, effectiveScope, requestedBaseRef).then(
      (view) => {
        if (changesGenRef.current !== gen) return;
        setState({ status: "loaded", view });
        // 持久化的基线已经不存在时后端会回落到默认推断，返回的 baseRef 与请求的
        // 不一致就是失效信号，把持久化值清掉（决策 9）。
        if (requestedBaseRef !== "" && view.baseRef !== requestedBaseRef) {
          clearGitBaseline(sessionId);
        }
      },
      (err: unknown) => {
        if (changesGenRef.current !== gen) return;
        setState({ status: "error", message: errorText(err) });
      },
    );
  }, [
    active,
    cwd,
    sessionId,
    effectiveScope,
    requestedBaseRef,
    doneTick,
    reloadTick,
    clearGitBaseline,
  ]);

  // 分支清单只喂基线下拉，所以只在本分支档取；它失败不影响变动列表本身，沿用
  // 「单条 git 子命令失败只让对应字段留空」的容错约定。
  React.useEffect(() => {
    if (!enabled || cwd === "" || scope !== "branch") return;
    branchesGenRef.current += 1;
    const gen = branchesGenRef.current;
    WorkspaceFsGitBranches(sessionId).then(
      (view) => {
        if (branchesGenRef.current === gen) setBranches(view);
      },
      () => {
        if (branchesGenRef.current === gen) setBranches(null);
      },
    );
  }, [enabled, cwd, sessionId, scope, doneTick, reloadTick]);

  const loaded = state.status === "loaded" ? state.view : null;
  const notARepo = loaded?.notARepo === true;

  return {
    scope,
    setScope,
    state,
    branches,
    baseRef: loaded?.baseRef ?? "",
    selectBaseline: (ref) => setGitBaseline(sessionId, ref),
    count: loaded && !notARepo ? (loaded.changes?.length ?? 0) : null,
    notARepo,
    reload: () => setReloadTick((tick) => tick + 1),
    overlayChanges: loaded && !notARepo ? (loaded.changes ?? []) : null,
  };
}
