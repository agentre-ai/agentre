// frontend/src/components/agentre/remote-devices/device-row.test.tsx
import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { DeviceRow } from "./device-row";
import type { DeviceRowModel } from "./use-remote-devices";

const baseDevice: DeviceRowModel = {
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
  account: undefined,
  paths: [{ kind: "lan", state: "dead" }],
  unclaimed: false,
  viaRelay: false,
};

function renderRow(device: DeviceRowModel) {
  return render(
    <DeviceRow
      device={device}
      now={1_000_000}
      onRefresh={() => {}}
      onRename={() => {}}
      onEditTLS={() => {}}
      onRemove={() => {}}
    />,
  );
}

describe("DeviceRow", () => {
  it("renders name + URL", () => {
    renderRow(baseDevice);
    expect(screen.getByText("linux-srv")).toBeInTheDocument();
    expect(screen.getByText(/192\.168\.1\.100/)).toBeInTheDocument();
  });
  it("shows OS 默认 badge for default mode", () => {
    renderRow(baseDevice);
    expect(screen.getByText("OS Default")).toBeInTheDocument();
  });
  it("renders 尚未连接 when LastSeenAt = 0", () => {
    renderRow(baseDevice);
    expect(screen.getByText(/Never connected/)).toBeInTheDocument();
  });
  it("renders friendly error for tofu_mismatch in destructive style", () => {
    const d = { ...baseDevice, lastError: "tofu_mismatch" };
    renderRow(d);
    expect(
      screen.getByText(/identity fingerprint changed/),
    ).toBeInTheDocument();
  });
  // R18：daemon 版本过旧时，说明落在这台设备自己那一行 —— 它是设备属性，
  // 不是会话事件，也不是浮在聊天上的横幅。
  it("explains an outdated daemon on the device row", () => {
    const d = { ...baseDevice, daemonOutdated: true };
    renderRow(d);
    expect(screen.getByText(/Outdated daemon/)).toBeInTheDocument();
  });

  it("says nothing about the version when the daemon is current", () => {
    renderRow(baseDevice);
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

  // ── R15 可达路径 chips ─────────────────────────────────────────────────────
  it("renders the in-use LAN path highlighted and the relay path available", () => {
    const d: DeviceRowModel = {
      ...baseDevice,
      online: true,
      account: {
        ID: 10,
        Name: "linux-srv",
        Kind: "agentred",
        Platform: "linux",
        Version: "0.3.0",
        Fingerprint: "fp",
        LastSeenAt: 1,
        Status: 1,
        Online: true,
        IsThisDevice: false,
      },
      paths: [
        { kind: "lan", state: "in-use" },
        { kind: "relay", state: "available" },
      ],
    };
    renderRow(d);
    const lan = screen.getByLabelText("Direct · In use");
    expect(lan).toBeInTheDocument();
    expect(screen.getByText("Relay")).toBeInTheDocument();
    // 在用路径高亮:主色文本。
    expect(lan.className).toMatch(/font-semibold/);
  });

  it("labels an unreachable path with text, not styling alone", () => {
    const d: DeviceRowModel = {
      ...baseDevice,
      paths: [{ kind: "lan", state: "dead" }],
    };
    renderRow(d);
    // 失效态除样式(划线/淡出)外另有文字表达。
    expect(screen.getByText("Direct · Unreachable")).toBeInTheDocument();
  });

  it("shows 经中转 as the address when the relay path is in use", () => {
    const d: DeviceRowModel = {
      ...baseDevice,
      account: {
        ID: 10,
        Name: "linux-srv",
        Kind: "agentred",
        Platform: "linux",
        Version: "0.3.0",
        Fingerprint: "fp",
        LastSeenAt: 1,
        Status: 1,
        Online: true,
        IsThisDevice: false,
      },
      viaRelay: true,
      paths: [
        { kind: "lan", state: "dead" },
        { kind: "relay", state: "in-use" },
      ],
    };
    renderRow(d);
    expect(screen.getByText(/Via relay/)).toBeInTheDocument();
    expect(screen.queryByText(/192\.168\.1\.100/)).not.toBeInTheDocument();
  });

  // ── 未认领标注 + 认领动作 ──────────────────────────────────────────────────
  it("marks an unclaimed device and explains the consequence", () => {
    const d: DeviceRowModel = { ...baseDevice, unclaimed: true };
    renderRow(d);
    expect(screen.getByText("Unclaimed")).toBeInTheDocument();
    expect(screen.getByText(/other devices can't see it/i)).toBeInTheDocument();
  });

  it("does not show the unclaimed marking on a claimed device", () => {
    renderRow(baseDevice);
    expect(screen.queryByText("Unclaimed")).not.toBeInTheDocument();
  });

  it("reveals claim instructions when the claim action is pressed", async () => {
    const user = userEvent.setup();
    const d: DeviceRowModel = { ...baseDevice, unclaimed: true };
    renderRow(d);
    expect(screen.queryByText(/agentred login/)).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Claim to account" }));
    expect(screen.getByText(/agentred login/)).toBeInTheDocument();
  });
});
