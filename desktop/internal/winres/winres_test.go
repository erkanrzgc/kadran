package winres

import (
	"bytes"
	"debug/pe"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// sysoPath, depodaki üretilmiş nesne: `go build` onu kendiliğinden bağlar.
var sysoPath = filepath.Join("..", "..", "rsrc_windows_amd64.syso")

func inputs(t *testing.T) ([]byte, []byte) {
	t.Helper()
	ico, err := os.ReadFile(filepath.Join("..", "..", "assets", "kadran.ico"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := os.ReadFile(filepath.Join("..", "..", "assets", "kadran.manifest"))
	if err != nil {
		t.Fatal(err)
	}
	return ico, manifest
}

// TestSysoIsCurrent: depodaki .syso, simge ve manifestten yeniden üretilenle
// aynı. Değiştiyse: KADRAN_UPDATE_SYSO=1 go test ./internal/winres
func TestSysoIsCurrent(t *testing.T) {
	ico, manifest := inputs(t)
	want, err := Build(ico, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if os.Getenv("KADRAN_UPDATE_SYSO") == "1" {
		if err := os.WriteFile(sysoPath, want, 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	got, err := os.ReadFile(sysoPath)
	if err != nil {
		t.Fatalf("%s okunamadı (KADRAN_UPDATE_SYSO=1 ile üret): %v", sysoPath, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal(".syso güncel değil; KADRAN_UPDATE_SYSO=1 ile yenile")
	}
}

// resolve, kaynak ağacında tür → kimlik → dil yolunu izler ve verinin
// bölüm içi adresini (yer değiştirme toplamı) ve boyunu döner.
func resolve(t *testing.T, sec []byte, typ, id uint16) (int, int) {
	t.Helper()
	le := binary.LittleEndian
	find := func(dir int, want uint16) uint32 {
		named, ids := int(le.Uint16(sec[dir+12:])), int(le.Uint16(sec[dir+14:]))
		for i := range named + ids {
			e := dir + 16 + 8*i
			if uint16(le.Uint32(sec[e:])) == want {
				return le.Uint32(sec[e+4:])
			}
		}
		t.Fatalf("kaynak bulunamadı: tür %d kimlik %d", typ, id)
		return 0
	}
	typeDir := find(0, typ)
	if typeDir&0x80000000 == 0 {
		t.Fatal("tür girdisi alt dizin değil")
	}
	idDir := find(int(typeDir&0x7fffffff), id)
	langDir := int(idDir & 0x7fffffff)
	entry := int(le.Uint32(sec[langDir+16+4:]))
	return int(le.Uint32(sec[entry:])), int(le.Uint32(sec[entry+4:]))
}

func TestObjectParsesAndCarriesIconAndManifest(t *testing.T) {
	ico, manifest := inputs(t)
	obj, err := Build(ico, manifest)
	if err != nil {
		t.Fatal(err)
	}
	f, err := pe.NewFile(bytes.NewReader(obj))
	if err != nil {
		t.Fatal(err)
	}
	if f.Machine != pe.IMAGE_FILE_MACHINE_AMD64 || len(f.Sections) != 1 || f.Sections[0].Name != ".rsrc" {
		t.Fatalf("başlık: %+v", f.FileHeader)
	}
	sec, err := f.Sections[0].Data()
	if err != nil {
		t.Fatal(err)
	}
	images, err := parseICO(ico)
	if err != nil {
		t.Fatal(err)
	}
	// Her veri girdisi yer değiştirir: simgeler + grup + manifest.
	if got := len(f.Sections[0].Relocs); got != len(images)+2 {
		t.Fatalf("yer değiştirme %d", got)
	}
	for _, r := range f.Sections[0].Relocs {
		if r.Type != relAMD64Addr32NB || r.SymbolTableIndex != 0 {
			t.Fatalf("yer değiştirme %+v", r)
		}
	}
	if len(f.Symbols) != 1 || f.Symbols[0].Name != ".rsrc" || f.Symbols[0].SectionNumber != 1 {
		t.Fatalf("simgeler %+v", f.Symbols)
	}
	at, size := resolve(t, sec, typeManifest, manifestID)
	if !bytes.Equal(sec[at:at+size], manifest) {
		t.Fatal("manifest bozuk")
	}
	at, size = resolve(t, sec, typeGroupIcon, AppIconID)
	group := sec[at : at+size]
	if binary.LittleEndian.Uint16(group[4:]) != uint16(len(images)) || size != 6+14*len(images) {
		t.Fatalf("simge grubu %d bayt", size)
	}
	for i, img := range images {
		e := group[6+14*i:]
		id := binary.LittleEndian.Uint16(e[12:])
		if e[0] != img.width || binary.LittleEndian.Uint32(e[8:]) != uint32(len(img.data)) {
			t.Fatalf("grup girdisi %d", i)
		}
		at, size := resolve(t, sec, typeIcon, id)
		if !bytes.Equal(sec[at:at+size], img.data) {
			t.Fatalf("simge %d bozuk", id)
		}
	}
}

func TestRejectsNonIcon(t *testing.T) {
	for _, bad := range [][]byte{nil, []byte("PNG"), {0, 0, 2, 0, 1, 0}, {0, 0, 1, 0, 9, 0}} {
		if _, err := Build(bad, []byte("<x/>")); err == nil {
			t.Errorf("%v kabul edildi", bad)
		}
	}
}
