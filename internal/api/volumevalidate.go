package api

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"

	panelyv1 "github.com/erkanrzgc/panely/internal/pb/panely/v1"
)

// ════════════════════════════════════════════════════════════════════
//  HACİM DOĞRULAMASI
// ════════════════════════════════════════════════════════════════════
//
// ── Sınırlar executor'ınkiyle AYNI olmak ZORUNDA ────────────────────
//
// Buradaki desen, sayılar ve yasak kökler `internal/exec/validate.go`
// ile birebir aynıdır. Kopya olmaları kasıtlı: panelyd ayrıcalıklı
// paketi içe aktarmıyor ve aktarmamalı — yetki sınırı ikilinin
// kendisinde, ortak bir kütüphanede değil.
//
// ⚠ Buradaki kontrol executor'ınkinden GEVŞEK OLAMAZ. Gevşek olsaydı
// `app create` tanımı kabul eder, ilk `deploy` executor tarafından
// reddedilirdi: kullanıcı geçerli sandığı bir kayıtla kalır ve hatayı
// tamamen başka bir komutta, günler sonra görür. Aynı tuzak env'de
// ölçüldü (API değer başına, executor toplam üzerinden sınırlıyordu).
//
// SIKI olması ise zararsız: burada reddedilen bir tanım zaten hiçbir
// zaman executor'a ulaşmaz.

var volumeNamePat = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

const (
	maxVolumes   = 16
	maxMountPath = 4096 // PATH_MAX
)

// forbiddenMountRoots, konteyner içinde üzerine bağlanamayacak çekirdek
// arayüzleridir. Bunları gölgelemek, uygulamanın kendi süreç bilgisini
// ya da cihazlarını göremez hâle gelmesi demektir — ve belirtisi
// "uygulama açılmıyor" değil, çok daha tuhaf şeylerdir.
var forbiddenMountRoots = []string{"/proc", "/sys", "/dev"}

// validateVolumes, hacim listesini doğrular.
func validateVolumes(vols []*panelyv1.AppVolume) error {
	if len(vols) > maxVolumes {
		return fmt.Errorf("çok fazla hacim (%d, sınır %d)", len(vols), maxVolumes)
	}
	seenNames := make(map[string]bool, len(vols))
	seenPaths := make([]string, 0, len(vols))

	for i, v := range vols {
		if v == nil {
			return fmt.Errorf("hacim %d boş", i)
		}
		name := v.GetName()
		if !volumeNamePat.MatchString(name) {
			return fmt.Errorf("hacim adı geçersiz (%q) — "+
				"^[a-z0-9][a-z0-9-]{0,63}$ olmalı", name)
		}
		// Aynı ad iki kez: ikisi de diskte AYNI dizini gösterirdi ve
		// hangi bağlama noktasının geçerli olduğu belirsiz kalırdı.
		if seenNames[name] {
			return fmt.Errorf("hacim adı %q iki kez verildi", name)
		}
		seenNames[name] = true

		mp := v.GetMountPath()
		if err := validateMountPath(mp); err != nil {
			return fmt.Errorf("hacim %q: %w", name, err)
		}
		// İç içe geçen bağlama noktaları, hangi hacmin görüneceğini
		// bağlama SIRASINA bırakır — güvenlik sınırında belirsiz
		// davranış kabul edilemez.
		for _, prev := range seenPaths {
			if pathOverlaps(prev, mp) {
				return fmt.Errorf("bağlama noktası %q ile %q çakışıyor", prev, mp)
			}
		}
		seenPaths = append(seenPaths, mp)
	}
	return nil
}

// validateMountPath, konteyner İÇİNDEKİ bağlama noktasını denetler.
//
// Host yolu hiçbir yerde alınmadığı için buradaki kontroller host'u
// korumaz; konteyner içinde tutarlı ve öngörülebilir bir dosya sistemi
// sağlarlar.
func validateMountPath(p string) error {
	switch {
	case p == "":
		return errors.New("bağlama noktası boş olamaz")
	case len(p) > maxMountPath:
		return fmt.Errorf("bağlama noktası çok uzun (%d bayt)", len(p))
	case !path.IsAbs(p):
		return fmt.Errorf("bağlama noktası mutlak olmalı (%q)", p)
	// Temizlik kontrolü `..` denetimini de kapsar: path.Clean("/a/../b")
	// "/b" döner, yani girdiden farklıdır. Ayrıca "//a", "/a/./b" ve
	// "/a/" gibi AYNI yeri gösteren farklı yazımları da eler — bunlar
	// elenmezse aşağıdaki çakışma kontrolü atlatılabilirdi.
	case path.Clean(p) != p:
		return fmt.Errorf("bağlama noktası temiz değil (%q, beklenen %q)",
			p, path.Clean(p))
	case p == "/":
		return errors.New("bağlama noktası kök dizin olamaz")
	}
	for _, root := range forbiddenMountRoots {
		if p == root || strings.HasPrefix(p, root+"/") {
			return fmt.Errorf("bağlama noktası %q altına düşemez (%q)", root, p)
		}
	}
	return nil
}

// pathOverlaps, iki temiz mutlak yolun aynı ya da iç içe olup olmadığını
// söyler. Her iki yolun da path.Clean'den geçtiği varsayılır.
func pathOverlaps(a, b string) bool {
	return a == b ||
		strings.HasPrefix(a, b+"/") ||
		strings.HasPrefix(b, a+"/")
}

// validateVolumeRemove, ekleme ile ayırmanın ÇELİŞMEDİĞİNİ doğrular.
//
// Aynı hacmi hem tanımlayıp hem ayırmak iki zıt niyet taşır. Sessizce
// birini seçmek, hangisinin uygulandığını kullanıcı için belirsiz
// bırakırdı.
func validateVolumeRemove(set []*panelyv1.AppVolume, remove []string) error {
	for _, name := range remove {
		if !volumeNamePat.MatchString(name) {
			return fmt.Errorf("ayrılacak hacim adı geçersiz (%q)", name)
		}
		for _, v := range set {
			if v.GetName() == name {
				return fmt.Errorf("%q hem tanımlanıyor hem ayrılıyor — "+
					"hangisini istediğinizi belirtin", name)
			}
		}
	}
	return nil
}
