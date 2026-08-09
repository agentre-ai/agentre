package agent_svc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/syncmeta_entity"
	"github.com/agentre-ai/agentre/internal/pkg/syncwire"
	"github.com/agentre-ai/agentre/internal/repository/agent_repo"
	"github.com/agentre-ai/agentre/internal/repository/agent_repo/mock_agent_repo"
	"github.com/agentre-ai/agentre/internal/service/sync_svc"
)

// recordingSync 记下域服务交出来的每一条改动（与 project_svc/sync_notify_test.go
// 同一手法）。
type recordingSync struct {
	sync_svc.SyncSvc
	changes []sync_svc.LocalChange
}

func (r *recordingSync) NotifyLocalChange(_ context.Context, ch sync_svc.LocalChange) {
	r.changes = append(r.changes, ch)
}

func registerRecordingSync(t *testing.T) *recordingSync {
	t.Helper()
	rec := &recordingSync{}
	sync_svc.SetDefault(rec)
	t.Cleanup(func() { sync_svc.SetDefault(nil) })
	// 同步已装配 → execTargetSnapshot 会真的去查执行目标行，装一个空列表桩。
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	et := mock_agent_repo.NewMockAgentExecTargetRepo(ctrl)
	et.EXPECT().ListByAgent(gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
	agent_repo.RegisterAgentExecTarget(et)
	return rec
}

func avatarAgent(id int64) *agent_entity.Agent {
	return &agent_entity.Agent{
		ID: id, Name: "Eva",
		SyncMeta: syncmeta_entity.SyncMeta{SyncID: "agent-sync-1"},
	}
}

// TestUploadAvatar_NotifiesSync R16a：「哈希参与内容比较，因此**换头像照常触发
// 同步**」。头像正文按内容哈希单独传，但触发这条同步的仍然是 Agent 行的一次普通
// 修改——不发通知的话，新头像只有等用户碰巧改了这个 Agent 的别的字段才会到对端。
func TestUploadAvatar_NotifiesSync(t *testing.T) {
	ctx, agentMock, _, _, svc := setupSvc(t)
	rec := registerRecordingSync(t)
	agentMock.EXPECT().Find(gomock.Any(), int64(9)).Return(avatarAgent(9), nil)
	agentMock.EXPECT().UpdateAvatar(gomock.Any(), int64(9), gomock.Any(), gomock.Any()).Return(nil)

	_, err := svc.UploadAvatar(ctx, &UploadAvatarRequest{
		ID: 9, DataURL: "data:image/png;base64,iVBORw0KGgo=",
	})
	require.NoError(t, err)

	require.Len(t, rec.changes, 1)
	assert.Equal(t, syncwire.KindAgent, rec.changes[0].Kind)
	assert.Equal(t, sync_svc.OpUpdate, rec.changes[0].Op)
	assert.Equal(t, "agent-sync-1", rec.changes[0].Meta.SyncID)
}

// TestDeleteAvatar_NotifiesSync 同上：清掉自定义头像也是一次内容变化，对端必须
// 跟着退回色块 + 图标呈现。
func TestDeleteAvatar_NotifiesSync(t *testing.T) {
	ctx, agentMock, _, _, svc := setupSvc(t)
	rec := registerRecordingSync(t)
	agentMock.EXPECT().Find(gomock.Any(), int64(9)).Return(avatarAgent(9), nil)
	agentMock.EXPECT().UpdateAvatar(gomock.Any(), int64(9), "", gomock.Any()).Return(nil)

	_, err := svc.DeleteAvatar(ctx, &DeleteAvatarRequest{ID: 9})
	require.NoError(t, err)

	require.Len(t, rec.changes, 1)
	assert.Equal(t, syncwire.KindAgent, rec.changes[0].Kind)
	assert.Equal(t, sync_svc.OpUpdate, rec.changes[0].Op)
}
