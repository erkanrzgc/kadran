package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Setting, settings tablosundan bir anahtarı okur. Anahtar yoksa ok=false
// döner; bu bir hata değildir.
func (s *Store) Setting(ctx context.Context, key string) (string, bool, error) {
	var value string
	err := s.db.QueryRowContext(ctx,
		`SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("could not read the setting: %w", err)
	}
	return value, true, nil
}

// SetSetting, bir anahtarı yazar; varsa üzerine yazar (upsert).
func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		key, value, time.Now().Unix())
	if err != nil {
		return fmt.Errorf("could not write the setting: %w", err)
	}
	return nil
}
