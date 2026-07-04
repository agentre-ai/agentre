package orch_entity

// Task Kind：新节点/边只来自 dispatch；ask 是平级问答。
const (
	TaskKindDispatch = "dispatch"
	TaskKindAsk      = "ask"
)

// Task 客观生命周期状态（驱动 UI 颜色/计数；语义结果走 Result 自由文本，不是状态）。
const (
	TaskPending          = "pending"
	TaskRunning          = "running"
	TaskAwaitingChildren = "awaiting-children" // 等子任务回报
	TaskAwaitingUser     = "awaiting-user"     // 等你审批/回复（唯一琥珀）
	TaskDone             = "done"
	TaskCanceled         = "canceled"
	TaskPaused           = "paused"
	TaskError            = "error" // 技术崩溃（运行时退出/不可恢复异常）
)

// Task 编排树上的一个任务。
type Task struct {
	ID           int64  `gorm:"column:id;primaryKey;autoIncrement"`
	RunID        int64  `gorm:"column:run_id;type:bigint;not null;default:0"`
	AgentID      int64  `gorm:"column:agent_id;type:bigint;not null;default:0"`
	SessionID    int64  `gorm:"column:session_id;type:bigint;not null;default:0"` // 复用 chat_entity.Session
	ParentTaskID int64  `gorm:"column:parent_task_id;type:bigint;not null;default:0"`
	Kind         string `gorm:"column:kind;type:text;not null;default:'dispatch'"`
	Status       string `gorm:"column:status;type:text;not null;default:'pending'"`
	Brief        string `gorm:"column:brief;type:text;not null;default:''"`
	Result       string `gorm:"column:result;type:text;not null;default:''"`  // agent 自报语义报告（完整正文，供 read）
	Summary      string `gorm:"column:summary;type:text;not null;default:''"` // 显式小结（仅 finish/report 写；非空=主动汇报）
	CallSeq      int    `gorm:"column:call_seq;type:int;not null;default:0"`  // 同 agent 第几次被 dispatch
	Refs         string `gorm:"column:refs;type:text;not null;default:''"`    // JSON：被引用的产物/任务
	NodeRef      string `gorm:"column:node_ref;type:text;not null;default:''"` // 对应的流程节点 label(Leader 打标)；空=未打标
	Createtime   int64  `gorm:"column:createtime;type:bigint;not null;default:0"`
	Updatetime   int64  `gorm:"column:updatetime;type:bigint;not null;default:0"`
}

func (*Task) TableName() string { return "orch_tasks" }

// IsTerminal 是否到达终态。
func (t *Task) IsTerminal() bool {
	return t != nil && (t.Status == TaskDone || t.Status == TaskCanceled || t.Status == TaskError)
}

// IsWaitingUser 是否在等你（审批/回复）。
func (t *Task) IsWaitingUser() bool { return t != nil && t.Status == TaskAwaitingUser }

// IsActive 是否仍活着（非终态）。
func (t *Task) IsActive() bool { return t != nil && !t.IsTerminal() }
