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
// yapılandırmaya dönüyor. Uzlaştırma panelyd açılışında da çağrılmalı.
package deploy

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/erkanrzgc/panely/internal/execclient"
	"github.com/erkanrzgc/panely/internal/proxydrv"
	"github.com/erkanrzgc/panely/internal/store"
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
}

// Reconciler, durumu ters vekile yansıtır.
type Reconciler struct {
	deployments Deployments
	replicas    Replicas
	proxy       Proxy
	admin       proxydrv.Admin
}

// New, uzlaştırıcıyı kurar.
//
// Admin bloğu ZORUNLU ve burada da kontrol ediliyor: admin'siz bir
// yapılandırma yüklenirse Caddy varsayılan TCP :2019'a döner ve panelyd
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
				"panelyd'yi Caddy'den kalıcı olarak kilitler")
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
	res := Result{Skipped: map[string]string{}}

	deps, err := rc.deployments.ActiveDeployments(ctx)
	if err != nil {
		return res, fmt.Errorf("deploy: aktif dağıtımlar okunamadı: %w", err)
	}

	routes := make([]proxydrv.AppRoute, 0, len(deps))
	for _, d := range deps {
		if d.Domain == "" {
			// Alan adı olmayan uygulama geçerli: yalnızca iç ağdan
			// erişilir ve vekilde hiç görünmez. Atlanan DEĞİL, kapsam
			// dışı.
			continue
		}

		ups, why := rc.upstreamsFor(ctx, d)
		if why != "" {
			res.Skipped[d.AppID] = why
			continue
		}
		routes = append(routes, proxydrv.AppRoute{
			AppID:     d.AppID,
			Domain:    d.Domain,
			Upstreams: ups,
		})
		res.Routed = append(res.Routed, d.AppID)
	}

	cfg, err := proxydrv.BuildConfig(proxydrv.BuildOptions{
		Admin:  rc.admin,
		Routes: routes,
	})
	if err != nil {
		return res, fmt.Errorf("deploy: vekil yapılandırması üretilemedi: %w", err)
	}

	// Load, yüklemekle kalmıyor GERİ DE OKUYOR: "200 aldım", canlı
	// yapılandırmanın benimki olduğunu kanıtlamaz (K-054).
	if err := rc.proxy.Load(ctx, cfg); err != nil {
		return res, fmt.Errorf("deploy: vekil yapılandırması yüklenemedi: %w", err)
	}

	sort.Strings(res.Routed)
	return res, nil
}

// upstreamsFor, tek bir dağıtımın arka uç adreslerini KURAR.
//
// Boş bir sebep dizesi "sorun yok" demek; dolu olan atlama gerekçesidir.
func (rc *Reconciler) upstreamsFor(ctx context.Context, d store.Deployment) ([]proxydrv.Upstream, string) {
	reps, err := rc.replicas.ListReplicas(ctx, d.AppID)
	if err != nil {
		return nil, fmt.Sprintf("konteynerler listelenemedi: %v", err)
	}

	// Belirlenimli sıra: aynı durumdan aynı JSON çıkmazsa geri okuma
	// karşılaştırması her yüklemede gürültü üretirdi. Docker'ın liste
	// sırası garanti değil.
	sort.Slice(reps, func(i, j int) bool { return reps[i].Index < reps[j].Index })

	var (
		ups     []proxydrv.Upstream
		notMine int
		notUp   int
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
				"%d başka sürümden, %d hazır değil)",
			d.ReleaseID, len(reps), notMine, notUp)
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
