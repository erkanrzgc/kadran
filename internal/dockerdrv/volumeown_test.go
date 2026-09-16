package dockerdrv

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ════════════════════════════════════════════════════════════════════
//  HACİM SAHİPLİĞİ
// ════════════════════════════════════════════════════════════════════
//
// ── Bu testlerin var olma sebebi CANLIDA ÖLÇÜLDÜ ────────────────────
//
// Hacimler bağlanıyordu ama hiçbir yerde `MkdirAll` yoktu; dizini Docker
// kendisi oluşturuyordu ve sahibi root:root oluyordu. Canlı sunucuda
// ölçüldü:
//
//	docker run -v .../_probe/data:/veri --user 101:101 alpine \
//	  sh -c 'echo x > /veri/test.txt'
//	→ sh: can't create /veri/test.txt: Permission denied
//
// Panely'nin canlı imajlarının ikisi `USER 101` ile koşuyor. Yani hacim
// "çalışıyor" görünür, konteyner başlar, ve uygulama kendi diskine
// YAZAMAZ. Özelliğin var olma sebebi tam da yazabilmek.
//
// Bu, env'de KARŞILIĞI OLMAYAN bir sorundu: env'de son mil yalnızca alanı
// doldurmaktı, burada bir de sahiplik var.

// chownCall, kaydedilmiş bir sahiplik değişikliğidir.
type chownCall struct {
	path string
	uid  int
}

// captureChown, gerçek chown yerine çağrıları kaydeder.
//
// ── Neden dikiş? ─────────────────────────────────────────────────────
//
// `os.Chown` Windows'ta desteklenmiyor ve bu paketin testleri CI
// matrisinde Windows'ta da KOŞUYOR (dosyanın başındaki nota bakın).
// Gerçek chown'a bağlı bir test yalnızca Linux'ta koşardı ve "derleniyor
// ama koşmuyor" boşluğu bu projede daha önce gerçek hata sakladı.
//
// Aynı dikiş `mountinfoPath` için de var ve gerekçesi aynı.
func captureChown(t *testing.T) *[]chownCall {
	t.Helper()
	calls := []chownCall{}
	old := chownDir
	chownDir = func(path string, uid int) error {
		calls = append(calls, chownCall{path: filepath.ToSlash(path), uid: uid})
		return nil
	}
	t.Cleanup(func() { chownDir = old })
	return &calls
}

// TestCreateMakesVolumeDirectory, dizinin GERÇEKTEN oluşturulduğunu
// doğrular.
//
// Docker'ın kendi otomatik oluşturmasına güvenmek, sahipliği Docker'ın
// seçmesi demekti — ve o seçim root:root.
func TestCreateMakesVolumeDirectory(t *testing.T) {
	captureChown(t)
	f := newFakeDocker(t)
	root := hardenedRoot(t, "rw,nosuid,nodev,relatime")
	c := f.client(root)

	spec := validSpec()
	spec.Mounts = []Mount{{VolumeName: "data", MountPath: "/data"}}

	if err := c.ContainerCreate(context.Background(), spec); err != nil {
		t.Fatalf("konteyner oluşturulamadı: %v", err)
	}

	dir := filepath.Join(root, "blog", "data")
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("hacim dizini oluşturulmadı (%s): %v — Docker onu "+
			"root:root olarak yaratır ve uygulama yazamaz", dir, err)
	}
	if !info.IsDir() {
		t.Errorf("%s dizin değil", dir)
	}
}

// TestCreateChownsVolumeToImageUser, sahipliğin İMAJDAN türetildiğini
// doğrular.
//
// Kullanıcıyı isteğe ekletmek de mümkündü ama yanlış olurdu: doğru uid
// zaten imajın içinde yazılı ve kullanıcıya sormak, yanlış yazma
// ihtimalini bedava getirirdi.
func TestCreateChownsVolumeToImageUser(t *testing.T) {
	calls := captureChown(t)
	f := newFakeDocker(t)
	f.imageUser = "101"
	root := hardenedRoot(t, "rw,nosuid,nodev,relatime")
	c := f.client(root)

	spec := validSpec()
	spec.Mounts = []Mount{{VolumeName: "data", MountPath: "/data"}}

	if err := c.ContainerCreate(context.Background(), spec); err != nil {
		t.Fatalf("konteyner oluşturulamadı: %v", err)
	}

	if len(*calls) != 1 {
		t.Fatalf("%d chown çağrısı, 1 bekleniyordu: %+v", len(*calls), *calls)
	}
	got := (*calls)[0]
	if got.uid != 101 {
		t.Errorf("uid %d, imaj 101 diyor — uygulama kendi diskine yazamaz",
			got.uid)
	}
	want := filepath.ToSlash(filepath.Join(root, "blog", "data"))
	if got.path != want {
		t.Errorf("chown yolu %q, %q bekleniyordu", got.path, want)
	}
}

// TestCreateTreatsEmptyImageUserAsRoot, USER yazmayan imajı doğrular.
//
// Canlıda ölçüldü: `web` uygulamasının imajı `Config.User=""` döndürüyor
// ve konteyner uid 0 ile koşuyor. Boş dizeyi hata saymak, bugün çalışan
// bir uygulamayı kırardı.
func TestCreateTreatsEmptyImageUserAsRoot(t *testing.T) {
	calls := captureChown(t)
	f := newFakeDocker(t)
	f.imageUser = ""
	root := hardenedRoot(t, "rw,nosuid,nodev,relatime")
	c := f.client(root)

	spec := validSpec()
	spec.Mounts = []Mount{{VolumeName: "data", MountPath: "/data"}}

	if err := c.ContainerCreate(context.Background(), spec); err != nil {
		t.Fatalf("USER'ı olmayan imaj reddedildi: %v", err)
	}
	if len(*calls) != 1 || (*calls)[0].uid != 0 {
		t.Errorf("boş USER için chown %+v, uid 0 bekleniyordu", *calls)
	}
}

// TestCreateAcceptsUserWithGroup, "uid:gid" biçimini doğrular.
//
// Dockerfile `USER 101:101` yazabiliyor. Tamamını sayıya çevirmeye
// çalışmak bu geçerli biçimi reddederdi.
func TestCreateAcceptsUserWithGroup(t *testing.T) {
	calls := captureChown(t)
	f := newFakeDocker(t)
	f.imageUser = "101:101"
	root := hardenedRoot(t, "rw,nosuid,nodev,relatime")
	c := f.client(root)

	spec := validSpec()
	spec.Mounts = []Mount{{VolumeName: "data", MountPath: "/data"}}

	if err := c.ContainerCreate(context.Background(), spec); err != nil {
		t.Fatalf("uid:gid biçimi reddedildi: %v", err)
	}
	if len(*calls) != 1 || (*calls)[0].uid != 101 {
		t.Errorf("chown %+v, uid 101 bekleniyordu", *calls)
	}
}

// TestCreateRejectsNonNumericImageUser, AD biçimindeki USER'ın
// REDDEDİLDİĞİNİ doğrular.
//
// `USER nginx` geçerli bir Dockerfile satırıdır ama adı uid'ye çevirmek
// imajın `/etc/passwd`'ını okumayı gerektirir — ayrıcalıklı yüzeye
// dosya sistemi ayrıştırıcısı eklemek demek.
//
// Yarım doğrulamak yerine AÇIKÇA REDDEDİLİYOR: sessizce root'a
// chown etmek, konteynerin yazamadığı bir hacmi "hazır" ilan ederdi ve
// hata dağıtımdan saatler sonra uygulamanın kendi günlüğünde görünürdü.
func TestCreateRejectsNonNumericImageUser(t *testing.T) {
	captureChown(t)
	f := newFakeDocker(t)
	f.imageUser = "nginx"
	root := hardenedRoot(t, "rw,nosuid,nodev,relatime")
	c := f.client(root)

	spec := validSpec()
	spec.Mounts = []Mount{{VolumeName: "data", MountPath: "/data"}}

	err := c.ContainerCreate(context.Background(), spec)
	if err == nil {
		t.Fatal("sayısal olmayan USER kabul edildi — hacmin sahibi yanlış " +
			"olur ve uygulama yazamaz")
	}
	if !strings.Contains(err.Error(), "nginx") {
		t.Errorf("hata hangi USER'dan bahsettiğini söylemiyor: %v", err)
	}
}

// TestCreateKeepsExistingVolumeContent, hazırlığın VERİYİ BOZMADIĞINI
// doğrular.
//
// Bu yol her dağıtımda, her geri almada ve her iyileştirmede yeniden
// koşuyor. Dizini yeniden oluşturan ya da temizleyen bir uygulama,
// kalıcı diski her dağıtımda silerdi — yani özelliği tam tersine
// çevirirdi.
func TestCreateKeepsExistingVolumeContent(t *testing.T) {
	captureChown(t)
	f := newFakeDocker(t)
	root := hardenedRoot(t, "rw,nosuid,nodev,relatime")
	c := f.client(root)

	dir := filepath.Join(root, "blog", "data")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	stamp := filepath.Join(dir, "veri.txt")
	if err := os.WriteFile(stamp, []byte("onemli"), 0o600); err != nil {
		t.Fatal(err)
	}

	spec := validSpec()
	spec.Mounts = []Mount{{VolumeName: "data", MountPath: "/data"}}
	if err := c.ContainerCreate(context.Background(), spec); err != nil {
		t.Fatalf("konteyner oluşturulamadı: %v", err)
	}

	got, err := os.ReadFile(stamp)
	if err != nil {
		t.Fatalf("var olan veri SİLİNDİ: %v", err)
	}
	if string(got) != "onemli" {
		t.Errorf("var olan veri değişti: %q", got)
	}
}

// TestCreateWithoutVolumesNeverTouchesDisk, hacimsiz konteynerin disk
// işlemi YAPMADIĞINI doğrular.
//
// KONTROL GRUBU: olmasaydı "her zaman mkdir+chown yap" diyen bir
// uygulama da yukarıdaki testleri geçerdi — ve hacim istemeyen her
// uygulama için gereksiz bir imaj sorgusu doğardı.
func TestCreateWithoutVolumesNeverTouchesDisk(t *testing.T) {
	calls := captureChown(t)
	f := newFakeDocker(t)
	root := hardenedRoot(t, "rw,nosuid,nodev,relatime")
	c := f.client(root)

	if err := c.ContainerCreate(context.Background(), validSpec()); err != nil {
		t.Fatalf("hacimsiz konteyner oluşturulamadı: %v", err)
	}
	if len(*calls) != 0 {
		t.Errorf("hacim yokken chown çağrıldı: %+v", *calls)
	}
	if _, err := os.Stat(filepath.Join(root, "blog")); err == nil {
		t.Error("hacim yokken uygulama dizini oluşturuldu")
	}
	for _, r := range f.requests {
		if strings.Contains(r.Path, "/images/") {
			t.Errorf("hacim yokken imaj sorgulandı (%s) — gereksiz çağrı", r.Path)
		}
	}
}
