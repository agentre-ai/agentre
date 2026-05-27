package remote_device_svc_test

import (
	"context"
	"errors"
	"testing"

	"github.com/cago-frame/cago/pkg/consts"
	. "github.com/smartystreets/goconvey/convey"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"agentre/internal/daemon/handlers"
	"agentre/internal/model/entity/llm_provider_entity"
	"agentre/internal/pkg/agentruntime/mock_agentruntime"
	"agentre/internal/repository/llm_provider_repo"
	llmrepomock "agentre/internal/repository/llm_provider_repo/mock_llm_provider_repo"
	remoterepomock "agentre/internal/repository/remote_device_repo/mock_remote_device_repo"
	"agentre/internal/service/remote_device_svc"
	svcmock "agentre/internal/service/remote_device_svc/mock_remote_device_svc"
)

func TestRemoteDeviceSvc_SyncProvider(t *testing.T) {
	Convey("SyncProvider", t, func() {
		ctrl := gomock.NewController(t)
		t.Cleanup(ctrl.Finish)
		t.Cleanup(func() { llm_provider_repo.RegisterLLMProvider(nil) })

		providerRepo := llmrepomock.NewMockLLMProviderRepo(ctrl)
		llm_provider_repo.RegisterLLMProvider(providerRepo)
		deviceRepo := remoterepomock.NewMockPairedAgentredRepo(ctrl)
		dial := svcmock.NewMockDaemonDialPort(ctrl)
		kc := svcmock.NewMockKeychainPort(ctrl)
		pool := svcmock.NewMockConnPool(ctrl)
		svc := remote_device_svc.New(deviceRepo, dial, kc, pool)

		provider := &llm_provider_entity.LLMProvider{
			ProviderKey: "prov-1",
			Name:        "Anthropic Prod",
			Type:        string(llm_provider_entity.TypeAnthropic),
			BaseURL:     "https://api.anthropic.com",
			Model:       "claude-sonnet-4-6",
			APIKey:      "sk-secret",
			Updatetime:  1716000500,
			Status:      consts.ACTIVE,
		}

		Convey("copies local provider metadata and API key to remote llm.upsert", func() {
			lease := svcmock.NewMockLease(ctrl)
			client := mock_agentruntime.NewMockDaemonClientPort(ctrl)
			providerRepo.EXPECT().FindByKey(gomock.Any(), "prov-1").Return(provider, nil)
			pool.EXPECT().Borrow(gomock.Any(), int64(42)).Return(lease, nil)
			lease.EXPECT().Client().Return(client)
			client.EXPECT().
				Call(gomock.Any(), "llm.upsert", gomock.Any(), gomock.Any()).
				DoAndReturn(func(_ context.Context, _ string, params, result any) error {
					got, ok := params.(handlers.LLMUpsertParams)
					require.True(t, ok)
					So(got.ProviderKey, ShouldEqual, "prov-1")
					So(got.Name, ShouldEqual, "Anthropic Prod")
					So(got.Type, ShouldEqual, "anthropic")
					So(got.BaseURL, ShouldEqual, "https://api.anthropic.com")
					So(got.Model, ShouldEqual, "claude-sonnet-4-6")
					So(got.APIKey, ShouldEqual, "sk-secret")
					So(got.UpdatedAt, ShouldEqual, int64(1716000500))
					_, ok = result.(*handlers.OK)
					require.True(t, ok)
					return nil
				})
			lease.EXPECT().Release()

			err := svc.SyncProvider(context.Background(), 42, "prov-1")
			So(err, ShouldBeNil)

			cached := svc.ListDeviceProviders(42)
			So(cached, ShouldHaveLength, 1)
			So(cached[0].Key, ShouldEqual, "prov-1")
			So(cached[0].Name, ShouldEqual, "Anthropic Prod")
			So(cached[0].Type, ShouldEqual, "anthropic")
		})

		Convey("returns provider-not-found before dialing when local provider is missing", func() {
			providerRepo.EXPECT().FindByKey(gomock.Any(), "missing").Return(nil, nil)

			err := svc.SyncProvider(context.Background(), 42, "missing")
			So(err, ShouldNotBeNil)
		})

		Convey("releases the lease and leaves cache untouched when remote upsert fails", func() {
			lease := svcmock.NewMockLease(ctrl)
			client := mock_agentruntime.NewMockDaemonClientPort(ctrl)
			providerRepo.EXPECT().FindByKey(gomock.Any(), "prov-1").Return(provider, nil)
			pool.EXPECT().Borrow(gomock.Any(), int64(42)).Return(lease, nil)
			lease.EXPECT().Client().Return(client)
			client.EXPECT().
				Call(gomock.Any(), "llm.upsert", gomock.Any(), gomock.Any()).
				Return(errors.New("remote boom"))
			lease.EXPECT().Release()

			err := svc.SyncProvider(context.Background(), 42, "prov-1")
			So(err, ShouldNotBeNil)
			So(svc.ListDeviceProviders(42), ShouldBeNil)
		})
	})
}
