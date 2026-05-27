package agentruntime

import (
	"context"
	"testing"
)

// askAnswerImpl 同时实现 AskAnswerSink 与 UserAskAnswerer (签名一致)。
// 编译期断言:相同方法签名同时满足两个 interface,Plan A 期间新旧名可热替换。
type askAnswerImpl struct{}

func (askAnswerImpl) SubmitAnswer(_ context.Context, _ int64, _ string,
	_ []AskQuestion, _ []AskAnswer, _ bool) error {
	return nil
}

var (
	_ AskAnswerSink   = askAnswerImpl{}
	_ UserAskAnswerer = askAnswerImpl{}
)

// toolPermissionImpl 同样验证 ToolPermissionSink ↔ ToolPermissionResponder 双向。
type toolPermissionImpl struct{}

func (toolPermissionImpl) SubmitToolPermission(_ context.Context, _ int64, _ string,
	_, _ bool, _ string) error {
	return nil
}

var (
	_ ToolPermissionSink      = toolPermissionImpl{}
	_ ToolPermissionResponder = toolPermissionImpl{}
)

// TestControlCompileCompat 占位 test 让 Go test runner 至少跑一遍包,
// 实际断言是上面那两组 var _ = ... 编译期 check。
func TestControlCompileCompat(t *testing.T) {
	_ = askAnswerImpl{}
	_ = toolPermissionImpl{}
}
