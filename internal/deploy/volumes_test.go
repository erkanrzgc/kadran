package deploy

import (
	"context"
	"testing"

	"github.com/erkanrzgc/panely/internal/store"
)

// volumeApplication, hacim taşıyan bir uygulama üretir.
func volumeApplication() store.App {
	app := testApplication()
	app.Volumes = []store.VolumeMount{
		{Name: "data", MountPath: "/var/lib/app"},
		{Name: "config", MountPath: "/etc/app", ReadOnly: true},
	}
	return app
}

// TestDeployPassesVolumesToCreatedReplica, HACİM İŞİNİN SON HALKASIDIR.
//
// `createReplica` alan listesini ELLE kuruyor ve derleyici eksik alanı
// yakalamaz. `Volumes` yazılmazsa göç iner, şema taşır, `app create
// -volume data:/var/lib/app` "başarılı" der, `app show` hacmi gösterir —
// ve konteyner hiçbir disk bağlanmadan doğar.
//
// Bedeli env'dekinden AĞIR: kullanıcı diskin bağlı olduğunu sanıp veri
// yazar, veri konteynerin KENDİ katmanında durur ve bir sonraki
// dağıtımda KAYBOLUR. Belirti "disk çalışmıyor" değil, "verilerim gitti".
//
// Env'de aynı halka bir kez unutuldu (K-082) ve rollout.go'ya bunu anan
// bir uyarı yorumu kondu. Bu test o uyarının kanıtı.
func TestDeployPassesVolumesToCreatedReplica(t *testing.T) {
	life := &fakeLifecycle{materialize: true}
	r := newHarness(t, life, true).rollout

	if err := r.Run(context.Background(), volumeApplication(),
		store.Release{ID: relNew}); err != nil {
		t.Fatalf("dağıtım başarısız: %v", err)
	}

	if len(life.created) == 0 {
		t.Fatal("hiç replika kurulmadı — test bir şey ölçemedi")
	}
	for _, o := range life.created {
		if len(o.Volumes) != 2 {
			t.Fatalf("replika #%d %d hacimle kuruldu, 2 bekleniyordu: %+v — "+
				"uygulama diskine hiç bağlanmaz ve verisi dağıtımda kaybolur",
				o.Index, len(o.Volumes), o.Volumes)
		}
		byName := map[string]bool{}
		for _, v := range o.Volumes {
			byName[v.VolumeName] = v.ReadOnly
			if v.MountPath == "" {
				t.Errorf("hacim %q bağlama noktasız geçti", v.VolumeName)
			}
		}
		// ⚠ read_only TAŞINMALI: kaybolursa salt-okunur olması gereken
		// hacim yazılabilir bağlanır — sessiz bir yetki genişlemesi.
		if !byName["config"] {
			t.Error("read_only bayrağı sürücüye ulaşmadı — salt-okunur " +
				"hacim YAZILABİLİR bağlanır")
		}
		if byName["data"] {
			t.Error("read_only bayrağı uydurulmuş — yazılabilir hacim " +
				"salt-okunur bağlanır ve uygulama çalışmaz")
		}
	}
}

// TestHealPassesVolumesToRecreatedReplica, İYİLEŞTİRME yolunu da sınar.
//
// Ayrı bir çağrı yolu. Bu yol özellikle sinsi: konteyner gece çöker,
// gözetmen onu yeniden kurar, ve bu kez disksiz açılır. Kimse dağıtım
// yapmadığı için kimse şüphelenmez — ve uygulama verisini boş bir
// dizine yazmaya başlar.
func TestHealPassesVolumesToRecreatedReplica(t *testing.T) {
	life := &fakeLifecycle{materialize: true}
	r := newHarness(t, life, true).rollout

	if _, err := r.Rollback(context.Background(), volumeApplication(),
		store.Release{ID: relOld}); err != nil {
		t.Fatalf("geri alma başarısız: %v", err)
	}

	if len(life.created) == 0 {
		t.Fatal("hiç replika kurulmadı — test bir şey ölçemedi")
	}
	for _, o := range life.created {
		if len(o.Volumes) != 2 {
			t.Errorf("yeniden kurulan replika #%d disksiz: %+v", o.Index, o.Volumes)
		}
	}
}

// TestReplicaVolumesAreACopy, sürücüye giden dilimin uygulama tanımıyla
// PAYLAŞILMADIĞINI doğrular.
//
// Go'da dilim ataması ALTTAKİ DİZİYİ paylaşır; alt katmanlardan biri onu
// değiştirirse kontrol düzlemindeki kayıt da bozulur ve değişiklik
// hiçbir yazma yolundan geçmediği için kaynağı bulunamaz.
func TestReplicaVolumesAreACopy(t *testing.T) {
	life := &fakeLifecycle{materialize: true}
	r := newHarness(t, life, true).rollout

	app := volumeApplication()
	if err := r.Run(context.Background(), app,
		store.Release{ID: relNew}); err != nil {
		t.Fatalf("dağıtım başarısız: %v", err)
	}
	if len(life.created) == 0 {
		t.Fatal("hiç replika kurulmadı")
	}

	life.created[0].Volumes[0].MountPath = "/BOZULDU"

	if app.Volumes[0].MountPath == "/BOZULDU" {
		t.Error("sürücüye giden dilim uygulama tanımıyla PAYLAŞILIYOR — " +
			"alt katman kontrol düzlemindeki kaydı bozabilir")
	}
}

// TestNoVolumesMeansNoMounts, KONTROL GRUBUdur.
//
// Olmasaydı "her zaman bir hacim ekle" diyen bir uygulama da üstteki
// testleri geçerdi.
func TestNoVolumesMeansNoMounts(t *testing.T) {
	life := &fakeLifecycle{materialize: true}
	r := newHarness(t, life, true).rollout

	if err := r.Run(context.Background(), testApplication(),
		store.Release{ID: relNew}); err != nil {
		t.Fatalf("dağıtım başarısız: %v", err)
	}
	for _, o := range life.created {
		if len(o.Volumes) != 0 {
			t.Errorf("hacimsiz uygulamaya hacim uyduruldu: %+v", o.Volumes)
		}
	}
}
