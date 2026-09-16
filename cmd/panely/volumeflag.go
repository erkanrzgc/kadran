package main

import (
	"errors"
	"flag"
	"fmt"
	"strings"

	panelyv1 "github.com/erkanrzgc/panely/internal/pb/panely/v1"
)

// ════════════════════════════════════════════════════════════════════
//  -volume AD:/yol[:ro|:rw]
// ════════════════════════════════════════════════════════════════════
//
// ── ⚠ Docker'ın `-v` sözdiziminden KASTEN farklı ────────────────────
//
// Docker'da `-v /host/yol:/konteyner` ilk parça HOST YOLUDUR. Panely
// host yolu KABUL ETMİYOR: şemada öyle bir alan yok ve yolu executor
// kuruyor (TOCTOU sınıfı baştan siliniyor).
//
// Bu yüzden ilk parça bir AD, ve yol gibi görünen bir ilk parça AÇIKÇA
// REDDEDİLİYOR. Sessizce ad sanmak, Docker alışkanlığıyla
// `-volume /srv/veri:/veri` yazan birinin host dizinini bağladığını
// sanmasına yol açardı — oysa panely `/srv/veri` adında bir hacim
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
		return volumeSpec{}, errors.New("boş hacim tanımı")
	}
	parts := strings.Split(raw, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return volumeSpec{}, fmt.Errorf(
			"hacim biçimi AD:/bağlama/noktası[:ro] olmalı (%q)", raw)
	}

	name, mount := parts[0], parts[1]
	if name == "" {
		return volumeSpec{}, fmt.Errorf("hacim adı boş (%q)", raw)
	}
	if mount == "" {
		return volumeSpec{}, fmt.Errorf("bağlama noktası boş (%q)", raw)
	}
	// ⚠ Docker alışkanlığını YAKALA ve anlat.
	if strings.HasPrefix(name, "/") || strings.HasPrefix(name, ".") {
		return volumeSpec{}, fmt.Errorf(
			"ilk parça bir HACİM ADI olmalı, host yolu değil (%q). "+
				"Panely host yolu kabul etmez; yolu kendisi kurar "+
				"(/var/lib/panely/volumes/<uygulama>/<ad>)", name)
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
				"bilinmeyen hacim seçeneği %q — yalnızca `ro` veya `rw`", parts[2])
		}
	}
	return spec, nil
}

// volumeList, tekrarlanabilir -volume bayrağının topladıklarıdır.
type volumeList struct {
	vals *[]*panelyv1.AppVolume
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
			return fmt.Errorf("%q birden çok kez verildi", spec.name)
		}
	}
	*l.vals = append(*l.vals, &panelyv1.AppVolume{
		Name:      spec.name,
		MountPath: spec.mountPath,
		ReadOnly:  spec.readOnly,
	})
	return nil
}

// volumeFlag, tekrarlanabilir bir hacim bayrağı tanımlar.
func (c *cli) volumeFlag(fs *flag.FlagSet, name, usage string) *[]*panelyv1.AppVolume {
	vals := []*panelyv1.AppVolume{}
	fs.Var(volumeList{vals: &vals}, name, usage)
	return &vals
}
