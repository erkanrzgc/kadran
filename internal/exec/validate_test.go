package exec

import (
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	panelyv1 "github.com/erkanrzgc/panely/internal/pb/panely/v1"
)

// Bu dosya kaçış DENEMELERİNİ sınar. "Geçerli girdi geçiyor mu" sorusu
// ikincildir; asıl soru "şemanın izin verdiği en kötü girdi ne yapabilir".

func validCreateRequest() *panelyv1.ContainerCreateRequest {
	return &panelyv1.ContainerCreateRequest{
		Ref: &panelyv1.ContainerRef{
			Release: &panelyv1.ReleaseRef{AppId: "blog", ReleaseId: "01hx9k2m"},
			Replica: 0,
		},
		CommitSha: "a1b2c3d4e5f6",
		Env:       map[string]string{"PORT": "3000"},
		Limits: &panelyv1.ResourceLimits{
			MemoryBytes: 256 << 20,
			CpuMillis:   500,
			BlkioWeight: 500,
		},
		Volumes: []*panelyv1.VolumeMount{
			{VolumeName: "data", MountPath: "/var/lib/app", ReadOnly: false},
		},
		ContainerPort: 3000,
	}
}

// Kontrol: kurgunun kendisi geçerli olmalı. Bu olmadan aşağıdaki her
// negatif test BOŞ YERE geçebilirdi — kurgu zaten geçersizse hangi alanın
// reddi ölçtüğümüz belirsizleşir.
func TestValidCreateRequestIsAccepted(t *testing.T) {
	if err := validateCreate(validCreateRequest()); err != nil {
		t.Fatalf("geçerli istek reddedildi: %v", err)
	}
}

func TestMountPathRejectsEscapeAttempts(t *testing.T) {
	cases := map[string]string{
		"göreli yol":         "var/lib/app",
		"üst dizin":          "/var/lib/../../etc",
		"gizli üst dizin":    "/var/lib/app/..",
		"kök dizin":          "/",
		"çift eğik çizgi":    "//var/lib/app",
		"nokta bileşeni":     "/var/./lib",
		"sondaki eğik çizgi": "/var/lib/app/",
		"procfs":             "/proc",
		"procfs altı":        "/proc/self/environ",
		"sysfs altı":         "/sys/fs/cgroup",
		"devtmpfs":           "/dev",
		"devtmpfs altı":      "/dev/shm",
		"boş":                "",
		"tek nokta":          ".",
		"Windows tarzı":      `C:\data`,
	}
	if len(cases) == 0 {
		t.Fatal("test bir şey ölçmüyor")
	}
	for ad, yol := range cases {
		t.Run(ad, func(t *testing.T) {
			if err := validateMountPath(yol); err == nil {
				t.Errorf("kabul edildi ama reddedilmeliydi: %q", yol)
			}
		})
	}
}

func TestMountPathAcceptsCleanAbsolutePaths(t *testing.T) {
	// Reddin anlamlı olması için kabulün de ölçülmesi gerekir: her şeyi
	// reddeden bir doğrulayıcı yukarıdaki testi de geçerdi.
	for _, yol := range []string{"/data", "/var/lib/app", "/srv/uploads/img"} {
		if err := validateMountPath(yol); err != nil {
			t.Errorf("geçerli yol reddedildi %q: %v", yol, err)
		}
	}
}

func TestOverlappingMountsRejected(t *testing.T) {
	cases := [][2]string{
		{"/data", "/data"},       // birebir aynı
		{"/data", "/data/inner"}, // iç içe
		{"/data/inner", "/data"}, // ters sırada iç içe
	}
	for _, c := range cases {
		req := validCreateRequest()
		req.Volumes = []*panelyv1.VolumeMount{
			{VolumeName: "bir", MountPath: c[0]},
			{VolumeName: "iki", MountPath: c[1]},
		}
		if err := validateCreate(req); err == nil {
			t.Errorf("çakışan bağlama kabul edildi: %q + %q", c[0], c[1])
		}
	}

	// Komşu ama iç içe OLMAYAN yollar geçmeli. Bu kontrol olmadan
	// pathOverlaps saf `strings.HasPrefix` ile yazılabilir ve "/data"
	// ile "/database" yanlışlıkla çakışık sayılırdı.
	req := validCreateRequest()
	req.Volumes = []*panelyv1.VolumeMount{
		{VolumeName: "bir", MountPath: "/data"},
		{VolumeName: "iki", MountPath: "/database"},
	}
	if err := validateCreate(req); err != nil {
		t.Errorf("komşu yollar reddedildi: %v", err)
	}
}

func TestAppIDRejectsPathAndTagInjection(t *testing.T) {
	bad := []string{
		"", "../evil", "a/b", "a..b", "Blog", "1blog", "-blog",
		"blog:latest", "blog@sha256", "blog space",
		strings.Repeat("a", maxAppIDLen+1),
	}
	for _, id := range bad {
		if err := validateAppID(id); err == nil {
			t.Errorf("app_id kabul edildi ama reddedilmeliydi: %q", id)
		}
	}
	for _, id := range []string{"blog", "my-app", "a", strings.Repeat("a", maxAppIDLen)} {
		if err := validateAppID(id); err != nil {
			t.Errorf("geçerli app_id reddedildi %q: %v", id, err)
		}
	}
}

func TestCommitSHARejectsTagInjection(t *testing.T) {
	// commit_sha `panely/<app>:<sha>` etiketine giriyor. Hex dışı bir
	// karakter, etiketi başka bir imaja ya da registry'ye kaydırabilirdi.
	bad := []string{
		"", "abc", "ABCDEF1", "a1b2c3d4e5f!", "a1b2c3d:latest",
		"a1b2c3d/evil", "a1b2c3d@sha256:x", strings.Repeat("a", 65),
	}
	for _, sha := range bad {
		req := validCreateRequest()
		req.CommitSha = sha
		if err := validateCreate(req); err == nil {
			t.Errorf("commit_sha kabul edildi ama reddedilmeliydi: %q", sha)
		}
	}
}

func TestLimitsMustBeExplicitAndBounded(t *testing.T) {
	cases := map[string]*panelyv1.ResourceLimits{
		"nil":           nil,
		"tamamen sıfır": {},
		"bellek sıfır":  {MemoryBytes: 0, CpuMillis: 500, BlkioWeight: 500},
		"bellek çok az": {MemoryBytes: 1024, CpuMillis: 500, BlkioWeight: 500},
		"cpu sıfır":     {MemoryBytes: 256 << 20, CpuMillis: 0, BlkioWeight: 500},
		"cpu aşırı":     {MemoryBytes: 256 << 20, CpuMillis: maxCPUMillis + 1, BlkioWeight: 500},
		"blkio sıfır":   {MemoryBytes: 256 << 20, CpuMillis: 500, BlkioWeight: 0},
		"blkio aşırı":   {MemoryBytes: 256 << 20, CpuMillis: 500, BlkioWeight: 1001},
	}
	for ad, l := range cases {
		t.Run(ad, func(t *testing.T) {
			if err := validateLimits(l); err == nil {
				t.Error("limitsiz/geçersiz konteyner kabul edildi")
			}
		})
	}
}

func TestEnvRejectsMalformedKeysAndNUL(t *testing.T) {
	cases := map[string]map[string]string{
		"boş anahtar":    {"": "x"},
		"rakamla başlar": {"1PORT": "x"},
		"tire içerir":    {"MY-VAR": "x"},
		"eşittir içerir": {"A=B": "x"},
		"boşluk içerir":  {"MY VAR": "x"},
		"değerde NUL":    {"PORT": "30\x0000"},
		"anahtarda NUL":  {"PO\x00RT": "x"},
	}
	for ad, env := range cases {
		t.Run(ad, func(t *testing.T) {
			if err := validateEnv(env); err == nil {
				t.Error("bozuk env kabul edildi")
			}
		})
	}

	tooMany := make(map[string]string, maxEnvEntries+1)
	for i := 0; i <= maxEnvEntries; i++ {
		tooMany["K"+strings.Repeat("x", i%40)+string(rune('a'+i%26))] = "v"
	}
	if len(tooMany) > maxEnvEntries {
		if err := validateEnv(tooMany); err == nil {
			t.Error("sınırsız env kabul edildi")
		}
	}

	big := map[string]string{"BIG": strings.Repeat("x", maxEnvBytes+1)}
	if err := validateEnv(big); err == nil {
		t.Error("aşırı büyük env kabul edildi")
	}
}

func TestSelectorAllowsAbsentReplicaButBoundsPresentOne(t *testing.T) {
	rel := &panelyv1.ReleaseRef{AppId: "blog", ReleaseId: "01hx9k2m"}

	// Replika VERİLMEMİŞ: sürümün tamamı seçilir — geçerli olmalı.
	if err := validateSelector(&panelyv1.ContainerSelector{Release: rel}); err != nil {
		t.Fatalf("replikasız seçici reddedildi: %v", err)
	}

	// Verilmişse sınır içinde olmalı.
	asiri := uint32(maxReplica)
	if err := validateSelector(&panelyv1.ContainerSelector{
		Release: rel, Replica: &asiri,
	}); err == nil {
		t.Error("sınır dışı replika kabul edildi")
	}

	sifir := uint32(0)
	if err := validateSelector(&panelyv1.ContainerSelector{
		Release: rel, Replica: &sifir,
	}); err != nil {
		t.Errorf("geçerli replika reddedildi: %v", err)
	}
}

func TestContainerPortMustBeInRange(t *testing.T) {
	for _, port := range []uint32{0, 65536, 1 << 20} {
		req := validCreateRequest()
		req.ContainerPort = port
		if err := validateCreate(req); err == nil {
			t.Errorf("geçersiz port kabul edildi: %d", port)
		}
	}
}

// ── Doğrulama gerçekten UÇ NOKTADA koşuyor mu ────────────────────────
//
// Yukarıdaki testlerin hepsi doğrulayıcıları DOĞRUDAN çağırıyor. Hiçbiri,
// handler'ın onları çağırdığını kanıtlamaz — çağırmayan bir handler bu
// dosyayı olduğu gibi geçirirdi. Aşağıdaki test tam olarak o boşluğu
// kapatıyor: kötü istek InvalidArgument, iyi istek Unimplemented almalı.
// İki kodun FARKLI olması, doğrulamanın sürücüden önce koştuğunu gösterir.

func TestHandlersValidateBeforeAnythingElse(t *testing.T) {
	srv := &Server{}

	kotuRef := &panelyv1.ContainerRef{
		Release: &panelyv1.ReleaseRef{AppId: "../evil", ReleaseId: "x"},
	}
	iyiSel := &panelyv1.ContainerSelector{
		Release: &panelyv1.ReleaseRef{AppId: "blog", ReleaseId: "01hx9k2m"},
	}
	kotuSel := &panelyv1.ContainerSelector{
		Release: &panelyv1.ReleaseRef{AppId: "../evil", ReleaseId: "x"},
	}

	kotuCreate := validCreateRequest()
	kotuCreate.Volumes[0].MountPath = "/proc/self"

	cases := []struct {
		ad   string
		kotu func() error
		iyi  func() error
	}{
		{
			"NetworkEnsure",
			func() error {
				_, err := srv.NetworkEnsure(t.Context(), &panelyv1.NetworkEnsureRequest{AppId: "../evil"})
				return err
			},
			func() error {
				_, err := srv.NetworkEnsure(t.Context(), &panelyv1.NetworkEnsureRequest{AppId: "blog"})
				return err
			},
		},
		{
			"ContainerCreate",
			func() error {
				_, err := srv.ContainerCreate(t.Context(), kotuCreate)
				return err
			},
			func() error {
				_, err := srv.ContainerCreate(t.Context(), validCreateRequest())
				return err
			},
		},
		{
			"ContainerStart",
			func() error {
				_, err := srv.ContainerStart(t.Context(), &panelyv1.ContainerStartRequest{Selector: kotuSel})
				return err
			},
			func() error {
				_, err := srv.ContainerStart(t.Context(), &panelyv1.ContainerStartRequest{Selector: iyiSel})
				return err
			},
		},
		{
			"ContainerStop",
			func() error {
				_, err := srv.ContainerStop(t.Context(), &panelyv1.ContainerStopRequest{
					Selector: iyiSel, TimeoutSeconds: maxStopTimeoutSeconds + 1,
				})
				return err
			},
			func() error {
				_, err := srv.ContainerStop(t.Context(), &panelyv1.ContainerStopRequest{
					Selector: iyiSel, TimeoutSeconds: 10,
				})
				return err
			},
		},
		{
			"ContainerRemove",
			func() error {
				_, err := srv.ContainerRemove(t.Context(), &panelyv1.ContainerRemoveRequest{Selector: kotuSel})
				return err
			},
			func() error {
				_, err := srv.ContainerRemove(t.Context(), &panelyv1.ContainerRemoveRequest{Selector: iyiSel})
				return err
			},
		},
		{
			"ContainerList",
			func() error {
				_, err := srv.ContainerList(t.Context(), &panelyv1.ContainerListRequest{AppId: "../evil"})
				return err
			},
			func() error {
				// Boş app_id KASITLI olarak geçerlidir: öksüz konteyner
				// taraması bunu gerektiriyor.
				_, err := srv.ContainerList(t.Context(), &panelyv1.ContainerListRequest{})
				return err
			},
		},
		{
			"ContainerLogs",
			func() error {
				return srv.ContainerLogs(&panelyv1.ContainerLogsRequest{Ref: kotuRef}, nil)
			},
			func() error {
				return srv.ContainerLogs(&panelyv1.ContainerLogsRequest{
					Ref: &panelyv1.ContainerRef{Release: iyiSel.GetRelease()},
				}, nil)
			},
		},
	}

	if len(cases) == 0 {
		t.Fatal("test bir şey ölçmüyor")
	}

	for _, c := range cases {
		t.Run(c.ad, func(t *testing.T) {
			if got := status.Code(c.kotu()); got != codes.InvalidArgument {
				t.Errorf("kötü istek %s aldı, InvalidArgument bekleniyordu — "+
					"handler doğrulamayı atlıyor olabilir", got)
			}
			if got := status.Code(c.iyi()); got != codes.Unimplemented {
				t.Errorf("iyi istek %s aldı, Unimplemented bekleniyordu", got)
			}
		})
	}
}
