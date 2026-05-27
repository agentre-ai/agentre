package canonical

// FileEdit 局部修改文件(含 created/modified/deleted diff);前端 DiffCard 渲染。
// 来源:claudecode Edit/MultiEdit / codex fileChange{Changes[].Kind in {modified,deleted}}。
type FileEdit struct {
	Files []FileEditPatch `json:"files"`
}

func (FileEdit) canonicalKind() Kind { return KindFileEdit }

type FileEditPatch struct {
	Path       string         `json:"path"`
	Kind       FileChangeKind `json:"kind"` // "created" / "modified" / "deleted"
	Hunks      []DiffHunk     `json:"hunks"`
	Plus       int            `json:"plus,omitempty"`
	Minus      int            `json:"minus,omitempty"`
	Truncated  bool           `json:"truncated,omitempty"`
	ReplaceAll bool           `json:"replaceAll,omitempty"` // claudecode Edit.replace_all
}

type FileChangeKind string

const (
	ChangeCreated  FileChangeKind = "created"
	ChangeModified FileChangeKind = "modified"
	ChangeDeleted  FileChangeKind = "deleted"
)

type DiffHunk struct {
	OldStart int        `json:"oldStart"`
	OldLines int        `json:"oldLines"`
	NewStart int        `json:"newStart"`
	NewLines int        `json:"newLines"`
	Header   string     `json:"header,omitempty"`
	Lines    []DiffLine `json:"lines"`
}

type DiffLine struct {
	Op   DiffOp `json:"op"`
	Old  int    `json:"old,omitempty"`
	New  int    `json:"new,omitempty"`
	Text string `json:"text"`
}

type DiffOp string

const (
	OpContext DiffOp = " "
	OpAdd     DiffOp = "+"
	OpRemove  DiffOp = "-"
)
