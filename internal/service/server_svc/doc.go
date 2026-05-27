// Package server_svc 实现桌面端与 agentre 联机后端之间的接入。
//
// 责任：
//   - RFC 8628 Device Flow 登录（StartLogin / PollLoginToken / CancelLogin）
//   - access token 刷新循环
//   - 设备列表 / 当前状态读取
//   - 登出（远端 revoke + 本地 keychain 清除 + server_state 清空）
//
// 提供桌面端与 Hub 的接入层;sync / 业务能力按需扩展。
package server_svc
