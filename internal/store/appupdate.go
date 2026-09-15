package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ErrDomainTaken, alan adının BAŞKA bir uygulamaya ait olduğunu bildirir.
//
// ErrAppExists'ten ayrı olması şart. İkisi de SQLite'ta aynı hata sınıfını
// (SQLITE_CONSTRAINT_UNIQUE) üretiyor ve tek bir hataya indirgemek,
// "yeni-uygulama zaten var" gibi hem yanlış hem de yanlış alanı gösteren
// bir mesaj demekti — kullanıcı var olmayan bir kimliği aramaya çıkardı.
var ErrDomainTaken = errors.New("alan adı başka bir uygulamada")

// AppUpdate, bir uygulama tanımında DEĞİŞTİRİLEBİLİR alanlardır.
//
// ── Neden işaretçi? ──────────────────────────────────────────────────
//
// `nil` = "dokunma", işaret edilen değer = "bu yap". Ayrım gerçek bir
// ihtiyaç: boş dize hem `Domain` hem `HealthPath` için GEÇERLİ bir değer
// ("ters vekilde görünme", "HTTP yoklaması yapma"). Düz bir struct'ta
// "belirtilmedi" ile "temizle" aynı sıfır değere düşerdi ve alanı
// doldurmayan her istemci onları SESSİZCE silerdi.
//
// `Replicas` aynı hataya düşse doğrulayıcıya çarpardı (0 replika
// reddediliyor) — yani sessizce kaybolabilecek iki alan, tam da bu işin
// var olma sebebi olan ikisi.
//
// ── Neden bu dört alan? ──────────────────────────────────────────────
//
// Her biri için "bu değişiklik NE ZAMAN etkili olur" sorusunun ölçülmüş
// bir cevabı var:
//
//	Domain     → uzlaştırma anında (rota apps.domain'den JOIN'le üretilir)
//	Replicas   → BİR SONRAKİ dağıtımda (uzlaştırıcı konteyner yaratmaz)
//	HealthPath → BİR SONRAKİ dağıtımda (kapı yoklamayı orada okur)
//	GitBranch  → istemcinin bir sonraki sha çözümünde (sunucu kullanmaz)
//
// ⚠ `ContainerPort` KASTEN YOK. store.Deployment onu `apps`'ten JOIN'le
// CANLI okuyor (deployments.go), yani değiştirmek bir sonraki uzlaştırmada
// ters vekili — çalışan konteynerlerin DİNLEMEDİĞİ — yeni porta yönlendirir
// ve siteyi anında düşürür. Portu değiştirmek yeni bir dağıtım gerektirir;
// dağıtımsız temsil edilebilir olması bir tuzaktı.
//
// Git kaynağı (host/owner/repo) da yok: deponun değişmesi güncelleme değil,
// başka bir uygulamadır.
type AppUpdate struct {
	Domain     *string
	GitBranch  *string
	HealthPath *string
	Replicas   *uint32

	// Env, EKLENECEK ya da GÜNCELLENECEK ortam değişkenleridir; adı
	// geçmeyen anahtarlara dokunulmaz. EnvRemove ise silinecek
	// anahtarlardır.
	//
	// ── Neden işaretçi DEĞİL, neden birleştirme? ─────────────────────
	//
	// Yukarıdaki dört alanın işaretçi olma sebebi presence: "belirtilmedi"
	// ile "temizle" ayrımı. Harita alanlarında bu ayrım proto3'te ZATEN
	// TEMSİL EDİLEMEZ — `map<string,string>` alanının presence'ı yoktur,
	// yani tel üzerinde "hiç verilmedi" ile "boş harita gönderildi" aynı
	// şeydir. İşaretçi eklemek bu gerçeği değiştirmez, yalnızca Go
	// tarafında var olmayan bir bilgiyi varmış gibi gösterirdi.
	//
	// Dolayısıyla "tamamen değiştir" semantiği dürüstçe uygulanamaz:
	// `-env` yazmayan HER güncelleme bütün env'i silerdi. Birleştirme bir
	// kullanılabilirlik tercihi değil, şemanın temsil edebildiği tek
	// doğru davranış.
	//
	// Silme bu yüzden AYRI bir alan: birleştirme tek başına bir anahtarı
	// kaldıramaz ve boş dizeye ayarlamak silmek değildir (konteyner
	// değişkeni tanımlı ama boş görür — "tanımlı mı" diye bakan uygulama
	// yanlış cevap alır).
	//
	// Etki zamanı: BİR SONRAKİ dağıtımda. Docker çalışan bir konteynerin
	// ortamını değiştiremez; bu altyapının kısıtı, bizim tercihimiz
	// değil. Çağıran bunu kullanıcıya SÖYLEMEK zorunda.
	Env       map[string]string
	EnvRemove []string
}

// IsEmpty, hiçbir alanın belirtilmediğini söyler.
func (u AppUpdate) IsEmpty() bool {
	return u.Domain == nil && u.GitBranch == nil &&
		u.HealthPath == nil && u.Replicas == nil &&
		len(u.Env) == 0 && len(u.EnvRemove) == 0
}

// ChangesEnv, güncellemenin ortam değişkenlerine dokunup dokunmadığını
// söyler.
//
// Çağıran bunu kullanıcıyı UYARMAK için kullanıyor: env değişti ama
// çalışan konteynerler hâlâ eskisini taşıyor. "Değişti mi" değil
// "belirtildi mi" sorusu yeterli — aynı değeri yeniden yazan bir
// güncelleme de kullanıcının yeni bir dağıtım beklediğini gösterir.
func (u AppUpdate) ChangesEnv() bool {
	return len(u.Env) > 0 || len(u.EnvRemove) > 0
}

// ChangesDomain, güncellemenin alan adını GERÇEKTEN değiştirip
// değiştirmediğini söyler.
//
// "Belirtildi mi" değil "değişti mi" sorusu soruluyor: çağıran bunu ters
// vekili yeniden uzlaştırmak için kullanıyor ve aynı değeri yeniden yazan
// bir güncelleme yüzünden bütün sitelerin yapılandırmasını yeniden
// yüklemenin gereği yok.
func (u AppUpdate) ChangesDomain(current string) bool {
	return u.Domain != nil && *u.Domain != current
}

// Apply, güncellemeyi bir KOPYAYA uygular ve sonucu döner.
//
// Çağıranın (API katmanı) doğrulamayı BİRLEŞTİRİLMİŞ tanıma yapabilmesi
// için var. Deltayı tek başına doğrulamak yetmezdi: tek başına geçerli
// görünen bir değişiklik, mevcut durumla birleşince geçersiz bir tanım
// üretebilir ve o tanım diske yazılırdı.
//
// Değer alıcı KASITLI — girdi struct'ı değişmez, yeni bir kopya döner.
func (u AppUpdate) Apply(app App) App {
	u.applyTo(&app)
	return app
}

func (u AppUpdate) applyTo(app *App) {
	if u.Domain != nil {
		app.Domain = *u.Domain
	}
	if u.GitBranch != nil {
		app.GitBranch = *u.GitBranch
	}
	if u.HealthPath != nil {
		app.HealthPath = *u.HealthPath
	}
	if u.Replicas != nil {
		app.Replicas = *u.Replicas
	}
	applyEnv(app, u.Env, u.EnvRemove)
}

// applyEnv, birleştirmeyi YENİ bir haritaya yapar.
//
// ── Neden kopya? ─────────────────────────────────────────────────────
//
// `Apply` değer alıcılı ve kopya döndürüyor, ama Go'da struct kopyası
// haritayı DERİN kopyalamaz: kopyadaki `Env` ile orijinaldeki aynı
// haritayı gösterir. Doğrudan üzerine yazmak, API katmanının yalnızca
// DOĞRULAMAK için aldığı geçici birleşimin mevcut kaydı da değiştirmesi
// demekti — ve o kayıt hemen ardından "eski değer" olarak okunuyor.
// Doğrulama, doğruladığı şeyi değiştiremez.
//
// ── Sıra: önce yaz, sonra sil ────────────────────────────────────────
//
// Aynı anahtar hem Env'de hem EnvRemove'da geçerse silme kazanır. Bu
// durum API katmanında zaten ÇELİŞKİ olarak reddediliyor, yani buraya
// ulaşmamalı; yine de davranış belirsiz bırakılmıyor. Belirsiz bırakılan
// her sıra, bir gün haritanın gezilme sırasına bağlı bir hata üretir.
func applyEnv(app *App, set map[string]string, remove []string) {
	if len(set) == 0 && len(remove) == 0 {
		return
	}
	merged := make(map[string]string, len(app.Env)+len(set))
	for k, v := range app.Env {
		merged[k] = v
	}
	for k, v := range set {
		merged[k] = v
	}
	for _, k := range remove {
		delete(merged, k)
	}
	app.Env = merged
}

// UpdateApp, var olan bir uygulamanın değiştirilebilir alanlarını yazar.
//
// ── Neden oku-değiştir-yaz, dinamik SQL değil? ───────────────────────
//
// Belirtilen alanlara göre SQL cümlesi kurmak akla yakın ama sütun listesi
// çalışma zamanında oluşurdu. Bunun yerine satır aynı transaction içinde
// OKUNUYOR, bellekte değiştiriliyor ve SABİT bir UPDATE ile geri
// yazılıyor. Değişmeyen alanlar kendi değerleriyle yeniden yazılır.
//
// Asıl kazanç, cümlenin ADLANDIRMADIĞI sütunlar: `release_seq`,
// `created_at`, `container_port`, `id`, git kaynağı ve limitler burada
// hiç geçmiyor, yani bu yoldan DEĞİŞTİRİLEMEZLER. Sayacın sıfırlanması
// özellikle sinsi olurdu: bir sonraki sürüm yine "r1" adını alır ve hostta
// VAR OLAN konteynerleri adresler — iki farklı commit, aynı ad.
func (s *Store) UpdateApp(ctx context.Context, id string, upd AppUpdate) (App, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return App{}, fmt.Errorf("güncelleme transaction'ı açılamadı: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	app, err := scanApp(tx.QueryRowContext(ctx, appSelect+` WHERE id = ?`, id))
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return App{}, fmt.Errorf("%w: %s", ErrAppNotFound, id)
	case err != nil:
		return App{}, fmt.Errorf("uygulama okunamadı: %w", err)
	}

	upd.applyTo(&app)
	app.UpdatedAt = time.Now()

	// Birleştirilmiş harita yeniden serileştiriliyor. Sütun cümleye
	// EKLENMEZSE `-env` sessizce işlemsiz kalır: fonksiyon güncellenmiş
	// struct'ı döndürür, kullanıcı "başarılı" görür, diskteki satır
	// değişmez. Bu, ölçek küçültmedeki hatanın (K-080) birebir şekli.
	env, err := json.Marshal(sortedArgs(app.Env))
	if err != nil {
		return App{}, fmt.Errorf("ortam değişkenleri serileştirilemedi: %w", err)
	}

	const q = `
		UPDATE apps SET
			git_branch = ?, health_path = ?, domain = ?, replicas = ?,
			env_json = ?, updated_at = ?
		WHERE id = ?`

	if _, err := tx.ExecContext(ctx, q,
		app.GitBranch, app.HealthPath, app.Domain, app.Replicas,
		string(env), app.UpdatedAt.UnixNano(), app.ID,
	); err != nil {
		if isUniqueViolation(err) {
			// Kimlik değişmiyor, dolayısıyla ihlal edilebilecek TEK kısıt
			// alan adı indeksidir.
			return App{}, domainConflict(ctx, tx, app.Domain, app.ID, err)
		}
		return App{}, fmt.Errorf("uygulama güncellenemedi (%s): %w", id, err)
	}

	if err := tx.Commit(); err != nil {
		return App{}, fmt.Errorf("güncelleme yazılamadı (%s): %w", id, err)
	}
	return app, nil
}

// rowQuerier, *sql.DB ve *sql.Tx'in ortak yüzeyidir.
type rowQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// domainConflict, alan adı çakışmasını SAHİBİNİ adlandırarak anlatır.
//
// ── Neden yazmadan ÖNCE kontrol edilmiyor? ───────────────────────────
//
// "Bu alan adı başkasında mı?" diye önden sormak akla yakın, ama o sorgu
// uygulamanın KENDİ satırını da bulur: `WHERE domain = ?` yazan bir
// kontrol, alan adına hiç dokunmayan bir güncellemeyi bile "çakışma" diye
// reddederdi ve `AND id != ?` yazmayı hatırlamaya bağlı kalırdı.
//
// Yazmayı DENEYİP hatayı açıklamak o sınıfı tamamen siler: bir satırı
// kendi değeriyle güncellemek benzersizlik indeksini zaten ihlal etmez.
// Kendiyle çakışma temsil edilemez hâle gelir — doğrulanan değil.
//
// Ek fayda: başarılı yolda fazladan sorgu yok.
func domainConflict(ctx context.Context, q rowQuerier, domain, selfID string, cause error) error {
	var owner string
	err := q.QueryRowContext(ctx,
		`SELECT id FROM apps WHERE domain = ? AND id != ?`, domain, selfID).Scan(&owner)
	if err != nil {
		// Sahibi bulunamadı. UYDURMUYORUZ: yanlış bir açıklama, hiç
		// açıklama olmamasından kötüdür — kullanıcıyı var olmayan bir
		// çakışmayı aramaya gönderir.
		return fmt.Errorf(
			"uygulama yazılamadı (benzersizlik ihlali, sebebi belirlenemedi): %w", cause)
	}
	return fmt.Errorf("%w: %q zaten %q uygulamasında", ErrDomainTaken, domain, owner)
}
