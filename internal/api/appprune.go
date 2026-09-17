package api

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/erkanrzgc/panely/internal/audit"
	panelyv1 "github.com/erkanrzgc/panely/internal/pb/panely/v1"
	"github.com/erkanrzgc/panely/internal/store"
)

// pruneGrace, budanan konteynere SIGTERM ile SIGKILL arasında verilen
// süredir.
//
// `deleteGrace` ile aynı değer ama AYRI bir sabit: ikisi farklı sorulara
// cevap veriyor. Silme, kullanıcının açıkça yok etmek istediği bir
// uygulamayı kapatıyor; budama ise trafiği ÇOKTAN bırakmış bir sürümü.
// Birini değiştirmek istediğimizde ötekini de değiştirmek zorunda
// kalmamalıyız.
const pruneGrace = 10 * time.Second

// PruneApp, eski sürümlerin konteynerlerini kaldırır.
//
// ── Saklama politikası: aktif + bir önceki AKTİF ────────────────────
//
// K-061 duran konteynerleri KASTEN biriktiriyor: duran bir konteyneri
// başlatmak saniyeler, imajdan kurmak dakikalar sürüyor ve geri alma
// bunun üstüne oturuyor. Dolayısıyla budama geri alma hedefine
// dokunamaz — dokunursa K-061'in gerekçesi çöker ve `panely rollback`
// sessizce yavaşlar.
//
// ⚠ "Bir önceki" dağıtım GEÇMİŞİNDEN okunuyor, `releases.seq`'ten
// DEĞİL. Göç 0005 tam olarak bunun için var: r5 canlıyken r3'e geri
// alınmışsa sıradaki geri alma hedefi r5'tir, r2 değil. Sıra numarası
// aktivasyon geçmişi değildir.
//
// ── Neden trafik riski yok ──────────────────────────────────────────
//
// Uzlaştırıcı YALNIZCA aktif sürümün replikalarını rotalıyor
// (`upstreamsFor` içinde `rep.ReleaseID != d.ReleaseID` elemesi) ve
// sağlık gözetmeni yalnızca `ActiveDeployments`'ı izliyor. Yani aktif
// olmayan bir sürümün konteyneri hiçbir koşulda istek almıyor ve
// gözetmen onu geri getirmeye çalışmıyor. Budama bu iki değişmezin
// üstüne oturuyor.
//
// ── Ölçek fazlaları budanmıyor ──────────────────────────────────────
//
// `app update -replicas 1` sonrası kalan #1, #2 konteynerleri AKTİF
// sürüme ait, yani korunuyorlar. Onları kapatan şey `ensureReplicas`;
// iki mekanizmanın aynı konteyneri hedeflemesi yarış demekti.
func (s *Server) PruneApp(
	ctx context.Context, req *panelyv1.PruneAppRequest,
) (*panelyv1.PruneAppResponse, error) {
	const action = "app.prune"

	appID := req.GetAppId()
	dry := req.GetDryRun()
	tgt := appTarget(appID)
	params := map[string]string{"dry_run": strconv.FormatBool(dry)}

	// ⚠ Boş kimlik "bütün uygulamalar" DEĞİL, hatadır.
	//
	// proto3'te string presence taşımaz: alanı doldurmayı unutan çağıran
	// ile boş gönderen çağıran telde ayırt edilemez. Boşu "hepsi"
	// saysaydık, bir unutma YIKICI bir işlemi her uygulamaya uygulatırdı.
	// "Hepsini buda" isteği CLI'da ListApps üzerinden bir döngü.
	if !appIDPattern.MatchString(appID) {
		return nil, s.denied(ctx, action, tgt, params, fmt.Errorf(
			"uygulama kimliği geçersiz (%q) — boş kimlik 'bütün uygulamalar' "+
				"ANLAMINA GELMEZ, her uygulama tek tek budanır", appID))
	}

	keep, err := keepSet(ctx, s.store, appID)
	if err != nil {
		// ⚠ FAIL-CLOSED. Aktif sürüm bilinemiyorsa hiçbir şey silinmiyor.
		//
		// Alternatif — "korunacak sürüm yok, öyleyse hepsini sil" —
		// `app delete`'in yıkıcılığını kapısız bir komuta kaçırırdı.
		// Hiç dağıtılmamış bir uygulamada budanacak bir şey zaten yok;
		// dağıtılmış ama kaydı okunamayan bir uygulamada ise silmek tam
		// olarak yapılmaması gereken şey.
		_ = s.recordAction(ctx, action, tgt, params, audit.OutcomeDenied,
			"korunacak sürümler belirlenemedi — hiçbir şeye dokunulmadı")
		return nil, appError(err)
	}
	params["kept"] = strconv.Itoa(len(keep))

	stale, err := s.staleReleases(ctx, appID, keep)
	if err != nil {
		return nil, s.completed(ctx, action, tgt, params, err)
	}
	params["stale"] = strconv.Itoa(len(stale))

	var removed uint32
	if !dry {
		removed, err = s.removeReleases(ctx, appID, stale)
		params["containers_removed"] = strconv.FormatUint(uint64(removed), 10)
		if err != nil {
			// Kısmi ilerleme KAYBEDİLMİYOR: sayı parametreye yazıldı ve
			// komut yeniden çalıştırılabilir — ikinci deneme daha az
			// konteyner bulur. Her adım "zaten yok"a dayanıklı.
			return nil, s.completed(ctx, action, tgt, params, err)
		}
	}

	if err := s.completed(ctx, action, tgt, params, nil); err != nil {
		return nil, err
	}
	return &panelyv1.PruneAppResponse{
		AppId:             appID,
		KeptReleases:      keptLabels(keep),
		PrunedReleases:    stale,
		ContainersRemoved: removed,
		// Sabit true: bu RPC imaja ve yetime DOKUNMUYOR. Alanlar K-088'in
		// dersi — kullanıcı "budadım" deyince diskin toparlandığını
		// varsayar, oysa budanan yalnızca konteyner.
		ImagesUntouched:   true,
		OrphansOutOfScope: true,
	}, nil
}

// deploymentReader, saklama kümesinin ihtiyaç duyduğu iki sorgudur.
//
// ── Neden arayüz, doğrudan *store.Store değil ───────────────────────
//
// Executor arayüzüyle birebir aynı gerekçe: BAŞARISIZLIK YOLLARI ancak
// cevap kontrol edilebilirse sınanabilir. Somut tipe bağlı kalınca
// aşağıdaki `default:` dalı — beklenmedik bir veritabanı hatası —
// testte HİÇ uyarılmıyordu ve bir mutasyon bunu ölçtü: dalı sessizce
// yutan değişiklik bütün testleri YEŞİL geçti.
//
// Yutulduğunda ne olurdu: geçici bir okuma hatasında saklama kümesi
// {aktif} olarak kalır ve budama GERİ ALMA HEDEFİNİ SİLER. Sessiz,
// yıkıcı, ve "budama başarılı" diyerek.
type deploymentReader interface {
	ActiveDeployment(ctx context.Context, appID string) (store.Deployment, error)
	PreviousActiveRelease(ctx context.Context, appID string) (string, error)
}

// keepSet, budamanın DOKUNMAYACAĞI sürümleri ve KORUNMA SEBEBİNİ verir.
//
// Sebep de dönüyor çünkü yanıtta görünüyor: "r5 korundu" operatöre en
// çok merak ettiği şeyi söylemiyor — geri alma hâlâ çalışıyor mu?
// "r5 (geri alma hedefi)" söylüyor.
func keepSet(ctx context.Context, d deploymentReader, appID string) (map[string]string, error) {
	active, err := d.ActiveDeployment(ctx, appID)
	if err != nil {
		return nil, err
	}
	keep := map[string]string{active.ReleaseID: "aktif"}

	prev, err := d.PreviousActiveRelease(ctx, appID)
	switch {
	case err == nil:
		// ⚠ Üzerine YAZILMIYOR. Geri alma sonrası aktif sürüm geçmişte
		// de görünebilir; "aktif" etiketi daha güçlü olanı.
		if _, ok := keep[prev]; !ok {
			keep[prev] = "geri alma hedefi"
		}
	case errors.Is(err, store.ErrNoPreviousDeployment):
		// İlk dağıtım: geri alınacak bir şey yok. Bu bir HATA DEĞİL,
		// normal bir dal — korunan tek sürüm aktif olan.
	default:
		return nil, err
	}
	return keep, nil
}

// staleReleases, hostta duran ama KORUNMAYAN sürümleri bulur.
//
// Sürümler kontrol düzleminden değil HOST'tan okunuyor; `app delete`
// ile aynı gerekçe: gerçek olan orada duran konteynerler. `ListReleases`
// üst sınırla kısıtlı ve sürüm sayısı sınırı aşan bir uygulamada bazı
// konteynerler atlanırdı.
//
// ⚠ `ListReplicas` `panely.app_id` ETİKETİYLE süzüyor. Panely'nin
// yaratmadığı konteynerler (elle kurulmuş bir veritabanı, başka bir
// kullanıcının işi) bu şemayla adreslenemez ve budama onları GÖREMEZ.
func (s *Server) staleReleases(
	ctx context.Context, appID string, keep map[string]string,
) ([]string, error) {
	reps, err := s.exec.ListReplicas(ctx, appID)
	if err != nil {
		return nil, fmt.Errorf("konteynerler listelenemedi: %w", err)
	}

	seen := map[string]struct{}{}
	stale := make([]string, 0, len(reps))
	for _, r := range reps {
		if _, ok := keep[r.ReleaseID]; ok {
			continue
		}
		if _, ok := seen[r.ReleaseID]; ok {
			continue
		}
		seen[r.ReleaseID] = struct{}{}
		stale = append(stale, r.ReleaseID)
	}
	// Sıra belirleyici olsun: aynı girdi aynı denetim kaydını üretmeli.
	sort.Strings(stale)
	return stale, nil
}

// removeReleases, verilen sürümlerin konteynerlerini durdurup siler.
//
// ── Durdurma ÖNCE, ve bu YÜK TAŞIYOR ───────────────────────────────
//
// Sürücünün `ContainerRemove`'u `force=true` ile çağırıyor (ölçüldü,
// dockerdrv/container.go), yani tek başına SIGKILL demek. Budanan
// sürümler normalde ÇOKTAN durmuş oluyor — boşaltma onları kapatıyor —
// ama boşaltma başarısız olduysa (K-061'deki `DrainError` dalı) eski
// sürümün konteyneri hâlâ çalışıyor olabilir. `StopRelease` tam o
// durumda devreye giriyor ve `pruneGrace` kadar düzgün kapanma hakkı
// veriyor.
//
// Yani sıra bir nezaket değil: onsuz, boşaltması yarıda kalmış bir
// sürüm budanırken koparılırdı.
func (s *Server) removeReleases(
	ctx context.Context, appID string, releases []string,
) (uint32, error) {
	var removed uint32
	for _, relID := range releases {
		if _, err := s.exec.StopRelease(ctx, appID, relID, pruneGrace); err != nil {
			return removed, fmt.Errorf("sürüm %s durdurulamadı: %w", relID, err)
		}
		n, err := s.exec.RemoveRelease(ctx, appID, relID)
		if err != nil {
			return removed, fmt.Errorf("sürüm %s kaldırılamadı: %w", relID, err)
		}
		removed += n
	}
	return removed, nil
}

// keptLabels, korunan sürümleri "r6 (aktif)" biçiminde sıralı döndürür.
func keptLabels(keep map[string]string) []string {
	out := make([]string, 0, len(keep))
	for id, why := range keep {
		out = append(out, id+" ("+why+")")
	}
	// Harita gezintisi rastgele: sıralamazsak aynı durum her çağrıda
	// başka bir sırayla görünür ve çıktılar karşılaştırılamaz.
	sort.Slice(out, func(i, j int) bool {
		return strings.Compare(out[i], out[j]) < 0
	})
	return out
}
