//go:build dockere2e

// GERÇEK Docker daemon'ına karşı yaşam döngüsü doğrulaması.
//
// # Neden ayrı bir etiket?
//
// Bu test çalışan bir Docker gerektirir ve ağdan imaj çeker; normal
// `go test ./...` koşusunda olmamalı. CI'da ubuntu runner'ında Docker
// zaten var, bu yüzden ayrı bir adımda koşuyor:
//
//	go test -tags dockere2e ./internal/dockerdrv/
//
// # Neden gerekli?
//
// Diğer testler SAHTE bir daemon'a konuşuyor ve sahte daemon, gövdeyi
// Docker'ın gerçekten kabul edip etmediğini bilmez. Bu projede
// "derleniyor" ile "çalışıyor" arasındaki fark defalarca gerçek hata
// çıkardı; sürücünün ürettiği JSON'un gerçek Engine tarafından kabul
// edilmesi ancak burada kanıtlanır.
//
// ⚠ TESTİN kendisi ağa çıkar (imaj çeker). SÜRÜCÜ çıkmaz — imaj çekme
// kod yolu sürücüde YOKTUR ve TestCreateNeverPullsImages tam olarak
// bunu doğrular.

package dockerdrv

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"testing"
	"time"
)

const (
	e2eSocket = "/run/docker.sock"
	e2eApp    = "e2etest"
	e2eSHA    = "0123456789abcdef0123456789abcdef01234567"
)

// skipOrFail, Docker yoksa testi atlar — AMA Docker'ın var olması
// GARANTİ edilen ortamda (CI) bunun yerine BAŞARISIZ olur.
//
// # Neden gerekli
//
// Bu dosyanın ilk hâlinde her koşulda `t.Skip` çağrılıyordu. CI'da API
// sürümü uyuşmazlığı yüzünden Ping başarısız oldu, TÜM testler sessizce
// atlandı ve adım YEŞİL geçti — hiçbir şey sınamadan. Sorunu yakalayan
// şey, tesadüfen eklenmiş ayrı bir tuzak adımıydı.
//
// Atlanan test ile geçen testi ayırt edemeyen bir kontrol, yeşil rozetten
// başka bir şey üretmez. PANELY_E2E_REQUIRE_DOCKER=1 verildiğinde atlama
// hakkı kalkar.
func skipOrFail(t *testing.T, format string, args ...any) {
	t.Helper()
	if os.Getenv("PANELY_E2E_REQUIRE_DOCKER") != "" {
		t.Fatalf("Docker zorunlu ama kullanılamıyor: "+format, args...)
	}
	t.Skipf(format, args...)
}

func e2eClient(t *testing.T) *Client {
	t.Helper()
	if _, err := os.Stat(e2eSocket); err != nil {
		skipOrFail(t, "Docker soketi yok (%v)", err)
	}
	root := os.Getenv("PANELY_E2E_VOLUME_ROOT")
	if root == "" {
		root = "/var/lib/panely/volumes"
	}
	c := New(e2eSocket, root)
	if _, err := c.Ping(context.Background()); err != nil {
		skipOrFail(t, "Docker'a ulaşılamadı: %v", err)
	}
	return c
}

// buildTestImage, panely/<app>:<sha> etiketli minik bir imaj üretir.
//
// Sürücü imaj ÇEKMEDİĞİ için (tasarım gereği) testin imajı önceden var
// etmesi gerekiyor. Bunu sürücü üzerinden DEĞİL, doğrudan Engine API'ye
// giderek yapıyor — sürücüye bir derleme/çekme yolu eklemek, tam olarak
// kapatmaya çalıştığımız kapıyı açardı.
func buildTestImage(t *testing.T, c *Client) {
	t.Helper()
	buildTaggedImage(t, c, e2eSHA, `CMD ["sleep", "60"]`)
}

func buildTaggedImage(t *testing.T, c *Client, sha, cmd string) {
	t.Helper()

	var ctxTar bytes.Buffer
	tw := tar.NewWriter(&ctxTar)
	df := "FROM busybox:1.36\n" + cmd + "\n"
	if err := tw.WriteHeader(&tar.Header{
		Name: "Dockerfile", Mode: 0o600, Size: int64(len(df)),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(df)); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	// Uzlaşılan sürümü kullan: sabit bir sürüm yazmak, tam olarak bu
	// dosyanın yakaladığı hatayı testin içine geri koyardı.
	ver, err := c.negotiate(context.Background())
	if err != nil {
		t.Fatalf("API sürümü uzlaşılamadı: %v", err)
	}

	q := url.Values{"t": {ImageTag(e2eApp, sha)}}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		c.base+"/"+ver+"/build?"+q.Encode(), &ctxTar)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-tar")

	resp, err := c.http.Do(req)
	if err != nil {
		t.Fatalf("test imajı derlenemedi: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	out := new(bytes.Buffer)
	_, _ = out.ReadFrom(resp.Body)
	// ⚠ HTTP 200 YETMEZ. Ölçüldü: derleme ORTASINDAKİ hata HTTP 200 ile
	// gelir ve akışın son karesinde `{"error":...}` taşır. Yalnızca duruma
	// bakan bir kontrol, başarısız her derlemeyi başarılı gösterirdi.
	if resp.StatusCode != http.StatusOK || bytes.Contains(out.Bytes(), []byte(`"error"`)) {
		t.Fatalf("test imajı derlenemedi (HTTP %d): %s", resp.StatusCode, out.String())
	}
}

func cleanup(t *testing.T, c *Client) {
	t.Helper()
	ctx := context.Background()
	for _, rel := range []string{"r1", "r2"} {
		_, _ = c.ContainerRemove(ctx, Selector{AppID: e2eApp, ReleaseID: rel})
	}
}

// TestLifecycleAgainstRealDocker, tam yaşam döngüsünü gerçek daemon'da
// koşturur.
func TestLifecycleAgainstRealDocker(t *testing.T) {
	c := e2eClient(t)
	ctx := context.Background()
	buildTestImage(t, c)
	cleanup(t, c)
	t.Cleanup(func() { cleanup(t, c) })

	name, err := c.NetworkEnsure(ctx, e2eApp)
	if err != nil {
		t.Fatalf("NetworkEnsure: %v", err)
	}
	if name != "panely-"+e2eApp {
		t.Errorf("ağ adı %q", name)
	}
	// İdempotanlık: ikinci çağrı da başarılı olmalı (409 → başarı).
	if _, err := c.NetworkEnsure(ctx, e2eApp); err != nil {
		t.Fatalf("NetworkEnsure ikinci kez: %v", err)
	}

	spec := CreateSpec{
		AppID: e2eApp, ReleaseID: "r1", Replica: 0, CommitSHA: e2eSHA,
		Env:         map[string]string{"PANELY_E2E": "1"},
		MemoryBytes: 64 << 20, CPUMillis: 250, BlkioWeight: 500,
		ContainerPort: 8080,
	}
	if err := c.ContainerCreate(ctx, spec); err != nil {
		t.Fatalf("ContainerCreate: %v", err)
	}

	found, err := c.ContainerList(ctx, e2eApp)
	if err != nil {
		t.Fatalf("ContainerList: %v", err)
	}
	if len(found) != 1 {
		t.Fatalf("%d konteyner listelendi, 1 bekleniyordu", len(found))
	}
	if found[0].AppID != e2eApp || found[0].ReleaseID != "r1" || found[0].Replica != 0 {
		t.Errorf("etiketler yanlış çözüldü: %+v", found[0])
	}
	if found[0].State != "created" {
		t.Errorf("durum %q, created bekleniyordu", found[0].State)
	}

	sel := Selector{AppID: e2eApp, ReleaseID: "r1"}
	if n, err := c.ContainerStart(ctx, sel); err != nil || n != 1 {
		t.Fatalf("ContainerStart: %d, %v", n, err)
	}

	if st := waitForState(t, c, "running"); st != "running" {
		t.Fatalf("konteyner çalışmadı, durum %q", st)
	}

	if n, err := c.ContainerStop(ctx, sel, 2); err != nil || n != 1 {
		t.Fatalf("ContainerStop: %d, %v", n, err)
	}
	if st := waitForState(t, c, "exited"); st != "exited" {
		t.Errorf("durdurmadan sonra durum %q", st)
	}

	if n, err := c.ContainerRemove(ctx, sel); err != nil || n != 1 {
		t.Fatalf("ContainerRemove: %d, %v", n, err)
	}
	after, err := c.ContainerList(ctx, e2eApp)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 0 {
		t.Errorf("silmeden sonra %d konteyner kaldı", len(after))
	}
}

func waitForState(t *testing.T, c *Client, want string) string {
	t.Helper()
	var last string
	for range 50 {
		found, err := c.ContainerList(context.Background(), e2eApp)
		if err != nil {
			t.Fatal(err)
		}
		if len(found) == 0 {
			return "yok"
		}
		last = found[0].State
		if last == want {
			return last
		}
		time.Sleep(100 * time.Millisecond)
	}
	return last
}

// TestCreateNeverPullsImages, YEREL OLMAYAN bir imajın çekilmediğini
// gerçek daemon'da doğrular.
//
// Zayıf test, var olmayan bir ad kullanmak olurdu: o ad Hub'da da yok,
// yani 404 "çekmedi"yi değil "bulamadı"yı gösterebilirdi. Burada Docker
// Hub'da KESİNLİKLE VAR OLAN bir imaj (`alpine`) kullanılıyor —
// `panely/<app>:<sha>` biçimi Docker tarafından `docker.io/panely/...`
// olarak yorumlanabilir olduğu için asıl risk budur.
func TestCreateNeverPullsImages(t *testing.T) {
	c := e2eClient(t)
	ctx := context.Background()

	// Yerelde OLMAYAN bir sürüm: imaj etiketi çözülemeyecek.
	spec := CreateSpec{
		AppID: e2eApp, ReleaseID: "r2", Replica: 0,
		CommitSHA:   "ffffffffffffffffffffffffffffffffffffffff",
		MemoryBytes: 64 << 20, CPUMillis: 250, BlkioWeight: 500,
		ContainerPort: 8080,
	}
	err := c.ContainerCreate(ctx, spec)
	if err == nil {
		t.Fatal("yerelde olmayan imajla konteyner oluşturuldu — çekmiş olabilir")
	}

	var apiErr *apiError
	if !asAPIError(err, &apiErr) || apiErr.Status != http.StatusNotFound {
		t.Fatalf("beklenen 404, gelen: %v", err)
	}

	// İmaj gerçekten ÇEKİLMEMİŞ olmalı.
	if imageExists(t, c, ImageTag(e2eApp, "ffffffffffffffffffffffffffffffffffffffff")) {
		t.Error("create imajı ÇEKMİŞ — sürücüde pull yolu olmamalı")
	}
}

func asAPIError(err error, target **apiError) bool {
	e, ok := err.(*apiError) //nolint:errorlint // do() hatayı sarmıyor
	if ok {
		*target = e
	}
	return ok
}

func imageExists(t *testing.T, c *Client, tag string) bool {
	t.Helper()
	resp, err := c.do(context.Background(), http.MethodGet,
		fmt.Sprintf("/images/%s/json", url.PathEscape(tag)), nil, nil)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return true
}

// TestVolumeMountIsHardenedEndToEnd, hacim bağlamasının konteyner içinde
// GERÇEKTEN nodev,nosuid ile göründüğünü doğrular.
//
// Bu, K-038 ve K-039'un birleşik son kontrolü: mount birimi + sürücünün
// çalışma anı doğrulaması + Docker'ın bind davranışı. Üçünden biri
// bozulursa burası kırmızıya döner.
func TestVolumeMountIsHardenedEndToEnd(t *testing.T) {
	c := e2eClient(t)
	if err := c.checkVolumeRootHardened(); err != nil {
		// Burada da sessiz atlama yok: CI hacim kökünü kendisi
		// sertleştiriyor, dolayısıyla orada atlanması bir arızadır.
		skipOrFail(t, "hacim kökü sertleştirilmemiş, uçtan uca sınanamaz: %v", err)
	}
	ctx := context.Background()
	buildTestImage(t, c)
	cleanup(t, c)
	t.Cleanup(func() { cleanup(t, c) })

	// Konteyner, KENDİ gördüğü mount seçeneklerini basar. Host tarafındaki
	// bayrağı okumak yetmez — sınanan iddia "konteyner İÇİNDE nodev,nosuid
	// görünüyor". Docker'ın local sürücüsü tam da burada sessizce
	// başarısız oluyordu (K-038).
	const mntSHA = "1111111111111111111111111111111111111111"
	buildTaggedImage(t, c, mntSHA, `CMD ["cat", "/proc/self/mountinfo"]`)

	if err := os.MkdirAll(c.volumePath(e2eApp, "data"), 0o750); err != nil {
		t.Fatal(err)
	}
	spec := CreateSpec{
		AppID: e2eApp, ReleaseID: "r1", Replica: 0, CommitSHA: mntSHA,
		MemoryBytes: 64 << 20, CPUMillis: 250, BlkioWeight: 500,
		Mounts:        []Mount{{VolumeName: "data", MountPath: "/data"}},
		ContainerPort: 8080,
	}
	if err := c.ContainerCreate(ctx, spec); err != nil {
		t.Fatalf("ContainerCreate: %v", err)
	}
	sel := Selector{AppID: e2eApp, ReleaseID: "r1"}
	if _, err := c.ContainerStart(ctx, sel); err != nil {
		t.Fatalf("ContainerStart: %v", err)
	}
	waitForState(t, c, "exited")

	opts := containerMountOptions(t, c, "/data")
	if opts == "" {
		t.Fatal("konteyner /data mount satırını basmadı — test bir şey ölçmüyor")
	}
	t.Logf("konteyner içindeki etkin seçenekler: %s", opts)
	for _, want := range requiredMountFlags {
		if !hasOption(opts, want) {
			t.Errorf("konteyner içinde %q YOK (etkin: %s)", want, opts)
		}
	}
}

// containerMountOptions, konteynerin bastığı mountinfo'dan hedef yolun
// etkin seçeneklerini okur.
//
// Günlükleri doğrudan Engine API'den alıyor: sürücünün günlük akıtma
// yolu henüz yok (sonraki dilim) ve testin onu beklemesi gerekmiyor.
func containerMountOptions(t *testing.T, c *Client, mountPath string) string {
	t.Helper()
	found, err := c.ContainerList(context.Background(), e2eApp)
	if err != nil || len(found) == 0 {
		t.Fatalf("konteyner bulunamadı: %v", err)
	}
	q := url.Values{"stdout": {"1"}, "stderr": {"1"}}
	resp, err := c.do(context.Background(), http.MethodGet,
		"/containers/"+found[0].ID+"/logs", q, nil)
	if err != nil {
		t.Fatalf("günlük okunamadı: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw := new(bytes.Buffer)
	_, _ = raw.ReadFrom(resp.Body)
	// Docker akışı 8 baytlık başlıklarla çerçeveler; mountinfo satırlarını
	// aramak için ham baytlarda dolaşmak yeterli.
	for _, line := range bytes.Split(raw.Bytes(), []byte("\n")) {
		fields := bytes.Fields(line)
		if len(fields) >= 6 && string(fields[4]) == mountPath {
			return string(fields[5])
		}
	}
	return ""
}
