package agent_entity

// AgentExecTarget Agent 有序执行目标列表里的一项（R15）。列表的每一项是一个
// backend；backend 自己的 device_id 决定这一档落在哪台机器上（空 = 当前桌面端，
// 非空 = 那台 agentred）。
type AgentExecTarget struct {
	ID             int64 `gorm:"column:id;primaryKey;autoIncrement"`
	AgentID        int64 `gorm:"column:agent_id;type:bigint;not null"`
	AgentBackendID int64 `gorm:"column:agent_backend_id;type:bigint;not null"`
	SortOrder      int   `gorm:"column:sort_order;type:int;not null;default:0"`
}

func (*AgentExecTarget) TableName() string { return "agent_exec_targets" }
