package handlers

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agentskill"
)

// fakeSkillDisc 替身发现器:记录入参,回预置包,免依赖真实 claude 二进制。
type fakeSkillDisc struct {
	gotQuery agentskill.DiscoverQuery
	packs    []agentskill.SkillPack
}

func (f *fakeSkillDisc) Discover(_ context.Context, q agentskill.DiscoverQuery) ([]agentskill.SkillPack, error) {
	f.gotQuery = q
	return f.packs, nil
}

func TestSkillsHandler_List_RunsDaemonDiscoverer(t *testing.T) {
	fd := &fakeSkillDisc{packs: []agentskill.SkillPack{
		{ID: "superpowers@claude-plugins-official", Name: "superpowers", Installed: true, Source: agentskill.SourceInstalled, GloballyEnabled: true},
	}}
	restore := agentskill.SwapDiscovererForTest(agent_backend_entity.TypeClaudeCode, fd)
	t.Cleanup(restore)

	// daemon 自己解析本机 CLI 路径喂给发现器(desktop 不知道 daemon 的 claude 在哪)。
	SetResolveCLIPathFunc(func(bt string) (string, bool, error) {
		require.Equal(t, "claudecode", bt)
		return "/daemon/bin/claude", true, nil
	})
	t.Cleanup(ResetResolveCLIPathFunc)

	h := NewSkillsHandlers()
	res, err := h.List(context.Background(), SkillsListParams{BackendType: "claudecode"})
	require.NoError(t, err)
	require.Len(t, res.Packs, 1)
	require.Equal(t, "superpowers@claude-plugins-official", res.Packs[0].ID)
	require.True(t, res.Packs[0].GloballyEnabled)
	require.Equal(t, "/daemon/bin/claude", fd.gotQuery.CLIPath)
	require.Equal(t, agent_backend_entity.TypeClaudeCode, fd.gotQuery.BackendType)
}

func TestSkillsHandler_List_ExplicitCLIPathWins(t *testing.T) {
	fd := &fakeSkillDisc{}
	restore := agentskill.SwapDiscovererForTest(agent_backend_entity.TypeClaudeCode, fd)
	t.Cleanup(restore)
	// 显式传 CLIPath 时直接用,不再走本机解析。
	SetResolveCLIPathFunc(func(string) (string, bool, error) {
		t.Fatal("must not resolve when CLIPath provided")
		return "", false, nil
	})
	t.Cleanup(ResetResolveCLIPathFunc)

	h := NewSkillsHandlers()
	_, err := h.List(context.Background(), SkillsListParams{BackendType: "claudecode", CLIPath: "/custom/claude"})
	require.NoError(t, err)
	require.Equal(t, "/custom/claude", fd.gotQuery.CLIPath)
}

func TestSkillsHandler_List_NoDiscovererReturnsEmpty(t *testing.T) {
	h := NewSkillsHandlers()
	res, err := h.List(context.Background(), SkillsListParams{BackendType: "nonesuch"})
	require.NoError(t, err)
	require.NotNil(t, res.Packs, "Packs 永远非 nil,序列化给 desktop 是空数组而非 null")
	require.Empty(t, res.Packs)
}
