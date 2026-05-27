// Package server_state_entity 维护桌面端与 Server 联机状态的单行实体。
package server_state_entity

import "time"

// ServerState 桌面端联机状态，全表只有一行（id = 1，CHECK 约束）。
type ServerState struct {
	ID                int64  `gorm:"column:id;primaryKey;autoIncrement:false"`
	ServerURL         string `gorm:"column:server_url;type:text;not null;default:''"`
	DeviceID          int64  `gorm:"column:device_id;type:integer;not null;default:0"`
	DeviceFingerprint string `gorm:"column:device_fingerprint;type:text;not null;default:''"`
	ServerUserID      int64  `gorm:"column:server_user_id;type:integer;not null;default:0"`
	KeychainAccount   string `gorm:"column:keychain_account;type:text;not null;default:''"`
	Updatetime        int64  `gorm:"column:updatetime;type:integer;not null;default:0"`
}

// TableName GORM 表名。
func (s *ServerState) TableName() string { return "server_state" }

// IsLoggedIn returns true when user, device, and keychain are all bound.
// Any one of them being zero/empty means the desktop is not connected.
func (s *ServerState) IsLoggedIn() bool {
	return s.ServerUserID != 0 && s.DeviceID != 0 && s.KeychainAccount != ""
}

// Touch sets Updatetime to now (ms epoch).
func (s *ServerState) Touch() { s.Updatetime = time.Now().UnixMilli() }
