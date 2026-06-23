package workflow_entity_test

import (
	"context"
	"testing"

	"github.com/cago-frame/cago/pkg/consts"
	. "github.com/smartystreets/goconvey/convey"

	"github.com/agentre-ai/agentre/internal/model/entity/workflow_entity"
)

func TestWorkflow_Check(t *testing.T) {
	ctx := context.Background()
	Convey("Check 校验 nil / 空名 / 合法", t, func() {
		var w0 *workflow_entity.Workflow
		So(w0.Check(ctx), ShouldNotBeNil)
		So((&workflow_entity.Workflow{Name: "  "}).Check(ctx), ShouldNotBeNil)
		So((&workflow_entity.Workflow{Name: "产品开发流程"}).Check(ctx), ShouldBeNil)
	})
}

// IsActive 是 T11 SOP 注入的 gate(nil-safe):软删/非 active 流程不得注入提示。
func TestWorkflow_IsActive(t *testing.T) {
	Convey("IsActive: nil → false; ACTIVE → true; Status=0 → false", t, func() {
		var w0 *workflow_entity.Workflow
		So(w0.IsActive(), ShouldBeFalse)
		So((&workflow_entity.Workflow{Status: consts.ACTIVE}).IsActive(), ShouldBeTrue)
		So((&workflow_entity.Workflow{Status: 0}).IsActive(), ShouldBeFalse)
	})
}
