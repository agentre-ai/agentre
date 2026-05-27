package project_entity

// ProjectAgent 是 Project ↔ Agent 多对多成员关系的关联行。
//
// 仅存「直接成员」；父项目成员**只读继承**到子项目，继承在查询时按 parent_id 链
// 上溯聚合（spec §3.3 决议 3），不入库以避免父改后子副本不同步。
type ProjectAgent struct {
	ProjectID int64 `gorm:"column:project_id;primaryKey"`
	AgentID   int64 `gorm:"column:agent_id;primaryKey"`
	JoinedAt  int64 `gorm:"column:joined_at;type:bigint;not null;default:0"`
}

func (*ProjectAgent) TableName() string { return "project_agents" }
