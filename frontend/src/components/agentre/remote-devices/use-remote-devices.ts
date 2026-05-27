import { useCallback, useEffect, useState } from "react";

import {
  RemoteDeviceList,
  RemoteDeviceAdd,
  RemoteDeviceRemove,
  RemoteDeviceUpdateTLS,
  RemoteDeviceRefresh,
  RemoteDeviceRename,
} from "../../../../wailsjs/go/app/App";
import { EventsOn } from "../../../../wailsjs/runtime/runtime";
import type { remote_device_svc } from "../../../../wailsjs/go/models";

export type DeviceView = remote_device_svc.DeviceView;
export type AddRequest = remote_device_svc.AddRequest;

type StateEvent = {
  id: number;
  name: string;
  online: boolean;
  lastSeenAt: number;
  lastError: string;
};

const EVENT_NAME = "remote.device.state";

export function useRemoteDevices() {
  const [devices, setDevices] = useState<DeviceView[]>([]);
  const [loading, setLoading] = useState(true);

  const reload = useCallback(async () => {
    const list = await RemoteDeviceList();
    setDevices(list ?? []);
  }, []);

  useEffect(() => {
    void reload().finally(() => setLoading(false));
  }, [reload]);

  useEffect(() => {
    const off = EventsOn(EVENT_NAME, (payload: unknown) => {
      const ev = payload as StateEvent;
      setDevices((prev) =>
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
      setDevices((prev) => prev.map((x) => (x.id === id ? (v ?? x) : x)));
    },
  };
}
