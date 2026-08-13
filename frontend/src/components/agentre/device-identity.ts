export type PairedDeviceIdentity = {
  id: number;
  daemonFingerprint?: string;
};

export type ExecutionDeviceResolution = {
  local: boolean;
  remote: boolean;
  pairedDeviceId: number;
};

/**
 * Resolve a persisted/synced execution-device identity at the local UI boundary.
 * Canonical fingerprints remain the stored value; only RPCs backed by a local
 * paired-device row consume pairedDeviceId.
 */
export function resolveExecutionDevice(
  deviceId: string,
  localFingerprint: string,
  devices: PairedDeviceIdentity[],
): ExecutionDeviceResolution {
  const value = deviceId.trim();
  if (value === "" || (localFingerprint !== "" && value === localFingerprint)) {
    return { local: true, remote: false, pairedDeviceId: 0 };
  }
  if (/^\d+$/.test(value)) {
    const pairedDeviceId = Number(value);
    return {
      local: false,
      remote: pairedDeviceId > 0,
      pairedDeviceId: pairedDeviceId > 0 ? pairedDeviceId : 0,
    };
  }
  const paired = devices.find((device) => device.daemonFingerprint === value);
  return {
    local: false,
    remote: true,
    pairedDeviceId: paired?.id ?? 0,
  };
}

export function deviceSelectValue(
  deviceId: string,
  localFingerprint: string,
  localValue: string,
): string {
  return deviceId === "" ||
    (localFingerprint !== "" && deviceId === localFingerprint)
    ? localValue
    : deviceId;
}

export function persistedDeviceIdForSelection(
  value: string,
  localValue: string,
  localFingerprint: string,
): string {
  return value === localValue ? localFingerprint : value;
}

export function pairedDeviceSelectValue(device: PairedDeviceIdentity): string {
  return device.daemonFingerprint || String(device.id);
}
