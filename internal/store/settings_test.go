package store

import (
	"context"
	"testing"
	"time"
)

func TestSettingRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if _, ok, err := s.Setting(ctx, "k"); err != nil || ok {
		t.Fatalf("eksik anahtar: ok=%v err=%v", ok, err)
	}
	if err := s.SetSetting(ctx, "k", "bir"); err != nil {
		t.Fatal(err)
	}
	if v, ok, err := s.Setting(ctx, "k"); err != nil || !ok || v != "bir" {
		t.Fatalf("okunan = %q ok=%v err=%v", v, ok, err)
	}
	if err := s.SetSetting(ctx, "k", "iki"); err != nil {
		t.Fatal(err)
	}
	var updated int64
	if err := s.db.QueryRowContext(ctx, `SELECT updated_at FROM settings WHERE key = ?`, "k").Scan(&updated); err != nil {
		t.Fatal(err)
	}
	if d := time.Now().Unix() - updated; d < 0 || d > 60 {
		t.Fatalf("updated_at = %d, simdiye yakin bekleniyordu", updated)
	}
	if v, _, _ := s.Setting(ctx, "k"); v != "iki" {
		t.Fatalf("üzerine yazma sonrası = %q", v)
	}
	if _, ok, _ := s.Setting(ctx, "baska"); ok {
		t.Fatal("başka anahtar görünüyor")
	}
}
