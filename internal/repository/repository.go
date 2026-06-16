package repository

import "github.com/jackc/pgx/v5/pgxpool"

type Repository struct {
	SystemConfig *SystemConfigRepository
	Users        *UserRepository
	Sessions     *SessionRepository
	Libraries    *LibraryRepository
	DisplayOrder *DisplayOrderRepository
}

func New(pool *pgxpool.Pool) *Repository {
	return &Repository{
		SystemConfig: NewSystemConfigRepository(pool),
		Users:        NewUserRepository(pool),
		Sessions:     NewSessionRepository(pool),
		Libraries:    NewLibraryRepository(pool),
		DisplayOrder: NewDisplayOrderRepository(pool),
	}
}
