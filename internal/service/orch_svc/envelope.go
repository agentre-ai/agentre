package orch_svc

import (
	"fmt"
	"html"
	"strings"
)

// dispatchDoneMsg 子任务完成、无显式小结 → 轻量通知(首行摘要 + read 提示)。
func dispatchDoneMsg(dispatchID, agentID int64, callSeq int, excerpt string) string {
	return fmt.Sprintf(
		`<dispatch_done dispatch_id="%d" agent="%d" call_seq="%d">%s(read(dispatch_id=%d) 看全文)</dispatch_done>`,
		dispatchID, agentID, callSeq, html.EscapeString(excerpt), dispatchID,
	)
}

// dispatchReportMsg 子任务主动小结(finish→final:true / report→final:false)→ 内联回报。
func dispatchReportMsg(dispatchID, agentID int64, callSeq int, summary string, final bool) string {
	return fmt.Sprintf(
		`<dispatch_report dispatch_id="%d" agent="%d" call_seq="%d" final="%t">%s</dispatch_report>`,
		dispatchID, agentID, callSeq, final, html.EscapeString(summary),
	)
}

// dispatchErrorMsg 子任务技术崩溃 → 轻量通知(read 看详情)。
func dispatchErrorMsg(dispatchID, agentID int64, reason string) string {
	return fmt.Sprintf(
		`<dispatch_error dispatch_id="%d" agent="%d" reason="%s">(read(dispatch_id=%d) 看详情;决定重试/换 agent/放弃该分支)</dispatch_error>`,
		dispatchID, agentID, html.EscapeString(reason), dispatchID,
	)
}

// firstLine 取首行并按 rune 截断到 maxRunes(超出补 …),用作 dispatch_done 摘要。
func firstLine(s string, maxRunes int) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	r := []rune(s)
	if len(r) > maxRunes {
		return string(r[:maxRunes]) + "…"
	}
	return s
}
