package data_svc_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/cago-frame/cago/database/db"
	"github.com/cago-frame/cago/pkg/consts"
	"github.com/cago-frame/cago/pkg/utils/testutils"
	. "github.com/smartystreets/goconvey/convey"
	"go.uber.org/mock/gomock"

	"agentre/internal/model/entity/agent_backend_entity"
	"agentre/internal/model/entity/agent_entity"
	"agentre/internal/model/entity/department_entity"
	"agentre/internal/model/entity/llm_provider_entity"
	"agentre/internal/model/entity/paired_agentred_entity"
	"agentre/internal/repository/agent_backend_repo"
	"agentre/internal/repository/agent_backend_repo/mock_agent_backend_repo"
	"agentre/internal/repository/agent_repo"
	"agentre/internal/repository/agent_repo/mock_agent_repo"
	"agentre/internal/repository/department_repo"
	"agentre/internal/repository/department_repo/mock_department_repo"
	"agentre/internal/repository/llm_provider_repo"
	"agentre/internal/repository/llm_provider_repo/mock_llm_provider_repo"
	"agentre/internal/repository/remote_device_repo"
	"agentre/internal/repository/remote_device_repo/mock_remote_device_repo"
	"agentre/internal/service/data_svc"
)

type dataSvcMocks struct {
	ctx       context.Context
	providers *mock_llm_provider_repo.MockLLMProviderRepo
	backends  *mock_agent_backend_repo.MockAgentBackendRepo
	depts     *mock_department_repo.MockDepartmentRepo
	agents    *mock_agent_repo.MockAgentRepo
	devices   *mock_remote_device_repo.MockPairedAgentredRepo
	dbMock    sqlmock.Sqlmock
	svc       data_svc.DataSvc
}

// setupDataSvcTest 注入 5 个 mock repo + sqlmock,返回测试句柄。
func setupDataSvcTest(t *testing.T) *dataSvcMocks {
	t.Helper()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	dbCtx, _, dbMock := testutils.Database(t)
	_ = db.Ctx(dbCtx) // 提示编译器 dbCtx 已挂上 db

	m := &dataSvcMocks{
		ctx:       dbCtx,
		providers: mock_llm_provider_repo.NewMockLLMProviderRepo(ctrl),
		backends:  mock_agent_backend_repo.NewMockAgentBackendRepo(ctrl),
		depts:     mock_department_repo.NewMockDepartmentRepo(ctrl),
		agents:    mock_agent_repo.NewMockAgentRepo(ctrl),
		devices:   mock_remote_device_repo.NewMockPairedAgentredRepo(ctrl),
		dbMock:    dbMock,
	}
	llm_provider_repo.RegisterLLMProvider(m.providers)
	agent_backend_repo.RegisterAgentBackend(m.backends)
	department_repo.RegisterDepartment(m.depts)
	agent_repo.RegisterAgent(m.agents)
	remote_device_repo.RegisterPairedAgentred(m.devices)

	m.svc = data_svc.Default()
	return m
}

func TestExport_LLMProvidersOnly_Scrubbed(t *testing.T) {
	m := setupDataSvcTest(t)

	rows := []*llm_provider_entity.LLMProvider{
		{
			ID: 1, ProviderKey: "key-1", Type: "anthropic", Name: "Main",
			APIKey: "secret", BaseURL: "https://x", Model: "claude",
			MaxOutput: 8192, ContextWindow: 200000, Status: consts.ACTIVE,
		},
	}
	m.providers.EXPECT().List(gomock.Any()).Return(rows, nil)

	Convey("Export llm-providers without secrets", t, func() {
		res, err := m.svc.Export(m.ctx, &data_svc.ExportRequest{
			Scopes:         []string{string(data_svc.ScopeLLMProviders)},
			IncludeSecrets: false,
		})
		So(err, ShouldBeNil)
		So(res, ShouldNotBeNil)

		var bundle data_svc.BundleV1
		So(json.Unmarshal(res.JSON, &bundle), ShouldBeNil)
		So(bundle.Format, ShouldEqual, data_svc.BundleFormat)
		So(bundle.Version, ShouldEqual, data_svc.BundleVersion)
		So(bundle.SecretsIncluded, ShouldBeFalse)
		So(bundle.Items.LLMProviders, ShouldHaveLength, 1)

		p := bundle.Items.LLMProviders[0]
		So(p.ProviderKey, ShouldEqual, "key-1")
		So(p.Name, ShouldEqual, "Main")
		So(p.APIKey, ShouldEqual, "") // 关键断言:脱敏
		So(res.Summary[string(data_svc.ScopeLLMProviders)], ShouldEqual, 1)
	})
}

func TestExport_LLMProviders_IncludeSecrets(t *testing.T) {
	m := setupDataSvcTest(t)
	m.providers.EXPECT().List(gomock.Any()).Return([]*llm_provider_entity.LLMProvider{
		{ID: 1, ProviderKey: "k1", Type: "anthropic", Name: "M", APIKey: "sk-xxx"},
	}, nil)

	Convey("Export 携带 includeSecrets", t, func() {
		res, err := m.svc.Export(m.ctx, &data_svc.ExportRequest{
			Scopes: []string{string(data_svc.ScopeLLMProviders)}, IncludeSecrets: true,
		})
		So(err, ShouldBeNil)
		var b data_svc.BundleV1
		So(json.Unmarshal(res.JSON, &b), ShouldBeNil)
		So(b.SecretsIncluded, ShouldBeTrue)
		So(b.Items.LLMProviders[0].APIKey, ShouldEqual, "sk-xxx")
	})
}

func TestExport_Organization_CrossRefsViaExportKey(t *testing.T) {
	m := setupDataSvcTest(t)
	m.depts.EXPECT().List(gomock.Any()).Return([]*department_entity.Department{
		{ID: 10, Name: "Eng", ParentID: 0, LeadAgentID: 20},
		{ID: 11, Name: "Backend", ParentID: 10, LeadAgentID: 0},
	}, nil)
	m.agents.EXPECT().List(gomock.Any()).Return([]*agent_entity.Agent{
		{ID: 20, Name: "Lead", DepartmentID: 10, AgentBackendID: 30, ParentAgentID: 0},
		{ID: 21, Name: "IC", DepartmentID: 11, AgentBackendID: 30, ParentAgentID: 20},
	}, nil)
	m.backends.EXPECT().List(gomock.Any()).Return([]*agent_backend_entity.AgentBackend{
		{ID: 30, Type: "claudecode", Name: "Local"},
	}, nil)

	Convey("Organization scope 串好 exportKey 引用", t, func() {
		res, err := m.svc.Export(m.ctx, &data_svc.ExportRequest{
			Scopes: []string{string(data_svc.ScopeOrganization)},
		})
		So(err, ShouldBeNil)
		var b data_svc.BundleV1
		So(json.Unmarshal(res.JSON, &b), ShouldBeNil)
		So(b.Items.Departments, ShouldHaveLength, 2)
		So(b.Items.Agents, ShouldHaveLength, 2)

		// 找 "Backend" 部门,它的 parentKey 必须指向 "Eng" 的 exportKey
		var eng, back data_svc.BundleDepartment
		for _, d := range b.Items.Departments {
			if d.Name == "Eng" {
				eng = d
			}
			if d.Name == "Backend" {
				back = d
			}
		}
		So(back.ParentKey, ShouldEqual, eng.ExportKey)
		So(eng.LeadAgentKey, ShouldNotBeEmpty)
		// 找 IC,parentAgentKey 必须指向 Lead 的 exportKey
		var lead, ic data_svc.BundleAgent
		for _, a := range b.Items.Agents {
			if a.Name == "Lead" {
				lead = a
			}
			if a.Name == "IC" {
				ic = a
			}
		}
		So(ic.ParentAgentKey, ShouldEqual, lead.ExportKey)
	})
}

func TestExport_UnknownScope_Errors(t *testing.T) {
	m := setupDataSvcTest(t)
	Convey("未知 scope 应报错", t, func() {
		_, err := m.svc.Export(m.ctx, &data_svc.ExportRequest{
			Scopes: []string{"nonsense"},
		})
		So(err, ShouldNotBeNil)
	})
}

func TestPreviewImport_RejectsBadFormat(t *testing.T) {
	m := setupDataSvcTest(t)
	Convey("Format 不对应拒收", t, func() {
		_, err := m.svc.PreviewImport(m.ctx, []byte(`{"format":"foo","version":1}`))
		So(err, ShouldNotBeNil)
	})
	Convey("Version > 1 拒收", t, func() {
		_, err := m.svc.PreviewImport(m.ctx, []byte(`{"format":"agentre-data-bundle","version":2}`))
		So(err, ShouldNotBeNil)
	})
}

func TestPreviewImport_NoConflict_DefaultsCreate(t *testing.T) {
	m := setupDataSvcTest(t)
	m.providers.EXPECT().List(gomock.Any()).Return([]*llm_provider_entity.LLMProvider{}, nil)
	m.devices.EXPECT().List(gomock.Any()).Return([]*paired_agentred_entity.PairedAgentred{}, nil)
	m.backends.EXPECT().List(gomock.Any()).Return([]*agent_backend_entity.AgentBackend{}, nil)
	m.depts.EXPECT().List(gomock.Any()).Return([]*department_entity.Department{}, nil)

	bundle := data_svc.BundleV1{
		Format: data_svc.BundleFormat, Version: 1,
		Scopes: []string{string(data_svc.ScopeLLMProviders)},
		Items:  data_svc.BundleItems{LLMProviders: []data_svc.BundleLLMProvider{{ProviderKey: "k1", Name: "P1"}}},
	}
	raw, _ := json.Marshal(bundle)

	Convey("无冲突,默认 create", t, func() {
		pv, err := m.svc.PreviewImport(m.ctx, raw)
		So(err, ShouldBeNil)
		So(pv.Items, ShouldHaveLength, 1)
		So(pv.Items[0].Conflict, ShouldBeFalse)
		So(pv.Items[0].DefaultAction, ShouldEqual, data_svc.ActionCreate)
	})
}

func TestPreviewImport_ProviderKeyConflict(t *testing.T) {
	m := setupDataSvcTest(t)
	m.providers.EXPECT().List(gomock.Any()).Return([]*llm_provider_entity.LLMProvider{
		{ID: 5, ProviderKey: "k1", Name: "Local Name"},
	}, nil)
	m.devices.EXPECT().List(gomock.Any()).Return(nil, nil)
	m.backends.EXPECT().List(gomock.Any()).Return(nil, nil)
	m.depts.EXPECT().List(gomock.Any()).Return(nil, nil)

	bundle := data_svc.BundleV1{
		Format: data_svc.BundleFormat, Version: 1,
		Scopes: []string{string(data_svc.ScopeLLMProviders)},
		Items:  data_svc.BundleItems{LLMProviders: []data_svc.BundleLLMProvider{{ProviderKey: "k1", Name: "Bundle Name"}}},
	}
	raw, _ := json.Marshal(bundle)

	Convey("同 providerKey 标 conflict", t, func() {
		pv, err := m.svc.PreviewImport(m.ctx, raw)
		So(err, ShouldBeNil)
		So(pv.Items[0].Conflict, ShouldBeTrue)
		So(pv.Items[0].LocalID, ShouldEqual, 5)
		So(pv.Items[0].LocalName, ShouldEqual, "Local Name")
		So(pv.Items[0].DefaultAction, ShouldEqual, data_svc.ActionSkip)
	})
}

func TestPreviewImport_BackendRefsMissingProvider_Dangling(t *testing.T) {
	m := setupDataSvcTest(t)
	m.providers.EXPECT().List(gomock.Any()).Return(nil, nil)
	m.devices.EXPECT().List(gomock.Any()).Return(nil, nil)
	m.backends.EXPECT().List(gomock.Any()).Return(nil, nil)
	m.depts.EXPECT().List(gomock.Any()).Return(nil, nil)

	bundle := data_svc.BundleV1{
		Format: data_svc.BundleFormat, Version: 1,
		Scopes: []string{string(data_svc.ScopeAgentBackends)},
		Items: data_svc.BundleItems{
			AgentBackends: []data_svc.BundleAgentBackend{
				{ExportKey: "ab-1", Name: "B1", LLMProviderKey: "missing-key"},
			},
		},
	}
	raw, _ := json.Marshal(bundle)

	Convey("backend 引用未导入的 provider → dangling + 强制 skip", t, func() {
		pv, err := m.svc.PreviewImport(m.ctx, raw)
		So(err, ShouldBeNil)
		So(pv.Items[0].Dangling, ShouldBeTrue)
		So(pv.Items[0].DefaultAction, ShouldEqual, data_svc.ActionSkip)
	})
}

func TestApplyImport_Providers_Create(t *testing.T) {
	m := setupDataSvcTest(t)

	// PreviewImport calls providers+devices+backends+depts once each.
	// applyProviders calls providers.List again; applyRemoteDevices calls devices.List again;
	// applyAgentBackends calls backends.List again.
	m.providers.EXPECT().List(gomock.Any()).Return([]*llm_provider_entity.LLMProvider{}, nil).Times(2)
	m.devices.EXPECT().List(gomock.Any()).Return(nil, nil).Times(2)
	m.backends.EXPECT().List(gomock.Any()).Return(nil, nil).Times(2)
	m.depts.EXPECT().List(gomock.Any()).Return(nil, nil)

	m.providers.EXPECT().Create(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, p *llm_provider_entity.LLMProvider) error {
			p.ID = 100
			return nil
		})

	m.dbMock.ExpectBegin()
	m.dbMock.ExpectCommit()

	bundle := data_svc.BundleV1{
		Format: data_svc.BundleFormat, Version: 1,
		Scopes: []string{string(data_svc.ScopeLLMProviders)},
		Items: data_svc.BundleItems{LLMProviders: []data_svc.BundleLLMProvider{
			{ProviderKey: "k1", Name: "P1", Type: "anthropic", APIKey: "sk-x"},
		}},
	}
	raw, _ := json.Marshal(bundle)

	Convey("create 新行,事务提交", t, func() {
		res, err := m.svc.ApplyImport(m.ctx, &data_svc.ApplyImportRequest{
			Raw:              raw,
			FallbackStrategy: data_svc.ActionCreate,
		})
		So(err, ShouldBeNil)
		So(res.Counts["created"], ShouldEqual, 1)
		So(m.dbMock.ExpectationsWereMet(), ShouldBeNil)
	})
}

func TestApplyImport_Providers_SkipConflict(t *testing.T) {
	m := setupDataSvcTest(t)
	existing := []*llm_provider_entity.LLMProvider{{ID: 5, ProviderKey: "k1", Name: "P1"}}
	m.providers.EXPECT().List(gomock.Any()).Return(existing, nil).Times(2)
	m.devices.EXPECT().List(gomock.Any()).Return(nil, nil).Times(2)
	m.backends.EXPECT().List(gomock.Any()).Return(nil, nil).Times(2)
	m.depts.EXPECT().List(gomock.Any()).Return(nil, nil)
	// 不 EXPECT Create / Update — 必须不调

	m.dbMock.ExpectBegin()
	m.dbMock.ExpectCommit()

	bundle := data_svc.BundleV1{
		Format: data_svc.BundleFormat, Version: 1,
		Scopes: []string{string(data_svc.ScopeLLMProviders)},
		Items: data_svc.BundleItems{LLMProviders: []data_svc.BundleLLMProvider{
			{ProviderKey: "k1", Name: "P1"},
		}},
	}
	raw, _ := json.Marshal(bundle)

	Convey("skip 不调写方法", t, func() {
		res, err := m.svc.ApplyImport(m.ctx, &data_svc.ApplyImportRequest{
			Raw:              raw,
			FallbackStrategy: data_svc.ActionSkip,
		})
		So(err, ShouldBeNil)
		So(res.Counts["skipped"], ShouldEqual, 1)
	})
}

func TestApplyImport_Providers_Overwrite(t *testing.T) {
	m := setupDataSvcTest(t)
	existing := []*llm_provider_entity.LLMProvider{{ID: 5, ProviderKey: "k1", Name: "Old", Status: consts.ACTIVE}}
	m.providers.EXPECT().List(gomock.Any()).Return(existing, nil).Times(2)
	m.devices.EXPECT().List(gomock.Any()).Return(nil, nil).Times(2)
	m.backends.EXPECT().List(gomock.Any()).Return(nil, nil).Times(2)
	m.depts.EXPECT().List(gomock.Any()).Return(nil, nil)

	m.providers.EXPECT().Update(gomock.Any(), gomock.AssignableToTypeOf(&llm_provider_entity.LLMProvider{})).
		DoAndReturn(func(_ context.Context, p *llm_provider_entity.LLMProvider) error {
			So(p.ID, ShouldEqual, 5)
			So(p.Name, ShouldEqual, "New")
			So(p.Status, ShouldEqual, consts.ACTIVE) // 保留本地原 status
			return nil
		})

	m.dbMock.ExpectBegin()
	m.dbMock.ExpectCommit()

	bundle := data_svc.BundleV1{
		Format: data_svc.BundleFormat, Version: 1,
		Scopes: []string{string(data_svc.ScopeLLMProviders)},
		Items: data_svc.BundleItems{LLMProviders: []data_svc.BundleLLMProvider{
			{ProviderKey: "k1", Name: "New"},
		}},
	}
	raw, _ := json.Marshal(bundle)

	Convey("overwrite 调 Update,保留 status", t, func() {
		res, err := m.svc.ApplyImport(m.ctx, &data_svc.ApplyImportRequest{
			Raw:              raw,
			FallbackStrategy: data_svc.ActionOverwrite,
		})
		So(err, ShouldBeNil)
		So(res.Counts["overwrote"], ShouldEqual, 1)
	})
}

func TestApplyImport_Backend_ResolvesProviderRef(t *testing.T) {
	m := setupDataSvcTest(t)
	m.providers.EXPECT().List(gomock.Any()).Return([]*llm_provider_entity.LLMProvider{}, nil).Times(2)
	m.devices.EXPECT().List(gomock.Any()).Return(nil, nil).Times(2)
	m.backends.EXPECT().List(gomock.Any()).Return([]*agent_backend_entity.AgentBackend{}, nil).Times(2)
	m.depts.EXPECT().List(gomock.Any()).Return(nil, nil)

	// 先 Create provider
	m.providers.EXPECT().Create(gomock.Any(), gomock.AssignableToTypeOf(&llm_provider_entity.LLMProvider{})).
		DoAndReturn(func(_ context.Context, p *llm_provider_entity.LLMProvider) error {
			p.ID = 50
			return nil
		})
	// 再 Create backend,其 llm_provider_key 必须等于 bundle 里的 key1(provider_key 是 stable,本就传过去)
	m.backends.EXPECT().Create(gomock.Any(), gomock.AssignableToTypeOf(&agent_backend_entity.AgentBackend{})).
		DoAndReturn(func(_ context.Context, bk *agent_backend_entity.AgentBackend) error {
			So(bk.LLMProviderKey, ShouldEqual, "key1")
			bk.ID = 60
			return nil
		})

	m.dbMock.ExpectBegin()
	m.dbMock.ExpectCommit()

	bundle := data_svc.BundleV1{
		Format: data_svc.BundleFormat, Version: 1,
		Scopes: []string{string(data_svc.ScopeLLMProviders), string(data_svc.ScopeAgentBackends)},
		Items: data_svc.BundleItems{
			LLMProviders: []data_svc.BundleLLMProvider{{ProviderKey: "key1", Name: "P"}},
			AgentBackends: []data_svc.BundleAgentBackend{
				{ExportKey: "ab-1", Type: "claudecode", Name: "B", LLMProviderKey: "key1"},
			},
		},
	}
	raw, _ := json.Marshal(bundle)

	Convey("backend 引用 provider,wire 后保留 stable key", t, func() {
		res, err := m.svc.ApplyImport(m.ctx, &data_svc.ApplyImportRequest{
			Raw: raw, FallbackStrategy: data_svc.ActionCreate,
		})
		So(err, ShouldBeNil)
		So(res.Counts["created"], ShouldEqual, 2)
	})
}

func TestApplyImport_Backend_ResolvesRemoteDeviceRef(t *testing.T) {
	m := setupDataSvcTest(t)
	localDevice := &paired_agentred_entity.PairedAgentred{ID: 5, InstanceUUID: "uuid-1", Name: "Server1"}
	m.providers.EXPECT().List(gomock.Any()).Return(nil, nil).Times(2)
	m.devices.EXPECT().List(gomock.Any()).Return([]*paired_agentred_entity.PairedAgentred{localDevice}, nil).Times(2)
	m.backends.EXPECT().List(gomock.Any()).Return([]*agent_backend_entity.AgentBackend{}, nil).Times(2)
	m.depts.EXPECT().List(gomock.Any()).Return(nil, nil).Times(1)

	m.backends.EXPECT().Create(gomock.Any(), gomock.AssignableToTypeOf(&agent_backend_entity.AgentBackend{})).
		DoAndReturn(func(_ context.Context, bk *agent_backend_entity.AgentBackend) error {
			So(bk.DeviceID, ShouldEqual, "5")
			bk.ID = 60
			return nil
		})

	m.dbMock.ExpectBegin()
	m.dbMock.ExpectCommit()

	bundle := data_svc.BundleV1{
		Format: data_svc.BundleFormat, Version: 1,
		Scopes: []string{string(data_svc.ScopeRemoteDevices), string(data_svc.ScopeAgentBackends)},
		Items: data_svc.BundleItems{
			RemoteDevices: []data_svc.BundleRemoteDevice{
				{InstanceUUID: "uuid-1", Name: "Server1"},
			},
			AgentBackends: []data_svc.BundleAgentBackend{
				{ExportKey: "ab-1", Type: "codex", Name: "Remote Codex", DeviceID: "uuid-1"},
			},
		},
	}
	raw, _ := json.Marshal(bundle)

	Convey("backend 引用远端设备 instanceUUID 时,落库为本地 row ID", t, func() {
		res, err := m.svc.ApplyImport(m.ctx, &data_svc.ApplyImportRequest{
			Raw: raw, FallbackStrategy: data_svc.ActionSkip,
		})
		So(err, ShouldBeNil)
		So(res.Counts["skipped"], ShouldEqual, 1)
		So(res.Counts["created"], ShouldEqual, 1)
	})
}

func TestApplyImport_Backend_FollowsDuplicatedRemoteDevice(t *testing.T) {
	m := setupDataSvcTest(t)
	localDevice := &paired_agentred_entity.PairedAgentred{ID: 5, InstanceUUID: "uuid-1", Name: "Server1"}
	m.providers.EXPECT().List(gomock.Any()).Return(nil, nil).Times(2)
	m.devices.EXPECT().List(gomock.Any()).Return([]*paired_agentred_entity.PairedAgentred{localDevice}, nil).Times(2)
	m.backends.EXPECT().List(gomock.Any()).Return([]*agent_backend_entity.AgentBackend{}, nil).Times(2)
	m.depts.EXPECT().List(gomock.Any()).Return(nil, nil).Times(1)

	m.devices.EXPECT().Create(gomock.Any(), gomock.AssignableToTypeOf(&paired_agentred_entity.PairedAgentred{})).
		DoAndReturn(func(_ context.Context, d *paired_agentred_entity.PairedAgentred) error {
			So(d.Name, ShouldEqual, "Server1 (copy)")
			d.ID = 99
			return nil
		})
	m.backends.EXPECT().Create(gomock.Any(), gomock.AssignableToTypeOf(&agent_backend_entity.AgentBackend{})).
		DoAndReturn(func(_ context.Context, bk *agent_backend_entity.AgentBackend) error {
			So(bk.DeviceID, ShouldEqual, "99")
			bk.ID = 60
			return nil
		})

	m.dbMock.ExpectBegin()
	m.dbMock.ExpectCommit()

	bundle := data_svc.BundleV1{
		Format: data_svc.BundleFormat, Version: 1,
		Scopes: []string{string(data_svc.ScopeRemoteDevices), string(data_svc.ScopeAgentBackends)},
		Items: data_svc.BundleItems{
			RemoteDevices: []data_svc.BundleRemoteDevice{
				{InstanceUUID: "uuid-1", Name: "Server1"},
			},
			AgentBackends: []data_svc.BundleAgentBackend{
				{ExportKey: "ab-1", Type: "codex", Name: "Remote Codex", DeviceID: "5"},
			},
		},
	}
	raw, _ := json.Marshal(bundle)

	Convey("旧包数字 deviceId 在远端设备 duplicate 后绑定新设备", t, func() {
		res, err := m.svc.ApplyImport(m.ctx, &data_svc.ApplyImportRequest{
			Raw: raw,
			Actions: map[string]data_svc.ItemAction{
				"remote-devices:uuid-1": data_svc.ActionDuplicate,
			},
			FallbackStrategy: data_svc.ActionCreate,
		})
		So(err, ShouldBeNil)
		So(res.Counts["duplicated"], ShouldEqual, 1)
		So(res.Counts["created"], ShouldEqual, 1)
	})
}

func TestApplyImport_Org_TwoPassBackfill(t *testing.T) {
	m := setupDataSvcTest(t)
	// PreviewImport calls providers+devices+backends+depts once each.
	// applyProviders/applyRemoteDevices/applyAgentBackends each call their respective List once.
	// applyDepartments uses FindByName (not List). agents.List is never called.
	m.providers.EXPECT().List(gomock.Any()).Return(nil, nil).Times(2)
	m.devices.EXPECT().List(gomock.Any()).Return(nil, nil).Times(2)
	m.backends.EXPECT().List(gomock.Any()).Return(nil, nil).Times(2)
	m.depts.EXPECT().List(gomock.Any()).Return(nil, nil).Times(1)
	// agents.List is NOT called (preview doesn't call it, applyAgents uses ListByDepartment)

	// 部门两个:Eng(根) + Backend(parent=Eng);Eng.LeadAgentKey 指向 Lead
	// Agent 两个:Lead(部门 Eng) + IC(部门 Backend,parent Lead)

	// applyDepartments calls FindByName for each dept (Eng: parentID=0, Backend: parentID=100)
	m.depts.EXPECT().FindByName(gomock.Any(), "Eng", int64(0)).Return(nil, nil)
	m.depts.EXPECT().FindByName(gomock.Any(), "Backend", int64(100)).Return(nil, nil)

	// applyAgents calls ListByDepartment per agent
	m.agents.EXPECT().ListByDepartment(gomock.Any(), int64(100)).Return([]*agent_entity.Agent{}, nil)
	m.agents.EXPECT().ListByDepartment(gomock.Any(), int64(101)).Return([]*agent_entity.Agent{}, nil)

	m.depts.EXPECT().Create(gomock.Any(), gomock.AssignableToTypeOf(&department_entity.Department{})).
		DoAndReturn(func(_ context.Context, d *department_entity.Department) error {
			switch d.Name {
			case "Eng":
				d.ID = 100
				So(d.ParentID, ShouldEqual, 0)
			case "Backend":
				d.ID = 101
				So(d.ParentID, ShouldEqual, 100) // 已通过 keymap 解析到 Eng
			}
			return nil
		}).Times(2)
	m.agents.EXPECT().Create(gomock.Any(), gomock.AssignableToTypeOf(&agent_entity.Agent{})).
		DoAndReturn(func(_ context.Context, a *agent_entity.Agent) error {
			switch a.Name {
			case "Lead":
				a.ID = 200
				So(a.DepartmentID, ShouldEqual, 100)
				So(a.ParentAgentID, ShouldEqual, 0) // 第一遍尚未回填
			case "IC":
				a.ID = 201
				So(a.DepartmentID, ShouldEqual, 101)
				So(a.ParentAgentID, ShouldEqual, 0) // 第一遍尚未回填
			}
			return nil
		}).Times(2)

	// backfillOrg: Find Eng dept (ID=100) to set LeadAgentID=200
	m.depts.EXPECT().Find(gomock.Any(), int64(100)).Return(&department_entity.Department{ID: 100, Name: "Eng"}, nil)
	// backfillOrg: Find IC agent (ID=201) to set ParentAgentID=200
	m.agents.EXPECT().Find(gomock.Any(), int64(201)).Return(&agent_entity.Agent{ID: 201, Name: "IC"}, nil)

	// 第二遍:Update Eng 把 LeadAgentID=200,Update IC 把 ParentAgentID=200
	m.depts.EXPECT().Update(gomock.Any(), gomock.AssignableToTypeOf(&department_entity.Department{})).
		DoAndReturn(func(_ context.Context, d *department_entity.Department) error {
			So(d.ID, ShouldEqual, 100)
			So(d.LeadAgentID, ShouldEqual, 200)
			return nil
		})
	m.agents.EXPECT().Update(gomock.Any(), gomock.AssignableToTypeOf(&agent_entity.Agent{})).
		DoAndReturn(func(_ context.Context, a *agent_entity.Agent) error {
			So(a.ID, ShouldEqual, 201)
			So(a.ParentAgentID, ShouldEqual, 200)
			return nil
		})

	m.dbMock.ExpectBegin()
	m.dbMock.ExpectCommit()

	bundle := data_svc.BundleV1{
		Format: data_svc.BundleFormat, Version: 1,
		Scopes: []string{string(data_svc.ScopeOrganization)},
		Items: data_svc.BundleItems{
			Departments: []data_svc.BundleDepartment{
				{ExportKey: "dept-eng", Name: "Eng", ParentKey: "", LeadAgentKey: "ag-lead"},
				{ExportKey: "dept-be", Name: "Backend", ParentKey: "dept-eng"},
			},
			Agents: []data_svc.BundleAgent{
				{ExportKey: "ag-lead", Name: "Lead", DepartmentKey: "dept-eng"},
				{ExportKey: "ag-ic", Name: "IC", DepartmentKey: "dept-be", ParentAgentKey: "ag-lead"},
			},
		},
	}
	raw, _ := json.Marshal(bundle)

	Convey("org 两遍 backfill 串好 parent/lead", t, func() {
		res, err := m.svc.ApplyImport(m.ctx, &data_svc.ApplyImportRequest{
			Raw: raw, FallbackStrategy: data_svc.ActionCreate,
		})
		So(err, ShouldBeNil)
		So(res.Counts["created"], ShouldEqual, 4)
	})
}

func TestApplyImport_Org_OverwriteExistingDept(t *testing.T) {
	m := setupDataSvcTest(t)
	m.providers.EXPECT().List(gomock.Any()).Return(nil, nil).Times(2)
	m.devices.EXPECT().List(gomock.Any()).Return(nil, nil).Times(2)
	m.backends.EXPECT().List(gomock.Any()).Return(nil, nil).Times(2)
	// depts.List called once in PreviewImport; existing Eng causes conflict for bundle Eng
	m.depts.EXPECT().List(gomock.Any()).Return([]*department_entity.Department{
		{ID: 10, Name: "Eng", ParentID: 0},
	}, nil).Times(1)

	// applyDepartments: FindByName finds the existing dept (same name, same parent)
	m.depts.EXPECT().FindByName(gomock.Any(), "Eng", int64(0)).
		Return(&department_entity.Department{ID: 10, Name: "Eng", ParentID: 0}, nil)

	// overwrite calls Update
	m.depts.EXPECT().Update(gomock.Any(), gomock.AssignableToTypeOf(&department_entity.Department{})).
		DoAndReturn(func(_ context.Context, d *department_entity.Department) error {
			So(d.ID, ShouldEqual, 10)
			So(d.Description, ShouldEqual, "Engineering team")
			return nil
		})

	m.dbMock.ExpectBegin()
	m.dbMock.ExpectCommit()

	bundle := data_svc.BundleV1{
		Format: data_svc.BundleFormat, Version: 1,
		Scopes: []string{string(data_svc.ScopeOrganization)},
		Items: data_svc.BundleItems{
			Departments: []data_svc.BundleDepartment{
				// Same name "Eng" → conflict detected by PreviewImport → overwrite fallback used
				{ExportKey: "dept-eng", Name: "Eng", Description: "Engineering team", ParentKey: ""},
			},
		},
	}
	raw, _ := json.Marshal(bundle)

	Convey("overwrite 已有部门调 Update", t, func() {
		res, err := m.svc.ApplyImport(m.ctx, &data_svc.ApplyImportRequest{
			Raw: raw, FallbackStrategy: data_svc.ActionOverwrite,
		})
		So(err, ShouldBeNil)
		So(res.Counts["overwrote"], ShouldEqual, 1)
	})
}

func TestApplyImport_Org_OverwriteExistingAgent(t *testing.T) {
	m := setupDataSvcTest(t)
	m.providers.EXPECT().List(gomock.Any()).Return(nil, nil).Times(2)
	m.devices.EXPECT().List(gomock.Any()).Return(nil, nil).Times(2)
	m.backends.EXPECT().List(gomock.Any()).Return(nil, nil).Times(2)
	// No local depts or agents — no conflict detected by PreviewImport
	m.depts.EXPECT().List(gomock.Any()).Return(nil, nil).Times(1)

	// applyDepartments: new dept, create
	m.depts.EXPECT().FindByName(gomock.Any(), "Eng", int64(0)).Return(nil, nil)
	m.depts.EXPECT().Create(gomock.Any(), gomock.AssignableToTypeOf(&department_entity.Department{})).
		DoAndReturn(func(_ context.Context, d *department_entity.Department) error {
			d.ID = 10
			return nil
		})

	// applyAgents: ListByDepartment returns existing agent "Lead" for overwrite
	m.agents.EXPECT().ListByDepartment(gomock.Any(), int64(10)).
		Return([]*agent_entity.Agent{{ID: 20, Name: "Lead", DepartmentID: 10}}, nil)
	m.agents.EXPECT().Update(gomock.Any(), gomock.AssignableToTypeOf(&agent_entity.Agent{})).
		DoAndReturn(func(_ context.Context, a *agent_entity.Agent) error {
			So(a.ID, ShouldEqual, 20)
			So(a.Description, ShouldEqual, "lead agent")
			return nil
		})

	m.dbMock.ExpectBegin()
	m.dbMock.ExpectCommit()

	bundle := data_svc.BundleV1{
		Format: data_svc.BundleFormat, Version: 1,
		Scopes: []string{string(data_svc.ScopeOrganization)},
		Items: data_svc.BundleItems{
			Departments: []data_svc.BundleDepartment{
				{ExportKey: "dept-eng", Name: "Eng", ParentKey: ""},
			},
			Agents: []data_svc.BundleAgent{
				// Same name "Lead" so ListByDepartment finds it; explicit action=overwrite
				{ExportKey: "ag-lead", Name: "Lead", Description: "lead agent", DepartmentKey: "dept-eng"},
			},
		},
	}
	raw, _ := json.Marshal(bundle)

	Convey("overwrite 已有 agent 调 Update (explicit action)", t, func() {
		res, err := m.svc.ApplyImport(m.ctx, &data_svc.ApplyImportRequest{
			Raw: raw,
			// Explicitly override action for this agent
			Actions: map[string]data_svc.ItemAction{
				"organization:ag-lead": data_svc.ActionOverwrite,
			},
			FallbackStrategy: data_svc.ActionCreate,
		})
		So(err, ShouldBeNil)
		So(res.Counts["created"], ShouldEqual, 1)   // dept created
		So(res.Counts["overwrote"], ShouldEqual, 1) // agent overwrote
	})
}

func TestPreviewImport_OrgScope_AgentsAndDepts(t *testing.T) {
	m := setupDataSvcTest(t)
	m.providers.EXPECT().List(gomock.Any()).Return(nil, nil)
	m.devices.EXPECT().List(gomock.Any()).Return(nil, nil)
	m.backends.EXPECT().List(gomock.Any()).Return(nil, nil)
	m.depts.EXPECT().List(gomock.Any()).Return([]*department_entity.Department{
		{ID: 10, Name: "Eng", ParentID: 0},
	}, nil)

	bundle := data_svc.BundleV1{
		Format: data_svc.BundleFormat, Version: 1,
		Scopes: []string{string(data_svc.ScopeOrganization)},
		Items: data_svc.BundleItems{
			Departments: []data_svc.BundleDepartment{
				{ExportKey: "dept-eng", Name: "Eng", ParentKey: ""},            // conflict (same root name)
				{ExportKey: "dept-be", Name: "Backend", ParentKey: "dept-eng"}, // parent in bundle → no dangling
				{ExportKey: "dept-x", Name: "X", ParentKey: "dept-missing"},    // parent NOT in bundle → dangling
			},
			Agents: []data_svc.BundleAgent{
				{ExportKey: "ag-1", Name: "A1", DepartmentKey: "dept-eng"},     // dept in bundle
				{ExportKey: "ag-2", Name: "A2", DepartmentKey: "dept-gone"},    // dept NOT in bundle → dangling
				{ExportKey: "ag-3", Name: "A3", AgentBackendKey: "ab-missing"}, // backend NOT in bundle → dangling
				{ExportKey: "ag-4", Name: "A4", ParentAgentKey: "ag-gone"},     // parent NOT in bundle → dangling
			},
		},
	}
	raw, _ := json.Marshal(bundle)

	Convey("org preview: conflict, dangling dept parent, dangling agent refs", t, func() {
		pv, err := m.svc.PreviewImport(m.ctx, raw)
		So(err, ShouldBeNil)
		So(pv.Items, ShouldHaveLength, 7)

		// Find Eng dept item — should be conflict
		var engItem data_svc.ImportItem
		for _, it := range pv.Items {
			if it.Scope == string(data_svc.ScopeOrganization) && it.Name == "Eng" {
				engItem = it
			}
		}
		So(engItem.Conflict, ShouldBeTrue)
		So(engItem.DefaultAction, ShouldEqual, data_svc.ActionSkip)
	})
}

func TestPreviewImport_BackendLegacyNumericDeviceIDMatchesLocalDevice(t *testing.T) {
	m := setupDataSvcTest(t)
	m.providers.EXPECT().List(gomock.Any()).Return(nil, nil)
	m.devices.EXPECT().List(gomock.Any()).Return([]*paired_agentred_entity.PairedAgentred{
		{ID: 1, InstanceUUID: "uuid-1", Name: "Server1"},
	}, nil)
	m.backends.EXPECT().List(gomock.Any()).Return(nil, nil)
	m.depts.EXPECT().List(gomock.Any()).Return(nil, nil)

	bundle := data_svc.BundleV1{
		Format: data_svc.BundleFormat, Version: 1,
		Scopes: []string{string(data_svc.ScopeRemoteDevices), string(data_svc.ScopeAgentBackends)},
		Items: data_svc.BundleItems{
			RemoteDevices: []data_svc.BundleRemoteDevice{
				{InstanceUUID: "uuid-1", Name: "Server1"},
			},
			AgentBackends: []data_svc.BundleAgentBackend{
				{ExportKey: "ab-1", Type: "codex", Name: "Remote Codex", DeviceID: "1"},
			},
		},
	}
	raw, _ := json.Marshal(bundle)

	Convey("旧导出包里的数字 deviceId 能匹配本地已有远端设备", t, func() {
		pv, err := m.svc.PreviewImport(m.ctx, raw)
		So(err, ShouldBeNil)
		So(pv.Items, ShouldHaveLength, 2)
		var backend data_svc.ImportItem
		for _, it := range pv.Items {
			if it.Scope == string(data_svc.ScopeAgentBackends) {
				backend = it
			}
		}
		So(backend.Dangling, ShouldBeFalse)
		So(backend.DefaultAction, ShouldEqual, data_svc.ActionCreate)
	})
}

func TestExport_RemoteDevices(t *testing.T) {
	m := setupDataSvcTest(t)
	m.devices.EXPECT().List(gomock.Any()).Return([]*paired_agentred_entity.PairedAgentred{
		{ID: 1, InstanceUUID: "uuid-1", Name: "Server1", URL: "http://x", TLSCertPEM: "pem"},
	}, nil)

	Convey("export remote-devices scope without secrets", t, func() {
		res, err := m.svc.Export(m.ctx, &data_svc.ExportRequest{
			Scopes:         []string{string(data_svc.ScopeRemoteDevices)},
			IncludeSecrets: false,
		})
		So(err, ShouldBeNil)
		var b data_svc.BundleV1
		So(json.Unmarshal(res.JSON, &b), ShouldBeNil)
		So(b.Items.RemoteDevices, ShouldHaveLength, 1)
		So(b.Items.RemoteDevices[0].InstanceUUID, ShouldEqual, "uuid-1")
		So(b.Items.RemoteDevices[0].TLSCertPEM, ShouldEqual, "") // scrubbed
	})
}

func TestExport_AgentBackends_RemoteDeviceUsesInstanceUUID(t *testing.T) {
	m := setupDataSvcTest(t)
	m.backends.EXPECT().List(gomock.Any()).Return([]*agent_backend_entity.AgentBackend{
		{ID: 10, Type: "codex", Name: "Remote Codex", DeviceID: "7"},
	}, nil)
	m.devices.EXPECT().List(gomock.Any()).Return([]*paired_agentred_entity.PairedAgentred{
		{ID: 7, InstanceUUID: "uuid-7", Name: "Server7"},
	}, nil)

	Convey("export agent-backends 将本地 device row ID 转成可迁移的 instanceUUID", t, func() {
		res, err := m.svc.Export(m.ctx, &data_svc.ExportRequest{
			Scopes: []string{string(data_svc.ScopeAgentBackends)},
		})
		So(err, ShouldBeNil)
		var b data_svc.BundleV1
		So(json.Unmarshal(res.JSON, &b), ShouldBeNil)
		So(b.Items.AgentBackends, ShouldHaveLength, 1)
		So(b.Items.AgentBackends[0].DeviceID, ShouldEqual, "uuid-7")
	})
}

func TestApplyImport_RemoteDevice_Create(t *testing.T) {
	m := setupDataSvcTest(t)
	m.providers.EXPECT().List(gomock.Any()).Return(nil, nil).Times(2)
	m.devices.EXPECT().List(gomock.Any()).Return([]*paired_agentred_entity.PairedAgentred{}, nil).Times(2)
	m.backends.EXPECT().List(gomock.Any()).Return(nil, nil).Times(2)
	m.depts.EXPECT().List(gomock.Any()).Return(nil, nil).Times(1)

	m.devices.EXPECT().Create(gomock.Any(), gomock.AssignableToTypeOf(&paired_agentred_entity.PairedAgentred{})).
		DoAndReturn(func(_ context.Context, d *paired_agentred_entity.PairedAgentred) error {
			So(d.InstanceUUID, ShouldEqual, "uuid-1")
			So(d.Name, ShouldEqual, "Server1")
			d.ID = 50
			return nil
		})

	m.dbMock.ExpectBegin()
	m.dbMock.ExpectCommit()

	bundle := data_svc.BundleV1{
		Format: data_svc.BundleFormat, Version: 1,
		Scopes: []string{string(data_svc.ScopeRemoteDevices)},
		Items: data_svc.BundleItems{
			RemoteDevices: []data_svc.BundleRemoteDevice{
				{InstanceUUID: "uuid-1", Name: "Server1", URL: "http://x"},
			},
		},
	}
	raw, _ := json.Marshal(bundle)

	Convey("create 远端设备", t, func() {
		res, err := m.svc.ApplyImport(m.ctx, &data_svc.ApplyImportRequest{
			Raw: raw, FallbackStrategy: data_svc.ActionCreate,
		})
		So(err, ShouldBeNil)
		So(res.Counts["created"], ShouldEqual, 1)
	})
}

func TestApplyImport_Backend_Overwrite(t *testing.T) {
	m := setupDataSvcTest(t)
	m.providers.EXPECT().List(gomock.Any()).Return(nil, nil).Times(2)
	m.devices.EXPECT().List(gomock.Any()).Return(nil, nil).Times(2)
	m.backends.EXPECT().List(gomock.Any()).Return([]*agent_backend_entity.AgentBackend{
		{ID: 10, Name: "Local", Type: "claudecode"},
	}, nil).Times(2)
	m.depts.EXPECT().List(gomock.Any()).Return(nil, nil).Times(1)

	m.backends.EXPECT().Update(gomock.Any(), gomock.AssignableToTypeOf(&agent_backend_entity.AgentBackend{})).
		DoAndReturn(func(_ context.Context, bk *agent_backend_entity.AgentBackend) error {
			So(bk.ID, ShouldEqual, 10)
			So(bk.CLIPath, ShouldEqual, "/usr/local/bin/claude")
			return nil
		})

	m.dbMock.ExpectBegin()
	m.dbMock.ExpectCommit()

	bundle := data_svc.BundleV1{
		Format: data_svc.BundleFormat, Version: 1,
		Scopes: []string{string(data_svc.ScopeAgentBackends)},
		Items: data_svc.BundleItems{
			AgentBackends: []data_svc.BundleAgentBackend{
				{ExportKey: "ab-1", Name: "Local", Type: "claudecode", CLIPath: "/usr/local/bin/claude"},
			},
		},
	}
	raw, _ := json.Marshal(bundle)

	Convey("overwrite backend 调 Update", t, func() {
		res, err := m.svc.ApplyImport(m.ctx, &data_svc.ApplyImportRequest{
			Raw:              raw,
			FallbackStrategy: data_svc.ActionOverwrite,
		})
		So(err, ShouldBeNil)
		So(res.Counts["overwrote"], ShouldEqual, 1)
	})
}

func TestApplyImport_RemoteDevice_Skip(t *testing.T) {
	m := setupDataSvcTest(t)
	existing := []*paired_agentred_entity.PairedAgentred{{ID: 5, InstanceUUID: "uuid-1", Name: "S1"}}
	m.providers.EXPECT().List(gomock.Any()).Return(nil, nil).Times(2)
	m.devices.EXPECT().List(gomock.Any()).Return(existing, nil).Times(2)
	m.backends.EXPECT().List(gomock.Any()).Return(nil, nil).Times(2)
	m.depts.EXPECT().List(gomock.Any()).Return(nil, nil).Times(1)

	m.dbMock.ExpectBegin()
	m.dbMock.ExpectCommit()

	bundle := data_svc.BundleV1{
		Format: data_svc.BundleFormat, Version: 1,
		Scopes: []string{string(data_svc.ScopeRemoteDevices)},
		Items: data_svc.BundleItems{
			RemoteDevices: []data_svc.BundleRemoteDevice{
				{InstanceUUID: "uuid-1", Name: "S1"},
			},
		},
	}
	raw, _ := json.Marshal(bundle)

	Convey("skip 远端设备冲突", t, func() {
		res, err := m.svc.ApplyImport(m.ctx, &data_svc.ApplyImportRequest{
			Raw: raw, FallbackStrategy: data_svc.ActionSkip,
		})
		So(err, ShouldBeNil)
		So(res.Counts["skipped"], ShouldEqual, 1)
	})
}

func TestApplyImport_RemoteDevice_Overwrite(t *testing.T) {
	m := setupDataSvcTest(t)
	existing := []*paired_agentred_entity.PairedAgentred{{ID: 5, InstanceUUID: "uuid-1", Name: "S1", TLSMode: "none"}}
	m.providers.EXPECT().List(gomock.Any()).Return(nil, nil).Times(2)
	m.devices.EXPECT().List(gomock.Any()).Return(existing, nil).Times(2)
	m.backends.EXPECT().List(gomock.Any()).Return(nil, nil).Times(2)
	m.depts.EXPECT().List(gomock.Any()).Return(nil, nil).Times(1)

	// updateRemoteDevice calls UpdateTLS + UpdateEndpoint + Rename
	m.devices.EXPECT().UpdateTLS(gomock.Any(), int64(5), gomock.Any(), gomock.Any()).Return(nil)
	m.devices.EXPECT().UpdateEndpoint(gomock.Any(), int64(5), gomock.Any(), gomock.Any()).Return(nil)
	m.devices.EXPECT().Rename(gomock.Any(), int64(5), "S1 Renamed").Return(nil)

	m.dbMock.ExpectBegin()
	m.dbMock.ExpectCommit()

	bundle := data_svc.BundleV1{
		Format: data_svc.BundleFormat, Version: 1,
		Scopes: []string{string(data_svc.ScopeRemoteDevices)},
		Items: data_svc.BundleItems{
			RemoteDevices: []data_svc.BundleRemoteDevice{
				{InstanceUUID: "uuid-1", Name: "S1 Renamed"},
			},
		},
	}
	raw, _ := json.Marshal(bundle)

	Convey("overwrite 远端设备调 UpdateTLS+Rename", t, func() {
		res, err := m.svc.ApplyImport(m.ctx, &data_svc.ApplyImportRequest{
			Raw: raw, FallbackStrategy: data_svc.ActionOverwrite,
		})
		So(err, ShouldBeNil)
		So(res.Counts["overwrote"], ShouldEqual, 1)
	})
}

func TestApplyImport_Agent_CreateGuardedByExisting(t *testing.T) {
	m := setupDataSvcTest(t)
	m.providers.EXPECT().List(gomock.Any()).Return(nil, nil).Times(2)
	m.devices.EXPECT().List(gomock.Any()).Return(nil, nil).Times(2)
	m.backends.EXPECT().List(gomock.Any()).Return(nil, nil).Times(2)
	// No local depts — no conflict in preview
	m.depts.EXPECT().List(gomock.Any()).Return(nil, nil).Times(1)

	// dept is new
	m.depts.EXPECT().FindByName(gomock.Any(), "Eng", int64(0)).Return(nil, nil)
	m.depts.EXPECT().Create(gomock.Any(), gomock.AssignableToTypeOf(&department_entity.Department{})).
		DoAndReturn(func(_ context.Context, d *department_entity.Department) error {
			d.ID = 10
			return nil
		})

	// agent "Coder" already exists in dept 10 — apply-time conflict
	existingAgent := &agent_entity.Agent{ID: 77, Name: "Coder", DepartmentID: 10}
	m.agents.EXPECT().ListByDepartment(gomock.Any(), int64(10)).
		Return([]*agent_entity.Agent{existingAgent}, nil)
	// Create must NOT be called — no EXPECT().Create

	m.dbMock.ExpectBegin()
	m.dbMock.ExpectCommit()

	bundle := data_svc.BundleV1{
		Format: data_svc.BundleFormat, Version: 1,
		Scopes: []string{string(data_svc.ScopeOrganization)},
		Items: data_svc.BundleItems{
			Departments: []data_svc.BundleDepartment{
				{ExportKey: "dept-eng", Name: "Eng"},
			},
			Agents: []data_svc.BundleAgent{
				{ExportKey: "ag-coder", Name: "Coder", DepartmentKey: "dept-eng"},
			},
		},
	}
	raw, _ := json.Marshal(bundle)

	Convey("agent create guarded by existing local: skipped, keymap wired to existing ID", t, func() {
		res, err := m.svc.ApplyImport(m.ctx, &data_svc.ApplyImportRequest{
			Raw:              raw,
			FallbackStrategy: data_svc.ActionCreate,
		})
		So(err, ShouldBeNil)
		So(res.Counts["skipped"], ShouldEqual, 1) // agent silently skipped
		So(res.Counts["created"], ShouldEqual, 1) // dept still created
	})
}

func TestApplyImport_NestedDept_CreateGuardedByExisting(t *testing.T) {
	m := setupDataSvcTest(t)
	m.providers.EXPECT().List(gomock.Any()).Return(nil, nil).Times(2)
	m.devices.EXPECT().List(gomock.Any()).Return(nil, nil).Times(2)
	m.backends.EXPECT().List(gomock.Any()).Return(nil, nil).Times(2)
	// No local depts — no conflict in preview (child dept can't be compared without parent resolution)
	m.depts.EXPECT().List(gomock.Any()).Return(nil, nil).Times(1)

	// Root Eng dept is new
	m.depts.EXPECT().FindByName(gomock.Any(), "Eng", int64(0)).Return(nil, nil)
	m.depts.EXPECT().Create(gomock.Any(), gomock.AssignableToTypeOf(&department_entity.Department{})).
		DoAndReturn(func(_ context.Context, d *department_entity.Department) error {
			if d.Name == "Eng" {
				d.ID = 10
			}
			return nil
		})

	// Child "Backend" already exists under Eng (parentID=10) at apply-time
	existingChild := &department_entity.Department{ID: 55, Name: "Backend", ParentID: 10}
	m.depts.EXPECT().FindByName(gomock.Any(), "Backend", int64(10)).Return(existingChild, nil)
	// Create for Backend must NOT be called

	m.dbMock.ExpectBegin()
	m.dbMock.ExpectCommit()

	bundle := data_svc.BundleV1{
		Format: data_svc.BundleFormat, Version: 1,
		Scopes: []string{string(data_svc.ScopeOrganization)},
		Items: data_svc.BundleItems{
			Departments: []data_svc.BundleDepartment{
				{ExportKey: "dept-eng", Name: "Eng", ParentKey: ""},
				{ExportKey: "dept-be", Name: "Backend", ParentKey: "dept-eng"},
			},
		},
	}
	raw, _ := json.Marshal(bundle)

	Convey("nested dept create guarded by existing: skipped, keymap wired to existing ID", t, func() {
		res, err := m.svc.ApplyImport(m.ctx, &data_svc.ApplyImportRequest{
			Raw:              raw,
			FallbackStrategy: data_svc.ActionCreate,
		})
		So(err, ShouldBeNil)
		So(res.Counts["skipped"], ShouldEqual, 1) // Backend silently skipped
		So(res.Counts["created"], ShouldEqual, 1) // Eng created
	})
}

func TestApplyImport_RemoteDevice_Overwrite_UpdatesURL(t *testing.T) {
	m := setupDataSvcTest(t)
	existing := []*paired_agentred_entity.PairedAgentred{{
		ID:                5,
		InstanceUUID:      "uuid-1",
		Name:              "OldName",
		URL:               "ws://old-host:9000",
		DaemonFingerprint: "old-fp",
		TLSMode:           "default",
	}}
	m.providers.EXPECT().List(gomock.Any()).Return(nil, nil).Times(2)
	m.devices.EXPECT().List(gomock.Any()).Return(existing, nil).Times(2)
	m.backends.EXPECT().List(gomock.Any()).Return(nil, nil).Times(2)
	m.depts.EXPECT().List(gomock.Any()).Return(nil, nil).Times(1)

	m.devices.EXPECT().UpdateTLS(gomock.Any(), int64(5), gomock.Any(), gomock.Any()).Return(nil)
	// Key assertion: UpdateEndpoint must be called with new URL + fingerprint
	m.devices.EXPECT().UpdateEndpoint(gomock.Any(), int64(5), "ws://new-host:9000", "new-fp").Return(nil)
	m.devices.EXPECT().Rename(gomock.Any(), int64(5), "NewName").Return(nil)

	m.dbMock.ExpectBegin()
	m.dbMock.ExpectCommit()

	bundle := data_svc.BundleV1{
		Format: data_svc.BundleFormat, Version: 1,
		Scopes: []string{string(data_svc.ScopeRemoteDevices)},
		Items: data_svc.BundleItems{
			RemoteDevices: []data_svc.BundleRemoteDevice{
				{
					InstanceUUID:      "uuid-1",
					Name:              "NewName",
					URL:               "ws://new-host:9000",
					DaemonFingerprint: "new-fp",
					TLSMode:           "default",
				},
			},
		},
	}
	raw, _ := json.Marshal(bundle)

	Convey("overwrite 远端设备时 URL 和 DaemonFingerprint 被持久化", t, func() {
		res, err := m.svc.ApplyImport(m.ctx, &data_svc.ApplyImportRequest{
			Raw: raw, FallbackStrategy: data_svc.ActionOverwrite,
		})
		So(err, ShouldBeNil)
		So(res.Counts["overwrote"], ShouldEqual, 1)
	})
}

func TestApplyImport_Backend_Skip(t *testing.T) {
	m := setupDataSvcTest(t)
	existing := []*agent_backend_entity.AgentBackend{{ID: 10, Name: "Local"}}
	m.providers.EXPECT().List(gomock.Any()).Return(nil, nil).Times(2)
	m.devices.EXPECT().List(gomock.Any()).Return(nil, nil).Times(2)
	m.backends.EXPECT().List(gomock.Any()).Return(existing, nil).Times(2)
	m.depts.EXPECT().List(gomock.Any()).Return(nil, nil).Times(1)

	m.dbMock.ExpectBegin()
	m.dbMock.ExpectCommit()

	bundle := data_svc.BundleV1{
		Format: data_svc.BundleFormat, Version: 1,
		Scopes: []string{string(data_svc.ScopeAgentBackends)},
		Items: data_svc.BundleItems{
			AgentBackends: []data_svc.BundleAgentBackend{
				{ExportKey: "ab-1", Name: "Local"},
			},
		},
	}
	raw, _ := json.Marshal(bundle)

	Convey("skip backend 冲突", t, func() {
		res, err := m.svc.ApplyImport(m.ctx, &data_svc.ApplyImportRequest{
			Raw: raw, FallbackStrategy: data_svc.ActionSkip,
		})
		So(err, ShouldBeNil)
		So(res.Counts["skipped"], ShouldEqual, 1)
	})
}

func TestApplyImport_Org_SkipAgent(t *testing.T) {
	m := setupDataSvcTest(t)
	m.providers.EXPECT().List(gomock.Any()).Return(nil, nil).Times(2)
	m.devices.EXPECT().List(gomock.Any()).Return(nil, nil).Times(2)
	m.backends.EXPECT().List(gomock.Any()).Return(nil, nil).Times(2)
	m.depts.EXPECT().List(gomock.Any()).Return(nil, nil).Times(1)

	// dept create
	m.depts.EXPECT().FindByName(gomock.Any(), "Eng", int64(0)).Return(nil, nil)
	m.depts.EXPECT().Create(gomock.Any(), gomock.AssignableToTypeOf(&department_entity.Department{})).
		DoAndReturn(func(_ context.Context, d *department_entity.Department) error { d.ID = 10; return nil })

	// agent skip — ListByDepartment returns existing match
	m.agents.EXPECT().ListByDepartment(gomock.Any(), int64(10)).
		Return([]*agent_entity.Agent{{ID: 20, Name: "Lead", DepartmentID: 10}}, nil)

	m.dbMock.ExpectBegin()
	m.dbMock.ExpectCommit()

	bundle := data_svc.BundleV1{
		Format: data_svc.BundleFormat, Version: 1,
		Scopes: []string{string(data_svc.ScopeOrganization)},
		Items: data_svc.BundleItems{
			Departments: []data_svc.BundleDepartment{
				{ExportKey: "dept-eng", Name: "Eng"},
			},
			Agents: []data_svc.BundleAgent{
				{ExportKey: "ag-lead", Name: "Lead", DepartmentKey: "dept-eng"},
			},
		},
	}
	raw, _ := json.Marshal(bundle)

	Convey("skip agent via explicit action", t, func() {
		res, err := m.svc.ApplyImport(m.ctx, &data_svc.ApplyImportRequest{
			Raw: raw,
			Actions: map[string]data_svc.ItemAction{
				"organization:ag-lead": data_svc.ActionSkip,
			},
			FallbackStrategy: data_svc.ActionCreate,
		})
		So(err, ShouldBeNil)
		So(res.Counts["skipped"], ShouldEqual, 1)
		So(res.Counts["created"], ShouldEqual, 1)
	})
}

func TestApplyImport_Provider_Duplicate(t *testing.T) {
	m := setupDataSvcTest(t)
	existing := []*llm_provider_entity.LLMProvider{{ID: 5, ProviderKey: "k1", Name: "P1"}}
	m.providers.EXPECT().List(gomock.Any()).Return(existing, nil).Times(2)
	m.devices.EXPECT().List(gomock.Any()).Return(nil, nil).Times(2)
	m.backends.EXPECT().List(gomock.Any()).Return(nil, nil).Times(2)
	m.depts.EXPECT().List(gomock.Any()).Return(nil, nil).Times(1)

	// Duplicate creates a new row with renamed name and new UUID key
	m.providers.EXPECT().Create(gomock.Any(), gomock.AssignableToTypeOf(&llm_provider_entity.LLMProvider{})).
		DoAndReturn(func(_ context.Context, p *llm_provider_entity.LLMProvider) error {
			// uniqueName appends " (copy)" since "P1" is taken
			So(p.Name, ShouldEqual, "P1 (copy)")
			p.ID = 99
			return nil
		})

	m.dbMock.ExpectBegin()
	m.dbMock.ExpectCommit()

	bundle := data_svc.BundleV1{
		Format: data_svc.BundleFormat, Version: 1,
		Scopes: []string{string(data_svc.ScopeLLMProviders)},
		Items: data_svc.BundleItems{LLMProviders: []data_svc.BundleLLMProvider{
			{ProviderKey: "k1", Name: "P1", Type: "anthropic"},
		}},
	}
	raw, _ := json.Marshal(bundle)

	Convey("duplicate 走 uniqueName 加 (copy) 后缀", t, func() {
		res, err := m.svc.ApplyImport(m.ctx, &data_svc.ApplyImportRequest{
			Raw: raw, FallbackStrategy: data_svc.ActionDuplicate,
		})
		So(err, ShouldBeNil)
		So(res.Counts["duplicated"], ShouldEqual, 1)
	})
}

func TestExport_BackendKey_SharedBetweenScopes(t *testing.T) {
	m := setupDataSvcTest(t)

	// AgentBackend().List must be called exactly once — the fix merges the two
	// independent calls into a single shared backendKey map.
	m.backends.EXPECT().List(gomock.Any()).Times(1).Return([]*agent_backend_entity.AgentBackend{
		{ID: 30, Type: "claudecode", Name: "Local"},
	}, nil)
	m.depts.EXPECT().List(gomock.Any()).Return([]*department_entity.Department{
		{ID: 10, Name: "Eng"},
	}, nil)
	m.agents.EXPECT().List(gomock.Any()).Return([]*agent_entity.Agent{
		{ID: 20, Name: "Coder", DepartmentID: 10, AgentBackendID: 30},
	}, nil)

	Convey("同时请求 ScopeAgentBackends + ScopeOrganization 时 backend exportKey 保持一致", t, func() {
		res, err := m.svc.Export(m.ctx, &data_svc.ExportRequest{
			Scopes: []string{
				string(data_svc.ScopeAgentBackends),
				string(data_svc.ScopeOrganization),
			},
		})
		So(err, ShouldBeNil)

		var b data_svc.BundleV1
		So(json.Unmarshal(res.JSON, &b), ShouldBeNil)

		So(b.Items.AgentBackends, ShouldHaveLength, 1)
		So(b.Items.Agents, ShouldHaveLength, 1)

		// The agent's agentBackendKey must equal the backend's exportKey.
		backendExportKey := b.Items.AgentBackends[0].ExportKey
		agentBackendKey := b.Items.Agents[0].AgentBackendKey
		So(backendExportKey, ShouldNotBeEmpty)
		So(agentBackendKey, ShouldEqual, backendExportKey) // line 220: key round-trip assertion
	})
}
