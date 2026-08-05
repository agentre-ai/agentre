// frontend/src/components/agentre/remote-devices/device-row.test.tsx
import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { DeviceRow } from "./device-row";
import type { DeviceView } from "./use-remote-devices";

const baseDevice: DeviceView = {
  id: 1,
  name: "linux-srv",
  url: "ws://192.168.1.100:7456/rpc",
  daemonFingerprint: "fp",
  instanceUUID: "u",
  tlsMode: "default",
  tlsCertPEM: "",
  pairedAt: 1,
  lastSeenAt: 0,
  lastError: "",
  online: false,
  daemonOutdated: false,
};

describe("DeviceRow", () => {
  it("renders name + URL", () => {
    render(
      <DeviceRow
        device={baseDevice}
        now={1_000_000}
        onRefresh={() => {}}
        onRename={() => {}}
        onEditTLS={() => {}}
        onRemove={() => {}}
      />,
    );
    expect(screen.getByText("linux-srv")).toBeInTheDocument();
    expect(screen.getByText(/192\.168\.1\.100/)).toBeInTheDocument();
  });
  it("shows OS 默认 badge for default mode", () => {
    render(
      <DeviceRow
        device={baseDevice}
        now={1_000_000}
        onRefresh={() => {}}
        onRename={() => {}}
        onEditTLS={() => {}}
        onRemove={() => {}}
      />,
    );
    expect(screen.getByText("OS Default")).toBeInTheDocument();
  });
  it("renders 尚未连接 when LastSeenAt = 0", () => {
    render(
      <DeviceRow
        device={baseDevice}
        now={1_000_000}
        onRefresh={() => {}}
        onRename={() => {}}
        onEditTLS={() => {}}
        onRemove={() => {}}
      />,
    );
    expect(screen.getByText(/Never connected/)).toBeInTheDocument();
  });
  it("renders friendly error for tofu_mismatch in destructive style", () => {
    const d = { ...baseDevice, lastError: "tofu_mismatch" };
    render(
      <DeviceRow
        device={d}
        now={1_000_000}
        onRefresh={() => {}}
        onRename={() => {}}
        onEditTLS={() => {}}
        onRemove={() => {}}
      />,
    );
    expect(
      screen.getByText(/identity fingerprint changed/),
    ).toBeInTheDocument();
  });
  // R18：daemon 版本过旧时，说明落在这台设备自己那一行 —— 它是设备属性，
  // 不是会话事件，也不是浮在聊天上的横幅。
  it("explains an outdated daemon on the device row", () => {
    const d = { ...baseDevice, daemonOutdated: true };
    render(
      <DeviceRow
        device={d}
        now={1_000_000}
        onRefresh={() => {}}
        onRename={() => {}}
        onEditTLS={() => {}}
        onRemove={() => {}}
      />,
    );
    expect(screen.getByText(/Outdated daemon/)).toBeInTheDocument();
  });

  it("says nothing about the version when the daemon is current", () => {
    render(
      <DeviceRow
        device={baseDevice}
        now={1_000_000}
        onRefresh={() => {}}
        onRename={() => {}}
        onEditTLS={() => {}}
        onRemove={() => {}}
      />,
    );
    expect(screen.queryByText(/Outdated daemon/)).not.toBeInTheDocument();
  });

  it("fires onRemove from action menu", async () => {
    const user = userEvent.setup();
    const onRemove = vi.fn();
    render(
      <DeviceRow
        device={baseDevice}
        now={1_000_000}
        onRefresh={() => {}}
        onRename={() => {}}
        onEditTLS={() => {}}
        onRemove={onRemove}
      />,
    );
    await user.click(screen.getByLabelText("More actions"));
    await user.click(await screen.findByText("Unpair"));
    expect(onRemove).toHaveBeenCalled();
  });
});
