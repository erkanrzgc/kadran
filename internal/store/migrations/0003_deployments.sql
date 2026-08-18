-- Panely kontrol düzlemi şeması — göç 0003: aktif dağıtımlar
--
-- ⚠⚠ BU DOSYA TARİHSELDİR — göç 0005 tabloyu YENİDEN KURDU.
--
-- Aşağıdaki `app_id BİRİNCİL ANAHTAR` tasarımı ve iki tetikleyici artık
-- çalışan şema DEĞİL: 0005 tabloyu ekle-sadece bir GEÇMİŞE çevirdi,
-- "tek aktif sürüm" garantisini kısmi tekil indekse taşıdı ve UPDATE
-- tetikleyicisini kasten geri koymadı. Güncel gerçek için 0005'i okuyun.
--
-- Dosya, uygulanmış bir göç olduğu için gövdesi DEĞİŞTİRİLMEDİ (0002'nin
-- başındaki gerekçe: uygulanmış göçü düzenlemek yeni kurulumlarla mevcut
-- kurulumları sessizce ayrıştırır). Eklenen tek şey bu uyarıdır.
--
-- Bir uygulamanın HANGİ sürümünün trafik aldığı burada duruyor. Bu tablo
-- olmadan ters vekilin yapılandırması üretilemez: "hangi sürüm canlı"
-- sorusunun `releases` içinde bir cevabı yok — orada yalnızca hangi
-- sürümlerin DERLENDİĞİ yazılı.

-- ══════════════════════════════════════════════════════════════════
--  deployments — uygulama başına AKTİF sürüm
-- ══════════════════════════════════════════════════════════════════
--
-- ── Neden apps'e bir sütun değil? ─────────────────────────────────
--
-- `apps.active_release_id` daha az tablo olurdu ama DAİRESEL bir yabancı
-- anahtar üretirdi: apps → releases → apps. SQLite bunu kabul eder, ama
-- satır ekleme sırası kilitlenir ve ilk sürümü yazmak imkânsızlaşır.
--
-- ── Neden app_id BİRİNCİL ANAHTAR? ───────────────────────────────
--
-- Bir uygulamanın aynı anda İKİ aktif sürümü olamaz. Bunu bir kontrolle
-- doğrulamak yerine TEMSİL EDİLEMEZ kılmak, blue-green geçişinin yarıda
-- kalması hâlinde bile ikili durumun oluşmamasını garanti eder.

CREATE TABLE deployments (
    app_id       TEXT PRIMARY KEY REFERENCES apps(id),
    release_id   TEXT NOT NULL,

    -- Bu sürümün trafiği ne zaman devraldığı.
    activated_at INTEGER NOT NULL,

    -- Sürüm gerçekten var olmalı. `releases` birincil anahtarı
    -- (app_id, id) olduğu için bileşik anahtarla bağlanıyor: yalnızca
    -- release_id'ye bağlanmak, BAŞKA bir uygulamanın sürümünü aktif
    -- yapmaya izin verirdi.
    FOREIGN KEY (app_id, release_id) REFERENCES releases(app_id, id),

    CHECK (activated_at > 0)
) STRICT;

-- ══════════════════════════════════════════════════════════════════
--  Aktif sürüm DERLENMİŞ olmak zorunda
-- ══════════════════════════════════════════════════════════════════
--
-- K-042 zinciri burada bitiyor. `releases` şeması "status=BUILT ise
-- image_id dolu olmalı" diyor; buradaki tetikleyici de "aktif sürüm
-- BUILT olmalı" diyor. İkisi birleşince, imajı KANITLANMAMIŞ bir sürüme
-- trafik taşımak veritabanında TEMSİL EDİLEMEZ hâle geliyor.
--
-- Neden CHECK değil de tetikleyici: CHECK yalnızca kendi satırının
-- sütunlarına bakabilir, başka tabloya sorgu atamaz.
--
-- Neden uygulama katmanına bırakılmadı: bırakılsaydı, dağıtım akışındaki
-- tek bir hata — ya da ileride yazılacak ikinci bir yol — derlemesi
-- BAŞARISIZ bir sürümü canlıya alabilirdi ve bu, kullanıcının sitesinin
-- 502 vermesi demekti.

CREATE TRIGGER deployments_insert_requires_built_release
BEFORE INSERT ON deployments
FOR EACH ROW
WHEN (SELECT status FROM releases
      WHERE app_id = NEW.app_id AND id = NEW.release_id) IS NOT 2
BEGIN
    SELECT RAISE(ABORT,
        'aktif sürüm BUILT olmalı — imajı kanıtlanmamış sürüme trafik taşınamaz');
END;

CREATE TRIGGER deployments_update_requires_built_release
BEFORE UPDATE ON deployments
FOR EACH ROW
WHEN (SELECT status FROM releases
      WHERE app_id = NEW.app_id AND id = NEW.release_id) IS NOT 2
BEGIN
    SELECT RAISE(ABORT,
        'aktif sürüm BUILT olmalı — imajı kanıtlanmamış sürüme trafik taşınamaz');
END;

-- `IS NOT` kullanılıyor, `!=` değil: sürüm satırı hiç yoksa alt sorgu
-- NULL döner ve `NULL != 2` de NULL'dur — yani WHEN yanlış sayılır ve
-- tetikleyici HİÇ ÇALIŞMAZ. Yabancı anahtar bu durumu zaten yakalıyor
-- ama iki savunmanın biri sessizce devre dışı kalmamalı.

-- "Şu an hangi sürümler canlı" ters vekil yapılandırmasının her
-- üretiminde soruluyor.
CREATE INDEX idx_deployments_release ON deployments (app_id, release_id);
