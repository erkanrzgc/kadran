-- Panely kontrol düzlemi şeması — göç 0005: dağıtım GEÇMİŞİ
--
-- 0003 uygulama başına TEK satır tutuyordu ve her aktivasyon bir öncekini
-- eziyordu. "Önceki AKTİF sürüm" sorusunun cevabı şemada YOKTU, yani
-- `panely rollback` (Faz 1 kabul ölçütü #5) yazılamıyordu.
--
-- ── Neden `releases.seq` yetmiyor ─────────────────────────────────────
--
-- Sürüm sırası ≠ aktivasyon geçmişi. r5'ten r3'e geri alındıysa BİR
-- SONRAKİ geri alma r4'e değil r5'e gitmelidir. Sıra numarası bunu
-- söyleyemez; yalnızca ne zaman neyin canlı OLDUĞU söyleyebilir.
--
-- ── Neden tablo YENİDEN KURULUYOR, sütun eklenmiyor ──────────────────
--
-- 0003'te `app_id` BİRİNCİL ANAHTAR. Bu, uygulama başına ikinci bir satırı
-- imkânsız kılar — ekle-sadece bir geçmiş tam olarak onu gerektiriyor.
-- `ALTER TABLE ADD COLUMN` bu kısıtı kaldıramaz.
--
-- Yeniden kurulum FK'ler AÇIKKEN güvenli, çünkü `deployments` referans
-- VEREN taraftır; hiçbir tablo ona referans vermiyor. (Göçler transaction
-- içinde koşuyor ve `PRAGMA foreign_keys` orada değiştirilemez — yani
-- kapatma seçeneği zaten yoktu.)

-- ══════════════════════════════════════════════════════════════════
--  deployments — uygulama başına AKTİVASYON GEÇMİŞİ
-- ══════════════════════════════════════════════════════════════════

CREATE TABLE deployments_new (
    -- ── Neden kendi sıra numarası? ────────────────────────────────
    --
    -- Geçmiş `activated_at` ile SIRALANAMAZ: `releases.seq` yorumundaki
    -- gerekçenin aynısı geçerli — iki aktivasyon aynı nanosaniyeye
    -- düşebilir ve sıra belirsizleşir. Geri alma "hangi satır daha
    -- önceydi" sorusuna kesin cevap ister.
    --
    -- AUTOINCREMENT, rowid'in YENİDEN KULLANILMASINI engeller. Sade
    -- INTEGER PRIMARY KEY, en yüksek satır silinirse o numarayı bir
    -- sonraki eklemeye verir; ekle-sadece bir tabloda bu bugün olmaz ama
    -- #29 DeleteApp geldiğinde olabilir ve geçmiş sırası sessizce
    -- bozulurdu.
    seq            INTEGER PRIMARY KEY AUTOINCREMENT,

    app_id         TEXT NOT NULL REFERENCES apps(id),
    release_id     TEXT NOT NULL,

    -- Bu sürümün trafiği devraldığı an.
    activated_at   INTEGER NOT NULL,

    -- Trafiği BIRAKTIĞI an. NULL = HÂLÂ AKTİF.
    --
    -- Aktifliğin NULL ile temsil edilmesi kasıtlı: aşağıdaki kısmi tekil
    -- indeks yalnızca NULL satırları kapsıyor, yani "aynı anda iki aktif
    -- sürüm" veritabanında TEMSİL EDİLEMEZ kalıyor.
    deactivated_at INTEGER,

    -- Sürüm gerçekten var olmalı. `releases` birincil anahtarı
    -- (app_id, id) olduğu için bileşik anahtarla bağlanıyor: yalnızca
    -- release_id'ye bağlanmak, BAŞKA bir uygulamanın sürümünü aktif
    -- yapmaya izin verirdi.
    FOREIGN KEY (app_id, release_id) REFERENCES releases(app_id, id),

    CHECK (activated_at > 0),

    -- Bir sürüm devraldığı andan ÖNCE bırakamaz.
    CHECK (deactivated_at IS NULL OR deactivated_at >= activated_at)
) STRICT;

-- Mevcut satırlar AÇIK olarak taşınıyor: 0003'teki tek satır, tanımı gereği
-- o uygulamanın şu an canlı olan sürümüdür. Canlı kurulumda (Hetzner)
-- `portfolio`, `pfprobe` ve `web` bu yoldan geçecek.
INSERT INTO deployments_new (app_id, release_id, activated_at, deactivated_at)
SELECT app_id, release_id, activated_at, NULL FROM deployments;

-- Eski tablonun tetikleyicileri ve indeksleri DROP ile birlikte gider.
DROP TABLE deployments;
ALTER TABLE deployments_new RENAME TO deployments;

-- ══════════════════════════════════════════════════════════════════
--  Değişmez: bir uygulamanın aynı anda İKİ aktif sürümü olamaz
-- ══════════════════════════════════════════════════════════════════
--
-- 0003 bunu `app_id BİRİNCİL ANAHTAR` ile temsil edilemez kılıyordu ve o
-- garanti KAYBEDİLMEMELİ: blue-green geçişi yarıda kalsa bile ikili durum
-- oluşmamalı. Kısmi tekil indeks aynı garantiyi geçmişi silmeden verir —
-- kapanmış satırlar (deactivated_at NOT NULL) indeksin dışında kalır ve
-- istendiği kadar birikebilir.
CREATE UNIQUE INDEX idx_deployments_one_active
    ON deployments (app_id) WHERE deactivated_at IS NULL;

-- "Bu uygulamanın önceki aktif sürümü neydi" geri almanın birincil
-- sorgusu; en yeniden geriye doğru okunuyor.
CREATE INDEX idx_deployments_history ON deployments (app_id, seq DESC);

-- ══════════════════════════════════════════════════════════════════
--  Aktif sürüm DERLENMİŞ olmak zorunda (0003'ten devralındı)
-- ══════════════════════════════════════════════════════════════════
--
-- K-042 zinciri: `releases` şeması "status=BUILT ise image_id dolu olmalı"
-- diyor; bu tetikleyici de "aktif sürüm BUILT olmalı" diyor. İkisi
-- birleşince, imajı KANITLANMAMIŞ bir sürüme trafik taşımak veritabanında
-- temsil edilemez hâle geliyor.
--
-- `IS NOT` kullanılıyor, `!=` değil: sürüm satırı hiç yoksa alt sorgu NULL
-- döner ve `NULL != 2` de NULL'dur — yani WHEN yanlış sayılır ve
-- tetikleyici HİÇ ÇALIŞMAZ. Yabancı anahtar bu durumu zaten yakalıyor ama
-- iki savunmanın biri sessizce devre dışı kalmamalı.
CREATE TRIGGER deployments_insert_requires_built_release
BEFORE INSERT ON deployments
FOR EACH ROW
WHEN (SELECT status FROM releases
      WHERE app_id = NEW.app_id AND id = NEW.release_id) IS NOT 2
BEGIN
    SELECT RAISE(ABORT,
        'aktif sürüm BUILT olmalı — imajı kanıtlanmamış sürüme trafik taşınamaz');
END;

-- ⚠ 0003'teki UPDATE karşılığı KASTEN GERİ KONMADI.
--
-- Artık her aktivasyon bir INSERT; UPDATE yalnızca AÇIK bir satırı
-- KAPATIYOR ve `release_id`'ye dokunamıyor (aşağıdaki tetikleyici zorluyor).
-- Yani UPDATE'te BUILT kontrolü gereksiz — ve gereksizden de kötü:
-- aktifken bir şekilde FAILED'a düşmüş bir sürümün satırı KAPATILAMAZ
-- olurdu. Bu tam olarak geri almanın gerektiği durumda geri almayı
-- engellerdi.

-- ══════════════════════════════════════════════════════════════════
--  Geçmiş EKLE-SADECE
-- ══════════════════════════════════════════════════════════════════
--
-- Gerekçe denetim zincirininkiyle aynı (0001): geçmişi ezebilen bir tablo
-- geçmişi yanıtlayamaz. Tek meşru değişiklik AÇIK bir satırın
-- KAPATILMASIDIR.
--
-- DELETE kasten SERBEST bırakıldı: saklama politikası ("son N sürüm") ve
-- #29 DeleteApp henüz yazılmadı. 0002'deki FK'nin NO ACTION bırakılmasıyla
-- aynı tercih — silmeyi ekleyen kişi ne olacağına karar vermek ZORUNDA
-- kalsın, karar şimdiden ve sessizce verilmesin.
CREATE TRIGGER deployments_history_is_append_only
BEFORE UPDATE ON deployments
FOR EACH ROW
WHEN NEW.seq            IS NOT OLD.seq
  OR NEW.app_id         IS NOT OLD.app_id
  OR NEW.release_id     IS NOT OLD.release_id
  OR NEW.activated_at   IS NOT OLD.activated_at
  OR OLD.deactivated_at IS NOT NULL
BEGIN
    SELECT RAISE(ABORT,
        'dağıtım geçmişi ekle-sadece — yalnızca AÇIK bir satır kapatılabilir');
END;
