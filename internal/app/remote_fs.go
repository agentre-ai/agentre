package app

import (
	"agentre/internal/service/remote_fs_svc"
)

// RemoteFsListDir 列出远端 device 上某目录下的子项。
//   - deviceID = paired_agentred.id 字符串化(与 ProjectLocationUpsert 一致)
//   - path 为空 → daemon 端解析为 $HOME
func (a *App) RemoteFsListDir(deviceID, path string) (*remote_fs_svc.ListDirView, error) {
	return remote_fs_svc.Default().ListDir(a.ctx, deviceID, path)
}

// RemoteFsMkdir 在远端 device 上的 parent 下创建文件夹 name(非递归)。
func (a *App) RemoteFsMkdir(deviceID, parent, name string) (*remote_fs_svc.MkdirView, error) {
	return remote_fs_svc.Default().Mkdir(a.ctx, deviceID, parent, name)
}
