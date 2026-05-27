// Package entity 存放领域实体（充血模型）。
//
// 每个领域一个子包：internal/model/entity/<domain>_entity，
// 包内一般包含：
//   - <Domain> 结构体，带 GORM tag，TableName() 方法绑定表名；
//   - 状态常量（如 StatusActive / StatusDeleted）；
//   - 业务方法：Check(ctx)、IsActive()、GetXxx()/SetXxx() 等围绕实体自身数据的规则。
//
// 充血模型要求把围绕单个实体的校验、状态判断、字段序列化等行为放在实体上，
// 让 service 层专注于跨实体协作与外部依赖编排，而不是把所有规则堆进 service。
//
// 不适合放进 entity 的：跨实体协调、外部 IO（DB/HTTP/RPC）、依赖其它 service 的逻辑。
package entity
