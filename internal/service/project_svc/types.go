// Package project_svc 提供 Project 模块的业务逻辑层。
//
// Project 是「工作上下文」一等公民：名字 + 本地路径 + 成员 Agent。
// 详细设计见 docs/superpowers/specs/2026-05-19-project-module-design.md。
package project_svc

import "github.com/agentre-ai/agentre/internal/model/entity/project_entity"

// CreateProjectRequest 新建项目入参。
//
// Path 必填、绝对路径；Git 仓库检测由 ProjectDetectGitRepo 完成。
type CreateProjectRequest struct {
	ParentID    int64
	Name        string
	Icon        string
	Color       string
	Description string
	Path        string
	// InitialAgentIDs 创建后立即写入 project_agents 的直接成员；可为空。
	InitialAgentIDs []int64
}

// UpdateProjectRequest 更新项目入参。
//
// 不允许跨树移动（改 ParentID）—— 单独走 Move 接口；当前 spec 留作下次。
type UpdateProjectRequest struct {
	ID          int64
	Name        string
	Icon        string
	Color       string
	Description string
}

// ReorderProjectsRequest 调整同一 parent 下项目展示顺序。
type ReorderProjectsRequest struct {
	ParentID   int64
	OrderedIDs []int64
}

// ProjectAgentMember 项目成员视图，区分直接成员 vs 继承成员。
type ProjectAgentMember struct {
	AgentID       int64
	JoinedAt      int64
	FromProjectID int64  // 继承来源；== ProjectDetail.ID 时即直接成员
	FromName      string // 继承来源项目名；直接成员留空
	AgentName     string
	AvatarColor   string
	AvatarIcon    string
	AvatarDataURL string
}

// ProjectDetail Get() 返回的项目详情 + 成员列表。
type ProjectDetail struct {
	Project          *project_entity.Project
	DirectMembers    []*ProjectAgentMember
	InheritedMembers []*ProjectAgentMember
}

// ProjectNode 项目树节点 —— ListTree() 返回的形态，子项目嵌套挂在 Children。
type ProjectNode struct {
	Project  *project_entity.Project
	Children []*ProjectNode
}

// GitRepoInfo 路径下 Git 仓库探测结果，新建项目模态用。
type GitRepoInfo struct {
	IsGitRepo     bool
	CurrentBranch string
	Origin        string
}
