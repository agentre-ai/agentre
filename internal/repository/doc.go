// Package repository 汇总所有数据访问子包。
//
// 每个领域一个子包：internal/repository/<domain>_repo，组织如下：
//
//	type XxxRepo interface { ... }                     // 消费方约束：service 只依赖这个接口
//	var defaultXxx XxxRepo                             // 进程级单例
//	func Xxx() XxxRepo { return defaultXxx }           // 取实例
//	func RegisterXxx(i XxxRepo) { defaultXxx = i }     // bootstrap 中注入实现
//	type xxxRepo struct{}                              // 默认 GORM 实现
//	func NewXxx() XxxRepo { return &xxxRepo{} }
//
// 查询统一走 db.Ctx(ctx)；事务通过 db.WithContextDB(ctx, tx) 注入上下文，
// 后续同 ctx 内的 db.Ctx(ctx) 会自动使用事务句柄。
//
// mock 放在 mock_<domain>_repo 子目录，用 `go generate ./...` (mockgen) 生成。
package repository
