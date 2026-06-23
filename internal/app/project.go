package app

import (
	"github.com/agentre-ai/agentre/internal/model/entity/chat_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/project_entity"
	"github.com/agentre-ai/agentre/internal/service/project_location_svc"
	"github.com/agentre-ai/agentre/internal/service/project_svc"
)

// ProjectCreateRequest 前端新建项目入参，与 project_svc.CreateProjectRequest
// 同构 —— 单独定义是为了让 wails 把字段名小写化序列化到 TS。
type ProjectCreateRequest struct {
	ParentID        int64   `json:"parentID"`
	Name            string  `json:"name"`
	Icon            string  `json:"icon"`
	Color           string  `json:"color"`
	Description     string  `json:"description"`
	Path            string  `json:"path"`
	InitialAgentIDs []int64 `json:"initialAgentIDs"`
}

// ProjectUpdateRequest 更新项目入参。
type ProjectUpdateRequest struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Icon        string `json:"icon"`
	Color       string `json:"color"`
	Description string `json:"description"`
}

// ProjectReorderRequest 调整同级项目展示顺序。
type ProjectReorderRequest struct {
	ParentID   int64   `json:"parentID"`
	OrderedIDs []int64 `json:"orderedIDs"`
}

// ProjectItem 项目摘要 —— 树节点 / 列表 / 详情共用。
type ProjectItem struct {
	ID          int64  `json:"id"`
	ParentID    int64  `json:"parentID"`
	Name        string `json:"name"`
	Icon        string `json:"icon"`
	Color       string `json:"color"`
	Description string `json:"description"`
	Path        string `json:"path"`
	IsGitRepo   bool   `json:"isGitRepo"`
	SortOrder   int    `json:"sortOrder"`
	Createtime  int64  `json:"createtime"`
	Updatetime  int64  `json:"updatetime"`
}

// ProjectTreeNode 项目树节点；前端递归渲染。
type ProjectTreeNode struct {
	Project  *ProjectItem       `json:"project"`
	Children []*ProjectTreeNode `json:"children"`
}

// ProjectMemberItem 项目成员视图（含继承标记）。
type ProjectMemberItem struct {
	AgentID       int64  `json:"agentID"`
	JoinedAt      int64  `json:"joinedAt"`
	FromProjectID int64  `json:"fromProjectID"`
	FromName      string `json:"fromName"`
	Inherited     bool   `json:"inherited"`
	AgentName     string `json:"agentName"`
	AvatarColor   string `json:"avatarColor"`
	AvatarIcon    string `json:"avatarIcon"`
	AvatarDataURL string `json:"avatarDataUrl"`
}

// ProjectDetailResponse Get(id) 的前端 DTO。
type ProjectDetailResponse struct {
	Project          *ProjectItem         `json:"project"`
	DirectMembers    []*ProjectMemberItem `json:"directMembers"`
	InheritedMembers []*ProjectMemberItem `json:"inheritedMembers"`
}

// ProjectSessionItem 项目下会话摘要。
type ProjectSessionItem struct {
	ID             int64  `json:"id"`
	AgentID        int64  `json:"agentID"`
	Title          string `json:"title"`
	AgentStatus    string `json:"agentStatus"`
	LastMessageAt  int64  `json:"lastMessageAt"`
	LastReadAt     int64  `json:"lastReadAt"`
	NeedsAttention bool   `json:"needsAttention"`
}

// ProjectGitRepoInfo 路径下 git 探测结果。
type ProjectGitRepoInfo struct {
	IsGitRepo     bool   `json:"isGitRepo"`
	CurrentBranch string `json:"currentBranch"`
	Origin        string `json:"origin"`
}

// ProjectCreate 创建项目。
func (a *App) ProjectCreate(req *ProjectCreateRequest) (*ProjectItem, error) {
	p, err := project_svc.Default().Create(a.ctx, &project_svc.CreateProjectRequest{
		ParentID:        req.ParentID,
		Name:            req.Name,
		Icon:            req.Icon,
		Color:           req.Color,
		Description:     req.Description,
		Path:            req.Path,
		InitialAgentIDs: req.InitialAgentIDs,
	})
	if err != nil {
		return nil, err
	}
	return toProjectItem(p), nil
}

// ProjectUpdate 更新项目基本信息（不含 parent_id 移动）。
func (a *App) ProjectUpdate(req *ProjectUpdateRequest) (*ProjectItem, error) {
	p, err := project_svc.Default().Update(a.ctx, &project_svc.UpdateProjectRequest{
		ID:          req.ID,
		Name:        req.Name,
		Icon:        req.Icon,
		Color:       req.Color,
		Description: req.Description,
	})
	if err != nil {
		return nil, err
	}
	return toProjectItem(p), nil
}

// ProjectReorder 持久化同一父项目下的项目顺序。
func (a *App) ProjectReorder(req *ProjectReorderRequest) error {
	var svcReq *project_svc.ReorderProjectsRequest
	if req != nil {
		svcReq = &project_svc.ReorderProjectsRequest{
			ParentID:   req.ParentID,
			OrderedIDs: req.OrderedIDs,
		}
	}
	return project_svc.Default().Reorder(a.ctx, svcReq)
}

// ProjectDelete 软删除项目；有子项目 / 活跃会话时拒绝。
func (a *App) ProjectDelete(id int64) error {
	return project_svc.Default().Delete(a.ctx, id)
}

// ProjectGet 拉单个项目详情 + 成员（直接 + 继承）。
func (a *App) ProjectGet(id int64) (*ProjectDetailResponse, error) {
	detail, err := project_svc.Default().Get(a.ctx, id)
	if err != nil {
		return nil, err
	}
	return &ProjectDetailResponse{
		Project:          toProjectItem(detail.Project),
		DirectMembers:    toProjectMembers(detail.DirectMembers, false),
		InheritedMembers: toProjectMembers(detail.InheritedMembers, true),
	}, nil
}

// ProjectListTree 返回完整项目树。
func (a *App) ProjectListTree() ([]*ProjectTreeNode, error) {
	roots, err := project_svc.Default().ListTree(a.ctx)
	if err != nil {
		return nil, err
	}
	return toProjectTreeNodes(roots), nil
}

// ProjectAddMember 向项目追加一个直接成员。
func (a *App) ProjectAddMember(projectID, agentID int64) error {
	return project_svc.Default().AddMember(a.ctx, projectID, agentID)
}

// ProjectRemoveMember 移除项目直接成员（继承成员需要去父项目移除）。
func (a *App) ProjectRemoveMember(projectID, agentID int64) error {
	return project_svc.Default().RemoveMember(a.ctx, projectID, agentID)
}

// ProjectLocationList 列出某项目在所有远端设备上配置的路径。
func (a *App) ProjectLocationList(projectID int64) ([]*project_location_svc.ProjectLocationView, error) {
	return project_location_svc.Default().ListByProject(a.ctx, projectID)
}

// ProjectLocationUpsert 添加 / 更新某项目在某远端设备上的路径（deviceID = paired_agentred.id 字符串化）。
func (a *App) ProjectLocationUpsert(projectID int64, deviceID, path string) (*project_location_svc.ProjectLocationView, error) {
	return project_location_svc.Default().Upsert(a.ctx, projectID, deviceID, path)
}

// ProjectLocationRemove 删除某项目在某远端设备上的路径（软删，status=DELETE）。
func (a *App) ProjectLocationRemove(projectID int64, deviceID string) error {
	return project_location_svc.Default().RemoveByProjectAndDevice(a.ctx, projectID, deviceID)
}

// ProjectListSessions 项目下未软删除的会话列表。
func (a *App) ProjectListSessions(projectID int64) ([]*ProjectSessionItem, error) {
	rows, err := project_svc.Default().ListSessions(a.ctx, projectID)
	if err != nil {
		return nil, err
	}
	return toProjectSessionItems(rows), nil
}

// ProjectDetectGitRepo 新建项目模态用：选完目录后探测一次 git 仓库状态。
func (a *App) ProjectDetectGitRepo(path string) (*ProjectGitRepoInfo, error) {
	info, err := project_svc.Default().DetectGitRepo(a.ctx, path)
	if err != nil {
		return nil, err
	}
	return &ProjectGitRepoInfo{
		IsGitRepo:     info.IsGitRepo,
		CurrentBranch: info.CurrentBranch,
		Origin:        info.Origin,
	}, nil
}

// toProjectItem 把 entity 转 wails-frontend DTO。
func toProjectItem(p *project_entity.Project) *ProjectItem {
	if p == nil {
		return nil
	}
	return &ProjectItem{
		ID:          p.ID,
		ParentID:    p.ParentID,
		Name:        p.Name,
		Icon:        p.Icon,
		Color:       p.Color,
		Description: p.Description,
		Path:        p.Path,
		IsGitRepo:   p.IsGitRepo(),
		SortOrder:   p.SortOrder,
		Createtime:  p.Createtime,
		Updatetime:  p.Updatetime,
	}
}

func toProjectMembers(ms []*project_svc.ProjectAgentMember, inherited bool) []*ProjectMemberItem {
	out := make([]*ProjectMemberItem, 0, len(ms))
	for _, m := range ms {
		out = append(out, &ProjectMemberItem{
			AgentID:       m.AgentID,
			JoinedAt:      m.JoinedAt,
			FromProjectID: m.FromProjectID,
			FromName:      m.FromName,
			Inherited:     inherited,
			AgentName:     m.AgentName,
			AvatarColor:   m.AvatarColor,
			AvatarIcon:    m.AvatarIcon,
			AvatarDataURL: m.AvatarDataURL,
		})
	}
	return out
}

func toProjectTreeNodes(nodes []*project_svc.ProjectNode) []*ProjectTreeNode {
	out := make([]*ProjectTreeNode, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, &ProjectTreeNode{
			Project:  toProjectItem(n.Project),
			Children: toProjectTreeNodes(n.Children),
		})
	}
	return out
}

func toProjectSessionItems(rows []*chat_entity.Session) []*ProjectSessionItem {
	out := make([]*ProjectSessionItem, 0, len(rows))
	for _, s := range rows {
		out = append(out, &ProjectSessionItem{
			ID:             s.ID,
			AgentID:        s.AgentID,
			Title:          s.Title,
			AgentStatus:    s.AgentStatus,
			LastMessageAt:  s.LastMessageAt,
			LastReadAt:     s.LastReadAt,
			NeedsAttention: s.IsWaitingForUser(),
		})
	}
	return out
}
