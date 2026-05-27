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
    expect(screen.getByText("OS 默认")).toBeInTheDocument();
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
    expect(screen.getByText(/尚未连接/)).toBeInTheDocument();
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
    expect(screen.getByText(/身份指纹/)).toBeInTheDocument();
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
    await user.click(screen.getByLabelText("更多操作"));
    await user.click(await screen.findByText("解除配对"));
    expect(onRemove).toHaveBeenCalled();
  });
});
