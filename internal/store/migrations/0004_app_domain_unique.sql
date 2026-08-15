-- Bir alan adı EN FAZLA bir uygulamaya ait olabilir.
--
-- ── Neden şemada, doğrulamada değil? ────────────────────────────────
--
-- İki uygulama aynı alan adını taşıdığında `proxydrv.BuildConfig` TÜM
-- yapılandırmayı reddediyor (haklı olarak — hangisinin kazandığı sıraya
-- bağlı olurdu). Sonuç tek bir uygulamanın bozulması değil:
--
--   * o andan sonra hiçbir uzlaştırma başarılı olamaz
--   * panelyd yeniden başladığında sıfırdan uzlaştırır ve HİÇBİR rota
--     kurulamaz → sunucudaki BÜTÜN siteler düşer
--
-- Bunu bir doğrulayıcıya bırakmak, o doğrulayıcıyı atlayan her yolun
-- (göç betiği, elle SQL, ileride eklenecek bir RPC) aynı deliği yeniden
-- açması demekti. Şema kısıtı böyle bir yol bırakmıyor.
--
-- ── Neden KISMİ indeks? ─────────────────────────────────────────────
--
-- Alan adı olmayan uygulama geçerli ve YAYGIN: yalnızca iç ağdan
-- erişilen bir iş yükünün ters vekilde görünmesi gerekmez. Boş dize
-- tekrarlanabilmeli; düz bir UNIQUE indeks ikinci alan adsız uygulamayı
-- reddederdi.
CREATE UNIQUE INDEX IF NOT EXISTS apps_domain_unique
    ON apps (domain)
    WHERE domain != '';
