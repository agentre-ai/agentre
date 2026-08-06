import { useCallback, useEffect, useMemo, useState } from "react";

import {
  RemoteDeviceList,
  RemoteDeviceAdd,
  RemoteDeviceRemove,
  RemoteDeviceUpdateTLS,
  RemoteDeviceRefresh,
  RemoteDeviceRename,
  ServerListDevices,
} from "../../../../wailsjs/go/app/App";
import { EventsOn } from "../../../../wailsjs/runtime/runtime";
import type {
  remote_device_svc,
  server_svc,
} from "../../../../wailsjs/go/models";

export type DeviceView = remote_device_svc.DeviceView;
export type AddRequest = remote_device_svc.AddRequest;

// ── R15 可达路径 ─────────────────────────────────────────────────────────────
// 一行一台机器:LAN 配对与账号归属两来源按设备指纹合并。该行呈现「可达路径」
// 而非凭据来源 —— 直连(LAN)与中转(账号)各自是一条路径。

export type PathKind = "lan" | "relay";
/** 路径状态:在用(高亮)/ 可用(存在但非当前在用)/ 失效(试过但不可达)。 */
export type PathState = "in-use" | "available" | "dead";

export type DevicePath = {
  kind: PathKind;
  state: PathState;
};

/** 账号来源。known=false 表示桌面未登录或没拉到清单 —— 此时无法判定「未认领」。 */
export type AccountSource = {
  known: boolean;
  devices: server_svc.Device[];
};

/** 合并后的一行设备:LAN 视图 + 可选账号设备 + 可达路径。 */
export type DeviceRowModel = DeviceView & {
  /** 账号来源的设备(指纹匹配);undefined = 仅 LAN 来源。 */
  account?: server_svc.Device;
  /** 可达路径 chips。 */
  paths: DevicePath[];
  /** 未认领:仅本机配对、账号清单里没有这台机器的指纹(且清单已知)。 */
  unclaimed: boolean;
  /** true → 主地址位显示「经中转」而非 LAN url(中转路径在用)。 */
  viaRelay: boolean;
};

/** hub devices.status 的 ACTIVE 值。 */
const RELAY_ACTIVE_STATUS = 1;

export function mergeDeviceSources(
  lan: DeviceView[],
  account: AccountSource,
): DeviceRowModel[] {
  const accountByFp = new Map<string, server_svc.Device>();
  for (const d of account.devices) {
    if (d.Fingerprint && !accountByFp.has(d.Fingerprint)) {
      accountByFp.set(d.Fingerprint, d);
    }
  }
  return lan.map((d) => {
    const acc = accountByFp.get(d.daemonFingerprint);
    const online = d.online;
    const paths: DevicePath[] = [
      { kind: "lan", state: online ? "in-use" : "dead" },
    ];
    let relayInUse = false;
    if (acc) {
      const active = acc.Status === RELAY_ACTIVE_STATUS;
      relayInUse = active && !online;
      paths.push({
        kind: "relay",
        state: active ? (online ? "available" : "in-use") : "dead",
      });
    }
    return {
      ...d,
      account: acc,
      paths,
      unclaimed: account.known && acc == null,
      viaRelay: relayInUse,
    };
  });
}

type StateEvent = {
  id: number;
  name: string;
  online: boolean;
  lastSeenAt: number;
  lastError: string;
};

const EVENT_NAME = "remote.device.state";

export function useRemoteDevices() {
  const [lanDevices, setLanDevices] = useState<DeviceView[]>([]);
  const [account, setAccount] = useState<AccountSource>({
    known: false,
    devices: [],
  });
  const [loading, setLoading] = useState(true);

  // 合并后的行 = LAN 来源 + 账号来源。在线态事件只改 LAN 来源,merge 重算路径。
  const devices = useMemo(
    () => mergeDeviceSources(lanDevices, account),
    [lanDevices, account],
  );

  const reload = useCallback(async () => {
    const list = (await RemoteDeviceList()) ?? [];
    setLanDevices(list);
    // 账号来源:未登录时 ServerListDevices 本地即返回 ErrNotLoggedIn(known=false,
    // 不判未认领);已登录但拉取失败(服务器离线)同样 known=false,不把每台都误标未认领。
    let acc: AccountSource;
    try {
      const accountList = (await ServerListDevices()) ?? [];
      acc = { known: true, devices: accountList };
    } catch {
      acc = { known: false, devices: [] };
    }
    setAccount(acc);
  }, []);

  useEffect(() => {
    void reload().finally(() => setLoading(false));
  }, [reload]);

  useEffect(() => {
    const off = EventsOn(EVENT_NAME, (payload: unknown) => {
      const ev = payload as StateEvent;
      setLanDevices((prev) =>
        prev.map((d) =>
          d.id === ev.id
            ? {
                ...d,
                name: ev.name || d.name,
                online: ev.online,
                lastSeenAt: ev.lastSeenAt,
                lastError: ev.lastError,
              }
            : d,
        ),
      );
    });
    const onFocus = () => {
      void reload();
    };
    window.addEventListener("focus", onFocus);
    return () => {
      off?.();
      window.removeEventListener("focus", onFocus);
    };
  }, [reload]);

  return {
    devices,
    loading,
    reload,
    add: async (req: AddRequest) => {
      await RemoteDeviceAdd(req);
      await reload();
    },
    remove: async (id: number) => {
      await RemoteDeviceRemove(id);
      await reload();
    },
    updateTLS: async (id: number, mode: string, pem: string) => {
      await RemoteDeviceUpdateTLS(id, mode, pem);
      await reload();
    },
    rename: async (id: number, name: string) => {
      await RemoteDeviceRename(id, name);
      await reload();
    },
    refresh: async (id: number) => {
      const v = await RemoteDeviceRefresh(id);
      setLanDevices((prev) => prev.map((x) => (x.id === id ? (v ?? x) : x)));
    },
  };
}
