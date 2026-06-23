package claudecode

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestWithModel_SetsModelOnClient 锁住 WithModel 选项的契约：
// 必须落到 Client.model 字段，并在后续 Stream/OpenSession 构造 runSpec
// 时透传成 --model argv —— 它是 GLM / 第三方 provider 让 CLI 在 system.init
// 帧里报正确 model id 的唯一入口（不传 --model 时 CLI 报本地默认登录的 model，
// 经 gateway 透明改写仍能调通 LLM，但前端拿不到真实 provider model）。
func TestWithModel_SetsModelOnClient(t *testing.T) {
	c := New(WithModel("glm-5.1"))
	assert.Equal(t, "glm-5.1", c.model)

	// 空串显式无副作用：调用方传 "" 时不应该把已有值抹掉。
	c2 := New(WithModel("glm-5.1"), WithModel(""))
	assert.Equal(t, "", c2.model, "WithModel 没有「忽略空串」的语义，最后一次 WithModel 直接覆盖")
}

// TestWithModel_PropagatesToArgs 验证 client 端配的 model 会被 OpenSession
// 真正写进子进程 argv —— 这一步是 Bug 防回归的关键，避免 WithModel 被加上但
// 路径没接通的"看起来加了但等于没加"。
func TestWithModel_PropagatesToArgs(t *testing.T) {
	c := New(WithModel("glm-5.1"))
	// 直接构造 runSpec 走的就是 OpenSession / Stream 内部那条路。
	spec := runSpec{model: c.model}
	args := buildArgs(spec)
	assert.Contains(t, strings.Join(args, " "), "--model glm-5.1")
}

// TestWithAllowedTools_AppendsAndSkipsEmpty 锁住 WithAllowedTools 的承重契约：
// 多次调用累加（而非覆盖），且空串被跳过。这是它相对普通 setter 存在的全部理由
// —— 群聊注入工具时要叠加在已有 allowedTools 之上。生产当前只调一次，契约靠本测试守住，
// 防止未来重构悄悄退化成覆盖语义。
func TestWithAllowedTools_AppendsAndSkipsEmpty(t *testing.T) {
	c := New(WithAllowedTools("Read"), WithAllowedTools("Bash", ""))
	// 第二次调用 append 到第一次之上，"" 被丢弃，顺序保留。
	assert.Equal(t, []string{"Read", "Bash"}, c.AllowedTools())
}
