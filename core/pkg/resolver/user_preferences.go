package resolver

import (
	"cmp"
	"context"
	"fmt"
	"regexp"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/authorization"
	"github.com/logoscore/logos-core/core/pkg/graphql/gqlctx"
	"github.com/logoscore/logos-core/core/pkg/graphql/model"
	"github.com/logoscore/logos-core/core/pkg/logger"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"go.uber.org/zap"
)

// Picker history: the icon picker's "Frequently used" row and the operation
// picker's "Recent" section. Both used to live in localStorage and vanished
// with it — on a new browser, another origin, a cleared cache, or when the
// browser evicted site data on its own. See models.User.FrequentIcons.

// iconNamePattern is the shape of what the SPA stores as a document icon: a
// lucide name, or "si:<slug>" for a brand icon. Whether the icon still exists
// is left to the client, which is the only side that knows the bundle; this
// only keeps arbitrary strings out of the user document.
var iconNamePattern = regexp.MustCompile(`^[A-Za-z0-9:_-]{1,64}$`)

// maxImportedIconCount bounds an imported count. A real one is a number of
// clicks; this only stops a hand-crafted import from pinning an icon forever.
const maxImportedIconCount = 10_000

// isCaller reports whether obj is the caller's own user. The picker history is
// private to its owner, including from admins reading someone else's User.
func isCaller(ctx context.Context, obj *models.User) bool {
	return obj != nil && gqlctx.AuthFromContext(ctx).UserID == obj.UserID.String()
}

// viewableOperation loads an operation and checks the caller may view it.
func (r *userResolver) viewableOperation(ctx context.Context, id uuid.UUID) (*models.Operation, error) {
	op, err := gqlctx.LoadOperation(ctx, r.operationRepo, id)
	if err != nil {
		return nil, err
	}
	if err := authorization.AuthorizeOperationRole(ctx, &op, models.OperationRoleViewer); err != nil {
		return nil, err
	}
	return &op, nil
}

func (r *userResolver) reloadCaller(ctx context.Context, uid uuid.UUID) (*models.User, error) {
	u, err := r.userRepo.FindByID(ctx, uid)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch updated user: %w", err)
	}
	return &u, nil
}

// RecordIconUse counts one pick of an icon. No event is published: nobody but
// the caller reads this, and a UserUpdated per click would refetch every admin
// user list for nothing.
func (r *userResolver) RecordIconUse(ctx context.Context, name string) (*models.User, error) {
	uid, err := callerUserID(ctx)
	if err != nil {
		return nil, err
	}
	if !iconNamePattern.MatchString(name) {
		return nil, fmt.Errorf("invalid icon name %q", name)
	}
	if err := r.preferencesRepo.RecordIconUse(ctx, uid, name, time.Now()); err != nil {
		return nil, fmt.Errorf("failed to record icon use: %w", err)
	}
	return r.reloadCaller(ctx, uid)
}

// TouchRecentOperation moves an operation to the front of the caller's recent
// list. It is refused for an operation the caller cannot view, so the list
// cannot be used to probe operation IDs, and never holds one it would have to
// hide on read anyway.
func (r *userResolver) TouchRecentOperation(ctx context.Context, operationID string) (*models.User, error) {
	uid, err := callerUserID(ctx)
	if err != nil {
		return nil, err
	}
	opID, err := uuid.Parse(operationID)
	if err != nil {
		return nil, fmt.Errorf("invalid operation ID: %w", err)
	}
	if _, err := r.viewableOperation(ctx, opID); err != nil {
		if repository.IsNotFound(err) {
			return nil, fmt.Errorf("operation not found")
		}
		return nil, err
	}
	if err := r.preferencesRepo.TouchRecentOperation(ctx, uid, opID); err != nil {
		return nil, fmt.Errorf("failed to record recent operation: %w", err)
	}
	return r.reloadCaller(ctx, uid)
}

// ImportLocalPreferences seeds both lists from what the SPA kept in
// localStorage. Each is written only while the stored one is empty (see
// IUserPreferencesRepository.SeedFrequentIcons), so this is safe to repeat.
//
// Bad entries are dropped rather than refused: the input is whatever an older
// build of the SPA left in a browser, and one stale entry should not cost the
// operator the rest of the list.
func (r *userResolver) ImportLocalPreferences(ctx context.Context, input model.ImportLocalPreferencesInput) (*models.User, error) {
	uid, err := callerUserID(ctx)
	if err != nil {
		return nil, err
	}

	if icons := sanitizeImportedIcons(input.FrequentIcons, time.Now()); len(icons) > 0 {
		if err := r.preferencesRepo.SeedFrequentIcons(ctx, uid, icons); err != nil {
			return nil, fmt.Errorf("failed to import frequent icons: %w", err)
		}
	}

	var ops []uuid.UUID
	for _, raw := range input.RecentOperationIds {
		if len(ops) == models.MaxRecentOperations {
			break
		}
		id, err := uuid.Parse(raw)
		if err != nil || slices.Contains(ops, id) {
			continue
		}
		if _, err := r.viewableOperation(ctx, id); err != nil {
			continue
		}
		ops = append(ops, id)
	}
	if len(ops) > 0 {
		if err := r.preferencesRepo.SeedRecentOperations(ctx, uid, ops); err != nil {
			return nil, fmt.Errorf("failed to import recent operations: %w", err)
		}
	}

	return r.reloadCaller(ctx, uid)
}

// sanitizeImportedIcons validates, dedupes (first occurrence wins), clamps and
// ranks an imported icon list the way the repository ranks a live one.
func sanitizeImportedIcons(in []*model.IconUsageInput, now time.Time) []models.IconUsage {
	var out []models.IconUsage
	seen := map[string]bool{}
	for _, ic := range in {
		if ic == nil || !iconNamePattern.MatchString(ic.Name) || seen[ic.Name] {
			continue
		}
		at, err := time.Parse(time.RFC3339, ic.LastUsedAt)
		if err != nil {
			continue
		}
		if at.After(now) {
			at = now
		}
		seen[ic.Name] = true
		out = append(out, models.IconUsage{
			Name:     ic.Name,
			Count:    min(max(ic.Count, 1), maxImportedIconCount),
			LastUsed: at.UTC(),
		})
	}
	slices.SortStableFunc(out, func(a, b models.IconUsage) int {
		return cmp.Or(cmp.Compare(b.Count, a.Count), b.LastUsed.Compare(a.LastUsed))
	})
	if len(out) > models.MaxFrequentIcons {
		out = out[:models.MaxFrequentIcons]
	}
	return out
}

// FrequentIcons is the stored ranking, names only. Empty on anyone else's user.
func (r *userResolver) FrequentIcons(ctx context.Context, obj *models.User) ([]string, error) {
	if !isCaller(ctx, obj) {
		return []string{}, nil
	}
	names := make([]string, 0, len(obj.FrequentIcons))
	for _, ic := range obj.FrequentIcons {
		names = append(names, ic.Name)
	}
	return names, nil
}

// RecentOperations resolves the stored IDs in order, leaving out operations
// that were deleted or that the caller can no longer view. They stay in the
// stored list and age out as new ones are touched.
//
// A failed lookup drops that one operation instead of failing the field: it is
// non-null on the non-null `me`, so an error here would null the whole query
// the app shell boots from, over a convenience list.
func (r *userResolver) RecentOperations(ctx context.Context, obj *models.User) ([]*models.Operation, error) {
	if !isCaller(ctx, obj) {
		return []*models.Operation{}, nil
	}
	ops := make([]*models.Operation, 0, len(obj.RecentOperations))
	for _, id := range obj.RecentOperations {
		op, err := gqlctx.LoadOperation(ctx, r.operationRepo, id)
		if err != nil {
			if !repository.IsNotFound(err) {
				logger.From(ctx).Warn("resolve recent operation", zap.String("operation_id", id.String()), zap.Error(err))
			}
			continue
		}
		if authorization.AuthorizeOperationRole(ctx, &op, models.OperationRoleViewer) != nil {
			continue
		}
		ops = append(ops, &op)
	}
	return ops, nil
}
