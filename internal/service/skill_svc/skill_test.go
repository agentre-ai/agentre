package skill_svc

import (
	"context"
	"testing"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agentskill"
	"github.com/agentre-ai/agentre/internal/service/skill_svc/mock_skill_svc"
	. "github.com/smartystreets/goconvey/convey"
	"go.uber.org/mock/gomock"
)

type fakeDisc struct{ packs []agentskill.SkillPack }

func (f fakeDisc) Discover(_ context.Context, _ agentskill.DiscoverQuery) ([]agentskill.SkillPack, error) {
	return f.packs, nil
}

func newForTest(a AgentLookup, b BackendLookup) *Service { return &Service{agent: a, backend: b} }

func newForTestRemote(a AgentLookup, b BackendLookup, r RemoteDiscoverer) *Service {
	return &Service{agent: a, backend: b, remote: r}
}

// fakeRemoteDisc 替身远端发现器:记录入参,回预置 daemon 包。
type fakeRemoteDisc struct {
	gotDeviceID int64
	gotBackend  string
	packs       []agentskill.SkillPack
}

func (f *fakeRemoteDisc) ListSkills(_ context.Context, deviceID int64, backendType string) ([]agentskill.SkillPack, error) {
	f.gotDeviceID = deviceID
	f.gotBackend = backendType
	return f.packs, nil
}

func TestListAgentSkillPacks_RemoteBackendUsesDaemonDiscovery(t *testing.T) {
	Convey("远端 backend(DeviceID 非空)走 daemon 发现,不混入 desktop 本地发现", t, func() {
		ctrl := gomock.NewController(t)
		al := mock_skill_svc.NewMockAgentLookup(ctrl)
		bl := mock_skill_svc.NewMockBackendLookup(ctrl)
		ag := &agent_entity.Agent{ID: 1, AgentBackendID: 9}
		al.EXPECT().Find(gomock.Any(), int64(1)).Return(ag, nil).AnyTimes()
		// 远端 backend:Type=claudecode + DeviceID=7。
		bl.EXPECT().Find(gomock.Any(), int64(9)).Return(&agent_backend_entity.AgentBackend{
			Type: string(agent_backend_entity.TypeClaudeCode), DeviceID: "7",
		}, nil).AnyTimes()
		// 本地发现器回一个独有包:若路由错跑了本地,它会冒出来。
		restore := agentskill.SwapDiscovererForTest(agent_backend_entity.TypeClaudeCode, fakeDisc{[]agentskill.SkillPack{
			{ID: "local-only@desktop", Name: "local-only", Installed: true, Source: agentskill.SourceInstalled},
		}})
		defer restore()
		remote := &fakeRemoteDisc{packs: []agentskill.SkillPack{
			{ID: "superpowers@claude-plugins-official", Name: "superpowers", Installed: true, Source: agentskill.SourceInstalled, GloballyEnabled: true},
		}}
		s := newForTestRemote(al, bl, remote)

		cat, err := s.ListAgentSkillPacks(context.Background(), 1, false)
		So(err, ShouldBeNil)
		// 用解析出的 deviceID + backend type 调远端发现。
		So(remote.gotDeviceID, ShouldEqual, int64(7))
		So(remote.gotBackend, ShouldEqual, "claudecode")
		byID := map[string]SkillPackDTO{}
		for _, p := range cat.Packs {
			byID[p.ID] = p
		}
		// daemon 包进目录;desktop 本地发现不应混入。
		So(byID["superpowers@claude-plugins-official"].Installed, ShouldBeTrue)
		So(byID["superpowers@claude-plugins-official"].GloballyEnabled, ShouldBeTrue)
		_, hasLocal := byID["local-only@desktop"]
		So(hasLocal, ShouldBeFalse)
	})
}

func TestListAgentSkillPacks(t *testing.T) {
	Convey("合并推荐 + 发现 + 授权标注", t, func() {
		ctrl := gomock.NewController(t)
		al := mock_skill_svc.NewMockAgentLookup(ctrl)
		bl := mock_skill_svc.NewMockBackendLookup(ctrl)
		ag := &agent_entity.Agent{ID: 1, AgentBackendID: 9}
		ag.SetSkills([]agent_entity.AgentSkillItem{{ID: "superpowers@claude-plugins-official", Enabled: true}})
		al.EXPECT().Find(gomock.Any(), int64(1)).Return(ag, nil).AnyTimes()
		bl.EXPECT().Find(gomock.Any(), int64(9)).Return(&agent_backend_entity.AgentBackend{Type: string(agent_backend_entity.TypeClaudeCode)}, nil).AnyTimes()
		restore := agentskill.SwapDiscovererForTest(agent_backend_entity.TypeClaudeCode, fakeDisc{[]agentskill.SkillPack{
			{ID: "superpowers@claude-plugins-official", Name: "superpowers", Installed: true, Source: agentskill.SourceInstalled, GloballyEnabled: true},
			{ID: "opsctl@opskat", Name: "opsctl", Installed: true, Source: agentskill.SourceInstalled},
		}})
		defer restore()
		s := newForTest(al, bl)

		Convey("ListAgentSkillPacks", func() {
			cat, err := s.ListAgentSkillPacks(context.Background(), 1, false)
			So(err, ShouldBeNil)
			byID := map[string]SkillPackDTO{}
			for _, p := range cat.Packs {
				byID[p.ID] = p
			}
			So(byID["superpowers@claude-plugins-official"].Enabled, ShouldBeTrue)
			So(byID["superpowers@claude-plugins-official"].Installed, ShouldBeTrue)
			So(byID["superpowers@claude-plugins-official"].Recommended, ShouldBeTrue) // 推荐∩安装
			So(byID["opsctl@opskat"].Enabled, ShouldBeFalse)
			So(byID["code-review@claude-plugins-official"].Recommended, ShouldBeTrue)
			So(byID["code-review@claude-plugins-official"].Installed, ShouldBeFalse)
			So(byID["code-review@claude-plugins-official"].Enabled, ShouldBeFalse)
			So(byID["superpowers@claude-plugins-official"].GloballyEnabled, ShouldBeTrue)
			So(byID["opsctl@opskat"].GloballyEnabled, ShouldBeFalse)
		})
		Convey("EnabledPluginsMap 只发 agent 显式覆盖(true/false),其余继承", func() {
			ag.SetSkills([]agent_entity.AgentSkillItem{
				{ID: "superpowers@claude-plugins-official", Enabled: true},      // 强制开
				{ID: "frontend-design@claude-plugins-official", Enabled: false}, // 强制关(全局开的也能关)
			})
			m, err := s.EnabledPluginsMap(context.Background(), 1)
			So(err, ShouldBeNil)
			So(m["superpowers@claude-plugins-official"], ShouldBeTrue)
			val, hasFD := m["frontend-design@claude-plugins-official"]
			So(hasFD, ShouldBeTrue)
			So(val, ShouldBeFalse)
			_, hasOpsctl := m["opsctl@opskat"] // 已装、未覆盖 → 不在 map(继承全局)
			So(hasOpsctl, ShouldBeFalse)
		})
	})

	Convey("Codex 目录只合并 Codex 发现项,不混入 Claude Code 推荐包", t, func() {
		ctrl := gomock.NewController(t)
		al := mock_skill_svc.NewMockAgentLookup(ctrl)
		bl := mock_skill_svc.NewMockBackendLookup(ctrl)
		ag := &agent_entity.Agent{ID: 2, AgentBackendID: 10}
		al.EXPECT().Find(gomock.Any(), int64(2)).Return(ag, nil).AnyTimes()
		bl.EXPECT().Find(gomock.Any(), int64(10)).Return(&agent_backend_entity.AgentBackend{Type: string(agent_backend_entity.TypeCodex)}, nil).AnyTimes()
		restore := agentskill.SwapDiscovererForTest(agent_backend_entity.TypeCodex, fakeDisc{[]agentskill.SkillPack{
			{ID: "browser@openai-bundled", Name: "browser", Installed: true, Source: agentskill.SourceInstalled, GloballyEnabled: true},
		}})
		defer restore()
		s := newForTest(al, bl)

		cat, err := s.ListAgentSkillPacks(context.Background(), 2, false)
		So(err, ShouldBeNil)
		byID := map[string]SkillPackDTO{}
		for _, p := range cat.Packs {
			byID[p.ID] = p
		}
		So(byID["browser@openai-bundled"].Installed, ShouldBeTrue)
		_, hasClaudeSuperpowers := byID["superpowers@claude-plugins-official"]
		So(hasClaudeSuperpowers, ShouldBeFalse)
		_, hasClaudeCodeReview := byID["code-review@claude-plugins-official"]
		So(hasClaudeCodeReview, ShouldBeFalse)
	})
}
