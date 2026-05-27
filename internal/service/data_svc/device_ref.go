package data_svc

import (
	"strconv"

	"agentre/internal/model/entity/paired_agentred_entity"
)

type deviceRefResolver struct {
	bundleUUIDs      map[string]struct{}
	localUUIDs       map[string]struct{}
	localIDToUUID    map[string]string
	singleBundleUUID string
}

func newDeviceRefResolver(localDevices []*paired_agentred_entity.PairedAgentred, bundleDevices []BundleRemoteDevice) *deviceRefResolver {
	r := &deviceRefResolver{
		bundleUUIDs:   make(map[string]struct{}, len(bundleDevices)),
		localUUIDs:    make(map[string]struct{}, len(localDevices)),
		localIDToUUID: make(map[string]string, len(localDevices)),
	}
	if len(bundleDevices) == 1 {
		r.singleBundleUUID = bundleDevices[0].InstanceUUID
	}
	for _, d := range bundleDevices {
		if d.InstanceUUID != "" {
			r.bundleUUIDs[d.InstanceUUID] = struct{}{}
		}
	}
	for _, d := range localDevices {
		if d == nil {
			continue
		}
		if d.InstanceUUID != "" {
			r.localUUIDs[d.InstanceUUID] = struct{}{}
			r.localIDToUUID[strconv.FormatInt(d.ID, 10)] = d.InstanceUUID
		}
	}
	return r
}

func (r *deviceRefResolver) StableKey(ref string) (string, bool) {
	if r == nil || ref == "" {
		return "", false
	}
	if _, ok := r.bundleUUIDs[ref]; ok {
		return ref, true
	}
	if _, ok := r.localUUIDs[ref]; ok {
		return ref, true
	}
	if uuid, ok := r.localIDToUUID[ref]; ok {
		return uuid, true
	}
	if r.singleBundleUUID != "" && isDecimalDeviceID(ref) {
		return r.singleBundleUUID, true
	}
	return "", false
}

func isDecimalDeviceID(ref string) bool {
	id, err := strconv.ParseInt(ref, 10, 64)
	return err == nil && id > 0
}
