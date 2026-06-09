package models

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/dto"
)

func GetUserItemData(ctx context.Context, pool *pgxpool.Pool, userID, itemID string) (*dto.UserDataRow, error) {
	row := pool.QueryRow(ctx,
		"SELECT playback_position_ticks, play_count, is_favorite, played, last_played_date FROM user_item_data WHERE user_id = $1::uuid AND item_id = $2::uuid",
		userID, itemID)

	var ud dto.UserDataRow
	err := row.Scan(&ud.PlaybackPositionTicks, &ud.PlayCount, &ud.IsFavorite, &ud.Played, &ud.LastPlayedDate)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &ud, nil
}

func UpsertUserItemData(ctx context.Context, pool *pgxpool.Pool, userID, itemID string, position *int64, playCount *int32, isFavorite *bool, played *bool) error {
	_, err := pool.Exec(ctx,
		`INSERT INTO user_item_data (user_id, item_id, playback_position_ticks, play_count, is_favorite, played, last_played_date)
		 VALUES ($1::uuid, $2::uuid, COALESCE($3::bigint, 0), COALESCE($4, 0), COALESCE($5, false), COALESCE($6, false), NOW())
		 ON CONFLICT (user_id, item_id) DO UPDATE SET
		   playback_position_ticks = COALESCE($3::bigint, user_item_data.playback_position_ticks),
		   play_count = COALESCE($4, user_item_data.play_count),
		   is_favorite = COALESCE($5, user_item_data.is_favorite),
		   played = COALESCE($6, user_item_data.played),
		   last_played_date = NOW()`,
		userID, itemID, position, playCount, isFavorite, played)
	return err
}

// SetHiddenFromResume 仅更新 is_hidden_from_resume 标记,不动 playback_position
// 等其它字段。用于 HideFromResume 端点:客户端从"继续观看"列表移除条目时,
// 位置数据保留,可通过 Hide=false 再恢复显示。
func SetHiddenFromResume(ctx context.Context, pool *pgxpool.Pool, userID, itemID string, hidden bool) error {
	_, err := pool.Exec(ctx,
		`INSERT INTO user_item_data (user_id, item_id, is_hidden_from_resume)
		 VALUES ($1::uuid, $2::uuid, $3)
		 ON CONFLICT (user_id, item_id) DO UPDATE SET is_hidden_from_resume = $3`,
		userID, itemID, hidden)
	return err
}

// QueryNextUp 实现 Emby 的 /Shows/NextUp:对用户"在追"的每部剧(至少看完过一集),
// 按"接着最后看完的那一集往后"推下一集(且下一集未看完),按最近播放时间倒序。
// 只考虑正片(季号 > 0,排除 Specials),无 played 集的剧不返回。
