package repository

import "fmt"

// LatestVirtualActivityExpression returns the activity timestamp used to sort
// a latest-library item. Series activity follows its newest episode, while a
// movie uses its own created_at.
func LatestVirtualActivityExpression(itemAlias string) string {
	return fmt.Sprintf(`GREATEST(
		%s.created_at,
		COALESCE((
			SELECT latest_episode.created_at
			FROM items latest_episode
			WHERE latest_episode.type = 'Episode'
			  AND latest_episode.series_id = %s.id
			ORDER BY latest_episode.created_at DESC, latest_episode.id DESC
			LIMIT 1
		), %s.created_at)
	)`, itemAlias, itemAlias, itemAlias)
}

// LatestVirtualMembersSQL returns a subquery selecting the latest movie and
// parent-Series IDs. limitParam and allowedLibraryParam are caller-owned SQL
// placeholder indexes.
func LatestVirtualMembersSQL(limitParam int, allowedLibraryParam *int) string {
	movieScope := ""
	seriesScope := ""
	if allowedLibraryParam != nil {
		movieScope = fmt.Sprintf(" AND latest_movie.library_id = ANY($%d::uuid[])", *allowedLibraryParam)
		seriesScope = fmt.Sprintf(" AND latest_series.library_id = ANY($%d::uuid[])", *allowedLibraryParam)
	}
	return fmt.Sprintf(`
		SELECT latest_candidate.id
		FROM (
			SELECT latest_movie.id, latest_movie.created_at AS activity_at
			FROM items latest_movie
			WHERE latest_movie.type = 'Movie'
			  AND latest_movie.merged_to_id IS NULL%s

			UNION ALL

			SELECT latest_series.id,
			       GREATEST(latest_series.created_at, COALESCE(latest_episode.created_at, latest_series.created_at)) AS activity_at
			FROM items latest_series
			LEFT JOIN LATERAL (
				SELECT episode.created_at
				FROM items episode
				WHERE episode.type = 'Episode'
				  AND episode.series_id = latest_series.id
				ORDER BY episode.created_at DESC, episode.id DESC
				LIMIT 1
			) latest_episode ON TRUE
			WHERE latest_series.type = 'Series'
			  AND latest_series.merged_to_id IS NULL%s
		) latest_candidate
		ORDER BY latest_candidate.activity_at DESC, latest_candidate.id DESC
		LIMIT $%d::bigint`, movieScope, seriesScope, limitParam)
}
