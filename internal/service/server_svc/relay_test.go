package server_svc_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/websocket"
	. "github.com/smartystreets/goconvey/convey"
	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/daemon/rpc"
	"github.com/agentre-ai/agentre/internal/model/entity/server_state_entity"
	"github.com/agentre-ai/agentre/internal/pkg/keychain"
	"github.com/agentre-ai/agentre/internal/repository/server_state_repo"
	"github.com/agentre-ai/agentre/internal/repository/server_state_repo/mock_server_state_repo"
	"github.com/agentre-ai/agentre/internal/service/server_svc"
)

// relayEndpointServer 起一个假的中转服务端:校验 Bearer 头,把任何路径升级成
// websocket,对 auth.account 请求回成功。用于验证 server_svc 的 relay 拨号。
func relayEndpointServer(t *testing.T, bearer string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer "+bearer {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		up := &websocket.Upgrader{}
		ws, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		for {
			var f rpc.Frame
			if err := ws.ReadJSON(&f); err != nil {
				return
			}
			_ = ws.WriteJSON(rpc.Frame{JSONRPC: "2.0", ID: f.ID, Result: json.RawMessage(`{"ok":true,"instanceUUID":"uuid-1"}`)})
		}
	}))
	return srv
}

// setupRelaySvc wires a logged-in server_svc with the given repo row + access token.
func setupRelaySvc(t *testing.T, row *server_state_entity.ServerState, token string) server_svc.ServerSvc {
	t.Helper()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	mRepo := mock_server_state_repo.NewMockServerStateRepo(ctrl)
	server_state_repo.RegisterServerState(mRepo)
	if row != nil {
		mRepo.EXPECT().Get(gomock.Any()).Return(row, nil)
	}
	keychain.SetDefault(keychain.NewMemory())
	svc := server_svc.New(server_svc.NewHTTPClient("http://relay.hub", token), nil)
	return svc
}

func TestDialDaemonRelay_NotLoggedIn(t *testing.T) {
	Convey("DialDaemonRelay with no persisted login → ErrNotLoggedIn, no dial", t, func() {
		svc := setupRelaySvc(t, &server_state_entity.ServerState{ID: 1}, "")
		_, err := svc.DialDaemonRelay(context.Background(), "sha256:daemon", "sha256:desktop")
		So(errors.Is(err, server_svc.ErrNotLoggedIn), ShouldBeTrue)
	})
}

func TestDialDaemonRelay_LoggedInDialAndHandshake(t *testing.T) {
	Convey("logged in: relay dial authenticates with the access token and completes auth.account", t, func() {
		srv := relayEndpointServer(t, "tok-9")
		defer srv.Close()

		row := &server_state_entity.ServerState{
			ID: 1, ServerURL: srv.URL, DeviceID: 1, ServerUserID: 1,
			KeychainAccount: "agentre.server.refresh_token",
		}
		ctrl := gomock.NewController(t)
		t.Cleanup(ctrl.Finish)
		mRepo := mock_server_state_repo.NewMockServerStateRepo(ctrl)
		server_state_repo.RegisterServerState(mRepo)
		mRepo.EXPECT().Get(gomock.Any()).Return(row, nil)
		keychain.SetDefault(keychain.NewMemory())
		svc := server_svc.New(server_svc.NewHTTPClient(srv.URL, "tok-9"), nil)

		c, err := svc.DialDaemonRelay(context.Background(), "sha256:daemon", "sha256:desktop")
		So(err, ShouldBeNil)
		So(c, ShouldNotBeNil)
		So(c.Closed(), ShouldNotBeNil)
		_ = c.Close()
	})
}
