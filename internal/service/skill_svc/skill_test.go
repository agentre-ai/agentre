package skill_svc

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agentskill"
	"github.com/agentre-ai/agentre/internal/service/skill_svc/mock_skill_svc"
)

type fakeDisc struct{ packs []agentskill.SkillPack }

func (f fakeDisc) Discover(_ context.Context, _ agentskill.DiscoverQuery) ([]agentskill.SkillPack, error) {
	return f.packs, nil
}

type fakeCommandDisc struct{ commands []agentskill.SkillCommand }

func (f fakeCommandDisc) DiscoverCommands(_ context.Context, _ agentskill.CommandDiscoverQuery) ([]agentskill.SkillCommand, error) {
	return f.commands, nil
}

// fakeExecTargets 替身执行目标查找器：技能授权挂在执行目标行上(R15e),测试直接
// 给一份预置的有序行,可在子 Convey 里重新赋值来模拟"改了授权"。
type fakeExecTargets struct {
	rows []*agent_entity.AgentExecTarget
}

func (f *fakeExecTargets) ListByAgent(_ context.Context, _ int64) ([]*agent_entity.AgentExecTarget, error) {
	return f.rows, nil
}

// skillTarget 造一个只带技能授权的执行目标行,方便测试装配。
func skillTarget(items ...agent_entity.AgentSkillItem) *agent_entity.AgentExecTarget {
	t := &agent_entity.AgentExecTarget{}
	t.SetSkills(items)
	return t
}

func newForTest(a AgentLookup, b BackendLookup, et ExecTargetLookup) *Service {
	return &Service{agent: a, backend: b, execTarget: et}
}

func newForTestRemote(a AgentLookup, b BackendLookup, et ExecTargetLookup, r RemoteDiscoverer) *Service {
	return &Service{agent: a, backend: b, execTarget: et, remote: r}
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
		et := &fakeExecTargets{}
		s := newForTestRemote(al, bl, et, remote)

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

// TestListAgentSkillPacks_ReadsAuthorizationFromExecTargetNotAgentRow 锁住 R15e
// 的存放位置:agents.skills_json 即便还留着(遗留列,保留但不再被读取),也不能再
// 影响目录的授权标注 —— 真源是这一档执行目标行自己的 SkillsJSON。
func TestListAgentSkillPacks_ReadsAuthorizationFromExecTargetNotAgentRow(t *testing.T) {
	Convey("Agent 行上的 legacy skills_json 与执行目标行的授权不一致时,以执行目标行为准", t, func() {
		ctrl := gomock.NewController(t)
		al := mock_skill_svc.NewMockAgentLookup(ctrl)
		bl := mock_skill_svc.NewMockBackendLookup(ctrl)
		// Agent 行上的遗留列写着"启用 opsctl",但真正的授权(执行目标行)写着"启用
		// superpowers"——两者互相矛盾,用来证明读路径只看后者。
		ag := &agent_entity.Agent{ID: 5, AgentBackendID: 9, SkillsJSON: `[{"id":"opsctl@opskat","enabled":true}]`}
		al.EXPECT().Find(gomock.Any(), int64(5)).Return(ag, nil).AnyTimes()
		bl.EXPECT().Find(gomock.Any(), int64(9)).Return(&agent_backend_entity.AgentBackend{Type: string(agent_backend_entity.TypeClaudeCode)}, nil).AnyTimes()
		restore := agentskill.SwapDiscovererForTest(agent_backend_entity.TypeClaudeCode, fakeDisc{[]agentskill.SkillPack{
			{ID: "superpowers@claude-plugins-official", Name: "superpowers", Installed: true, Source: agentskill.SourceInstalled},
			{ID: "opsctl@opskat", Name: "opsctl", Installed: true, Source: agentskill.SourceInstalled},
		}})
		defer restore()
		et := &fakeExecTargets{rows: []*agent_entity.AgentExecTarget{
			skillTarget(agent_entity.AgentSkillItem{ID: "superpowers@claude-plugins-official", Enabled: true}),
		}}
		s := newForTest(al, bl, et)

		cat, err := s.ListAgentSkillPacks(context.Background(), 5, false)
		So(err, ShouldBeNil)
		byID := map[string]SkillPackDTO{}
		for _, p := range cat.Packs {
			byID[p.ID] = p
		}
		So(byID["superpowers@claude-plugins-official"].Enabled, ShouldBeTrue)
		So(byID["opsctl@opskat"].Enabled, ShouldBeFalse) // 遗留列说 true,真源(执行目标行)没提它 → 不生效
	})
}

// TestEnabledPluginsMap_DoesNotUnionAcrossTargets 锁住 R15e 的"不做并集":Agent 有
// 两档、两份互不相干的技能授权时,EnabledPluginsMap(agentID) 只看最靠前那一档,
// 不把两档的授权合并展示 —— caps 按每一档判定,不按 Agent 判定。
func TestEnabledPluginsMap_DoesNotUnionAcrossTargets(t *testing.T) {
	Convey("Agent 挂两档,第二档独有的技能不出现在结果里", t, func() {
		ctrl := gomock.NewController(t)
		al := mock_skill_svc.NewMockAgentLookup(ctrl)
		ag := &agent_entity.Agent{ID: 6, AgentBackendID: 9}
		al.EXPECT().Find(gomock.Any(), int64(6)).Return(ag, nil).AnyTimes()
		et := &fakeExecTargets{rows: []*agent_entity.AgentExecTarget{
			skillTarget(agent_entity.AgentSkillItem{ID: "first-only@x", Enabled: true}),
			skillTarget(agent_entity.AgentSkillItem{ID: "second-only@x", Enabled: true}),
		}}
		s := newForTest(al, nil, et)

		m, err := s.EnabledPluginsMap(context.Background(), 6)
		So(err, ShouldBeNil)
		_, hasFirst := m["first-only@x"]
		So(hasFirst, ShouldBeTrue)
		_, hasSecond := m["second-only@x"]
		So(hasSecond, ShouldBeFalse)
	})
}

func TestListAgentSkillPacks(t *testing.T) {
	Convey("合并推荐 + 发现 + 授权标注", t, func() {
		ctrl := gomock.NewController(t)
		al := mock_skill_svc.NewMockAgentLookup(ctrl)
		bl := mock_skill_svc.NewMockBackendLookup(ctrl)
		ag := &agent_entity.Agent{ID: 1, AgentBackendID: 9}
		al.EXPECT().Find(gomock.Any(), int64(1)).Return(ag, nil).AnyTimes()
		bl.EXPECT().Find(gomock.Any(), int64(9)).Return(&agent_backend_entity.AgentBackend{Type: string(agent_backend_entity.TypeClaudeCode)}, nil).AnyTimes()
		restore := agentskill.SwapDiscovererForTest(agent_backend_entity.TypeClaudeCode, fakeDisc{[]agentskill.SkillPack{
			{ID: "superpowers@claude-plugins-official", Name: "superpowers", Installed: true, Source: agentskill.SourceInstalled, GloballyEnabled: true},
			{ID: "opsctl@opskat", Name: "opsctl", Installed: true, Source: agentskill.SourceInstalled},
		}})
		defer restore()
		// 授权挂在执行目标行上(R15e),不是 Agent 行:et 是这条 Agent 唯一那一档的
		// 技能授权。
		et := &fakeExecTargets{rows: []*agent_entity.AgentExecTarget{
			skillTarget(agent_entity.AgentSkillItem{ID: "superpowers@claude-plugins-official", Enabled: true}),
		}}
		s := newForTest(al, bl, et)

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
		Convey("Given installed packs with inherited and explicit states, When listing the catalog, Then EffectiveEnabled reports the launch-time truth", func() {
			et.rows = []*agent_entity.AgentExecTarget{skillTarget(
				agent_entity.AgentSkillItem{ID: "superpowers@claude-plugins-official", Enabled: false}, // 显式关覆盖全局开
				agent_entity.AgentSkillItem{ID: "opsctl@opskat", Enabled: true},                        // 显式开覆盖全局关
			)}

			cat, err := s.ListAgentSkillPacks(context.Background(), 1, false)
			So(err, ShouldBeNil)
			byID := map[string]SkillPackDTO{}
			for _, p := range cat.Packs {
				byID[p.ID] = p
			}

			So(byID["superpowers@claude-plugins-official"].EffectiveEnabled, ShouldBeFalse)
			So(byID["opsctl@opskat"].EffectiveEnabled, ShouldBeTrue)
			So(byID["code-review@claude-plugins-official"].EffectiveEnabled, ShouldBeFalse)
		})
		Convey("Given an installed globally-enabled pack without an agent override, When listing the catalog, Then it is effectively enabled by inheritance", func() {
			et.rows = nil

			cat, err := s.ListAgentSkillPacks(context.Background(), 1, false)
			So(err, ShouldBeNil)
			byID := map[string]SkillPackDTO{}
			for _, p := range cat.Packs {
				byID[p.ID] = p
			}

			So(byID["superpowers@claude-plugins-official"].EffectiveEnabled, ShouldBeTrue)
			So(byID["opsctl@opskat"].EffectiveEnabled, ShouldBeFalse)
		})
		Convey("EnabledPluginsMap 只发 agent 显式覆盖(true/false),其余继承", func() {
			et.rows = []*agent_entity.AgentExecTarget{skillTarget(
				agent_entity.AgentSkillItem{ID: "superpowers@claude-plugins-official", Enabled: true},      // 强制开
				agent_entity.AgentSkillItem{ID: "frontend-design@claude-plugins-official", Enabled: false}, // 强制关(全局开的也能关)
			)}
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
		et := &fakeExecTargets{}
		s := newForTest(al, bl, et)

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

func TestListAgentSkillCommands(t *testing.T) {
	Convey("Given an agent with inherited plugin skills plus backend-native standalone skills", t, func() {
		ctrl := gomock.NewController(t)
		al := mock_skill_svc.NewMockAgentLookup(ctrl)
		bl := mock_skill_svc.NewMockBackendLookup(ctrl)
		ag := &agent_entity.Agent{ID: 3, AgentBackendID: 12}
		al.EXPECT().Find(gomock.Any(), int64(3)).Return(ag, nil).AnyTimes()
		bl.EXPECT().Find(gomock.Any(), int64(12)).Return(&agent_backend_entity.AgentBackend{Type: string(agent_backend_entity.TypeCodex)}, nil).AnyTimes()
		restorePacks := agentskill.SwapDiscovererForTest(agent_backend_entity.TypeCodex, fakeDisc{[]agentskill.SkillPack{
			{ID: "browser@openai-bundled", Name: "browser", Skills: []string{"browser"}, Installed: true, Source: agentskill.SourceInstalled, GloballyEnabled: true},
			{ID: "superpowers@openai-curated", Name: "superpowers", Skills: []string{"tdd"}, Installed: true, Source: agentskill.SourceInstalled, GloballyEnabled: false},
		}})
		defer restorePacks()
		restoreCommands := agentskill.SwapCommandDiscovererForTest(agent_backend_entity.TypeCodex, fakeCommandDisc{[]agentskill.SkillCommand{
			{Name: "browser:browser"}, // duplicate of enabled plugin-derived command
			{Name: "shadcn", Description: "Compose shadcn UI"},
		}})
		defer restoreCommands()
		et := &fakeExecTargets{}
		s := newForTest(al, bl, et)

		Convey("When commands are listed, Then enabled plugin and standalone names merge once while disabled packs stay hidden", func() {
			catalog, err := s.ListAgentSkillCommands(context.Background(), 3, "/tmp/project")
			So(err, ShouldBeNil)
			So(catalog.Commands, ShouldResemble, []SkillCommandDTO{
				{Name: "browser:browser"},
				{Name: "shadcn", Description: "Compose shadcn UI"},
			})
		})
	})
}
