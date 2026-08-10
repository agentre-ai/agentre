import * as React from "react";
import type { TFunction } from "i18next";
import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router-dom";

import { Button } from "@/components/ui/button";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { cn } from "@/lib/utils";

import {
  ListAgentBackends,
  ListAgentExecTargetAvailability,
} from "../../../wailsjs/go/app/App";
import type { agent_backend_svc } from "../../../wailsjs/go/models";
import { EventsOn } from "../../../wailsjs/runtime/runtime";

import { DeviceTag } from "./device-tag";
import { navigateToTarget } from "./not-chattable";

// ── 数据：把 R15 的可用性判定(ListAgentExecTargetAvailability)与全量后端列表
//    (ListAgentBackends)按 agentBackendId 拼接成"改选"浮层/空会话态一行要用的候选
//    列表——两个都是既有的通用 wails 绑定(非组织架构页私有)，顺序沿用可用性接口的
//    返回顺序(即执行目标表的 sort_order)。────────────────────────────────────

export type ExecTargetCandidate = {
  agentBackendId: number;
  deviceId: string;
  deviceName: string;
  online: boolean;
  backendType: string;
  backendName: string;
  available: boolean;
  reason: string;
  hint: string;
  // projectPath: 这一档所在的机器上、这个项目的路径(本机档取 projects.path，
  // agentred 档取 project_locations 里那一行)。会话不绑项目或那台机器上没配时
  // 为空串——选机器时真正要判断的是"换过去在哪个目录干活"，比机器名更有信息量。
  projectPath: string;
};

// REMOTE_DEVICE_STATE_EVENT 是 remote_device_watcher_svc 的在线态推送通道
// (Go 侧 remote_device_watcher_svc.EventName)，use-remote-devices 订的是同一条。
const REMOTE_DEVICE_STATE_EVENT = "remote.device.state";

export function useExecTargetCandidates(agentId: number, projectId: number) {
  const [candidates, setCandidates] = React.useState<ExecTargetCandidate[]>([]);
  const [loading, setLoading] = React.useState(false);
  // loaded 区分「候选真的是空的」与「还没拉到 / 这次拉失败了」——两种情况下
  // candidates 都是 []，但只有前者能据以判断某一档确实没了。
  const [loaded, setLoaded] = React.useState(false);
  // 只有最新一次请求可以写状态：设备上下线推送与 agentId/projectId 变化都会让多次
  // reload 重叠，先发的那次晚返回时不能把新快照盖回旧的（否则用户看着「将在 X 上
  // 运行」按下回车、实际跑到了别处——正是这个订阅要防的那件事）。卸载时也把代次推
  // 进一格，在飞的请求回来后不再写已卸载组件的状态。
  const reqRef = React.useRef(0);

  const reload = React.useCallback(async () => {
    const req = ++reqRef.current;
    if (!agentId) {
      setCandidates([]);
      setLoaded(false);
      return;
    }
    setLoading(true);
    try {
      const [availability, backends] = await Promise.all([
        ListAgentExecTargetAvailability(agentId, projectId),
        ListAgentBackends(),
      ]);
      if (req !== reqRef.current) return;
      const backendById = new Map<number, agent_backend_svc.BackendItem>();
      for (const b of backends?.items ?? []) backendById.set(b.id, b);
      setCandidates(
        (availability ?? []).map((a) => {
          const b = backendById.get(a.agentBackendId);
          return {
            agentBackendId: a.agentBackendId,
            deviceId: b?.deviceId ?? "",
            deviceName: b?.deviceName ?? "",
            online: b?.online ?? false,
            backendType: b?.type ?? "",
            backendName: b?.name ?? "",
            available: a.available,
            reason: a.reason,
            hint: a.hint,
            projectPath: a.projectPath,
          };
        }),
      );
      setLoaded(true);
    } catch (e) {
      if (req !== reqRef.current) return;
      console.error("[chat] exec target candidates load failed", e);
      setCandidates([]);
      setLoaded(false);
    } finally {
      if (req === reqRef.current) setLoading(false);
    }
    // projectId 变化也要重新拉——"这台机器上有没有配这个项目的路径"这一项判据
    // 只在绑定项目时生效。
  }, [agentId, projectId]);

  React.useEffect(() => {
    void reload();
  }, [reload]);

  React.useEffect(
    () => () => {
      reqRef.current++;
    },
    [],
  );

  // 选中结果在起轮前是活的(R15a)：空会话态期间可用性变了就重新算，否则用户看着
  // "将在 X 上运行"按下回车、实际跑到了别处。信号取既有的 remote.device.state
  // 推送(agentred 上下线正是可用性判据里唯一会自己翻转的那一项)，不新造轮询；
  // 这个 hook 只在空会话态那一行里挂载，订阅随它一起卸载。
  React.useEffect(() => {
    const off = EventsOn(REMOTE_DEVICE_STATE_EVENT, () => {
      void reload();
    });
    return () => off?.();
  }, [reload]);

  return { candidates, loading, loaded, reload };
}

const REASON_I18N_KEY: Record<string, string> = {
  "no-backend": "orphanBackend",
  "backend-requires-provider": "backendRequiresProvider",
  "provider-inactive": "providerInactive",
  "remote-provider-missing": "remoteProviderMissing",
  "gateway-not-running": "gatewayNotRunning",
  "remote-openclaw-unavailable": "remoteOpenclawUnavailable",
  "unknown-backend": "unknownBackend",
  "exec-target-unpaired": "unpaired",
  "exec-target-offline": "offline",
  "exec-target-project-path-missing": "projectPathMissing",
};

function reasonLabel(reason: string, t: TFunction): string {
  const key = REASON_I18N_KEY[reason] ?? "unknownBackend";
  return t(`chatPanel.execTarget.reasons.${key}`);
}

function candidateLabel(c: ExecTargetCandidate, t: TFunction): string {
  if (!c.deviceId) return t("deviceTag.local");
  return c.deviceName || c.deviceId;
}

// ── 空会话态「将在 X 上运行 · 改选」（R15a）。只有一个（可用性判定成功返回长度<=1）
//    候选时整行不出现，视觉上等价于今天什么都不显示。────────────────────────────

export type NewSessionExecTargetLineProps = {
  agentId: number;
  agentName: string;
  projectId: number;
  overrideBackendId: number | null;
  onOverride: (agentBackendId: number | null) => void;
};

export function NewSessionExecTargetLine(props: NewSessionExecTargetLineProps) {
  const { t } = useTranslation();
  const { candidates, loaded } = useExecTargetCandidates(
    props.agentId,
    props.projectId,
  );

  const defaultCandidate = candidates.find((c) => c.available);
  const overrideCandidate = props.overrideBackendId
    ? candidates.find(
        (c) => c.agentBackendId === props.overrideBackendId && c.available,
      )
    : undefined;
  const effective = overrideCandidate ?? defaultCandidate;
  // 手动指定的档一旦不再可用，回落到自动挑选——不留一个死指向。这个 effect 必须
  // 在任何 early return 之前调用（Rules of Hooks），所以放在整个组件的顶部。
  // 只在候选列表真的成功加载过之后才判：还没拉到 / 这次拉失败时 candidates 同样是
  // 空的，此时清掉手动指定，会把用户刚选的机器悄悄换回自动挑选的那一档。
  React.useEffect(() => {
    if (!loaded) return;
    if (props.overrideBackendId && !overrideCandidate) {
      props.onOverride(null);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [loaded, props.overrideBackendId, overrideCandidate]);

  if (candidates.length <= 1) return null;

  if (!effective) {
    return (
      <div
        data-testid="new-session-exec-target-all-unavailable"
        className="mt-2 flex max-w-md gap-2 rounded-md border border-destructive bg-destructive-soft px-3 py-2.5 text-left"
      >
        <div className="flex min-w-0 flex-col gap-1">
          <span className="text-xs font-semibold">
            {t("chatPanel.execTarget.allUnavailableTitle", {
              name: props.agentName,
            })}
          </span>
          <span className="text-2xs leading-relaxed text-muted-foreground">
            {candidates.map((c, i) => (
              <React.Fragment key={c.agentBackendId}>
                {i + 1} · {candidateLabel(c, t)} —— {reasonLabel(c.reason, t)}
                <br />
              </React.Fragment>
            ))}
          </span>
        </div>
      </div>
    );
  }

  // "掉档"：不是手动指定、而是自动挑选跳过了排最前的档，唯一需要加重的一处。
  const dropped =
    !overrideCandidate &&
    candidates.length > 0 &&
    effective.agentBackendId !== candidates[0].agentBackendId;

  return (
    <div
      data-testid="new-session-exec-target-line"
      className={cn(
        "mt-2 flex flex-wrap items-center justify-center gap-1.5 rounded-md px-2 py-1",
        dropped && "bg-status-waiting-bg",
      )}
    >
      {dropped ? (
        <span className="text-2xs text-muted-foreground">
          {t("chatPanel.execTarget.droppedPrefix", {
            name: candidateLabel(candidates[0], t),
          })}
        </span>
      ) : (
        <span className="text-2xs text-muted-foreground">
          {t("chatPanel.execTarget.willRunPrefix")}
        </span>
      )}
      {/* "会话跑在哪台机器"复用既有的 DeviceTag，不引入新组件：deviceId === "" 那
          一支本来就渲染"本机"。掉档的加重由整行底色 + 措辞 + 原因承担，chip 的配色
          仍按机器归属走，与命令面板/聊天头/成员页保持同一套视觉。 */}
      <DeviceTag
        deviceId={effective.deviceId}
        deviceName={effective.deviceName || effective.deviceId}
        online={effective.online}
      />
      <span className="text-2xs text-muted-foreground">
        {t("chatPanel.execTarget.willRunSuffix")}
      </span>
      <span className="text-2xs text-muted-foreground">·</span>
      <Popover>
        <PopoverTrigger asChild>
          <button
            type="button"
            className="text-2xs text-muted-foreground underline-offset-2 hover:underline"
          >
            {t("chatPanel.execTarget.reselect")}
          </button>
        </PopoverTrigger>
        <PopoverContent align="center" className="w-72 p-0">
          <ExecTargetReselectPopover
            agentId={props.agentId}
            candidates={candidates}
            selectedId={effective.agentBackendId}
            onPick={props.onOverride}
          />
        </PopoverContent>
      </Popover>
      {dropped && candidates[0] && (
        <p className="w-full text-center text-2xs text-subtle-foreground">
          {reasonLabel(candidates[0].reason, t)}
        </p>
      )}
    </div>
  );
}

// ExecTargetReselectPopover 只在弹层实际打开时挂载（Radix Popover 默认关闭态不
// 渲染 content），useNavigate 因此放在这里而不是 NewSessionExecTargetLine
// 顶层——后者在任何新会话态都会渲染，很多既有测试没有包 <MemoryRouter>，
// 提前拿 navigate 会在这些测试里直接抛错。
function ExecTargetReselectPopover(props: {
  agentId: number;
  candidates: ExecTargetCandidate[];
  selectedId: number;
  onPick: (agentBackendId: number) => void;
}) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const onGoAdjust = () => {
    navigateToTarget(navigate, "org-agent:<id>", props.agentId);
  };
  return (
    <div>
      <div className="flex items-baseline justify-between gap-2 border-b border-border px-3 py-2">
        <span className="text-2xs font-semibold">
          {t("chatPanel.execTarget.pickerTitle")}
        </span>
        <span className="text-2xs text-subtle-foreground">
          {t("chatPanel.execTarget.pickerScope")}
        </span>
      </div>
      {props.candidates.map((c, i) => (
        <button
          key={c.agentBackendId}
          type="button"
          disabled={!c.available}
          onClick={() => props.onPick(c.agentBackendId)}
          className={cn(
            "flex w-full items-center gap-2 border-b border-border px-3 py-2 text-left last:border-b-0",
            c.available ? "hover:bg-accent" : "cursor-not-allowed opacity-60",
          )}
        >
          <span
            className={cn(
              "size-3.5 shrink-0 rounded-full border-[1.5px]",
              c.agentBackendId === props.selectedId
                ? "border-primary shadow-[inset_0_0_0_3px_var(--primary)]"
                : "border-border-strong",
            )}
            aria-hidden="true"
          />
          <span className="inline-flex size-5 shrink-0 items-center justify-center rounded-sm bg-secondary font-mono text-2xs text-muted-foreground">
            {i + 1}
          </span>
          <div className="flex min-w-0 flex-1 flex-col">
            <span className="text-xs font-semibold">
              {candidateLabel(c, t)}
            </span>
            {/* 那台机器上这个项目的路径：序号解释"为什么默认是它"，路径回答
                "换过去在哪个目录干活"。没绑项目/那台机器上没配路径时整行不出现，
                不留一行空的。 */}
            {c.projectPath ? (
              <span
                data-testid="exec-target-project-path"
                title={c.projectPath}
                className="truncate font-mono text-2xs text-subtle-foreground"
              >
                {c.projectPath}
              </span>
            ) : null}
            {!c.available && (
              <span className="font-mono text-2xs text-muted-foreground">
                {reasonLabel(c.reason, t)}
              </span>
            )}
          </div>
        </button>
      ))}
      <div className="flex items-center justify-between gap-2 bg-muted px-3 py-2">
        <span className="text-2xs text-muted-foreground">
          {t("chatPanel.execTarget.stickyHint")}
        </span>
        <button
          type="button"
          onClick={onGoAdjust}
          className="text-2xs text-primary-text underline-offset-2 hover:underline"
        >
          {t("chatPanel.execTarget.goAdjust")}
        </button>
      </div>
    </div>
  );
}

// ── 聊天头「会话所在机器离线」提示（R15b）：会话钉住的那一档在远端且当前离线时，
//    给一条走得通的路——不会改派，只能等它上线或用同样的开头新建一个会话。──────

export type SessionOfflineBannerProps = {
  deviceName: string;
  onCreateNewSession: () => void;
};

export function SessionOfflineBanner(props: SessionOfflineBannerProps) {
  const { t } = useTranslation();
  return (
    <div
      data-testid="session-offline-banner"
      className="mx-4 mb-2 flex flex-col gap-2 rounded-md border border-destructive bg-destructive-soft px-3 py-2.5"
    >
      <div className="flex gap-2">
        <div className="flex min-w-0 flex-col gap-1">
          <span className="text-xs font-semibold">
            {t("chatPanel.execTarget.offlineTitle", {
              name: props.deviceName,
            })}
          </span>
          <span className="text-2xs leading-relaxed text-muted-foreground">
            {t("chatPanel.execTarget.offlineHint")}
          </span>
        </div>
      </div>
      <Button
        type="button"
        variant="outline"
        size="sm"
        className="w-fit"
        onClick={props.onCreateNewSession}
      >
        {t("chatPanel.execTarget.offlineNewSession")}
      </Button>
    </div>
  );
}
