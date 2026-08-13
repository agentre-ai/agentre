package fakes

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/cago-frame/cago/pkg/consts"
	"github.com/cago-frame/cago/pkg/logger"
	"go.uber.org/zap"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/paired_agentred_entity"
	"github.com/agentre-ai/agentre/internal/repository/agent_backend_repo"
	"github.com/agentre-ai/agentre/internal/repository/agent_repo"
	"github.com/agentre-ai/agentre/internal/repository/remote_device_repo"
	"github.com/agentre-ai/agentre/internal/service/agent_backend_svc"
	"github.com/agentre-ai/agentre/internal/service/agent_svc"
)

// 「桌面端 + 浏览器同时对着一台 agentred」那个场景(agentre-server e2e/dual/)需要这个
// 桌面端**接上一台确定的 agentred**。真实路径是「配对码 / 认领机器」那套交互,e2e 里
// 没人能点,于是这里把它留下的东西直接播下去:
//
//   - 一条 paired_agentreds 行(名字 / URL / daemon 指纹);
//   - 一个绑定该行的 claudecode backend(agent_backends.device_id = 该行 id);
//   - 一个用这个 backend 的 Agent。
//
// 于是桌面端与这个 Agent 的每一轮都走 remote runtime → ConnPool.Borrow →
// GET /v1/relay/client?daemon_fingerprint=<fp>,落到浏览器接的那台 agentred 上。
// 两端因此在**同一条中继链路**上,而不是各自走各自的传输。
//
// 缺 env(其余每一套 e2e)时整段是 no-op:committed 那套仍然完全离线跑。
const (
	e2eAgentredFingerprintEnv = "AGENTRE_E2E_AGENTRED_FINGERPRINT"
	e2eAgentredURLEnv         = "AGENTRE_E2E_AGENTRED_URL"
)

// RemoteAgentName 是那个「跑在 agentred 上」的 Agent 的名字。spec 按它在 agent picker
// 里取行,所以它是接缝的一部分,不能随手改。
const RemoteAgentName = "E2E Remote Agent"

const (
	remoteDeviceName  = "E2E Remote Agentred"
	remoteBackendName = "E2E Remote Backend"
)

// errSystemAgentMissing 只可能在迁移出问题时发生(CEO 是迁移种下的)。
var errSystemAgentMissing = errors.New("system agent not found (migration gap?)")

func nowMs() int64 { return time.Now().UnixMilli() }

// installE2ERemoteAgentred 播一台已配对的 agentred + 指向它的 Agent。
//
// 幂等:`wails dev` 会把同一个二进制跑两遍(先生成前端绑定、再正式跑),Install 因此
// 每次 run 执行两次;三样东西都先查后建。
//
// 它不再重跑 bootstrap.InitRemoteDevice:登录夹具(installE2ELoggedInAccount)在 seed
// 本机 backend 之前已经按新 keychain 与重绑的 server_svc 重建过 remote_device_svc ——
// ConnPool 手里已是带访问令牌的新 server_svc(中继拨号不再 ErrNotLoggedIn),再跑一次
// 只是拿同一把 keychain 重装、self identity 不变。这里播种不得再触碰 remote_device_svc,
// 否则就把 self identity 又换了一次。
//
// 失败只记日志不 panic —— e2e 环境异常应该让 Playwright 用例红在它自己的断言上,
// 而不是让 app 崩在启动里(那样连日志都不好读)。
func installE2ERemoteAgentred(ctx context.Context) {
	fingerprint := strings.TrimSpace(os.Getenv(e2eAgentredFingerprintEnv))
	url := strings.TrimSpace(os.Getenv(e2eAgentredURLEnv))
	if fingerprint == "" || url == "" {
		return
	}

	deviceID, err := ensurePairedAgentred(ctx, url, fingerprint)
	if err != nil {
		logger.Ctx(ctx).Error("e2efakes.remote: seed paired agentred failed", zap.Error(err))
		return
	}
	backendID, err := ensureRemoteBackend(ctx, deviceID)
	if err != nil {
		logger.Ctx(ctx).Error("e2efakes.remote: seed remote backend failed", zap.Error(err))
		return
	}
	if err := ensureRemoteAgent(ctx, backendID); err != nil {
		logger.Ctx(ctx).Error("e2efakes.remote: seed remote agent failed", zap.Error(err))
		return
	}
	logger.Ctx(ctx).Info("e2efakes.remote: desktop joined the e2e agentred",
		zap.String("daemonFingerprint", fingerprint),
		zap.Int64("deviceID", deviceID),
		zap.Int64("backendID", backendID))
}

// ensurePairedAgentred 返回那条配对行的 id(没有就建一条)。
func ensurePairedAgentred(ctx context.Context, url, fingerprint string) (int64, error) {
	repo := remote_device_repo.PairedAgentred()
	existing, err := repo.FindByURL(ctx, url)
	if err != nil {
		return 0, err
	}
	if existing != nil {
		return existing.ID, nil
	}
	row := &paired_agentred_entity.PairedAgentred{
		Name:              remoteDeviceName,
		URL:               url,
		DaemonFingerprint: fingerprint,
		InstanceUUID:      fingerprint,
		TLSMode:           "default",
		PairedAt:          nowMs(),
		LastSeenAt:        nowMs(),
		Status:            consts.ACTIVE,
	}
	if err := row.Check(ctx); err != nil {
		return 0, err
	}
	if err := repo.Create(ctx, row); err != nil {
		return 0, err
	}
	return row.ID, nil
}

// ensureRemoteBackend 返回那个绑定了配对行的 claudecode backend 的 id。
func ensureRemoteBackend(ctx context.Context, deviceID int64) (int64, error) {
	existing, err := agent_backend_repo.AgentBackend().FindByName(ctx, remoteBackendName)
	if err != nil {
		return 0, err
	}
	if existing != nil {
		return existing.ID, nil
	}
	resp, err := agent_backend_svc.AgentBackend().Create(ctx, &agent_backend_svc.CreateBackendRequest{
		Type:     string(agent_backend_entity.TypeClaudeCode),
		Name:     remoteBackendName,
		DeviceID: strconv.FormatInt(deviceID, 10),
	})
	if err != nil {
		return 0, err
	}
	return resp.Item.ID, nil
}

// ensureRemoteAgent 保证有一个用远端 backend 的 Agent(已存在就把执行目标改过去,
// 让重跑时的取值与首次一致)。
func ensureRemoteAgent(ctx context.Context, backendID int64) error {
	existing, err := agent_repo.Agent().FindByName(ctx, RemoteAgentName)
	if err != nil {
		return err
	}
	if existing == nil {
		ceo, err := agent_repo.Agent().FindSystem(ctx)
		if err != nil {
			return err
		}
		if ceo == nil {
			return errSystemAgentMissing
		}
		_, err = agent_svc.Agent().Create(ctx, &agent_svc.CreateAgentRequest{
			Name:           RemoteAgentName,
			ParentAgentID:  ceo.ID,
			AgentBackendID: backendID,
		})
		return err
	}
	_, err = agent_svc.Agent().Update(ctx, &agent_svc.UpdateAgentRequest{
		ID:          existing.ID,
		Name:        existing.Name,
		ExecTargets: []agent_svc.ExecTargetInputDTO{{AgentBackendID: backendID}},
	})
	return err
}
