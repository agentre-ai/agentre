//go:build !e2e

// Package fakes 在生产/默认构建中只暴露一个空 Install;真正的 e2e fake 装配
// 仅在 `-tags e2e` 下编译进来(见 install.go)。
package fakes

import "context"

// Install 在生产/默认构建中是空操作。
func Install(_ context.Context) {}

// InstallAgentred 在 agentred(daemon)的 e2e 构建里注册确定性 fake runtime;生产/默认
// 构建里与 Install 一样是空操作。安装的是 fake 本身,不含桌面侧那套 backend/agent 播种
// (daemon 的 SQLite 没有那些表;agentred 的 runtime 选择按 wire RunParams 里的
// backend.type 走 agentruntime.RuntimeFor,只注册就够)。
func InstallAgentred(_ context.Context) {}
