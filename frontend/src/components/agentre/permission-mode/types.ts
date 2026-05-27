// Permission Mode = 后端 runtime 暴露的工具许可门级别(claudecode 4 档 / codex
// 2 档 / builtin 0 档)。集合从 Capabilities.PermissionModeMeta 动态读;本文件
// 保留 UI 元数据 + 通用 helper(归一化 / 循环切换 / claudecode bypass-lockout 判定)。
//
// 加新 backend 时:后端注册到 capability.PermissionModeMeta,前端无需改动;若新
// 出现的 mode 字面量不在 PERMISSION_MODE_META_UI,会走兜底渲染(label = key,
// 灰色 pill,无图标)。

// PermissionMode 是动态字符串(由 caps.permissionModeMeta.allowedModes 决定);
// 保留 type alias 仅为可读性。
export type PermissionMode = string;

export interface PermissionModeMetaUI {
  key: PermissionMode;
  label: string;
  iconName:
    | "circle-question"
    | "shield-check"
    | "clipboard-list"
    | "shield-off";
  desc: string;
  /** Tailwind tone classes — 控件颜色按等级递增警示。 */
  pillClass: string;
  iconClass: string;
}

// PERMISSION_MODE_META_UI 是已知 mode 的展示元数据查找表。新 mode 没在表里时,
// 调用方走兜底渲染(fallbackPermissionModeMetaUI 或自行 fallback)。
export const PERMISSION_MODE_META_UI: Record<string, PermissionModeMetaUI> = {
  default: {
    key: "default",
    label: "Default",
    iconName: "circle-question",
    desc: "工具调用前逐次询问，会触发 can_use_tool",
    pillClass: "bg-muted text-foreground border-border",
    iconClass: "text-muted-foreground",
  },
  acceptEdits: {
    key: "acceptEdits",
    label: "Accept Edits",
    iconName: "shield-check",
    desc: "自动接受文件编辑，其他工具仍走询问",
    pillClass: "bg-primary-soft text-primary-text border-primary-text/60",
    iconClass: "text-primary-text",
  },
  plan: {
    key: "plan",
    label: "Plan",
    iconName: "clipboard-list",
    desc: "只读分析，不执行任何写入或副作用工具",
    pillClass:
      "bg-status-waiting-bg text-status-waiting border-status-waiting/60",
    iconClass: "text-status-waiting",
  },
  bypassPermissions: {
    key: "bypassPermissions",
    label: "Bypass",
    iconName: "shield-off",
    desc: "跳过全部许可门；首发选中后 CLI 以 --permission-mode bypassPermissions 启动",
    pillClass: "bg-destructive-soft text-destructive border-destructive/60",
    iconClass: "text-destructive",
  },
};

// 未知 mode 的兜底元数据(label = key,通用 pill 样式)。
export function fallbackPermissionModeMetaUI(
  key: PermissionMode,
): PermissionModeMetaUI {
  return {
    key,
    label: key || "Unknown",
    iconName: "circle-question",
    desc: "",
    pillClass: "bg-muted text-foreground border-border",
    iconClass: "text-muted-foreground",
  };
}

// nextPermissionMode 按给定 order 循环切换。order 来自
// caps.permissionModeMeta.order(runtime 决定的 Shift+Tab 顺序)。order 为空时
// 返回 current(无可切换 mode)。
export function nextPermissionMode(
  current: PermissionMode,
  order: readonly string[],
): PermissionMode {
  if (order.length === 0) return current;
  const idx = order.indexOf(current);
  if (idx < 0) return order[0];
  return order[(idx + 1) % order.length];
}

// normalizePermissionMode 把任意 raw 值归一到 allowedModes 集合内;
//   - raw 在 allowedModes 中 → 返回 raw
//   - raw 缺失/非法,但 backendDefault 在 allowedModes 中 → 返回 backendDefault
//   - 否则返回 defaultMode(runtime 报的默认)
//
// 注:不做"启动后能否切到 bypass"的二次禁用(那是 permissionModeDisabledReason+ctx
// 的事),这里只校验 raw 在集合中。
export function normalizePermissionMode(
  raw: PermissionMode | null | undefined,
  allowedModes: readonly string[],
  defaultMode: PermissionMode,
  backendDefault?: PermissionMode | null,
): PermissionMode {
  if (typeof raw === "string" && raw.length > 0 && allowedModes.includes(raw)) {
    return raw;
  }
  if (
    typeof backendDefault === "string" &&
    backendDefault.length > 0 &&
    allowedModes.includes(backendDefault)
  ) {
    return backendDefault;
  }
  return defaultMode;
}

// permissionModeDisabledReason 判定某个 mode 在当前会话状态下是否被禁用,返
// 原因文案(null = 可用)。
//
// 唯一规则:**claudecode** 后端、会话已经启动(hasActiveSession=true)、且当前
// 会话不是以 bypass 启动的(permissionModeAtLaunch !== 'bypassPermissions')时,
// bypass 档置灰 —— Claude CLI 拒绝运行时进 bypass,必须新建会话首发选 bypass
// 才能解锁双向切换。
//
// 这条规则与具体 runtime CLI 行为绑定,因此保留 runtimeKey 参数(string,而非
// 枚举,避免硬绑死 backend 类型)。其它 runtime(codex/builtin/remote)不会触发
// 这条规则,统一返 null。
export interface PermissionModeDisableCtx {
  hasActiveSession?: boolean;
  permissionModeAtLaunch?: PermissionMode | null;
}

export function permissionModeDisabledReason(
  mode: PermissionMode,
  runtimeKey: string | null | undefined,
  ctx?: PermissionModeDisableCtx,
): string | null {
  if (runtimeKey !== "claudecode" || mode !== "bypassPermissions") {
    return null;
  }
  const active = ctx?.hasActiveSession === true;
  if (!active) {
    return null;
  }
  if (ctx?.permissionModeAtLaunch === "bypassPermissions") {
    return null;
  }
  return "该会话启动时未选择 bypassPermissions；Claude CLI 不允许中途进入 bypass。新建会话时选 bypass 即可解锁双向切换。";
}
