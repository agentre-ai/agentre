import * as React from "react";

import { GetSessionCapabilities } from "@/../wailsjs/go/app/App";

import { Capabilities, type PermissionModeMeta } from "./types";

// useSessionCapabilities 拉 chat session 对应 runtime 的能力矩阵 + permission
// mode metadata。sessionId 改变时重新拉;<=0 时返回 null(没选会话)。
//
// 业务用法:
//   const { caps } = useSessionCapabilities(sessionId);
//   if (caps?.has("abort")) { ... }
//   <Button disabled={!caps?.has("abort")}>停止</Button>
//   <PermissionModePill modes={caps?.permissionModeMeta.order ?? []} ... />
export function useSessionCapabilities(sessionId: number | undefined | null) {
  const [caps, setCaps] = React.useState<Capabilities | null>(null);

  React.useEffect(() => {
    if (!sessionId || sessionId <= 0) {
      setCaps(null);
      return;
    }
    let cancelled = false;
    void GetSessionCapabilities({ sessionId } as never)
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
          console.error("[capability] load failed", e);
          setCaps(null);
        }
      });
    return () => {
      cancelled = true;
    };
  }, [sessionId]);

  return { caps };
}
