package data_svc

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/cago-frame/cago/database/db"
	"github.com/cago-frame/cago/pkg/consts"
	"github.com/cago-frame/cago/pkg/i18n"
	"github.com/cago-frame/cago/pkg/logger"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/department_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/llm_provider_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/paired_agentred_entity"
	"github.com/agentre-ai/agentre/internal/pkg/code"
	"github.com/agentre-ai/agentre/internal/repository/agent_backend_repo"
	"github.com/agentre-ai/agentre/internal/repository/agent_repo"
	"github.com/agentre-ai/agentre/internal/repository/department_repo"
	"github.com/agentre-ai/agentre/internal/repository/llm_provider_repo"
	"github.com/agentre-ai/agentre/internal/repository/remote_device_repo"
)

// ApplyImport 按 actions 写入,整批事务。
func (s *dataSvc) ApplyImport(ctx context.Context, req *ApplyImportRequest) (*ApplyImportResult, error) {
	var b BundleV1
	if err := json.Unmarshal(req.Raw, &b); err != nil {
		return nil, i18n.NewError(ctx, code.DataBundleFormatInvalid)
	}
	if b.Format != BundleFormat {
		return nil, i18n.NewError(ctx, code.DataBundleFormatInvalid)
	}
	if b.Version != BundleVersion {
		return nil, i18n.NewError(ctx, code.DataBundleVersionUnsupported)
	}

	// 重跑 PreviewImport 拿 items + 默认 action,然后用 req.Actions 覆盖
	preview, err := s.PreviewImport(ctx, req.Raw)
	if err != nil {
		return nil, err
	}
	actions := mergeActions(preview.Items, req.Actions, req.FallbackStrategy)

	counts := map[string]int{"created": 0, "overwrote": 0, "skipped": 0, "duplicated": 0}
	km := newKeyMap()

	err = db.Ctx(ctx).Transaction(func(tx *gorm.DB) error {
		txCtx := db.WithContextDB(ctx, tx)

		if err := applyProviders(txCtx, b, actions, km, counts, s.now()); err != nil {
			return err
		}
		deviceRefs, err := applyRemoteDevices(txCtx, b, actions, km, counts, s.now())
		if err != nil {
			return err
		}
		if err := applyAgentBackends(txCtx, b, actions, km, deviceRefs, counts, s.now()); err != nil {
			return err
		}
		if err := applyDepartments(txCtx, b, actions, km, counts, s.now()); err != nil {
			return err
		}
		if err := applyAgents(txCtx, b, actions, km, counts, s.now()); err != nil {
			return err
		}
		if err := backfillOrg(txCtx, b, km, s.now()); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		logger.Ctx(ctx).Error("data_svc.ApplyImport: txn rollback", zap.Error(err))
		return nil, err
	}
	logger.Ctx(ctx).Info("data_svc.ApplyImport: committed", zap.Any("counts", counts))
	return &ApplyImportResult{Counts: counts}, nil
}

// mergeActions 把 PreviewImport 的 default action 与用户传入的 req.Actions 合并。
// fallback 用于用户没显式指定的冲突项。
func mergeActions(items []ImportItem, override map[string]ItemAction, fallback ItemAction) map[string]ItemAction {
	out := make(map[string]ItemAction, len(items))
	for _, it := range items {
		key := it.Scope + ":" + it.SourceKey
		if a, ok := override[key]; ok && a != "" {
			out[key] = a
		} else if it.Conflict && fallback != "" {
			out[key] = fallback
		} else {
			out[key] = it.DefaultAction
		}
		// dangling 强制 skip
		if it.Dangling {
			out[key] = ActionSkip
		}
	}
	return out
}

func actionKey(scope, source string) string { return scope + ":" + source }

func applyProviders(ctx context.Context, b BundleV1, actions map[string]ItemAction, km *keyMap, counts map[string]int, now int64) error {
	existing, err := llm_provider_repo.LLMProvider().List(ctx)
	if err != nil {
		return err
	}
	byKey := map[string]*llm_provider_entity.LLMProvider{}
	for _, p := range existing {
		byKey[p.ProviderKey] = p
	}

	for _, p := range b.Items.LLMProviders {
		act := actions[actionKey(string(ScopeLLMProviders), p.ProviderKey)]
		local := byKey[p.ProviderKey]

		switch act {
		case ActionSkip:
			counts["skipped"]++
			if local != nil {
				km.providers[p.ProviderKey] = local.ID
			}
		case ActionOverwrite:
			if local == nil {
				return i18n.NewError(ctx, code.DataImportInvalidAction)
			}
			local.Type = p.Type
			local.Name = p.Name
			local.BaseURL = p.BaseURL
			local.Model = p.Model
			local.MaxOutput = p.MaxOutput
			local.ContextWindow = p.ContextWindow
			if p.APIKey != "" {
				local.APIKey = p.APIKey
			}
			local.Updatetime = now
			if err := llm_provider_repo.LLMProvider().Update(ctx, local); err != nil {
				return err
			}
			km.providers[p.ProviderKey] = local.ID
			counts["overwrote"]++
		case ActionDuplicate:
			row := newProviderEntity(p, now)
			row.ProviderKey = uuid.NewString()
			row.Name = uniqueName(p.Name, func(n string) bool {
				for _, e := range existing {
					if e.Name == n {
						return true
					}
				}
				return false
			})
			if err := llm_provider_repo.LLMProvider().Create(ctx, row); err != nil {
				return err
			}
			km.providers[p.ProviderKey] = row.ID
			counts["duplicated"]++
		case ActionCreate:
			if local != nil {
				return i18n.NewError(ctx, code.DataImportInvalidAction)
			}
			row := newProviderEntity(p, now)
			if err := llm_provider_repo.LLMProvider().Create(ctx, row); err != nil {
				return err
			}
			km.providers[p.ProviderKey] = row.ID
			counts["created"]++
		default:
			return i18n.NewError(ctx, code.DataImportInvalidAction)
		}
	}
	return nil
}

func newProviderEntity(p BundleLLMProvider, now int64) *llm_provider_entity.LLMProvider {
	return &llm_provider_entity.LLMProvider{
		ProviderKey:   p.ProviderKey,
		Type:          p.Type,
		Name:          p.Name,
		BaseURL:       p.BaseURL,
		Model:         p.Model,
		APIKey:        p.APIKey,
		MaxOutput:     p.MaxOutput,
		ContextWindow: p.ContextWindow,
		Status:        consts.ACTIVE,
		Createtime:    now,
		Updatetime:    now,
	}
}

func applyRemoteDevices(ctx context.Context, b BundleV1, actions map[string]ItemAction, km *keyMap, counts map[string]int, now int64) (*deviceRefResolver, error) {
	existing, err := remote_device_repo.PairedAgentred().List(ctx)
	if err != nil {
		return nil, err
	}
	byKey := map[string]*paired_agentred_entity.PairedAgentred{}
	for _, d := range existing {
		byKey[d.InstanceUUID] = d
		km.devices[d.InstanceUUID] = d.ID
	}
	deviceRefs := newDeviceRefResolver(existing, b.Items.RemoteDevices)

	for _, d := range b.Items.RemoteDevices {
		act := actions[actionKey(string(ScopeRemoteDevices), d.InstanceUUID)]
		local := byKey[d.InstanceUUID]

		switch act {
		case ActionSkip:
			counts["skipped"]++
			if local != nil {
				km.devices[d.InstanceUUID] = local.ID
			}
		case ActionOverwrite:
			if local == nil {
				return nil, i18n.NewError(ctx, code.DataImportInvalidAction)
			}
			local.Name = d.Name
			local.URL = d.URL
			local.DaemonFingerprint = d.DaemonFingerprint
			local.TLSMode = d.TLSMode
			if d.TLSCertPEM != "" {
				local.TLSCertPEM = d.TLSCertPEM
			}
			local.Updatetime = now
			if err := updateRemoteDevice(ctx, local); err != nil {
				return nil, err
			}
			km.devices[d.InstanceUUID] = local.ID
			counts["overwrote"]++
		case ActionDuplicate:
			row := newDeviceEntity(d, now)
			row.InstanceUUID = uuid.NewString()
			row.Name = uniqueName(d.Name, func(n string) bool {
				for _, e := range existing {
					if e.Name == n {
						return true
					}
				}
				return false
			})
			if err := remote_device_repo.PairedAgentred().Create(ctx, row); err != nil {
				return nil, err
			}
			km.devices[d.InstanceUUID] = row.ID
			counts["duplicated"]++
		case ActionCreate:
			if local != nil {
				return nil, i18n.NewError(ctx, code.DataImportInvalidAction)
			}
			row := newDeviceEntity(d, now)
			if err := remote_device_repo.PairedAgentred().Create(ctx, row); err != nil {
				return nil, err
			}
			km.devices[d.InstanceUUID] = row.ID
			counts["created"]++
		default:
			return nil, i18n.NewError(ctx, code.DataImportInvalidAction)
		}
	}
	return deviceRefs, nil
}

func newDeviceEntity(d BundleRemoteDevice, now int64) *paired_agentred_entity.PairedAgentred {
	return &paired_agentred_entity.PairedAgentred{
		InstanceUUID:      d.InstanceUUID,
		Name:              d.Name,
		URL:               d.URL,
		DaemonFingerprint: d.DaemonFingerprint,
		TLSMode:           d.TLSMode,
		TLSCertPEM:        d.TLSCertPEM,
		PairedAt:          d.PairedAt,
		LastSeenAt:        0,
		LastError:         "",
		Status:            consts.ACTIVE,
		Createtime:        now,
		Updatetime:        now,
	}
}

// updateRemoteDevice — repo 没有通用 Update,组合 UpdateTLS + UpdateEndpoint + Rename。
func updateRemoteDevice(ctx context.Context, d *paired_agentred_entity.PairedAgentred) error {
	if err := remote_device_repo.PairedAgentred().UpdateTLS(ctx, d.ID, d.TLSMode, d.TLSCertPEM); err != nil {
		return err
	}
	if err := remote_device_repo.PairedAgentred().UpdateEndpoint(ctx, d.ID, d.URL, d.DaemonFingerprint); err != nil {
		return err
	}
	return remote_device_repo.PairedAgentred().Rename(ctx, d.ID, d.Name)
}

func applyAgentBackends(ctx context.Context, b BundleV1, actions map[string]ItemAction, km *keyMap, deviceRefs *deviceRefResolver, counts map[string]int, now int64) error {
	existing, err := agent_backend_repo.AgentBackend().List(ctx)
	if err != nil {
		return err
	}
	byName := map[string]*agent_backend_entity.AgentBackend{}
	nameCount := map[string]int{}
	for _, bk := range existing {
		nameCount[bk.Name]++
		byName[bk.Name] = bk
	}

	for _, bk := range b.Items.AgentBackends {
		act := actions[actionKey(string(ScopeAgentBackends), bk.ExportKey)]
		var local *agent_backend_entity.AgentBackend
		if nameCount[bk.Name] == 1 {
			local = byName[bk.Name]
		}

		switch act {
		case ActionSkip:
			counts["skipped"]++
			if local != nil {
				km.backends[bk.ExportKey] = local.ID
			}
		case ActionOverwrite:
			if local == nil {
				return i18n.NewError(ctx, code.DataImportInvalidAction)
			}
			deviceID, err := importBackendDeviceID(ctx, bk, km, deviceRefs)
			if err != nil {
				return err
			}
			assignBackendFields(local, bk, now, deviceID)
			if err := agent_backend_repo.AgentBackend().Update(ctx, local); err != nil {
				return err
			}
			km.backends[bk.ExportKey] = local.ID
			counts["overwrote"]++
		case ActionDuplicate:
			deviceID, err := importBackendDeviceID(ctx, bk, km, deviceRefs)
			if err != nil {
				return err
			}
			row := newBackendEntity(bk, now, deviceID)
			row.Name = uniqueName(bk.Name, func(n string) bool {
				for _, e := range existing {
					if e.Name == n {
						return true
					}
				}
				return false
			})
			if err := agent_backend_repo.AgentBackend().Create(ctx, row); err != nil {
				return err
			}
			km.backends[bk.ExportKey] = row.ID
			counts["duplicated"]++
		case ActionCreate:
			if local != nil {
				return i18n.NewError(ctx, code.DataImportInvalidAction)
			}
			deviceID, err := importBackendDeviceID(ctx, bk, km, deviceRefs)
			if err != nil {
				return err
			}
			row := newBackendEntity(bk, now, deviceID)
			if err := agent_backend_repo.AgentBackend().Create(ctx, row); err != nil {
				return err
			}
			km.backends[bk.ExportKey] = row.ID
			counts["created"]++
		default:
			return i18n.NewError(ctx, code.DataImportInvalidAction)
		}
	}
	return nil
}

func importBackendDeviceID(ctx context.Context, bk BundleAgentBackend, km *keyMap, deviceRefs *deviceRefResolver) (string, error) {
	if bk.DeviceID == "" {
		return "", nil
	}
	stableKey, ok := deviceRefs.StableKey(bk.DeviceID)
	if !ok {
		return "", i18n.NewError(ctx, code.DataImportDanglingRef)
	}
	id, ok := km.devices[stableKey]
	if !ok {
		return "", i18n.NewError(ctx, code.DataImportDanglingRef)
	}
	return strconv.FormatInt(id, 10), nil
}

func newBackendEntity(bk BundleAgentBackend, now int64, deviceID string) *agent_backend_entity.AgentBackend {
	return &agent_backend_entity.AgentBackend{
		Type:                  bk.Type,
		Name:                  bk.Name,
		LLMProviderKey:        bk.LLMProviderKey,
		DeviceID:              deviceID,
		CLIPath:               bk.CLIPath,
		ModelRoutes:           bk.ModelRoutes,
		Sandbox:               bk.Sandbox,
		Approval:              bk.Approval,
		EnvJSON:               bk.EnvJSON,
		ReasoningEffort:       bk.ReasoningEffort,
		DefaultPermissionMode: bk.DefaultPermissionMode,
		Status:                consts.ACTIVE,
		Createtime:            now,
		Updatetime:            now,
	}
}

func assignBackendFields(local *agent_backend_entity.AgentBackend, bk BundleAgentBackend, now int64, deviceID string) {
	local.Type = bk.Type
	local.Name = bk.Name
	local.LLMProviderKey = bk.LLMProviderKey
	local.DeviceID = deviceID
	local.CLIPath = bk.CLIPath
	local.ModelRoutes = bk.ModelRoutes
	local.Sandbox = bk.Sandbox
	local.Approval = bk.Approval
	local.EnvJSON = bk.EnvJSON
	local.ReasoningEffort = bk.ReasoningEffort
	local.DefaultPermissionMode = bk.DefaultPermissionMode
	local.Updatetime = now
}

// uniqueName 给同名记录加 (copy) / (copy 2) 后缀直到不冲突。
func uniqueName(base string, taken func(string) bool) string {
	if !taken(base) {
		return base
	}
	candidate := base + " (copy)"
	for i := 2; taken(candidate); i++ {
		candidate = base + " (copy " + strconv.Itoa(i) + ")"
	}
	return candidate
}

// applyDepartments 第一遍:按 parentKey 拓扑排序(根优先),lead_agent_id=0,留待 backfillOrg。
func applyDepartments(ctx context.Context, b BundleV1, actions map[string]ItemAction, km *keyMap, counts map[string]int, now int64) error {
	pending := append([]BundleDepartment(nil), b.Items.Departments...)
	resolved := map[string]bool{} // exportKey → 已处理
	for len(pending) > 0 {
		progress := false
		next := pending[:0]
		for _, d := range pending {
			if d.ParentKey != "" && !resolved[d.ParentKey] {
				next = append(next, d)
				continue
			}
			if err := applyOneDepartment(ctx, d, actions, km, counts, now); err != nil {
				return err
			}
			resolved[d.ExportKey] = true
			progress = true
		}
		pending = next
		if !progress {
			// 引用环或孤儿,跳过剩余
			break
		}
	}
	return nil
}

// applyOneDepartment 创建/跳过/覆盖一条部门记录。LeadAgentID 留待 backfillOrg 填写。
func applyOneDepartment(ctx context.Context, d BundleDepartment, actions map[string]ItemAction, km *keyMap, counts map[string]int, now int64) error {
	parentID := int64(0)
	if d.ParentKey != "" {
		parentID = km.depts[d.ParentKey]
	}

	existing, err := department_repo.Department().FindByName(ctx, d.Name, parentID)
	if err != nil {
		return err
	}

	act := actions[actionKey(string(ScopeOrganization), d.ExportKey)]
	switch act {
	case ActionSkip:
		counts["skipped"]++
		if existing != nil {
			km.depts[d.ExportKey] = existing.ID
		}
	case ActionOverwrite:
		if existing == nil {
			return i18n.NewError(ctx, code.DataImportInvalidAction)
		}
		existing.Name = d.Name
		existing.Description = d.Description
		existing.Icon = d.Icon
		existing.AccentColor = d.AccentColor
		existing.SortOrder = d.SortOrder
		existing.ParentID = parentID
		existing.Updatetime = now
		// LeadAgentID 留待 backfill 阶段
		if err := department_repo.Department().Update(ctx, existing); err != nil {
			return err
		}
		km.depts[d.ExportKey] = existing.ID
		counts["overwrote"]++
	case ActionDuplicate:
		row := &department_entity.Department{
			Name:        d.Name,
			Description: d.Description,
			Icon:        d.Icon,
			AccentColor: d.AccentColor,
			ParentID:    parentID,
			SortOrder:   d.SortOrder,
			Status:      consts.ACTIVE,
			Createtime:  now,
			Updatetime:  now,
		}
		if existing != nil {
			row.Name = uniqueName(d.Name, func(n string) bool {
				ex, _ := department_repo.Department().FindByName(ctx, n, parentID)
				return ex != nil
			})
		}
		if err := department_repo.Department().Create(ctx, row); err != nil {
			return err
		}
		km.depts[d.ExportKey] = row.ID
		counts["duplicated"]++
	case ActionCreate:
		if existing != nil {
			// V1: preview can't detect dept conflict when parent resolves at apply-time.
			// Treat create-with-existing as skip; keymap still wires to existing.
			km.depts[d.ExportKey] = existing.ID
			counts["skipped"]++
			return nil
		}
		row := &department_entity.Department{
			Name:        d.Name,
			Description: d.Description,
			Icon:        d.Icon,
			AccentColor: d.AccentColor,
			ParentID:    parentID,
			SortOrder:   d.SortOrder,
			Status:      consts.ACTIVE,
			Createtime:  now,
			Updatetime:  now,
		}
		if err := department_repo.Department().Create(ctx, row); err != nil {
			return err
		}
		km.depts[d.ExportKey] = row.ID
		counts["created"]++
	default:
		return i18n.NewError(ctx, code.DataImportInvalidAction)
	}
	return nil
}

// applyAgents 第一遍:创建/匹配 agent,parent_agent_id=0,department/backend 用 keymap 解析。
func applyAgents(ctx context.Context, b BundleV1, actions map[string]ItemAction, km *keyMap, counts map[string]int, now int64) error {
	for _, a := range b.Items.Agents {
		deptID := int64(0)
		if a.DepartmentKey != "" {
			deptID = km.depts[a.DepartmentKey]
		}
		backendID := int64(0)
		if a.AgentBackendKey != "" {
			backendID = km.backends[a.AgentBackendKey]
		}

		// 找 (deptID, name) 匹配
		list, err := agent_repo.Agent().ListByDepartment(ctx, deptID)
		if err != nil {
			return err
		}
		var existing *agent_entity.Agent
		for _, row := range list {
			if row.Name == a.Name {
				existing = row
				break
			}
		}

		act := actions[actionKey(string(ScopeOrganization), a.ExportKey)]
		switch act {
		case ActionSkip:
			counts["skipped"]++
			if existing != nil {
				km.agents[a.ExportKey] = existing.ID
			}
		case ActionOverwrite:
			if existing == nil {
				return i18n.NewError(ctx, code.DataImportInvalidAction)
			}
			existing.Name = a.Name
			existing.Description = a.Description
			existing.AvatarColor = a.AvatarColor
			existing.AvatarIcon = a.AvatarIcon
			existing.AvatarDataURL = a.AvatarDataURL
			existing.SystemBadge = a.SystemBadge
			existing.DepartmentID = deptID
			existing.AgentBackendID = backendID
			existing.SortOrder = a.SortOrder
			existing.PromptJSON = a.PromptJSON
			existing.SkillsJSON = a.SkillsJSON
			existing.Updatetime = now
			// ParentAgentID 留待 backfill
			if err := agent_repo.Agent().Update(ctx, existing); err != nil {
				return err
			}
			km.agents[a.ExportKey] = existing.ID
			counts["overwrote"]++
		case ActionDuplicate:
			row := &agent_entity.Agent{
				Name:           a.Name,
				Description:    a.Description,
				AvatarColor:    a.AvatarColor,
				AvatarIcon:     a.AvatarIcon,
				AvatarDataURL:  a.AvatarDataURL,
				SystemBadge:    a.SystemBadge,
				DepartmentID:   deptID,
				AgentBackendID: backendID,
				SortOrder:      a.SortOrder,
				PromptJSON:     a.PromptJSON,
				SkillsJSON:     a.SkillsJSON,
				Status:         consts.ACTIVE,
				Createtime:     now,
				Updatetime:     now,
			}
			if existing != nil {
				row.Name = uniqueName(a.Name, func(n string) bool {
					for _, e := range list {
						if e.Name == n {
							return true
						}
					}
					return false
				})
			}
			if err := agent_repo.Agent().Create(ctx, row); err != nil {
				return err
			}
			km.agents[a.ExportKey] = row.ID
			counts["duplicated"]++
		case ActionCreate:
			if existing != nil {
				// V1: preview can't detect agent conflict because dept resolution requires apply-time keymap.
				// Treat create-with-existing as skip; keymap still wires to existing.
				km.agents[a.ExportKey] = existing.ID
				counts["skipped"]++
				continue
			}
			row := &agent_entity.Agent{
				Name:           a.Name,
				Description:    a.Description,
				AvatarColor:    a.AvatarColor,
				AvatarIcon:     a.AvatarIcon,
				AvatarDataURL:  a.AvatarDataURL,
				SystemBadge:    a.SystemBadge,
				DepartmentID:   deptID,
				AgentBackendID: backendID,
				SortOrder:      a.SortOrder,
				PromptJSON:     a.PromptJSON,
				SkillsJSON:     a.SkillsJSON,
				Status:         consts.ACTIVE,
				Createtime:     now,
				Updatetime:     now,
			}
			if err := agent_repo.Agent().Create(ctx, row); err != nil {
				return err
			}
			km.agents[a.ExportKey] = row.ID
			counts["created"]++
		default:
			return i18n.NewError(ctx, code.DataImportInvalidAction)
		}
	}
	return nil
}

// backfillOrg 第二遍:用 keymap 把 department.LeadAgentID 和 agent.ParentAgentID 填回去。
func backfillOrg(ctx context.Context, b BundleV1, km *keyMap, now int64) error {
	for _, d := range b.Items.Departments {
		if d.LeadAgentKey == "" {
			continue
		}
		localID, ok := km.depts[d.ExportKey]
		if !ok {
			continue
		}
		leadID, ok := km.agents[d.LeadAgentKey]
		if !ok {
			continue
		}
		row, err := department_repo.Department().Find(ctx, localID)
		if err != nil || row == nil {
			return err
		}
		row.LeadAgentID = leadID
		row.Updatetime = now
		if err := department_repo.Department().Update(ctx, row); err != nil {
			return err
		}
	}
	for _, a := range b.Items.Agents {
		if a.ParentAgentKey == "" {
			continue
		}
		localID, ok := km.agents[a.ExportKey]
		if !ok {
			continue
		}
		parentID, ok := km.agents[a.ParentAgentKey]
		if !ok {
			continue
		}
		row, err := agent_repo.Agent().Find(ctx, localID)
		if err != nil || row == nil {
			return err
		}
		row.ParentAgentID = parentID
		row.Updatetime = now
		if err := agent_repo.Agent().Update(ctx, row); err != nil {
			return err
		}
	}
	return nil
}
