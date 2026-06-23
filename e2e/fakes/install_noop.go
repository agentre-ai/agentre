//go:build !e2e

// Package fakes 在生产/默认构建中只暴露一个空 Install;真正的 e2e fake 装配
// 仅在 `-tags e2e` 下编译进来(见 install.go)。
package fakes

import "context"

// Install 在生产/默认构建中是空操作。
func Install(_ context.Context) {}
