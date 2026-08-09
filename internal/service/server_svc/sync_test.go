package server_svc_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/model/entity/server_state_entity"
	"github.com/agentre-ai/agentre/internal/pkg/syncwire"
)

func loggedInState(url string) *server_state_entity.ServerState {
	return &server_state_entity.ServerState{
		ID: 1, ServerURL: url, ServerUserID: 7, DeviceID: 42,
		KeychainAccount: "agentre.server.refresh_token",
	}
}

func TestSyncPush(t *testing.T) {
	Convey("SyncPush 把一批改动发到 /v1/sync/push 并解析处置结果", t, func() {
		var body map[string]any
		var gotPath string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &body)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok","data":{"results":[
				{"sync_id":"p-1","kind":"project","version":12,"status":"conflict",
				 "overwritten_version":11,"overwritten_device_id":5}
			]}}`))
		}))
		defer srv.Close()

		svc, mRepo, _ := setupServerSvc(t, srv.URL)
		mRepo.EXPECT().Get(gomock.Any()).Return(loggedInState(srv.URL), nil)

		out, err := svc.SyncPush(context.Background(), []syncwire.PushItem{{
			Kind: "project", SyncID: "p-1", BaseVersion: 8, UpdatedAt: 1700,
			Payload: []byte(`{"name":"Alpha"}`),
		}})

		So(err, ShouldBeNil)
		So(gotPath, ShouldEqual, "/v1/sync/push")
		So(len(out), ShouldEqual, 1)
		So(out[0].Status, ShouldEqual, syncwire.PushStatusConflict)
		So(out[0].Version, ShouldEqual, int64(12))
		So(out[0].OverwrittenVersion, ShouldEqual, int64(11))
		So(out[0].OverwrittenDeviceID, ShouldEqual, int64(5))

		items := body["items"].([]any)
		first := items[0].(map[string]any)
		So(first["base_version"], ShouldEqual, float64(8))
		So(first["payload"].(map[string]any)["name"], ShouldEqual, "Alpha")
	})

	Convey("超窗口设备的上行翻成 ErrResyncRequired（R6a）", t, func() {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"code":30500,"msg":"resync required"}`))
		}))
		defer srv.Close()

		svc, mRepo, _ := setupServerSvc(t, srv.URL)
		mRepo.EXPECT().Get(gomock.Any()).Return(loggedInState(srv.URL), nil)

		_, err := svc.SyncPush(context.Background(), []syncwire.PushItem{{Kind: "project", SyncID: "p-1"}})
		So(err, ShouldEqual, syncwire.ErrResyncRequired)
	})

	Convey("未登录时一个网络请求都不发（R12）", t, func() {
		called := false
		srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
		defer srv.Close()

		svc, mRepo, _ := setupServerSvc(t, srv.URL)
		mRepo.EXPECT().Get(gomock.Any()).Return(&server_state_entity.ServerState{ID: 1}, nil)

		_, err := svc.SyncPush(context.Background(), []syncwire.PushItem{{Kind: "project", SyncID: "p-1"}})
		So(err, ShouldNotBeNil)
		So(called, ShouldBeFalse)
	})
}

func TestSyncPull(t *testing.T) {
	Convey("SyncPull 按游标增量下行，墓碑也在其中（R6）", t, func() {
		var gotQuery, gotPath string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			gotQuery = r.URL.RawQuery
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok","data":{"items":[
				{"kind":"project","sync_id":"p-1","payload":{"name":"A"},"version":11,
				 "updated_at":1700,"source_device_id":5,"deleted":false},
				{"kind":"project","sync_id":"p-2","payload":{},"version":12,"deleted":true}
			],"next_cursor":12,"has_more":false}}`))
		}))
		defer srv.Close()

		svc, mRepo, _ := setupServerSvc(t, srv.URL)
		mRepo.EXPECT().Get(gomock.Any()).Return(loggedInState(srv.URL), nil)

		page, err := svc.SyncPull(context.Background(), 10, 200)
		So(err, ShouldBeNil)
		So(gotPath, ShouldEqual, "/v1/sync/pull")
		So(gotQuery, ShouldEqual, "cursor=10&limit=200")
		So(len(page.Items), ShouldEqual, 2)
		So(page.Items[0].Version, ShouldEqual, int64(11))
		So(page.Items[0].SourceDeviceID, ShouldEqual, int64(5))
		So(page.Items[1].Deleted, ShouldBeTrue)
		So(page.NextCursor, ShouldEqual, int64(12))
		So(page.HasMore, ShouldBeFalse)
	})
}
