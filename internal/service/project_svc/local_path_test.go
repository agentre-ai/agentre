package project_svc_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/model/entity/project_entity"
)

// 本文件锁住 R10 的写口：「本机未配置路径」的项目可以就地指定路径解除该状态
// （project_svc.SetLocalPath），指定之后与本机创建的项目无任何差别。

func TestProjectSvcSetLocalPath_GivenUnconfiguredProject_ThenClearsMissingFlag(t *testing.T) {
	ctx, mp, _, _, svc := setupProjectSvc(t)
	tmp := t.TempDir()

	existing := &project_entity.Project{ID: 9, Name: "agentre-hub", LocalPathMissing: true}
	mp.EXPECT().Find(ctx, int64(9)).Return(existing, nil)
	mp.EXPECT().Update(ctx, gomock.Any()).DoAndReturn(func(_ interface{}, p *project_entity.Project) error {
		assert.False(t, p.LocalPathMissing)
		assert.Equal(t, tmp, p.Path)
		return nil
	})

	got, err := svc.SetLocalPath(ctx, 9, tmp)
	require.NoError(t, err)
	assert.False(t, got.LocalPathMissing, "指定路径后应解除本机未配置状态")
	assert.Equal(t, tmp, got.Path)
}

func TestProjectSvcSetLocalPath_GivenNonexistentPath_ThenRejects(t *testing.T) {
	ctx, mp, _, _, svc := setupProjectSvc(t)

	existing := &project_entity.Project{ID: 10, Name: "agentre-hub", LocalPathMissing: true}
	mp.EXPECT().Find(ctx, int64(10)).Return(existing, nil)

	_, err := svc.SetLocalPath(ctx, 10, "/this/path/does/not/exist/at/all")
	require.Error(t, err)
}

func TestProjectSvcSetLocalPath_GivenProjectNotFound_ThenRejects(t *testing.T) {
	ctx, mp, _, _, svc := setupProjectSvc(t)
	mp.EXPECT().Find(ctx, int64(11)).Return(nil, nil)

	_, err := svc.SetLocalPath(ctx, 11, t.TempDir())
	require.Error(t, err)
}

func TestProjectSvcSetLocalPath_GivenBlankPath_ThenRejects(t *testing.T) {
	ctx, mp, _, _, svc := setupProjectSvc(t)
	existing := &project_entity.Project{ID: 12, Name: "x", LocalPathMissing: true}
	mp.EXPECT().Find(ctx, int64(12)).Return(existing, nil)

	_, err := svc.SetLocalPath(ctx, 12, "   ")
	require.Error(t, err)
}
