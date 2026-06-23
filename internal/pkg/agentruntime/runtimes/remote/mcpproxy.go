package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"go.uber.org/zap"

	"github.com/cago-frame/cago/pkg/logger"

	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/runtimes/remote/wire"
)

// MCPProxyDispatcher 把一条经隧道回到 desktop 的 MCP HTTP 请求重放到 desktop 本机 gateway。
// 由 bootstrap 用 desktop gateway base URL 装配(见 RegisterMCPProxyDispatcher),让 remote
// 包不反向依赖 httpgateway。
type MCPProxyDispatcher func(ctx context.Context, req wire.MCPProxyRequest) (wire.MCPProxyResponse, error)

// mcpProxyDispatcher 是进程级注入点(单 desktop 进程一份),与 chat_svc.RegisterGateway
// 同款 bootstrap 接线风格。nil = 未装配(理论上不该发生,handler 回 502 让 CLI 看到错误)。
var mcpProxyDispatcher MCPProxyDispatcher

// RegisterMCPProxyDispatcher bootstrap 接线入口;传 nil 清空(测试用)。
func RegisterMCPProxyDispatcher(d MCPProxyDispatcher) { mcpProxyDispatcher = d }

// NewLocalGatewayDispatcher 构造一个把隧道请求重放到 desktop 本机 gateway 的 dispatcher:
// 用 baseURL()(desktop gateway base,如 http://127.0.0.1:52401)+ req.Path 拼目标,带上原
// headers(含 desktop 签的 token)+ body 发 HTTP,把应答装回 MCPProxyResponse。bootstrap 用
// gw.BaseURL 装配。baseURL 取值时机推迟到每次请求(端口 0 晚绑定也拿得到实际地址)。
func NewLocalGatewayDispatcher(baseURL func() string, client *http.Client) MCPProxyDispatcher {
	if client == nil {
		client = http.DefaultClient
	}
	return func(ctx context.Context, req wire.MCPProxyRequest) (wire.MCPProxyResponse, error) {
		base := ""
		if baseURL != nil {
			base = baseURL()
		}
		if base == "" {
			return wire.MCPProxyResponse{}, errors.New("mcp proxy: desktop gateway base url unavailable")
		}
		method := req.Method
		if method == "" {
			method = http.MethodPost
		}
		target := strings.TrimRight(base, "/") + req.Path
		httpReq, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(req.Body))
		if err != nil {
			return wire.MCPProxyResponse{}, err
		}
		for k, vs := range req.Headers {
			for _, v := range vs {
				httpReq.Header.Add(k, v)
			}
		}
		httpResp, err := client.Do(httpReq)
		if err != nil {
			return wire.MCPProxyResponse{}, err
		}
		defer func() { _ = httpResp.Body.Close() }()
		body, err := io.ReadAll(httpResp.Body)
		if err != nil {
			return wire.MCPProxyResponse{}, err
		}
		return wire.MCPProxyResponse{
			Status:  httpResp.StatusCode,
			Headers: httpResp.Header,
			Body:    body,
		}, nil
	}
}

// handleMCPProxy 处理 daemon 经 MethodMCPProxy 反向请求过来的 MCP HTTP 调用:解包 →
// 用注入的 dispatcher 在 desktop 本机重放 → 把应答原路返回(成为该 JSON-RPC 请求的 result)。
// 返回的 error 才会让 daemon 侧 Request 失败;能装进 MCPProxyResponse 的错误(含未装配 /
// 重放失败)都以 HTTP 状态码回给 CLI,避免一个工具调用打挂整条 RPC 连接。
func (r *Runtime) handleMCPProxy(ctx context.Context, raw json.RawMessage) (any, error) {
	var req wire.MCPProxyRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		logger.Ctx(ctx).Warn("remote runtime: mcpProxy request unmarshal failed", zap.Error(err))
		return wire.MCPProxyResponse{Status: 400, Body: []byte("mcp proxy: bad request frame")}, nil
	}
	d := mcpProxyDispatcher
	if d == nil {
		logger.Ctx(ctx).Warn("remote runtime: mcpProxy with no dispatcher registered",
			zap.String("path", req.Path))
		return wire.MCPProxyResponse{Status: 502, Body: []byte("mcp proxy: desktop dispatcher unavailable")}, nil
	}
	resp, err := d(ctx, req)
	if err != nil {
		logger.Ctx(ctx).Warn("remote runtime: mcpProxy dispatch failed",
			zap.String("path", req.Path), zap.Error(err))
		return wire.MCPProxyResponse{Status: 502, Body: []byte("mcp proxy: " + err.Error())}, nil
	}
	return resp, nil
}
