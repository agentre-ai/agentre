// Package service 汇总所有业务服务子包。
//
// 每个领域一个子包：internal/service/<domain>_svc，组织如下：
//
//	type XxxSvc interface { ... }                  // 服务对外暴露的能力
//	type xxxSvc struct { ... }                     // 默认实现，组合所需的 repo / 外部依赖
//	var defaultXxx = &xxxSvc{}                     // 进程级单例
//	func Xxx() XxxSvc { return defaultXxx }        // 取实例（或 NewXxx 构造定制实例）
//
// 服务层只依赖 repository 接口（依赖倒置），通过 mock 进行单元测试。
// Wails 绑定层（main.go / app.go）保持轻薄：解析请求 → 调用 service → 返回结果，
// 业务逻辑全部进入 service，确保可被 go test 覆盖。
//
// 跨服务调用通过 svc.Other() 取单例；后台任务使用 gogo.Go(func() error { ... })，
// 不要把请求 ctx 透传进 goroutine，按需用闭包捕获。
package service
