package orch_svc

import (
	"context"
	"fmt"
	"strings"
)

// ReadTask 拉取本 Run 内某任务的最终输出(settled 分支):Summary(有则)+ 完整 Result。
// 目标任务须与调用者同 Run。running/peek 属切片 B。
func (s *orchSvc) ReadTask(ctx context.Context, sessionID, taskID int64) (string, error) {
	caller, err := s.tasks.FindBySession(ctx, sessionID)
	if err != nil {
		return "", err
	}
	if caller == nil {
		return "", errRunNotActive
	}
	tk, err := s.tasks.Find(ctx, taskID)
	if err != nil {
		return "", err
	}
	if tk == nil || tk.RunID != caller.RunID {
		return "", errForeignTask
	}
	if !tk.IsTerminal() {
		var b strings.Builder
		fmt.Fprintf(&b, "task #%d · agent#%d · %s(运行中)", tk.ID, tk.AgentID, tk.Status)
		latest, _ := s.chat.LatestAssistantText(ctx, tk.SessionID)
		if strings.TrimSpace(latest) == "" {
			b.WriteString("\n(运行中,尚无输出)")
		} else {
			fmt.Fprintf(&b, "\n【当前】%s", latest)
		}
		return b.String(), nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "task #%d · agent#%d · %s", tk.ID, tk.AgentID, tk.Status)
	if tk.Summary != "" {
		fmt.Fprintf(&b, "\n【小结】%s", tk.Summary)
	}
	if tk.Result != "" {
		fmt.Fprintf(&b, "\n【输出】%s", tk.Result)
	}
	return b.String(), nil
}
