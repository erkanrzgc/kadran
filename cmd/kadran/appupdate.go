package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"sort"

	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
)

// appUpdateFlags, `app update`'in kabul ettiği seçeneklerin değerleridir.
type appUpdateFlags struct {
	domain   string
	branch   string
	health   string
	replicas uint

	// env BİRLEŞTİRİLİR, envRemove siler. Yukarıdaki dört alandan farklı
	// olarak bunların "açıkça boş verildi" hâli yok: proto3 harita
	// alanlarının presence'ı olmadığı için semantik zaten birleştirme
	// (bkz. api.proto UpdateAppRequest.env).
	env       map[string]string
	envRemove []string

	// Hacimler de birlestirilir, ADA gore. Gerekce env ile ayni:
	// proto3'te repeated alanlarin presence'i yok.
	volumes      []*kadranv1.AppVolume
	volumeRemove []string
}

// runAppUpdate, var olan bir uygulamanın alanlarını değiştirir.
func (c *cli) runAppUpdate(ctx context.Context, args []string) int {
	fs := c.newFlagSet("app update")
	var v appUpdateFlags
	fs.StringVar(&v.domain, "domain", "",
		"domain to serve the app on; an empty value (-domain=\"\") REMOVES the app from the proxy")
	fs.StringVar(&v.branch, "branch", "", "default branch")
	fs.StringVar(&v.health, "health-path", "",
		"health check path; an empty value DISABLES the check")
	fs.UintVar(&v.replicas, "replicas", 0, "number of replicas")
	env := c.stringMapFlag(fs, "env",
		"environment variable KEY=VALUE (repeatable); variables not "+
			"named are LEFT ALONE")
	envRemove := c.stringSliceFlag(fs, "env-rm",
		"name of an environment variable to delete (repeatable)")
	volumes := c.volumeFlag(fs, "volume",
		"persistent volume NAME:/mount/point[:ro]; volumes not named are LEFT ALONE")
	volumeRemove := c.stringSliceFlag(fs, "volume-rm",
		"name of a volume to detach (does NOT DELETE the data, only removes the mount)")
	skipDNS := fs.Bool(skipDNSCheckFlag, false, "continue even if the domain's DNS does not point at the server (e.g. Cloudflare proxy)")
	asJSON := fs.Bool("json", false, "machine-readable JSON output")
	timeout := fs.Duration("timeout", defaultTimeout, "overall time limit")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() < 1 || fs.NArg() > 2 {
		return c.usageError("usage: kadran app update [options] <name> [target] — " +
			"options go BEFORE the name")
	}

	// ⚠ VERİLEN seçenekler, DEĞERLERİ değil.
	//
	// Go'nun flag paketi "verilmedi" ile "sıfır değeriyle verildi"yi aynı
	// şeye indirger: `-domain=""` ile `-domain` hiç yazılmamış olması
	// ikisi de boş dize üretir. O ayrımı kaybetmek, alan adına dokunmak
	// istemeyen her komutun onu SESSİZCE silmesi demekti.
	//
	// fs.Visit YALNIZCA gerçekten ayarlanmış seçenekleri gezer. Bu,
	// şemadaki `optional` kararının komut satırı tarafındaki karşılığı:
	// ayrım baştan sona korunuyor.
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })

	// Bayrak yardımcıları kendi depolarını tutuyor; struct'a burada
	// aktarılıyorlar ki buildUpdateRequest bir FlagSet'e bağlı kalmasın
	// ve testten doğrudan çağrılabilsin.
	v.env = *env
	v.envRemove = *envRemove
	v.volumes = *volumes
	v.volumeRemove = *volumeRemove

	req := buildUpdateRequest(fs.Arg(0), v, set)
	if isEmptyUpdate(req) {
		return c.usageError("nothing to change — " +
			"use -domain, -branch, -health-path, -replicas, -env, " +
			"-env-rm, -volume or -volume-rm")
	}

	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()

	// Yalnızca yeni bir alan adı verildiğinde; `-domain=""` vekilden
	// çıkarır, denetlenecek ad yok (K-128).
	if req.Domain != nil {
		if err := c.checkDomain(ctx, req.GetDomain(), fs.Arg(1), *skipDNS); err != nil {
			return c.fail(err)
		}
	}

	conn, _, err := c.connect(ctx, fs.Arg(1))
	if err != nil {
		return c.fail(err)
	}
	defer func() { _ = conn.Close() }()

	resp, err := conn.RPC().UpdateApp(ctx, req)
	if err != nil {
		return c.failUpdate(err)
	}

	if *asJSON {
		body, err := protoToJSON(resp)
		if err != nil {
			return c.fail(err)
		}
		return c.writeJSON(json.RawMessage(body))
	}

	s := resp.GetApp().GetSpec()
	fmt.Fprintf(c.stdout, "App updated: %s\n", s.GetAppId())
	if req.Domain != nil {
		fmt.Fprintf(c.stdout, "  Domain  : %s\n", orNone(s.GetDomain()))
	}
	if req.GitBranch != nil {
		fmt.Fprintf(c.stdout, "  Branch  : %s (used by the next deploy)\n", s.GetGitBranch())
	}
	if req.HealthPath != nil {
		fmt.Fprintf(c.stdout, "  Health  : %s (takes effect on the next deploy)\n", orNone(s.GetHealthPath()))
	}
	if req.Replicas != nil {
		// ⚠ Replika değişikliği İKİ AŞAMALI ve mesaj bunu ayırmak
		// zorunda. Rota HEMEN daralıyor/genişliyor (sunucu tarafında
		// uzlaştırma koşuyor); konteynerlerin fiilen kurulması ya da
		// durdurulması bir sonraki dağıtımda/iyileştirmede oluyor.
		//
		// Tek cümleyle "sonraki dağıtımda etkili" demek, ölçek
		// küçültmede trafiğin ZATEN daraldığını gizlerdi.
		fmt.Fprintf(c.stdout,
			"  Replicas: %d (traffic now, containers on the next deploy)\n",
			s.GetReplicas())
	}

	if len(req.GetEnv()) > 0 || len(req.GetEnvRemove()) > 0 {
		// ⚠ DEĞERLER BASILMIYOR, yalnızca adlar.
		//
		// Terminal çıktısı ekran görüntüsüne, kayıt dosyasına ve hata
		// bildirimine gider. Kullanıcı kendi kutusundaki değeri `docker
		// inspect` ile zaten okuyabilir; onu istemediği bir yere taşıyan
		// taraf biz olmayalım.
		for _, k := range sortedKeys(req.GetEnv()) {
			fmt.Fprintf(c.stdout, "  Env     : %s set\n", k)
		}
		for _, k := range req.GetEnvRemove() {
			fmt.Fprintf(c.stdout, "  Env     : %s DELETED\n", k)
		}
	}

	for _, vol := range req.GetVolumes() {
		mode := "read-write"
		if vol.GetReadOnly() {
			mode = "read-only"
		}
		fmt.Fprintf(c.stdout, "  Disk    : %s -> %s (%s)\n",
			vol.GetName(), vol.GetMountPath(), mode)
	}
	for _, name := range req.GetVolumeRemove() {
		// ⚠ "AYRILDI" deniyor, "silindi" DEGIL. Diskteki veri duruyor ve
		// kullanicinin bunu bilmesi sart: "silindi" okuyan biri veriyi
		// kaybettigini sanar ve yedekten donmeye kalkar.
		fmt.Fprintf(c.stdout, "  Disk    : %s DETACHED (the data stays on disk)\n", name)
	}

	// Ters vekilin durumu SUSULAMAZ. Alan adı değişip trafiğin
	// taşınmaması mümkün ve o durumda "güncellendi" tek başına yanıltıcı.
	if d := resp.GetProxyDetail(); d != "" {
		fmt.Fprintf(c.stdout, "\n%s\n", d)
	}

	// Env uyarısı da SUSULAMAZ ve ayrı basılıyor: env değişikliği
	// çalışan konteynerlere ULAŞMAZ. Bunu yutmak, kullanıcının
	// DATABASE_URL'in devreye girdiğini sanması demek — ve uygulama
	// çalışmayınca hatayı veritabanı tarafında araması.
	if d := resp.GetEnvDetail(); d != "" {
		fmt.Fprintf(c.stdout, "\n%s\n", d)
	}
	// Hacim uyarisi da SUSULAMAZ ve AYRI: env ile hacim ayni anda
	// degismis olabilir ve ikisi farkli sey soylemek zorunda.
	if d := resp.GetVolumeDetail(); d != "" {
		fmt.Fprintf(c.stdout, "\n%s\n", d)
	}
	return exitOK
}

// sortedKeys, haritanın anahtarlarını SABİT sırayla verir.
//
// Go'da harita gezinme sırası kasten rastgele. Sırasız basmak, aynı
// komutun her çalıştırmada farklı çıktı vermesi demekti — ve bu, çıktıyı
// karşılaştıran her betiği (ve her testi) kırılgan yapardı.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// failUpdate, sunucu hatasını kullanıcıya basar.
//
// ── Neden "uygulama güncellenemedi" ÖN EKİ YOK? ─────────────────────
//
// Diğer komutlar hatalarını böyle sarmalıyor ve burada da öyle yazılmıştı.
// Ama bu RPC'nin bir hata yolunda mesaj tam tersini söylüyor: alan adı
// yazıldıktan SONRA ters vekil güncellenemezse sunucu değişikliğin
// KAYDEDİLDİĞİNİ bildiriyor. Ön ekle birlikte terminalde şu çıkıyordu:
//
//	kadran: uygulama güncellenemedi: alan adı ... KAYDEDİLDİ, ama ...
//
// Operatör ilk üç kelimeyi okuyup tam ters sonuca varır — üstelik
// sunucudaki mesajın var olma sebebi tam olarak bunu engellemekti.
//
// Ön ek artık bir SONUÇ İDDİA ETMİYOR, yalnızca hangi komutun konuştuğunu
// söylüyor. Sunucunun mesajları zaten kendi kendini açıklıyor.
//
// ⚠ Bu kusur, sunucu tarafındaki test YEŞİLKEN vardı: test hatanın
// "KAYDEDİLDİ" içerdiğini doğruluyordu ama kullanıcının GÖRDÜĞÜ satırı
// hiç kurmuyordu. Aşağıdaki test o satırı kuruyor.
func (c *cli) failUpdate(err error) int {
	return c.fail(fmt.Errorf("app update: %w", err))
}

// buildUpdateRequest, YALNIZCA komut satırında verilmiş alanları isteğe
// koyar.
//
// Ayrı bir fonksiyon olması testin gereği değil, testin MÜMKÜN olmasının
// şartı: `set` haritasını doğrudan vermek, alan adının BOŞ VERİLMESİ ile
// HİÇ VERİLMEMESİ durumlarını bir FlagSet kurmadan yan yana sınamayı
// sağlıyor.
func buildUpdateRequest(appID string, v appUpdateFlags, set map[string]bool) *kadranv1.UpdateAppRequest {
	req := &kadranv1.UpdateAppRequest{AppId: appID}
	if set["domain"] {
		req.Domain = &v.domain
	}
	if set["branch"] {
		req.GitBranch = &v.branch
	}
	if set["health-path"] {
		req.HealthPath = &v.health
	}
	if set["replicas"] {
		r := uint32(v.replicas) //nolint:gosec // sunucu 1-64 doğruluyor
		req.Replicas = &r
	}
	// ⚠ `set` kontrolü burada da ŞART, "harita boş değilse" yetmez.
	//
	// Boş bir harita göndermek zararsız görünür — sunucu birleştiriyor,
	// boş harita hiçbir şeyi değiştirmez. Ama sunucu "env belirtildi mi"
	// diye bakıp UYARI üretiyor: `-env` hiç yazmayan bir kullanıcı,
	// dokunmadığı bir şey için "yeniden dağıtın" uyarısı alırdı.
	if set["env"] {
		req.Env = v.env
	}
	if set["env-rm"] {
		req.EnvRemove = v.envRemove
	}
	if set["volume"] {
		req.Volumes = v.volumes
	}
	if set["volume-rm"] {
		req.VolumeRemove = v.volumeRemove
	}
	return req
}

func isEmptyUpdate(req *kadranv1.UpdateAppRequest) bool {
	return req.Domain == nil && req.GitBranch == nil &&
		req.HealthPath == nil && req.Replicas == nil &&
		len(req.GetEnv()) == 0 && len(req.GetEnvRemove()) == 0 &&
		len(req.GetVolumes()) == 0 && len(req.GetVolumeRemove()) == 0
}

// orNone, boş değeri görünür kılar.
//
// Boş bir satır ("Alan adı: ") kullanıcıya değerin ne olduğunu değil,
// çıktının bozuk olduğunu düşündürür.
func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}
