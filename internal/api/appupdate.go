package api

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/erkanrzgc/kadran/internal/audit"
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

	// Env, ters vekili İLGİLENDİRMİYOR — uzlaştırma tetiklemiyor.
	// Konteynerin ortamı rota tablosundan değil, oluşturma anından
	// geliyor; uzlaştırmak hiçbir şeyi değiştirmezdi. Burada saptanan
	// tek şey, kullanıcıya SÖYLENECEK olan.
	envChanged := upd.ChangesEnv()
	volumesChanged := upd.ChangesVolumes()

	app, opErr := s.store.UpdateApp(ctx, appID, upd)
	if err := s.completed(ctx, action, tgt, params, opErr); err != nil {
		return nil, appError(err)
	}

	resp := &kadranv1.UpdateAppResponse{App: appToProto(app)}
	if domainMoved || replicasChanged {
		detail, err := s.moveTraffic(ctx, appID, current.Domain, app.Domain)
		if err != nil {
			return nil, err
		}
		resp.ProxyDetail = detail
	}
	if envChanged {
		resp.EnvDetail = envNeedsRedeploy(appID)
	}
	if volumesChanged {
		resp.VolumeDetail = volumesNeedRedeploy(appID)
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
func (s *Server) moveTraffic(ctx context.Context, appID, from, to string) (string, error) {
	res, err := s.reconciler.Reconcile(ctx)
	if err != nil {
		// Değişiklik YAZILDI. Bunu söylemeyen bir hata, kullanıcıyı
		// komutun hiç etki etmediğini sanıp başka bir alan adıyla
		// yeniden denemeye iterdi — oysa kayıt zaten yeni değerde.
		return "", status.Errorf(codes.Unavailable,
			"domain %q → %q SAVED, but the reverse proxy "+
				"could not be updated: traffic is still on the old route. Reconciliation "+
				"is retried on the next deploy or when kadrand restarts. "+
				"Cause: %v", from, to, err)
	}

	if to == "" {
		return "app REMOVED from the reverse proxy — no longer served on any domain", nil
	}
	if why, skipped := res.Skipped[appID]; skipped {
		// Hata DEĞİL: hiç dağıtılmamış bir uygulamanın ayakta replikası
		// olmaz ve rota üretilemez. Ama sessiz kalmak, kullanıcının yeni
		// alan adının canlıda cevap verdiğini sanması demekti.
		return fmt.Sprintf(
			"⚠ domain saved but TRAFFIC NOT MOVED (%s) — "+
				"deploy the app: kadran deploy %s", why, appID), nil
	}
	return fmt.Sprintf("reverse proxy updated — %q now goes to this app", to), nil
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
