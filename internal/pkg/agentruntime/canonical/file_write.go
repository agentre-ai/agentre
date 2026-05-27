package canonical

// FileWrite 全量写入文件;前端 WriteCard 渲染。
// 来源:claudecode Write{file_path, content} / codex fileChange{Changes[].Kind=created}。
type FileWrite struct {
	Path      string `json:"path"`
	Content   string `json:"content"`
	Lines     int    `json:"lines"`
	Bytes     int    `json:"bytes"`
	Truncated bool   `json:"truncated,omitempty"` // 内容超 chat_svc.writeContentByteCap 时截断,标记 true
}

func (FileWrite) canonicalKind() Kind { return KindFileWrite }
