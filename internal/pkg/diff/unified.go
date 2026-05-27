// internal/pkg/diff/unified.go
package diff

import (
	"bufio"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// hunkHeaderRE 匹配 unified diff hunk header: @@ -oldStart,oldLines +newStart,newLines @@ [context]
// oldLines / newLines 可省略 (=1)。
var hunkHeaderRE = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@(.*)$`)

// FromUnifiedDiffString 解析标准 unified diff body(不含 file header)。
// 调用方负责传 path / kind, 我们只解析 hunks。
func FromUnifiedDiffString(path string, kind Kind, raw string) (Payload, error) {
	f := File{Path: path, Kind: kind}

	var (
		currentHunk *Hunk
		oldLine     int
		newLine     int
	)
	scanner := bufio.NewScanner(strings.NewReader(raw))
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if m := hunkHeaderRE.FindStringSubmatch(line); m != nil {
			if currentHunk != nil {
				f.Hunks = append(f.Hunks, *currentHunk)
			}
			os_, _ := strconv.Atoi(m[1])
			ol := 1
			if m[2] != "" {
				ol, _ = strconv.Atoi(m[2])
			}
			ns, _ := strconv.Atoi(m[3])
			nl := 1
			if m[4] != "" {
				nl, _ = strconv.Atoi(m[4])
			}
			currentHunk = &Hunk{
				OldStart: os_, OldLines: ol,
				NewStart: ns, NewLines: nl,
				Header: strings.TrimSpace(m[5]),
			}
			oldLine = os_
			newLine = ns
			continue
		}
		if currentHunk == nil {
			if strings.HasPrefix(line, "@@") {
				return Payload{}, fmt.Errorf("diff: invalid hunk header %q", line)
			}
			continue
		}
		if len(line) == 0 {
			currentHunk.Lines = append(currentHunk.Lines, Line{Op: OpContext, Old: oldLine, New: newLine, Text: ""})
			oldLine++
			newLine++
			continue
		}
		switch line[0] {
		case ' ':
			currentHunk.Lines = append(currentHunk.Lines, Line{Op: OpContext, Old: oldLine, New: newLine, Text: line[1:]})
			oldLine++
			newLine++
		case '+':
			currentHunk.Lines = append(currentHunk.Lines, Line{Op: OpAdd, New: newLine, Text: line[1:]})
			f.Plus++
			newLine++
		case '-':
			currentHunk.Lines = append(currentHunk.Lines, Line{Op: OpDel, Old: oldLine, Text: line[1:]})
			f.Minus++
			oldLine++
		case '\\':
			// "\ No newline at end of file" — 忽略
		default:
			return Payload{}, fmt.Errorf("diff: invalid line %q", line)
		}
	}
	if err := scanner.Err(); err != nil {
		return Payload{}, err
	}
	if currentHunk != nil {
		f.Hunks = append(f.Hunks, *currentHunk)
	}

	// 累计所有 hunks 的行数,超过 MaxLinesPerFile 就标记截断,只保留前 MaxLinesPerFile 条。
	total := 0
	for _, h := range f.Hunks {
		total += len(h.Lines)
	}
	if total > MaxLinesPerFile {
		f.Truncated = true
		remain := MaxLinesPerFile
		trimmed := make([]Hunk, 0, len(f.Hunks))
		for _, h := range f.Hunks {
			if remain <= 0 {
				break
			}
			if len(h.Lines) <= remain {
				trimmed = append(trimmed, h)
				remain -= len(h.Lines)
			} else {
				h.Lines = h.Lines[:remain]
				trimmed = append(trimmed, h)
				remain = 0
			}
		}
		f.Hunks = trimmed
	}

	return Payload{Files: []File{f}}, nil
}
