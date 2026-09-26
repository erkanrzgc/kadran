package bootstrap

import (
	"slices"
	"testing"
)

// Hacim arşivleyicisi (K-111): her şeyi OKUYABİLEN bir birim. Güvenliği
// tek bir satırda değil, birkaç yönergenin BİRLİKTE durmasında yaşıyor.
// Testler her birini ayrı kilitliyor; hepsi sunucuda kontrol gruplu
// ölçüldü.
const hacimBirimi = "panely-volume-backup.service"

// TestVolumeArchiverReadsWithOneCapabilityOnly, birimin uygulama
// verisini okuyabildiğini ve bundan fazlasını YAPAMADIĞINI doğrular.
//
// Boş bırakılırsa veri okunamaz: pfprobe/veri 101:101 0750 (ölçüldü:
// yetkisiz tar "Permission denied"). Fazlası (ör. CAP_DAC_OVERRIDE)
// root'a YAZMA da verir.
func TestVolumeArchiverReadsWithOneCapabilityOnly(t *testing.T) {
	yetki := yonergeDegerleri(t, hacimBirimi, "CapabilityBoundingSet")
	if !slices.Equal(yetki, []string{"CAP_DAC_READ_SEARCH"}) {
		t.Errorf("CapabilityBoundingSet=%q — yalnızca CAP_DAC_READ_SEARCH olmalı", yetki)
	}
	if nnp := yonergeDegerleri(t, hacimBirimi, "NoNewPrivileges"); !slices.Equal(nnp, []string{"yes"}) {
		t.Errorf("NoNewPrivileges=%q — setuid bir ikiliyle yetki yükseltilebilir", nnp)
	}
}

// TestVolumeArchiverCannotReachAnything, her şeyi okuyabilen sürecin
// hiçbir yere ULAŞAMADIĞINI doğrular.
//
// ── Neden adres ailesi yasağı ŞART ───────────────────────────────────
//
// Birim root uid'iyle koşuyor ve root Docker soketinin SAHİBİ: izin
// bitleri onu durdurmuyor. Ölçüldü: kısıtsız birim soketten Docker'a
// bağlandı, `RestrictAddressFamilies=none` ile bağlanamadı. Docker'a
// ulaşan bir süreç host'un tamamıdır. PrivateNetwork yalnızca ağı
// kapatıyor, dosya sistemindeki Unix soketlerini DEĞİL.
func TestVolumeArchiverCannotReachAnything(t *testing.T) {
	if aile := yonergeDegerleri(t, hacimBirimi, "RestrictAddressFamilies"); !slices.Equal(aile, []string{"none"}) {
		t.Errorf("RestrictAddressFamilies=%q — 'none' olmalı; root uid'i Docker soketine bağlanabilir", aile)
	}
	if ag := yonergeDegerleri(t, hacimBirimi, "PrivateNetwork"); !slices.Equal(ag, []string{"yes"}) {
		t.Errorf("PrivateNetwork=%q — uygulama verisini okuyan süreç ağa çıkabilir", ag)
	}
}

// TestVolumeArchiveOutputOutsideDaemonDirs, arşivlerin panelyd'nin
// SİLEMEYECEĞİ ve DEĞİŞTİREMEYECEĞİ bir yerde durduğunu doğrular.
//
// Çıktı dizini daemon'un yazabildiği bir yerde olsaydı ele geçirilen
// panelyd arşivleri silebilir ya da kendi hazırladığı bir arşivi yerine
// koyabilirdi; geri yükleme o veriyi geri getirirdi (K-100'ün sınıfı).
// Ayrıca birim panely KULLANICISIYLA koşsaydı dosyalar daemon'unkiyle
// aynı sahipte doğardı. Grup panely: yükleyici okuyabilsin diye.
// Ölçüldü: panely okuyor, silemiyor, üzerine yazamıyor, yanına dosya
// koyamıyor.
func TestVolumeArchiveOutputOutsideDaemonDirs(t *testing.T) {
	durum := yonergeDegerleri(t, hacimBirimi, "StateDirectory")
	if len(durum) != 1 {
		t.Fatalf("StateDirectory=%q — tek bir çıktı dizini bekleniyordu", durum)
	}
	cikti := "/var/lib/" + durum[0]
	for _, dizin := range daemonYazilabilirYollar(t) {
		if altinda(cikti, dizin) {
			t.Errorf("arşiv dizini %q, daemon'un yazabildiği %q altında", cikti, dizin)
		}
	}
	if yw := yonergeDegerleri(t, hacimBirimi, "ReadWritePaths"); len(yw) != 0 {
		t.Errorf("ReadWritePaths=%q — her şeyi okuyan süreç başka yerlere de yazabilir", yw)
	}
	if ps := yonergeDegerleri(t, hacimBirimi, "ProtectSystem"); !slices.Equal(ps, []string{"strict"}) {
		t.Errorf("ProtectSystem=%q — strict değilse süreç dosya sistemine yazabilir", ps)
	}
	if u := yonergeDegerleri(t, hacimBirimi, "User"); len(u) != 0 {
		t.Errorf("User=%q — arşivler root'a ait olmalı; aksi hâlde sahibi onları silebilir", u)
	}
	if g := yonergeDegerleri(t, hacimBirimi, "Group"); !slices.Equal(g, []string{"panely"}) {
		t.Errorf("Group=%q — yükleyici (panely) arşivleri okuyamaz", g)
	}
	if m := yonergeDegerleri(t, hacimBirimi, "UMask"); !slices.Equal(m, []string{"0027"}) {
		t.Errorf("UMask=%q — 0027 değilse panely grubu arşivlere yazabilir", m)
	}
}

// TestVolumeArchiverFailureIsNotified, arşivleme düşerse Telegram'a
// haber gittiğini doğrular. Günde bir koşan bir yedeğin sessiz
// başarısızlığı, geri yükleme gününe kadar fark edilmez.
func TestVolumeArchiverFailureIsNotified(t *testing.T) {
	if of := yonergeDegerleri(t, hacimBirimi, "OnFailure"); !slices.Contains(of, "panely-notify-failure@%n.service") {
		t.Errorf("OnFailure=%q — hacim yedeği düşerse kimse haber almaz", of)
	}
}

// TestVolumeArchiverTimerSurvivesDowntime, kaçan koşunun telafi
// edildiğini doğrular.
func TestVolumeArchiverTimerSurvivesDowntime(t *testing.T) {
	if p := yonergeDegerleri(t, "panely-volume-backup.timer", "Persistent"); !slices.Equal(p, []string{"true"}) {
		t.Errorf("Persistent=%q — kapalı kalan sunucu o günün hacim yedeğini sessizce atlar", p)
	}
	if oc := yonergeDegerleri(t, "panely-volume-backup.timer", "OnCalendar"); len(oc) == 0 {
		t.Error("OnCalendar yok — zamanlayıcı hiç tetiklenmez")
	}
}
