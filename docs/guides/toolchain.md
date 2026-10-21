# Go araç zinciri

Kök modül, ayrı Caddy modülü ve CI Go **1.26.10** gerektirir. Bu sürüm, desteklenen Go 1.26 dalının güvenlik yamalarını (GO-2026-6809, crypto/x509 dahil) içerir; güncel ana dal Go 1.27.2'dir. [Resmî sürüm geçmişi](https://go.dev/doc/devel/release).

`GOTOOLCHAIN=auto` açıkken eski yerel Go, modüldeki sürümü indirip doğrular. Alternatif olarak derleme komutunu `GOTOOLCHAIN=go1.26.10` ile çalıştır. Sistem Go kurulumunu değiştirmek gerekmez. Daha eski bir sürüme sabitlemek derlemeyi engeller.

Her iki modülü ayrı tara: depo kökünde `govulncheck ./...`, ardından `build/caddy` içinde aynı komut. Yeşil tarama yalnız erişilebilir çağrı yolları için sonuçtur. `-show verbose` çıktısındaki GO-2026-5932, kullanılmayan `x/crypto/openpgp` paketi için modül düzeyinde bildirimdir; bu paketin hiçbir sürümünde düzeltme yoktur ve projeye eklenmemelidir. [Go zafiyet kaydı](https://pkg.go.dev/vuln/GO-2026-5932).
