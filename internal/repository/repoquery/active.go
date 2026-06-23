package repoquery

import (
	"context"
	"fmt"

	"github.com/cago-frame/cago/database/db"
	"github.com/cago-frame/cago/pkg/consts"
)

func ActiveMap[T any, K comparable](ctx context.Context, column string, keys []K, keyOf func(*T) K) (map[K]*T, error) {
	out := make(map[K]*T, len(keys))
	if len(keys) == 0 {
		return out, nil
	}
	var rows []*T
	if err := db.Ctx(ctx).Where(fmt.Sprintf("%s IN ? AND status = ?", column), keys, consts.ACTIVE).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[keyOf(row)] = row
	}
	return out, nil
}
