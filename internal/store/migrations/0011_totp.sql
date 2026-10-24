-- İki aşamalı giriş (K-155).
--
-- ── Sunucu başına TEK sır ───────────────────────────────────────────
--
-- Kadran'ı tek bir operatör yönetiyor; sır anahtar başına değil sunucu
-- başına. Oturum jetonu yine de giriş yapan anahtarın parmak izine bağlı.
-- Satır yoksa ya da `secret` NULL ise TOTP KAPALI: göç hiçbir davranışı
-- değiştirmez, açmak bilinçli bir `kadran totp enable`.
--
-- Sır düz duruyor: kasa (K-123) kullanılamaz, kadrand değerleri
-- mühürleyebilir ama açamaz. Yedekler de sırrı taşıyor; yerel yedekler
-- 0600, uzak yedek age ile şifreli (K-098).
CREATE TABLE totp (
    id INTEGER PRIMARY KEY CHECK (id = 1),

    -- Etkin sır (20 bayt). NULL = TOTP kapalı.
    secret BLOB,

    -- Etkin sırrın onaylandığı an (unix saniye).
    confirmed_at INTEGER,

    -- Son KABUL EDİLEN adım. Bu ve önceki adımların kodları reddedilir:
    -- aynı kod ikinci kez geçmez. Güncelleme koşullu (`last_step < ?`),
    -- iki eşzamanlı girişten yalnız biri kazanır.
    last_step INTEGER NOT NULL DEFAULT 0,

    -- Oturum jetonlarının imza anahtarı (32 bayt). Her onayda yenilenir.
    token_key BLOB,

    -- Her onayda artar; jetondaki kuşak bununla karşılaştırılır.
    generation INTEGER NOT NULL DEFAULT 0,

    -- Onay bekleyen kurulum. Yeniden kurulumda etkin sırrın YANINDA
    -- durur: onaylanana kadar eski sır geçerli.
    pending_secret BLOB,
    pending_created_at INTEGER
) STRICT;

-- Kurtarma kodlarının SHA-256 özetleri. Kodlar rastgele ve yüksek
-- entropili; yavaş bir özet gerekmez. Tek kullanımlık (used_at).
CREATE TABLE totp_recovery (
    hash BLOB PRIMARY KEY,
    used_at INTEGER
) STRICT;

-- Anahtar başına ardışık başarısız giriş. KALICI: yeniden başlatma kilidi
-- sıfırlamaz.
CREATE TABLE totp_failures (
    fingerprint TEXT PRIMARY KEY,
    count INTEGER NOT NULL,
    locked_until INTEGER NOT NULL DEFAULT 0,
    last_failure_at INTEGER NOT NULL
) STRICT;
