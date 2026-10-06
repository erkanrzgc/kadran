package main

import (
	"errors"
	"flag"
	"fmt"
	"strings"

	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
)

// ════════════════════════════════════════════════════════════════════
//  -volume AD:/yol[:ro|:rw]
// ════════════════════════════════════════════════════════════════════
//
// ── ⚠ Docker'ın `-v` sözdiziminden KASTEN farklı ────────────────────
//
// Docker'da `-v /host/yol:/konteyner` ilk parça HOST YOLUDUR. Kadran
// host yolu KABUL ETMİYOR: şemada öyle bir alan yok ve yolu executor
// kuruyor (TOCTOU sınıfı baştan siliniyor).
//
// Bu yüzden ilk parça bir AD, ve yol gibi görünen bir ilk parça AÇIKÇA
// REDDEDİLİYOR. Sessizce ad sanmak, Docker alışkanlığıyla
// `-volume /srv/veri:/veri` yazan birinin host dizinini bağladığını
// sanmasına yol açardı — oysa kadran `/srv/veri` adında bir hacim
// yaratmaya çalışır (ve ad deseni onu zaten reddeder, ama hata mesajı
// sebebi anlatmazdı).

// volumeSpec, ayrıştırılmış tek bir -volume değeridir.
type volumeSpec struct {
	name      string
	mountPath string
	readOnly  bool
}

// parseVolumeFlag, "AD:/yol" ya da "AD:/yol:ro" biçimini çözer.
//
// Ayrı bir fonksiyon olması testin gereği: biçim hatalarını bir FlagSet
// kurmadan yan yana sınamayı sağlıyor.
func parseVolumeFlag(raw string) (volumeSpec, error) {
	if raw == "" {
		return volumeSpec{}, errors.New("empty volume definition")
	}
	parts := strings.Split(raw, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return volumeSpec{}, fmt.Errorf(
			"volume must look like NAME:/mount/point[:ro] (%q)", raw)
	}

	name, mount := parts[0], parts[1]
	if name == "" {
		return volumeSpec{}, fmt.Errorf("volume name is empty (%q)", raw)
	}
	if mount == "" {
		return volumeSpec{}, fmt.Errorf("mount point is empty (%q)", raw)
	}
	// ⚠ Docker alışkanlığını YAKALA ve anlat.
	if strings.HasPrefix(name, "/") || strings.HasPrefix(name, ".") {
		return volumeSpec{}, fmt.Errorf(
			"the first part must be a VOLUME NAME, not a host path (%q). "+
				"Kadran does not accept host paths; it creates the path itself "+
				"(/var/lib/kadran/volumes/<app>/<name>)", name)
	}

	spec := volumeSpec{name: name, mountPath: mount}
	if len(parts) == 3 {
		switch parts[2] {
		case "ro":
			spec.readOnly = true
		case "rw":
			// Varsayılan zaten yazılabilir. Açıkça yazabilmek niyeti
			// belgeliyor; reddetmek kullanıcıyı "neden ro çalışıyor da
			// rw çalışmıyor" sorusuna iterdi.
			spec.readOnly = false
		default:
			return volumeSpec{}, fmt.Errorf(
				"unknown volume option %q — only `ro` or `rw`", parts[2])
		}
	}
	return spec, nil
}

// volumeList, tekrarlanabilir -volume bayrağının topladıklarıdır.
type volumeList struct {
	vals *[]*kadranv1.AppVolume
}

func (l volumeList) String() string {
	if l.vals == nil || len(*l.vals) == 0 {
		return ""
	}
	names := make([]string, 0, len(*l.vals))
	for _, v := range *l.vals {
		names = append(names, v.GetName())
	}
	return strings.Join(names, ",")
}

func (l volumeList) Set(raw string) error {
	spec, err := parseVolumeFlag(raw)
	if err != nil {
		return err
	}
	// Sunucu da reddediyor ama istemcide yakalamak kullanıcıyı ağ gidiş
	// dönüşü beklemeden uyarır. stringMapFlag ile aynı kural: sessizce
	// üzerine yazmak, hangi tanımın geçerli olduğunu belirsiz bırakırdı.
	for _, v := range *l.vals {
		if v.GetName() == spec.name {
			return fmt.Errorf("%q given more than once", spec.name)
		}
	}
	*l.vals = append(*l.vals, &kadranv1.AppVolume{
		Name:      spec.name,
		MountPath: spec.mountPath,
		ReadOnly:  spec.readOnly,
	})
	return nil
}

// volumeFlag, tekrarlanabilir bir hacim bayrağı tanımlar.
func (c *cli) volumeFlag(fs *flag.FlagSet, name, usage string) *[]*kadranv1.AppVolume {
	vals := []*kadranv1.AppVolume{}
	fs.Var(volumeList{vals: &vals}, name, usage)
	return &vals
}
