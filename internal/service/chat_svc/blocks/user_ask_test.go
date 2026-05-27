package blocks

import (
	"testing"

	cagoblocks "github.com/cago-frame/agents/agent/blocks"
	. "github.com/smartystreets/goconvey/convey"
)

func TestUserAskBlock_TypeAndAudience(t *testing.T) {
	Convey("UserAskBlock 类型 + Audience", t, func() {
		b := UserAskBlock{}
		So(b.Type(), ShouldEqual, "user_ask")
		So(b.Audience(), ShouldEqual, cagoblocks.ToUI)
	})
}

func TestUserAskBlock_FieldsMutable(t *testing.T) {
	Convey("UserAskBlock 可 mutate Answered/Answers/Skipped", t, func() {
		b := &UserAskBlock{}
		b.Answered = true
		b.Answers = []AskAnswerDTO{{QuestionIndex: 0, Labels: []string{"A"}}}
		So(b.Answered, ShouldBeTrue)
		So(b.Skipped, ShouldBeFalse)
		So(b.Answers, ShouldHaveLength, 1)
		So(b.Answers[0].Labels[0], ShouldEqual, "A")
	})
}

func TestUserAskBlock_FactoryRegistered(t *testing.T) {
	Convey("UserAskBlock 已通过 init() 注册到 cago factory; Encode/Decode round-trip", t, func() {
		b := &UserAskBlock{RequestID: "r-1", Questions: []AskQuestionDTO{{Question: "q?"}}}
		sb, err := cagoblocks.Encode(b)
		So(err, ShouldBeNil)
		So(sb.Type, ShouldEqual, "user_ask")

		decoded, err := cagoblocks.Decode(sb)
		So(err, ShouldBeNil)
		got, ok := decoded.(UserAskBlock)
		So(ok, ShouldBeTrue)
		So(got.RequestID, ShouldEqual, "r-1")
	})
}
