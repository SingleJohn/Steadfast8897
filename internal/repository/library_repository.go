package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/db/gen"
)

type LibraryRepository struct {
	queries *dbgen.Queries
}

type WatchLibrary struct {
	ID    uuid.UUID
	Name  string
	Paths []string
}

func NewLibraryRepository(pool *pgxpool.Pool) *LibraryRepository {
	return &LibraryRepository{queries: dbgen.New(pool)}
}

func (r *LibraryRepository) ListLibraries(ctx context.Context) ([]Library, error) {
	rows, err := r.queries.ListLibraries(ctx)
	if err != nil {
		return nil, err
	}
	libs := make([]Library, 0, len(rows))
	for _, row := range rows {
		libs = append(libs, mapLibrary(row.ID, row.Name, row.CollectionType, row.Paths, row.CreatedAt, row.PrimaryImagePath, row.PrimaryImageTag, row.SortOrder, row.ScrapeConfig))
	}
	return libs, nil
}

func (r *LibraryRepository) ListLibrariesForWatcher(ctx context.Context) ([]WatchLibrary, error) {
	rows, err := r.queries.ListLibrariesForWatcher(ctx)
	if err != nil {
		return nil, err
	}
	libs := make([]WatchLibrary, 0, len(rows))
	for _, row := range rows {
		libs = append(libs, WatchLibrary{
			ID:    fromPGUUID(row.ID),
			Name:  row.Name,
			Paths: row.Paths,
		})
	}
	return libs, nil
}

func (r *LibraryRepository) GetLibraryByID(ctx context.Context, id uuid.UUID) (*Library, error) {
	row, err := r.queries.GetLibraryByID(ctx, toPGUUID(id))
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	lib := mapLibrary(row.ID, row.Name, row.CollectionType, row.Paths, row.CreatedAt, row.PrimaryImagePath, row.PrimaryImageTag, row.SortOrder, row.ScrapeConfig)
	return &lib, nil
}

func (r *LibraryRepository) CreateLibrary(ctx context.Context, name, collectionType string, paths []string) (*Library, error) {
	row, err := r.queries.CreateLibrary(ctx, dbgen.CreateLibraryParams{
		Name:           name,
		CollectionType: collectionType,
		Paths:          paths,
	})
	if err != nil {
		return nil, err
	}
	lib := mapLibrary(row.ID, row.Name, row.CollectionType, row.Paths, row.CreatedAt, row.PrimaryImagePath, row.PrimaryImageTag, row.SortOrder, row.ScrapeConfig)
	return &lib, nil
}

func (r *LibraryRepository) UpdateLibrary(ctx context.Context, id uuid.UUID, name *string) (*Library, error) {
	if name != nil {
		if err := r.queries.UpdateLibraryName(ctx, dbgen.UpdateLibraryNameParams{Name: *name, ID: toPGUUID(id)}); err != nil {
			return nil, err
		}
	}
	return r.GetLibraryByID(ctx, id)
}

func (r *LibraryRepository) UpdateLibrarySortOrder(ctx context.Context, id uuid.UUID, sortOrder int) error {
	return r.queries.UpdateLibrarySortOrder(ctx, dbgen.UpdateLibrarySortOrderParams{
		SortOrder: int32(sortOrder),
		ID:        toPGUUID(id),
	})
}

func (r *LibraryRepository) UpdateLibraryScrapeConfig(ctx context.Context, id uuid.UUID, rawJSON *string) error {
	if rawJSON == nil {
		return r.queries.UpdateLibraryScrapeConfigNull(ctx, toPGUUID(id))
	}
	return r.queries.UpdateLibraryScrapeConfig(ctx, dbgen.UpdateLibraryScrapeConfigParams{
		Column1: []byte(*rawJSON),
		ID:      toPGUUID(id),
	})
}

func (r *LibraryRepository) MarkLibraryDeleted(ctx context.Context, id uuid.UUID) (bool, error) {
	n, err := r.queries.MarkLibraryDeleted(ctx, toPGUUID(id))
	return n > 0, err
}

func (r *LibraryRepository) CountLibraryItems(ctx context.Context, id uuid.UUID) (int64, error) {
	return r.queries.CountLibraryItems(ctx, toPGUUID(id))
}

func (r *LibraryRepository) FinalizeLibraryDeletion(ctx context.Context, id uuid.UUID) error {
	return r.queries.FinalizeLibraryDeletion(ctx, toPGUUID(id))
}

func (r *LibraryRepository) ListDeletedLibraryIDs(ctx context.Context) ([]uuid.UUID, error) {
	rows, err := r.queries.ListDeletedLibraryIDs(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, fromPGUUID(row))
	}
	return ids, nil
}

func (r *LibraryRepository) GetLibraryNameIncludingDeleted(ctx context.Context, id uuid.UUID) (string, error) {
	name, err := r.queries.GetLibraryNameIncludingDeleted(ctx, toPGUUID(id))
	if err == pgx.ErrNoRows {
		return "", nil
	}
	return name, err
}

func (r *LibraryRepository) AddLibraryPath(ctx context.Context, id uuid.UUID, path string) error {
	return r.queries.AddLibraryPath(ctx, dbgen.AddLibraryPathParams{Column1: path, ID: toPGUUID(id)})
}

func (r *LibraryRepository) UpdateLibraryImage(ctx context.Context, id uuid.UUID, imagePath, imageTag string) error {
	return r.queries.UpdateLibraryImage(ctx, dbgen.UpdateLibraryImageParams{
		PrimaryImagePath: textValue(imagePath),
		PrimaryImageTag:  textValue(imageTag),
		ID:               toPGUUID(id),
	})
}

func (r *LibraryRepository) DeleteLibraryImage(ctx context.Context, id uuid.UUID) error {
	return r.queries.DeleteLibraryImage(ctx, toPGUUID(id))
}

func (r *LibraryRepository) RemoveLibraryPath(ctx context.Context, id uuid.UUID, path string) error {
	return r.queries.RemoveLibraryPath(ctx, dbgen.RemoveLibraryPathParams{Column1: path, ID: toPGUUID(id)})
}

func mapLibrary(id pgtype.UUID, name, collectionType string, paths []string, createdAt pgtype.Timestamp, primaryImagePath, primaryImageTag pgtype.Text, sortOrder int32, scrapeConfig any) Library {
	return Library{
		ID:               fromPGUUID(id),
		Name:             name,
		CollectionType:   collectionType,
		Paths:            paths,
		CreatedAt:        createdAt.Time,
		PrimaryImagePath: ptrText(primaryImagePath),
		PrimaryImageTag:  ptrText(primaryImageTag),
		SortOrder:        int(sortOrder),
		ScrapeConfig:     optionalScrapeConfig(scrapeConfig),
	}
}
