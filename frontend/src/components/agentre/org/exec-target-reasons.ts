import type { TFunction } from "i18next";

// execTargetReasonLabel 把 chat_svc.BlockReason(后端结构化枚举字符串,见
// internal/service/chat_svc/types.go 的 BlockReason 常量 + exec_pick.go 新增的
// 三个 exec-target-* 值)翻成本地化短标签，供 R15 执行目标列表的每档状态徽标用。
//
// 刻意不直接展示后端 ExecTargetAvailabilityView.Hint 字段——那是 Go 侧写死的中文
// 提示串，不是 i18n 资源，展示它会违反「新可见 UI 文案一律走 i18n」。Reason 是稳定
// 的类型化取值，前端按值查表即可双语呈现；未知取值兜底成「未知原因」。
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

export function execTargetReasonLabel(reason: string, t: TFunction): string {
  const key = REASON_I18N_KEY[reason] ?? "unknownBackend";
  return t(`org.agent.execTargets.reasons.${key}`);
}
