-- K-123: ortam değişkeni değerleri kasada.
--
-- Satır VARSA apps.env_json'daki bütün değerler `recipient` alıcısına
-- mühürlü. Yoksa (taze kurulum, eski sürümden yükseltme, eski bir yedeğin
-- geri yüklenmesi, geri dönüş betiği) daemon açılışta bütün değerleri
-- mühürler ve satırı yazar. Bir değerin mühürlü olup olmadığı önekinden
-- çıkarılmıyor: `age:` ile başlayan düz bir değer atlanırdı.
CREATE TABLE env_seal (
    id        INTEGER PRIMARY KEY CHECK (id = 1),
    recipient TEXT    NOT NULL,
    sealed_at INTEGER NOT NULL
) STRICT;
