package services

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/models"
	"fyms/internal/services/scraper"
)

func mergedToNfoData(merged *scraper.MergedDetails, tmdbID int64, studio *string) NfoData {
	var title *string
	if s := strings.TrimSpace(merged.Title); s != "" {
		title = &s
	}
	var originalTitle *string
	if s := strings.TrimSpace(merged.OriginalTitle); s != "" {
		originalTitle = &s
	}
	var plot *string
	if s := strings.TrimSpace(merged.Overview); s != "" {
		plot = &s
	}
	var premiered *string
	if s := strings.TrimSpace(merged.Premiered); s != "" {
		premiered = &s
	}
	var tagline *string
	if s := strings.TrimSpace(merged.Tagline); s != "" {
		tagline = &s
	}
	var imdbID *string
	var tvdbID *int32
	if merged.ExternalIDs != nil {
		if s := strings.TrimSpace(merged.ExternalIDs["imdb"]); s != "" {
			imdbID = &s
		}
		if s := strings.TrimSpace(merged.ExternalIDs["tvdb"]); s != "" {
			if v, err := strconv.ParseInt(s, 10, 32); err == nil && v > 0 {
				tv := int32(v)
				tvdbID = &tv
			}
		}
	}
	var tmdbIDi32 *int32
	if tmdbID > 0 {
		v := int32(tmdbID)
		tmdbIDi32 = &v
	}
	var rating *float64
	if merged.Rating != nil {
		v := *merged.Rating
		rating = &v
	}
	actors := make([]NfoActor, 0, len(merged.Actors))
	for _, actor := range merged.Actors {
		actors = append(actors, NfoActor{
			Name:     actor.Name,
			Role:     actor.Role,
			TmdbID:   actor.TmdbID,
			ImageURL: actor.ImageURL,
		})
	}
	return NfoData{
		Title:         title,
		OriginalTitle: originalTitle,
		Plot:          plot,
		Year:          merged.Year,
		Rating:        rating,
		TmdbID:        tmdbIDi32,
		ImdbID:        imdbID,
		TvdbID:        tvdbID,
		Genres:        append([]string(nil), merged.Genres...),
		Actors:        actors,
		Directors:     append([]string(nil), merged.Directors...),
		Premiered:     premiered,
		Tagline:       tagline,
		Studio:        studio,
	}
}

func applyMergedDetails(ctx context.Context, pool *pgxpool.Pool, itemID string, client *TmdbClient, itemType string, tmdbID int64, merged *scraper.MergedDetails, updateTMDBID bool, source models.PlatformScanSource) (map[string]interface{}, error) {
	if merged == nil {
		return nil, fmt.Errorf("merged details is nil")
	}
	saveMode := getScrapeSaveMode(ctx, pool)
	var studio *string
	if len(merged.Platforms) > 0 {
		candidate := strings.TrimSpace(merged.Platforms[0])
		if candidate != "" {
			studio = &candidate
		}
	}
	nfo := mergedToNfoData(merged, tmdbID, studio)
	ApplyNfoDataWithType(ctx, pool, itemID, itemType, &nfo, source)

	var externalIDs []externalIDRecord
	for provider, value := range merged.ExternalIDs {
		externalIDs = append(externalIDs, externalIDRecord{Provider: provider, Value: value})
	}
	if len(externalIDs) == 0 && tmdbID > 0 {
		externalIDs = append(externalIDs, externalIDRecord{Provider: "tmdb", Value: strconv.FormatInt(tmdbID, 10)})
	}
	upsertExternalIDs(ctx, pool, itemID, externalIDs)

	if studio != nil {
		if err := models.MarkPlatformScanMatched(ctx, pool, itemID, *studio, source); err != nil {
			return nil, err
		}
		if itemType == "Series" {
			if err := models.PropagateStudioToChildren(ctx, pool, itemID, *studio); err != nil {
				return nil, err
			}
		}
	} else {
		if err := models.MarkPlatformScanNoMatch(ctx, pool, itemID, source, "no platform matched from merged details"); err != nil {
			return nil, err
		}
	}

	targets := resolveScrapeSaveTargets(ctx, pool, itemID, itemType)
	saveToData := saveMode == "database" || saveMode == "both"
	saveToMedia := saveMode == "media_dir" || saveMode == "both"

	if saveToMedia && targets.NfoPath != "" {
		if ok := writeNfoFile(targets.NfoPath, itemType, &nfo); !ok {
			slog.Warn("[Scraper] Failed to write NFO to media directory", "item_id", itemID, "path", targets.NfoPath)
		}
	}

	if updateTMDBID && tmdbID > 0 {
		_, err := pool.Exec(ctx,
			"UPDATE items SET tmdb_id = $1, imdb_id = COALESCE(NULLIF($2, ''), imdb_id), updated_at = NOW() WHERE id = $3::uuid",
			int32(tmdbID), derefStr(nfo.ImdbID), itemID)
		if err != nil {
			return nil, fmt.Errorf("update ids: %w", err)
		}
	}

	if len(merged.PosterURLs) > 0 {
		posterURL := merged.PosterURLs[0]
		var dbPosterPath string
		var dbPosterTag *string
		mediaSaved := false
		if saveToMedia && targets.PosterPath != "" {
			if client.downloadImageURL(ctx, posterURL, targets.PosterPath) {
				dbPosterPath = targets.PosterPath
				dbPosterTag = GenerateImageTag(targets.PosterPath)
				mediaSaved = true
			}
		}
		if saveToData || (saveToMedia && !mediaSaved) {
			dataPath := fmt.Sprintf("data/metadata/%s/poster.jpg", itemID)
			if client.downloadImageURL(ctx, posterURL, dataPath) && dbPosterPath == "" {
				dbPosterPath = dataPath
				dbPosterTag = GenerateImageTag(dataPath)
			}
		}
		if dbPosterPath != "" {
			_, _ = pool.Exec(ctx,
				"UPDATE items SET primary_image_path = $1, primary_image_tag = $2, updated_at = NOW() WHERE id = $3::uuid",
				dbPosterPath, dbPosterTag, itemID)
		}
	}

	if len(merged.BackdropURLs) > 0 {
		backdropURL := merged.BackdropURLs[0]
		var dbBackdropPath string
		var dbBackdropTag *string
		mediaSaved := false
		if saveToMedia && targets.BackdropPath != "" {
			if client.downloadImageURL(ctx, backdropURL, targets.BackdropPath) {
				dbBackdropPath = targets.BackdropPath
				dbBackdropTag = GenerateImageTag(targets.BackdropPath)
				mediaSaved = true
			}
		}
		if saveToData || (saveToMedia && !mediaSaved) {
			dataPath := fmt.Sprintf("data/metadata/%s/backdrop.jpg", itemID)
			if client.downloadImageURL(ctx, backdropURL, dataPath) && dbBackdropPath == "" {
				dbBackdropPath = dataPath
				dbBackdropTag = GenerateImageTag(dataPath)
			}
		}
		if dbBackdropPath != "" {
			_, _ = pool.Exec(ctx,
				"UPDATE items SET backdrop_image_path = $1, backdrop_image_tag = $2, updated_at = NOW() WHERE id = $3::uuid",
				dbBackdropPath, dbBackdropTag, itemID)
		}
	}

	if itemType == "Series" && tmdbID > 0 {
		scrapeSeasonPosters(ctx, pool, client, itemID, tmdbID, saveMode)
		scrapeEpisodeMetadata(ctx, pool, client, itemID, tmdbID)
	}

	return map[string]interface{}{
		"success": true,
		"tmdb_id": tmdbID,
		"name":    nfo.Title,
	}, nil
}

func applyTMDBDetails(ctx context.Context, pool *pgxpool.Pool, itemID string, client *TmdbClient, itemType string, itemName string, tmdbID int64, details map[string]interface{}, updateTMDBID bool, source models.PlatformScanSource) (map[string]interface{}, error) {
	saveMode := getScrapeSaveMode(ctx, pool)

	// Extract overview with fallback chain: primary language -> en-US -> Douban
	overview := jsonStringNonEmpty(details, "overview")
	if overview == nil && client.language != "en-US" {
		enClient := client.cloneWithLanguage("en-US")
		var enDetails map[string]interface{}
		if itemType == "Movie" {
			enDetails, _ = enClient.GetMovieDetails(ctx, tmdbID)
		} else {
			enDetails, _ = enClient.GetTVDetails(ctx, tmdbID)
		}
		if enDetails != nil {
			overview = jsonStringNonEmpty(enDetails, "overview")
		}
	}
	if overview == nil {
		fallbackName := itemName
		if title := jsonStringPtr(details, map[string]string{"Movie": "title", "Series": "name"}[itemType]); title != nil && strings.TrimSpace(*title) != "" {
			fallbackName = *title
		}
		overview = fetchDoubanOverview(client.httpClient, fallbackName)
	}

	rating := jsonFloat64(details, "vote_average")

	// Genres
	var genres []string
	if genreArr, ok := details["genres"].([]interface{}); ok {
		for _, g := range genreArr {
			if gm, ok := g.(map[string]interface{}); ok {
				if n, ok := gm["name"].(string); ok && n != "" {
					genres = append(genres, n)
				}
			}
		}
	}

	// Actors (up to 20)
	var actors []NfoActor
	if credits, ok := details["credits"].(map[string]interface{}); ok {
		if castArr, ok := credits["cast"].([]interface{}); ok {
			limit := 20
			if len(castArr) < limit {
				limit = len(castArr)
			}
			for _, c := range castArr[:limit] {
				cm, ok := c.(map[string]interface{})
				if !ok {
					continue
				}
				aName, ok := cm["name"].(string)
				if !ok || aName == "" {
					continue
				}
				role, _ := cm["character"].(string)
				var tmdbActorID *int32
				if aid, ok := jsonInt64(cm, "id"); ok {
					v := int32(aid)
					tmdbActorID = &v
				}
				var imageURL *string
				if pp, ok := cm["profile_path"].(string); ok && pp != "" {
					u := fmt.Sprintf("%s/w185%s", TMDB_IMAGE_BASE, pp)
					imageURL = &u
				}
				actors = append(actors, NfoActor{
					Name:     aName,
					Role:     role,
					TmdbID:   tmdbActorID,
					ImageURL: imageURL,
				})
			}
		}
	}

	// Directors
	var directors []string
	if credits, ok := details["credits"].(map[string]interface{}); ok {
		if crewArr, ok := credits["crew"].([]interface{}); ok {
			for _, c := range crewArr {
				cm, ok := c.(map[string]interface{})
				if !ok {
					continue
				}
				if job, _ := cm["job"].(string); job == "Director" {
					if dn, ok := cm["name"].(string); ok && dn != "" {
						directors = append(directors, dn)
					}
				}
			}
		}
	}

	// Extract platform/studio from networks (TV) or production_companies (Movie)
	studio := ExtractPlatform(details, itemType)

	// Build title key based on type
	titleKey := "title"
	dateKey := "release_date"
	if itemType != "Movie" {
		titleKey = "name"
		dateKey = "first_air_date"
	}

	title := jsonStringPtr(details, titleKey)
	premiered := jsonStringPtr(details, dateKey)

	var nfoYear *int32
	if premiered != nil && len(*premiered) >= 4 {
		if y := parseYearPrefix(*premiered); y > 0 {
			v := int32(y)
			nfoYear = &v
		}
	}

	var tmdbIDi32 *int32
	{
		v := int32(tmdbID)
		tmdbIDi32 = &v
	}

	nfo := NfoData{
		Title:     title,
		Plot:      overview,
		Year:      nfoYear,
		Rating:    rating,
		TmdbID:    tmdbIDi32,
		Genres:    genres,
		Actors:    actors,
		Directors: directors,
		Premiered: premiered,
		Studio:    studio,
	}

	ApplyNfoDataWithPlatformSource(ctx, pool, itemID, &nfo, source)

	var externalIDs []externalIDRecord
	externalIDs = append(externalIDs, externalIDRecord{Provider: "tmdb", Value: strconv.FormatInt(tmdbID, 10)})
	if imdbID := jsonStringNonEmpty(details, "imdb_id"); imdbID != nil {
		externalIDs = append(externalIDs, externalIDRecord{Provider: "imdb", Value: *imdbID})
	}
	upsertExternalIDs(ctx, pool, itemID, externalIDs)

	// Set studio and propagate to children for Series
	if studio != nil {
		if err := models.MarkPlatformScanMatched(ctx, pool, itemID, *studio, source); err != nil {
			return nil, err
		}
		if itemType == "Series" {
			if err := models.PropagateStudioToChildren(ctx, pool, itemID, *studio); err != nil {
				return nil, err
			}
		}
	} else {
		if err := models.MarkPlatformScanNoMatch(ctx, pool, itemID, source, "no platform matched from TMDB details"); err != nil {
			return nil, err
		}
	}

	targets := resolveScrapeSaveTargets(ctx, pool, itemID, itemType)
	saveToData := saveMode == "database" || saveMode == "both"
	saveToMedia := saveMode == "media_dir" || saveMode == "both"

	if saveToMedia && targets.NfoPath != "" {
		if ok := writeNfoFile(targets.NfoPath, itemType, &nfo); !ok {
			slog.Warn("[TMDB] Failed to write NFO to media directory", "item_id", itemID, "path", targets.NfoPath)
		}
	}

	if updateTMDBID {
		_, err := pool.Exec(ctx,
			"UPDATE items SET tmdb_id = $1, updated_at = NOW() WHERE id = $2::uuid",
			int32(tmdbID), itemID)
		if err != nil {
			return nil, fmt.Errorf("update tmdb_id: %w", err)
		}
	}

	// Download poster
	if posterPath, ok := details["poster_path"].(string); ok && posterPath != "" {
		var dbPosterPath string
		var dbPosterTag *string

		mediaSaved := false
		if saveToMedia && targets.PosterPath != "" {
			if client.DownloadImage(ctx, posterPath, targets.PosterPath, "w500") {
				dbPosterPath = targets.PosterPath
				dbPosterTag = GenerateImageTag(targets.PosterPath)
				mediaSaved = true
			} else {
				slog.Warn("[TMDB] Failed to save poster to media directory, falling back to data/metadata/",
					"item_id", itemID, "path", targets.PosterPath)
			}
		}
		if saveToData || (saveToMedia && !mediaSaved) {
			dataPath := fmt.Sprintf("data/metadata/%s/poster.jpg", itemID)
			if client.DownloadImage(ctx, posterPath, dataPath, "w500") && dbPosterPath == "" {
				dbPosterPath = dataPath
				dbPosterTag = GenerateImageTag(dataPath)
			}
		}
		if dbPosterPath != "" {
			_, _ = pool.Exec(ctx,
				"UPDATE items SET primary_image_path = $1, primary_image_tag = $2, updated_at = NOW() WHERE id = $3::uuid",
				dbPosterPath, dbPosterTag, itemID)
		}
	}

	// Download backdrop
	if backdropPath, ok := details["backdrop_path"].(string); ok && backdropPath != "" {
		var dbBackdropPath string
		var dbBackdropTag *string

		mediaSaved := false
		if saveToMedia && targets.BackdropPath != "" {
			if client.DownloadImage(ctx, backdropPath, targets.BackdropPath, "w1280") {
				dbBackdropPath = targets.BackdropPath
				dbBackdropTag = GenerateImageTag(targets.BackdropPath)
				mediaSaved = true
			} else {
				slog.Warn("[TMDB] Failed to save backdrop to media directory, falling back to data/metadata/",
					"item_id", itemID, "path", targets.BackdropPath)
			}
		}
		if saveToData || (saveToMedia && !mediaSaved) {
			dataPath := fmt.Sprintf("data/metadata/%s/backdrop.jpg", itemID)
			if client.DownloadImage(ctx, backdropPath, dataPath, "w1280") && dbBackdropPath == "" {
				dbBackdropPath = dataPath
				dbBackdropTag = GenerateImageTag(dataPath)
			}
		}
		if dbBackdropPath != "" {
			_, _ = pool.Exec(ctx,
				"UPDATE items SET backdrop_image_path = $1, backdrop_image_tag = $2, updated_at = NOW() WHERE id = $3::uuid",
				dbBackdropPath, dbBackdropTag, itemID)
		}
	}

	// Scrape season posters for Series
	if itemType == "Series" {
		scrapeSeasonPosters(ctx, pool, client, itemID, tmdbID, saveMode)
	}

	return map[string]interface{}{
		"success": true,
		"tmdb_id": tmdbID,
		"name":    nfo.Title,
	}, nil
}

func scrapeSeasonPosters(ctx context.Context, pool *pgxpool.Pool, client *TmdbClient, seriesID string, tmdbID int64, saveMode string) {
	rows, err := pool.Query(ctx,
		"SELECT id, index_number FROM items WHERE parent_id = $1::uuid AND type = 'Season' ORDER BY index_number",
		seriesID)
	if err != nil {
		return
	}
	defer rows.Close()

	type seasonRow struct {
		id       uuid.UUID
		indexNum *int32
	}
	var seasons []seasonRow
	for rows.Next() {
		var s seasonRow
		if err := rows.Scan(&s.id, &s.indexNum); err != nil {
			continue
		}
		seasons = append(seasons, s)
	}
	rows.Close()

	for _, s := range seasons {
		num := int32(1)
		if s.indexNum != nil {
			num = *s.indexNum
		}

		var existingTag *string
		_ = pool.QueryRow(ctx,
			"SELECT primary_image_tag FROM items WHERE id = $1",
			s.id).Scan(&existingTag)
		if existingTag != nil {
			continue
		}

		posterPath := client.GetSeasonImages(ctx, tmdbID, num)
		if posterPath == nil {
			continue
		}

		sid := s.id.String()
		saveToData := saveMode == "database" || saveMode == "both"
		saveToMedia := saveMode == "media_dir" || saveMode == "both"

		var dbPosterPath string
		var dbPosterTag *string

		mediaSaved := false
		if saveToMedia {
			mediaPath := resolveSeasonPosterMediaPath(ctx, pool, sid)
			if mediaPath != "" {
				if client.DownloadImage(ctx, *posterPath, mediaPath, "w500") {
					dbPosterPath = mediaPath
					dbPosterTag = GenerateImageTag(mediaPath)
					mediaSaved = true
				} else {
					slog.Warn("[TMDB] Failed to save season poster to media directory", "season_id", sid, "path", mediaPath)
				}
			}
		}
		if saveToData || (saveToMedia && !mediaSaved) {
			dataPath := fmt.Sprintf("data/metadata/%s/poster.jpg", sid)
			if client.DownloadImage(ctx, *posterPath, dataPath, "w500") && dbPosterPath == "" {
				dbPosterPath = dataPath
				dbPosterTag = GenerateImageTag(dataPath)
			}
		}

		if dbPosterPath != "" {
			_, _ = pool.Exec(ctx,
				"UPDATE items SET primary_image_path = $1, primary_image_tag = $2, updated_at = NOW() WHERE id = $3",
				dbPosterPath, dbPosterTag, s.id)
		}

		time.Sleep(200 * time.Millisecond)
	}
}
