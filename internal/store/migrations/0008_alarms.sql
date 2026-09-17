-- Etkin alarmlar: "şu an bozuk olan şeyler" kümesi.
--
-- ── Neden bir TABLO, bellekte bir harita değil ──────────────────────
--
-- Alarm KENAR TETİKLEMELİ olmak zorunda: durum bozulduğunda BİR KEZ
-- haber verilir, düzeldiğinde bir kez daha. Her gözetim turunda tekrar
-- haber vermek, bu projenin zaten ölümcül saydığı yanlış-alarm
-- döngüsüdür — "yanlış alarm veren bir kontrol kapatılmaya mahkûmdur"
-- (K-024) ve tekrarlayan sahte alarmların sonu, gerçek olanın da yok
-- sayılmasıdır.
--
-- Kenar tetikleme "daha önce haber verdim mi?" bilgisini gerektirir ve
-- o bilgi panelyd'nin ÖMRÜNDEN UZUN yaşamalı. Bellekte tutulsaydı
-- `Restart=on-failure` ile çöküp kalkan bir daemon her açılışta
-- BÜTÜN alarmları yeniden ateşlerdi — yani en çok gürültüyü tam da en
-- kötü durumda (çökme döngüsü) üretirdi.
--
-- `health.Supervisor` bilerek bellekte durum tutuyor ve girdileri
-- siliyor; o durum bir TUR boyunca anlamlı. Alarm durumu turlar ve
-- süreçler arası anlamlı olmak zorunda, bu yüzden paylaşılmıyor.
--
-- ── Neden GEÇMİŞ değil, ETKİN KÜME ──────────────────────────────────
--
-- Bu tablo "ne oldu" sorusunun cevabı DEĞİL; o soru denetim zincirinin
-- işi ve alarm açılış/kapanışları oraya da yazılıyor. Burası yalnızca
-- "şu an ne bozuk" sorusunu cevaplıyor, dolayısıyla kapanan alarm
-- satırı SİLİNİYOR. Geçmişi de burada tutmak, tablonun sınırsız
-- büyümesi ve "etkin" sorgusunun her seferinde filtrelenmesi demekti.
CREATE TABLE alarms (
    -- Alarmın KİMLİĞİ: tür + hedef. Örn. "heal_exhausted:pfprobe".
    --
    -- Birincil anahtar olması kenar tetiklemenin kendisi: aynı koşul
    -- ikinci kez yükseltilmek istendiğinde INSERT çakışır ve bunu
    -- "zaten haber verildi" olarak okuyoruz. Yani tekilleştirme
    -- uygulama mantığında değil, ŞEMADA zorlanıyor.
    id TEXT PRIMARY KEY,

    -- Alarm türü (heal_exhausted, backup_failed, proxy_unreconciled,
    -- disk_low). Türe göre sorgulamak ve sunmak için ayrı tutuluyor.
    kind TEXT NOT NULL,

    -- Neyin bozuk olduğu: uygulama kimliği, "panely.db", "host" gibi.
    target TEXT NOT NULL,

    -- Ciddiyet: "uyari" | "kritik". Metin, sayı değil — sayısal bir
    -- seviye okuyucuyu eşik ezberlemeye zorlar ve sıralama dışında bir
    -- şey kazandırmaz.
    severity TEXT NOT NULL,

    -- Koşulun BAŞLADIĞI an (unix saniye). "Ne zamandır bozuk"
    -- sorusunun cevabı; alarm listesinde en çok bakılan alan.
    since INTEGER NOT NULL,

    -- İnsan okuyacak açıklama.
    --
    -- ⚠ Buraya kullanıcı verisi ya da hata METNİ yazılmamalı: alarm
    -- listesi denetim zincirine de düşüyor ve zincir ekle-sadece'dir.
    -- Derleme hatası gibi metinler kullanıcının deposundan gelen içerik
    -- taşıyabilir (K-053'ün aynı gerekçesi).
    detail TEXT NOT NULL
) STRICT;

-- Türe göre listeleme ve "bu türden etkin alarm var mı" sorgusu için.
CREATE INDEX alarms_kind_idx ON alarms(kind);
