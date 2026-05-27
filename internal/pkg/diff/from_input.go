package diff

import "strings"

// FromEdit 处理 claudecode Edit 工具的 input(map),返回 diff.Payload。
// 一定有 1 个 File(可能 0 hunks)。
func FromEdit(input map[string]any) Payload {
	filePath := pickString(input, "file_path")
	oldS := pickString(input, "old_string")
	newS := pickString(input, "new_string")
	replaceAll, _ := input["replace_all"].(bool)
	return FromClaudeCodeEdit(filePath, oldS, newS, replaceAll)
}

// FromMultiEdit 处理 claudecode MultiEdit 工具的 input(map),把 edits 列表串成
// 单 File 多 Hunk。edits 为空或全无效时仍返单 File(0 hunks)。
func FromMultiEdit(input map[string]any) Payload {
	filePath := pickString(input, "file_path")
	editsRaw, _ := input["edits"].([]any)
	var allHunks []Hunk
	var plus, minus int
	for _, e := range editsRaw {
		m, ok := e.(map[string]any)
		if !ok {
			continue
		}
		one := FromClaudeCodeEdit("", pickString(m, "old_string"), pickString(m, "new_string"), false)
		if len(one.Files) == 0 {
			continue
		}
		allHunks = append(allHunks, one.Files[0].Hunks...)
		plus += one.Files[0].Plus
		minus += one.Files[0].Minus
	}
	return Payload{Files: []File{{
		Path:  filePath,
		Kind:  KindModified,
		Hunks: allHunks,
		Plus:  plus,
		Minus: minus,
	}}}
}

// FromFileChange 处理 codex file_change 工具的 input(map),返回多 File Payload。
// 没有有效 file change 时返 (Payload{}, false)。
func FromFileChange(input map[string]any) (Payload, bool) {
	changesRaw, _ := input["changes"].([]any)
	if len(changesRaw) == 0 {
		return Payload{}, false
	}
	var files []File
	for _, c := range changesRaw {
		m, ok := c.(map[string]any)
		if !ok {
			continue
		}
		path := pickString(m, "path")
		kind := mapCodexKind(m["kind"])
		raw := pickString(m, "diff")
		if (kind == KindCreated || kind == KindDeleted) && !hasUnifiedHunk(raw) {
			files = append(files, fileFromWholeContent(path, kind, raw))
			continue
		}
		p, err := FromUnifiedDiffString(path, kind, raw)
		if err != nil {
			if kind == KindCreated || kind == KindDeleted {
				files = append(files, fileFromWholeContent(path, kind, raw))
			}
			continue
		}
		if len(p.Files) == 0 {
			continue
		}
		files = append(files, p.Files[0])
	}
	if len(files) == 0 {
		return Payload{}, false
	}
	return Payload{Files: files}, true
}

func mapCodexKind(v any) Kind {
	switch k := v.(type) {
	case string:
		return mapCodexKindType(k)
	case map[string]any:
		return mapCodexKindType(pickString(k, "type"))
	default:
		return KindModified
	}
}

func mapCodexKindType(k string) Kind {
	switch strings.ToLower(k) {
	case "add", "create", "created":
		return KindCreated
	case "delete", "deleted", "remove":
		return KindDeleted
	default:
		return KindModified
	}
}

func hasUnifiedHunk(raw string) bool {
	for _, line := range strings.Split(raw, "\n") {
		if hunkHeaderRE.MatchString(line) {
			return true
		}
	}
	return false
}

func fileFromWholeContent(path string, kind Kind, raw string) File {
	lines := splitLinesNoTrailingEmpty(raw)
	f := File{Path: path, Kind: kind}
	if kind != KindCreated && kind != KindDeleted {
		return f
	}

	hunk := Hunk{}
	if kind == KindCreated {
		f.Plus = len(lines)
		hunk.OldStart = 0
		hunk.OldLines = 0
		hunk.NewStart = 1
		hunk.NewLines = len(lines)
		hunk.Lines = make([]Line, 0, len(lines))
		for i, text := range lines {
			hunk.Lines = append(hunk.Lines, Line{Op: OpAdd, New: i + 1, Text: text})
		}
	} else {
		f.Minus = len(lines)
		hunk.OldStart = 1
		hunk.OldLines = len(lines)
		hunk.NewStart = 0
		hunk.NewLines = 0
		hunk.Lines = make([]Line, 0, len(lines))
		for i, text := range lines {
			hunk.Lines = append(hunk.Lines, Line{Op: OpDel, Old: i + 1, Text: text})
		}
	}
	if len(hunk.Lines) == 0 {
		return f
	}
	if len(hunk.Lines) > MaxLinesPerFile {
		f.Truncated = true
		hunk.Lines = hunk.Lines[:MaxLinesPerFile]
	}
	f.Hunks = []Hunk{hunk}
	return f
}

func pickString(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}
