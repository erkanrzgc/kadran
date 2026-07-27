-- Panely kontrol düzlemi şeması — göç 0001
--
-- Faz 0 kapsamı: denetim zinciri ve ayarlar. Uygulama/sürüm/hacim
-- tabloları Faz 1'de kendi göçleriyle gelir; şimdiden yaratıp sonra
-- değiştirmek gereksiz churn üretir.

-- ══════════════════════════════════════════════════════════════════
--  Denetim günlüğü (§1.3) — yalnızca ekleme, hash zincirli
-- ══════════════════════════════════════════════════════════════════
--
-- Değişmezlik iki katmanda savunulur:
--
--   1. Veritabanı katmanı: aşağıdaki tetikleyiciler UPDATE ve DELETE'i
--      tamamen reddeder. Uygulamadaki bir hata veya SQL enjeksiyonu
--      geçmişi değiştiremez.
--
--   2. Kriptografik katman: her satır kendinden öncekinin hash'ini
--      taşır. Dosyaya doğrudan erişimi olan bir saldırgan satırı
--      değiştirse bile zincir kopar ve `panely audit verify` bunu
--      tespit eder.
--
-- Tek başına hiçbiri yeterli değildir; birlikte kurcalamayı hem
-- zorlaştırır hem görünür kılar.

CREATE TABLE audit_log (
    -- rowid takma adı: 1'den başlar, boşluksuz artar. Zincirin sırası.
    seq          INTEGER PRIMARY KEY,

    ts_unix_nano INTEGER NOT NULL,

    -- Aktör. Parola girişi olmadığı için gerçek kimlik SSH anahtar
    -- parmak izidir; IP şartname §1.3 gereği ayrıca tutulur.
    actor_fp     TEXT NOT NULL DEFAULT '',
    actor_ip     TEXT NOT NULL DEFAULT '',
    actor_label  TEXT NOT NULL DEFAULT '',
    actor_origin TEXT NOT NULL DEFAULT '',

    action       TEXT NOT NULL,
    target       TEXT NOT NULL DEFAULT '',

    -- İsteğin kanonik JSON gösterimi. Kasa alanları buraya yazılmadan
    -- önce "[REDACTED]" ile değiştirilir.
    params_json  TEXT NOT NULL DEFAULT '{}',

    -- 1=SUCCESS, 2=FAILURE, 3=DENIED (şema/değişmez reddi)
    outcome      INTEGER NOT NULL,
    detail       TEXT NOT NULL DEFAULT '',

    -- 1=DAEMON, 2=EXECUTOR
    source       INTEGER NOT NULL,

    prev_hash    BLOB NOT NULL,
    hash         BLOB NOT NULL,

    CHECK (length(prev_hash) = 32),
    CHECK (length(hash) = 32),
    CHECK (outcome BETWEEN 1 AND 3),
    CHECK (source BETWEEN 1 AND 2),
    CHECK (seq > 0)
) STRICT;

CREATE INDEX idx_audit_log_ts ON audit_log (ts_unix_nano);
CREATE INDEX idx_audit_log_action ON audit_log (action);

-- Reddedilen istekler güvenlik modelinin devreye girdiği anlardır;
-- ayrı sorgulanabilmeleri gerekir.
CREATE INDEX idx_audit_log_denied ON audit_log (seq) WHERE outcome = 3;

CREATE TRIGGER audit_log_append_only_update
BEFORE UPDATE ON audit_log
BEGIN
    SELECT RAISE(ABORT, 'audit_log yalnızca eklemeye açıktır: UPDATE reddedildi');
END;

CREATE TRIGGER audit_log_append_only_delete
BEFORE DELETE ON audit_log
BEGIN
    SELECT RAISE(ABORT, 'audit_log yalnızca eklemeye açıktır: DELETE reddedildi');
END;

-- ══════════════════════════════════════════════════════════════════
--  Ayarlar
-- ══════════════════════════════════════════════════════════════════

CREATE TABLE settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at INTEGER NOT NULL
) STRICT;
