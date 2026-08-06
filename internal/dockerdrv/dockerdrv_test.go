package dockerdrv

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Testler SAHTE bir Docker daemon'ına konuşur.
//
// Neden TCP üzerinden httptest: bu paketin testleri CI matrisinde
// Windows'ta da KOŞMALI. Unix soketine bağlı bir test yalnızca Linux'ta
// koşar ve "derleniyor ama koşmuyor" boşluğu bu projede daha önce gerçek
// hata sakladı (bkz. .github/workflows/ci.yml'deki arm64 notu).

type fakeDocker struct {
	*httptest.Server
	// requests, gelen her isteğin yolu ve gövdesi.
	requests []recorded
	// containers, /containers/json'ın döndüreceği kayıtlar.
	containers []listEntry
	// status, bir sonraki yanıtın HTTP kodu. 0 ise 200/201.
	status int
}

type recorded struct {
	Method string
	Path   string
	Query  string
	Body   []byte
}

func newFakeDocker(t *testing.T) *fakeDocker {
	t.Helper()
	f := &fakeDocker{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, 0)
		if r.Body != nil {
			buf := make([]byte, 1<<16)
			n, _ := r.Body.Read(buf)
			body = buf[:n]
		}
		f.requests = append(f.requests, recorded{
			Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery, Body: body,
		})

		if f.status != 0 {
			w.WriteHeader(f.status)
			_, _ = w.Write([]byte(`{"message":"sahte hata"}`))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/containers/json") {
			_ = json.NewEncoder(w).Encode(f.containers)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"Id":"sahte"}`))
	}))
	t.Cleanup(f.Close)
	return f
}

// client, sahte daemon'a konuşan bir istemci kurar.
func (f *fakeDocker) client(volumeRoot string) *Client {
	return &Client{http: f.Client(), base: f.URL, volumeRoot: volumeRoot}
}

// hardenedRoot, sertleştirme kontrolünü geçen sahte bir mountinfo kurar.
func hardenedRoot(t *testing.T, opts string) string {
	t.Helper()
	root := "/var/lib/panely/volumes"
	f := filepath.Join(t.TempDir(), "mountinfo")
	line := "415 406 8:1 /vol " + root + " " + opts + " - ext4 /dev/sda1 rw\n"
	if err := os.WriteFile(f, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	old := mountinfoPath
	mountinfoPath = f
	t.Cleanup(func() { mountinfoPath = old })
	return root
}

func validSpec() CreateSpec {
	return CreateSpec{
		AppID: "blog", ReleaseID: "r1", Replica: 0,
		CommitSHA:   "7fd1a60b01f91b314f59955a4e4d4e80d8edf11d",
		Env:         map[string]string{"PORT": "8080"},
		MemoryBytes: 256 << 20, CPUMillis: 500, BlkioWeight: 500,
		ContainerPort: 8080,
	}
}

// ── Pozitif kontrol ──────────────────────────────────────────────────

// TestCreateSendsExpectedBody, geçerli bir isteğin GEÇTİĞİNİ doğrular.
//
// Aşağıdaki reddetme testlerinin anlamlı olması buna bağlı: her şeyi
// reddeden bir sürücü de o testleri geçerdi.
func TestCreateSendsExpectedBody(t *testing.T) {
	f := newFakeDocker(t)
	c := f.client(hardenedRoot(t, "rw,nosuid,nodev,relatime"))

	if err := c.ContainerCreate(context.Background(), validSpec()); err != nil {
		t.Fatalf("geçerli istek reddedildi: %v", err)
	}
	if len(f.requests) != 1 {
		t.Fatalf("%d istek gitti, 1 bekleniyordu", len(f.requests))
	}

	var body map[string]any
	if err := json.Unmarshal(f.requests[0].Body, &body); err != nil {
		t.Fatalf("gövde çözümlenemedi: %v", err)
	}
	if got := body["Image"]; got != "panely/blog:7fd1a60b01f91b314f59955a4e4d4e80d8edf11d" {
		t.Errorf("Image %v", got)
	}
	if !strings.Contains(f.requests[0].Query, "panely_blog_r1_0") {
		t.Errorf("konteyner adı beklenen biçimde değil: %s", f.requests[0].Query)
	}
}

// ── Tehlikeli alanların YOKLUĞU ──────────────────────────────────────

// TestCreateBodyOmitsDangerousFields, tehlikeli seçeneklerin TELDE HİÇ
// GÖRÜNMEDİĞİNİ doğrular.
//
// "false gönderiliyor" yetmez: alan gövdede varsa ileride biri ona true
// atayabilir. Bu test, alanın TEMSİL EDİLEMEZ olduğunu — yani gövdede hiç
// bulunmadığını — zorlar. Şemadaki aynı kuralın sürücü katmanındaki
// karşılığı budur.
func TestCreateBodyOmitsDangerousFields(t *testing.T) {
	f := newFakeDocker(t)
	c := f.client(hardenedRoot(t, "rw,nosuid,nodev,relatime"))
	if err := c.ContainerCreate(context.Background(), validSpec()); err != nil {
		t.Fatal(err)
	}

	wire := string(f.requests[0].Body)
	for _, forbidden := range []string{
		"Privileged", "CapAdd", "Devices", "PidMode", "UsernsMode",
		"IpcMode", "CgroupParent", "Sysctls", "DeviceCgroupRules",
		"PortBindings", "PublishAllPorts",
	} {
		if strings.Contains(wire, forbidden) {
			t.Errorf("gövdede %q alanı var — temsil edilemez olmalıydı:\n%s", forbidden, wire)
		}
	}
}

// TestCreatePinsSecurityOpt, no-new-privileges'ın gönderildiğini ve
// seccomp'a DOKUNULMADIĞINI doğrular.
//
// seccomp'u açıkça yazmamak kasıtlı: Docker varsayılan profili kendisi
// uygular, ama SecurityOpt'a bir seccomp girdisi yazmak onu DEĞİŞTİRMEK
// demektir ve ileride yanlış bir profilin yazılmasına kapı açar.
func TestCreatePinsSecurityOpt(t *testing.T) {
	f := newFakeDocker(t)
	c := f.client(hardenedRoot(t, "rw,nosuid,nodev,relatime"))
	if err := c.ContainerCreate(context.Background(), validSpec()); err != nil {
		t.Fatal(err)
	}

	var body struct {
		HostConfig struct {
			SecurityOpt   []string `json:"SecurityOpt"`
			NetworkMode   string   `json:"NetworkMode"`
			RestartPolicy struct {
				Name string `json:"Name"`
			} `json:"RestartPolicy"`
		} `json:"HostConfig"`
	}
	if err := json.Unmarshal(f.requests[0].Body, &body); err != nil {
		t.Fatal(err)
	}

	hc := body.HostConfig
	if len(hc.SecurityOpt) != 1 || hc.SecurityOpt[0] != "no-new-privileges:true" {
		t.Errorf("SecurityOpt %v", hc.SecurityOpt)
	}
	if strings.Contains(strings.Join(hc.SecurityOpt, ","), "seccomp") {
		t.Error("SecurityOpt seccomp'a dokunuyor — varsayılan profil değiştirilmemeli")
	}
	if hc.NetworkMode != "panely-blog" {
		t.Errorf("NetworkMode %q — app_id'den türetilmeli", hc.NetworkMode)
	}
	if hc.RestartPolicy.Name != "no" {
		t.Errorf("RestartPolicy %q — yeniden başlatma sağlık denetçisinin işi", hc.RestartPolicy.Name)
	}
}

// ── Hacim sertleştirmesi ─────────────────────────────────────────────

// TestCreateRefusesWhenVolumeRootNotHardened, sertleştirme yoksa hacimli
// konteynerin OLUŞTURULMADIĞINI doğrular.
//
// Bu testin var olma sebebi ölçülmüş bir sessiz arızadır: mount birimi bir
// yeniden başlatmayı geçemedi ve hiçbir hata üretmeden hacimler
// korumasız açıldı (docs/decisions.md K-039).
func TestCreateRefusesWhenVolumeRootNotHardened(t *testing.T) {
	spec := validSpec()
	spec.Mounts = []Mount{{VolumeName: "data", MountPath: "/data"}}

	cases := []struct{ name, opts string }{
		{"iki bayrak da yok", "rw,relatime"},
		{"yalnızca nosuid var", "rw,nosuid,relatime"},
		{"yalnızca nodev var", "rw,nodev,relatime"},
		{"benzer isimli bayrak", "rw,nodevfoo,nosuidbar,relatime"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeDocker(t)
			c := f.client(hardenedRoot(t, tc.opts))
			if err := c.ContainerCreate(context.Background(), spec); err == nil {
				t.Fatal("sertleştirilmemiş hacim kökünde konteyner oluşturuldu")
			}
			if len(f.requests) != 0 {
				t.Errorf("reddedilmesine rağmen %d istek Docker'a gitti", len(f.requests))
			}
		})
	}
}

// TestCreateAllowsHardenedVolumeRoot, POZİTİF KONTROL: bayraklar varken
// hacimli konteyner oluşturulabilmeli.
func TestCreateAllowsHardenedVolumeRoot(t *testing.T) {
	spec := validSpec()
	spec.Mounts = []Mount{{VolumeName: "data", MountPath: "/data"}}

	f := newFakeDocker(t)
	c := f.client(hardenedRoot(t, "rw,nosuid,nodev,relatime"))
	if err := c.ContainerCreate(context.Background(), spec); err != nil {
		t.Fatalf("sertleştirilmiş kökte reddedildi: %v", err)
	}

	var body struct {
		HostConfig struct {
			Binds []string `json:"Binds"`
		} `json:"HostConfig"`
	}
	if err := json.Unmarshal(f.requests[0].Body, &body); err != nil {
		t.Fatal(err)
	}
	want := "/var/lib/panely/volumes/blog/data:/data"
	if len(body.HostConfig.Binds) != 1 || body.HostConfig.Binds[0] != want {
		t.Errorf("Binds %v, beklenen [%s]", body.HostConfig.Binds, want)
	}
}

// TestVolumeRootMustBeItsOwnMount, hacim kökünün ÜST mount'unun
// bayraklarına yaslanılmadığını doğrular.
//
// mountinfo'da yalnızca `/` varsa ve kontrol alt dizin eşleşmesi kabul
// etseydi, kök dosya sisteminin bayrakları hacim kökününmüş gibi okunur
// ve kontrol sistematik olarak yanlış cevap verirdi.
func TestVolumeRootMustBeItsOwnMount(t *testing.T) {
	f := filepath.Join(t.TempDir(), "mountinfo")
	// Yalnızca kök mount var, hacim kökü YOK.
	line := "1 0 8:1 / / rw,nosuid,nodev,relatime - ext4 /dev/sda1 rw\n"
	if err := os.WriteFile(f, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	old := mountinfoPath
	mountinfoPath = f
	t.Cleanup(func() { mountinfoPath = old })

	c := &Client{volumeRoot: "/var/lib/panely/volumes"}
	err := c.checkVolumeRootHardened()
	if err == nil {
		t.Fatal("hacim kökü ayrı mount olmadığı hâlde kontrol geçti")
	}
	if !strings.Contains(err.Error(), "ayrı bir mount") {
		t.Errorf("hata sebebi belirsiz: %v", err)
	}
}

// TestLastMountWins, aynı yola yığılmış mount'larda SONUNCUSUNUN etkin
// sayıldığını doğrular.
//
// Sertleştirilmiş bir mount'un üzerine sertleştirilmemiş biri bağlanırsa
// etkin olan ikincisidir; ilkini okumak korumayı olduğundan iyi gösterir.
func TestLastMountWins(t *testing.T) {
	f := filepath.Join(t.TempDir(), "mountinfo")
	root := "/var/lib/panely/volumes"
	data := "415 406 8:1 /a " + root + " rw,nosuid,nodev,relatime - ext4 /dev/sda1 rw\n" +
		"416 406 8:1 /b " + root + " rw,relatime - ext4 /dev/sda1 rw\n"
	if err := os.WriteFile(f, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	old := mountinfoPath
	mountinfoPath = f
	t.Cleanup(func() { mountinfoPath = old })

	c := &Client{volumeRoot: root}
	if err := c.checkVolumeRootHardened(); err == nil {
		t.Fatal("üste bağlanan sertleştirilmemiş mount fark edilmedi")
	}
}

// ── Etiket disiplini ─────────────────────────────────────────────────

// TestListIgnoresContainersWithoutPanelyLabels, daemon'ın döndürdüğü ama
// Panely etiketi TAŞIMAYAN kayıtların elendiğini doğrular.
//
// Filtreyi daemon uyguluyor. Bir güvenlik özelliğini uzak tarafın doğru
// davranmasına bağlamak ona güvenmek demektir; oysa bu sürücünün var olma
// sebebi güvenmemek. Burada daemon KASTEN yabancı kayıt döndürüyor.
func TestListIgnoresContainersWithoutPanelyLabels(t *testing.T) {
	f := newFakeDocker(t)
	f.containers = []listEntry{
		{ID: "yabanci", State: "running", Labels: map[string]string{"com.example": "db"}},
		{ID: "etiketsiz", State: "running"},
		{ID: "eksik", State: "running", Labels: map[string]string{"panely.app_id": "blog"}},
		{ID: "bizim", State: "running", Labels: map[string]string{
			"panely.app_id": "blog", "panely.release_id": "r1", "panely.replica": "0",
		}},
	}
	c := f.client("/var/lib/panely/volumes")

	got, err := c.ContainerList(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "bizim" {
		t.Fatalf("%d konteyner döndü (%v) — yalnızca tam etiketli olan geçmeliydi", len(got), got)
	}
}

// TestSelectorNeverTouchesForeignContainers, seçicinin yabancı bir
// konteynere ASLA ulaşmadığını doğrular.
//
// Yıkıcı uçların (stop/remove) kimliği yalnızca KENDİ listemizden alması
// bu yüzden önemli: dışarıdan kimlik alınsaydı ele geçirilmiş bir panelyd
// veritabanı konteynerini silebilirdi.
func TestSelectorNeverTouchesForeignContainers(t *testing.T) {
	f := newFakeDocker(t)
	f.containers = []listEntry{
		{ID: "veritabani", State: "running", Labels: map[string]string{"com.example": "postgres"}},
	}
	c := f.client("/var/lib/panely/volumes")

	n, err := c.ContainerRemove(context.Background(), Selector{AppID: "blog", ReleaseID: "r1"})
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("%d konteyner silindi, 0 bekleniyordu", n)
	}
	for _, req := range f.requests {
		if req.Method == http.MethodDelete {
			t.Errorf("yabancı konteynere DELETE gitti: %s", req.Path)
		}
	}
}

// TestSelectorNarrowsToReplica, replika verildiğinde yalnızca onun
// seçildiğini doğrular.
func TestSelectorNarrowsToReplica(t *testing.T) {
	f := newFakeDocker(t)
	f.containers = []listEntry{
		{ID: "c0", State: "running", Labels: map[string]string{
			"panely.app_id": "blog", "panely.release_id": "r1", "panely.replica": "0"}},
		{ID: "c1", State: "running", Labels: map[string]string{
			"panely.app_id": "blog", "panely.release_id": "r1", "panely.replica": "1"}},
	}
	c := f.client("/var/lib/panely/volumes")

	all, err := c.ContainerStart(context.Background(), Selector{AppID: "blog", ReleaseID: "r1"})
	if err != nil || all != 2 {
		t.Fatalf("sürüm düzeyi seçim: %d konteyner, hata %v — 2 bekleniyordu", all, err)
	}

	one := uint32(1)
	f.requests = nil
	n, err := c.ContainerStart(context.Background(), Selector{AppID: "blog", ReleaseID: "r1", Replica: &one})
	if err != nil || n != 1 {
		t.Fatalf("replika düzeyi seçim: %d konteyner, hata %v — 1 bekleniyordu", n, err)
	}
	for _, req := range f.requests {
		if strings.Contains(req.Path, "/c0/") {
			t.Error("replika 1 istendi ama replika 0'a dokunuldu")
		}
	}
}

// TestRemoveKeepsVolumes, silmenin hacimlere DOKUNMADIĞINI doğrular.
//
// Konteyner silmek geri alınabilir, hacim silmek değildir ve §1.3 gereği
// TOTP kapısına tabidir.
func TestRemoveKeepsVolumes(t *testing.T) {
	f := newFakeDocker(t)
	f.containers = []listEntry{
		{ID: "c0", State: "exited", Labels: map[string]string{
			"panely.app_id": "blog", "panely.release_id": "r1", "panely.replica": "0"}},
	}
	c := f.client("/var/lib/panely/volumes")

	if _, err := c.ContainerRemove(context.Background(), Selector{AppID: "blog", ReleaseID: "r1"}); err != nil {
		t.Fatal(err)
	}
	var seen bool
	for _, req := range f.requests {
		if req.Method != http.MethodDelete {
			continue
		}
		seen = true
		if strings.Contains(req.Query, "v=true") {
			t.Errorf("silme hacimleri de kaldırıyor: %s", req.Query)
		}
	}
	if !seen {
		t.Fatal("hiç DELETE gitmedi — test bir şey ölçmüyor")
	}
}

// ── Ağ ───────────────────────────────────────────────────────────────

// TestNetworkEnsureTreatsConflictAsSuccess, var olan ağın hata
// sayılmadığını doğrular ("ensure" semantiği).
func TestNetworkEnsureTreatsConflictAsSuccess(t *testing.T) {
	f := newFakeDocker(t)
	f.status = http.StatusConflict
	c := f.client("/var/lib/panely/volumes")

	name, err := c.NetworkEnsure(context.Background(), "blog")
	if err != nil {
		t.Fatalf("409 hata sayıldı: %v", err)
	}
	if name != "panely-blog" {
		t.Errorf("ağ adı %q", name)
	}
}

// TestNetworkEnsurePropagatesRealErrors, 409 DIŞINDAKİ hataların
// yutulmadığını doğrular.
//
// Pozitif kontrolün tersi: yukarıdaki test 409'u başarı sayıyor, bu test
// o gevşemenin diğer hataları da kapsamadığını gösteriyor.
func TestNetworkEnsurePropagatesRealErrors(t *testing.T) {
	f := newFakeDocker(t)
	f.status = http.StatusInternalServerError
	c := f.client("/var/lib/panely/volumes")

	if _, err := c.NetworkEnsure(context.Background(), "blog"); err == nil {
		t.Fatal("500 yutuldu")
	}
}

// TestNetworkNameIsDerived, ağ adının app_id'den türetildiğini doğrular.
func TestNetworkNameIsDerived(t *testing.T) {
	if got := NetworkName("blog"); got != "panely-blog" {
		t.Errorf("NetworkName = %q", got)
	}
	if got := ImageTag("blog", "abc123"); got != "panely/blog:abc123" {
		t.Errorf("ImageTag = %q", got)
	}
}

// TestContainerNamesCannotCollide, iki farklı adres üçlüsünün aynı ada
// çözülemediğini doğrular.
//
// app_id ve release_id tire içerebildiği için tireyle ayırmak
// ("panely-a-b-c") belirsizlik yaratırdı: (app "a-b", release "c") ile
// (app "a", release "b-c") aynı ada düşerdi. Belirsiz adresleme, bir
// sürümü durdururken başkasını durdurmak demektir.
func TestContainerNamesCannotCollide(t *testing.T) {
	a := containerName("a-b", "c", 0)
	b := containerName("a", "b-c", 0)
	if a == b {
		t.Errorf("iki farklı üçlü aynı ada çözüldü: %q", a)
	}
}
