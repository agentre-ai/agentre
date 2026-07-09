package orch_svc

import "context"

// Report 子任务运行中主动向父推一条中途小结(final=false),不改状态、不收口。
// 不写 Dispatch.Summary(Summary 只留给终态 finish,避免污染完成分流)。
func (s *orchSvc) Report(ctx context.Context, sessionID int64, note string) error {
	tk, err := s.dispatches.FindBySession(ctx, sessionID)
	if err != nil {
		return err
	}
	if tk == nil {
		return errRunNotActive
	}
	s.injectToParent(ctx, tk.ParentDispatchID, dispatchReportMsg(tk.ID, tk.AgentID, tk.CallSeq, note, false))
	return nil
}
