package models

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Library struct {
	ID               uuid.UUID `json:"Id"`
	Name             string    `json:"Name"`
	CollectionType   string    `json:"CollectionType"`
	Paths            []string  `json:"Paths"`
	CreatedAt        time.Time `json:"CreatedAt"`
	PrimaryImagePath *string   `json:"PrimaryImagePath,omitempty"`
	PrimaryImageTag  *string   `json:"PrimaryImageTag,omitempty"`
}

func scanLibrary(row pgx.Row) (*Library, error) {
	var l Library
	err := row.Scan(&l.ID, &l.Name, &l.CollectionType, &l.Paths, &l.CreatedAt,
		&l.PrimaryImagePath, &l.PrimaryImageTag)
	if err != nil {
		return nil, err
	}
	return &l, nil
}

func GetAllLibraries(ctx context.Context, pool *pgxpool.Pool) ([]Library, error) {
	rows, err := pool.Query(ctx, "SELECT * FROM libraries ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var libs []Library
	for rows.Next() {
		var l Library
		if err := rows.Scan(&l.ID, &l.Name, &l.CollectionType, &l.Paths, &l.CreatedAt,
			&l.PrimaryImagePath, &l.PrimaryImageTag); err != nil {
			return nil, err
		}
		libs = append(libs, l)
	}
	return libs, rows.Err()
}

func GetLibraryByID(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID) (*Library, error) {
	row := pool.QueryRow(ctx, "SELECT * FROM libraries WHERE id = $1", id)
	l, err := scanLibrary(row)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return l, err
}

func CreateLibrary(ctx context.Context, pool *pgxpool.Pool, name, collectionType string, paths []string) (*Library, error) {
	row := pool.QueryRow(ctx,
		"INSERT INTO libraries (name, collection_type, paths) VALUES ($1, $2, $3) RETURNING *",
		name, collectionType, paths)
	return scanLibrary(row)
}

func UpdateLibrary(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID, name *string) (*Library, error) {
	if name != nil {
		_, err := pool.Exec(ctx, "UPDATE libraries SET name = $1 WHERE id = $2", *name, id)
		if err != nil {
			return nil, err
		}
	}
	return GetLibraryByID(ctx, pool, id)
}

func DeleteLibrary(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID) error {
	_, err := pool.Exec(ctx, "DELETE FROM items WHERE library_id = $1", id)
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, "DELETE FROM libraries WHERE id = $1", id)
	return err
}

func AddLibraryPath(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID, path string) error {
	_, err := pool.Exec(ctx,
		"UPDATE libraries SET paths = array_append(paths, $1) WHERE id = $2", path, id)
	return err
}

func UpdateLibraryImage(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID, imagePath, imageTag string) error {
	_, err := pool.Exec(ctx,
		"UPDATE libraries SET primary_image_path = $1, primary_image_tag = $2 WHERE id = $3",
		imagePath, imageTag, id)
	return err
}

func DeleteLibraryImage(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID) error {
	_, err := pool.Exec(ctx,
		"UPDATE libraries SET primary_image_path = NULL, primary_image_tag = NULL WHERE id = $1", id)
	return err
}

func RemoveLibraryPath(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID, path string) error {
	_, err := pool.Exec(ctx,
		"UPDATE libraries SET paths = array_remove(paths, $1) WHERE id = $2", path, id)
	return err
}
