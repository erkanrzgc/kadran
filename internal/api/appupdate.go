package api

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/erkanrzgc/kadran/internal/appnames"
	"github.com/erkanrzgc/kadran/internal/audit"
	"github.com/erkanrzgc/kadran/internal/deploy"
	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
	"github.com/erkanrzgc/kadran/internal/store"
	"github.com/erkanrzgc/kadran/internal/vault"
)

// UpdateApp, var olan bir uygulamanın değiştirilebilir alanlarını yazar.
//
// ── Sıra yük taşıyor ────────────────────────────────────────────────
//
//	oku → birleştir → DOĞRULA → yaz → (alan adı değiştiyse) uzlaştır
//
// Doğrulama BİRLEŞTİRİLMİŞ tanıma yapılıyor, isteğin kendisine değil.
// Deltayı tek başına doğrulamak yetmez: tek başına makul görünen bir
// değişiklik mevcut durumla birleşince geçersiz bir tanım üretebilir.
//
// Uzlaştırma EN SONA bırakıldı ve yazmadan önce yapılamaz: ters vekil
// yapılandırması `apps` tablosundan üretiliyor, yani yazılmamış bir alan
// adı ortaya çıkamaz. Bu, SetActiveRelease'in belgelediği sıranın
// aynısı — önce kontrol düzlemindeki gerçek, sonra ona uyum.
func (s *Server) UpdateApp(
	ctx context.Context, req *kadranv1.UpdateAppRequest,
) (*kadranv1.UpdateAppResponse, error) {
	const action = "app.update"

	appID := req.GetAppId()
	tgt := appTarget(appID)
	params := updateAuditParams(req)
	upd := updateFromProto(req)

	if upd.IsEmpty() {
		return nil, s.denied(ctx, action, tgt, params, errors.New(
			"no field given — this call would only advance the timestamp, "+
				"that is, a silent no-op"))
	}

	// Çelişki kontrolü BİRLEŞTİRMEDEN ÖNCE: `applyEnv` sırayı
	// belirlemiş durumda (önce yaz, sonra sil), yani çelişki buraya
	// ulaşırsa sessizce silme kazanırdı. Kullanıcının iki zıt niyetinden
	// birini sessizce seçmek, seçimi ondan gizlemek demektir.
	if err := validateEnvRemove(req.GetEnv(), req.GetEnvRemove()); err != nil {
		return nil, s.denied(ctx, action, tgt, params, err)
	}
	if err := validateVolumeRemove(req.GetVolumes(), req.GetVolumeRemove()); err != nil {
		return nil, s.denied(ctx, action, tgt, params, err)
	}
	if err := validateRedirectChange(req.GetRedirectAdd(), req.GetRedirectRemove()); err != nil {
		return nil, s.denied(ctx, action, tgt, params, err)
	}

	release, err := s.acquireApp(ctx, appID)
	if err != nil {
		return nil, s.completed(ctx, action, tgt, params, err)
	}
	defer release()

	current, err := s.store.GetApp(ctx, appID)
	if err != nil {
		// Var olmayan bir uygulamaya yazma DENEMESİ de zincire girer:
		// yalnızca başarılı yazmaları kaydeden bir denetim günlüğü,
		// "kim neyi denedi" sorusunu yanıtlayamaz.
		_ = s.recordAction(ctx, action, tgt, params, audit.OutcomeDenied, err.Error())
		return nil, appError(err)
	}

	merged := appToProto(upd.Apply(current)).GetSpec()
	view, err := envForValidation(appID, current.Env, upd)
	if err != nil {
		return nil, s.denied(ctx, action, tgt, params, err)
	}
	merged.Env = view
	if err := validateAppSpec(merged); err != nil {
		return nil, s.denied(ctx, action, tgt, params, err)
	}

	// Alan adının ve replika sayısının GERÇEKTEN değişip değişmediği
	// YAZMADAN ÖNCE saptanmalı: yazdıktan sonra eski değer okunamaz.
	domainMoved := upd.ChangesDomain(current.Domain)

	// ── Replika sayısı da ters vekili İLGİLENDİRİYOR ────────────────
	//
	// Eskiden yalnızca alan adı uzlaştırma tetikliyordu, çünkü rota
	// yalnızca alan adına bağlıydı. Artık DEĞİL: uzlaştırıcı indeksi
	// istenen sayının üstünde kalan replikaları rotalamıyor, yani rota
	// kümesi sayıya da bağlı.
	//
	// Tetiklemeseydik `moveTraffic`'in yorumundaki hata bire bir
	// tekrarlanırdı: kullanıcı 3'ten 1'e iner, komut "başarılı" der,
	// canlıda üç konteyner trafik almaya devam eder ve kimse fark etmez.
	// Mekanizmayı düzeltip tetikleyiciyi eksik bırakmak, hatayı en sık
	// kullanılan yolda açık tutmak olurdu.
	replicasChanged := upd.Replicas != nil && *upd.Replicas != current.Replicas

	// www ve yönlendirme adları da rota kümesinin parçası (appnames.Names):
	// değişirse ters vekil yeniden uzlaştırılmalı.
	namesChanged := upd.ChangesNames(current)

	// Env, ters vekili İLGİLENDİRMİYOR — uzlaştırma tetiklemiyor.
	// Konteynerin ortamı rota tablosundan değil, oluşturma anından
	// geliyor; uzlaştırmak hiçbir şeyi değiştirmezdi. Burada saptanan
	// tek şey, kullanıcıya SÖYLENECEK olan.
	envChanged := upd.ChangesEnv()
	volumesChanged := upd.ChangesVolumes()

	app, opErr := s.store.UpdateApp(ctx, appID, upd)
	if opErr != nil {
		return nil, appError(s.completed(ctx, action, tgt, params, opErr))
	}

	resp := &kadranv1.UpdateAppResponse{App: appToProto(app)}
	if domainMoved || replicasChanged || namesChanged {
		proxy, redirect, err := s.moveTraffic(ctx, appID, current, app,
			domainMoved || replicasChanged, namesChanged)
		if err != nil {
			return nil, s.completed(ctx, action, tgt, params, err)
		}
		resp.ProxyDetail = proxy
		resp.RedirectDetail = redirect
	}
	if envChanged {
		resp.EnvDetail = envNeedsRedeploy(appID)
	}
	if volumesChanged {
		resp.VolumeDetail = volumesNeedRedeploy(appID)
	}
	if err := s.completed(ctx, action, tgt, params, nil); err != nil {
		return nil, err
	}
	return resp, nil
}

// envForValidation, birleşik tanımın ortamını doğrulama için kurar (K-123).
//
// Var olan değerler mühürlü ve daemon onları açamıyor, ama boyut sınırı
// düz metin üzerinden işliyor. Mühürlü metni olduğu gibi doğrulamak sınırı
// ~1,4 kat erken doldururdu. Bu yüzden var olan her değerin yerine düz
// uzunluğunda bir yer tutucu konuyor (vault.PlainLen); yeni değerler kendi
// düz hâliyle doğrulanıyor.
func envForValidation(appID string, current map[string]string, upd store.AppUpdate) (map[string]string, error) {
	out := make(map[string]string, len(current)+len(upd.Env))
	for k, v := range current {
		n, err := vault.PlainLen(appID, k, v)
		if err != nil {
			return nil, err
		}
		out[k] = strings.Repeat("x", n)
	}
	for k, v := range upd.Env {
		out[k] = v
	}
	for _, k := range upd.EnvRemove {
		delete(out, k)
	}
	return out, nil
}

// envNeedsRedeploy, env değişikliğinin HENÜZ ETKİLİ OLMADIĞINI söyler.
//
// ── Neden uzlaştırma yetmiyor? ──────────────────────────────────────
//
// Alan adı değişikliği uzlaştırmayla anında taşınıyor, çünkü rota
// `apps` tablosundan ÜRETİLİYOR. Env öyle değil: konteynerin ortamı
// oluşturma anında çekirdeğe yazılıyor ve Docker onu sonradan
// değiştiremez. Yani burada "uzlaştır ve bitir" diye bir seçenek yok —
// yeni ortam ancak konteyner YENİDEN KURULUNCA doğar.
//
// ── Neden sessizce başarılı denmiyor? ───────────────────────────────
//
// `moveTraffic` bu dersi zaten bir kez öğretti: kayıt değişip
// gerçekliğin değişmemesi, kullanıcının komutu "çalıştı" sanması
// demek. Env'de bedeli daha ağır — kullanıcı DATABASE_URL'i ayarlar,
// uygulama hâlâ eski (ya da hiç) değeri görür, ve hatayı veritabanı
// tarafında aramaya başlar. Komutun kendisi şüpheli listesine bile
// girmez.
func envNeedsRedeploy(appID string) string {
	return fmt.Sprintf(
		"⚠ env SAVED but RUNNING CONTAINERS still run with the old "+
			"environment — Docker cannot change the environment of a "+
			"running container. Apply it: kadran deploy %s", appID)
}

// moveTraffic, alan adı değişikliğini ters vekile yansıtır.
//
// ── Neden dağıtım beklemiyoruz? ─────────────────────────────────────
//
// Uzlaştırma yalnızca İKİ yerden çağrılıyordu: kadrand açılışı ve
// dağıtım. Yani `app update -domain` tek başına trafiği taşımazdı; alan
// adı veritabanında değişir, canlıda hiçbir şey olmazdı ve kullanıcı
// komut "başarılı" dediği için taşındığını sanırdı. Bu işin var olma
// sebebi apex'i dağıtımsız taşımak, dolayısıyla uzlaştırma buraya ait.
//
// ── Üç sonuç, üç ayrı cevap ─────────────────────────────────────────
//
// Hepsini "tamam" diye raporlamak, en tehlikeli ikisini gizlerdi.
//
// Tek uzlaştırma, iki cevap: rota (alan adı/replika) değiştiyse proxy
// cevabı, adlar (www/yönlendirme) değiştiyse redirect cevabı döner. Yalnız
// adlar değiştiyse proxy cevabı BOŞ kalır.
func (s *Server) moveTraffic(
	ctx context.Context, appID string, before, after store.App, routeChanged, namesChanged bool,
) (proxy, redirect string, err error) {
	from, to := before.Domain, after.Domain
	res, err := s.reconciler.Reconcile(ctx)
	if err != nil {
		// Değişiklik YAZILDI. Bunu söylemeyen bir hata, kullanıcıyı
		// komutun hiç etki etmediğini sanıp başka bir alan adıyla
		// yeniden denemeye iterdi — oysa kayıt zaten yeni değerde.
		what := fmt.Sprintf("domain %q → %q", from, to)
		if !routeChanged {
			what = "redirect names"
		}
		return "", "", status.Errorf(codes.Unavailable,
			"%s SAVED, but the reverse proxy "+
				"could not be updated: traffic is still on the old route. Reconciliation "+
				"is retried on the next deploy or when kadrand restarts. "+
				"Cause: %v", what, err)
	}

	if routeChanged {
		switch {
		case to == "":
			proxy = "app REMOVED from the reverse proxy — no longer served on any domain"
		default:
			if why, skipped := res.Skipped[appID]; skipped {
				return "", "", status.Errorf(codes.Unavailable,
					"app definition SAVED but TRAFFIC NOT MOVED (%s) — deploy the app: kadran deploy %s", why, appID)
			}
			proxy = fmt.Sprintf("reverse proxy updated — %q now goes to this app", to)
		}
	}
	if namesChanged {
		redirect = redirectDetail(res, before, after)
	}
	return proxy, redirect, nil
}

// redirectDetail, ad değişikliğinin uzlaştırma sonucunu anlatır. Uygulanamayan
// her ad ayrı satırda: tek bir "tamam" en tehlikeli durumu gizlerdi.
func redirectDetail(res deploy.Result, before, app store.App) string {
	// Tam ad kümesi (www eşi dahil): kullanıcı yalnız -www dediğinde de
	// hangi adın yönlendirildiğini duymalı.
	was := appnames.Names(before.Domain, before.WWW, before.Redirects)
	names := appnames.Names(app.Domain, app.WWW, app.Redirects)
	// Rotalanma ÖNCE: rotalanmayan uygulamada "updated" ya da çıplak "removed"
	// demek yalan olurdu. Kayıt başarılı, bu yüzden hata değil.
	if why, skipped := res.Skipped[app.ID]; skipped {
		if d := unroutedRedirectDetail(app, why, was, names); d != "" {
			return d
		}
		return noRedirectNamesDetail(before, app, nil)
	}
	if len(names) == 0 {
		return noRedirectNamesDetail(before, app, was)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "reverse proxy updated — redirects: %s → %s",
		strings.Join(names, ", "), app.Domain)
	for _, name := range names {
		if why, skipped := res.SkippedRedirects[name]; skipped {
			fmt.Fprintf(&b, "\nredirect %s NOT applied (%s)", name, why)
		}
	}
	return b.String()
}

// unroutedRedirectDetail, rotalanmayan uygulamadaki ad değişikliğini anlatır.
// Kaldırma UYGULANIR (uzlaştırma istenmeyen adı canlıdan taşımıyor), ekleme
// uygulanmaz (rota yokken yönlendirme kurulamaz). Değişen ad yoksa boş döner.
func unroutedRedirectDetail(app store.App, why string, was, names []string) string {
	var lines []string
	if removed := namesNotIn(was, names); len(removed) > 0 {
		lines = append(lines, fmt.Sprintf(
			"redirects removed: %s — applied even though the app is not routed (%s)",
			strings.Join(removed, ", "), why))
	}
	if added := namesNotIn(names, was); len(added) > 0 {
		lines = append(lines, fmt.Sprintf("redirects SAVED (%s → %s) but NOT applied: %s — "+
			"they take effect when the app is routed: kadran deploy %s",
			strings.Join(added, ", "), app.Domain, why, app.ID))
	}
	return strings.Join(lines, "\n")
}

// noRedirectNamesDetail, uygulamada yönlendirme adı kalmadığında doğruyu
// söyler: domain yoksa boş domain basmaz, alt adın www eşi olmadığını
// "kaldırıldı" diye anlatmaz.
func noRedirectNamesDetail(before, app store.App, was []string) string {
	removed := ""
	if len(was) > 0 {
		removed = "redirects removed — "
	}
	switch {
	case app.Domain == "":
		return removed + "the app has no domain"
	case (app.WWW || before.WWW) && appnames.Partner(app.Domain) == "":
		return removed + app.Domain + " is a subdomain and has no www partner"
	default:
		return fmt.Sprintf("redirects removed — %s has no redirect names", app.Domain)
	}
}

// namesNotIn, a'da olup b'de olmayan adları a'nın sırasıyla döndürür.
func namesNotIn(a, b []string) []string {
	var out []string
	for _, n := range a {
		if !slices.Contains(b, n) {
			out = append(out, n)
		}
	}
	return out
}

// updateFromProto, isteği depo katmanının tipine çevirir.
//
// İşaretçiler KOPYALANIYOR, proto'nunkiler paylaşılmıyor: istek nesnesi
// çağrı bittikten sonra gRPC tarafından yeniden kullanılabilir.
func updateFromProto(req *kadranv1.UpdateAppRequest) store.AppUpdate {
	var upd store.AppUpdate
	if req.Domain != nil {
		v := req.GetDomain()
		upd.Domain = &v
	}
	if req.GitBranch != nil {
		v := req.GetGitBranch()
		upd.GitBranch = &v
	}
	if req.HealthPath != nil {
		v := req.GetHealthPath()
		upd.HealthPath = &v
	}
	if req.Replicas != nil {
		v := req.GetReplicas()
		upd.Replicas = &v
	}
	// Haritalar ve dilimler KOPYALANIYOR, proto'nunki paylaşılmıyor:
	// istek nesnesi çağrı bittikten sonra gRPC tarafından yeniden
	// kullanılabilir ve depo katmanı bu haritayı çağrıdan sonra da
	// tutabilir.
	if len(req.GetEnv()) > 0 {
		env := make(map[string]string, len(req.GetEnv()))
		for k, v := range req.GetEnv() {
			env[k] = v
		}
		upd.Env = env
	}
	if len(req.GetEnvRemove()) > 0 {
		upd.EnvRemove = append([]string(nil), req.GetEnvRemove()...)
	}
	if len(req.GetVolumes()) > 0 {
		upd.Volumes = volumesFromProto(req.GetVolumes())
	}
	if len(req.GetVolumeRemove()) > 0 {
		upd.VolumeRemove = append([]string(nil), req.GetVolumeRemove()...)
	}
	if req.Www != nil {
		v := req.GetWww()
		upd.WWW = &v
	}
	if len(req.GetRedirectAdd()) > 0 {
		upd.RedirectAdd = append([]string(nil), req.GetRedirectAdd()...)
	}
	if len(req.GetRedirectRemove()) > 0 {
		upd.RedirectRemove = append([]string(nil), req.GetRedirectRemove()...)
	}
	return upd
}

// updateAuditParams, denetime yalnızca BELİRTİLEN alanları yazar.
//
// Belirtilmeyen alanı boş değeriyle yazmak, kaydı okuyan birine
// "health_path boşaltıldı" dedirtirdi — oysa ona hiç dokunulmadı.
// Zincir ekle-sadece: bir kez yazılan yanlış bilgi düzeltilemez.
func updateAuditParams(req *kadranv1.UpdateAppRequest) map[string]string {
	params := map[string]string{}
	if req.Domain != nil {
		params["domain"] = req.GetDomain()
	}
	if req.GitBranch != nil {
		params["branch"] = req.GetGitBranch()
	}
	if req.HealthPath != nil {
		params["health_path"] = req.GetHealthPath()
	}
	if req.Replicas != nil {
		params["replicas"] = strconv.FormatUint(uint64(req.GetReplicas()), 10)
	}
	// ⚠ DEĞERLER DEĞİL, yalnızca anahtar adları.
	//
	// Denetim zinciri EKLE-SADECE: `audit_log` üzerinde UPDATE ve DELETE
	// tetikleyici düzeyinde yasak. Buraya bir kez yazılan sır SİLİNEMEZ
	// ve veritabanı yedekleri de onu taşır. Anahtar adının yazılması ise
	// şart — "kim hangi değişkeni ayarladı" denetlenebilir kalmalı.
	for k := range req.GetEnv() {
		params["env."+k] = "[REDACTED]"
	}
	for _, k := range req.GetEnvRemove() {
		params["env_remove."+k] = "removed"
	}
	for _, v := range req.GetVolumes() {
		params["volume."+v.GetName()] = v.GetMountPath()
	}
	for _, n := range req.GetVolumeRemove() {
		params["volume_remove."+n] = "detached"
	}
	// Yalnız BELİRTİLENLER (yukarıdaki ilke): www sağlanmadıysa yazılmaz.
	if req.Www != nil {
		params["www"] = strconv.FormatBool(req.GetWww())
	}
	if len(req.GetRedirectAdd()) > 0 {
		params["redirect_add"] = strings.Join(req.GetRedirectAdd(), ",")
	}
	if len(req.GetRedirectRemove()) > 0 {
		params["redirect_remove"] = strings.Join(req.GetRedirectRemove(), ",")
	}
	return params
}

// volumesNeedRedeploy, hacim degisikliginin HENUZ ETKILI OLMADIGINI
// soyler.
//
// Baglama konteyner olusturulurken kuruluyor; calisan bir konteynere
// sonradan disk eklenemez. Env ile ayni sinif ve ayni gerekce:
// kaydin degisip gercekligin degismemesi, kullanicinin komutun
// calistigini sanmasi demek.
//
// Burada bedeli ozellikle agir: kullanici diski bagladigini sanip
// uygulamanin verisini oraya yazdigini varsayar. Oysa veri konteynerin
// KENDI katmaninda durur ve bir sonraki dagitimda KAYBOLUR.
func volumesNeedRedeploy(appID string) string {
	return fmt.Sprintf(
		"\u26a0 volume definition SAVED but RUNNING CONTAINERS still run with the old "+
			"mounts -- a disk is mounted when the container is created. "+
			"Apply it: kadran deploy %s", appID)
}
