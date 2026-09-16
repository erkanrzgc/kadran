package dockerdrv

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
)

// ════════════════════════════════════════════════════════════════════
//  HACİM SAHİPLİĞİ
// ════════════════════════════════════════════════════════════════════
//
// ── Neden gerekli? ÖLÇÜLDÜ ───────────────────────────────────────────
//
// Bind bağlaması için host dizini yoksa Docker onu KENDİSİ oluşturur ve
// sahibi root:root olur. Canlı sunucuda ölçüldü:
//
//	docker run -v <kök>/_probe/data:/veri --user 101:101 alpine \
//	  sh -c 'echo x > /veri/test.txt'
//	→ sh: can't create /veri/test.txt: Permission denied
//
// Panely'nin sertleştirilmiş imajları (nginx-unprivileged vb.) uid 101
// ile koşuyor. Yani dizini Docker'a bıraktığımızda hacim bağlanır,
// konteyner başlar, ve uygulama kendi kalıcı diskine YAZAMAZ. Hata
// dağıtımdan sonra, uygulamanın kendi günlüğünde ortaya çıkar.
//
// ── Sahip neden İMAJDAN okunuyor? ───────────────────────────────────
//
// Doğru uid zaten imajın `Config.User` alanında yazılı. Kullanıcıdan
// istemek (`-volume data:/veri:uid=101`) aynı bilgiyi ikinci kez, bu kez
// yanlış yazılabilecek biçimde sormak olurdu — ve yanlış yazıldığında
// belirti yine "uygulama yazamıyor" olurdu.

// chownDir, hacim dizininin sahibini ayarlar.
//
// Değişken olması TESTİN ŞARTI: `os.Chown` Windows'ta desteklenmiyor ve
// bu paketin testleri CI matrisinde Windows'ta da koşuyor. Gerçek chown'a
// bağlı bir test yalnızca Linux'ta koşardı; "derleniyor ama koşmuyor"
// boşluğu bu projede daha önce gerçek hata sakladı.
//
// Aynı dikiş `mountinfoPath` için de var ve gerekçesi aynı. Değişken
// paket dışına açık değil — yalnızca bu paketin testleri değiştirebilir.
var chownDir = platformChownDir

// imageInspect, imaj sorgusunun umursadığımız kadarıdır.
type imageInspect struct {
	Config struct {
		User string `json:"User"`
	} `json:"Config"`
}

// imageUID, imajın çalışacağı kullanıcı kimliğini döndürür.
//
// ── Neden yalnızca SAYISAL kabul ediliyor? ──────────────────────────
//
// `USER nginx` geçerli bir Dockerfile satırıdır, ama adı kimliğe
// çevirmek imajın `/etc/passwd`'ını okumayı gerektirir — ayrıcalıklı
// sürece bir dosya sistemi ayrıştırıcısı eklemek demek.
//
// Sessizce 0'a düşmek KABUL EDİLEMEZ: hacim root'a ait olur, uygulama
// yazamaz, ve arıza dağıtımdan saatler sonra başka bir yerde görünür.
// Açıkça reddetmek, kullanıcıya Dockerfile'ında `USER 101` yazmasını
// söyleyen anlaşılır bir hata üretir.
func (c *Client) imageUID(ctx context.Context, tag string) (int, error) {
	var ins imageInspect
	if err := c.doJSON(ctx, http.MethodGet, "/images/"+tag+"/json", nil, nil, &ins); err != nil {
		return 0, fmt.Errorf("imaj kullanıcısı okunamadı (%s): %w", tag, err)
	}

	// Boş USER "root" demektir ve GEÇERLİDİR: USER yazmayan bir imaj
	// bugün canlıda çalışıyor (ölçüldü).
	user := ins.Config.User
	if user == "" {
		return 0, nil
	}
	// "uid:gid" biçimi geçerli; grup kısmı atılır, chown ikisine de aynı
	// değeri veriyor (hacim uygulamaya ait, paylaşılmıyor).
	uid, _, _ := strings.Cut(user, ":")
	n, err := strconv.Atoi(uid)
	if err != nil || n < 0 {
		return 0, fmt.Errorf(
			"imajın USER'ı sayısal değil (%q) — hacim sahibi belirlenemez. "+
				"Dockerfile'da sayısal kimlik kullanın (ör. USER 101)", user)
	}
	return n, nil
}

// prepareVolumes, hacim dizinlerini oluşturur ve sahibini ayarlar.
//
// ⚠ VAR OLAN DİZİN KORUNUR. Bu yol her dağıtımda, her geri almada ve her
// iyileştirmede yeniden koşuyor; dizini yeniden oluşturan bir uygulama
// kalıcı diski her dağıtımda silerdi — özelliği tam tersine çevirirdi.
// `MkdirAll` var olanı olduğu gibi bırakır, `Chown` yalnızca sahibi
// yazar; ikisi de idempotenttir.
//
// Sahip her seferinde YENİDEN yazılıyor ve bu kasıtlı: imajın `USER`'ı
// sürümler arasında değişebilir ve o durumda hacim yeni kullanıcıya
// geçmelidir. Bir kez ayarlayıp bırakmak, kullanıcı değişince sessizce
// yazılamaz hâle gelen bir hacim bırakırdı.
func (c *Client) prepareVolumes(ctx context.Context, spec CreateSpec) error {
	if len(spec.Mounts) == 0 {
		return nil
	}
	uid, err := c.imageUID(ctx, ImageTag(spec.AppID, spec.CommitSHA))
	if err != nil {
		return err
	}
	for _, m := range spec.Mounts {
		dir := c.volumePath(spec.AppID, m.VolumeName)
		// 0750: sahibi okur/yazar, grup okur, diğerleri hiçbir şey.
		// Konteyner dizini SAHİBİ olarak görüyor, yani gevşetmeye gerek
		// yok; 0777 denendi ve reddedildi — hacim kökü `--x` taşıdığı
		// için hosttaki herhangi bir kullanıcı bilinen yola yazabilirdi.
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("hacim dizini oluşturulamadı (%s): %w", dir, err)
		}
		if err := chownDir(dir, uid); err != nil {
			return fmt.Errorf("hacim sahibi ayarlanamadı (%s → uid %d): %w",
				dir, uid, err)
		}
	}
	return nil
}
