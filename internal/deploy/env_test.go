package deploy

import (
	"context"
	"testing"

	"github.com/erkanrzgc/panely/internal/store"
)

// envApplication, ortam değişkeni taşıyan bir uygulama üretir.
func envApplication() store.App {
	app := testApplication()
	app.Env = map[string]string{
		"DATABASE_URL": "postgres://user:pw@db:5432/blog",
		"LOG_LEVEL":    "debug",
	}
	return app
}

// TestDeployPassesEnvToCreatedReplica, BU DEĞİŞİKLİĞİN VAR OLMA
// SEBEBİDİR.
//
// `createReplica`, uygulama tanımından CreateReplicaOptions kuruyor ve
// alan listesi ELLE yazılmış. Bir alanı atlamak derlemeyi kırmaz: eksik
// alan Go'nun sıfır değerine düşer, yani `Env` yazılmazsa konteyner
// SESSİZCE ortamsız doğar.
//
// Sonuç tam olarak şu olurdu: göç iner, şema alanı taşır, `app create
// -env DATABASE_URL=...` "başarılı" der, kayıt doğrudur, `panely app
// show` değişkeni gösterir — ve uygulama hiçbir zaman göremez. Bütün
// katmanlar yeşil, gerçeklik boş.
//
// Bu, ölçek küçültmede (K-080) bir kez yaşanan hatanın birebir aynısı:
// mekanizma bağlandı, son halkası unutuldu.
func TestDeployPassesEnvToCreatedReplica(t *testing.T) {
	// materialize: kurulan konteyner replika listesinde GORUNSUN.
	// Olmadan saglik kapisi "bu surumun konteyneri gorunmuyor" deyip
	// dagitimi durdurur ve test env yerine kapiyi olcmus olur.
	life := &fakeLifecycle{materialize: true}
	r := newHarness(t, life, true).rollout

	if err := r.Run(context.Background(), envApplication(),
		store.Release{ID: relNew}); err != nil {
		t.Fatalf("dağıtım başarısız: %v", err)
	}

	if len(life.created) == 0 {
		t.Fatal("hiç replika kurulmadı — test bir şey ölçemedi")
	}
	for _, o := range life.created {
		if o.Env == nil {
			t.Fatalf("replika #%d ORTAMSIZ kuruldu — uygulama "+
				"DATABASE_URL'i hiçbir zaman göremez", o.Index)
		}
		if o.Env["DATABASE_URL"] != "postgres://user:pw@db:5432/blog" {
			t.Errorf("replika #%d DATABASE_URL %q", o.Index, o.Env["DATABASE_URL"])
		}
		if o.Env["LOG_LEVEL"] != "debug" {
			t.Errorf("replika #%d LOG_LEVEL %q", o.Index, o.Env["LOG_LEVEL"])
		}
	}
}

// TestHealPassesEnvToRecreatedReplica, İYİLEŞTİRME yolunu da sınar.
//
// Ayrı bir test çünkü ayrı bir çağrı yolu. `createReplica` üç yerden
// çağrılıyor (Run, ensureReplicas, Rollback) ve yalnızca birini
// doğrulayan bir test, diğer ikisinin env'i düşürmesine izin verirdi.
//
// Bu yol özellikle sinsi: konteyner gece yarısı çöker, gözetmen onu
// yeniden kurar, ve uygulama bu kez ortamsız açılır. Kimse dağıtım
// yapmadığı için kimse şüphelenmez.
func TestHealPassesEnvToRecreatedReplica(t *testing.T) {
	// Replika listesi BOŞ: hostta konteyner yok, yani ensureReplicas
	// eksik olanı yeniden kurmak zorunda.
	life := &fakeLifecycle{materialize: true}
	r := newHarness(t, life, true).rollout

	if _, err := r.Rollback(context.Background(), envApplication(),
		store.Release{ID: relOld}); err != nil {
		t.Fatalf("geri alma başarısız: %v", err)
	}

	if len(life.created) == 0 {
		t.Fatal("hiç replika kurulmadı — test bir şey ölçemedi")
	}
	for _, o := range life.created {
		if o.Env["DATABASE_URL"] != "postgres://user:pw@db:5432/blog" {
			t.Errorf("yeniden kurulan replika #%d ortamsız: %+v", o.Index, o.Env)
		}
	}
}

// TestReplicaEnvIsACopy, sürücüye giden haritanın uygulama tanımıyla
// PAYLAŞILMADIĞINI doğrular.
//
// Aynı haritayı paylaşmak, aşağıdaki katmanlardan birinin onu
// değiştirmesi hâlinde kontrol düzlemindeki kaydı da bozması demekti.
// Go'da harita atamak referans kopyalar; bu, gözden kaçması en kolay
// paylaşım biçimi.
func TestReplicaEnvIsACopy(t *testing.T) {
	// materialize: kurulan konteyner replika listesinde GORUNSUN.
	// Olmadan saglik kapisi "bu surumun konteyneri gorunmuyor" deyip
	// dagitimi durdurur ve test env yerine kapiyi olcmus olur.
	life := &fakeLifecycle{materialize: true}
	r := newHarness(t, life, true).rollout

	app := envApplication()
	if err := r.Run(context.Background(), app,
		store.Release{ID: relNew}); err != nil {
		t.Fatalf("dağıtım başarısız: %v", err)
	}
	if len(life.created) == 0 {
		t.Fatal("hiç replika kurulmadı")
	}

	// Sürücüye giden haritayı boz; uygulama tanımı ETKİLENMEMELİ.
	life.created[0].Env["DATABASE_URL"] = "BOZULDU"

	if app.Env["DATABASE_URL"] != "postgres://user:pw@db:5432/blog" {
		t.Errorf("sürücüye giden harita uygulama tanımıyla PAYLAŞILIYOR — "+
			"alt katman kontrol düzlemindeki kaydı bozabilir: %+v", app.Env)
	}
}
