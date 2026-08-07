-- Panely kontrol düzlemi şeması — göç 0002: uygulamalar ve sürümler
--
-- 0001 KASTEN değiştirilmedi. Kendi yorumu bu tabloların "Faz 1'de kendi
-- göçleriyle" geleceğini yazıyor; uygulanmış bir göçü sonradan düzenlemek,
-- yeni kurulumlar ile mevcut kurulumları sessizce ayrıştırır.

-- ══════════════════════════════════════════════════════════════════
--  apps — kullanıcı tarafından tanımlanan uygulama
-- ══════════════════════════════════════════════════════════════════
--
-- ── Neden kısıtlar BURADA da var? ─────────────────────────────────
--
-- Aynı kısıtlar executor'ın doğrulayıcılarında da duruyor ve bu bir
-- kopya DEĞİL, kasıtlı bir ikinci katman. Sapma yönü ikisinde de
-- güvenlidir:
--
--   veritabanı daha katıysa → istek reddedilir (can sıkıcı, güvenli)
--   executor daha katıysa   → istek reddedilir (can sıkıcı, güvenli)
--
-- Yani drift hiçbir yönde bir KAÇIŞ üretmiyor. Asıl otorite executor'dır;
-- burası, bozuk bir satırın veritabanına hiç girememesini sağlar.
--
-- SQLite'ta düzenli ifade yok; GLOB ile karakter kümesi zorlanıyor.
-- GLOB `[a-z]` sınıflarını destekler ama tekrar niceleyicisi yoktur,
-- bu yüzden uzunluk ayrı bir CHECK ile sınırlanıyor.

CREATE TABLE apps (
    -- ^[a-z][a-z0-9-]{0,31}$ — executor'ın app_id kısıtı.
    id              TEXT PRIMARY KEY,

    git_host        TEXT NOT NULL,
    git_owner       TEXT NOT NULL,
    git_repo        TEXT NOT NULL,

    -- Dal YALNIZCA istemcinin sha çözümü için saklanır. Sunucu tarafında
    -- hiçbir yere geçirilmez: panelyd'nin systemd birimi
    -- RestrictAddressFamilies=AF_UNIX ile çalışıyor ve ağa çıkamaz.
    git_branch      TEXT NOT NULL,

    -- Boş = "Dockerfile".
    dockerfile_path TEXT NOT NULL DEFAULT '',

    -- ⚠ Derleme argümanlarının DEĞERLERİ `docker history` çıktısında düz
    -- metin görünür (ölçüldü). Burası sır saklamak için DEĞİLDİR; sırlar
    -- Faz 2'nin kasasına gidecek.
    build_args_json TEXT NOT NULL DEFAULT '{}',

    container_port  INTEGER NOT NULL,
    replicas        INTEGER NOT NULL,
    health_path     TEXT NOT NULL DEFAULT '',
    domain          TEXT NOT NULL DEFAULT '',

    -- Kaynak limitleri. Sıfır KABUL EDİLMEZ — limitsiz konteyner yoktur
    -- (§2.2). Sınırsızlığı temsil edilemez kılmak, doğrulamaktan güçlü.
    memory_bytes    INTEGER NOT NULL,
    cpu_millis      INTEGER NOT NULL,
    blkio_weight    INTEGER NOT NULL,

    -- Sürüm sayacı. Bir sonraki sürümün adı bundan türetilir ve bu satır
    -- sürüm eklemesiyle AYNI transaction'da artırılır; iki eşzamanlı
    -- dağıtım aynı adı üretemez.
    release_seq     INTEGER NOT NULL DEFAULT 0,

    created_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL,

    CHECK (length(id) BETWEEN 1 AND 32),
    CHECK (id GLOB '[a-z]*'),
    CHECK (NOT id GLOB '*[^a-z0-9-]*'),

    CHECK (length(git_host)  BETWEEN 1 AND 253),
    CHECK (length(git_owner) BETWEEN 1 AND 100),
    CHECK (length(git_repo)  BETWEEN 1 AND 100),
    CHECK (length(git_branch) BETWEEN 1 AND 255),

    CHECK (container_port BETWEEN 1 AND 65535),
    CHECK (replicas BETWEEN 1 AND 64),

    CHECK (memory_bytes > 0),
    CHECK (cpu_millis > 0),
    CHECK (blkio_weight BETWEEN 10 AND 1000),

    CHECK (release_seq >= 0)
) STRICT;

-- ══════════════════════════════════════════════════════════════════
--  releases — bir commit'in tek bir derleme denemesi
-- ══════════════════════════════════════════════════════════════════
--
-- Sürüm ≠ commit. Aynı commit birden çok kez dağıtılabilir ve her denemesi
-- ayrı bir sürümdür; geri alma "önceki SÜRÜME dön" demektir, "önceki
-- commit'e" değil.

CREATE TABLE releases (
    app_id      TEXT NOT NULL REFERENCES apps(id),

    -- ── Neden türetilmiyor da saklanıyor? ─────────────────────────
    --
    -- `id` = 'r' || seq olarak üretiliyor, yani teoride türetilebilir.
    -- Ama bu değer HOST TARAFINDAKİ kimliktir: konteynerler
    -- `panely.release_id=<id>` etiketiyle işaretleniyor ve executor
    -- YALNIZCA bu etiketle adresleniyor. Adlandırma şeması bir gün
    -- değişirse, türetilmiş bir kimlik hostta duran gerçek konteynerleri
    -- göstermeyi bırakırdı. Saklanan kimlik bunu imkânsız kılar.
    --
    -- ^[a-z0-9]{1,64}$ — executor'ın release_id kısıtı.
    id          TEXT NOT NULL,

    -- Sıralama içindir. Zaman damgasına göre sıralamak yanlış olurdu:
    -- iki dağıtım aynı nanosaniyeye düşebilir ve sıra belirsizleşir.
    seq         INTEGER NOT NULL,

    -- TAM 40 haneli küçük harf onaltılık.
    commit_sha  TEXT NOT NULL,

    -- 1=BUILDING, 2=BUILT, 3=FAILED (panelyv1.ReleaseStatus ile aynı).
    status      INTEGER NOT NULL,

    -- Docker'ın aux karesinden gelen imaj kimliği.
    image_id    TEXT NOT NULL DEFAULT '',

    started_at  INTEGER NOT NULL,
    finished_at INTEGER NOT NULL DEFAULT 0,
    detail      TEXT NOT NULL DEFAULT '',

    PRIMARY KEY (app_id, id),

    CHECK (length(id) BETWEEN 1 AND 64),
    CHECK (NOT id GLOB '*[^a-z0-9]*'),
    CHECK (seq > 0),

    CHECK (length(commit_sha) = 40),
    CHECK (NOT commit_sha GLOB '*[^0-9a-f]*'),

    CHECK (status BETWEEN 1 AND 3),

    -- ── K-042 ŞEMA KATMANINDA ─────────────────────────────────────
    --
    -- Docker'ın klasik derleyicisi, derleme ORTASINDA ölen bir yapı için
    -- de HTTP 200 döner. Başarının tek pozitif kanıtı `aux` karesinden
    -- gelen imaj kimliğidir.
    --
    -- Bu CHECK, "derlendi ama kanıtı yok" durumunu VERİTABANINDA TEMSİL
    -- EDİLEMEZ yapar. Uygulama katmanındaki bir hata bile böyle bir satır
    -- yazamaz; INSERT'in kendisi patlar.
    CHECK (status != 2 OR image_id != ''),

    -- Derlenmekte olan bir sürümün bitiş zamanı ve imajı olamaz. Yarıda
    -- kalmış kayıtlar bu yüzden BUILDING'de görünür kalır ve Faz 2'nin
    -- öksüz imaj temizliği onları buradan bulur.
    CHECK (status != 1 OR (image_id = '' AND finished_at = 0)),

    -- Biten bir sürümün bitiş zamanı OLMALI.
    CHECK (status = 1 OR finished_at > 0)
) STRICT;

-- Aynı uygulamada iki sürüm aynı sıra numarasını alamaz.
CREATE UNIQUE INDEX idx_releases_app_seq ON releases (app_id, seq);

-- "Bu uygulamanın en yeni sürümleri" birincil sorgu.
CREATE INDEX idx_releases_app_recent ON releases (app_id, seq DESC);

-- Yarıda kalmış derlemeler ayrı sorgulanabilmeli: panelyd yeniden
-- başlatıldığında BUILDING'de asılı kalmış satırlar vardır ve bunlar
-- hostta öksüz imaj bırakmış OLABİLİR.
CREATE INDEX idx_releases_building ON releases (app_id, seq) WHERE status = 1;

-- apps.id silmeye karşı KORUNMUYOR çünkü DeleteApp henüz YOK (TOTP kapısı
-- Faz 2). Yabancı anahtar varsayılan davranışta (NO ACTION) bırakıldı:
-- silmeyi ekleyen kişi, sürümlerin ne olacağına karar vermek ZORUNDA
-- kalsın. ON DELETE CASCADE yazmak, o kararı şimdiden ve sessizce
-- vermek olurdu.
