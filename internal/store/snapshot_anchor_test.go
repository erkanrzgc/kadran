package store

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/erkanrzgc/kadran/internal/anchor"
)

// capaYolu, yedeğin yanındaki çapanın yolu.
func capaYolu(dbYolu string) string {
	return strings.TrimSuffix(dbYolu, snapshotExt) + anchor.Ext
}

// TestSnapshotWritesAnchorOfTheChainHead, her yedeğin yanına zincirin O
// ANKİ ucunun yazıldığını doğrular (K-126 C). Uzak yedek bu dosyayı kilitli
// önekle R2'ye taşıyor; içerik yanlışsa çapa hiçbir şeyi korumaz.
func TestSnapshotWritesAnchorOfTheChainHead(t *testing.T) {
	s, _ := newSnapshotStore(t)
	ctx := context.Background()
	for _, eylem := range []string{"app.create", "app.deploy", "app.update"} {
		if _, err := s.AppendAudit(ctx, testRecord(eylem)); err != nil {
			t.Fatalf("kayıt eklenemedi: %v", err)
		}
	}
	seq, hash, err := s.AuditHead(ctx)
	if err != nil {
		t.Fatal(err)
	}

	info, err := s.Snapshot(ctx)
	if err != nil {
		t.Fatalf("yedek alınamadı: %v", err)
	}
	yol := capaYolu(info.Path)
	data, err := os.ReadFile(yol)
	if err != nil {
		t.Fatalf("yedeğin yanında çapa yok: %v", err)
	}
	a, err := anchor.Parse(filepath.Base(yol), data)
	if err != nil {
		t.Fatalf("çapa ayrıştırılamadı: %v", err)
	}
	if a.Seq != seq || a.Hash != hash {
		t.Errorf("çapa zincirin ucu değil: #%d, uç #%d", a.Seq, seq)
	}
	if !a.Taken.Equal(info.Taken) {
		t.Errorf("çapanın damgası %v, yedeğinki %v", a.Taken, info.Taken)
	}
	// Windows dosya kipini tutmuyor; CI ve sunucu Linux.
	if runtime.GOOS != "windows" {
		if fi, err := os.Stat(yol); err != nil || fi.Mode().Perm() != 0o600 {
			t.Errorf("çapa kipi 0600 değil: %v %v", fi.Mode(), err)
		}
	}
}

// Boş zincirde çapalanacak bir şey yok; sıfır sıralı bir çapa Parse'ın
// reddedeceği bir dosya olurdu.
func TestSnapshotWithEmptyChainWritesNoAnchor(t *testing.T) {
	s, _ := newSnapshotStore(t)
	info, err := s.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("yedek alınamadı: %v", err)
	}
	if _, err := os.Stat(capaYolu(info.Path)); !os.IsNotExist(err) {
		t.Errorf("boş zincirde çapa yazıldı: %v", err)
	}
}

// Budama, yedekle birlikte çapasını da siler: aksi hâlde yerel dizin
// sınırsız büyür ve uzak yedek her seferinde atlanan çapaları listeler.
func TestSnapshotPruneRemovesAnchorsWithTheirSnapshots(t *testing.T) {
	s, path := newSnapshotStore(t)
	ctx := context.Background()
	if _, err := s.AppendAudit(ctx, testRecord("app.create")); err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)
	for i := 0; i < SnapshotKeep+3; i++ {
		if _, err := s.snapshotAt(ctx, base.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatalf("yedek %d alınamadı: %v", i, err)
		}
	}
	capalar, err := filepath.Glob(filepath.Join(SnapshotDir(path), "kadran-*"+anchor.Ext))
	if err != nil {
		t.Fatal(err)
	}
	if len(capalar) != SnapshotKeep {
		t.Fatalf("çapa sayısı %d, %d bekleniyordu", len(capalar), SnapshotKeep)
	}
	for _, c := range capalar {
		if _, err := os.Stat(strings.TrimSuffix(c, anchor.Ext) + snapshotExt); err != nil {
			t.Errorf("çapanın yedeği yok (yetim çapa): %s", filepath.Base(c))
		}
	}
}
