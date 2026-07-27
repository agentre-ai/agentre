package openclaw

import (
	"context"
	"strings"
	"time"

	"github.com/cago-frame/agents/provider"
)

// sessionUsageTimeout 给收轮补数留的时间。这是一次本地 WS 往返,给足余量即可 ——
// 超时只是少几个数字,绝不拖住收轮。
const sessionUsageTimeout = 3 * time.Second

// sessionDescribeMethod 是可选方法:它不在 requiredRuntimeMethods 里,必须按 hello
// 广播的方法表来判断能不能调。
const sessionDescribeMethod = "sessions.describe"

// 收轮时网关未必已经把本轮 usage 落进会话记录 —— 直接读会拿到上一轮的数字。
// 用开轮前的 endedAt 作基线轮询等它刷新;等不到就不报 usage(宁可空着,也不能
// 把上一轮的 token 数安在这一轮头上)。变量而非常量,便于测试压缩等待。
var (
	sessionUsagePollInterval = 200 * time.Millisecond
	sessionUsagePollAttempts = 6
)

// sessionRecord 是 sessions.describe 里我们关心的字段。
type sessionRecord struct {
	InputTokens   int    `json:"inputTokens"`
	OutputTokens  int    `json:"outputTokens"`
	TotalTokens   int    `json:"totalTokens"`
	EndedAt       int64  `json:"endedAt"`
	Model         string `json:"model"`
	ModelProvider string `json:"modelProvider"`
}

// describeSession 读一次会话记录;方法未广播或调用失败都返回 nil。
func describeSession(ctx context.Context, client interface {
	Call(context.Context, string, any, any) error
}, sessionKey string) *sessionRecord {
	var response struct {
		Session sessionRecord `json:"session"`
	}
	if err := client.Call(ctx, sessionDescribeMethod, map[string]any{"key": sessionKey}, &response); err != nil {
		return nil
	}
	return &response.Session
}

// publishSessionUsage 在收轮时补齐网关没在流式帧里给出的 usage 与 model。
//
// 实测 OpenClaw 2026.7.1-2 一整轮的 agent/chat 事件既不带 usage 也不带 model,助手
// 消息因此永远是「模型空 + ↑0 ↓0」。但网关自己按会话记着这些数:sessions.describe
// 会返回本会话最近一次运行的 inputTokens / outputTokens / totalTokens,以及实际使用
// 的 modelProvider + model,AgentRE 申请的 operator.read 就够读。
//
// 尽力而为:方法没广播、调用失败、或者本轮已经从帧里收到过 usage,都直接跳过。
func (a *activeTurn) publishSessionUsage() {
	if !a.sessionDescribe {
		return
	}
	// 收轮时 turnCtx 可能已被用户的「停止」取消,但中止轮同样有 token 花销,
	// 用 WithoutCancel 让这次补数照常发生。
	ctx, cancel := context.WithTimeout(context.WithoutCancel(a.ctx),
		sessionUsageTimeout+time.Duration(sessionUsagePollAttempts)*sessionUsagePollInterval)
	defer cancel()

	var latest *sessionRecord
	fresh := false
	for attempt := 0; attempt < sessionUsagePollAttempts; attempt++ {
		if record := describeSession(ctx, a.client, a.sessionKey); record != nil {
			latest = record
			if record.EndedAt > a.usageBaselineEndedAt {
				fresh = true
				break
			}
		}
		select {
		case <-time.After(sessionUsagePollInterval):
		case <-ctx.Done():
			attempt = sessionUsagePollAttempts
		}
	}
	if latest == nil {
		return
	}
	if model := qualifiedModel(latest.ModelProvider, latest.Model); model != "" {
		a.result.Model = model
	}
	if a.result.Usage != nil || !fresh {
		// 网关已在帧里报过 usage(那份更贴近本次 API call),或者记录始终没刷新 ——
		// 后者宁可不报,也不能把上一轮的数字安在这一轮头上。
		return
	}
	a.applyUsage(sessionUsage(latest.InputTokens, latest.OutputTokens, latest.TotalTokens))
}

// sessionUsage 把会话记录里的 token 数换成 provider.Usage;全是 0 时返回 nil,
// 让 UI 保持「没有用量」而不是显示一组假的零。
func sessionUsage(input, output, total int) *provider.Usage {
	if input <= 0 && output <= 0 && total <= 0 {
		return nil
	}
	usage := &provider.Usage{
		PromptTokens:     max(input, 0),
		CompletionTokens: max(output, 0),
		TotalTokens:      max(total, 0),
	}
	if usage.TotalTokens == 0 {
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	}
	return usage
}

// qualifiedModel 与探测里的模型 ID 约定一致:provider 前缀只在模型本身没带时补。
func qualifiedModel(providerID, model string) string {
	model = strings.TrimSpace(model)
	providerID = strings.TrimSpace(providerID)
	if model == "" || providerID == "" || strings.Contains(model, "/") {
		return model
	}
	return providerID + "/" + model
}
