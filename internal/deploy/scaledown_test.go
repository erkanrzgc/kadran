package deploy

import (
	"context"
	"strings"
	"testing"

	"github.com/erkanrzgc/panely/internal/execclient"
	panelyv1 "github.com/erkanrzgc/panely/internal/pb/panely/v1"
)

// ── Ölçek küçültme ───────────────────────────────────────────────────
//
// Bulunan hata şuydu: `panely app update -replicas 1` "başarılı" diyor,
// kaydı da doğru yazıyor — ama hostta hiçbir şey değişmiyordu.
// `ensureReplicas` yalnızca [0, Replicas) aralığını KURUYOR, fazlalıkları
// durdurmuyordu; `upstreamsFor` da indekse hiç bakmadan hepsini
// rotalıyordu. Sonuç: 3'ten 1'e inen bir uygulamada üç konteyner de
// trafik almaya devam ediyordu.
//
// İki ayrı iddia var ve ikisi ayrı ayrı sınanmalı:
//   1. TRAFİK fazlalıklara gitmiyor  (uzlaştırıcı)
//   2. Fazlalıklar DURDURULUYOR      (rollout)
//
// Yalnızca birincisi geçseydi konteynerler sonsuza kadar boşta koşardı;
// yalnızca ikincisi geçseydi durdurma anında hâlâ istek alan bir
// konteyner koparılırdı.

// TestScaleDownStopsRoutingToExtraReplicas, TRAFİK iddiasını sınar.
func TestScaleDownStopsRoutingToExtraReplicas(t *testing.T) {
	// Uygulama 1 replikaya indirildi ama hostta hâlâ üç konteyner var.
	deps := fakeDeployments{
		{AppID: "blog", ReleaseID: "r2", Domain: "blog.example.com",
			ContainerPort: 8080, Replicas: 1},
	}
	reps := fakeReplicas{byApp: map[string][]execclient.Replica{
		"blog": {
			running("blog", "r2", 0, "172.18.0.5"),
			running("blog", "r2", 1, "172.18.0.6"),
			running("blog", "r2", 2, "172.18.0.7"),
		},
	}}
	proxy := &fakeProxy{}

	res, err := mustReconciler(t, deps, reps, proxy).Reconcile(context.Background())
	if err != nil {
		t.Fatalf("uzlaştırılamadı: %v", err)
	}
	if len(res.Skipped) != 0 {
		t.Fatalf("uygulama atlandı: %s", res.Error())
	}

	dials := hosts(t, proxy.loaded)["blog.example.com"]
	if len(dials) != 1 {
		t.Fatalf("%d upstream rotalandı, 1 bekleniyordu: %v — ölçek "+
			"küçültmesi trafiği daraltmıyor", len(dials), dials)
	}
	if dials[0] != "172.18.0.5:8080" {
		t.Errorf("rotalanan upstream %q — #0 bekleniyordu", dials[0])
	}
}

// TestScaleUpRoutesEveryReplica, filtrenin FAZLA eleme yapmadığını
// doğrular.
//
// Kontrol grubu: aynı üç konteyner, ama bu kez istenen sayı da 3. Bu test
// olmasaydı `Replicas` yerine sabit 1 yazan bir uygulama da yukarıdaki
// testi geçerdi.
func TestScaleUpRoutesEveryReplica(t *testing.T) {
	deps := fakeDeployments{
		{AppID: "blog", ReleaseID: "r2", Domain: "blog.example.com",
			ContainerPort: 8080, Replicas: 3},
	}
	reps := fakeReplicas{byApp: map[string][]execclient.Replica{
		"blog": {
			running("blog", "r2", 0, "172.18.0.5"),
			running("blog", "r2", 1, "172.18.0.6"),
			running("blog", "r2", 2, "172.18.0.7"),
		},
	}}
	proxy := &fakeProxy{}

	if _, err := mustReconciler(t, deps, reps, proxy).Reconcile(context.Background()); err != nil {
		t.Fatalf("uzlaştırılamadı: %v", err)
	}
	if dials := hosts(t, proxy.loaded)["blog.example.com"]; len(dials) != 3 {
		t.Errorf("%d upstream rotalandı, 3 bekleniyordu: %v — filtre fazla eliyor",
			len(dials), dials)
	}
}

// TestZeroReplicasIsReportedNotSilentlyEmpty, sıfırın SEBEBİYLE birlikte
// bildirildiğini doğrular.
//
// Şema `CHECK (replicas BETWEEN 1 AND 64)` ile sıfırı yasaklıyor, yani
// buraya sıfır ancak eksik doldurulmuş bir Deployment'tan gelebilir.
// İndeks filtresi böyle bir değerle HER replikayı eler ve uygulama
// tamamen rotasız kalır. Tehlike sessizlik: mesaj "ayakta replikası yok"
// olsaydı operatör konteynerlerin peşine düşerdi — oysa konteynerler
// gayet ayakta.
func TestZeroReplicasIsReportedNotSilentlyEmpty(t *testing.T) {
	deps := fakeDeployments{
		{AppID: "blog", ReleaseID: "r2", Domain: "blog.example.com",
			ContainerPort: 8080}, // Replicas KASTEN doldurulmadı
	}
	reps := fakeReplicas{byApp: map[string][]execclient.Replica{
		"blog": {running("blog", "r2", 0, "172.18.0.5")},
	}}

	res, err := mustReconciler(t, deps, reps, &fakeProxy{}).Reconcile(context.Background())
	if err != nil {
		t.Fatalf("uzlaştırılamadı: %v", err)
	}
	why := res.Skipped["blog"]
	if why == "" {
		t.Fatal("sıfır replika atlanmadı — uygulama rotasız kalıp sessiz geçti")
	}
	if !strings.Contains(why, "sıfır") {
		t.Errorf("sebep %q — sıfır replika olduğunu SÖYLEMELİ; "+
			"'ayakta replikası yok' operatörü yanlış yere baktırır", why)
	}
}

// TestHealStopsExtraReplicasAfterScaleDown, DURDURMA iddiasını sınar.
//
// Fazlalıklar silinmiyor, yalnızca durduruluyor (K-061'in gerekçesi:
// tekrar büyütmek gerekirse imajdan kurmaya gerek kalmasın).
func TestHealStopsExtraReplicasAfterScaleDown(t *testing.T) {
	world := &healWorld{replicas: []execclient.Replica{
		runningAt(relNew, 0, healIP),
		runningAt(relNew, 1, "172.20.0.8"),
		runningAt(relNew, 2, "172.20.0.9"),
	}}
	h := newHealHarness(t, world)

	// Uygulama 1 replikaya indirildi; hostta hâlâ üç tane var.
	app := h.app
	app.Replicas = 1

	if _, err := h.rollout.Heal(context.Background(), app, h.rel); err != nil {
		t.Fatalf("iyileştirme başarısız: %v", err)
	}

	if len(world.stoppedReplicas) != 2 {
		t.Fatalf("%d fazlalık durduruldu, 2 bekleniyordu: %v",
			len(world.stoppedReplicas), world.stoppedReplicas)
	}
	for _, k := range world.stoppedReplicas {
		if k.idx < app.Replicas {
			t.Errorf("istenen aralıktaki replika #%d durduruldu — "+
				"ölçek küçültme canlı replikayı indiriyor", k.idx)
		}
	}
}

// TestHealKeepsEveryReplicaWhenCountMatches, durdurmanın SEBEPSİZ
// çalışmadığını doğrular.
//
// Kontrol grubu: aynı üç konteyner, istenen sayı da 3. Bu test olmasaydı
// "her zaman durdur" diyen bir uygulama da yukarıdakini geçerdi.
func TestHealKeepsEveryReplicaWhenCountMatches(t *testing.T) {
	world := &healWorld{replicas: []execclient.Replica{
		runningAt(relNew, 0, healIP),
		runningAt(relNew, 1, "172.20.0.8"),
		runningAt(relNew, 2, "172.20.0.9"),
	}}
	h := newHealHarness(t, world)

	app := h.app
	app.Replicas = 3

	if _, err := h.rollout.Heal(context.Background(), app, h.rel); err != nil {
		t.Fatalf("iyileştirme başarısız: %v", err)
	}
	if len(world.stoppedReplicas) != 0 {
		t.Errorf("sayı tutarken replika durduruldu: %v — durdurma "+
			"koşulsuz çalışıyor", world.stoppedReplicas)
	}
}

// TestScaleDownNeverTouchesAnotherRelease, ölçek küçültmenin YALNIZCA
// aktif sürüme dokunduğunu doğrular.
//
// ── Bu testi mutasyon yazdırdı ──────────────────────────────────────
//
// Durdurma döngüsündeki `rep.ReleaseID != rel.ID` filtresini kaldıran
// mutasyon YEŞİL geçmişti. Sebep mutasyonun zayıflığı değildi: mevcut
// testlerin dünyasında tek sürüm vardı, dolayısıyla filtre hiçbir şey
// yapmıyordu ve kaldırılması gözlenemiyordu.
//
// Oysa filtre tam olarak MAVİ-YEŞİL sırasında taşıyıcı. O anda hostta
// iki sürümün konteynerleri BİRLİKTE duruyor. Filtre olmasaydı, eski
// sürümün indeksi istenen sayının üstünde kalan replikaları ölçek
// fazlası sanılıp indirilirdi — üstelik boşaltma penceresi dolmadan,
// yani hâlâ istek alırken. Eski sürümü indirmek boşaltma yolunun işi,
// bu fonksiyonun değil.
func TestScaleDownNeverTouchesAnotherRelease(t *testing.T) {
	world := &healWorld{replicas: []execclient.Replica{
		// Aktif sürüm: istenen sayı kadar, fazlası yok.
		runningAt(relNew, 0, healIP),
		// ESKİ sürüm: indeksleri istenen sayının ÜSTÜNDE. Sürüm filtresi
		// olmasaydı ikisi de "ölçek fazlası" sanılırdı.
		runningAt(relOld, 1, "172.20.0.8"),
		runningAt(relOld, 2, "172.20.0.9"),
	}}
	h := newHealHarness(t, world)

	app := h.app
	app.Replicas = 1

	if _, err := h.rollout.Heal(context.Background(), app, h.rel); err != nil {
		t.Fatalf("iyileştirme başarısız: %v", err)
	}

	// ⚠ İDDİA ÇAĞRI KAYDINA DEĞİL, DÜNYANIN DURUMUNA BAKIYOR.
	//
	// İlk hâli `stoppedReplicas`'taki sürüm adını kontrol ediyordu ve
	// bu YETMEDİ: durdurma çağrısı yanlış sürüm adını taşıyabiliyordu
	// (gezilen replika `rep.ReleaseID` yerine aktif `rel.ID` ile
	// çağrılırsa). O hâlde kayıt "relNew durduruldu" der, oysa fiilen
	// eski sürümün konteyneri inmiştir. Kaydın kendisi yanılabiliyorsa
	// kayda bakmak ölçüm değildir.
	//
	// Durum sorgusu bu sınıfı kapatıyor: eski sürümün replikaları HÂLÂ
	// çalışıyor olmalı, çağrının nasıl adlandırıldığından bağımsız.
	for _, rep := range world.replicas {
		if rep.ReleaseID == relNew {
			continue
		}
		if rep.State != panelyv1.ContainerState_CONTAINER_STATE_RUNNING {
			t.Errorf("eski sürümün replikası %s#%d artık %s — ölçek "+
				"küçültme yalnızca aktif sürüme dokunmalı; eskiyi indirmek "+
				"boşaltma yolunun işi ve o boşaltma penceresini bekler",
				rep.ReleaseID, rep.Index, rep.State)
		}
	}
}

// runningAt, testApp altında verilen indekste çalışan bir replika üretir.
func runningAt(rel string, idx uint32, ip string) execclient.Replica {
	return running(testApp, rel, idx, ip)
}
