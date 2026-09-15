package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"sort"

	panelyv1 "github.com/erkanrzgc/panely/internal/pb/panely/v1"
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
}

// runAppUpdate, var olan bir uygulamanın alanlarını değiştirir.
func (c *cli) runAppUpdate(ctx context.Context, args []string) int {
	fs := c.newFlagSet("app update")
	var v appUpdateFlags
	fs.StringVar(&v.domain, "domain", "",
		"yayınlanacak alan adı; boş verilirse (-domain=\"\") uygulama vekilden ÇIKARILIR")
	fs.StringVar(&v.branch, "branch", "", "varsayılan dal")
	fs.StringVar(&v.health, "health-path", "",
		"sağlık yoklaması yolu; boş verilirse yoklama YAPILMAZ")
	fs.UintVar(&v.replicas, "replicas", 0, "replika sayısı")
	env := c.stringMapFlag(fs, "env",
		"ortam değişkeni ANAHTAR=DEĞER (tekrarlanabilir); adı geçmeyen "+
			"değişkenlere DOKUNULMAZ")
	envRemove := c.stringSliceFlag(fs, "env-rm",
		"silinecek ortam değişkeni adı (tekrarlanabilir)")
	asJSON := fs.Bool("json", false, "makine okunabilir JSON çıktısı")
	timeout := fs.Duration("timeout", defaultTimeout, "toplam süre sınırı")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() < 1 || fs.NArg() > 2 {
		return c.usageError("kullanım: panely app update [seçenekler] <ad> [hedef] — " +
			"seçenekler addan ÖNCE gelir")
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

	req := buildUpdateRequest(fs.Arg(0), v, set)
	if isEmptyUpdate(req) {
		return c.usageError("değiştirilecek bir alan verilmedi — " +
			"-domain, -branch, -health-path, -replicas, -env veya " +
			"-env-rm kullanın")
	}

	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()

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
	fmt.Fprintf(c.stdout, "Uygulama güncellendi: %s\n", s.GetAppId())
	if req.Domain != nil {
		fmt.Fprintf(c.stdout, "  Alan adı: %s\n", orNone(s.GetDomain()))
	}
	if req.GitBranch != nil {
		fmt.Fprintf(c.stdout, "  Dal     : %s (bir sonraki dağıtımda kullanılır)\n", s.GetGitBranch())
	}
	if req.HealthPath != nil {
		fmt.Fprintf(c.stdout, "  Sağlık  : %s (bir sonraki dağıtımda etkili)\n", orNone(s.GetHealthPath()))
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
			"  Replika : %d (trafik hemen, konteynerler sonraki dağıtımda)\n",
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
			fmt.Fprintf(c.stdout, "  Env     : %s ayarlandı\n", k)
		}
		for _, k := range req.GetEnvRemove() {
			fmt.Fprintf(c.stdout, "  Env     : %s SİLİNDİ\n", k)
		}
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
//	panely: uygulama güncellenemedi: alan adı ... KAYDEDİLDİ, ama ...
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
func buildUpdateRequest(appID string, v appUpdateFlags, set map[string]bool) *panelyv1.UpdateAppRequest {
	req := &panelyv1.UpdateAppRequest{AppId: appID}
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
	return req
}

func isEmptyUpdate(req *panelyv1.UpdateAppRequest) bool {
	return req.Domain == nil && req.GitBranch == nil &&
		req.HealthPath == nil && req.Replicas == nil &&
		len(req.GetEnv()) == 0 && len(req.GetEnvRemove()) == 0
}

// orNone, boş değeri görünür kılar.
//
// Boş bir satır ("Alan adı: ") kullanıcıya değerin ne olduğunu değil,
// çıktının bozuk olduğunu düşündürür.
func orNone(s string) string {
	if s == "" {
		return "(yok)"
	}
	return s
}
