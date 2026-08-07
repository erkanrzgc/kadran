//go:build dockere2e

// GERÇEK Docker daemon'ına karşı derleme ve günlük doğrulaması.
//
// # Neden yerel bir git daemon'ı?
//
// Derleme uçları `remote=<url>` ile çalışıyor, yani bağlamı BuildKit
// kendi çekiyor. Testi genel bir depoya bağlamak iki şeyi bozardı:
// depo içeriği değişirse test kayar, ve GitHub erişilemezse kırmızı olur.
// İkisi de sürücüyle ilgisiz sebeplerle "hata" üretir.
//
// Bunun yerine test kendi git sunucusunu kuruyor: içerik testin
// kontrolünde, ağ trafiği makinenin dışına çıkmıyor.
//
// ⚠ Bu, üretimdeki `https://` kısıtını GEVŞETMEZ. O kısıt
// exec.BuildContextURL'de ve orada birim testleriyle korunuyor; burada
// sınanan şey sürücünün gerçek Engine ile konuşabilmesi.

package dockerdrv

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// gitRepo, verilen Dockerfile'ı taşıyan yerel bir depo yayınlar ve
// bağlam URL'ini döndürür.
func gitRepo(t *testing.T, name, dockerfile string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		skipOrFail(t, "git yok: %v", err)
	}

	base := t.TempDir()
	// git daemon dizin izinlerine duyarlı; t.TempDir 0700 verebiliyor.
	if err := os.Chmod(base, 0o755); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(base, name)
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "Dockerfile"), []byte(dockerfile), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "e2e@panely.test"},
		{"config", "user.name", "panely e2e"},
		// GitHub'ın davranışını taklit et: tam SHA ile fetch'e izin ver.
		// ÖLÇÜLDÜ: github.com bunu destekliyor, dolayısıyla
		// BuildContextURL'in ürettiği `#<40-hex>` fragment'i çalışıyor.
		{"config", "uploadpack.allowReachableSHA1InWant", "true"},
		{"add", "-A"},
		{"commit", "-qm", "e2e"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(repo, ".git", "git-daemon-export-ok"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	port := startGitDaemon(t, base, name)
	return fmt.Sprintf("git://127.0.0.1:%d/%s#master", port, name)
}

// startGitDaemon, verilen kökü yayınlar ve portu döndürür.
//
// ⚠ SABİT PORT KULLANILMAZ ve "başlattım" YETERLİ SAYILMAZ.
//
// İlk hâli git'in varsayılan portunu (9418) kullanıyordu ve yalnızca
// cmd.Start()'a bakıyordu. Süreç başarıyla BAŞLIYOR, sonra "address
// already in use" ile ölüyor — Start() bunu görmez. Makinede o portu
// tutan başka bir git daemon varsa test SESSİZCE ONA bağlanır ve BAŞKA
// bir deponun içeriğini derler.
//
// Bu gerçekten oldu: geride kalmış bir daemon yüzünden test, kendi
// yazdığı Dockerfile yerine bambaşka bir depoyu derledi. İçerik tesadüfen
// uyuşsaydı test YEŞİL geçecek ve hiçbir şey kanıtlamayacaktı.
//
// Bu yüzden: işletim sisteminden BOŞ bir port alınıyor ve daemon'ın
// GERÇEKTEN bizim depomuzu yayınladığı `git ls-remote` ile doğrulanıyor.
func startGitDaemon(t *testing.T, base, repo string) int {
	t.Helper()
	port := freePort(t)
	cmd := exec.Command("git", "daemon",
		"--reuseaddr", "--export-all",
		fmt.Sprintf("--port=%d", port), "--base-path="+base, base)
	if err := cmd.Start(); err != nil {
		t.Fatalf("git daemon başlatılamadı: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})

	url := fmt.Sprintf("git://127.0.0.1:%d/%s", port, repo)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		probe := exec.Command("git", "ls-remote", url)
		if out, err := probe.CombinedOutput(); err == nil && len(out) > 0 {
			return port
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("git daemon %s adresinde depoyu yayınlamadı", url)
	return 0
}

// freePort, işletim sisteminden kullanılmayan bir port ister.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("boş port alınamadı: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	return port
}

// collectBuild, derlemeyi koşturur ve çıktıyı toplar.
func collectBuild(t *testing.T, c *Client, url, sha string) (string, string, error) {
	t.Helper()
	var out strings.Builder
	id, err := c.ImageBuild(context.Background(), BuildSpec{
		AppID: e2eApp, CommitSHA: sha, ContextURL: url,
	}, func(data []byte, _ bool) error {
		out.Write(data)
		return nil
	})
	return id, out.String(), err
}

// TestImageBuildAgainstRealDocker, uzak bağlamdan derlemenin uçtan uca
// çalıştığını doğrular.
func TestImageBuildAgainstRealDocker(t *testing.T) {
	c := e2eClient(t)
	const sha = "1111111111111111111111111111111111111111"
	url := gitRepo(t, "ok", "FROM busybox:1.36\nRUN echo panely-derleme-izi\n")
	t.Cleanup(func() { removeImage(t, c, ImageTag(e2eApp, sha)) })

	id, out, err := collectBuild(t, c, url, sha)
	if err != nil {
		t.Fatalf("derleme başarısız: %v\n%s", err, out)
	}
	// aux karesinden gelen kimlik: başarının POZİTİF kanıtı.
	if !strings.HasPrefix(id, "sha256:") {
		t.Errorf("imaj kimliği beklenen biçimde değil: %q", id)
	}
	// Derleme çıktısı çağırana AKMALI — istemci ilerlemeyi görebilmeli.
	if !strings.Contains(out, "panely-derleme-izi") {
		t.Errorf("derleme çıktısı akmadı:\n%s", out)
	}
	// Etiket sürücünün kurduğu ad olmalı.
	if !imageExists(t, c, ImageTag(e2eApp, sha)) {
		t.Errorf("imaj %q etiketiyle bulunamadı", ImageTag(e2eApp, sha))
	}
}

// TestImageBuildDetectsMidBuildFailure, bu dilimin ASIL tehlikesini
// gerçek daemon'da sınar.
//
// ÖLÇÜLDÜ: derleme ortasında ölen bir derleme HTTP 200 döndürür. Durum
// koduna bakan bir sürücü bunu BAŞARILI sayar, bozuk imaj dağıtılır ve
// arıza ancak üretimde görünür.
func TestImageBuildDetectsMidBuildFailure(t *testing.T) {
	c := e2eClient(t)
	const sha = "2222222222222222222222222222222222222222"
	url := gitRepo(t, "bad", "FROM busybox:1.36\nRUN echo once-calisir\nRUN exit 3\n")
	t.Cleanup(func() { removeImage(t, c, ImageTag(e2eApp, sha)) })

	id, out, err := collectBuild(t, c, url, sha)
	if err == nil {
		t.Fatalf("ORTADA ÖLEN derleme başarılı sayıldı — HTTP 200 yeterli görülmüş.\n%s", out)
	}
	if id != "" {
		t.Errorf("başarısız derleme imaj kimliği döndürdü: %q", id)
	}
	// Başarısızlığın sebebi kullanıcıya ulaşmalı.
	if !strings.Contains(err.Error(), "non-zero code: 3") {
		t.Errorf("hata sebebi taşınmadı: %v", err)
	}
	// İlk adımın çıktısı yine de akmış olmalı: kullanıcı NEREDE öldüğünü
	// görebilmeli.
	if !strings.Contains(out, "once-calisir") {
		t.Errorf("başarısız derlemenin çıktısı akmadı:\n%s", out)
	}
}

// TestImageBuildRejectsMissingContext, çekme aşamasındaki hatanın (HTTP
// 500) de yakalandığını doğrular. Başarısızlığın İKİNCİ biçimi budur.
func TestImageBuildRejectsMissingContext(t *testing.T) {
	c := e2eClient(t)
	// Depoyu kur (daemon ayağa kalksın) ama var olmayan bir ada işaret et.
	url := gitRepo(t, "ok", "FROM busybox:1.36\n")
	missing := strings.Replace(url, "/ok#", "/hicyok#", 1)

	if _, _, err := collectBuild(t, c, missing, e2eSHA); err == nil {
		t.Fatal("var olmayan bağlam kabul edildi")
	}
}

// TestContainerLogsSeparatesStreamsOnRealDocker, çerçeve çözümlemesini
// GERÇEK daemon çıktısında doğrular.
//
// Birim testi ölçülmüş baytlara karşı koşuyor; bu test o baytların hâlâ
// daemon'ın ürettiği şey olduğunu kanıtlıyor.
func TestContainerLogsSeparatesStreamsOnRealDocker(t *testing.T) {
	c := e2eClient(t)
	ctx := context.Background()
	buildTaggedImage(t, c, e2eSHA,
		`CMD ["sh", "-c", "echo cikti-satiri; echo hata-satiri 1>&2; sleep 30"]`)
	cleanup(t, c)
	t.Cleanup(func() { cleanup(t, c) })

	// ⚠ AĞI BU TEST KURAR — devralmaz.
	//
	// İlk hâlinde bu satır yoktu ve test geliştirme sunucusunda GEÇTİ:
	// `panely-e2etest` ağı önceki koşulardan KALMIŞTI. CI'da temiz bir
	// runner'da düştü (`network panely-e2etest not found`), çünkü dosya
	// adı sırası bu testi ağı kuran yaşam döngüsü testinden ÖNCE koşturuyor.
	//
	// Yani test geçmiyordu; ARTIK KALMIŞ DURUMA yaslanıyordu. Bir testin
	// yeşili, ancak kendi ön koşullarını kendisi kurduğunda bir şey ifade
	// eder — K-043'ün aynı sınıfı (o da bağımlılığın hazır olduğunu
	// varsaymıştı).
	if _, err := c.NetworkEnsure(ctx, e2eApp); err != nil {
		t.Fatalf("NetworkEnsure: %v", err)
	}

	spec := CreateSpec{
		AppID: e2eApp, ReleaseID: "r1", Replica: 0, CommitSHA: e2eSHA,
		MemoryBytes: 64 << 20, CPUMillis: 250, BlkioWeight: 500,
	}
	if err := c.ContainerCreate(ctx, spec); err != nil {
		t.Fatalf("ContainerCreate: %v", err)
	}
	if _, err := c.ContainerStart(ctx, Selector{AppID: e2eApp, ReleaseID: "r1"}); err != nil {
		t.Fatalf("ContainerStart: %v", err)
	}
	if st := waitForState(t, c, "running"); st != "running" {
		t.Fatalf("konteyner çalışmadı: %q", st)
	}
	// Çıktının yazılması için kısa bekleme.
	time.Sleep(time.Second)

	replica := uint32(0)
	var out, errOut strings.Builder
	err := c.ContainerLogs(ctx,
		Selector{AppID: e2eApp, ReleaseID: "r1", Replica: &replica},
		0, false, time.Time{},
		func(data []byte, isStderr bool) error {
			if isStderr {
				errOut.Write(data)
			} else {
				out.Write(data)
			}
			return nil
		})
	if err != nil {
		t.Fatalf("ContainerLogs: %v", err)
	}

	if !strings.Contains(out.String(), "cikti-satiri") {
		t.Errorf("stdout alınamadı: %q", out.String())
	}
	if !strings.Contains(errOut.String(), "hata-satiri") {
		t.Errorf("stderr alınamadı: %q", errOut.String())
	}
	// ⚠ AYRIM asıl ölçülen şey: karışmışlarsa çerçeve çözümlemesi
	// çalışmıyor demektir ve istemci hata satırlarını ayırt edemez.
	if strings.Contains(out.String(), "hata-satiri") {
		t.Error("stderr içeriği stdout'a karıştı — çerçeve çözümlemesi bozuk")
	}
	if strings.Contains(errOut.String(), "cikti-satiri") {
		t.Error("stdout içeriği stderr'e karıştı — çerçeve çözümlemesi bozuk")
	}
}

// removeImage, testin ürettiği imajı siler.
func removeImage(t *testing.T, c *Client, tag string) {
	t.Helper()
	_ = c.doJSON(context.Background(), "DELETE", "/images/"+tag, nil, nil, nil)
}
