// kadrand, Kadran'ın kontrol düzlemi daemon'ıdır.
//
// YETKİSİZ çalışır. Docker grubunda değildir, hiçbir yeteneği (capability)
// yoktur ve ayrıcalık gerektiren her şeyi executor'a tipli bir RPC olarak
// gönderir. `sudo -u kadran docker ps` BAŞARISIZ OLMAK ZORUNDADIR — ürünün
// tamamı bu iddiaya dayanır.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"os/user"
	"strconv"
	"syscall"
	"time"

	"github.com/erkanrzgc/kadran/internal/alarm"
	"github.com/erkanrzgc/kadran/internal/api"
	"github.com/erkanrzgc/kadran/internal/audit"
	"github.com/erkanrzgc/kadran/internal/deploy"
	"github.com/erkanrzgc/kadran/internal/execclient"
	"github.com/erkanrzgc/kadran/internal/grpcserve"
	"github.com/erkanrzgc/kadran/internal/health"
	"github.com/erkanrzgc/kadran/internal/liveness"
	"github.com/erkanrzgc/kadran/internal/logutil"
	"github.com/erkanrzgc/kadran/internal/proxydrv"
	"github.com/erkanrzgc/kadran/internal/sdnotify"
	"github.com/erkanrzgc/kadran/internal/sockets"
	"github.com/erkanrzgc/kadran/internal/store"
	"github.com/erkanrzgc/kadran/internal/vault"
	"github.com/erkanrzgc/kadran/internal/version"
)

const (
	defaultSocket      = "/run/kadran/api.sock"
	defaultExecSocket  = "/run/kadran-exec/exec.sock"
	defaultCaddySocket = "/run/kadran-caddy/admin.sock"
	defaultDB          = "/var/lib/kadran/kadran.db"
	defaultClientGroup = "kadran-client"
	// defaultVaultRecipient, kasanın AÇIK anahtarı (K-123). root 0644:
	// daemon okur, yazamaz. Yol bayrak varsayılanı, birim dosyasında yazılı
	// değil: ExecStart'ı yeniden yazan bir drop-in yeni bayrağı düşürürdü.
	defaultVaultRecipient = "/etc/kadran/vault.pub"
)

func main() {
	if err := run(); err != nil {
		slog.Error("daemon başlatılamadı", "hata", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		socketPath  = flag.String("socket", defaultSocket, "unix socket served to clients")
		execSocket  = flag.String("exec-socket", defaultExecSocket, "executor socket")
		caddySocket = flag.String("caddy-socket", defaultCaddySocket, "reverse proxy admin socket")
		dbPath      = flag.String("db", defaultDB, "SQLite database path")
		clientGroup = flag.String("client-group", defaultClientGroup, "group that may access api.sock")
		vaultPub    = flag.String("vault-recipient", defaultVaultRecipient, "vault public key file (K-123)")
		showVersion = flag.Bool("version", false, "print the version and exit")
		debug       = flag.Bool("debug", false, "verbose logging (also enabled by KADRAN_DEBUG=1)")

		backupEvery = flag.Duration("backup-interval", defaultBackupInterval,
			"scheduled backup interval (0 = off)")
		restoreFrom = flag.String("restore", "",
			"restore the given backup and exit (the daemon must be STOPPED)")

		diskEvery = flag.Duration("disk-check-interval", defaultDiskInterval,
			"disk usage check interval (0 = off)")
	)
	flag.Parse()

	if *showVersion {
		fmt.Printf("kadrand %s (%s) protocol %d\n", version.Version, version.Commit, version.Protocol)
		return nil
	}

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: logutil.Level(*debug, os.Getenv),
	})))

	// ── Ürünün merkezî iddiası ───────────────────────────────────────
	// kadrand root çalışıyorsa executor ayrımı dekoratiftir: ele geçirilen
	// daemon zaten her şeyi yapabilir. Bu yüzden root ile başlamayı
	// reddediyoruz. Executor'daki kontrolün tam aynası (o root OLMALI).
	if os.Geteuid() == 0 {
		return errors.New(
			"kadrand must not run as root — the executor split would be pointless. " +
				"check that the systemd unit file has User=kadran")
	}

	// ── Geri yükleme: veritabanı AÇILMADAN önce ─────────────────────
	//
	// store.Open göçleri uygular ve dosyayı yaratır. Geri yükleme tam da
	// o dosyayı değiştireceği için önce koşmak ZORUNDA; sonra koşsaydı
	// açtığımız tutamağın altından dosyayı çekerdik.
	//
	// Root kontrolünden SONRA: geri yükleme root çalışırsa dosyanın
	// sahibi root olur ve kadrand bir daha yazamaz — sessiz ve
	// açıklanması zor bir arıza.
	if *restoreFrom != "" {
		return runRestore(*dbPath, *socketPath, *restoreFrom)
	}

	clientGID, err := lookupGID(*clientGroup)
	if err != nil {
		return err
	}

	db, err := store.Open(context.Background(), *dbPath)
	if err != nil {
		return fmt.Errorf("could not open the database: %w", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			slog.Error("veritabanı kapatılamadı", "hata", err)
		}
	}()

	// Kasa zorunlu (K-123): açılamazsa daemon da açılmıyor. Eski sürümden
	// gelen düz değerler burada, API dinlemeye başlamadan mühürleniyor.
	sealer, err := vault.LoadSealer(*vaultPub)
	if err != nil {
		return err
	}
	sealed, err := db.EnableVault(context.Background(), sealer)
	if err != nil {
		return fmt.Errorf("could not open the vault: %w — restore the lost key from its backup to "+
			"/var/lib/kadran-exec/vault.key and run the installer again", err)
	}
	slog.Info("kasa açık", "alici", sealer.Recipient(), "muhurlenen_deger", sealed)

	exec, err := execclient.Dial(*execSocket)
	if err != nil {
		return err
	}
	defer func() {
		if err := exec.Close(); err != nil {
			slog.Error("executor bağlantısı kapatılamadı", "hata", err)
		}
	}()

	// Executor'a ulaşılamaması başlangıçta ÖLÜMCÜL DEĞİLDİR.
	//
	// Reddedip çıkmak systemd ile bir yeniden başlatma döngüsü yaratır ve
	// operatör hiçbir teşhis aracına erişemez. Uyarıp devam etmek, durum
	// ekranının "executor erişilemiyor" demesini sağlar — sorunu gizlemek
	// değil, görünür kılmak.
	probeExecutor(exec)

	// ── Ters vekil ───────────────────────────────────────────────────
	//
	// Yapılandırma SQLite'tan ÜRETİLİYOR ve bütün olarak yükleniyor;
	// Caddy'de kısmi güncelleme yok. Admin bloğu her yüklemede gitmek
	// zorunda: onsuz bir yükleme Caddy'yi varsayılan TCP :2019'a döndürür
	// ve kadrand unix soketinden bir daha ULAŞAMAZ.
	proxy := proxydrv.New(*caddySocket)
	reconciler, err := deploy.New(db, exec, proxy, proxydrv.Admin{
		// `fd/3`: soketi systemd yaratıyor, Caddy onu devralıyor.
		// Gerekçe deploy/systemd/kadran-caddy-admin.socket dosyasında.
		Listen: "fd/3",
		// Boş bırakılırsa Caddy HER isteği 403 "host not allowed" ile
		// reddeder (gerçek sunucuda ölçüldü).
		Origins: []string{"localhost"},
	})
	if err != nil {
		return err
	}

	// Yoklama süresi kapı aralığından KISA: aksi hâlde her tur uzar ve
	// toplam süre sınırı içine sığan ölçüm sayısı düşerdi.
	rollout, err := deploy.NewRollout(exec, db, reconciler,
		deploy.NewHTTPProber(deploy.DefaultProbeTimeout),
		deploy.DefaultGate, deploy.DefaultDrain)
	if err != nil {
		return err
	}

	// ── K-055'in yükümlülüğü ─────────────────────────────────────────
	//
	// Ters vekil `--resume` KULLANMIYOR: her başlayışında rotasız bir
	// yapılandırmaya dönüyor (ölçüldü — reboot sonrası :80/:443 hiç
	// dinlenmiyor). Gerçeğin kaynağı SQLite olduğu için doğru davranış bu,
	// ama kadrand açılışta uzlaştırmak ZORUNDA: aksi hâlde bir reboot
	// bütün siteleri sessizce yayından kaldırırdı.
	//
	// Başarısızlık ÖLÜMCÜL DEĞİL: executor yoklamasındaki gerekçenin
	// aynısı — reddedip çıkmak systemd ile bir yeniden başlatma döngüsü
	// yaratır ve operatör hiçbir teşhis aracına erişemez.
	proxyProblem := reconcileAtStartup(reconciler)

	// Uzlaştırıcı hem rollout'a hem API'ye veriliyor: `app update` alan
	// adı değiştiğinde trafiği DAĞITIM BEKLEMEDEN taşıyabilmeli.
	service, err := api.NewServer(api.ServerOptions{
		Store: db, Executor: exec, Rollout: rollout, Reconciler: reconciler,
	})
	if err != nil {
		return err
	}

	// İki aşamalı çağıran doğrulaması:
	//
	//  1. SO_PEERCRED — api.sock'a yalnızca kadran-client grubundaki
	//     süreçler bağlanabilir. O grubun tek üyesi, zorlanmış komuta
	//     bağlanmış istemci SSH kullanıcısıdır.
	//  2. Kimlik önsözü — kadran-connect, sshd'nin ortam değişkenlerinden
	//     türettiği SSH kimliğini bağlantı başında yazar.
	//
	// UYARI: SO_PEERCRED yalnızca BİRİNCİL grubu bildirir, bu yüzden
	// bootstrap istemci kullanıcısını `useradd -g kadran-client` ile
	// oluşturmalıdır — `-G` ile değil.
	creds, err := api.TransportCredentials(uint32(clientGID)) //nolint:gosec // gid daima 32 bite sığar
	if err != nil {
		return fmt.Errorf("could not set up caller verification: %w", err)
	}

	if err := sockets.EnsureParentDir(*socketPath); err != nil {
		return err
	}
	listener, err := sockets.Listen(sockets.ListenOptions{
		Path: *socketPath,
		Mode: 0o660,
		GID:  clientGID,
	})
	if err != nil {
		return err
	}

	// Önleyiciler (günlük + yetki, tekli VE akış) kurucunun içinde:
	// testler aynı kurucuyu kullanıyor (K-131).
	server := api.NewGRPCServer(service, creds)

	recordStartup(db)

	// ── Sağlık gözetmeni ────────────────────────────────────────────
	//
	// Kapanış bağlamı BURADA kuruluyor ve hem gözetmene hem gRPC
	// sunucusuna veriliyor: tek bir iptal kaynağı, tanımlı bir kapanış.
	//
	// Uzlaştırmadan SONRA başlıyor. Önce başlasaydı, henüz ayağa
	// kalkmamış konteynerleri "çökmüş" diye okuyup açılışın normal geçiş
	// anında gereksiz iyileştirme tetiklerdi.
	shutdown, stopSignals := signal.NotifyContext(
		context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stopSignals()

	// ── Alarm ────────────────────────────────────────────────────────
	//
	// Hedef şimdilik journal. Teslimat (Telegram/webhook) AYRI bir karar:
	// kadrand'nin systemd birimi `IPAddressDeny=any` taşıyor ve dışarı
	// çıkamıyor — ölçüldü, kısıtlı ortamda curl DNS bile çözemedi.
	// Tespit o kararı beklemek zorunda değil.
	alarms := alarm.New(db, alarm.LogSink{})

	// Her döngü BAŞLAMADAN kaydediliyor: damga kayıt anında taze başlıyor
	// ve watchdog ilk turundan itibaren hepsini görüyor (K-115).
	beats := liveness.NewRegistry(time.Now)

	supOpts := health.DefaultOptions
	supOpts.Progress = registerLoop(beats, "gözetmen", supOpts.Interval).Mark
	supervisor, err := health.New(
		rollout, db, db, alarms, health.SystemClock(), supOpts)
	if err != nil {
		return err
	}
	go func() { _ = supervisor.Run(shutdown) }()

	// Ters vekil açılışta uzlaştırılamadıysa TRAFİK AKMIYOR demektir.
	// Bu, koddaki en yüksek ciddiyetli koşul ve şimdiye kadar tek izi
	// bir slog.Error satırıydı.
	recordProxyAlarm(shutdown, alarms, proxyProblem)

	// K-055'in ikinci yarısı: ters vekil yeniden başlayınca rotasız açılıyor.
	// İzleyici kaybolan rotaları geri yüklüyor (bkz. proxywatch.go, K-112).
	go watchProxy(shutdown, &proxyWatcher{rp: reconciler, am: alarms}, proxyWatchInterval,
		registerLoop(beats, "vekil-izleyici", proxyWatchInterval))

	go watchDisk(shutdown, exec, alarms, *diskEvery, registerLoop(beats, "disk", *diskEvery))

	// Zamanlı yedekleme. Gözetmenle aynı kapanış bağlamını paylaşıyor:
	// tek iptal kaynağı, tanımlı kapanış.
	go runBackupScheduler(shutdown, db, alarms, *backupEvery,
		registerLoop(beats, "yedek", *backupEvery))

	startWatchdog(shutdown, beats, db.DB())

	slog.Info("daemon hazır",
		"surum", version.Version,
		"soket", *socketPath,
		"istemci_grubu", *clientGroup,
		"veritabani", *dbPath,
	)

	// ⚠ READY, "trafik akıyor" DEMEK DEĞİL — ve systemd'ye bunu
	// söylemek zorundayız.
	//
	// Ters vekil yapılandırması kadrand tarafından kuruluyor ve Caddy
	// yeniden başladığında rotasız açılıyor. Açılış uzlaştırması
	// başarısız olursa: systemd `active (running)` gösterir, `kadran
	// status` sağlıklı der, BÜTÜN SİTELER KAPALIDIR ve tek iz bir
	// slog.Error satırıdır.
	//
	// READY yine de gönderiliyor — göndermemek systemd'de yeniden
	// başlatma döngüsü yaratır ve operatör hiçbir teşhis aracına
	// erişemez. Bunun yerine STATUS metni gerçeği taşıyor: artık
	// `systemctl status kadrand` sorunu ilk satırda gösteriyor.
	if proxyProblem != "" {
		_ = sdnotify.Status("BOZUK: " + proxyProblem)
	} else {
		_ = sdnotify.Status("hazır — ters vekil uzlaştırıldı")
	}
	if err := sdnotify.Ready(); err != nil && !errors.Is(err, sdnotify.ErrNoSocket) {
		slog.Warn("systemd bilgilendirilemedi", "hata", err)
	}

	return grpcserve.RunContext(shutdown, server, listener)
}

// probeExecutor, executor'a ulaşılıp ulaşılamadığını günlüğe yazar.
func probeExecutor(exec *execclient.Client) {
	ctx, cancel := context.WithTimeout(context.Background(), execclient.DefaultTimeout)
	defer cancel()

	ping, err := exec.Ping(ctx)
	if err != nil {
		slog.Warn("executor'a ulaşılamıyor — ayrıcalıklı işlemler çalışmayacak", "hata", err)
		return
	}
	slog.Info("executor bağlandı", "surum", ping.Version, "protokol", ping.ProtocolVersion)
}

// recordStartup, daemon başlangıcını denetim zincirine yazar.
//
// Bu, zincire yazılan ilk gerçek olaydır ve iki işe yarar: yeniden
// başlatmalar denetim izinde görünür (beklenmedik bir yeniden başlatma
// araştırılması gereken bir olaydır), ve zincirin gerçekten çalıştığı
// kurulumdan hemen sonra doğrulanabilir olur.
func recordStartup(db *store.Store) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rec := audit.Record{
		Actor:      audit.SystemActor("daemon"),
		Action:     "daemon.start",
		Target:     "kadrand",
		ParamsJSON: fmt.Sprintf(`{"version":%q,"protocol":%d}`, version.Version, version.Protocol),
		Outcome:    audit.OutcomeSuccess,
		Source:     audit.SourceDaemon,
	}

	// Denetim kaydı yazılamıyorsa bu ciddi bir sorundur ama daemon'ı
	// durdurmaz: kaydı olmayan bir servis, hiç olmayan bir servisten
	// iyidir ve sorun günlükte görünür.
	if _, err := db.AppendAudit(ctx, rec); err != nil {
		slog.Error("başlangıç denetim kaydı yazılamadı", "hata", err)
	}
}

func lookupGID(name string) (int, error) {
	g, err := user.LookupGroup(name)
	if err != nil {
		return 0, fmt.Errorf(
			"group %q not found — has `kadran bootstrap` been run?: %w", name, err)
	}
	id, err := strconv.Atoi(g.Gid)
	if err != nil {
		return 0, fmt.Errorf("could not parse the gid of group %q: %w", name, err)
	}
	return id, nil
}

// startupReconcileTimeout, açılıştaki uzlaştırmanın süre sınırı.
//
// Sınırsız bırakmak, cevap vermeyen bir admin soketinde kadrand'nin hiç
// dinlemeye başlamamasına yol açardı: teşhis aracının kendisi kaybolurdu.
const startupReconcileTimeout = 30 * time.Second

// reconcileAtStartup, ters vekili SQLite'taki duruma getirir.
//
// Sonucu YUTMUYOR: rotalanamayan uygulamalar adlarıyla günlüğe yazılıyor.
// Sessiz bir başarı, bütün sitelerin düştüğü bir kurulumla aynı
// görünürdü.
// startupReconcileTries, açılış uzlaştırmasının deneme sayısıdır.
//
// Tek deneme yetmiyordu: en olası başarısızlık sebebi Caddy'nin henüz
// admin soketini açmamış olması ve bu SANİYELER içinde kendiliğinden
// geçiyor. Tek denemede vazgeçmek, geçici bir yarışı kalıcı bir
// kesintiye çeviriyordu.
const startupReconcileTries = 3

// reconcileAtStartup, ters vekili SQLite'taki duruma getirir ve
// başarısızlığı ÇAĞIRANA BİLDİRİR.
//
// Eskiden hiçbir şey döndürmüyordu; sonuç yalnızca günlüğe yazılıyor ve
// hemen ardından koşulsuz READY gönderiliyordu. Yani systemd'ye "hazırım"
// denirken bütün siteler kapalı olabiliyordu.
// startupReconciler, açılış uzlaştırmasının ihtiyaç duyduğu TEK metot.
//
// Somut *deploy.Reconciler yerine arayüz alınıyor ki BAŞARISIZLIK YOLU
// birim testinden geçebilsin. Öncesinde imza somuttu ve o yolu sınamanın
// tek yolu canlıda Caddy'yi durdurmaktı — yani bütün siteleri
// düşürmekti. Sınanamayan bir hata yolu, olmayan bir hata yolu gibi
// davranır.
type startupReconciler interface {
	Reconcile(ctx context.Context) (deploy.Result, error)
}

func reconcileAtStartup(rc startupReconciler) string {
	var last string
	for attempt := 1; attempt <= startupReconcileTries; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), startupReconcileTimeout)
		res, err := rc.Reconcile(ctx)
		cancel()

		if err == nil {
			if len(res.Skipped) > 0 {
				slog.Warn("bazı uygulamalar rotalanamadı", "ayrinti", res.Error())
				return "rotalanamayan uygulama var: " + res.Error()
			}
			slog.Info("ters vekil uzlaştırıldı", "rotalanan", res.Routed)
			return ""
		}

		last = err.Error()
		slog.Warn("ters vekil uzlaştırılamadı, yeniden denenecek",
			"deneme", attempt, "hata", err)
		if attempt < startupReconcileTries {
			time.Sleep(time.Duration(attempt) * time.Second)
		}
	}

	slog.Error("ters vekil açılışta uzlaştırılamadı — TRAFİK AKMIYOR",
		"deneme", startupReconcileTries, "hata", last)
	return "ters vekil uzlaştırılamadı: " + last
}
