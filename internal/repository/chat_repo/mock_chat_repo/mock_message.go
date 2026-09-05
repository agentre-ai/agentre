// Package mock_chat_repo: MockMessageRepo 转发自 mock_transcript_repo。
//
// chat_repo.MessageRepo 现在是 transcript_repo.MessageRepo 的类型别名（消息 / 块的仓储
// 已抽成独立域 transcript_repo，决策 8），所以它的 mock 也只应有一份实现，由
// mock_transcript_repo（通过 go:generate mockgen 生成，见
// internal/repository/transcript_repo/message.go）持有。这里的类型别名 + 转发构造函数
// 只是保留旧 import path，供尚未纳入本轮范围的调用方（internal/peer 等）按旧名继续引用
// mock_chat_repo.MockMessageRepo，而不必各自维护第二份 mock。
package mock_chat_repo

import (
	gomock "go.uber.org/mock/gomock"

	"github.com/agentre-hub/agentre/internal/repository/transcript_repo/mock_transcript_repo"
)

// MockMessageRepo 与 mock_transcript_repo.MockMessageRepo 是同一个类型,不是两份拷贝。
type MockMessageRepo = mock_transcript_repo.MockMessageRepo

// NewMockMessageRepo creates a new mock instance.
func NewMockMessageRepo(ctrl *gomock.Controller) *MockMessageRepo {
	return mock_transcript_repo.NewMockMessageRepo(ctrl)
}
