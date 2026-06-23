package server_svc_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/model/entity/server_state_entity"
	"github.com/agentre-ai/agentre/internal/service/server_svc"
)

func TestListDevices(t *testing.T) {
	Convey("ListDevices returns parsed device list when logged in", t, func() {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/v1/devices" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok","data":{"devices":[
				{"id":42,"name":"m1","kind":"desktop","platform":"darwin/arm64","capabilities":{"compute":true},"last_seen_at":123,"status":1,"is_this_device":true},
				{"id":43,"name":"m2","kind":"agentred","platform":"linux/amd64","capabilities":{"compute":true,"client":true},"last_seen_at":456,"status":1,"is_this_device":false}
			]}}`))
		}))
		defer srv.Close()

		svc, mRepo, _ := setupServerSvc(t, srv.URL)
		mRepo.EXPECT().Get(gomock.Any()).Return(&server_state_entity.ServerState{
			ID: 1, ServerURL: srv.URL, ServerUserID: 7, DeviceID: 42, KeychainAccount: "agentre.server.refresh_token",
		}, nil)

		out, err := svc.ListDevices(context.Background())
		So(err, ShouldBeNil)
		So(len(out), ShouldEqual, 2)
		So(out[0].ID, ShouldEqual, int64(42))
		So(out[0].IsThisDevice, ShouldBeTrue)
		So(out[0].Capabilities["compute"], ShouldBeTrue)
		So(out[1].IsThisDevice, ShouldBeFalse)
		So(out[1].Capabilities["client"], ShouldBeTrue)
	})

	Convey("ListDevices returns ErrNotLoggedIn when server_state is unbound", t, func() {
		svc, mRepo, _ := setupServerSvc(t, "http://unused")
		mRepo.EXPECT().Get(gomock.Any()).Return(&server_state_entity.ServerState{ID: 1}, nil)
		_, err := svc.ListDevices(context.Background())
		So(err, ShouldEqual, server_svc.ErrNotLoggedIn)
	})
}

func TestLogout(t *testing.T) {
	Convey("Logout best-effort revokes server-side then clears local state", t, func() {
		revokeHits := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v1/oauth/token/revoke" {
				revokeHits++
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"code":0,"msg":"ok","data":{}}`))
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()

		svc, mRepo, kc := setupServerSvc(t, srv.URL)
		// pre-seed a refresh token to confirm it's deleted
		_ = kc.Set("agentre.server.refresh_token", "rt-existing")

		mRepo.EXPECT().ClearLoginFields(gomock.Any()).Return(nil)

		err := svc.Logout(context.Background())
		So(err, ShouldBeNil)
		So(revokeHits, ShouldEqual, 1)
		v, getErr := kc.Get("agentre.server.refresh_token")
		So(v, ShouldEqual, "")
		_ = getErr // ErrNotFound expected
	})

	Convey("Logout still clears local state when remote revoke fails", t, func() {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		svc, mRepo, kc := setupServerSvc(t, srv.URL)
		_ = kc.Set("agentre.server.refresh_token", "rt-existing")
		mRepo.EXPECT().ClearLoginFields(gomock.Any()).Return(nil)

		err := svc.Logout(context.Background())
		So(err, ShouldBeNil)
		v, _ := kc.Get("agentre.server.refresh_token")
		So(v, ShouldEqual, "")
	})
}

func TestGetState(t *testing.T) {
	Convey("GetState returns the persisted row when present", t, func() {
		svc, mRepo, _ := setupServerSvc(t, "http://unused")
		mRepo.EXPECT().Get(gomock.Any()).Return(&server_state_entity.ServerState{ID: 1, ServerURL: "https://h"}, nil)
		got, err := svc.GetState(context.Background())
		So(err, ShouldBeNil)
		So(got.ServerURL, ShouldEqual, "https://h")
	})

	Convey("GetState returns a zero-value row when repo returns nil", t, func() {
		svc, mRepo, _ := setupServerSvc(t, "http://unused")
		mRepo.EXPECT().Get(gomock.Any()).Return(nil, nil)
		got, err := svc.GetState(context.Background())
		So(err, ShouldBeNil)
		So(got, ShouldNotBeNil)
		So(got.ID, ShouldEqual, int64(1))
		So(got.IsLoggedIn(), ShouldBeFalse)
	})
}
