package store

import (
	"context"
	"database/sql"
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
}

// IsEmpty, hiçbir alanın belirtilmediğini söyler.
func (u AppUpdate) IsEmpty() bool {
	return u.Domain == nil && u.GitBranch == nil &&
		u.HealthPath == nil && u.Replicas == nil
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

	const q = `
		UPDATE apps SET
			git_branch = ?, health_path = ?, domain = ?, replicas = ?,
			updated_at = ?
		WHERE id = ?`

	if _, err := tx.ExecContext(ctx, q,
		app.GitBranch, app.HealthPath, app.Domain, app.Replicas,
		app.UpdatedAt.UnixNano(), app.ID,
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
