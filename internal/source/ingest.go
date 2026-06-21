package source

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"fyms/internal/repository"
)

type SourceIngestor struct {
	repo       *repository.SourceRepository
	siteKey    string
	providerID int64
}

func NewSourceIngestor(repo *repository.SourceRepository, siteKey string, providerID int64) (*SourceIngestor, error) {
	siteKey = strings.TrimSpace(siteKey)
	if repo == nil {
		return nil, fmt.Errorf("source ingestor 缺少 repository")
	}
	if siteKey == "" {
		return nil, fmt.Errorf("source ingestor 缺少 site key")
	}
	if providerID <= 0 {
		return nil, fmt.Errorf("source ingestor 缺少 provider id")
	}
	return &SourceIngestor{repo: repo, siteKey: siteKey, providerID: providerID}, nil
}

func (i *SourceIngestor) IngestPage(ctx context.Context, page *ProviderPage) ([]repository.SourceItem, error) {
	if page == nil {
		return nil, fmt.Errorf("source page 为空")
	}
	items := make([]repository.SourceItem, 0, len(page.Items))
	for _, snapshot := range page.Items {
		item, err := i.IngestItem(ctx, snapshot)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, nil
}

func (i *SourceIngestor) IngestDetail(ctx context.Context, detail *ProviderDetail) (*repository.SourceItem, []repository.SourcePlaySource, error) {
	if detail == nil {
		return nil, nil, fmt.Errorf("source detail 为空")
	}
	detail.Item.DetailLoaded = true
	item, err := i.IngestItem(ctx, detail.Item)
	if err != nil {
		return nil, nil, err
	}
	playSources := make([]repository.SourcePlaySource, 0, len(detail.PlaySources))
	for _, snapshot := range detail.PlaySources {
		playSource, err := i.IngestPlaySource(ctx, *item, snapshot)
		if err != nil {
			return nil, nil, err
		}
		playSources = append(playSources, *playSource)
	}
	return item, playSources, nil
}

func (i *SourceIngestor) IngestItem(ctx context.Context, snapshot SourceItemSnapshot) (*repository.SourceItem, error) {
	sourceItemID := strings.TrimSpace(snapshot.SourceItemID)
	if sourceItemID == "" {
		return nil, fmt.Errorf("source item 缺少来源 ID")
	}
	if strings.TrimSpace(snapshot.Title) == "" {
		return nil, fmt.Errorf("source item 缺少标题: %s", sourceItemID)
	}
	publicUUID := SourceItemPublicUUID(i.siteKey, sourceItemID)
	if !snapshot.DetailLoaded {
		existing, err := i.repo.GetSourceItemByPublicUUID(ctx, publicUUID)
		if err != nil {
			return nil, err
		}
		if existing != nil && existing.DetailLoaded {
			snapshot = mergeSourceItemSnapshot(snapshot, *existing)
		}
	}
	return i.repo.UpsertSourceItem(ctx, repository.SourceItemUpsert{
		PublicUUID:     publicUUID,
		ProviderID:     i.providerID,
		SourceItemID:   sourceItemID,
		SourceParentID: snapshot.SourceParentID,
		ItemType:       defaultSnapshotString(snapshot.ItemType, "unknown"),
		Title:          strings.TrimSpace(snapshot.Title),
		OriginalTitle:  snapshot.OriginalTitle,
		SortTitle:      snapshot.SortTitle,
		Year:           snapshot.Year,
		Region:         snapshot.Region,
		Area:           snapshot.Area,
		Language:       snapshot.Language,
		CategoryName:   snapshot.CategoryName,
		NormalizedKind: defaultSnapshotString(snapshot.NormalizedKind, "unknown"),
		SeasonNumber:   snapshot.SeasonNumber,
		EpisodeNumber:  snapshot.EpisodeNumber,
		PosterURL:      snapshot.PosterURL,
		BackdropURL:    snapshot.BackdropURL,
		Remarks:        snapshot.Remarks,
		Summary:        snapshot.Summary,
		Directors:      snapshot.Directors,
		Actors:         snapshot.Actors,
		ProviderIDs:    jsonObjectBytes(snapshot.ProviderIDs),
		Raw:            jsonObjectBytes(snapshot.Raw),
		DetailLoaded:   snapshot.DetailLoaded,
	})
}

func (i *SourceIngestor) IngestPlaySource(ctx context.Context, item repository.SourceItem, snapshot PlaySourceSnapshot) (*repository.SourcePlaySource, error) {
	if item.ID <= 0 || strings.TrimSpace(item.PublicUUID) == "" {
		return nil, fmt.Errorf("source play source 缺少有效 source item")
	}
	rawURL := strings.TrimSpace(snapshot.RawURL)
	if rawURL == "" {
		return nil, fmt.Errorf("source play source 缺少播放地址: %s", item.SourceItemID)
	}
	lineName := defaultSnapshotString(snapshot.LineName, "默认线路")
	episodeKey := defaultSnapshotString(snapshot.EpisodeKey, "default")
	episodeTitle := defaultSnapshotString(snapshot.EpisodeTitle, episodeKey)
	return i.repo.UpsertPlaySource(ctx, repository.SourcePlaySourceUpsert{
		PublicUUID:      PlaySourcePublicUUID(item.PublicUUID, lineName, episodeKey),
		SourceItemID:    item.ID,
		ProviderID:      i.providerID,
		LineName:        lineName,
		EpisodeTitle:    episodeTitle,
		EpisodeKey:      episodeKey,
		EpisodeNumber:   snapshot.EpisodeNumber,
		RawURL:          rawURL,
		ParseMode:       defaultSnapshotString(snapshot.ParseMode, "unknown"),
		Flag:            snapshot.Flag,
		Headers:         jsonObjectBytes(snapshot.Headers),
		ResolverPayload: jsonObjectBytes(snapshot.ResolverPayload),
		SortOrder:       snapshot.SortOrder,
	})
}

func (p *CMSProvider) SearchAndIngest(ctx context.Context, ingestor *SourceIngestor, req SearchRequest) (*ProviderPage, []repository.SourceItem, error) {
	page, err := p.Search(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	items, err := ingestor.IngestPage(ctx, page)
	if err != nil {
		return nil, nil, err
	}
	return page, items, nil
}

func (p *CMSProvider) CategoryAndIngest(ctx context.Context, ingestor *SourceIngestor, req CategoryRequest) (*ProviderPage, []repository.SourceItem, error) {
	page, err := p.Category(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	items, err := ingestor.IngestPage(ctx, page)
	if err != nil {
		return nil, nil, err
	}
	return page, items, nil
}

func (p *CMSProvider) DetailAndIngest(ctx context.Context, ingestor *SourceIngestor, sourceItemID string) (*ProviderDetail, *repository.SourceItem, []repository.SourcePlaySource, error) {
	detail, err := p.Detail(ctx, sourceItemID)
	if err != nil {
		return nil, nil, nil, err
	}
	item, playSources, err := ingestor.IngestDetail(ctx, detail)
	if err != nil {
		return nil, nil, nil, err
	}
	return detail, item, playSources, nil
}

func mergeSourceItemSnapshot(snapshot SourceItemSnapshot, existing repository.SourceItem) SourceItemSnapshot {
	snapshot.DetailLoaded = true
	snapshot.OriginalTitle = preferStringPtr(snapshot.OriginalTitle, existing.OriginalTitle)
	snapshot.SortTitle = preferStringPtr(snapshot.SortTitle, existing.SortTitle)
	snapshot.Year = preferInt32Ptr(snapshot.Year, existing.Year)
	snapshot.Region = preferStringPtr(snapshot.Region, existing.Region)
	snapshot.Area = preferStringPtr(snapshot.Area, existing.Area)
	snapshot.Language = preferStringPtr(snapshot.Language, existing.Language)
	snapshot.CategoryName = preferStringPtr(snapshot.CategoryName, existing.CategoryName)
	snapshot.NormalizedKind = defaultSnapshotString(snapshot.NormalizedKind, existing.NormalizedKind)
	snapshot.SeasonNumber = preferInt32Ptr(snapshot.SeasonNumber, existing.SeasonNumber)
	snapshot.EpisodeNumber = preferInt32Ptr(snapshot.EpisodeNumber, existing.EpisodeNumber)
	snapshot.PosterURL = preferStringPtr(snapshot.PosterURL, existing.PosterURL)
	snapshot.BackdropURL = preferStringPtr(snapshot.BackdropURL, existing.BackdropURL)
	snapshot.Remarks = preferStringPtr(snapshot.Remarks, existing.Remarks)
	snapshot.Summary = preferStringPtr(snapshot.Summary, existing.Summary)
	if len(snapshot.Directors) == 0 {
		snapshot.Directors = existing.Directors
	}
	if len(snapshot.Actors) == 0 {
		snapshot.Actors = existing.Actors
	}
	snapshot.ProviderIDs = mergeJSONObjects(existing.ProviderIDs, snapshot.ProviderIDs)
	snapshot.Raw = mergeJSONObjects(existing.Raw, snapshot.Raw)
	return snapshot
}

func defaultSnapshotString(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func preferStringPtr(value, fallback *string) *string {
	if value != nil && strings.TrimSpace(*value) != "" {
		return value
	}
	return fallback
}

func preferInt32Ptr(value, fallback *int32) *int32 {
	if value != nil {
		return value
	}
	return fallback
}

func jsonObjectBytes(value map[string]any) []byte {
	if len(value) == 0 {
		return []byte("{}")
	}
	raw, err := json.Marshal(value)
	if err != nil || !json.Valid(raw) {
		return []byte("{}")
	}
	return raw
}

func mergeJSONObjects(raw []byte, overlay map[string]any) map[string]any {
	out := map[string]any{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out)
	}
	for key, value := range overlay {
		key = strings.TrimSpace(key)
		if key != "" {
			out[key] = value
		}
	}
	return out
}
