// Package diff 把 Edit / MultiEdit / Codex file_change 的 toolInput 规整成
// 前端可直接渲染的结构化 diff payload。Write 工具不走本包,有独立通路。
package diff

// Payload 是单个 tool_use 折算成的 diff 投影。
// 单文件场景也用 Files 列表(长度=1),避免单/多文件二态。
type Payload struct {
	Files []File `json:"files"`
}

type File struct {
	Path       string `json:"path"`
	Kind       Kind   `json:"kind"`
	Hunks      []Hunk `json:"hunks,omitempty"`
	Plus       int    `json:"plus"`
	Minus      int    `json:"minus"`
	Truncated  bool   `json:"truncated,omitempty"`
	ReplaceAll bool   `json:"replaceAll,omitempty"` // Edit replace_all=true 时透传
}

type Kind string

const (
	KindModified Kind = "modified"
	KindCreated  Kind = "created"
	KindDeleted  Kind = "deleted"
)

type Hunk struct {
	OldStart int    `json:"oldStart"`
	OldLines int    `json:"oldLines"`
	NewStart int    `json:"newStart"`
	NewLines int    `json:"newLines"`
	Header   string `json:"header,omitempty"`
	Lines    []Line `json:"lines"`
}

type Line struct {
	Op   Op     `json:"op"`
	Old  int    `json:"old,omitempty"`
	New  int    `json:"new,omitempty"`
	Text string `json:"text"`
}

type Op string

const (
	OpContext Op = " "
	OpAdd     Op = "+"
	OpDel     Op = "-"
)

// MaxLinesPerFile 单文件 diff 行数上限。超过时 File.Truncated=true,
// 行数仍然写完整(前端可决定是否做虚拟滚动),但前端会显示截断提示。
const MaxLinesPerFile = 200
