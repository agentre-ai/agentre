// internal/service/remote_device_svc/remove.go
package remote_device_svc

import (
	"context"

	"github.com/cago-frame/cago/pkg/logger"
	"go.uber.org/zap"
)

func (s *service) Remove(ctx context.Context, id int64) error {
	if err := s.repo.Delete(ctx, id); err != nil {
		return err
	}
	if err := s.keychain.Delete(keychainAccountForToken(id)); err != nil {
		logger.Ctx(ctx).Warn("remote_device remove: keychain delete failed; id not reused, leak is harmless",
			zap.Int64("id", id), zap.Error(err))
	}
	if s.watcher != nil {
		s.watcher.Stop(id)
	}
	return nil
}
