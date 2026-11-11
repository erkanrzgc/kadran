// Package winres, masaüstü ikilisinin Windows kaynaklarını (simge ve
// manifest) Go bağlayıcısının okuduğu bir COFF nesnesine (.syso) yazar.
//
// # Neden kendi üretecimiz?
//
// İkili `wails build` ile değil `go build` ile derleniyor; kaynakları
// koyan bir araç yok. Simge olmadan pencere ve görev çubuğu genel simge
// gösterir; manifest olmadan Windows ortak denetimlerin 6. sürümünü
// yüklemez ve yerel onay kutusu (TaskDialogIndirect) bulunamaz. Biçim
// küçük ve sabit: tek .rsrc bölümü, üç kaynak türü. Bir bağımlılık
// eklemek yerine burada yazıldı ve debug/pe ile sınanıyor.
//
// Çıktı belirlenimci: aynı girdi aynı baytları verir; depodaki .syso'nun
// güncel olduğunu test denetler.
package winres

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
)

// Kaynak türleri.
const (
	typeIcon      = 3
	typeGroupIcon = 14
	typeManifest  = 24
)

// AppIconID, Wails'in pencere simgesini aradığı kaynak kimliği (winc.AppIconID).
const AppIconID = 3

// manifestID, sürecin manifesti (CREATEPROCESS_MANIFEST_RESOURCE_ID).
const manifestID = 1

const langEnglishUS = 0x0409

// COFF sabitleri (amd64).
const (
	machineAMD64          = 0x8664
	relAMD64Addr32NB      = 3
	symClassStatic        = 3
	sectionCharacteristic = 0x40000040 // INITIALIZED_DATA | MEM_READ
	fileHeaderSize        = 20
	sectionHeaderSize     = 40
	relocationSize        = 10
	symbolSize            = 18
)

type resource struct {
	typ, id uint16
	data    []byte
}

type iconImage struct {
	width, height, colors, reserved byte
	planes, bitCount                uint16
	data                            []byte
}

// parseICO, .ico dosyasının görüntülerini okur.
func parseICO(ico []byte) ([]iconImage, error) {
	if len(ico) < 6 || binary.LittleEndian.Uint16(ico[0:]) != 0 || binary.LittleEndian.Uint16(ico[2:]) != 1 {
		return nil, errors.New("not an .ico file")
	}
	n := int(binary.LittleEndian.Uint16(ico[4:]))
	if n == 0 || len(ico) < 6+16*n {
		return nil, errors.New("the .ico directory is truncated")
	}
	images := make([]iconImage, 0, n)
	for i := range n {
		e := ico[6+16*i:]
		size := binary.LittleEndian.Uint32(e[8:])
		off := binary.LittleEndian.Uint32(e[12:])
		if uint64(off)+uint64(size) > uint64(len(ico)) {
			return nil, fmt.Errorf("image %d lies outside the .ico file", i)
		}
		images = append(images, iconImage{
			width: e[0], height: e[1], colors: e[2], reserved: e[3],
			planes: binary.LittleEndian.Uint16(e[4:]), bitCount: binary.LittleEndian.Uint16(e[6:]),
			data: ico[off : off+size],
		})
	}
	return images, nil
}

// resources, simge görüntüleri (1…n), simge grubu ve manifest.
func resources(ico, manifest []byte) ([]resource, error) {
	images, err := parseICO(ico)
	if err != nil {
		return nil, err
	}
	var group bytes.Buffer
	for _, v := range []uint16{0, 1, uint16(len(images))} {
		_ = binary.Write(&group, binary.LittleEndian, v)
	}
	out := make([]resource, 0, len(images)+2)
	for i, img := range images {
		id := uint16(i + 1)
		group.Write([]byte{img.width, img.height, img.colors, img.reserved})
		_ = binary.Write(&group, binary.LittleEndian, img.planes)
		_ = binary.Write(&group, binary.LittleEndian, img.bitCount)
		_ = binary.Write(&group, binary.LittleEndian, uint32(len(img.data)))
		_ = binary.Write(&group, binary.LittleEndian, id)
		out = append(out, resource{typ: typeIcon, id: id, data: img.data})
	}
	out = append(out, resource{typ: typeGroupIcon, id: AppIconID, data: group.Bytes()})
	out = append(out, resource{typ: typeManifest, id: manifestID, data: manifest})
	return out, nil
}

func align(n, to int) int { return (n + to - 1) / to * to }

// Build, .syso nesnesinin baytları.
func Build(ico, manifest []byte) ([]byte, error) {
	res, err := resources(ico, manifest)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(res, func(i, j int) bool {
		if res[i].typ != res[j].typ {
			return res[i].typ < res[j].typ
		}
		return res[i].id < res[j].id
	})
	var types []uint16
	byType := map[uint16][]resource{}
	for _, r := range res {
		if len(byType[r.typ]) == 0 {
			types = append(types, r.typ)
		}
		byType[r.typ] = append(byType[r.typ], r)
	}

	// Yerleşim: kök dizin, tür başına dizin, kaynak başına dil dizini,
	// veri girdileri, sonra veriler (8 bayt hizalı).
	dirSize := func(entries int) int { return 16 + 8*entries }
	offset := dirSize(len(types))
	typeDir := map[uint16]int{}
	for _, t := range types {
		typeDir[t] = offset
		offset += dirSize(len(byType[t]))
	}
	langDir := make([]int, len(res))
	for i := range res {
		langDir[i] = offset
		offset += dirSize(1)
	}
	dataEntry := make([]int, len(res))
	for i := range res {
		dataEntry[i] = offset
		offset += 16
	}
	dataAt := make([]int, len(res))
	for i, r := range res {
		offset = align(offset, 8)
		dataAt[i] = offset
		offset += len(r.data)
	}
	section := make([]byte, align(offset, 8))
	le := binary.LittleEndian
	writeDir := func(at, entries int) { le.PutUint16(section[at+14:], uint16(entries)) }
	writeEntry := func(at int, id uint16, target int, subdir bool) {
		le.PutUint32(section[at:], uint32(id))
		v := uint32(target)
		if subdir {
			v |= 0x80000000
		}
		le.PutUint32(section[at+4:], v)
	}

	writeDir(0, len(types))
	i := 0
	for ti, t := range types {
		writeEntry(16+8*ti, t, typeDir[t], true)
		writeDir(typeDir[t], len(byType[t]))
		for ri, r := range byType[t] {
			writeEntry(typeDir[t]+16+8*ri, r.id, langDir[i], true)
			writeDir(langDir[i], 1)
			writeEntry(langDir[i]+16, langEnglishUS, dataEntry[i], false)
			le.PutUint32(section[dataEntry[i]:], uint32(dataAt[i])) // bağlayıcı bölümün RVA'sını ekler
			le.PutUint32(section[dataEntry[i]+4:], uint32(len(r.data)))
			copy(section[dataAt[i]:], r.data)
			i++
		}
	}

	var obj bytes.Buffer
	rawAt := fileHeaderSize + sectionHeaderSize
	relocAt := rawAt + len(section)
	symAt := relocAt + relocationSize*len(res)
	// Dosya başlığı.
	for _, v := range []any{uint16(machineAMD64), uint16(1), uint32(0), uint32(symAt), uint32(1), uint16(0), uint16(0)} {
		_ = binary.Write(&obj, le, v)
	}
	// Bölüm başlığı.
	obj.WriteString(".rsrc\x00\x00\x00")
	for _, v := range []any{uint32(0), uint32(0), uint32(len(section)), uint32(rawAt), uint32(relocAt), uint32(0), uint16(len(res)), uint16(0), uint32(sectionCharacteristic)} {
		_ = binary.Write(&obj, le, v)
	}
	obj.Write(section)
	// Her veri girdisinin adresi bölüm simgesine göre yer değiştirir.
	for _, at := range dataEntry {
		for _, v := range []any{uint32(at), uint32(0), uint16(relAMD64Addr32NB)} {
			_ = binary.Write(&obj, le, v)
		}
	}
	// Simge tablosu: tek bölüm simgesi; ardından boş metin tablosu.
	obj.WriteString(".rsrc\x00\x00\x00")
	for _, v := range []any{uint32(0), int16(1), uint16(0), uint8(symClassStatic), uint8(0), uint32(4)} {
		_ = binary.Write(&obj, le, v)
	}
	return obj.Bytes(), nil
}
