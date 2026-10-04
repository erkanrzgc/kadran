package bootstrap

import (
	"context"
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"filippo.io/age"

	"github.com/erkanrzgc/kadran/internal/store"
	"github.com/erkanrzgc/kadran/internal/vault/vaulttest"
)

// Kasanın geri dönüş betiği (K-123) gerçek sqlite3 ve age ile koşuyor.
// CI'ın Linux işinde araçlar kurulu ve KADRAN_TEST_REQUIRE_KASA_TOOLS=1:
// orada eksik araç atlama değil hata (garanti ortamda t.Skip yasak).
func needKasaTools(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("bash betiği; Linux'ta koşuyor")
	}
	for _, tool := range []string{"bash", "sqlite3", "age", "od", "base64"} {
		if _, err := exec.LookPath(tool); err != nil {
			if os.Getenv("KADRAN_TEST_REQUIRE_KASA_TOOLS") == "1" {
				t.Fatalf("%s yok ama bu ortamda zorunlu", tool)
			}
			t.Skipf("%s yok", tool)
		}
	}
}

type kasaOrtam struct {
	db, key string
	id      *age.X25519Identity
}

// kasaliVeritabani, v0.5.0'ın yazdığı gibi mühürlü bir veritabanı kurar.
func kasaliVeritabani(t *testing.T, apps map[string]map[string]string) kasaOrtam {
	t.Helper()
	dir := t.TempDir()
	o := kasaOrtam{db: filepath.Join(dir, "kadran.db"), key: filepath.Join(dir, "vault.key"), id: vaulttest.NewIdentity(t)}
	if err := os.WriteFile(o.key, []byte(o.id.String()+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	s, err := store.Open(ctx, o.db)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.EnableVault(ctx, vaulttest.Sealer(t, o.id)); err != nil {
		t.Fatal(err)
	}
	for appID, env := range apps {
		if _, err := s.CreateApp(ctx, store.App{
			ID: appID, GitHost: "github.com", GitOwner: "o", GitRepo: appID,
			GitBranch: "main", DockerfilePath: "Dockerfile", Env: env,
			ContainerPort: 8080, Replicas: 1, HealthPath: "/",
			Domain: appID + ".example.com", MemoryBytes: 256 << 20,
			CPUMillis: 500, BlkioWeight: 500,
		}); err != nil {
			t.Fatal(err)
		}
	}
	return o
}

func kasaCoz(t *testing.T, o kasaOrtam, key string) (string, error) {
	t.Helper()
	cmd := exec.Command("bash", "kasa-coz.sh")
	cmd.Env = append(os.Environ(), "KADRAN_DB="+o.db, "KADRAN_VAULT_KEY="+key, "KADRAN_KASA_TEST=1")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func rawEnv(t *testing.T, path, appID string) string {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var env string
	if err := db.QueryRow(`SELECT env_json FROM apps WHERE id = ?`, appID).Scan(&env); err != nil {
		t.Fatal(err)
	}
	return env
}

func markerCount(t *testing.T, path string) int {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM env_seal`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// Gidiş-dönüş: betik değerleri BİREBİR düz metne çeviriyor (boş, satır
// sonlu, tırnaklı, Türkçe değerler dahil) ve işareti siliyor; ardından
// daemon yeniden açılınca değerler yine mühürleniyor.
func TestKasaCozRoundTrip(t *testing.T) {
	needKasaTools(t)
	degerler := map[string]string{
		"DATABASE_URL": "postgres://u:p@db:5432/x?sslmode=require",
		"BOS":          "",
		"SATIRLI":      "bir\niki\n\n",
		"TIRNAKLI":     `'"$x` + "`;--",
		"TURKCE":       "ğüşöçİı",
		// od -v olmadan tekrar eden 16 baytlık satırlar '*' ile kısalır.
		"TEKRAR": strings.Repeat("a", 64),
	}
	o := kasaliVeritabani(t, map[string]map[string]string{"blog": degerler, "shop": {"A": "1"}})

	out, err := kasaCoz(t, o, o.key)
	if err != nil {
		t.Fatalf("betik başarısız: %v\n%s", err, out)
	}
	if markerCount(t, o.db) != 0 {
		t.Fatal("kasa işareti silinmedi")
	}

	ctx := context.Background()
	s, err := store.Open(ctx, o.db)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := s.GetApp(ctx, "blog")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Env) != len(degerler) {
		t.Fatalf("%d değer döndü, %d bekleniyordu: %q", len(got.Env), len(degerler), got.Env)
	}
	for k, want := range degerler {
		if got.Env[k] != want {
			t.Errorf("%s = %q, %q bekleniyordu", k, got.Env[k], want)
		}
	}

	// v0.5.0'a dönüş: işaret yok, daemon bütün değerleri yeniden mühürler.
	n, err := s.EnableVault(ctx, vaulttest.Sealer(t, o.id))
	if err != nil {
		t.Fatalf("yeniden mühürlenemedi: %v", err)
	}
	if n != len(degerler)+1 {
		t.Errorf("%d değer mühürlendi, %d bekleniyordu", n, len(degerler)+1)
	}
}

// Yanlış anahtarla betik durmalı ve veritabanına dokunmamalı: yarım bir
// çözme, işaretsiz ve karışık (kimi düz kimi mühürlü) bir veritabanı
// bırakırdı.
func TestKasaCozWrongKeyChangesNothing(t *testing.T) {
	needKasaTools(t)
	o := kasaliVeritabani(t, map[string]map[string]string{"blog": {"A": "1", "B": "2"}})
	once := rawEnv(t, o.db, "blog")

	baska := filepath.Join(t.TempDir(), "baska.key")
	if err := os.WriteFile(baska, []byte(vaulttest.NewIdentity(t).String()+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := kasaCoz(t, o, baska); err == nil {
		t.Fatalf("yanlış anahtarla başarılı oldu:\n%s", out)
	}
	if rawEnv(t, o.db, "blog") != once || markerCount(t, o.db) != 1 {
		t.Fatal("başarısız koşu veritabanını değiştirdi")
	}
}

// Betik root olarak daemon'un yazdığı veriyi okuyor. Değişken adı SQL'e
// girmeden önce desenle sınanmalı; sınanmasaydı ele geçirilmiş bir daemon
// root'un sqlite3 oturumuna SQL sokardı.
func TestKasaCozRejectsHostileNamesFromDatabase(t *testing.T) {
	needKasaTools(t)
	o := kasaliVeritabani(t, map[string]map[string]string{"blog": {"A": "1"}})
	db, err := sql.Open("sqlite", "file:"+o.db)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := vaulttest.Sealer(t, o.id).Seal("blog", "A", "1")
	if err != nil {
		t.Fatal(err)
	}
	kotu := `A"')); DELETE FROM apps; --`
	if _, err := db.Exec(`UPDATE apps SET env_json = json_object(?, ?) WHERE id = 'blog'`, kotu, sealed); err != nil {
		t.Fatal(err)
	}
	db.Close()
	once := rawEnv(t, o.db, "blog")

	out, err := kasaCoz(t, o, o.key)
	if err == nil {
		t.Fatalf("düşmanca ad kabul edildi:\n%s", out)
	}
	if !strings.Contains(out, "geçersiz değişken adı") {
		t.Errorf("başka bir sebeple durdu: %s", out)
	}
	if rawEnv(t, o.db, "blog") != once {
		t.Fatal("veritabanı değişti")
	}
}

// Değer başka bir ada taşınmışsa (bağ tutmuyorsa) betik durmalı: executor
// da onu açmazdı.
func TestKasaCozRejectsMovedValue(t *testing.T) {
	needKasaTools(t)
	o := kasaliVeritabani(t, map[string]map[string]string{"blog": {"A": "1"}})
	db, err := sql.Open("sqlite", "file:"+o.db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE apps SET env_json = json_object('B', json_extract(env_json, '$.A')) WHERE id = 'blog'`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	out, err := kasaCoz(t, o, o.key)
	if err == nil || !strings.Contains(out, "başka bir uygulamaya ya da ada") {
		t.Fatalf("taşınmış değer reddedilmedi: %v\n%s", err, out)
	}
}
