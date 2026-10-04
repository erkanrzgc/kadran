package store

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"filippo.io/age"

	"github.com/erkanrzgc/kadran/internal/vault"
	"github.com/erkanrzgc/kadran/internal/vault/vaulttest"
)

// storeTestIdentity, newAppStore'un kasasını açan anahtar.
var storeTestIdentity = func() *age.X25519Identity {
	id, err := age.GenerateX25519Identity()
	if err != nil {
		panic(err)
	}
	return id
}()

// plain, uygulamanın mühürlü değerlerini açar; her değerin bu uygulamaya ve
// bu ada bağlı olduğunu da denetler.
func plain(t *testing.T, app App) map[string]string {
	t.Helper()
	out := make(map[string]string, len(app.Env))
	for k, v := range app.Env {
		gotApp, gotKey, val := vaulttest.Open(t, storeTestIdentity, v)
		if gotApp != app.ID || gotKey != k {
			t.Fatalf("%s/%s başka bir yere bağlı: (%s, %s)", app.ID, k, gotApp, gotKey)
		}
		out[k] = val
	}
	return out
}

// plantPlaintext, kasadan önceki bir veritabanını taklit eder: değer
// mühürlenmeden doğrudan sütuna yazılıyor.
func plantPlaintext(t *testing.T, s *Store, appID, envJSON string) {
	t.Helper()
	if _, err := s.db.ExecContext(context.Background(),
		`UPDATE apps SET env_json = ? WHERE id = ?`, envJSON, appID); err != nil {
		t.Fatal(err)
	}
}

// dbFilesContain, veritabanının diskteki BÜTÜN dosyalarında (ana dosya,
// WAL, paylaşılan bellek) değeri arar.
func dbFilesContain(t *testing.T, s *Store, needle string) bool {
	t.Helper()
	for _, suffix := range []string{"", "-wal", "-shm"} {
		data, err := os.ReadFile(s.path + suffix)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(data, []byte(needle)) {
			return true
		}
	}
	return false
}

func newPlainStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(context.Background(), t.TempDir()+"/kadran.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// Kabul testi (K-123): göçten önce düz değer veritabanı dosyalarında
// BULUNUYOR (kontrol), göçten sonra hiçbirinde bulunmuyor.
func TestEnableVaultSealsPlaintextAndScrubsFiles(t *testing.T) {
	ctx := context.Background()
	s := newPlainStore(t)
	if _, err := s.CreateApp(ctx, sampleApp("blog")); err != nil {
		t.Fatal(err)
	}
	const sir = "kasa-kabul-testi-benzersiz-deger-7f3a9c"
	plantPlaintext(t, s, "blog", `{"DB_PASSWORD":"`+sir+`","BOS":"","ONEKLI":"age:merhaba"}`)

	if !dbFilesContain(t, s, sir) {
		t.Fatal("KONTROL: düz değer göçten önce dosyalarda yok — ölçüm bir şey kanıtlamaz")
	}

	n, err := s.EnableVault(ctx, vaulttest.Sealer(t, storeTestIdentity))
	if err != nil {
		t.Fatalf("kasa açılamadı: %v", err)
	}
	if n != 3 {
		t.Errorf("%d değer mühürlendi, 3 bekleniyordu", n)
	}
	if dbFilesContain(t, s, sir) {
		t.Fatal("düz değer göçten sonra veritabanı dosyalarında duruyor")
	}

	got, err := s.GetApp(ctx, "blog")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"DB_PASSWORD": sir, "BOS": "", "ONEKLI": "age:merhaba"}
	p := plain(t, got)
	for k, v := range want {
		if p[k] != v {
			t.Errorf("%s = %q, %q bekleniyordu", k, p[k], v)
		}
	}
}

// İkinci açılış hiçbir şeyi yeniden mühürlememeli; yoksa her açılışta
// değerler bir kat daha sarılırdı.
func TestEnableVaultIsIdempotent(t *testing.T) {
	ctx := context.Background()
	s := newAppStore(t)
	app := sampleApp("blog")
	app.Env = map[string]string{"A": "1"}
	if _, err := s.CreateApp(ctx, app); err != nil {
		t.Fatal(err)
	}
	once, err := s.GetApp(ctx, "blog")
	if err != nil {
		t.Fatal(err)
	}

	n, err := s.EnableVault(ctx, vaulttest.Sealer(t, storeTestIdentity))
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("ikinci açılış %d değer mühürledi", n)
	}
	sonra, err := s.GetApp(ctx, "blog")
	if err != nil {
		t.Fatal(err)
	}
	if sonra.Env["A"] != once.Env["A"] {
		t.Fatal("ikinci açılış mühürlü değeri değiştirdi")
	}
}

// Anahtar değişmişse (kayıp anahtarın yerine yenisi üretilmiş) daemon
// açılmamalı: değerler çözülemez olurdu ve bu ancak dağıtımda görünürdü.
func TestEnableVaultRefusesChangedRecipient(t *testing.T) {
	ctx := context.Background()
	s := newAppStore(t)
	_, err := s.EnableVault(ctx, vaulttest.Sealer(t, vaulttest.NewIdentity(t)))
	if !errors.Is(err, ErrVaultRecipientChanged) {
		t.Fatalf("değişen alıcı kabul edildi: %v", err)
	}
}

// İşaret yokken mühürlü görünen bir değer, ikinci kez mühürlenmemeli:
// executor dış katmanı açar, uygulamaya içteki şifreli metni verirdi.
func TestEnableVaultRefusesSealedValuesWithoutMarker(t *testing.T) {
	ctx := context.Background()
	s := newPlainStore(t)
	if _, err := s.CreateApp(ctx, sampleApp("blog")); err != nil {
		t.Fatal(err)
	}
	sealed, err := vaulttest.Sealer(t, storeTestIdentity).Seal("blog", "A", "x")
	if err != nil {
		t.Fatal(err)
	}
	plantPlaintext(t, s, "blog", `{"A":"`+sealed+`"}`)

	if _, err := s.EnableVault(ctx, vaulttest.Sealer(t, storeTestIdentity)); err == nil {
		t.Fatal("işaretsiz mühürlü değer ikinci kez mühürlendi")
	}
	got, err := s.GetApp(ctx, "blog")
	if err != nil {
		t.Fatal(err)
	}
	if got.Env["A"] != sealed {
		t.Fatal("değer değişti")
	}
}

// Kasa açılmadan ortam değişkeni yazılamaz: düz metin veritabanına hiç
// girmemeli.
func TestEnvWritesRequireVault(t *testing.T) {
	ctx := context.Background()
	s := newPlainStore(t)

	app := sampleApp("blog")
	app.Env = map[string]string{"A": "1"}
	if _, err := s.CreateApp(ctx, app); err == nil {
		t.Fatal("kasasız depo ortam değişkeni yazdı")
	}
	if _, err := s.CreateApp(ctx, sampleApp("bos")); err != nil {
		t.Fatalf("ortam değişkeni olmayan uygulama yazılamadı: %v", err)
	}
	if _, err := s.UpdateApp(ctx, "bos", AppUpdate{Env: map[string]string{"A": "1"}}); err == nil {
		t.Fatal("kasasız depo güncellemede ortam değişkeni yazdı")
	}
}

// Tek boğaz: oluşturma ve güncelleme diske yalnız mühürlü değer yazıyor.
func TestCreateAndUpdatePersistOnlySealedValues(t *testing.T) {
	ctx := context.Background()
	s := newAppStore(t)
	const bir, iki = "olusturma-degeri-c41e2b", "guncelleme-degeri-9d07fa"

	app := sampleApp("blog")
	app.Env = map[string]string{"A": bir}
	if _, err := s.CreateApp(ctx, app); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateApp(ctx, "blog", AppUpdate{Env: map[string]string{"B": iki}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		t.Fatal(err)
	}
	for _, v := range []string{bir, iki} {
		if dbFilesContain(t, s, v) {
			t.Errorf("düz değer %q diske yazıldı", v)
		}
	}

	got, err := s.GetApp(ctx, "blog")
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range got.Env {
		if !strings.HasPrefix(v, vault.Prefix) {
			t.Errorf("%s mühürsüz okundu: %q", k, v)
		}
	}
	p := plain(t, got)
	if p["A"] != bir || p["B"] != iki {
		t.Errorf("açılan değerler %+v", p)
	}
}
