package orgtool_svc

// 写工具参数 struct。指针字段用于区分"不传(沿用现值)"与"显式清零/挪顶级";
// 非指针零值字段统一按"不变"处理(execWriteTool 先 Load 现值再 merge)。

type createDepartmentArgs struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	ParentID    int64  `json:"parentId"`
}

type updateDepartmentArgs struct { // 零值字段=不变;指针区分"不动"与"清零/挪顶级"
	ID          int64   `json:"id"`
	Name        string  `json:"name"`
	Description *string `json:"description"`
	LeadAgentID *int64  `json:"leadAgentId"`
	ParentID    *int64  `json:"parentId"`
}

type deleteDepartmentArgs struct {
	ID       int64  `json:"id"`
	Strategy string `json:"strategy"` // reparent(默认)|cascade
}

type createAgentArgs struct {
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	DepartmentID  int64    `json:"departmentId"`
	ParentAgentID int64    `json:"parentAgentId"`
	BackendID     int64    `json:"backendId"` // 0=继承调用者 agent 的 backend
	Prompt        []string `json:"prompt"`
}

type updateAgentArgs struct {
	ID            int64    `json:"id"`
	Name          string   `json:"name"`
	Description   *string  `json:"description"`
	Prompt        []string `json:"prompt"`       // nil=不变;空数组=清空
	DepartmentID  *int64   `json:"departmentId"` // 与 ParentAgentID 互斥;非 nil 且变化 → Move
	ParentAgentID *int64   `json:"parentAgentId"`
}

type deleteAgentArgs struct {
	ID int64 `json:"id"`
}
