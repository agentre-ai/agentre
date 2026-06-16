package agent_svc

import (
	"context"
	"encoding/base64"
	"strings"
	"time"

	"github.com/cago-frame/cago/database/db"
	"github.com/cago-frame/cago/pkg/consts"
	"github.com/cago-frame/cago/pkg/i18n"
	"gorm.io/gorm"

	"agentre/internal/model/entity/agent_entity"
	"agentre/internal/pkg/code"
	"agentre/internal/repository/agent_backend_repo"
	"agentre/internal/repository/agent_repo"
	"agentre/internal/repository/department_repo"
	"agentre/internal/service/department_svc"
)

const (
	// avatarMaxBytes 头像上传字节上限（解码后）。
	avatarMaxBytes = 2 * 1024 * 1024
)

// avatarDataURLPrefixes 允许的 data URL 前缀。
var avatarDataURLPrefixes = []string{
	"data:image/png;base64,",
	"data:image/jpeg;base64,",
	"data:image/webp;base64,",
}

// AgentSvc Agent 应用服务。
type AgentSvc interface {
	Create(ctx context.Context, req *CreateAgentRequest) (*CreateAgentResponse, error)
	Update(ctx context.Context, req *UpdateAgentRequest) (*UpdateAgentResponse, error)
	Move(ctx context.Context, req *MoveAgentRequest) (*MoveAgentResponse, error)
	Delete(ctx context.Context, req *DeleteAgentRequest) (*DeleteAgentResponse, error)
	UploadAvatar(ctx context.Context, req *UploadAvatarRequest) (*UploadAvatarResponse, error)
	DeleteAvatar(ctx context.Context, req *DeleteAvatarRequest) (*DeleteAvatarResponse, error)
}

type agentSvc struct {
	now func() int64
}

var defaultAgent AgentSvc = &agentSvc{now: func() int64 { return time.Now().Unix() }}

func Agent() AgentSvc { return defaultAgent }

func (s *agentSvc) Create(ctx context.Context, req *CreateAgentRequest) (*CreateAgentResponse, error) {
	now := s.now()
	a := &agent_entity.Agent{
		Name:           strings.TrimSpace(req.Name),
		Description:    strings.TrimSpace(req.Description),
		AvatarColor:    strings.TrimSpace(req.AvatarColor),
		AvatarIcon:     strings.TrimSpace(req.AvatarIcon),
		DepartmentID:   req.DepartmentID,
		ParentAgentID:  req.ParentAgentID,
		AgentBackendID: req.AgentBackendID,
		Status:         consts.ACTIVE,
		Createtime:     now,
		Updatetime:     now,
	}
	a.SetPrompt(req.Prompt)
	a.SetSkills(skillsFromDTO(req.Skills))
	if err := a.Check(ctx); err != nil {
		return nil, err
	}
	if err := s.requireActivePlacement(ctx, a.DepartmentID, a.ParentAgentID, 0); err != nil {
		return nil, err
	}
	if err := s.requireActiveBackend(ctx, a.AgentBackendID); err != nil {
		return nil, err
	}
	dup, err := agent_repo.Agent().FindByName(ctx, a.Name)
	if err != nil {
		return nil, err
	}
	if dup != nil {
		return nil, i18n.NewError(ctx, code.AgentNameDuplicated)
	}
	next, err := s.nextSortOrder(ctx, a.DepartmentID, a.ParentAgentID)
	if err != nil {
		return nil, err
	}
	a.SortOrder = next
	if err := agent_repo.Agent().Create(ctx, a); err != nil {
		return nil, err
	}
	return &CreateAgentResponse{Item: toItem(a)}, nil
}

func (s *agentSvc) Update(ctx context.Context, req *UpdateAgentRequest) (*UpdateAgentResponse, error) {
	existing, err := agent_repo.Agent().Find(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, i18n.NewError(ctx, code.AgentNotFound)
	}
	newName := strings.TrimSpace(req.Name)
	if newName != existing.Name {
		dup, err := agent_repo.Agent().FindByName(ctx, newName)
		if err != nil {
			return nil, err
		}
		if dup != nil && dup.ID != existing.ID {
			return nil, i18n.NewError(ctx, code.AgentNameDuplicated)
		}
	}
	existing.Name = newName
	existing.Description = strings.TrimSpace(req.Description)
	existing.AvatarColor = strings.TrimSpace(req.AvatarColor)
	existing.AvatarIcon = strings.TrimSpace(req.AvatarIcon)
	if req.AgentBackendID > 0 && req.AgentBackendID != existing.AgentBackendID {
		if err := s.requireActiveBackend(ctx, req.AgentBackendID); err != nil {
			return nil, err
		}
		existing.AgentBackendID = req.AgentBackendID
	}
	existing.SetPrompt(req.Prompt)
	existing.SetSkills(skillsFromDTO(req.Skills))
	existing.Updatetime = s.now()
	if err := existing.Check(ctx); err != nil {
		return nil, err
	}
	if err := agent_repo.Agent().Update(ctx, existing); err != nil {
		return nil, err
	}
	return &UpdateAgentResponse{Item: toItem(existing)}, nil
}

func (s *agentSvc) Move(ctx context.Context, req *MoveAgentRequest) (*MoveAgentResponse, error) {
	existing, err := agent_repo.Agent().Find(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, i18n.NewError(ctx, code.AgentNotFound)
	}
	if existing.IsSystem() {
		return nil, i18n.NewError(ctx, code.AgentSystemImmutable)
	}
	if err := s.requireActivePlacement(ctx, req.NewDepartmentID, req.NewParentAgentID, existing.ID); err != nil {
		return nil, err
	}
	sortOrder := req.NewSortOrder
	if sortOrder <= 0 {
		next, err := s.nextSortOrder(ctx, req.NewDepartmentID, req.NewParentAgentID)
		if err != nil {
			return nil, err
		}
		sortOrder = next
	}
	if err := agent_repo.Agent().UpdatePlacement(ctx, existing.ID, req.NewDepartmentID, req.NewParentAgentID, sortOrder); err != nil {
		return nil, err
	}
	existing.DepartmentID = req.NewDepartmentID
	existing.ParentAgentID = req.NewParentAgentID
	existing.SortOrder = sortOrder
	return &MoveAgentResponse{Item: toItem(existing)}, nil
}

func (s *agentSvc) Delete(ctx context.Context, req *DeleteAgentRequest) (*DeleteAgentResponse, error) {
	existing, err := agent_repo.Agent().Find(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, i18n.NewError(ctx, code.AgentNotFound)
	}
	if existing.IsSystem() {
		return nil, i18n.NewError(ctx, code.AgentSystemImmutable)
	}
	err = db.Ctx(ctx).Transaction(func(tx *gorm.DB) error {
		txCtx := db.WithContextDB(ctx, tx)
		if err := agent_repo.Agent().ClearLeadOfDepartment(txCtx, existing.ID); err != nil {
			return err
		}
		if err := agent_repo.Agent().ReparentChildren(txCtx, existing.ID, existing.DepartmentID, existing.ParentAgentID); err != nil {
			return err
		}
		return agent_repo.Agent().Delete(txCtx, existing.ID)
	})
	if err != nil {
		return nil, err
	}
	return &DeleteAgentResponse{}, nil
}

func (s *agentSvc) UploadAvatar(ctx context.Context, req *UploadAvatarRequest) (*UploadAvatarResponse, error) {
	existing, err := agent_repo.Agent().Find(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, i18n.NewError(ctx, code.AgentNotFound)
	}
	if err := validateAvatarDataURL(ctx, req.DataURL); err != nil {
		return nil, err
	}
	existing.AvatarDataURL = req.DataURL
	existing.Updatetime = s.now()
	if err := agent_repo.Agent().UpdateAvatar(ctx, existing.ID, req.DataURL, existing.Updatetime); err != nil {
		return nil, err
	}
	return &UploadAvatarResponse{Item: toItem(existing)}, nil
}

func (s *agentSvc) DeleteAvatar(ctx context.Context, req *DeleteAvatarRequest) (*DeleteAvatarResponse, error) {
	existing, err := agent_repo.Agent().Find(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, i18n.NewError(ctx, code.AgentNotFound)
	}
	existing.AvatarDataURL = ""
	existing.Updatetime = s.now()
	if err := agent_repo.Agent().UpdateAvatar(ctx, existing.ID, "", existing.Updatetime); err != nil {
		return nil, err
	}
	return &DeleteAvatarResponse{Item: toItem(existing)}, nil
}

func validateAvatarDataURL(ctx context.Context, dataURL string) error {
	if dataURL == "" {
		return i18n.NewError(ctx, code.AgentAvatarInvalid)
	}
	var payload string
	matched := false
	for _, prefix := range avatarDataURLPrefixes {
		if strings.HasPrefix(dataURL, prefix) {
			payload = dataURL[len(prefix):]
			matched = true
			break
		}
	}
	if !matched {
		return i18n.NewError(ctx, code.AgentAvatarInvalid)
	}
	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return i18n.NewError(ctx, code.AgentAvatarInvalid)
	}
	if len(decoded) > avatarMaxBytes {
		return i18n.NewError(ctx, code.AgentAvatarTooLarge)
	}
	return nil
}

func (s *agentSvc) requireActiveDepartment(ctx context.Context, id int64) error {
	if id <= 0 {
		return i18n.NewError(ctx, code.AgentDepartmentRequired)
	}
	d, err := department_repo.Department().Find(ctx, id)
	if err != nil {
		return err
	}
	if d == nil {
		return i18n.NewError(ctx, code.AgentDepartmentNotFound)
	}
	if !d.IsActive() {
		return i18n.NewError(ctx, code.AgentDepartmentInactive)
	}
	return nil
}

func (s *agentSvc) requireActivePlacement(ctx context.Context, departmentID, parentAgentID, movingAgentID int64) error {
	hasDepartment := departmentID > 0
	hasParentAgent := parentAgentID > 0
	if !hasDepartment && !hasParentAgent {
		return i18n.NewError(ctx, code.AgentDepartmentRequired)
	}
	if hasDepartment && hasParentAgent {
		return i18n.NewError(ctx, code.InvalidParameter)
	}
	if hasDepartment {
		return s.requireActiveDepartment(ctx, departmentID)
	}

	parent, err := agent_repo.Agent().Find(ctx, parentAgentID)
	if err != nil {
		return err
	}
	if parent == nil || !parent.IsActive() {
		return i18n.NewError(ctx, code.AgentParentNotFound)
	}
	if movingAgentID > 0 {
		if parentAgentID == movingAgentID {
			return i18n.NewError(ctx, code.AgentCircularReference)
		}
		all, err := agent_repo.Agent().List(ctx)
		if err != nil {
			return err
		}
		if hasAgentCycle(all, parentAgentID, movingAgentID) {
			return i18n.NewError(ctx, code.AgentCircularReference)
		}
	}
	return nil
}

func (s *agentSvc) nextSortOrder(ctx context.Context, departmentID, parentAgentID int64) (int, error) {
	if parentAgentID > 0 {
		return agent_repo.Agent().NextSortOrderByParent(ctx, parentAgentID)
	}
	return agent_repo.Agent().NextSortOrder(ctx, departmentID)
}

func hasAgentCycle(all []*agent_entity.Agent, startParentID, selfID int64) bool {
	index := make(map[int64]*agent_entity.Agent, len(all))
	for _, a := range all {
		index[a.ID] = a
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
		cur = next.ParentAgentID
	}
	return false
}

func (s *agentSvc) requireActiveBackend(ctx context.Context, id int64) error {
	if id <= 0 {
		return nil // 后端可选，0 表示未配置
	}
	b, err := agent_backend_repo.AgentBackend().Find(ctx, id)
	if err != nil {
		return err
	}
	if b == nil || !b.IsActive() {
		return i18n.NewError(ctx, code.AgentBackendInvalidRef)
	}
	return nil
}

func skillsFromDTO(items []department_svc.AgentSkillDTO) []agent_entity.AgentSkillItem {
	out := make([]agent_entity.AgentSkillItem, 0, len(items))
	for _, s := range items {
		out = append(out, agent_entity.AgentSkillItem{Label: s.Label, Enabled: s.Enabled})
	}
	return out
}

func toItem(a *agent_entity.Agent) *AgentItem {
	rawSkills := a.GetSkills()
	skills := make([]department_svc.AgentSkillDTO, 0, len(rawSkills))
	for _, s := range rawSkills {
		skills = append(skills, department_svc.AgentSkillDTO{Label: s.Label, Enabled: s.Enabled})
	}
	return &AgentItem{
		ID:             a.ID,
		Name:           a.Name,
		Description:    a.Description,
		AvatarColor:    a.AvatarColor,
		AvatarIcon:     a.AvatarIcon,
		AvatarDataURL:  a.AvatarDataURL,
		SystemBadge:    a.SystemBadge,
		DepartmentID:   a.DepartmentID,
		ParentAgentID:  a.ParentAgentID,
		AgentBackendID: a.AgentBackendID,
		SortOrder:      a.SortOrder,
		Prompt:         a.GetPrompt(),
		Skills:         skills,
		Createtime:     a.Createtime,
		Updatetime:     a.Updatetime,
	}
}
