package exec

import "strings"

// ══════════════════════════════════════════════════════════════════════
//  Depo beyaz listesi — kimlik bilgisinin ERİŞİMİNİ sınırlar
// ══════════════════════════════════════════════════════════════════════
//
// # Neden var?
//
// Özel depoları derleyebilmek için hostta bir git kimlik bilgisi duruyor
// ve dockerd onu kullanıyor (ölçüldü: dockerd root'un credential
// helper'ına danışıyor, bkz. K-056). Kimlik bilgisi RPC'den GEÇMİYOR —
// bu iyi — ama bir bedeli var: token'ın erişebildiği HER depo, Panely'nin
// de erişebildiği depo hâline geliyor.
//
// Somut saldırı: ImageBuild çağırabilen biri owner/repo'yu kurbanın özel
// deposuna çevirir. Kurban deposunda Dockerfile OLMASI GEREKMEZ —
// `dockerfile_path` de istekten geliyor ve derleme çıktısı istemciye
// AYNEN akıyor. Yani kaynak, hiçbir konteyner çalışmadan sızar.
//
// # Neden "ince taneli token" tek başına yetmez?
//
// Yetmez çünkü DOĞRULANAMAZ: operatörün token'ı hangi depolara açtığını
// executor göremez. Beyaz liste, zorlanabilir olan yarısıdır. İkisi
// birlikte kullanılmalı.
//
// # Bozuk girdi KAPALI tarafa düşer
//
// Girdiler burada ayrıştırılmıyor; doğrudan karşılaştırılıyor. Yazım
// hatası olan bir girdi hiçbir depoyla eşleşmez, yani derleme REDDEDİLİR.
// Can sıkıcı ama güvenli yön: sessizce fazla izin veren bir kısıt, hiç
// olmayan bir kısıttan daha kötüdür.
//
// # Büyük-küçük harf
//
// GitHub ve GitLab owner/repo adlarında harf ayrımı yapmıyor; `Owner/Repo`
// ile `owner/repo` aynı depodur. EqualFold kullanılmasaydı kısıt, harf
// değiştirerek atlanabilirdi.

// repoAllowed, deponun derlenmesine izin verilip verilmediğini bildirir.
//
// Boş liste "kısıt yok" demek. Bu, hostta kimlik bilgisi YOKKEN doğru
// davranış (herkese açık depolar zaten herkese açık); kimlik bilgisi
// varken tehlikeli olurdu ve o birleşimi kurulum betiği engelliyor.
func repoAllowed(owner, repo string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	want := owner + "/" + repo
	for _, entry := range allowed {
		if strings.EqualFold(strings.TrimSpace(entry), want) {
			return true
		}
	}
	return false
}
