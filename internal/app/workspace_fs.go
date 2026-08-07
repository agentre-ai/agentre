package app

import (
	"github.com/agentre-ai/agentre/internal/service/workspace_fs_svc"
)

// WorkspaceFsListDir 列出会话工作目录下 relPath 这一层。
//   - relPath 相对会话工作目录,空串表示工作目录本身;越界请求由服务层拒绝
//   - includeIgnored=false 时被 git 忽略的条目不出现在结果里
//
// 本地会话与远端(agentred)会话都走这一个方法,路由由服务层按会话解析出的
// deviceID 决定,前端不感知差异。
func (a *App) WorkspaceFsListDir(sessionID int64, relPath string, includeIgnored bool) (*workspace_fs_svc.ListDirView, error) {
	return workspace_fs_svc.Default().ListDir(a.ctx, sessionID, relPath, includeIgnored)
}

// WorkspaceFsGitChanges 取会话工作目录的 git 变动。
//   - scope: "uncommitted"(未提交档)或 "branch"(本分支档)
//   - baseRef: 仅 "branch" 档有意义,空串表示用推断出的默认基线;返回值的
//     BaseRef 是本次实际比较用的那个
func (a *App) WorkspaceFsGitChanges(sessionID int64, scope, baseRef string) (*workspace_fs_svc.GitChangesView, error) {
	return workspace_fs_svc.Default().GitChanges(a.ctx, sessionID, scope, baseRef)
}

// WorkspaceFsGitBranches 取会话工作目录的分支清单、当前分支与推断出的默认基线。
func (a *App) WorkspaceFsGitBranches(sessionID int64) (*workspace_fs_svc.GitBranchesView, error) {
	return workspace_fs_svc.Default().GitBranches(a.ctx, sessionID)
}

// WorkspaceFsReadFile 读取会话工作目录下 relPath 所指文件的内容(会话级文件
// 预览,纯读)。文本返回 UTF-8 正文;图片返回 base64 内容 + contentType;
// binary/tooLarge 是视图标志。本地与远端(agentred)会话都走这一个方法,路由
// 由服务层按会话解析出的 deviceID 决定;路径越界由服务层 / daemon 强制。
func (a *App) WorkspaceFsReadFile(sessionID int64, relPath string) (*workspace_fs_svc.ReadFileView, error) {
	return workspace_fs_svc.Default().ReadFile(a.ctx, sessionID, relPath)
}

// WorkspaceFsGitFileContent 取同一文件在 git HEAD 的版本(对比档左列)。文件未
// 跟踪 / 不在 HEAD → 空基线(hasHead=false);非 git 仓库 → notARepo。
func (a *App) WorkspaceFsGitFileContent(sessionID int64, relPath string) (*workspace_fs_svc.GitFileContentView, error) {
	return workspace_fs_svc.Default().GitFileContent(a.ctx, sessionID, relPath)
}
