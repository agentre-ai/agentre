package remote_device_svc

import (
	"strconv"
	"time"

	"github.com/agentre-ai/agentre/internal/model/entity/paired_agentred_entity"
)

func itoa(i int64) string { return strconv.FormatInt(i, 10) }

// toView projects entity → DTO; computes Online via current wall clock.
func toView(p *paired_agentred_entity.PairedAgentred) *DeviceView {
	if p == nil {
		return nil
	}
	return &DeviceView{
		ID:                p.ID,
		Name:              p.Name,
		URL:               p.URL,
		DaemonFingerprint: p.DaemonFingerprint,
		InstanceUUID:      p.InstanceUUID,
		TLSMode:           p.TLSMode,
		TLSCertPEM:        p.TLSCertPEM,
		PairedAt:          p.PairedAt,
		LastSeenAt:        p.LastSeenAt,
		LastError:         p.LastError,
		Online:            p.IsOnline(time.Now().UnixMilli()),
	}
}
