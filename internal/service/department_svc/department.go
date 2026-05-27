// Package department_svc 部门相关业务服务。
package department_svc

import (
	"context"
	"strings"
	"time"

	"github.com/cago-frame/cago/database/db"
	"github.com/cago-frame/cago/pkg/consts"
	"github.com/cago-frame/cago/pkg/i18n"
	"gorm.io/gorm"

	"agentre/internal/model/entity/agent_entity"
	"agentre/internal/model/entity/department_entity"
	"agentre/internal/pkg/code"
	"agentre/internal/repository/agent_backend_repo"
	"agentre/internal/repository/agent_repo"
	"agentre/internal/repository/department_repo"
	"agentre/internal/repository/llm_provider_repo"
)

const (
	StrategyReparent = "reparent"
	StrategyCascade  = "cascade"
)

// DepartmentSvc 部门应用服务。
type DepartmentSvc interface {
	Load(ctx context.Context, req *LoadOrgRequest) (*LoadOrgResponse, error)
	Create(ctx context.Context, req *CreateDepartmentRequest) (*CreateDepartmentResponse, error)
	Update(ctx context.Context, req *UpdateDepartmentRequest) (*UpdateDepartmentResponse, error)
	Move(ctx context.Context, req *MoveDepartmentRequest) (*MoveDepartmentResponse, error)
	Delete(ctx context.Context, req *DeleteDepartmentRequest) (*DeleteDepartmentResponse, error)
}

type departmentSvc struct {
	now func() int64
}

var defaultDepartment DepartmentSvc = &departmentSvc{now: func() int64 { return time.Now().Unix() }}

// Department 取默认服务单例。
func Department() DepartmentSvc { return defaultDepartment }

// Load 一次性聚合部门 + Agent + Backend 摘要，给前端首屏使用。
func (s *departmentSvc) Load(ctx context.Context, _ *LoadOrgRequest) (*LoadOrgResponse, error) {
	depts, err := department_repo.Department().List(ctx)
	if err != nil {
		return nil, err
	}
	agents, err := agent_repo.Agent().List(ctx)
	if err != nil {
		return nil, err
	}
	backends, err := agent_backend_repo.AgentBackend().List(ctx)
	if err != nil {
		return nil, err
	}
	providers, err := llm_provider_repo.LLMProvider().List(ctx)
	if err != nil {
		return nil, err
	}

	providerByKey := make(map[string]string)
	providerModelByKey := make(map[string]string)
	providerActiveByKey := make(map[string]bool)
	for _, p := range providers {
		providerByKey[p.ProviderKey] = p.Name
		providerModelByKey[p.ProviderKey] = p.Model
		providerActiveByKey[p.ProviderKey] = p.IsActive()
	}
	backendByID := make(map[int64]*BackendSummary)
	for _, b := range backends {
		backendByID[b.ID] = &BackendSummary{
			ID:                b.ID,
			Type:              b.Type,
			Name:              b.Name,
			LLMProviderName:   providerByKey[b.LLMProviderKey],
			LLMProviderModel:  providerModelByKey[b.LLMProviderKey],
			LLMProviderActive: providerActiveByKey[b.LLMProviderKey],
		}
	}
	deptByID := make(map[int64]*department_entity.Department)
	for _, d := range depts {
		deptByID[d.ID] = d
	}
	agentByID := make(map[int64]*agent_entity.Agent)
	agentsByParent := make(map[int64][]*agent_entity.Agent)
	directAgents := make(map[int64]int)
	for _, a := range agents {
		agentByID[a.ID] = a
		if a.ParentAgentID > 0 {
			agentsByParent[a.ParentAgentID] = append(agentsByParent[a.ParentAgentID], a)
		} else {
			directAgents[a.DepartmentID]++
		}
	}
	agentDescendantCount := make(map[int64]int)
	var countAgentDescendants func(id int64) int
	countAgentDescendants = func(id int64) int {
		if v, ok := agentDescendantCount[id]; ok {
			return v
		}
		total := 0
		for _, child := range agentsByParent[id] {
			total++
			total += countAgentDescendants(child.ID)
		}
		agentDescendantCount[id] = total
		return total
	}
	subdepts := make(map[int64]int)
	for _, d := range depts {
		subdepts[d.ParentID]++
	}
	memberCount := make(map[int64]int)
	var members func(id int64) int
	members = func(id int64) int {
		if v, ok := memberCount[id]; ok {
			return v
		}
		total := directAgents[id]
		for _, a := range agents {
			if a.DepartmentID == id && a.ParentAgentID == 0 {
				total += countAgentDescendants(a.ID)
			}
		}
		for _, child := range depts {
			if child.ParentID == id {
				total += members(child.ID)
			}
		}
		memberCount[id] = total
		return total
	}

	resp := &LoadOrgResponse{
		Departments: make([]*DepartmentItem, 0, len(depts)),
		Agents:      make([]*AgentItem, 0, len(agents)),
	}
	for _, d := range depts {
		lead := agentByID[d.LeadAgentID]
		resp.Departments = append(resp.Departments, toDepartmentItem(
			d, lead,
			directAgents[d.ID], subdepts[d.ID], members(d.ID),
		))
	}
	for _, a := range agents {
		item := &AgentItem{
			ID:             a.ID,
			Name:           a.Name,
			Description:    a.Description,
			AvatarColor:    a.AvatarColor,
			AvatarIcon:     a.AvatarIcon,
			AvatarDataURL:  a.AvatarDataURL,
			SystemBadge:    a.SystemBadge,
			DepartmentID:   a.DepartmentID,
			AgentBackendID: a.AgentBackendID,
			ParentAgentID:  a.ParentAgentID,
			SortOrder:      a.SortOrder,
			Prompt:         a.GetPrompt(),
			Skills:         toAgentSkillDTO(a.GetSkills()),
			Createtime:     a.Createtime,
			Updatetime:     a.Updatetime,
		}
		if d := deptByID[a.DepartmentID]; d != nil {
			item.DepartmentName = d.Name
		}
		if p := agentByID[a.ParentAgentID]; p != nil {
			item.ParentAgentName = p.Name
		}
		if b := backendByID[a.AgentBackendID]; b != nil {
			item.Backend = b
		}
		resp.Agents = append(resp.Agents, item)
	}
	return resp, nil
}

func toAgentSkillDTO(items []agent_entity.AgentSkillItem) []AgentSkillDTO {
	out := make([]AgentSkillDTO, 0, len(items))
	for _, s := range items {
		out = append(out, AgentSkillDTO{Label: s.Label, Enabled: s.Enabled})
	}
	return out
}

// Create 新建部门。
func (s *departmentSvc) Create(ctx context.Context, req *CreateDepartmentRequest) (*CreateDepartmentResponse, error) {
	now := s.now()
	d := &department_entity.Department{
		Name:        strings.TrimSpace(req.Name),
		Description: strings.TrimSpace(req.Description),
		Icon:        strings.TrimSpace(req.Icon),
		AccentColor: strings.TrimSpace(req.AccentColor),
		ParentID:    req.ParentID,
		Status:      consts.ACTIVE,
		Createtime:  now,
		Updatetime:  now,
	}
	if err := d.Check(ctx); err != nil {
		return nil, err
	}
	if d.ParentID > 0 {
		parent, err := department_repo.Department().Find(ctx, d.ParentID)
		if err != nil {
			return nil, err
		}
		if parent == nil {
			return nil, i18n.NewError(ctx, code.DepartmentParentNotFound)
		}
		if !parent.IsActive() {
			return nil, i18n.NewError(ctx, code.DepartmentParentInactive)
		}
	}
	dup, err := department_repo.Department().FindByName(ctx, d.Name, d.ParentID)
	if err != nil {
		return nil, err
	}
	if dup != nil {
		return nil, i18n.NewError(ctx, code.DepartmentNameDuplicated)
	}
	next, err := department_repo.Department().NextSortOrder(ctx, d.ParentID)
	if err != nil {
		return nil, err
	}
	d.SortOrder = next
	if err := department_repo.Department().Create(ctx, d); err != nil {
		return nil, err
	}
	return &CreateDepartmentResponse{Item: toDepartmentItem(d, nil, 0, 0, 0)}, nil
}

// Update 更新部门基本信息（不含 parent_id，那走 Move）。
func (s *departmentSvc) Update(ctx context.Context, req *UpdateDepartmentRequest) (*UpdateDepartmentResponse, error) {
	existing, err := department_repo.Department().Find(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, i18n.NewError(ctx, code.DepartmentNotFound)
	}
	newName := strings.TrimSpace(req.Name)
	if newName != existing.Name {
		dup, err := department_repo.Department().FindByName(ctx, newName, existing.ParentID)
		if err != nil {
			return nil, err
		}
		if dup != nil && dup.ID != existing.ID {
			return nil, i18n.NewError(ctx, code.DepartmentNameDuplicated)
		}
	}
	existing.Name = newName
	existing.Description = strings.TrimSpace(req.Description)
	existing.Icon = strings.TrimSpace(req.Icon)
	existing.AccentColor = strings.TrimSpace(req.AccentColor)
	existing.LeadAgentID = req.LeadAgentID
	existing.Updatetime = s.now()
	if err := existing.Check(ctx); err != nil {
		return nil, err
	}
	if existing.LeadAgentID > 0 {
		lead, err := agent_repo.Agent().Find(ctx, existing.LeadAgentID)
		if err != nil {
			return nil, err
		}
		if lead == nil || lead.DepartmentID != existing.ID {
			return nil, i18n.NewError(ctx, code.DepartmentLeadNotInDepartment)
		}
	}
	if err := department_repo.Department().Update(ctx, existing); err != nil {
		return nil, err
	}
	return &UpdateDepartmentResponse{Item: toDepartmentItem(existing, nil, 0, 0, 0)}, nil
}

// Move 改父部门 + 同级排序，含环检测。
func (s *departmentSvc) Move(ctx context.Context, req *MoveDepartmentRequest) (*MoveDepartmentResponse, error) {
	existing, err := department_repo.Department().Find(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, i18n.NewError(ctx, code.DepartmentNotFound)
	}
	if req.NewParentID > 0 {
		parent, err := department_repo.Department().Find(ctx, req.NewParentID)
		if err != nil {
			return nil, err
		}
		if parent == nil {
			return nil, i18n.NewError(ctx, code.DepartmentParentNotFound)
		}
		if !parent.IsActive() {
			return nil, i18n.NewError(ctx, code.DepartmentParentInactive)
		}
		all, err := department_repo.Department().List(ctx)
		if err != nil {
			return nil, err
		}
		if hasCycle(all, req.NewParentID, existing.ID) {
			return nil, i18n.NewError(ctx, code.DepartmentCircularReference)
		}
	}
	sortOrder := req.NewSortOrder
	if sortOrder <= 0 {
		next, err := department_repo.Department().NextSortOrder(ctx, req.NewParentID)
		if err != nil {
			return nil, err
		}
		sortOrder = next
	}
	existing.ParentID = req.NewParentID
	existing.SortOrder = sortOrder
	existing.Updatetime = s.now()
	if err := department_repo.Department().Update(ctx, existing); err != nil {
		return nil, err
	}
	return &MoveDepartmentResponse{Item: toDepartmentItem(existing, nil, 0, 0, 0)}, nil
}

// Delete 软删部门，支持 reparent / cascade。
func (s *departmentSvc) Delete(ctx context.Context, req *DeleteDepartmentRequest) (*DeleteDepartmentResponse, error) {
	existing, err := department_repo.Department().Find(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, i18n.NewError(ctx, code.DepartmentNotFound)
	}
	strategy := req.Strategy
	if strategy == "" {
		strategy = StrategyReparent
	}
	err = db.Ctx(ctx).Transaction(func(tx *gorm.DB) error {
		txCtx := db.WithContextDB(ctx, tx)
		switch strategy {
		case StrategyReparent:
			if err := department_repo.Department().ReparentChildren(txCtx, existing.ID, existing.ParentID); err != nil {
				return err
			}
			agents, err := agent_repo.Agent().ListByDepartment(txCtx, existing.ID)
			if err != nil {
				return err
			}
			var rootAgentID int64
			if existing.ParentID == 0 && len(agents) > 0 {
				rootAgent, err := agent_repo.Agent().FindSystem(txCtx)
				if err != nil {
					return err
				}
				if rootAgent == nil {
					return i18n.NewError(ctx, code.AgentParentNotFound)
				}
				rootAgentID = rootAgent.ID
			}
			for _, a := range agents {
				if existing.ParentID == 0 {
					if err := agent_repo.Agent().UpdatePlacement(txCtx, a.ID, 0, rootAgentID, a.SortOrder); err != nil {
						return err
					}
					continue
				}
				if err := agent_repo.Agent().UpdatePlacement(txCtx, a.ID, existing.ParentID, 0, a.SortOrder); err != nil {
					return err
				}
			}
		case StrategyCascade:
			all, err := department_repo.Department().List(txCtx)
			if err != nil {
				return err
			}
			subtree := collectSubtree(all, existing.ID)
			allAgents, err := agent_repo.Agent().List(txCtx)
			if err != nil {
				return err
			}
			for _, agentID := range collectAgentsInDepartments(allAgents, subtree) {
				if err := agent_repo.Agent().Delete(txCtx, agentID); err != nil {
					return err
				}
			}
			for _, id := range subtree {
				if err := department_repo.Department().Delete(txCtx, id); err != nil {
					return err
				}
			}
			return nil
		default:
			return i18n.NewError(ctx, code.InvalidParameter)
		}
		return department_repo.Department().Delete(txCtx, existing.ID)
	})
	if err != nil {
		return nil, err
	}
	return &DeleteDepartmentResponse{}, nil
}

// hasCycle 从 startParentID 沿 parent 链向上爬，若命中 selfID 则形成环。
func hasCycle(all []*department_entity.Department, startParentID, selfID int64) bool {
	index := make(map[int64]*department_entity.Department, len(all))
	for _, d := range all {
		index[d.ID] = d
	}
	cur := startParentID
	for cur > 0 {
		if cur == selfID {
			return true
		}
		next, ok := index[cur]
		if !ok {
			return false
		}
		cur = next.ParentID
	}
	return false
}

// collectSubtree 收集 rootID 自身 + 全部递归后代 ID（深度优先）。
func collectSubtree(all []*department_entity.Department, rootID int64) []int64 {
	children := make(map[int64][]int64)
	for _, d := range all {
		children[d.ParentID] = append(children[d.ParentID], d.ID)
	}
	var out []int64
	var walk func(id int64)
	walk = func(id int64) {
		out = append(out, id)
		for _, c := range children[id] {
			walk(c)
		}
	}
	walk(rootID)
	return out
}

func collectAgentsInDepartments(all []*agent_entity.Agent, departmentIDs []int64) []int64 {
	inScopeDepartment := make(map[int64]struct{}, len(departmentIDs))
	for _, id := range departmentIDs {
		inScopeDepartment[id] = struct{}{}
	}
	children := make(map[int64][]int64)
	for _, a := range all {
		children[a.ParentAgentID] = append(children[a.ParentAgentID], a.ID)
	}
	seen := make(map[int64]struct{})
	var out []int64
	var walk func(id int64)
	walk = func(id int64) {
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		out = append(out, id)
		for _, child := range children[id] {
			walk(child)
		}
	}
	for _, a := range all {
		if a.ParentAgentID == 0 {
			if _, ok := inScopeDepartment[a.DepartmentID]; ok {
				walk(a.ID)
			}
		}
	}
	return out
}

// toDepartmentItem 把 entity + lead 摘要 + 计数打平成 DTO。
func toDepartmentItem(
	d *department_entity.Department,
	lead *agent_entity.Agent,
	directAgentCount, subdepartmentCount, memberCount int,
) *DepartmentItem {
	item := &DepartmentItem{
		ID:                 d.ID,
		Name:               d.Name,
		Description:        d.Description,
		Icon:               d.Icon,
		AccentColor:        d.AccentColor,
		ParentID:           d.ParentID,
		LeadAgentID:        d.LeadAgentID,
		SortOrder:          d.SortOrder,
		DirectAgentCount:   directAgentCount,
		SubdepartmentCount: subdepartmentCount,
		MemberCount:        memberCount,
		Createtime:         d.Createtime,
		Updatetime:         d.Updatetime,
	}
	if lead != nil {
		item.LeadAgentName = lead.Name
	}
	return item
}
