package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"time"

	"github.com/erkanrzgc/panely/internal/alarm"
	"github.com/erkanrzgc/panely/internal/store"
)

// defaultBackupInterval, zamanlı yedekler arasındaki süredir.
//
// Bir saat, "kaybı kabul edilebilir pencere" ile "disk ve I/O maliyeti"
// arasındaki denge. Kontrol düzlemi veritabanı küçük (uygulama tanımları,
// ortam değişkenleri, denetim zinciri) ve `VACUUM INTO` saniyenin altında
// bitiyor; asıl maliyet dosya sayısı, onu da SnapshotKeep sınırlıyor.
//
// SnapshotKeep (24) ile birlikte yaklaşık bir günlük geri dönüş penceresi
// veriyor.
const defaultBackupInterval = time.Hour

// runBackupScheduler, düzenli aralıklarla yedek alır ve bağlam iptal
// edilene kadar çalışır.
//
// ── Neden systemd timer DEĞİL ───────────────────────────────────────
//
// Ayrı bir timer birimi, kurulum betiğine iki dosya daha ve "reboot'tan
// sonra hâlâ etkin mi?" diye ayrı bir kabul ölçütü eklerdi
// ([[systemd-unit-must-survive-reboot]]). Daemon zaten `*sql.DB`
// tutamağını elinde tutuyor ve `VACUUM INTO` süreç içinden tek satır.
// Daha küçük adım bu; timer'a ihtiyaç duyulursa o ayrı bir karardır.
//
// ⚠ Sonuç olarak yedekleme, daemon'ın ÇALIŞMASINA bağlı. panelyd
// çökmüşse yedek de alınmaz. Bu kabul edilmiş bir sınır: çökmüş bir
// daemon zaten veri de üretmiyor, yani kaçırılan yedek yeni bir şey
// taşımazdı.
func runBackupScheduler(
	ctx context.Context, db *store.Store, am *alarm.Manager,
	every time.Duration,
) {
	if every <= 0 {
		slog.Warn("zamanlı yedekleme KAPALI",
			"sebep", "--backup-interval 0 verildi")
		return
	}

	// Açılışta HEMEN bir yedek: aksi hâlde kurulumdan sonraki ilk saat
	// boyunca hiç yedek olmazdı ve operatör bunu ancak ihtiyaç anında
	// fark ederdi. Ayrıca kurulumun doğruluğunu hemen görünür kılıyor —
	// dizin izni yanlışsa bir saat sonra değil, şimdi öğreniyoruz.
	takeBackup(ctx, db, am)

	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			takeBackup(ctx, db, am)
		}
	}
}

// takeBackup, tek bir yedek alır ve sonucu günlüğe yazar.
//
// Başarısızlık ÖLÜMCÜL DEĞİL: yedek alınamaması daemon'ı durdurmaz.
// Göç öncesi yedekle (backup.go) kasten farklı davranıyor — orada
// başarısızlık ölümcül, çünkü hemen ardından GERİ ALINAMAZ bir göç
// geliyor. Burada öyle bir adım yok; durmak, çalışan bir paneli yedek
// diski doldu diye kapatmak olurdu.
//
// ⚠ Ama sessiz de kalmıyor. Sessiz arıza en pahalı arıza: yedek
// alınamadığını ancak geri yüklemeye çalışırken öğrenmek, bu dilimin
// var olma sebebini ortadan kaldırırdı. Bu yüzden başarısızlık artık
// bir ALARM açıyor ve başarı onu kapatıyor.
//
// ── Zamanlı yedek denetim zincirine GİRMİYOR ────────────────────────
//
// Yalnızca `panely backup create` (yani bir İNSANIN isteği) zincire
// yazılıyor. Canlı sunucuda doğrulandı: açılıştaki yedek journal'da
// var, zincirde YOK.
//
// Gerekçe record.go'daki kuralın aynısı: zincir, durum değiştiren
// KASITLI eylemlerin kaydı. Saatte bir otomatik kayıt yılda ~8.760
// satır demek ve gerçekten önemli olan girdileri (dağıtım, silme,
// geri alma) boğardı. Zamanlı yedeğin izi journal'da ve dosyanın
// kendi adında duruyor.
func takeBackup(ctx context.Context, db *store.Store, am *alarm.Manager) {
	start := time.Now()
	snap, err := db.Snapshot(ctx)
	if err != nil {
		slog.Error("YEDEK ALINAMADI — geri dönüş penceresi eskiyor",
			"hata", err)
		// Yedeksiz kalmak KRİTİK: bu, geri dönüş yolunun kendisinin
		// kaybolması demek ve fark edilmesi en geç olan arıza türü —
		// ancak geri yüklemeye ihtiyaç duyulduğu gün anlaşılır.
		am.Raise(ctx, store.Alarm{
			ID:       alarm.KindBackupFailed + ":panely.db",
			Kind:     alarm.KindBackupFailed,
			Target:   "panely.db",
			Severity: store.SeverityCritical,
			Since:    time.Now(),
			// Hata METNİ yazılmıyor: dosya yolu ve disk durumu
			// taşıyabilir, alarm ayrıntısı ise denetim zincirine de
			// düşüyor ve zincir ekle-sadece.
			Detail: "zamanlı yedek alınamıyor — geri dönüş yolu YOK",
		})
		return
	}
	// Başarılı yedek, varsa alarmı kapatır. Kenar tetiklemeli: alarm
	// yoksa sessiz.
	am.Clear(ctx, alarm.KindBackupFailed+":panely.db")
	// Alanlar TEK TEK yazılıyor. İlk hâli struct'ı olduğu gibi
	// veriyordu ve journal'da `dosya="{Path:... Bytes:... Taken:...}"`
	// diye tek bir kalabalık alan çıkıyordu — gerçek sunucuda görüldü.
	slog.Info("yedek alındı",
		"dosya", snap.Path, "bayt", snap.Bytes, "sure", time.Since(start))
}

// runRestore, bir yedeği veritabanının yerine koyar ve süreci sonlandırır.
//
// ── Neden RPC DEĞİL ─────────────────────────────────────────────────
//
// Geri yükleme, ÇALIŞAN daemon'ın altından veritabanını çekmek anlamına
// gelir: açık dosya tanıtıcısı eski inode'u tutmaya devam eder, yazmalar
// artık kimsenin görmediği bir dosyaya gider ve durum "geri yükledim ama
// hiçbir şey değişmedi" diye görünür. Bu yüzden geri yükleme bir API
// çağrısı değil, daemon KAPALIYKEN koşan yerel bir komuttur.
//
// `panely` CLI'ı da kullanılamazdı: o istemci makinesinde duruyor ve
// api.sock üzerinden konuşuyor — yani tam da kapalı olması gereken
// daemon'a.
func runRestore(dbPath, socketPath, snapshotPath string) error {
	// ── Daemon gerçekten kapalı mı? ─────────────────────────────────
	//
	// Soket dosyasının VARLIĞI yetmez: panelyd temiz kapanmadıysa soket
	// dosyası diskte kalır. Ölçülen şey bağlanabilirlik — dinleyen biri
	// varsa daemon ayaktadır.
	probeCtx, cancelProbe := context.WithTimeout(
		context.Background(), 2*time.Second)
	defer cancelProbe()

	var dialer net.Dialer
	if conn, err := dialer.DialContext(probeCtx, "unix", socketPath); err == nil {
		_ = conn.Close()
		return fmt.Errorf(
			"panelyd ÇALIŞIYOR (%s dinleniyor) — geri yükleme reddedildi. "+
				"Çalışan daemon veritabanının eski kopyasını açık tutar ve "+
				"geri yükleme sessizce etkisiz kalır. Önce durdurun: "+
				"systemctl stop panelyd", socketPath)
	}

	ctx := context.Background()
	safety, err := store.Restore(ctx, dbPath, snapshotPath)
	if err != nil {
		return err
	}

	fmt.Printf("geri yüklendi: %s → %s\n", snapshotPath, dbPath)
	if safety != "" {
		fmt.Printf("önceki hâlin güvenlik kopyası: %s\n", safety)
	}
	fmt.Println("daemon'ı başlatın: systemctl start panelyd")

	// ⚠ HACİM VERİSİ KAPSAM DIŞI. Bu dosya kontrol düzlemi
	// veritabanıdır: uygulama tanımları, ortam değişkenleri, denetim
	// zinciri ve dağıtım geçmişi. Uygulamaların kalıcı disklerindeki
	// veri (/var/lib/panely/volumes) yedeklenmiyor ve geri
	// yüklenmiyor — o dizinleri panelyd OKUYAMIYOR (gerçek sunucuda
	// ölçüldü: root:root 0750, panelyd uid 999).
	//
	// Bunu burada yazmak şart: "geri yükledim" diyen operatörün
	// uygulama verisinin de döndüğünü sanması, bu dilimin
	// üretebileceği EN PAHALI yanlış anlama olurdu.
	fmt.Fprintln(os.Stderr,
		"UYARI: yalnızca kontrol düzlemi veritabanı geri yüklendi. "+
			"Uygulamaların kalıcı disk verisi (/var/lib/panely/volumes) "+
			"bu yedeğin KAPSAMINDA DEĞİL.")
	return nil
}
