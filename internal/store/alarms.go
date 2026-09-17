package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Alarm, etkin bir arıza koşulunu tanımlar.
type Alarm struct {
	ID       string
	Kind     string
	Target   string
	Severity string
	Since    time.Time
	Detail   string
}

// Alarm ciddiyetleri.
//
// İki seviye yeter: "şimdi bak" ve "bilmen iyi olur". Daha ince bir
// ölçek, eşikleri ezberlemek zorunda kalan bir okuyucu üretir ve
// sıralama dışında bir şey kazandırmaz.
const (
	SeverityWarning  = "uyari"
	SeverityCritical = "kritik"
)

// RaiseAlarm, alarmı yükseltir ve GERÇEKTEN YENİ olup olmadığını
// döndürür.
//
// ── Dönen bool bu dilimin merkezi ───────────────────────────────────
//
// `true` yalnızca koşul BOZUK OLMAYAN durumdan bozuk duruma geçtiğinde
// dönüyor. Çağıran tarafın haber verme kararı buna bakıyor, yani
// bildirim KENAR TETİKLEMELİ oluyor.
//
// Tekilleştirme burada bir `if` ile değil, birincil anahtar çakışmasıyla
// yapılıyor: iki eşzamanlı yükseltme de aynı satırı hedefler ve
// yalnızca biri kazanır. Uygulama mantığındaki bir kontrol, iki
// gözetim turu çakıştığında yarışa açık olurdu.
//
// `since` KORUNUYOR (ON CONFLICT DO NOTHING): koşul sürüyorsa
// "ne zamandır bozuk" cevabı ilk anı göstermeli. Üzerine yazmak, üç
// gündür bozuk olan bir şeyi her turda "az önce bozuldu" diye
// gösterirdi.
func (s *Store) RaiseAlarm(ctx context.Context, a Alarm) (bool, error) {
	if a.ID == "" || a.Kind == "" {
		return false, errors.New("alarm kimliği ve türü zorunlu")
	}
	if a.Severity != SeverityWarning && a.Severity != SeverityCritical {
		return false, fmt.Errorf("bilinmeyen ciddiyet: %q", a.Severity)
	}
	if a.Since.IsZero() {
		a.Since = time.Now()
	}

	res, err := s.db.ExecContext(ctx, `
		INSERT INTO alarms (id, kind, target, severity, since, detail)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO NOTHING`,
		a.ID, a.Kind, a.Target, a.Severity, a.Since.Unix(), a.Detail)
	if err != nil {
		return false, fmt.Errorf("alarm yükseltilemedi (%s): %w", a.ID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("alarm sonucu okunamadı (%s): %w", a.ID, err)
	}
	return n > 0, nil
}

// ClearAlarm, alarmı kaldırır ve GERÇEKTEN ETKİN OLUP OLMADIĞINI
// döndürür.
//
// `true` yalnızca bozuk durumdan düzelmiş duruma geçişte dönüyor —
// yükseltmenin aynası. Hiç alarm yokken "düzeldi" demek, hiç
// bozulmamış bir şey için kurtuluş bildirimi göndermek olurdu.
func (s *Store) ClearAlarm(ctx context.Context, id string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM alarms WHERE id = ?`, id)
	if err != nil {
		return false, fmt.Errorf("alarm kapatılamadı (%s): %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("alarm sonucu okunamadı (%s): %w", id, err)
	}
	return n > 0, nil
}

// ListAlarms, etkin alarmları en CİDDİDEN ve en ESKİDEN başlayarak
// döndürür.
//
// Sıralama sunum tercihi değil: alarm listesine bakan kişi önce en
// kötüyü görmeli. Kritikler önde, eşit ciddiyette en uzun süredir
// bozuk olan önde.
func (s *Store) ListAlarms(ctx context.Context) ([]Alarm, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, kind, target, severity, since, detail
		FROM alarms
		ORDER BY CASE severity WHEN 'kritik' THEN 0 ELSE 1 END, since`)
	if err != nil {
		return nil, fmt.Errorf("alarmlar okunamadı: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Alarm
	for rows.Next() {
		var a Alarm
		var since int64
		if err := rows.Scan(&a.ID, &a.Kind, &a.Target,
			&a.Severity, &since, &a.Detail); err != nil {
			return nil, fmt.Errorf("alarm satırı okunamadı: %w", err)
		}
		a.Since = time.Unix(since, 0).UTC()
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("alarm listesi tamamlanamadı: %w", err)
	}
	return out, nil
}

// GetAlarm, tek bir alarmı döndürür. Yoksa sql.ErrNoRows sarılı döner.
func (s *Store) GetAlarm(ctx context.Context, id string) (Alarm, error) {
	var a Alarm
	var since int64
	err := s.db.QueryRowContext(ctx, `
		SELECT id, kind, target, severity, since, detail
		FROM alarms WHERE id = ?`, id).
		Scan(&a.ID, &a.Kind, &a.Target, &a.Severity, &since, &a.Detail)
	if errors.Is(err, sql.ErrNoRows) {
		return Alarm{}, err
	}
	if err != nil {
		return Alarm{}, fmt.Errorf("alarm okunamadı (%s): %w", id, err)
	}
	a.Since = time.Unix(since, 0).UTC()
	return a, nil
}

// EscalateAlarm, etkin bir alarmın ciddiyetini yükseltir ve gerçekten
// değişip değişmediğini döndürür.
//
// ── Neden gerekli ───────────────────────────────────────────────────
//
// `RaiseAlarm` ikinci kez çağrıldığında hiçbir şey yapmıyor — kenar
// tetiklemenin gereği bu. Ama o kural tek başına bir boşluk bırakıyor:
// %9 boş diskle "uyari" açılır, disk %3'e inince ikinci yükseltme
// sessizce yutulur ve alarm "uyari" olarak kalır.
//
// Yani "bir kez haber ver" kuralı, "kötüleştiğini haber verme"ye
// dönüşürdü. Tırmanma bu boşluğu kapatıyor: ciddiyet ARTIYORSA satır
// güncellenir ve çağıran yeniden haber verebilir.
//
// Tersi YAPILMIYOR — kritikten uyarıya düşürme yok. Bir kez kritik olan
// koşul, tamamen düzelene (ClearAlarm) kadar kritik kalır; eşiğin
// etrafında gidip gelen bir disk aksi hâlde her turda ciddiyet
// değiştirip tam da kaçınılmak istenen gürültüyü üretirdi.
func (s *Store) EscalateAlarm(ctx context.Context, id string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `
		UPDATE alarms SET severity = ?
		WHERE id = ? AND severity = ?`,
		SeverityCritical, id, SeverityWarning)
	if err != nil {
		return false, fmt.Errorf("alarm tırmandırılamadı (%s): %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("alarm sonucu okunamadı (%s): %w", id, err)
	}
	return n > 0, nil
}
