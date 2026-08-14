package effectiveconfig

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

func (s *Store) String(ctx context.Context, key, defaultValue string) (string, error) {
	snapshot, err := s.Get(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return defaultValue, nil
	}
	if err != nil {
		return "", err
	}
	value, ok := snapshot.Values[key]
	if !ok || value == nil {
		return defaultValue, nil
	}
	typed, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("effective configuration %s must be string", key)
	}
	return typed, nil
}
