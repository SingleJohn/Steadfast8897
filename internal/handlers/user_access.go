package handlers

import (
	"context"

	"github.com/google/uuid"

	"fyms/internal/models"
)

type userLibraryScope struct {
	AllowAll bool
	IDs      []string
	idSet    map[string]struct{}
}

func loadUserLibraryScope(ctx context.Context, state *AppState, userID string) (*userLibraryScope, error) {
	uid, err := uuid.Parse(userID)
	if err != nil {
		return &userLibraryScope{AllowAll: true}, nil
	}

	policy, err := state.Repo.Users.GetUserPolicy(ctx, uid)
	if err != nil {
		return nil, err
	}
	if policy == nil || policy.IsAdministrator || policy.EnableAllFolders {
		return &userLibraryScope{AllowAll: true}, nil
	}

	ids := append([]string(nil), policy.EnabledFolders...)
	if len(ids) == 0 {
		legacyIDs, err := state.Repo.Users.ListUserLibraryAccess(ctx, uid)
		if err != nil {
			return nil, err
		}
		for _, id := range legacyIDs {
			ids = append(ids, id.String())
		}
	}

	set := make(map[string]struct{}, len(ids))
	normalized := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" {
			continue
		}
		if _, ok := set[id]; ok {
			continue
		}
		set[id] = struct{}{}
		normalized = append(normalized, id)
	}

	return &userLibraryScope{IDs: normalized, idSet: set}, nil
}

func (s *userLibraryScope) allowsLibrary(id string) bool {
	if s == nil || s.AllowAll {
		return true
	}
	_, ok := s.idSet[id]
	return ok
}

func applyLibraryScope(opts *models.ItemQueryOptions, scope *userLibraryScope) {
	if opts == nil || scope == nil || scope.AllowAll {
		return
	}
	opts.AllowedLibraryIDs = append([]string(nil), scope.IDs...)
}

func userCanAccessItem(ctx context.Context, state *AppState, userID string, itemID string) (bool, error) {
	scope, err := loadUserLibraryScope(ctx, state, userID)
	if err != nil || scope == nil || scope.AllowAll {
		return err == nil, err
	}

	var libraryID string
	err = state.DB.QueryRow(ctx,
		"SELECT library_id::text FROM items WHERE id = $1::uuid", itemID).Scan(&libraryID)
	if err != nil {
		return false, err
	}
	return scope.allowsLibrary(libraryID), nil
}
