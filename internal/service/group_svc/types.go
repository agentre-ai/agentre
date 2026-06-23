package group_svc

import (
	"github.com/agentre-ai/agentre/internal/model/entity/group_entity"
)

type CreateGroupRequest struct {
	Title        string
	HostAgentID  int64
	DepartmentID int64
	ProjectID    int64
	// WorkflowID 可选绑定的协作流程(剧本库),0=不绑定;主持人每轮注入最新内容。
	WorkflowID int64
	// MemberAgentIDs 建群时一并拉入的初始成员（主持人之外）。每个都经 backendSupportsGroup
	// 门控 + 幂等（ensureMember）。
	MemberAgentIDs []int64
}

type GroupDetail struct {
	Group    *group_entity.Group
	Members  []*group_entity.GroupMember
	Messages []*group_entity.GroupMessage
	// MemberRunStates 是成员级运行态快照(memberID -> RunStateRunning/RunStateIdle),
	// 由调度器在跑集合派生 —— 区别于 GroupMember.Status 的"成员身份"(active/left)。
	// 让 roster 在打开群时(中途/重载)能立刻显示哪个成员正在跑,而不只是"是不是成员"。
	MemberRunStates map[int64]string
	// Tasks 全部任务卡(任务 tab 与历史卡片状态回写的数据源)。
	Tasks []*group_entity.GroupTask
}

type SendGroupMessageRequest struct {
	GroupID            int64
	Text               string
	RecipientMemberIDs []int64 // 可选: 显式收件人(优先于解析)
	ToUser             bool
}

// InviteResult 是 group_invite 成功拉入的一个成员(id + 显示名),回给主持人 turn。
type InviteResult struct {
	AgentID int64
	Name    string
}

const maxMembers = 8
