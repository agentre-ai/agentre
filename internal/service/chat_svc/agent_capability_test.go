package chat_svc_test

import (
	"context"
	"errors"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"go.uber.org/mock/gomock"

	"agentre/internal/model/entity/agent_backend_entity"
	"agentre/internal/model/entity/agent_entity"
	"agentre/internal/pkg/agentruntime/capability"
	"agentre/internal/repository/agent_backend_repo"
	"agentre/internal/repository/agent_backend_repo/mock_agent_backend_repo"
	"agentre/internal/repository/agent_repo"
	"agentre/internal/repository/agent_repo/mock_agent_repo"
	"agentre/internal/service/chat_svc"
)

// registerCapabilityRepos 注册 agent_repo + agent_backend_repo mock(并在测试后还原),
// 让 resolveAgentBackend 可以走通 agent → backend 解析。
func registerCapabilityRepos(t *testing.T, ctrl *gomock.Controller) (
	*mock_agent_repo.MockAgentRepo,
	*mock_agent_backend_repo.MockAgentBackendRepo,
) {
	t.Helper()
	agentMock := mock_agent_repo.NewMockAgentRepo(ctrl)
	backendMock := mock_agent_backend_repo.NewMockAgentBackendRepo(ctrl)

	prevAgent := agent_repo.Agent()
	prevBackend := agent_backend_repo.AgentBackend()
	agent_repo.RegisterAgent(agentMock)
	agent_backend_repo.RegisterAgentBackend(backendMock)
	t.Cleanup(func() {
		agent_repo.RegisterAgent(prevAgent)
		agent_backend_repo.RegisterAgentBackend(prevBackend)
	})
	return agentMock, backendMock
}

func TestAgentBackendHasCapability_LocalClaudeCodeHasMCPTools(t *testing.T) {
	Convey("给定本地 claudecode 后端, 探测 CapMCPTools 应返回 true", t, func() {
		ctrl := gomock.NewController(t)
		t.Cleanup(ctrl.Finish)
		agentMock, backendMock := registerCapabilityRepos(t, ctrl)
		ctx := context.Background()

		// agent 11 → backend 12(本地 claudecode, DeviceID 空 → IsLocal())。
		agentMock.EXPECT().Find(ctx, int64(11)).Return(&agent_entity.Agent{
			ID: 11, AgentBackendID: 12,
		}, nil)
		backendMock.EXPECT().Find(ctx, int64(12)).Return(&agent_backend_entity.AgentBackend{
			ID: 12, Type: string(agent_backend_entity.TypeClaudeCode), DeviceID: "",
		}, nil)

		svc := chat_svc.NewChat(chat_svc.NoopEmitter{})
		ok, err := svc.AgentBackendHasCapability(ctx, 11, capability.CapMCPTools)
		So(err, ShouldBeNil)
		// 通过真实注册表解析 claudecode runtime 的能力矩阵, 不伪造结果。
		So(ok, ShouldBeTrue)
	})
}

func TestAgentBackendHasCapability_RuntimeLacksCap(t *testing.T) {
	Convey("给定本地 claudecode 后端, 探测一个该 runtime 未声明的能力应返回 false", t, func() {
		ctrl := gomock.NewController(t)
		t.Cleanup(ctrl.Finish)
		agentMock, backendMock := registerCapabilityRepos(t, ctrl)
		ctx := context.Background()

		agentMock.EXPECT().Find(ctx, int64(11)).Return(&agent_entity.Agent{
			ID: 11, AgentBackendID: 12,
		}, nil)
		backendMock.EXPECT().Find(ctx, int64(12)).Return(&agent_backend_entity.AgentBackend{
			ID: 12, Type: string(agent_backend_entity.TypeClaudeCode), DeviceID: "",
		}, nil)

		svc := chat_svc.NewChat(chat_svc.NoopEmitter{})
		ok, err := svc.AgentBackendHasCapability(ctx, 11, capability.Capability("definitely-not-a-real-cap"))
		So(err, ShouldBeNil)
		So(ok, ShouldBeFalse)
	})
}

func TestAgentBackendHasCapability_UnresolvableAgent(t *testing.T) {
	Convey("给定 agent 不存在, resolveAgentBackend 报错应原样返回 (false, err)", t, func() {
		ctrl := gomock.NewController(t)
		t.Cleanup(ctrl.Finish)
		agentMock, _ := registerCapabilityRepos(t, ctrl)
		ctx := context.Background()

		agentMock.EXPECT().Find(ctx, int64(99)).Return(nil, nil)

		svc := chat_svc.NewChat(chat_svc.NoopEmitter{})
		ok, err := svc.AgentBackendHasCapability(ctx, 99, capability.CapMCPTools)
		So(err, ShouldNotBeNil)
		So(ok, ShouldBeFalse)
	})
}

func TestAgentBackendHasCapability_AgentFindError(t *testing.T) {
	Convey("给定 agent_repo.Find 返回底层错误, 应原样冒泡 (false, err)", t, func() {
		ctrl := gomock.NewController(t)
		t.Cleanup(ctrl.Finish)
		agentMock, _ := registerCapabilityRepos(t, ctrl)
		ctx := context.Background()

		agentMock.EXPECT().Find(ctx, int64(7)).Return(nil, errors.New("sqlite: disk I/O error"))

		svc := chat_svc.NewChat(chat_svc.NoopEmitter{})
		ok, err := svc.AgentBackendHasCapability(ctx, 7, capability.CapMCPTools)
		So(err, ShouldNotBeNil)
		So(ok, ShouldBeFalse)
	})
}

func TestAgentBackendHasCapability_RemoteBackendUnsupported(t *testing.T) {
	Convey("给定远端后端(DeviceID 非空), MVP 无 session 不探测能力 → (false, nil)", t, func() {
		ctrl := gomock.NewController(t)
		t.Cleanup(ctrl.Finish)
		agentMock, backendMock := registerCapabilityRepos(t, ctrl)
		ctx := context.Background()

		// LLMProviderKey 空 → resolveAgentBackend 跳过 gateway 校验, 远端后端也能解析成功;
		// AgentBackendHasCapability 因 !IsLocal() 在解析 runtime 前短路返回 false。
		agentMock.EXPECT().Find(ctx, int64(21)).Return(&agent_entity.Agent{
			ID: 21, AgentBackendID: 22,
		}, nil)
		backendMock.EXPECT().Find(ctx, int64(22)).Return(&agent_backend_entity.AgentBackend{
			ID: 22, Type: string(agent_backend_entity.TypeClaudeCode), DeviceID: "5",
		}, nil)

		svc := chat_svc.NewChat(chat_svc.NoopEmitter{})
		ok, err := svc.AgentBackendHasCapability(ctx, 21, capability.CapMCPTools)
		So(err, ShouldBeNil)
		So(ok, ShouldBeFalse)
	})
}
