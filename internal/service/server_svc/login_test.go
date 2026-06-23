package server_svc_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/model/entity/server_state_entity"
	"github.com/agentre-ai/agentre/internal/pkg/keychain"
	"github.com/agentre-ai/agentre/internal/repository/server_state_repo"
	"github.com/agentre-ai/agentre/internal/repository/server_state_repo/mock_server_state_repo"
	"github.com/agentre-ai/agentre/internal/service/server_svc"
)

// setupServerSvc builds a ServerSvc wired to:
//   - a fresh httptest server URL (caller-provided)
//   - a fresh mock_server_state_repo (registered as the default)
//   - a fresh in-memory keychain (registered as the default)
//
// Returns (svc, mockRepo, keychain). emitState is captured via a *atomic.Pointer
// caller can inspect.
func setupServerSvc(t *testing.T, srvURL string) (server_svc.ServerSvc, *mock_server_state_repo.MockServerStateRepo, keychain.Keychain) {
	t.Helper()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	mRepo := mock_server_state_repo.NewMockServerStateRepo(ctrl)
	server_state_repo.RegisterServerState(mRepo)
	kc := keychain.NewMemory()
	keychain.SetDefault(kc)
	svc := server_svc.New(server_svc.NewHTTPClient(srvURL, ""), nil)
	return svc, mRepo, kc
}

func TestStartLogin_Success(t *testing.T) {
	Convey("StartLogin healthchecks then issues device-flow authorize and persists server_state", t, func() {
		var authorizeHits atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/v1/healthz":
				_, _ = w.Write([]byte(`{"code":0,"msg":"ok","data":{"version":"v0.1.0","status":"ok","db_ping":true,"redis":true}}`))
			case "/v1/oauth/device/authorize":
				authorizeHits.Add(1)
				_, _ = w.Write([]byte(`{"code":0,"msg":"ok","data":{"device_code":"dc","user_code":"A4F-7Q2","verification_uri":"http://h/device","verification_uri_complete":"http://h/device?user_code=A4F-7Q2","interval":5,"expires_in":600}}`))
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer srv.Close()

		svc, mRepo, _ := setupServerSvc(t, srv.URL)
		mRepo.EXPECT().Get(gomock.Any()).Return(&server_state_entity.ServerState{ID: 1, DeviceFingerprint: "fp-existing"}, nil)
		mRepo.EXPECT().Save(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, s *server_state_entity.ServerState) error {
				So(s.ServerURL, ShouldEqual, srv.URL)
				So(s.DeviceFingerprint, ShouldEqual, "fp-existing")
				return nil
			},
		)

		res, err := svc.StartLogin(context.Background(), srv.URL)
		So(err, ShouldBeNil)
		So(res.UserCode, ShouldEqual, "A4F-7Q2")
		So(res.VerificationURIComplete, ShouldEqual, "http://h/device?user_code=A4F-7Q2")
		So(authorizeHits.Load(), ShouldEqual, int32(1))
	})
}

func TestStartLogin_FreshInstall_PersistsFingerprintEarly(t *testing.T) {
	Convey("StartLogin on fresh install persists a generated fingerprint BEFORE authorize", t, func() {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/v1/healthz":
				_, _ = w.Write([]byte(`{"code":0,"msg":"ok","data":{"version":"v0.1.0"}}`))
			case "/v1/oauth/device/authorize":
				_, _ = w.Write([]byte(`{"code":0,"msg":"ok","data":{"device_code":"dc","user_code":"X","verification_uri":"http://h/device","verification_uri_complete":"http://h/device?user_code=X","interval":5,"expires_in":600}}`))
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer srv.Close()

		svc, mRepo, _ := setupServerSvc(t, srv.URL)

		gomock.InOrder(
			mRepo.EXPECT().Get(gomock.Any()).Return(&server_state_entity.ServerState{ID: 1}, nil),
			// 1) early fingerprint persist (fp non-empty, hub_url still empty)
			mRepo.EXPECT().Save(gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, s *server_state_entity.ServerState) error {
					So(s.DeviceFingerprint, ShouldNotBeEmpty)
					So(s.ServerURL, ShouldEqual, "")
					return nil
				},
			),
			// 2) post-authorize save (hub_url now set; fingerprint unchanged)
			mRepo.EXPECT().Save(gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, s *server_state_entity.ServerState) error {
					So(s.ServerURL, ShouldEqual, srv.URL)
					So(s.DeviceFingerprint, ShouldNotBeEmpty)
					return nil
				},
			),
		)

		_, err := svc.StartLogin(context.Background(), srv.URL)
		So(err, ShouldBeNil)
	})
}

func TestPollLoginToken_Pending(t *testing.T) {
	Convey("PollLoginToken returns (false, nil) when authorization_pending", t, func() {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"code":30200,"msg":"authorization pending","data":null,"error":"authorization_pending"}`))
		}))
		defer srv.Close()

		svc, _, _ := setupServerSvc(t, srv.URL)
		done, err := svc.PollLoginToken(context.Background(), "dc")
		So(err, ShouldBeNil)
		So(done, ShouldBeFalse)
	})
}

func TestPollLoginToken_Success(t *testing.T) {
	Convey("PollLoginToken persists token to keychain + updates server_state + emits logged_in", t, func() {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/v1/oauth/device/token":
				_, _ = w.Write([]byte(`{"code":0,"msg":"ok","data":{"access_token":"a1","refresh_token":"r1","token_type":"Bearer","expires_in":3600,"refresh_expires_in":7776000,"device_id":42}}`))
			case "/v1/auth/me":
				_, _ = w.Write([]byte(`{"code":0,"msg":"ok","data":{"user_id":7,"email":"u@e.com","display_name":"u","avatar_url":"https://avatars/x","github_login":"u","device_id":42}}`))
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer srv.Close()

		svc, mRepo, kc := setupServerSvc(t, srv.URL)

		mRepo.EXPECT().Get(gomock.Any()).Return(&server_state_entity.ServerState{ID: 1, ServerURL: srv.URL, DeviceFingerprint: "fp-abc"}, nil)
		mRepo.EXPECT().Save(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, s *server_state_entity.ServerState) error {
				So(s.DeviceID, ShouldEqual, int64(42))
				So(s.ServerUserID, ShouldEqual, int64(7))
				So(s.KeychainAccount, ShouldEqual, "agentre.server.refresh_token")
				return nil
			},
		)

		ok, err := svc.PollLoginToken(context.Background(), "dc")
		So(err, ShouldBeNil)
		So(ok, ShouldBeTrue)
		v, _ := kc.Get("agentre.server.refresh_token")
		So(v, ShouldEqual, "r1")
	})
}

func TestPollLoginToken_AccessDenied(t *testing.T) {
	Convey("PollLoginToken returns ErrAccessDenied when error=access_denied", t, func() {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"code":30203,"msg":"denied","data":null,"error":"access_denied"}`))
		}))
		defer srv.Close()
		svc, _, _ := setupServerSvc(t, srv.URL)
		_, err := svc.PollLoginToken(context.Background(), "dc")
		So(err, ShouldEqual, server_svc.ErrAccessDenied)
	})
}
