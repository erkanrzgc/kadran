package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"time"

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
	ctx context.Context, db *store.Store, every time.Duration,
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
	takeBackup(ctx, db)

	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			takeBackup(ctx, db)
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
// var olma sebebini ortadan kaldırırdı. Alarm işi (sıradaki dilim) bu
// satırı bir bildirime bağlayacak.
func takeBackup(ctx context.Context, db *store.Store) {
	start := time.Now()
	path, err := db.Snapshot(ctx)
	if err != nil {
		slog.Error("YEDEK ALINAMADI — geri dönüş penceresi eskiyor",
			"hata", err)
		return
	}
	slog.Info("yedek alındı", "dosya", path, "sure", time.Since(start))
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
