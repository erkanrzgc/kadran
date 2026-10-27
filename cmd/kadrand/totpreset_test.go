package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/erkanrzgc/kadran/internal/audit"
	"github.com/erkanrzgc/kadran/internal/store"
	"github.com/erkanrzgc/kadran/internal/totp"
)

// ── Kökün kaçış yolu (K-155) ──────────────────────────────────────────

func totpAcikDB(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "kadran.db")
	db, err := store.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	now := time.Unix(1_800_000_000, 0)
	sir, err := totp.NewSecret()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.BeginTOTPEnrollment(ctx, sir, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ConfirmTOTP(ctx, totp.StepAt(now), make([]byte, totp.TokenKeySize), nil, now); err != nil {
		t.Fatal(err)
	}
	return path
}

func sonKayit(t *testing.T, path string) (store.TOTPState, []audit.Record) {
	t.Helper()
	db, err := store.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	st, err := db.TOTP(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	recs, err := db.ListAudit(context.Background(), 0, 1000)
	if err != nil {
		t.Fatal(err)
	}
	return st, recs
}

func TestTOTPResetDisablesAndAudits(t *testing.T) {
	path := totpAcikDB(t)
	var out bytes.Buffer
	if err := runTOTPReset(context.Background(), path, &out); err != nil {
		t.Fatal(err)
	}
	st, recs := sonKayit(t, path)
	if st.Enabled() {
		t.Fatal("sıfırlamadan sonra TOTP açık")
	}
	if len(recs) == 0 {
		t.Fatal("denetim kaydı yok")
	}
	son := recs[len(recs)-1]
	if son.Action != "totp.reset" || son.Outcome != audit.OutcomeSuccess ||
		son.Actor.Origin != "local" || son.Source != audit.SourceDaemon {
		t.Fatalf("son kayıt: %+v", son)
	}
	if !strings.Contains(out.String(), "TOTP disabled; every session is invalid") {
		t.Fatalf("çıktı: %q", out.String())
	}
}

// TestTOTPResetWhenOffChangesNothing: TOTP zaten kapalıysa denetim zincirine
// boş bir "sıfırlandı" kaydı düşmez.
func TestTOTPResetWhenOffChangesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kadran.db")
	var out bytes.Buffer
	if err := runTOTPReset(context.Background(), path, &out); err != nil {
		t.Fatal(err)
	}
	_, recs := sonKayit(t, path)
	for _, r := range recs {
		if r.Action == "totp.reset" {
			t.Fatalf("kapalıyken sıfırlama kaydı yazıldı: %+v", r)
		}
	}
	if !strings.Contains(out.String(), "TOTP is not on") {
		t.Fatalf("çıktı: %q", out.String())
	}
}
