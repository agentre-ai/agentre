package orch_entity

// Task 状态：与 Dispatch 的生命周期无关，只是一份待办清单条目的完成态。
const (
	TaskStatusPending    = "pending"
	TaskStatusInProgress = "in_progress"
	TaskStatusDone       = "done"
)

// Task 编排待办清单条目(与派发节点 Dispatch 无关的协作白板)。
type Task struct {
	ID               int64  `gorm:"column:id;primaryKey;autoIncrement"`
	RunID            int64  `gorm:"column:run_id;type:bigint;not null;default:0"`
	Seq              int    `gorm:"column:seq;type:int;not null;default:0"`
	Text             string `gorm:"column:text;type:text;not null;default:''"`
	Status           string `gorm:"column:status;type:text;not null;default:'pending'"`
	AssigneeAgentID  int64  `gorm:"column:assignee_agent_id;type:bigint;not null;default:0"`
	CreatedByAgentID int64  `gorm:"column:created_by_agent_id;type:bigint;not null;default:0"`
	Createtime       int64  `gorm:"column:createtime;type:bigint;not null;default:0"`
	Updatetime       int64  `gorm:"column:updatetime;type:bigint;not null;default:0"`
}

func (*Task) TableName() string { return "orch_tasks" }

func ValidTaskStatus(s string) bool {
	return s == TaskStatusPending || s == TaskStatusInProgress || s == TaskStatusDone
}
