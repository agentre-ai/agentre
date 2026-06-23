package group_svc

import (
	"context"
	"strings"
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agenttool"
)

// agentWithCreateTool 造一个已开启 group_create 工具的 agent(注入门控前置条件)。
func agentWithCreateTool(id int64) *agent_entity.Agent {
	a := &agent_entity.Agent{ID: id}
	a.SetTools([]agent_entity.AgentToolItem{{Key: agenttool.KeyGroupCreate, Enabled: true}})
	return a
}

func TestBuildCreateTurnMCP(t *testing.T) {
	Convey("普通单聊 + 已开 group_create → 注入 group server 只带 group_create;token 经 lookupCreate 可验", t, func() {
		s := newGroupSvc(chatSvcGateway{}, NoopEmitter{})
		s.SetGatewayBaseURL("http://127.0.0.1:1")

		specs := s.BuildCreateTurnMCP(context.Background(), agentWithCreateTool(7), 99, 0)
		So(specs, ShouldHaveLength, 1)
		So(specs[0].Name, ShouldEqual, "group")
		So(specs[0].Tools, ShouldResemble, []string{"group_create"})
		So(specs[0].URL, ShouldContainSubstring, "/mcp/group/")
		auth := specs[0].Headers["Authorization"]
		So(strings.HasPrefix(auth, "Bearer "), ShouldBeTrue)
		ref, ok := s.mcp.lookupCreate(strings.TrimPrefix(auth, "Bearer "))
		So(ok, ShouldBeTrue)
		So(ref.agentID, ShouldEqual, 7)
		So(ref.sessionID, ShouldEqual, 99)
	})

	Convey("未开 group_create 工具 → 不注入(per-agent 门控,镜像 org)", t, func() {
		s := newGroupSvc(chatSvcGateway{}, NoopEmitter{})
		s.SetGatewayBaseURL("http://127.0.0.1:1")
		So(s.BuildCreateTurnMCP(context.Background(), &agent_entity.Agent{ID: 7}, 99, 0), ShouldBeNil)

		off := &agent_entity.Agent{ID: 7}
		off.SetTools([]agent_entity.AgentToolItem{{Key: agenttool.KeyGroupCreate, Enabled: false}})
		So(s.BuildCreateTurnMCP(context.Background(), off, 99, 0), ShouldBeNil)
	})

	Convey("群成员轮(groupID>0)/ a==nil / baseURL 空 → 不注入", t, func() {
		s := newGroupSvc(chatSvcGateway{}, NoopEmitter{})
		s.SetGatewayBaseURL("http://127.0.0.1:1")
		So(s.BuildCreateTurnMCP(context.Background(), agentWithCreateTool(7), 99, 5), ShouldBeNil)
		So(s.BuildCreateTurnMCP(context.Background(), nil, 99, 0), ShouldBeNil)

		empty := newGroupSvc(chatSvcGateway{}, NoopEmitter{})
		So(empty.BuildCreateTurnMCP(context.Background(), agentWithCreateTool(7), 99, 0), ShouldBeNil)
	})
}
