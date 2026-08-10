package workspacefs

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// DefaultMaxSearchHits 是一次搜索返回的命中上限。比单层列举的 DefaultMaxEntries
// (2000)小:搜索结果是 240px 侧栏里的一条**扁平**列表,几百行之后正确的动作是
// 改查询串而不是继续滚;上限本身要回报给前端(Truncated),说明文案里带的就是
// 这个数字。
const DefaultMaxSearchHits = 500

// DefaultMaxSearchDirs 是一次搜索允许进入的目录数预算(每个 os.ReadDir 记一次)。
// 参照量级:本仓库剪枝后约 250 个目录,连 node_modules 一起数约 4300 个;两万给
// 大 monorepo 与"显示忽略项"(此时不剪枝)都留了余量,又保证遍历必然终止,不会
// 让一次搜索把符号链接之外的病态目录树走到超时。
//
// 触及预算按截断处理(Truncated=true)而不是报错:已经找到的命中仍然有用,但
// 必须显式告诉调用方结果不完整——不静默返回不完整结果。
const DefaultMaxSearchDirs = 20000

// SearchHit 是一次搜索的一个命中。
type SearchHit struct {
	// Path 相对搜索根,恒用 "/" 分隔(与 Change.Path 同一形状,前端拿它拼 cwd)。
	Path string
	// IsDir 区分目录与文件命中。目录同样参与匹配:用户按名找 "views" 这样的
	// 目录与找文件是同一个动作,前端据此选行的图标与菜单。符号链接按非目录
	// 计——遍历不跟随它(见 SearchFiles)。
	IsDir bool
}

// SearchFilesResult 是 SearchFiles 的返回结果。
type SearchFilesResult struct {
	Hits []SearchHit
	// Truncated 表示结果**不完整**:命中数触及 maxHits,或遍历触及 maxDirs
	// 目录预算。两种截断合用一个标志,前端出同一条说明。
	Truncated bool
}

// SearchFiles 从 root 递归搜索 basename 含 query 子串(不区分大小写)的文件与
// 目录。
//   - root 是会话已解析的绝对工作目录;为空 → ErrNoCwd(会话配置问题,与越界
//     的 ErrPathRefused 区分,契约同 ReadFile)。
//   - query 去掉首尾空白后为空 → 直接返回空结果,不遍历。
//   - ".git" 恒不进入,不受 includeIgnored 影响(与 ListDir 一致)。
//   - includeIgnored 为 false 时,被 git 忽略的目录**整棵剪枝**、被忽略的文件
//     不计入;为 true 时完全不做忽略判定(也就不起 git 子进程)。非 git 目录
//     判不出忽略,等价于不剪枝(设计决策 7:非 git 目录不判定)。
//   - 命中数达到 maxHits、或进入的目录数达到 maxDirs 时停下并置 Truncated。
//   - ctx 取消 / 超时 → 返回 ctx 的错误,而不是一份看着完整的部分结果。
//
// 遍历是**逐层**(BFS)的单 goroutine 走法,不并发:
//   - 浅的命中先出,而用户要找的文件通常离根更近;
//   - 忽略判定按层批量做一次 `git check-ignore`(每层一个子进程),而不是每个
//     目录一次——后者在几千个目录的仓库上就是几千次 fork,比遍历本身贵得多;
//   - 单 goroutine 让"截断截在哪"是确定的,并发走法的结果集会随调度抖动。
//
// 纯读:只 ReadDir 与 `git check-ignore`(只读子命令,带 --no-optional-locks),
// 不写文件、不碰 git 索引。
//
// 遍历不跟随符号链接(os.DirEntry.IsDir() 对链接恒为 false,因此链接目录不会被
// 展开):链接的目标可能在 root 之外,跟随就等于让搜索根逃出会话工作目录,还可
// 能成环。链接自身仍是 root 内的条目,按名命中时照常返回。
func SearchFiles(ctx context.Context, root, query string, includeIgnored bool, maxHits, maxDirs int) (*SearchFilesResult, error) {
	if root == "" {
		return nil, ErrNoCwd
	}
	if maxHits <= 0 {
		maxHits = DefaultMaxSearchHits
	}
	if maxDirs <= 0 {
		maxDirs = DefaultMaxSearchDirs
	}

	res := &SearchFilesResult{Hits: []SearchHit{}}
	needle := strings.ToLower(strings.TrimSpace(query))
	if needle == "" {
		return res, nil
	}

	// level 是当前这一层待展开的目录(相对 root 的 "/" 路径,"" 即 root 本身)。
	level := []string{""}
	visited := 0

	for len(level) > 0 {
		var children []SearchHit
		for _, dirRel := range level {
			if err := ctx.Err(); err != nil {
				return nil, fmt.Errorf("workspacefs: search: %w", err)
			}
			if visited >= maxDirs {
				res.Truncated = true
				return res, nil
			}
			visited++

			dirents, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(dirRel)))
			if err != nil {
				// 单个目录读不动(权限、并发删除)只跳过它,不让整次搜索失败——
				// 与 ListDir 对单条目出错的容错约定一致。
				continue
			}
			for _, d := range dirents {
				name := d.Name()
				if name == ".git" {
					continue
				}
				rel := name
				if dirRel != "" {
					rel = dirRel + "/" + name
				}
				children = append(children, SearchHit{Path: rel, IsDir: d.IsDir()})
			}
		}

		children = dropIgnored(ctx, root, children, includeIgnored)

		var next []string
		for _, c := range children {
			if strings.Contains(strings.ToLower(path.Base(c.Path)), needle) {
				if len(res.Hits) >= maxHits {
					res.Truncated = true
					return res, nil
				}
				res.Hits = append(res.Hits, c)
			}
			if c.IsDir {
				next = append(next, c.Path)
			}
		}
		level = next
	}

	return res, nil
}

// dropIgnored 把这一层里被 git 忽略的条目整批剔除(目录被剔除即等于整棵剪枝:
// 它不会进入下一层的展开清单)。includeIgnored 为 true 时原样返回,连 git 都不
// 起。判定用一次 `git check-ignore`,路径相对 root 传入;非 git 目录 / git 失败
// 时 gitIgnoredSet 返回空集合,等价于不剪枝。
func dropIgnored(ctx context.Context, root string, children []SearchHit, includeIgnored bool) []SearchHit {
	if includeIgnored || len(children) == 0 {
		return children
	}
	paths := make([]string, len(children))
	for i, c := range children {
		paths[i] = c.Path
	}
	ignored := gitIgnoredSet(ctx, root, paths)
	if len(ignored) == 0 {
		return children
	}
	kept := children[:0]
	for _, c := range children {
		if !ignored[c.Path] {
			kept = append(kept, c)
		}
	}
	return kept
}
