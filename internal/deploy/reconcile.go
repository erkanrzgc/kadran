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
	"log/slog"
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/erkanrzgc/kadran/internal/appnames"
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

	// gate, her yüklemeyi (durumu OKUMA + YÜKLEME) tek sıraya koyar.
	//
	// Vekil izleyicisi (Repair) ile bir dağıtımın uzlaştırması aynı anda
	// koşabiliyor. Kilit olmadan izleyici yapılandırmayı SetActiveRelease'ten
	// ÖNCEKİ durumdan kurup dağıtımın yüklemesinden SONRA yükleyebilirdi:
	// trafik, boşaltılmakta olan sürüme geri dönerdi. Kilitle her yükleme,
	// kendinden önceki yüklemelerden sonra okunmuş durumu taşıyor.
	gate chan struct{}
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
		return nil, errors.New("deploy: deployment, replica and proxy dependencies are required")
	}
	if admin.Listen == "" {
		return nil, errors.New(
			"deploy: the admin block is required — a config loaded without it " +
				"locks kadrand out of Caddy for good")
	}
	return &Reconciler{deployments: d, replicas: r, proxy: p, admin: admin, gate: make(chan struct{}, 1)}, nil
}

// Result, uzlaştırmanın sonucudur.
type Result struct {
	// Routed, trafiği yönlendirilen uygulamalar.
	Routed []string
	// Skipped, alan adı olduğu hâlde yönlendirilemeyen uygulamalar ve
	// sebepleri.
	Skipped map[string]string
	// SkippedRedirects, uygulanamayan yönlendirme adları ve sebepleri
	// (ad → sebep).
	SkippedRedirects map[string]string
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
	if err := rc.acquire(ctx); err != nil {
		return Result{}, err
	}
	defer rc.release()

	p, err := rc.plan(ctx)
	if err != nil {
		return p.res, err
	}
	routes := p.routes
	if len(p.skippedDomains) > 0 {
		// A skipped new release must not remove the healthy previous route
		// before rollout reports the failure. Preserve only validated live
		// upstreams for the app's known domain; it stays reported as skipped.
		live, err := rc.proxy.Current(ctx)
		if err != nil {
			return p.res, fmt.Errorf("deploy: could not preserve skipped live routes: %w", err)
		}
		routes = p.carrySkippedRoutes(live)
	}
	cfg, err := rc.build(routes)
	if err != nil {
		return p.res, err
	}

	// Load, yüklemekle kalmıyor GERİ DE OKUYOR: "200 aldım", canlı
	// yapılandırmanın benimki olduğunu kanıtlamaz (K-054).
	if err := rc.proxy.Load(ctx, cfg); err != nil {
		return p.res, fmt.Errorf("deploy: could not load the proxy config: %w", err)
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
	// SkippedRedirects, uygulanamayan yönlendirme adları ve sebepleri (ad →
	// sebep; Result.SkippedRedirects ile aynı). Alarmı ETKİLEMEZ: domain
	// rotaları yerinde, yalnız o ad yönlendirilmiyor; izleyici günlüğe yazar.
	SkippedRedirects map[string]string
	// Exact, canlı yapılandırmanın beklenenle İKİ YÖNLÜ birebir aynı
	// olduğu doğrulandıysa true: ne eksik rota, ne gönderilmemiş rota,
	// ne farklı upstream (K-054).
	Exact bool
}

// Repair fixes missing routes, wrong upstreams and unexpected live routes.
// An exact match requires no load.
//
// K-055'in ikinci yarısı: ters vekil `--resume` kullanmıyor, yani yeniden
// başlayınca rotasız açılıyor. "Açılışta uzlaştır" yapılmıştı, "ters
// vekil yeniden başladığında uzlaştır" HİÇ yapılmamıştı. Taze sunucu
// testinde (K-112) yalnızca Caddy yeniden başlatıldı ve site 40
// saniyenin 40'ında da kapalı kaldı; kadrand hiçbir şey fark etmedi.
//
// ── İyileştirme davranışı DEĞİŞMİYOR ────────────────────────────────
//
// Uzlaştırma sağlıksız bir uygulamayı atlıyor ve canlıdaki rotası
// iyileştirme bitene kadar duruyor. Onarım yüklemesi de o rotayı SİLMİYOR: atlanan
// uygulamanın canlı upstream'leri yüklemeye taşınıyor. İlk hâli yalnızca
// beklenen rotaları gönderip o rotayı siliyordu; güvenlik incelemesi
// buldu. Canlıya başkası da yazmış olabileceği için (K-054) taşınan her
// adres proxydrv.LiveUpstreams'de yeniden doğrulanıyor.
func (rc *Reconciler) Repair(ctx context.Context) (RepairResult, error) {
	if err := rc.acquire(ctx); err != nil {
		return RepairResult{}, err
	}
	defer rc.release()

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
		return out, fmt.Errorf("deploy: could not read the live proxy config: %w", err)
	}
	out.Missing = proxydrv.MissingHosts(cfg, live)
	if cfg, err = rc.build(p.carrySkippedRoutes(live)); err != nil {
		return out, err
	}
	// Taşımadan SONRA: atlanan uygulamanın taşınan adları da süzgeçten geçti.
	out.SkippedRedirects = p.res.SkippedRedirects
	// Compare after carrying the validated last known route of unhealthy
	// apps, so drift repair cannot remove their routes while healing waits.
	// This repairs changed upstreams and foreign routes as well as omissions.
	if proxydrv.Matches(cfg, live) == nil {
		out.Exact = true
		return out, nil
	}
	if err := rc.proxy.Load(ctx, cfg); err != nil {
		return out, fmt.Errorf("deploy: could not repair proxy drift: %w", err)
	}
	// Load geri okuyarak İKİ YÖNLÜ doğruladı (K-054).
	out.Exact = true
	return out, nil
}

func (rc *Reconciler) acquire(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case rc.gate <- struct{}{}:
		if err := ctx.Err(); err != nil {
			rc.release()
			return err
		}
		return nil
	}
}

func (rc *Reconciler) release() { <-rc.gate }

// plan, SQLite'taki durumdan rotaları ve atlanan uygulamaları çıkarır.
type plan struct {
	routes         []proxydrv.AppRoute
	skippedDomains map[string]string   // uygulama → alan adı (atlananlar)
	skippedNames   map[string][]string // uygulama → İSTENEN yönlendirme adları (atlananlar)
	res            Result

	// domains: bütün uygulamaların (rotalanan ve atlanan) alan adı → uygulama.
	// redirectTo: kabul edilmiş yönlendirme adı → hedef alan adı.
	domains    map[string]string
	redirectTo map[string]string
}

// acceptRedirect, adı domain için yönlendirme olarak kabul eder ya da
// SkippedRedirects'e sebebiyle yazıp false döner. Yalnız o ad elenir: bir
// çakışma alan adı rotalarını ya da kümenin geri kalanını düşürmez
// (BuildConfig çakışmayı hata sayar, bu yüzden süzme ondan ÖNCE yapılır).
// Aynı ad aynı hedefe tekrar gelirse kabul edilir.
func (p plan) acceptRedirect(name, domain string) bool {
	owner, isDomain := p.domains[name]
	switch {
	case !proxydrv.ValidDomain(name):
		p.res.SkippedRedirects[name] = "invalid name"
	case isDomain:
		p.res.SkippedRedirects[name] = fmt.Sprintf("collides with the domain of app %q", owner)
	case p.redirectTo[name] != "" && p.redirectTo[name] != domain:
		p.res.SkippedRedirects[name] = fmt.Sprintf("already redirects to %q", p.redirectTo[name])
	case p.redirectTo[name] == "" && len(p.redirectTo) >= proxydrv.MaxRedirects:
		// Aracı sınırı aşan kümeyi BÜTÜNÜYLE reddeder; sınırdan sonrası ad ad
		// atlanır (AppID sırası, kararlı). Zaten kabul edilmiş ad sayılmaz.
		p.res.SkippedRedirects[name] = "route set redirect limit reached"
	default:
		p.redirectTo[name] = domain
		return true
	}
	return false
}

// Carrying a previous route protects continuity, not the new desired state.
// Keep Result.Skipped and Result.Routed unchanged so callers still fail an
// operation whose own current release could not be routed.
func (p plan) carrySkippedRoutes(live *proxydrv.Config) []proxydrv.AppRoute {
	routes := p.routes
	// AppID sırası: çakışan yönlendirme adında "ilk gelen kazanır" kararlı kalsın.
	appIDs := make([]string, 0, len(p.skippedDomains))
	for appID := range p.skippedDomains {
		appIDs = append(appIDs, appID)
	}
	sort.Strings(appIDs)
	for _, appID := range appIDs {
		domain := p.skippedDomains[appID]
		ups := proxydrv.LiveUpstreams(live, domain)
		if len(ups) == 0 {
			continue
		}
		// Yeni sürümü rotalanamayan uygulamanın canlı yönlendirmeleri de
		// taşınır; yoksa başarısız bir dağıtım www'yi düşürürdü.
		//
		// Yalnız hâlâ İSTENEN adlar: -no-www / -redirect-rm atlanan uygulamada
		// da hemen etkili olur ("kaldırıldı" deyip 308'i canlıda bırakmak yalan
		// olurdu). Kaldırılan ad SkippedRedirects'e yazılmaz; uygulanamayan
		// değil, istenmeyen addır. Hedef koşulu yapısal: LiveRedirects yalnız
		// TAŞINAN domaine gidenleri verir, yani -domain taşımasından sonra eski
		// domaine giden ad (rotası taşınmıyor) kümeye giremez.
		desired := p.skippedNames[appID]
		var redirects []string
		for _, name := range proxydrv.LiveRedirects(live, domain) {
			if slices.Contains(desired, name) && p.acceptRedirect(name, domain) {
				redirects = append(redirects, name)
			}
		}
		routes = append(routes, proxydrv.AppRoute{AppID: appID, Domain: domain, Upstreams: ups, Redirects: redirects})
	}
	return routes
}

// plan'ı kurar. Çağıran rc.gate'i tutmalı.
func (rc *Reconciler) plan(ctx context.Context) (plan, error) {
	p := plan{skippedDomains: map[string]string{}, skippedNames: map[string][]string{},
		res:     Result{Skipped: map[string]string{}, SkippedRedirects: map[string]string{}},
		domains: map[string]string{}, redirectTo: map[string]string{}}

	deps, err := rc.deployments.ActiveDeployments(ctx)
	if err != nil {
		return p, fmt.Errorf("deploy: could not read active deployments: %w", err)
	}

	// AppID sırası: çakışmada ilk gelen kazanır, sonuç kararlı olsun.
	deps = slices.Clone(deps)
	sort.SliceStable(deps, func(i, j int) bool { return deps[i].AppID < deps[j].AppID })
	for _, d := range deps {
		if d.Domain != "" {
			p.domains[d.Domain] = d.AppID
		}
	}

	p.routes = make([]proxydrv.AppRoute, 0, len(deps))
	for _, d := range deps {
		if d.Domain == "" {
			// Alan adı olmayan uygulama geçerli: yalnızca iç ağdan
			// erişilir ve vekilde hiç görünmez. Atlanan DEĞİL, kapsam
			// dışı.
			continue
		}

		// Veritabanında geçersiz alan adlı bir uygulama zaten olabilir
		// (eski sürümde kabul edilmiş). Bu, proxydrv.FixedConfig'i bütün
		// rota kümesi için başarısız kılardı; yalnız o uygulama atlanır.
		// skippedDomains'e KONMUYOR: geçersiz alan adı için canlı rota
		// taşınmaz.
		if !proxydrv.ValidDomain(d.Domain) {
			p.res.Skipped[d.AppID] = fmt.Sprintf("invalid domain %q: the proxy would reject it", d.Domain)
			continue
		}

		ups, why := rc.upstreamsFor(ctx, d)
		if why != "" {
			p.res.Skipped[d.AppID] = why
			p.skippedDomains[d.AppID] = d.Domain
			p.skippedNames[d.AppID] = appnames.Names(d.Domain, d.WWW, d.Redirects)
			continue
		}
		var redirects []string
		for _, name := range appnames.Names(d.Domain, d.WWW, d.Redirects) {
			if p.acceptRedirect(name, d.Domain) {
				redirects = append(redirects, name)
			}
		}
		p.routes = append(p.routes, proxydrv.AppRoute{
			AppID:     d.AppID,
			Domain:    d.Domain,
			Upstreams: ups,
			Redirects: redirects,
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
		return nil, fmt.Errorf("deploy: could not build the proxy config: %w", err)
	}
	return cfg, nil
}

// upstreamsFor, tek bir dağıtımın arka uç adreslerini KURAR.
//
// Boş bir sebep dizesi "sorun yok" demek; dolu olan atlama gerekçesidir.
func (rc *Reconciler) upstreamsFor(ctx context.Context, d store.Deployment) ([]proxydrv.Upstream, string) {
	reps, err := rc.replicas.ListReplicas(ctx, d.AppID)
	if err != nil {
		return nil, fmt.Sprintf("could not list containers: %v", err)
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
		return nil, "requested replica count is zero — the schema forbids this, " +
			"so the Deployment record was filled in incompletely"
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
			return nil, fmt.Sprintf("address of replica #%d unusable: %v", rep.Index, err)
		}
		ups = append(ups, u)
	}

	if len(ups) == 0 {
		return nil, fmt.Sprintf(
			"active release (%s) has no running replica (%d containers on the host: "+
				"%d from another release, %d not ready, %d over the scale)",
			d.ReleaseID, len(reps), notMine, notUp, extra)
	}
	return ups, ""
}

// WarnSkippedRedirects, uygulanamayan her yönlendirme adını ayrı bir uyarı
// satırı olarak günlüğe yazar. Uzlaştırmayı çağıran her yol (açılış,
// dağıtım, iyileştirme, vekil izleyicisi) bunu kullanır: atlanan ad yalnız
// `app update` cevabında görünseydi, başka yoldan gelen çakışma sessiz kalırdı.
//
// Ad sırasıyla: harita gezintisi rastgele (Error ile aynı gerekçe).
func WarnSkippedRedirects(skipped map[string]string) {
	for _, name := range slices.Sorted(maps.Keys(skipped)) {
		slog.Warn("redirect name could not be applied", "name", name, "reason", skipped[name])
	}
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
