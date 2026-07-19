package models

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/dto"
	"fyms/internal/repository"
)

func libraryRepresentativeCountExpr(itemAlias string) string {
	return fmt.Sprintf(
		"COUNT(DISTINCT CASE WHEN %s.type = 'Movie' THEN COALESCE(%s.merged_to_id::text, %s.id::text) ELSE %s.id::text END)",
		itemAlias, itemAlias, itemAlias, itemAlias,
	)
}

func QueryItems(ctx context.Context, pool *pgxpool.Pool, options *ItemQueryOptions) (*ItemQueryResult, error) {
	result, err := repository.NewItemQueryRepository(pool).QueryItems(ctx, itemQueryOptionsToRepo(options))
	if err != nil {
		return nil, err
	}
	items := make([]dto.ItemRow, 0, len(result.Rows))
	userData := make([]dto.UserDataRow, 0, len(result.Rows))
	for _, row := range result.Rows {
		items = append(items, MapColsToItemRow(row))
		userData = append(userData, MapColsToUserDataRow(row))
	}
	return &ItemQueryResult{
		Items:      items,
		UserData:   userData,
		TotalCount: result.TotalCount,
	}, nil
}

func itemQueryOptionsToRepo(options *ItemQueryOptions) *repository.ItemQueryOptions {
	if options == nil {
		return nil
	}
	matches := make([]repository.ItemProviderIDMatch, 0, len(options.AnyProviderID))
	for _, p := range options.AnyProviderID {
		matches = append(matches, repository.ItemProviderIDMatch{
			Provider: p.Provider,
			ID:       p.ID,
		})
	}
	return &repository.ItemQueryOptions{
		ParentID:          options.ParentID,
		ParentIDs:         options.ParentIDs,
		ParentLibraryID:   options.ParentLibraryID,
		RecursiveParentID: options.RecursiveParentID,
		IncludeItemTypes:  options.IncludeItemTypes,
		SortBy:            options.SortBy,
		SortOrder:         options.SortOrder,
		Limit:             options.Limit,
		StartIndex:        options.StartIndex,
		Recursive:         options.Recursive,
		LibraryID:         options.LibraryID,
		SearchTerm:        options.SearchTerm,
		NameStartsWith:    options.NameStartsWith,
		Filters:           options.Filters,
		UserID:            options.UserID,
		GenreIDs:          options.GenreIDs,
		GenreNames:        options.GenreNames,
		TagIDs:            options.TagIDs,
		TagNames:          options.TagNames,
		PersonIDs:         options.PersonIDs,
		PersonNames:       options.PersonNames,
		PersonTypes:       options.PersonTypes,
		Years:             options.Years,
		Studio:            options.Studio,
		ActorName:         options.ActorName,
		CatalogPrefix:     options.CatalogPrefix,
		LatestItemLimit:   options.LatestItemLimit,
		AnyProviderID:     matches,
		HasSubtitles:      options.HasSubtitles,
		AllowedLibraryIDs: options.AllowedLibraryIDs,
		LightMode:         options.LightMode,
	}
}
