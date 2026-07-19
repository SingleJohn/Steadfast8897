package shared

import (
	"context"

	"github.com/google/uuid"

	"fyms/internal/models"
)

type UserLibraryScope struct {
	AllowAll bool
	IDs      []string
	idSet    map[string]struct{}
}

func LoadUserLibraryScope(ctx context.Context, state *AppState, userID string) (*UserLibraryScope, error) {
	uid, err := uuid.Parse(userID)
	if err != nil {
		return &UserLibraryScope{AllowAll: true}, nil
	}

	policy, err := state.Repo.Users.GetUserPolicy(ctx, uid)
	if err != nil {
		return nil, err
	}
	if policy == nil || policy.IsAdministrator || policy.EnableAllFolders {
		return &UserLibraryScope{AllowAll: true}, nil
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

	return &UserLibraryScope{IDs: normalized, idSet: set}, nil
}

func (s *UserLibraryScope) AllowsLibrary(id string) bool {
	if s == nil || s.AllowAll {
		return true
	}
	_, ok := s.idSet[id]
	return ok
}

func ApplyLibraryScope(opts *models.ItemQueryOptions, scope *UserLibraryScope) {
	if opts == nil || scope == nil || scope.AllowAll {
		return
	}
	opts.AllowedLibraryIDs = make([]string, len(scope.IDs))
	copy(opts.AllowedLibraryIDs, scope.IDs)
}

func UserCanAccessItem(ctx context.Context, state *AppState, userID string, itemID string) (bool, error) {
	scope, err := LoadUserLibraryScope(ctx, state, userID)
	if err != nil || scope == nil || scope.AllowAll {
		return err == nil, err
	}

	libraryID, err := state.Repo.Users.GetItemLibraryIDForAccess(ctx, itemID)
	if err != nil {
		return false, err
	}
	return scope.AllowsLibrary(libraryID), nil
}
