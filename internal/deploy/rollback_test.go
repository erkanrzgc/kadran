package deploy

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/erkanrzgc/panely/internal/execclient"
	"github.com/erkanrzgc/panely/internal/store"
)

// newRollbackHarness, geri alma düzeneğini HEDEF sürüme göre kurar.
//
// newHarness hedefi `relNew` diye sabitliyor; geri almada hedef `relOld`.
// Uzlaştırıcı yanlış sürüme göre kurulsaydı testler "trafik taşındı"
// derken aslında hiçbir şeyi doğrulamazdı.
func newRollbackHarness(t *testing.T, life *fakeLifecycle, target string) *harness {
	t.Helper()

	proxy := &fakeProxy{}
	rec, err := New(
		fakeDeployments{{
			AppID: testApp, ReleaseID: target,
			Domain: "example.test", ContainerPort: 8080,
		}},
		fakeReplicas{byApp: map[string][]execclient.Replica{
			testApp: {running(testApp, target, 0, "172.20.0.2")},
		}},
		proxy,
		testAdmin(),
	)
	if err != nil {
		t.Fatalf("uzlaştırıcı kurulamadı: %v", err)
	}

	life.proxy = proxy
	acts := &fakeActivations{}
	prober := &fakeProber{}
	r, err := NewRollout(life, acts, rec, prober, DefaultGate, DefaultDrain)
	if err != nil {
		t.Fatalf("orkestratör kurulamadı: %v", err)
	}
	clock := &fakeClock{now: time.Unix(0, 0)}
	r.clock = clock
	return &harness{
		rollout: r, life: life, acts: acts,
		clock: clock, proxy: proxy, prober: prober,
	}
}

func targetRelease(id string) store.Release {
	return store.Release{AppID: testApp, ID: id, CommitSHA: strings.Repeat("a", 40)}
}

// TestRollbackReusesStoppedContainers, geri almanın imajdan YENİDEN
// KURMADIĞINI doğrular.
//
// ── Neden bu, geri almanın varlık sebebi ────────────────────────────
//
// Boşaltma eski konteynerleri durdurur ama SİLMEZ (K-061) — tam da geri
// alma hızlı olsun diye. Duran konteyneri başlatmak saniyeler, imajdan
// kurmak dakikalar sürer. Faz 1'in 5. kabul ölçütü "saniyeler içinde geri
// gelir" diyor; `CreateReplica` çağrılırsa o ölçüt sessizce kaybolur.
func TestRollbackReusesStoppedContainers(t *testing.T) {
	life := &fakeLifecycle{replicas: bothReleasesUp()}
	h := newRollbackHarness(t, life, relOld)

	recreated, err := h.rollout.Rollback(
		context.Background(), testApplication(), targetRelease(relOld))
	if err != nil {
		t.Fatalf("geri alma başarısız: %v", err)
	}

	if len(life.created) != 0 {
		t.Fatalf("konteyner YENİDEN KURULDU (%d kez) — geri alma dakikalar "+
			"sürer, oysa duran konteyner hostta duruyordu", len(life.created))
	}
	if recreated {
		t.Error("recreated=true bildirildi ama hiçbir şey kurulmadı")
	}
	if len(life.started) == 0 {
		t.Fatal("hiçbir replika başlatılmadı")
	}
	if h.acts.active[testApp] != relOld {
		t.Errorf("aktif sürüm %q, %q bekleniyordu", h.acts.active[testApp], relOld)
	}
}

// TestRollbackRecreatesWhenContainersAreGone, konteyner kaybolduğunda
// geri almanın YİNE DE çalıştığını doğrular.
//
// Host yeniden başlamış, biri elle silmiş ya da replika sayısı artmış
// olabilir. Bu durumda hızlı yol yoktur ama geri alma yine de işe
// yaramalı — alternatifi, eksik replikayla canlıya çıkmak.
func TestRollbackRecreatesWhenContainersAreGone(t *testing.T) {
	// Hostta YALNIZCA canlı sürüm var; hedefin konteyneri yok.
	life := &fakeLifecycle{
		replicas:    []execclient.Replica{running(testApp, relNew, 0, "172.20.0.3")},
		materialize: true,
	}
	h := newRollbackHarness(t, life, relOld)

	recreated, err := h.rollout.Rollback(
		context.Background(), testApplication(), targetRelease(relOld))
	if err != nil {
		t.Fatalf("geri alma başarısız: %v", err)
	}

	if !recreated {
		t.Error("recreated=false bildirildi ama konteyner yeniden kuruldu — " +
			"operatör işlemin neden uzun sürdüğünü göremez")
	}
	if len(life.created) != 1 {
		t.Fatalf("%d konteyner kuruldu, 1 bekleniyordu", len(life.created))
	}
	if got := life.created[0].ReleaseID; got != relOld {
		t.Errorf("YANLIŞ sürüm kuruldu: %q, %q bekleniyordu", got, relOld)
	}
	if h.acts.active[testApp] != relOld {
		t.Errorf("aktif sürüm %q, %q bekleniyordu", h.acts.active[testApp], relOld)
	}
}

// TestRollbackStopsAtHealthGate, geri almanın da sağlık kapısından
// geçtiğini doğrular.
//
// ── Neden bu test kritik ────────────────────────────────────────────
//
// "Bu sürüm daha önce çalışıyordu" bir sağlık kanıtı DEĞİL: imaj bozulmuş,
// bağımlı bir servis düşmüş olabilir. Kapı atlanırsa geri alma, siteyi
// kurtaran değil İKİNCİ KEZ DÜŞÜREN işlem olur — ve bu, operatörün en
// çaresiz olduğu anda gerçekleşir.
//
// İddia "hata döndü" DEĞİL, TRAFİĞE DOKUNULMADIĞI: aktivasyon yazılmamış
// ve hiçbir konteyner durdurulmamış olmalı.
func TestRollbackStopsAtHealthGate(t *testing.T) {
	life := &fakeLifecycle{replicas: bothReleasesUp()}
	h := newRollbackHarness(t, life, relOld)
	h.prober.err = errors.New("bağlantı reddedildi")

	_, err := h.rollout.Rollback(
		context.Background(), testApplication(), targetRelease(relOld))
	if err == nil {
		t.Fatal("sağlıksız sürüme geri alındı")
	}
	if !strings.Contains(err.Error(), "TRAFİK TAŞINMADI") {
		t.Errorf("hata trafiğin taşınmadığını söylemiyor: %v", err)
	}

	if _, wrote := h.acts.active[testApp]; wrote {
		t.Error("kapı geçilmediği hâlde aktif sürüm YAZILDI")
	}
	if len(life.stopped) != 0 {
		t.Errorf("kapı geçilmediği hâlde %d sürüm durduruldu — "+
			"canlı sürüm öldürülmüş olabilir", len(life.stopped))
	}
	if h.proxy.calls != 0 {
		t.Errorf("kapı geçilmediği hâlde ters vekile %d kez yüklendi", h.proxy.calls)
	}
}

// TestRollbackDrainsTheReplacedRelease, geri almadan SONRA yerini
// bıraktığı sürümün durdurulduğunu doğrular.
//
// Durdurulmasaydı iki sürüm birden ayakta kalır ve küçük bir VPS'te
// kaynak iki katına çıkardı. Sıra da önemli: durdurma ters vekil
// yüklendikten SONRA olmalı, yoksa trafiği ALAN konteyner ölür.
func TestRollbackDrainsTheReplacedRelease(t *testing.T) {
	life := &fakeLifecycle{replicas: bothReleasesUp()}
	h := newRollbackHarness(t, life, relOld)

	if _, err := h.rollout.Rollback(
		context.Background(), testApplication(), targetRelease(relOld)); err != nil {
		t.Fatalf("geri alma başarısız: %v", err)
	}

	stopped := life.stoppedReleases()
	if len(stopped) != 1 || stopped[0] != relNew {
		t.Fatalf("durdurulan sürümler %v, [%s] bekleniyordu", stopped, relNew)
	}

	// Hedef sürüm ASLA durdurulmamalı — canlıya yeni aldığımız o.
	for _, s := range stopped {
		if s == relOld {
			t.Fatal("geri alınan sürümün KENDİSİ durduruldu — site düştü")
		}
	}

	// Boşaltma penceresi gerçekten beklendi mi?
	var waited bool
	for _, d := range h.clock.slept {
		if d == DefaultDrain.Window {
			waited = true
		}
	}
	if !waited {
		t.Errorf("boşaltma penceresi beklenmedi (uykular: %v) — "+
			"uçan istekler kopar", h.clock.slept)
	}

	// SIRA: durdurma, ters vekile yüklendikten SONRA olmalı.
	for i, loads := range life.proxyLoadsThen {
		if loads == 0 {
			t.Errorf("durdurma #%d ters vekil yüklenmeden ÖNCE oldu — "+
				"trafiği alan konteyner öldürülmüş olur", i)
		}
	}
}
