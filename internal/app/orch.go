package app

import (
	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
	"github.com/agentre-ai/agentre/internal/service/orch_svc"
)

// RunItemDTO 编排 Run 列表条目。
type RunItemDTO struct {
	ID            int64  `json:"id"`
	Goal          string `json:"goal"`
	LeaderAgentID int64  `json:"leaderAgentId"`
	Status        string `json:"status"`
	ProjectID     int64  `json:"projectId"`
	// FlowID 关联的编排流程库 ID（0=临时/无）。
	FlowID      int64  `json:"flowId"`
	// FlowContent 创建时快照的流程正文。
	FlowContent string `json:"flowContent"`
	// RootTaskID 根 Task 的 ID。
	RootTaskID  int64  `json:"rootTaskId"`
	Createtime  int64  `json:"createtime"`
	Updatetime  int64  `json:"updatetime"`
}

// TaskDTO 编排 Task 条目。
type TaskDTO struct {
	ID           int64  `json:"id"`
	RunID        int64  `json:"runId"`
	AgentID      int64  `json:"agentId"`
	SessionID    int64  `json:"sessionId"`
	ParentTaskID int64  `json:"parentTaskId"`
	Kind         string `json:"kind"`
	Status       string `json:"status"`
	Brief        string `json:"brief"`
	Result       string `json:"result"`
	CallSeq      int    `json:"callSeq"`
	// Refs JSON 格式的被引用产物/任务列表。
	Refs       string `json:"refs"`
	Createtime int64  `json:"createtime"`
	Updatetime int64  `json:"updatetime"`
}

// RunDetailDTO 编排 Run 详情（Run + 全部 Task）。
type RunDetailDTO struct {
	Run   *RunItemDTO `json:"run"`
	Tasks []*TaskDTO  `json:"tasks"`
}

// RunCreateRequest 创建编排 Run 的前端请求。
type RunCreateRequest struct {
	Goal            string  `json:"goal"`
	LeaderAgentID   int64   `json:"leaderAgentId"`
	FlowID          int64   `json:"flowId"`
	FlowContent     string  `json:"flowContent"`
	ProjectID       int64   `json:"projectId"`
	AllowedAgentIDs []int64 `json:"allowedAgentIds"`
}

func toRunItem(r *orch_entity.OrchestrationRun) *RunItemDTO {
	return &RunItemDTO{
		ID:            r.ID,
		Goal:          r.Goal,
		LeaderAgentID: r.LeaderAgentID,
		Status:        r.Status,
		ProjectID:     r.ProjectID,
		FlowID:        r.FlowID,
		FlowContent:   r.FlowContent,
		RootTaskID:    r.RootTaskID,
		Createtime:    r.Createtime,
		Updatetime:    r.Updatetime,
	}
}

func toTaskDTO(t *orch_entity.Task) *TaskDTO {
	return &TaskDTO{
		ID:           t.ID,
		RunID:        t.RunID,
		AgentID:      t.AgentID,
		SessionID:    t.SessionID,
		ParentTaskID: t.ParentTaskID,
		Kind:         t.Kind,
		Status:       t.Status,
		Brief:        t.Brief,
		Result:       t.Result,
		CallSeq:      t.CallSeq,
		Refs:         t.Refs,
		Createtime:   t.Createtime,
		Updatetime:   t.Updatetime,
	}
}

// RunCreate 创建编排 Run，返回 Run 详情（含根 Task）。
func (a *App) RunCreate(req *RunCreateRequest) (*RunDetailDTO, error) {
	d, err := orch_svc.Default().CreateRun(a.ctx, &orch_svc.CreateRunRequest{
		Goal:            req.Goal,
		LeaderAgentID:   req.LeaderAgentID,
		FlowID:          req.FlowID,
		FlowContent:     req.FlowContent,
		ProjectID:       req.ProjectID,
		AllowedAgentIDs: req.AllowedAgentIDs,
	})
	if err != nil {
		return nil, err
	}
	return &RunDetailDTO{Run: toRunItem(d.Run), Tasks: []*TaskDTO{toTaskDTO(d.RootTask)}}, nil
}

// RunList 返回全部 Run 列表。
func (a *App) RunList() ([]*RunItemDTO, error) {
	rs, err := orch_svc.Default().ListRuns(a.ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*RunItemDTO, 0, len(rs))
	for _, r := range rs {
		out = append(out, toRunItem(r))
	}
	return out, nil
}

// RunLoad 加载指定 Run 的详情（Run + 全部 Task）。
func (a *App) RunLoad(id int64) (*RunDetailDTO, error) {
	d, err := orch_svc.Default().LoadRun(a.ctx, id)
	if err != nil {
		return nil, err
	}
	tasks := make([]*TaskDTO, 0, len(d.Tasks))
	for _, t := range d.Tasks {
		tasks = append(tasks, toTaskDTO(t))
	}
	return &RunDetailDTO{Run: toRunItem(d.Run), Tasks: tasks}, nil
}

// RunPause 暂停指定 Run。
func (a *App) RunPause(id int64) error { return orch_svc.Default().PauseRun(a.ctx, id) }

// RunResume 恢复指定 Run。
func (a *App) RunResume(id int64) error { return orch_svc.Default().ResumeRun(a.ctx, id) }

// RunStop 停止指定 Run。
func (a *App) RunStop(id int64) error { return orch_svc.Default().StopRun(a.ctx, id) }

// RunSpeak 向编排 Run 中的指定会话发一条消息（干预/补充指令）。
func (a *App) RunSpeak(sessionID int64, message string) error {
	return orch_svc.Default().Speak(a.ctx, sessionID, message)
}
