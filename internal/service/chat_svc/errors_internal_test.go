package chat_svc

import (
	"context"
	"errors"
	"testing"

	"github.com/cago-frame/cago/pkg/utils/httputils"
	"github.com/stretchr/testify/assert"

	"github.com/agentre-ai/agentre/internal/pkg/code"
)

// cause 存在时:Error() = 本地化 headline + 换行 + 原始 cause,让 Wails 边界能把详情带给前端。
func TestOperationFailedWithCause_ErrorCarriesCause(t *testing.T) {
	cause := errors.New("SQL logic error: table chat_sessions has no column named run_id (1)")

	err := operationFailedWithCause(context.Background(), cause)

	assert.Equal(t,
		"操作失败\nSQL logic error: table chat_sessions has no column named run_id (1)",
		err.Error())
}

// cause 为 nil 时退化成原来的通用错误,行为与改动前完全一致。
func TestOperationFailedWithCause_NilCauseDegrades(t *testing.T) {
	err := operationFailedWithCause(context.Background(), nil)

	assert.Equal(t, "操作失败", err.Error())
}

// 契约测试:errors.Is 能穿透到 cause。
// 注:原消费者 orch_svc 已随编排移除删除,这里锁的是 Go 错误包装的通用契约,不是回归护栏。
func TestOperationFailedWithCause_UnwrapsToCause(t *testing.T) {
	sentinel := errors.New("database is locked (5) (SQLITE_BUSY)")

	err := operationFailedWithCause(context.Background(), sentinel)

	assert.True(t, errors.Is(err, sentinel))
}

// 契约测试:errors.As 仍能取出 httputils.Error 且 Code 保持 OperationFailed。
func TestOperationFailedWithCause_AsHTTPError(t *testing.T) {
	err := operationFailedWithCause(context.Background(), errors.New("boom"))

	var httpErr *httputils.Error
	assert.True(t, errors.As(err, &httpErr))
	assert.Equal(t, code.OperationFailed, httpErr.Code)
}
