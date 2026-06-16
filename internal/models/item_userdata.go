package models

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/dto"
	"fyms/internal/repository"
)

func GetUserItemData(ctx context.Context, pool *pgxpool.Pool, userID, itemID string) (*dto.UserDataRow, error) {
	return repository.NewItemHelperRepository(pool).GetUserItemData(ctx, userID, itemID)
}

func UpsertUserItemData(ctx context.Context, pool *pgxpool.Pool, userID, itemID string, position *int64, playCount *int32, isFavorite *bool, played *bool) error {
	return repository.NewItemHelperRepository(pool).UpsertUserItemData(ctx, userID, itemID, position, playCount, isFavorite, played)
}

// SetHiddenFromResume 仅更新 is_hidden_from_resume 标记,不动 playback_position
// 等其它字段。用于 HideFromResume 端点:客户端从"继续观看"列表移除条目时,
// 位置数据保留,可通过 Hide=false 再恢复显示。
func SetHiddenFromResume(ctx context.Context, pool *pgxpool.Pool, userID, itemID string, hidden bool) error {
	return repository.NewItemHelperRepository(pool).SetHiddenFromResume(ctx, userID, itemID, hidden)
}

// QueryNextUp 实现 Emby 的 /Shows/NextUp:对用户"在追"的每部剧(至少看完过一集),
// 按"接着最后看完的那一集往后"推下一集(且下一集未看完),按最近播放时间倒序。
// 只考虑正片(季号 > 0,排除 Specials),无 played 集的剧不返回。
