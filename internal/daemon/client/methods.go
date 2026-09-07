package client

import (
	"context"

	"google.golang.org/protobuf/proto"

	"github.com/agentre-hub/agentre/pkg/wire/agentrewire"
	"github.com/agentre-hub/agentre/pkg/wire/protorpc"
)

// 本文件是桌面端这一侧的 typed 调用面：一个方法一个函数，method ID 与消息类型的配对
// 只在这里出现一次。
//
// 从前每个调用方各自写 `protorpc.CallMethod(ctx, conn, uint32(agentrewire.RpcMethod_
// RPC_METHOD_XXX), req, func() *agentrewire.XxxResponse { ... })`，于是 13 个 service
// 包、Wails 绑定层和两个 internal/pkg 包里都散着 method ID 与 payload 构造。对面
// agentre-server 早就是另一种形状：mirror_svc 的 machineConn 只对业务层公开具体方法。
// 这里把桌面端拉到同一条线上。
//
// 它刻意只是**一层薄壳**：交回的仍然是 wire 消息，翻成领域类型是各自领域的事
// （runtime 那一族由 protowire 负责）。这层要解决的是「method ID 被抄了二十遍」，
// 不是「谁来做协议到领域的翻译」。
//
// 参数取 Caller —— 只要求「交得出一条在跑的连接」这一件事。连接池的租约
// （lease.Client()）、*ProtobufClient、以及测试里直接握着的 *protorpc.Conn（经 On 包
// 一层）都满足它；调用面不该因此要求调用方还得能 Close 或报得出指纹。

// Caller 是 typed 调用面需要的最小依赖：一条已经在跑的连接。
type Caller interface{ Conn() *protorpc.Conn }

// On 把一条裸连接当作调用面的入口。生产代码手上普遍是租约或客户端，用不到它；
// 它是给直接握着 *protorpc.Conn 的地方（测试装置）准备的。
func On(conn *protorpc.Conn) Caller { return rawCaller{conn: conn} }

type rawCaller struct{ conn *protorpc.Conn }

func (c rawCaller) Conn() *protorpc.Conn { return c.conn }

func call[Req proto.Message, Resp proto.Message](
	ctx context.Context, conn Caller, method agentrewire.RpcMethod,
	request Req, newResponse func() Resp,
) (Resp, error) {
	return protorpc.CallMethod(ctx, conn.Conn(), uint32(method), request, newResponse)
}

// ── 设备与健康 ──────────────────────────────────────────────────────────────

func HealthPing(ctx context.Context, conn Caller, request *agentrewire.HealthPingRequest) (*agentrewire.HealthPingResponse, error) {
	return call(ctx, conn, agentrewire.RpcMethod_RPC_METHOD_HEALTH_PING, request,
		func() *agentrewire.HealthPingResponse { return &agentrewire.HealthPingResponse{} })
}

func ClaudeCodeUsage(ctx context.Context, conn Caller, request *agentrewire.ClaudeCodeUsageRequest) (*agentrewire.ClaudeCodeUsageResponse, error) {
	return call(ctx, conn, agentrewire.RpcMethod_RPC_METHOD_CLAUDE_CODE_USAGE, request,
		func() *agentrewire.ClaudeCodeUsageResponse { return &agentrewire.ClaudeCodeUsageResponse{} })
}

func LLMUpsert(ctx context.Context, conn Caller, request *agentrewire.LLMUpsertRequest) (*agentrewire.LLMUpsertResponse, error) {
	return call(ctx, conn, agentrewire.RpcMethod_RPC_METHOD_LLM_UPSERT, request,
		func() *agentrewire.LLMUpsertResponse { return &agentrewire.LLMUpsertResponse{} })
}

func AgentredSelfUpdate(ctx context.Context, conn Caller, request *agentrewire.AgentredSelfUpdateRequest) (*agentrewire.AgentredSelfUpdateResponse, error) {
	return call(ctx, conn, agentrewire.RpcMethod_RPC_METHOD_AGENTRED_SELF_UPDATE, request,
		func() *agentrewire.AgentredSelfUpdateResponse { return &agentrewire.AgentredSelfUpdateResponse{} })
}

// ── CLI 与技能 ──────────────────────────────────────────────────────────────

func CLIResolvePath(ctx context.Context, conn Caller, request *agentrewire.CLIResolvePathRequest) (*agentrewire.CLIResolvePathResponse, error) {
	return call(ctx, conn, agentrewire.RpcMethod_RPC_METHOD_CLI_RESOLVE_PATH, request,
		func() *agentrewire.CLIResolvePathResponse { return &agentrewire.CLIResolvePathResponse{} })
}

func CLIProbe(ctx context.Context, conn Caller, request *agentrewire.CLIProbeRequest) (*agentrewire.CLIProbeResponse, error) {
	return call(ctx, conn, agentrewire.RpcMethod_RPC_METHOD_CLI_PROBE, request,
		func() *agentrewire.CLIProbeResponse { return &agentrewire.CLIProbeResponse{} })
}

func SkillsList(ctx context.Context, conn Caller, request *agentrewire.SkillsListRequest) (*agentrewire.SkillsListResponse, error) {
	return call(ctx, conn, agentrewire.RpcMethod_RPC_METHOD_SKILLS_LIST, request,
		func() *agentrewire.SkillsListResponse { return &agentrewire.SkillsListResponse{} })
}

// ── 远端文件系统 ────────────────────────────────────────────────────────────

func RemoteFsListDir(ctx context.Context, conn Caller, request *agentrewire.RemoteFsListDirRequest) (*agentrewire.RemoteFsListDirResponse, error) {
	return call(ctx, conn, agentrewire.RpcMethod_RPC_METHOD_REMOTE_FS_LIST_DIR, request,
		func() *agentrewire.RemoteFsListDirResponse { return &agentrewire.RemoteFsListDirResponse{} })
}

func RemoteFsMkdir(ctx context.Context, conn Caller, request *agentrewire.RemoteFsMkdirRequest) (*agentrewire.RemoteFsMkdirResponse, error) {
	return call(ctx, conn, agentrewire.RpcMethod_RPC_METHOD_REMOTE_FS_MKDIR, request,
		func() *agentrewire.RemoteFsMkdirResponse { return &agentrewire.RemoteFsMkdirResponse{} })
}

func WorkspaceFsGitState(ctx context.Context, conn Caller, request *agentrewire.WorkspaceFsGitStateRequest) (*agentrewire.WorkspaceFsGitStateResponse, error) {
	return call(ctx, conn, agentrewire.RpcMethod_RPC_METHOD_WORKSPACE_FS_GIT_STATE, request,
		func() *agentrewire.WorkspaceFsGitStateResponse { return &agentrewire.WorkspaceFsGitStateResponse{} })
}

// ── 终端 ────────────────────────────────────────────────────────────────────

func TerminalOpen(ctx context.Context, conn Caller, request *agentrewire.TerminalOpenRequest) (*agentrewire.TerminalOpenResponse, error) {
	return call(ctx, conn, agentrewire.RpcMethod_RPC_METHOD_TERMINAL_OPEN, request,
		func() *agentrewire.TerminalOpenResponse { return &agentrewire.TerminalOpenResponse{} })
}

func TerminalWrite(ctx context.Context, conn Caller, request *agentrewire.TerminalWriteRequest) error {
	_, err := call(ctx, conn, agentrewire.RpcMethod_RPC_METHOD_TERMINAL_WRITE, request,
		func() *agentrewire.Empty { return &agentrewire.Empty{} })
	return err
}

func TerminalResize(ctx context.Context, conn Caller, request *agentrewire.TerminalResizeRequest) error {
	_, err := call(ctx, conn, agentrewire.RpcMethod_RPC_METHOD_TERMINAL_RESIZE, request,
		func() *agentrewire.Empty { return &agentrewire.Empty{} })
	return err
}

func TerminalClose(ctx context.Context, conn Caller, request *agentrewire.TerminalCloseRequest) error {
	_, err := call(ctx, conn, agentrewire.RpcMethod_RPC_METHOD_TERMINAL_CLOSE, request,
		func() *agentrewire.Empty { return &agentrewire.Empty{} })
	return err
}

// ── 会话与运行时 ────────────────────────────────────────────────────────────

func SessionList(ctx context.Context, conn Caller, request *agentrewire.SessionListRequest) (*agentrewire.SessionListResponse, error) {
	return call(ctx, conn, agentrewire.RpcMethod_RPC_METHOD_SESSION_LIST, request,
		func() *agentrewire.SessionListResponse { return &agentrewire.SessionListResponse{} })
}

func SessionAttach(ctx context.Context, conn Caller, request *agentrewire.SessionAttachRequest) (*agentrewire.SessionAttachResponse, error) {
	return call(ctx, conn, agentrewire.RpcMethod_RPC_METHOD_SESSION_ATTACH, request,
		func() *agentrewire.SessionAttachResponse { return &agentrewire.SessionAttachResponse{} })
}

func SessionPull(ctx context.Context, conn Caller, request *agentrewire.SessionPullRequest) (*agentrewire.SessionPullResponse, error) {
	return call(ctx, conn, agentrewire.RpcMethod_RPC_METHOD_SESSION_PULL, request,
		func() *agentrewire.SessionPullResponse { return &agentrewire.SessionPullResponse{} })
}

func RuntimeCapabilities(ctx context.Context, conn Caller, request *agentrewire.RuntimeCapabilitiesRequest) (*agentrewire.RuntimeCapabilitiesResponse, error) {
	return call(ctx, conn, agentrewire.RpcMethod_RPC_METHOD_RUNTIME_CAPABILITIES, request,
		func() *agentrewire.RuntimeCapabilitiesResponse { return &agentrewire.RuntimeCapabilitiesResponse{} })
}

func RuntimeRun(ctx context.Context, conn Caller, request *agentrewire.RuntimeRunRequest) (*agentrewire.RuntimeRunResponse, error) {
	return call(ctx, conn, agentrewire.RpcMethod_RPC_METHOD_RUNTIME_RUN, request,
		func() *agentrewire.RuntimeRunResponse { return &agentrewire.RuntimeRunResponse{} })
}
