package exec

import "strings"

// ══════════════════════════════════════════════════════════════════════
//  Depo beyaz listesi (--allow-repo) — derlenebilecek depoları sınırlar
// ══════════════════════════════════════════════════════════════════════
//
// # Neden var?
//
// K-167'den beri özel depoları kadran-git indirir; yürütücüde jeton yok ve
// hangi özel depoların okunabileceğini GitHub'daki uygulama kurulumu belirler
// (kullanıcının seçtiği depolar). Bu liste ONUN ÜSTÜNE konan, root'un yazdığı
// ikinci kapıdır: kadrand ele geçirilse de değiştiremez. Verilmişse HER
// derlemede (özel ya da herkese açık) kesişim olarak uygulanır.
//
// Saldırı anlatımı hâlâ geçerli: ImageBuild çağırabilen biri owner/repo'yu
// kurbanın deposuna çevirirse, Dockerfile OLMASI GEREKMEZ — `dockerfile_path`
// de istekten geliyor ve derleme çıktısı istemciye AYNEN akıyor. Kurulum
// seçimi bu saldırıyı kurulu depolarla sınırlar; --allow-repo daha da daraltır.
//
// # Neden kurulum seçimi tek başına yetmeyebilir?
//
// "All repositories" kurulumu bütün depoları açar (Kadran yalnız uyarır).
// Onu kısmak isteyen operatör listeyi burada verir.
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
// Boş liste "ek kısıt yok" demek. Herkese açık depolar zaten herkese açık;
// özel depolar için sınırı yürütücü değil GitHub kurulumu koyar (K-167).
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
