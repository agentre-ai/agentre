package claudeskill

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/agentre-ai/agentre/internal/pkg/agentskill"
	. "github.com/smartystreets/goconvey/convey"
)

// mustSkill 在 skillsDir 下造一个合法 skill 目录(含 SKILL.md)。
func mustSkill(t *testing.T, skillsDir, name string) {
	t.Helper()
	dir := filepath.Join(skillsDir, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: "+name+"\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestParsePluginList(t *testing.T) {
	Convey("解析 plugin list --json", t, func() {
		raw := []byte(`[
		  {"id":"superpowers@claude-plugins-official","enabled":true,"scope":"user"},
		  {"id":"opsctl@opskat","enabled":false,"scope":"user"}
		]`)
		packs, err := parsePluginList(raw)
		So(err, ShouldBeNil)
		So(len(packs), ShouldEqual, 2)
		So(packs[0].ID, ShouldEqual, "superpowers@claude-plugins-official")
		So(packs[0].Name, ShouldEqual, "superpowers") // id 取 @ 前段
		So(packs[0].Installed, ShouldBeTrue)
		So(packs[0].Source, ShouldEqual, agentskill.SourceInstalled)
		So(packs[0].GloballyEnabled, ShouldBeTrue)  // superpowers enabled:true
		So(packs[1].GloballyEnabled, ShouldBeFalse) // opsctl enabled:false
		Convey("空/坏 JSON → 空,不 panic", func() {
			p, _ := parsePluginList([]byte(""))
			So(p, ShouldResemble, []agentskill.SkillPack{})
			p2, _ := parsePluginList([]byte("not json"))
			So(p2, ShouldResemble, []agentskill.SkillPack{})
		})
		Convey("无 @ 的裸 id → Name 即 id", func() {
			p, _ := parsePluginList([]byte(`[{"id":"barepack","enabled":true,"scope":"user"}]`))
			So(len(p), ShouldEqual, 1)
			So(p[0].ID, ShouldEqual, "barepack")
			So(p[0].Name, ShouldEqual, "barepack")
		})
		Convey("installPath 命中 → Skills 填上包内 skill 名", func() {
			root := t.TempDir()
			skills := filepath.Join(root, "skills")
			mustSkill(t, skills, "alpha")
			mustSkill(t, skills, "beta")
			raw := []byte(`[{"id":"sp@x","enabled":true,"scope":"user","installPath":"` + root + `"}]`)
			p, _ := parsePluginList(raw)
			So(len(p), ShouldEqual, 1)
			So(p[0].Skills, ShouldResemble, []string{"alpha", "beta"})
		})
		Convey("无 installPath → Skills 为空(不 panic)", func() {
			So(packs[0].Skills, ShouldBeEmpty)
		})
	})
}

func TestDiscover(t *testing.T) {
	Convey("Discover 经可注入 runner 取 plugin list 并解析", t, func() {
		var gotName string
		var gotArgs []string
		d := Discoverer{run: func(_ context.Context, name string, args ...string) ([]byte, error) {
			gotName, gotArgs = name, args
			return []byte(`[{"id":"superpowers@official","enabled":true,"scope":"user"}]`), nil
		}}
		packs, err := d.Discover(context.Background(), agentskill.DiscoverQuery{})
		So(err, ShouldBeNil)
		So(gotName, ShouldEqual, "claude") // 空 CLIPath → 默认 binary
		So(gotArgs, ShouldResemble, []string{"plugin", "list", "--json"})
		So(len(packs), ShouldEqual, 1)
		So(packs[0].ID, ShouldEqual, "superpowers@official")
		So(packs[0].GloballyEnabled, ShouldBeTrue)

		Convey("CLIPath 非空(含前后空白)→ trim 后用指定 binary 定位安装", func() {
			d := Discoverer{run: func(_ context.Context, name string, _ ...string) ([]byte, error) {
				gotName = name
				return []byte("[]"), nil
			}}
			_, err := d.Discover(context.Background(), agentskill.DiscoverQuery{CLIPath: "  /opt/claude  "})
			So(err, ShouldBeNil)
			So(gotName, ShouldEqual, "/opt/claude")
		})

		Convey("CLI 报错 → 软降级空发现,不向上报错", func() {
			d := Discoverer{run: func(context.Context, string, ...string) ([]byte, error) {
				return nil, errors.New("claude: command not found")
			}}
			packs, err := d.Discover(context.Background(), agentskill.DiscoverQuery{})
			So(err, ShouldBeNil)
			So(packs, ShouldResemble, []agentskill.SkillPack{})
		})

		Convey("默认 runner(run=nil)走真实 exec;缺失 binary 时软降级、不 panic", func() {
			d := Discoverer{}
			packs, err := d.Discover(context.Background(), agentskill.DiscoverQuery{CLIPath: "agentre-no-such-binary-xyz"})
			So(err, ShouldBeNil)
			So(packs, ShouldResemble, []agentskill.SkillPack{})
		})
	})
}

func TestScanSkills(t *testing.T) {
	Convey("扫描 plugin installPath 下的 skills/*/SKILL.md", t, func() {
		root := t.TempDir()
		skills := filepath.Join(root, "skills")
		mustSkill(t, skills, "brainstorming")
		mustSkill(t, skills, "tdd")
		// 没有 SKILL.md 的目录不算 skill
		So(os.MkdirAll(filepath.Join(skills, "not-a-skill"), 0o755), ShouldBeNil)
		// skills 下的散落文件不算
		So(os.WriteFile(filepath.Join(skills, "README.md"), []byte("x"), 0o644), ShouldBeNil)

		So(scanSkills(root), ShouldResemble, []string{"brainstorming", "tdd"})

		Convey("installPath 为空 → nil", func() {
			So(scanSkills(""), ShouldBeNil)
		})
		Convey("没有 skills 目录(纯命令插件)→ nil", func() {
			cmdOnly := t.TempDir()
			So(os.MkdirAll(filepath.Join(cmdOnly, "commands"), 0o755), ShouldBeNil)
			So(scanSkills(cmdOnly), ShouldBeNil)
		})
	})
}
