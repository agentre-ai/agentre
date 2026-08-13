import { describe, expect, it } from "vitest";

import {
  deviceSelectValue,
  pairedDeviceSelectValue,
  persistedDeviceIdForSelection,
  resolveExecutionDevice,
} from "../device-identity";

const devices = [{ id: 7, daemonFingerprint: "sha256:remote-daemon" }];

const localFingerprint = "sha256:local-desktop";
const localValue = "__local__";

describe("execution device identity", () => {
  it("Given the local canonical fingerprint, When resolving an execution target, Then it remains local without a paired row", () => {
    expect(
      resolveExecutionDevice(localFingerprint, localFingerprint, devices),
    ).toEqual({ local: true, remote: false, pairedDeviceId: 0 });
    expect(
      deviceSelectValue(localFingerprint, localFingerprint, localValue),
    ).toBe(localValue);
    expect(
      persistedDeviceIdForSelection(localValue, localValue, localFingerprint),
    ).toBe(localFingerprint);
  });

  it("Given a paired remote canonical fingerprint, When resolving local RPC routing, Then it yields the paired row while preserving the fingerprint as the select value", () => {
    expect(
      resolveExecutionDevice("sha256:remote-daemon", localFingerprint, devices),
    ).toEqual({ local: false, remote: true, pairedDeviceId: 7 });
    expect(pairedDeviceSelectValue(devices[0])).toBe("sha256:remote-daemon");
  });

  it("Given a legacy numeric device id, When resolving it, Then the paired-row route remains compatible", () => {
    expect(resolveExecutionDevice("7", localFingerprint, devices)).toEqual({
      local: false,
      remote: true,
      pairedDeviceId: 7,
    });
  });

  it("Given an unpaired remote fingerprint, When resolving it, Then it stays remote but exposes no local paired route", () => {
    expect(
      resolveExecutionDevice("sha256:unknown", localFingerprint, devices),
    ).toEqual({ local: false, remote: true, pairedDeviceId: 0 });
  });
});
