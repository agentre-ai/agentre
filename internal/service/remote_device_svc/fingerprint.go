package remote_device_svc

import "strings"

// IsSelfDevice reports whether deviceID is this installation's own canonical
// device fingerprint (the agentre-device-fingerprint keychain account, R5 —
// shared with LAN pairing and account login). After R13 canonicalization a
// local backend's DeviceID is the desktop's own fingerprint, so every branch
// that once keyed off be.IsLocal()/be.IsRemote() must also treat a self
// fingerprint as local. Only sha256:-prefixed named fingerprints can match;
// empty / legacy numeric values short-circuit without a keychain read. Safe to
// call before bootstrap (returns false).
func IsSelfDevice(deviceID string) bool {
	if !strings.HasPrefix(deviceID, "sha256:") {
		return false
	}
	if defaultSvc == nil {
		return false
	}
	fp, err := defaultSvc.DeviceFingerprint()
	if err != nil || fp == "" {
		return false
	}
	return deviceID == fp
}
