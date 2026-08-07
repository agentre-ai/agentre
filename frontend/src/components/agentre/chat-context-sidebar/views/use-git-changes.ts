import * as React from "react";

import {
  WorkspaceFsGitBranches,
  WorkspaceFsGitChanges,
} from "@/../wailsjs/go/app/App";
import { useChatSidebarStore } from "@/stores/chat-sidebar-store";
import { useSessionStatus } from "@/stores/session-status-store";

import type { workspace_fs_svc } from "@/../wailsjs/go/models";

/** 未提交档回答「现在还有什么没提交」，本分支档回答「相对基线一共改了什么」。 */
export type GitScope = "uncommitted" | "branch";

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
};

/**
 * useGitChanges 管 Git 模式的取数。数据是快照，在「本模式可见且无缓存」与
 * 「当前会话轮次结束」两个时机自动重拉（决策 13）——轮次结束是「文件可能变了」
 * 的唯一强信号，所以订阅 session-status-store 的 doneTick。
 */
export function useGitChanges({ sessionId, cwd, enabled }: Params): GitChanges {
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

  // 请求的基线只在本分支档有意义；空串让后端用它推断出的默认基线。
  const requestedBaseRef = scope === "branch" ? persistedBaseline : "";

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
    if (!enabled || cwd === "") return;
    changesGenRef.current += 1;
    const gen = changesGenRef.current;
    setState({ status: "loading" });
    WorkspaceFsGitChanges(sessionId, scope, requestedBaseRef).then(
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
    enabled,
    cwd,
    sessionId,
    scope,
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
  };
}

/**
 * errorText 取后端已本地化的错误文案原样呈现 —— Wails 边界只过 Error() 字符串、
 * 没有结构化的错误码通道，而后端对「远端设备不在线」「远端 agentred 版本过旧」
 * 等各自给了明确文案，照搬即可。拿不到文案时才回落到通用提示。
 */
function errorText(err: unknown): string {
  if (err instanceof Error) return err.message;
  if (typeof err === "string") return err;
  return "";
}
