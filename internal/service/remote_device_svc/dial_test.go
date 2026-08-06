package remote_device_svc_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gorilla/websocket"
	. "github.com/smartystreets/goconvey/convey"

	"github.com/agentre-ai/agentre/internal/daemon/rpc"
	"github.com/agentre-ai/agentre/internal/service/remote_device_svc"
)

// fakeAccountJWT 是测试用的假账号凭据(真实现是 server 签发的 RS256 JWT)。
const fakeAccountJWT = "acct-jwt"

// fakeDaemon 是一台假 agentred:升级 websocket,记录收到的每一帧,并对请求回
// 固定结果(或固定错误)。用来验证直连握手在线路上到底发了什么。
type fakeDaemon struct {
	srv *httptest.Server

	mu     sync.Mutex
	frames []rpc.Frame
}

func newFakeDaemon(t *testing.T, instanceUUID string, reject *rpc.Error) *fakeDaemon {
	t.Helper()
	d := &fakeDaemon{}
	d.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = ws.Close() }()
		for {
			var f rpc.Frame
			if err := ws.ReadJSON(&f); err != nil {
				return
			}
			d.record(f)
			out := rpc.Frame{JSONRPC: "2.0", ID: f.ID}
			if reject != nil {
				out.Error = reject
			} else {
				out.Result = json.RawMessage(`{"ok":true,"instanceUUID":"` + instanceUUID + `"}`)
			}
			_ = ws.WriteJSON(out)
		}
	}))
	t.Cleanup(d.srv.Close)
	return d
}

func (d *fakeDaemon) url() string { return "ws" + strings.TrimPrefix(d.srv.URL, "http") + "/rpc" }

func (d *fakeDaemon) record(f rpc.Frame) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.frames = append(d.frames, f)
}

func (d *fakeDaemon) received() []rpc.Frame {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]rpc.Frame(nil), d.frames...)
}

// 直连的账号握手就是一次 auth.account:daemon 侧全靠缓存的公钥本地验签(R3),
// 客户端不为此多跑任何一轮。
func TestRealDial_OpenAccount_PresentsCredentialInOneRoundTrip(t *testing.T) {
	Convey("OpenAccount completes with a single auth.account request over the direct connection", t, func() {
		d := newFakeDaemon(t, "uuid-1", nil)

		c, err := remote_device_svc.NewDaemonDial().OpenAccount(context.Background(), remote_device_svc.AccountArgs{
			URL:                       d.url(),
			TLSMode:                   "default",
			Credential:                fakeAccountJWT,
			DeviceFingerprint:         "sha256:desktop",
			ExpectedDaemonFingerprint: rpc.DaemonFingerprint("uuid-1"),
		})
		So(err, ShouldBeNil)
		So(c, ShouldNotBeNil)
		defer func() { _ = c.Close() }()

		frames := d.received()
		So(len(frames), ShouldEqual, 1)
		So(frames[0].Method, ShouldEqual, "auth.account")
		var p rpc.AccountParams
		So(json.Unmarshal(frames[0].Params, &p), ShouldBeNil)
		So(p.Credential, ShouldEqual, "acct-jwt")
		So(p.DeviceFingerprint, ShouldEqual, "sha256:desktop")
	})
}

// daemon 的六种拒绝理由都以 -32001 返回(桌面端按 code 分类,不看文案),
// 统一映射成 ErrUnauthorized,让 ConnPool 继续把它判成终止条件。
func TestRealDial_OpenAccount_DaemonRejects_MapsToUnauthorized(t *testing.T) {
	Convey("daemon rejects the account credential → ErrUnauthorized", t, func() {
		d := newFakeDaemon(t, "uuid-1", &rpc.Error{Code: rpc.ErrUnauthorized.Code, Message: "account credential revoked"})

		c, err := remote_device_svc.NewDaemonDial().OpenAccount(context.Background(), remote_device_svc.AccountArgs{
			URL:                       d.url(),
			TLSMode:                   "default",
			Credential:                fakeAccountJWT,
			DeviceFingerprint:         "sha256:desktop",
			ExpectedDaemonFingerprint: rpc.DaemonFingerprint("uuid-1"),
		})
		So(c, ShouldBeNil)
		So(errors.Is(err, remote_device_svc.ErrUnauthorized), ShouldBeTrue)
	})
}

// 账号握手同样受 TOFU 约束:接到的 daemon 不是本地登记的那台就断开,
// 否则连接会被 ConnPool 按 deviceID 缓存成「那台机器」。
func TestRealDial_OpenAccount_OtherDaemon_MapsToTOFUMismatch(t *testing.T) {
	Convey("the answering daemon is not the pinned one → ErrTOFUMismatch", t, func() {
		d := newFakeDaemon(t, "uuid-other", nil)

		c, err := remote_device_svc.NewDaemonDial().OpenAccount(context.Background(), remote_device_svc.AccountArgs{
			URL:                       d.url(),
			TLSMode:                   "default",
			Credential:                fakeAccountJWT,
			DeviceFingerprint:         "sha256:desktop",
			ExpectedDaemonFingerprint: rpc.DaemonFingerprint("uuid-1"),
		})
		So(c, ShouldBeNil)
		So(errors.Is(err, remote_device_svc.ErrTOFUMismatch), ShouldBeTrue)
	})

	Convey("no pinned daemon fingerprint at all → still refused, never silently accepted", t, func() {
		d := newFakeDaemon(t, "uuid-1", nil)

		c, err := remote_device_svc.NewDaemonDial().OpenAccount(context.Background(), remote_device_svc.AccountArgs{
			URL:               d.url(),
			TLSMode:           "default",
			Credential:        fakeAccountJWT,
			DeviceFingerprint: "sha256:desktop",
		})
		So(c, ShouldBeNil)
		So(errors.Is(err, remote_device_svc.ErrTOFUMismatch), ShouldBeTrue)
	})
}
