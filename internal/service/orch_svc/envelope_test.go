package orch_svc

import (
	"strings"
	"testing"
)

func TestDispatchDoneMsg_ShapeAndEscape(t *testing.T) {
	got := dispatchDoneMsg(11, 3, 2, `实现完成 <ok> & "done"`)
	if !strings.HasPrefix(got, `<task_done task_id="11" agent="3" call_seq="2">`) {
		t.Fatalf("bad prefix: %s", got)
	}
	if !strings.Contains(got, `read(task_id=11)`) {
		t.Fatalf("missing read hint: %s", got)
	}
	if strings.Contains(got, "<ok>") || !strings.Contains(got, "&lt;ok&gt;") {
		t.Fatalf("excerpt not escaped: %s", got)
	}
	if !strings.HasSuffix(got, `</task_done>`) {
		t.Fatalf("bad suffix: %s", got)
	}
}

func TestDispatchReportMsg_FinalFlagAndEscape(t *testing.T) {
	fin := dispatchReportMsg(11, 3, 2, "已完成", true)
	if !strings.Contains(fin, `final="true"`) || !strings.Contains(fin, "已完成") {
		t.Fatalf("bad final report: %s", fin)
	}
	interim := dispatchReportMsg(11, 3, 2, `中途 <x>`, false)
	if !strings.Contains(interim, `final="false"`) || !strings.Contains(interim, "&lt;x&gt;") {
		t.Fatalf("bad interim report: %s", interim)
	}
}

func TestDispatchErrorMsg_ReasonEscaped(t *testing.T) {
	got := dispatchErrorMsg(12, 4, `崩溃 <boom>`)
	if !strings.Contains(got, `task_id="12" agent="4"`) || !strings.Contains(got, `reason="崩溃 &lt;boom&gt;"`) {
		t.Fatalf("bad error msg: %s", got)
	}
	if !strings.Contains(got, `read(task_id=12)`) {
		t.Fatalf("missing read hint: %s", got)
	}
}

func TestFirstLine_TruncatesAndStripsNewline(t *testing.T) {
	if got := firstLine("  第一行\n第二行  ", 100); got != "第一行" {
		t.Fatalf("newline strip: %q", got)
	}
	if got := firstLine("abcdef", 3); got != "abc…" {
		t.Fatalf("truncate: %q", got)
	}
	if got := firstLine("abc", 3); got != "abc" {
		t.Fatalf("exact: %q", got)
	}
}
