package data_svc

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/cago-frame/cago/pkg/i18n"

	"agentre/internal/buildinfo"
	"agentre/internal/model/entity/agent_backend_entity"
	"agentre/internal/model/entity/agent_entity"
	"agentre/internal/model/entity/department_entity"
	"agentre/internal/model/entity/llm_provider_entity"
	"agentre/internal/model/entity/paired_agentred_entity"
	"agentre/internal/pkg/code"
	"agentre/internal/repository/agent_backend_repo"
	"agentre/internal/repository/agent_repo"
	"agentre/internal/repository/department_repo"
	"agentre/internal/repository/llm_provider_repo"
	"agentre/internal/repository/remote_device_repo"
)

const (
	backendExportKeyPrefix = "ab-"
	deptExportKeyPrefix    = "dept-"
	agentExportKeyPrefix   = "ag-"
)

// Export 收集所选 scope 快照并返回 JSON bytes。
func (s *dataSvc) Export(ctx context.Context, req *ExportRequest) (*ExportResult, error) {
	if err := ValidateScopes(ctx, req.Scopes); err != nil {
		return nil, err
	}
	set := scopeSet(req.Scopes)

	bundle := BundleV1{
		Format:          BundleFormat,
		Version:         BundleVersion,
		ExportedAt:      time.Unix(s.now(), 0).Format(time.RFC3339),
		ExportedFrom:    BundleOrigin{Commit: buildinfo.CommitID},
		Scopes:          req.Scopes,
		SecretsIncluded: req.IncludeSecrets,
	}
	summary := map[string]int{}
	var remoteRows []*paired_agentred_entity.PairedAgentred
	remoteRowsLoaded := false

	if _, ok := set[ScopeLLMProviders]; ok {
		rows, err := llm_provider_repo.LLMProvider().List(ctx)
		if err != nil {
			return nil, err
		}
		bundle.Items.LLMProviders = make([]BundleLLMProvider, 0, len(rows))
		for _, r := range rows {
			bundle.Items.LLMProviders = append(bundle.Items.LLMProviders, toBundleProvider(r, req.IncludeSecrets))
		}
		summary[string(ScopeLLMProviders)] = len(bundle.Items.LLMProviders)
	}
	if _, ok := set[ScopeRemoteDevices]; ok {
		rows, err := remote_device_repo.PairedAgentred().List(ctx)
		if err != nil {
			return nil, err
		}
		remoteRows = rows
		remoteRowsLoaded = true
		bundle.Items.RemoteDevices = make([]BundleRemoteDevice, 0, len(rows))
		for _, r := range rows {
			bundle.Items.RemoteDevices = append(bundle.Items.RemoteDevices, toBundleDevice(r, req.IncludeSecrets))
		}
		summary[string(ScopeRemoteDevices)] = len(bundle.Items.RemoteDevices)
	}

	// Build a shared backendKey map once if either scope that uses it is requested.
	// This guarantees AgentBackends and Organization reference the same exportKey values.
	var backendKey map[int64]string
	_, needBackends := set[ScopeAgentBackends]
	_, needOrg := set[ScopeOrganization]
	if needBackends || needOrg {
		backends, err := agent_backend_repo.AgentBackend().List(ctx)
		if err != nil {
			return nil, err
		}
		backendKey = make(map[int64]string, len(backends))
		deviceUUIDByID := map[string]string{}
		if needBackends && backendsHaveDeviceID(backends) {
			rows := remoteRows
			if !remoteRowsLoaded {
				rows, err = remote_device_repo.PairedAgentred().List(ctx)
				if err != nil {
					return nil, err
				}
			}
			deviceUUIDByID = deviceUUIDByRowID(rows)
		}
		for _, b := range backends {
			backendKey[b.ID] = backendExportKeyPrefix + s.newUUID()
		}
		if needBackends {
			bundle.Items.AgentBackends = make([]BundleAgentBackend, 0, len(backends))
			for _, b := range backends {
				bundle.Items.AgentBackends = append(bundle.Items.AgentBackends, toBundleBackend(b, backendKey[b.ID], deviceUUIDByID))
			}
			summary[string(ScopeAgentBackends)] = len(bundle.Items.AgentBackends)
		}
	}

	if needOrg {
		// 先 list 全部 department + agent,再分配 exportKey,最后串好 parent / lead / backend ref
		depts, err := department_repo.Department().List(ctx)
		if err != nil {
			return nil, err
		}
		agents, err := agent_repo.Agent().List(ctx)
		if err != nil {
			return nil, err
		}

		deptKey := make(map[int64]string, len(depts))
		agentKey := make(map[int64]string, len(agents))
		for _, d := range depts {
			deptKey[d.ID] = deptExportKeyPrefix + s.newUUID()
		}
		for _, a := range agents {
			agentKey[a.ID] = agentExportKeyPrefix + s.newUUID()
		}

		bundle.Items.Departments = make([]BundleDepartment, 0, len(depts))
		for _, d := range depts {
			bundle.Items.Departments = append(bundle.Items.Departments, toBundleDept(d, deptKey, agentKey))
		}
		bundle.Items.Agents = make([]BundleAgent, 0, len(agents))
		for _, a := range agents {
			bundle.Items.Agents = append(bundle.Items.Agents, toBundleAgent(a, deptKey, agentKey, backendKey))
		}
		summary[string(ScopeOrganization)] = len(bundle.Items.Departments) + len(bundle.Items.Agents)
	}

	raw, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		return nil, i18n.NewError(ctx, code.DataExportEncodeFailed)
	}
	return &ExportResult{JSON: raw, Summary: summary}, nil
}

func toBundleProvider(p *llm_provider_entity.LLMProvider, secrets bool) BundleLLMProvider {
	apiKey := ""
	if secrets {
		apiKey = p.APIKey
	}
	return BundleLLMProvider{
		ProviderKey: p.ProviderKey, Type: p.Type, Name: p.Name,
		BaseURL: p.BaseURL, Model: p.Model,
		MaxOutput: p.MaxOutput, ContextWindow: p.ContextWindow,
		APIKey: apiKey,
	}
}

func toBundleDevice(d *paired_agentred_entity.PairedAgentred, secrets bool) BundleRemoteDevice {
	pem := ""
	if secrets {
		pem = d.TLSCertPEM
	}
	return BundleRemoteDevice{
		InstanceUUID: d.InstanceUUID, Name: d.Name, URL: d.URL,
		DaemonFingerprint: d.DaemonFingerprint, TLSMode: d.TLSMode, TLSCertPEM: pem,
		PairedAt: d.PairedAt,
	}
}

func backendsHaveDeviceID(backends []*agent_backend_entity.AgentBackend) bool {
	for _, b := range backends {
		if b != nil && b.DeviceID != "" {
			return true
		}
	}
	return false
}

func deviceUUIDByRowID(devices []*paired_agentred_entity.PairedAgentred) map[string]string {
	out := make(map[string]string, len(devices))
	for _, d := range devices {
		if d == nil || d.InstanceUUID == "" {
			continue
		}
		out[strconv.FormatInt(d.ID, 10)] = d.InstanceUUID
	}
	return out
}

func toBundleBackend(b *agent_backend_entity.AgentBackend, exportKey string, deviceUUIDByID map[string]string) BundleAgentBackend {
	deviceID := b.DeviceID
	if uuid, ok := deviceUUIDByID[b.DeviceID]; ok {
		deviceID = uuid
	}
	return BundleAgentBackend{
		ExportKey: exportKey,
		Type:      b.Type, Name: b.Name,
		LLMProviderKey: b.LLMProviderKey, DeviceID: deviceID, CLIPath: b.CLIPath,
		ModelRoutes: b.ModelRoutes, Sandbox: b.Sandbox, Approval: b.Approval,
		EnvJSON: b.EnvJSON, ReasoningEffort: b.ReasoningEffort,
		DefaultPermissionMode: b.DefaultPermissionMode,
	}
}

func toBundleDept(d *department_entity.Department, deptKey, agentKey map[int64]string) BundleDepartment {
	out := BundleDepartment{
		ExportKey: deptKey[d.ID],
		Name:      d.Name, Description: d.Description, Icon: d.Icon, AccentColor: d.AccentColor,
		SortOrder: d.SortOrder,
	}
	if d.ParentID > 0 {
		out.ParentKey = deptKey[d.ParentID]
	}
	if d.LeadAgentID > 0 {
		out.LeadAgentKey = agentKey[d.LeadAgentID]
	}
	return out
}

func toBundleAgent(a *agent_entity.Agent, deptKey, agentKey, backendKey map[int64]string) BundleAgent {
	out := BundleAgent{
		ExportKey: agentKey[a.ID],
		Name:      a.Name, Description: a.Description,
		AvatarColor: a.AvatarColor, AvatarIcon: a.AvatarIcon, AvatarDataURL: a.AvatarDataURL,
		SystemBadge: a.SystemBadge,
		SortOrder:   a.SortOrder, PromptJSON: a.PromptJSON, SkillsJSON: a.SkillsJSON,
	}
	if a.DepartmentID > 0 {
		out.DepartmentKey = deptKey[a.DepartmentID]
	}
	if a.ParentAgentID > 0 {
		out.ParentAgentKey = agentKey[a.ParentAgentID]
	}
	if a.AgentBackendID > 0 {
		out.AgentBackendKey = backendKey[a.AgentBackendID]
	}
	return out
}
