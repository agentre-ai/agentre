package orch_entity

// Dispatch Kind：新节点/边只来自 dispatch；ask 是平级问答。
const (
	DispatchKindDispatch = "dispatch"
	DispatchKindAsk      = "ask"
)

// Dispatch 客观生命周期状态（驱动 UI 颜色/计数；语义结果走 Result 自由文本，不是状态）。
const (
	DispatchPending          = "pending"
	DispatchRunning          = "running"
	DispatchAwaitingChildren = "awaiting-children" // 等子任务回报
	DispatchAwaitingUser     = "awaiting-user"     // 等你审批/回复（唯一琥珀）
	DispatchDone             = "done"
	DispatchCanceled         = "canceled"
	DispatchPaused           = "paused"
	DispatchError            = "error" // 技术崩溃（运行时退出/不可恢复异常）
)

// Dispatch 编排树上的一次派发：把活儿派给某 agent、绑一条子会话，即执行树上的一个节点。
type Dispatch struct {
	ID               int64  `gorm:"column:id;primaryKey;autoIncrement"`
	RunID            int64  `gorm:"column:run_id;type:bigint;not null;default:0"`
	AgentID          int64  `gorm:"column:agent_id;type:bigint;not null;default:0"`
	SessionID        int64  `gorm:"column:session_id;type:bigint;not null;default:0"` // 复用 chat_entity.Session
	ParentDispatchID int64  `gorm:"column:parent_dispatch_id;type:bigint;not null;default:0"`
	Kind             string `gorm:"column:kind;type:text;not null;default:'dispatch'"`
	Status           string `gorm:"column:status;type:text;not null;default:'pending'"`
	Brief            string `gorm:"column:brief;type:text;not null;default:''"`
	Result           string `gorm:"column:result;type:text;not null;default:''"`  // agent 自报语义报告（完整正文，供 read）
	Summary          string `gorm:"column:summary;type:text;not null;default:''"` // 显式小结（仅 finish/report 写；非空=主动汇报）
	CallSeq          int    `gorm:"column:call_seq;type:int;not null;default:0"`  // 同 agent 第几次被 dispatch
	Refs             string `gorm:"column:refs;type:text;not null;default:''"`    // JSON：被引用的产物/任务
	Createtime       int64  `gorm:"column:createtime;type:bigint;not null;default:0"`
	Updatetime       int64  `gorm:"column:updatetime;type:bigint;not null;default:0"`
}

func (*Dispatch) TableName() string { return "orch_dispatches" }

// IsTerminal 是否到达终态。
func (t *Dispatch) IsTerminal() bool {
	return t != nil && (t.Status == DispatchDone || t.Status == DispatchCanceled || t.Status == DispatchError)
}

// IsWaitingUser 是否在等你（审批/回复）。
func (t *Dispatch) IsWaitingUser() bool { return t != nil && t.Status == DispatchAwaitingUser }

// IsActive 是否仍活着（非终态）。
func (t *Dispatch) IsActive() bool { return t != nil && !t.IsTerminal() }
