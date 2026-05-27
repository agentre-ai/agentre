import * as React from "react";

import { GetBackendCapabilities } from "@/../wailsjs/go/app/App";

import { Capabilities, type PermissionModeMeta } from "./types";

// useBackendCapabilities 按 backend type 拉 runtime 能力矩阵 + permission mode
// metadata。给「新对话还没建 session」场景用 — 已有 session 走
// useSessionCapabilities,语义一致(响应形状复用)。
//
// 业务用法:
//   const { caps } = useBackendCapabilities(newSessionAgent?.backendType);
//   if (caps?.has("set_permission_mode")) { /* 渲染 pill */ }
export function useBackendCapabilities(backendType: string | undefined | null) {
  const [caps, setCaps] = React.useState<Capabilities | null>(null);

  React.useEffect(() => {
    if (!backendType) {
      setCaps(null);
      return;
    }
    let cancelled = false;
    void GetBackendCapabilities({ backendType } as never)
      .then((resp) => {
        if (cancelled) return;
        const meta: PermissionModeMeta = {
          allowedModes: resp?.permissionModeMeta?.allowedModes ?? [],
          defaultMode: resp?.permissionModeMeta?.defaultMode ?? "",
          switchableDuringTurn:
            resp?.permissionModeMeta?.switchableDuringTurn ?? false,
          order: resp?.permissionModeMeta?.order ?? [],
        };
        setCaps(new Capabilities(new Set(resp?.capabilities ?? []), meta));
      })
      .catch((e: unknown) => {
        if (!cancelled) {
          console.error("[capability] backend caps load failed", e);
          setCaps(null);
        }
      });
    return () => {
      cancelled = true;
    };
  }, [backendType]);

  return { caps };
}
