package catalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// GetCategory returns the Central-projected category currently held in the POS
// SQLite catalog. Callers use this only to snapshot the category fact that was
// known locally when an offline sale completed.
func (r *Repository) GetCategory(ctx context.Context, id string) (Category, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Category{}, ErrNotFound
	}
	var category Category
	err := r.db.SQL().QueryRowContext(ctx, `
        SELECT id, parent_id, name, code, sort_order
        FROM catalog_categories
        WHERE id = ? AND is_active = 1`, id).Scan(
		&category.ID, &category.ParentID, &category.Name, &category.Code, &category.SortOrder,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Category{}, ErrNotFound
		}
		return Category{}, fmt.Errorf("get catalog category: %w", err)
	}
	return category, nil
}
