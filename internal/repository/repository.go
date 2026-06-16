package repository

import "github.com/jackc/pgx/v5/pgxpool"

type Repository struct {
	SystemConfig *SystemConfigRepository
	Users        *UserRepository
	Sessions     *SessionRepository
	Libraries    *LibraryRepository
	DisplayOrder *DisplayOrderRepository
	ScrapeQueue  *ScrapeQueueRepository
	RefreshQueue *RefreshQueueRepository
	TaskRuns     *TaskRunRepository
	ScanIngest   *ScanIngestRepository
	ItemHelpers  *ItemHelperRepository
	APIKeys      *APIKeyRepository
	ImageLookup  *ImageLookupRepository
}

func New(pool *pgxpool.Pool) *Repository {
	return &Repository{
		SystemConfig: NewSystemConfigRepository(pool),
		Users:        NewUserRepository(pool),
		Sessions:     NewSessionRepository(pool),
		Libraries:    NewLibraryRepository(pool),
		DisplayOrder: NewDisplayOrderRepository(pool),
		ScrapeQueue:  NewScrapeQueueRepository(pool),
		RefreshQueue: NewRefreshQueueRepository(pool),
		TaskRuns:     NewTaskRunRepository(pool),
		ScanIngest:   NewScanIngestRepository(pool),
		ItemHelpers:  NewItemHelperRepository(pool),
		APIKeys:      NewAPIKeyRepository(pool),
		ImageLookup:  NewImageLookupRepository(pool),
	}
}
