package workflowtool_svc

// 写工具参数 struct。update 用指针区分"不传(沿用现值)"与"显式改"。
type createWorkflowArgs struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

type updateWorkflowArgs struct {
	ID      int64   `json:"id"`
	Name    *string `json:"name"`    // nil=不变
	Content *string `json:"content"` // nil=不变
}

type deleteWorkflowArgs struct {
	ID int64 `json:"id"`
}
