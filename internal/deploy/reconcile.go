// Package deploy, kontrol düzlemindeki gerçeği ters vekile yansıtır.
//
// # Neden "uzlaştırma", "güncelleme" değil?
//
// Caddy'nin `POST /load` ucu kök nesnenin TAMAMINI değiştiriyor; kısmi
// güncelleme diye bir şey yok. Dolayısıyla her yükleme, TÜM uygulamaların
// durumundan yeniden üretilmek zorunda. Tek bir uygulamadan üretilen bir
// yapılandırma, diğerlerinin rotalarını siler — yani bir dağıtım,
// alakasız bir siteyi internetten düşürür.
//
// Bu aynı zamanda K-055'in doğurduğu yükümlülüğü karşılıyor: ters vekil
// `--resume` kullanmıyor, yani her yeniden başlatmada rotasız bir
// yapılandırmaya dönüyor. Uzlaştırma kadrand açılışında da çağrılmalı.
package deploy

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/erkanrzgc/kadran/internal/execclient"
	"github.com/erkanrzgc/kadran/internal/proxydrv"
	"github.com/erkanrzgc/kadran/internal/store"
)

// Deployments, kontrol düzlemindeki dağıtım durumunu okur.
type Deployments interface {
	ActiveDeployments(ctx context.Context) ([]store.Deployment, error)
}

// Replicas, hostta GERÇEKTEN duran konteynerleri okur.
//
// Ayrı bir arayüz olması kasıtlı: SQLite ne İSTEDİĞİMİZİ biliyor, host ne
// OLDUĞUNU. Rota üretimi ikincisine bakmak zorunda — "kaydettim" ile
// "çalışıyor" arasındaki farkı görmeyen bir yapılandırma, trafiği ölü bir
// konteynere taşır.
type Replicas interface {
	ListReplicas(ctx context.Context, appID string) ([]execclient.Replica, error)
}

// Proxy, üretilen yapılandırmayı yükler ve GERİ OKUR.
type Proxy interface {
	Load(ctx context.Context, cfg *proxydrv.Config) error
	Current(ctx context.Context) (*proxydrv.Config, error)
}

// Reconciler, durumu ters vekile yansıtır.
type Reconciler struct {
	deployments Deployments
	replicas    Replicas
	proxy       Proxy
	admin       proxydrv.Admin

	// mu, her yüklemeyi (durumu OKUMA + YÜKLEME) tek sıraya koyar.
	//
	// Vekil izleyicisi (Repair) ile bir dağıtımın uzlaştırması aynı anda
	// koşabiliyor. Kilit olmadan izleyici yapılandırmayı SetActiveRelease'ten
	// ÖNCEKİ durumdan kurup dağıtımın yüklemesinden SONRA yükleyebilirdi:
	// trafik, boşaltılmakta olan sürüme geri dönerdi. Kilitle her yükleme,
	// kendinden önceki yüklemelerden sonra okunmuş durumu taşıyor.
	mu sync.Mutex
}

// New, uzlaştırıcıyı kurar.
//
// Admin bloğu ZORUNLU ve burada da kontrol ediliyor: admin'siz bir
// yapılandırma yüklenirse Caddy varsayılan TCP :2019'a döner ve kadrand
// unix soketinden bir daha ULAŞAMAZ — yani sistem kendini kalıcı olarak
// kilitler. proxydrv.BuildConfig de aynı kontrolü yapıyor; bu, hatanın
// yükleme anında değil kurulum anında görülmesi için.
func New(d Deployments, r Replicas, p Proxy, admin proxydrv.Admin) (*Reconciler, error) {
	if d == nil || r == nil || p == nil {
		return nil, errors.New("deploy: dağıtım, replika ve vekil bağımlılıkları zorunlu")
	}
	if admin.Listen == "" {
		return nil, errors.New(
			"deploy: admin bloğu zorunlu — onsuz yüklenen yapılandırma " +
				"kadrand'yi Caddy'den kalıcı olarak kilitler")
	}
	return &Reconciler{deployments: d, replicas: r, proxy: p, admin: admin}, nil
}

// Result, uzlaştırmanın sonucudur.
type Result struct {
	// Routed, trafiği yönlendirilen uygulamalar.
	Routed []string
	// Skipped, alan adı olduğu hâlde yönlendirilemeyen uygulamalar ve
	// sebepleri.
	Skipped map[string]string
}

// Reconcile, canlı durumu ters vekile yansıtır.
//
// ── Hasta bir uygulama, SAĞLAM olanları düşürmez ────────────────────
//
// Aktif sürümünün ayakta hiçbir replikası kalmamış bir uygulama
// ATLANIYOR, yükleme iptal edilmiyor. Gerekçe: proxydrv upstream'siz bir
// rotayı reddediyor (haklı olarak — sessiz 502 üretirdi), yani hasta
// uygulamayı yapılandırmaya koyamayız. Ama onun yüzünden yüklemeyi
// tamamen durdurmak, TEK bir bozuk uygulamanın sunucudaki her siteyi
// yayından kaldırması demekti.
//
// Atlananlar sessizce yutulmuyor: Result'ta adlarıyla ve sebepleriyle
// dönüyorlar ve çağıran bunu günlüğe yazıyor.
func (rc *Reconciler) Reconcile(ctx context.Context) (Result, error) {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	p, err := rc.plan(ctx)
	if err != nil {
		return p.res, err
	}
	cfg, err := rc.build(p.routes)
	if err != nil {
		return p.res, err
	}

	// Load, yüklemekle kalmıyor GERİ DE OKUYOR: "200 aldım", canlı
	// yapılandırmanın benimki olduğunu kanıtlamaz (K-054).
	if err := rc.proxy.Load(ctx, cfg); err != nil {
		return p.res, fmt.Errorf("deploy: vekil yapılandırması yüklenemedi: %w", err)
	}

	sort.Strings(p.res.Routed)
	return p.res, nil
}

// RepairResult, bir onarım turunun sonucudur.
type RepairResult struct {
	// Missing, canlıda HİÇ rotası olmadığı için geri yüklenen alan adları.
	Missing []string
	// Skipped, o an rotalanamayan (sağlıksız) uygulamalar ve sebepleri.
	// Doluysa izleyici "her şey yolunda" diyemez.
	Skipped map[string]string
	// Exact, canlı yapılandırmanın beklenenle İKİ YÖNLÜ birebir aynı
	// olduğu doğrulandıysa true: ne eksik rota, ne gönderilmemiş rota,
	// ne farklı upstream (K-054).
	Exact bool
}

// Repair, ters vekil beklenen bir alan adını KAYBETMİŞSE uzlaştırır; her
// şey yerindeyse hiçbir şey yüklemez.
//
// K-055'in ikinci yarısı: ters vekil `--resume` kullanmıyor, yani yeniden
// başlayınca rotasız açılıyor. "Açılışta uzlaştır" yapılmıştı, "ters
// vekil yeniden başladığında uzlaştır" HİÇ yapılmamıştı. Taze sunucu
// testinde (K-112) yalnızca Caddy yeniden başlatıldı ve site 40
// saniyenin 40'ında da kapalı kaldı; kadrand hiçbir şey fark etmedi.
//
// ── İyileştirme davranışı DEĞİŞMİYOR ────────────────────────────────
//
// Tetik proxydrv.MissingHosts: yalnızca EKSİK alan adı. Uzlaştırma
// sağlıksız bir uygulamayı atlıyor ve canlıdaki rotası iyileştirme bitene
// kadar duruyor. Onarım yüklemesi de o rotayı SİLMİYOR: atlanan
// uygulamanın canlı upstream'leri yüklemeye taşınıyor. İlk hâli yalnızca
// beklenen rotaları gönderip o rotayı siliyordu; güvenlik incelemesi
// buldu. Canlıya başkası da yazmış olabileceği için (K-054) taşınan her
// adres proxydrv.LiveUpstreams'de yeniden doğrulanıyor.
func (rc *Reconciler) Repair(ctx context.Context) (RepairResult, error) {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	p, err := rc.plan(ctx)
	if err != nil {
		return RepairResult{}, err
	}
	out := RepairResult{Skipped: p.res.Skipped}
	cfg, err := rc.build(p.routes)
	if err != nil {
		return out, err
	}
	live, err := rc.proxy.Current(ctx)
	if err != nil {
		return out, fmt.Errorf("deploy: canlı vekil yapılandırması okunamadı: %w", err)
	}
	out.Missing = proxydrv.MissingHosts(cfg, live)
	if len(out.Missing) == 0 {
		// Yükleme yok; canlı İKİ YÖNLÜ birebir mi? İzleyici alarmı
		// yalnızca öyleyse kapatır (K-112).
		out.Exact = proxydrv.Matches(cfg, live) == nil
		return out, nil
	}

	routes := p.routes
	for appID, domain := range p.skippedDomains {
		if ups := proxydrv.LiveUpstreams(live, domain); len(ups) > 0 {
			routes = append(routes, proxydrv.AppRoute{AppID: appID, Domain: domain, Upstreams: ups})
		}
	}
	if cfg, err = rc.build(routes); err != nil {
		return out, err
	}
	if err := rc.proxy.Load(ctx, cfg); err != nil {
		return out, fmt.Errorf("deploy: kaybolan rotalar yüklenemedi: %w", err)
	}
	// Load geri okuyarak İKİ YÖNLÜ doğruladı (K-054).
	out.Exact = true
	return out, nil
}

// plan, SQLite'taki durumdan rotaları ve atlanan uygulamaları çıkarır.
type plan struct {
	routes         []proxydrv.AppRoute
	skippedDomains map[string]string // uygulama → alan adı (atlananlar)
	res            Result
}

// plan'ı kurar. Çağıran rc.mu'yu tutmalı.
func (rc *Reconciler) plan(ctx context.Context) (plan, error) {
	p := plan{skippedDomains: map[string]string{}, res: Result{Skipped: map[string]string{}}}

	deps, err := rc.deployments.ActiveDeployments(ctx)
	if err != nil {
		return p, fmt.Errorf("deploy: aktif dağıtımlar okunamadı: %w", err)
	}

	p.routes = make([]proxydrv.AppRoute, 0, len(deps))
	for _, d := range deps {
		if d.Domain == "" {
			// Alan adı olmayan uygulama geçerli: yalnızca iç ağdan
			// erişilir ve vekilde hiç görünmez. Atlanan DEĞİL, kapsam
			// dışı.
			continue
		}

		ups, why := rc.upstreamsFor(ctx, d)
		if why != "" {
			p.res.Skipped[d.AppID] = why
			p.skippedDomains[d.AppID] = d.Domain
			continue
		}
		p.routes = append(p.routes, proxydrv.AppRoute{
			AppID:     d.AppID,
			Domain:    d.Domain,
			Upstreams: ups,
		})
		p.res.Routed = append(p.res.Routed, d.AppID)
	}
	return p, nil
}

// build, rotalardan yüklenecek yapılandırmayı kurar.
func (rc *Reconciler) build(routes []proxydrv.AppRoute) (*proxydrv.Config, error) {
	cfg, err := proxydrv.BuildConfig(proxydrv.BuildOptions{
		Admin:  rc.admin,
		Routes: routes,
	})
	if err != nil {
		return nil, fmt.Errorf("deploy: vekil yapılandırması üretilemedi: %w", err)
	}
	return cfg, nil
}

// upstreamsFor, tek bir dağıtımın arka uç adreslerini KURAR.
//
// Boş bir sebep dizesi "sorun yok" demek; dolu olan atlama gerekçesidir.
func (rc *Reconciler) upstreamsFor(ctx context.Context, d store.Deployment) ([]proxydrv.Upstream, string) {
	reps, err := rc.replicas.ListReplicas(ctx, d.AppID)
	if err != nil {
		return nil, fmt.Sprintf("konteynerler listelenemedi: %v", err)
	}

	// ⚠ Replicas SIFIR OLAMAZ ve sıfırsa sessiz kalmıyoruz.
	//
	// Şema `CHECK (replicas BETWEEN 1 AND 64)` ile bunu garanti ediyor,
	// yani sıfır ancak elle kurulmuş bir Deployment'tan gelebilir —
	// alanı doldurmayı unutan yeni bir kod yolundan. Aşağıdaki indeks
	// filtresi böyle bir değerle HER replikayı eler ve uygulama sessizce
	// tamamen rotasız kalırdı; üstelik hata mesajı "ayakta replikası yok"
	// diyerek operatörü konteynerlerin peşine düşürürdü.
	//
	// Bu yüzden sebep açıkça söyleniyor. Uygulama yine atlanıyor
	// (fail-closed), ama NEDEN atlandığı belli.
	if d.Replicas == 0 {
		return nil, "istenen replika sayısı sıfır — şema bunu yasaklıyor, " +
			"demek ki Deployment kaydı eksik dolduruldu"
	}

	// Belirlenimli sıra: aynı durumdan aynı JSON çıkmazsa geri okuma
	// karşılaştırması her yüklemede gürültü üretirdi. Docker'ın liste
	// sırası garanti değil.
	sort.Slice(reps, func(i, j int) bool { return reps[i].Index < reps[j].Index })

	var (
		ups     []proxydrv.Upstream
		notMine int
		notUp   int
		extra   int
	)
	for _, rep := range reps {
		// ⚠ YALNIZCA AKTİF SÜRÜMÜN replikaları. Blue-green geçişi
		// sırasında hostta iki sürümün konteynerleri AYNI ANDA duruyor;
		// sürüm filtresi olmasaydı trafik eskiyle yeni arasında
		// rastgele bölünürdü — üstelik geçiş "başarılı" görünerek.
		if rep.ReleaseID != d.ReleaseID {
			notMine++
			continue
		}
		if !rep.Routable() {
			// Çalışıyor ama adresi yok: konteyner yeni başlamış ve ağ
			// henüz kurulmamış olabilir. Bu GEÇİCİ bir durum, hata
			// değil — çağıran yeniden denemeli.
			notUp++
			continue
		}
		if rep.Index >= d.Replicas {
			// ⚠ ÖLÇEK KÜÇÜLTMESİNİN FAZLALIKLARI.
			//
			// Replika sayısı 3'ten 1'e indirildiğinde #1 ve #2 hostta
			// ÇALIŞMAYA DEVAM EDER: onları durduran şey `ensureReplicas`
			// ve o da ancak bir sonraki dağıtımda/iyileştirmede koşar.
			// Bu filtre olmasaydı ikisi de trafik almaya devam ederdi ve
			// `app update -replicas 1` "başarılı" deyip HİÇBİR ŞEY
			// değiştirmemiş olurdu.
			//
			// Sıra taşıyıcı: rota ÖNCE daraltılıyor, konteyner SONRA
			// durduruluyor. Tersi, hâlâ istek alan bir konteyneri
			// koparırdı.
			extra++
			continue
		}
		u, err := proxydrv.NewUpstream(rep.IPAddress, d.ContainerPort)
		if err != nil {
			// Adres executor'dan geliyor ve ayrıştırılamıyorsa bu, üst
			// katmanda bir bozulmadır; sessizce atlamak trafiği eksik
			// replikaya sıkıştırırdı.
			return nil, fmt.Sprintf("replika #%d adresi kullanılamaz: %v", rep.Index, err)
		}
		ups = append(ups, u)
	}

	if len(ups) == 0 {
		return nil, fmt.Sprintf(
			"aktif sürümün (%s) ayakta replikası yok (hostta %d konteyner: "+
				"%d başka sürümden, %d hazır değil, %d ölçek fazlası)",
			d.ReleaseID, len(reps), notMine, notUp, extra)
	}
	return ups, ""
}

// Error, atlanan uygulamaları tek satırlık okunabilir bir metne çevirir.
//
// Sıralı: harita gezintisi rastgele olduğundan, aksi hâlde aynı arıza her
// koşuda başka bir sırayla görünür ve günlükler karşılaştırılamazdı.
func (r Result) Error() string {
	if len(r.Skipped) == 0 {
		return ""
	}
	names := make([]string, 0, len(r.Skipped))
	for app := range r.Skipped {
		names = append(names, app)
	}
	sort.Strings(names)

	var b strings.Builder
	for i, app := range names {
		if i > 0 {
			b.WriteString("; ")
		}
		fmt.Fprintf(&b, "%s: %s", app, r.Skipped[app])
	}
	return b.String()
}
