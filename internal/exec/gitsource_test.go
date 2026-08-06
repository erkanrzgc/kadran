package exec

import (
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	panelyv1 "github.com/erkanrzgc/panely/internal/pb/panely/v1"
)

const testSHA = "7fd1a60b01f91b314f59955a4e4d4e80d8edf11d"

func validGitSource() *panelyv1.GitSource {
	return &panelyv1.GitSource{
		Host:      "github.com",
		Owner:     "octocat",
		Repo:      "Hello-World",
		CommitSha: testSHA,
	}
}

func validBuildRequest() *panelyv1.ImageBuildRequest {
	return &panelyv1.ImageBuildRequest{
		Release: &panelyv1.ReleaseRef{AppId: "blog", ReleaseId: "r1"},
		Source:  validGitSource(),
	}
}

// TestValidBuildRequestIsAccepted, POZİTİF KONTROLDÜR.
//
// Aşağıdaki reddetme testlerinin hepsi, her şeyi reddeden bir
// doğrulayıcıyla da geçerdi. Bu test onu reddediyor.
func TestValidBuildRequestIsAccepted(t *testing.T) {
	if err := validateImageBuild(validBuildRequest(), []string{"github.com"}); err != nil {
		t.Fatalf("geçerli istek reddedildi: %v", err)
	}
}

// TestGitHostWhitelistIsEnforced, beyaz listenin gerçekten uygulandığını
// doğrular. Liste executor'ın bayrağından gelir; ele geçirilmiş bir
// panelyd ona ekleme yapamamalı.
func TestGitHostWhitelistIsEnforced(t *testing.T) {
	src := validGitSource()
	src.Host = "evil.example.com"

	if err := validateGitSource(src, []string{"github.com"}); err == nil {
		t.Error("beyaz listede olmayan host kabul edildi")
	}

	// Aynı istek, host izinliyken geçmeli — yoksa test host'u değil
	// başka bir şeyi ölçüyor olurdu.
	if err := validateGitSource(src, []string{"github.com", "evil.example.com"}); err != nil {
		t.Errorf("izinli host reddedildi: %v", err)
	}
}

// TestEmptyWhitelistDoesNotMeanAllowAll, boş listenin "kısıt yok" olarak
// yorumlanmadığını doğrular.
//
// NewServer boş listeyi varsayılana çevirir. Bunu yapmasaydı, bir
// yapılandırma hatası sessizce güvenlik açığına dönerdi.
func TestEmptyWhitelistDoesNotMeanAllowAll(t *testing.T) {
	srv, err := NewServer(ServerOptions{Journal: &Journal{}})
	if err != nil {
		t.Fatal(err)
	}
	if len(srv.allowedGitHosts) == 0 {
		t.Fatal("boş beyaz liste boş kaldı — her host kabul edilirdi")
	}

	src := validGitSource()
	src.Host = "evil.example.com"
	if err := validateGitSource(src, srv.allowedGitHosts); err == nil {
		t.Error("varsayılan yapılandırmada rastgele host kabul edildi")
	}
}

// TestHostRejectsURLSyntax, host alanının bir URL parçası olamayacağını
// doğrular.
//
// Buradaki her girdi, serbest bir URL alanı olsaydı gerçek bir saldırı
// olurdu: ext:: komut çalıştırır, ssh:// executor'ın anahtarlarını
// kullanır, user:token@ kimlik bilgisini provenance'a sızdırır.
func TestHostRejectsURLSyntax(t *testing.T) {
	bad := []string{
		"ext::sh -c 'id'",
		"https://github.com",
		"ssh://git@github.com",
		"git://github.com",
		"github.com:22",
		"user:token@github.com",
		"github.com/evil",
		"github.com/../etc",
		"-oProxyCommand=id",
		"localhost",            // tek etiket, nokta yok
		"192.168.1.1",          // beyaz listede olmadığı için de düşer
		"github.com\nevil.com", // satır sonu enjeksiyonu
		"github.com evil.com",  // boşluk
		"GitHub.com",           // büyük harf — normalize edilmiyor, reddediliyor
		"",
	}
	for _, h := range bad {
		src := validGitSource()
		src.Host = h
		if err := validateGitSource(src, []string{"github.com"}); err == nil {
			t.Errorf("geçersiz host kabul edildi: %q", h)
		}
	}
}

// TestOwnerRepoRejectPathInjection, yol bileşenlerinin ek segment
// enjekte edemeyeceğini doğrular.
func TestOwnerRepoRejectPathInjection(t *testing.T) {
	bad := []string{
		"..", ".", "a/b", "../etc", "a\\b", "a:b", "a b", "",
		"-repo", ".hidden", "a#frag", "a?q=1", strings.Repeat("a", 101),
	}
	for _, v := range bad {
		src := validGitSource()
		src.Owner = v
		if err := validateGitSource(src, []string{"github.com"}); err == nil {
			t.Errorf("geçersiz owner kabul edildi: %q", v)
		}
		src = validGitSource()
		src.Repo = v
		if err := validateGitSource(src, []string{"github.com"}); err == nil {
			t.Errorf("geçersiz repo kabul edildi: %q", v)
		}
	}
}

// TestCommitSHAMustBeFullHex, referansın tam 40 haneli hex olmasını
// doğrular.
//
// ── Bu test neyi kapatıyor? ──
//
// CVE-2026-33748: BuildKit'in git fragment'i `<ref>:<subdir>` biçiminde
// ayrıştırılıyor ve subdir bileşeni depo kökünün dışına erişebiliyordu.
// Referans 40 haneli hex olmak ZORUNDAYSA iki nokta üst üste temsil
// edilemez, dolayısıyla subdir bileşeni oluşturulamaz.
//
// Ayrıca dal adı reddedilir: dal hareket eder, geri alma (§2.1)
// tekrarlanabilirliğe dayanır.
func TestCommitSHAMustBeFullHex(t *testing.T) {
	bad := []string{
		"main", "HEAD", "v1.2.3", "refs/heads/main",
		"7fd1a60",                    // kısa sha
		testSHA + "0",                // 41 hane
		testSHA[:39],                 // 39 hane
		strings.ToUpper(testSHA),     // büyük harf
		testSHA + ":etc/passwd",      // subdir bileşeni — CVE-2026-33748
		testSHA + ":../../../../etc", //
		testSHA + "#" + testSHA,      // ikinci fragment
		"main:/etc",                  //
		"",
	}
	for _, sha := range bad {
		src := validGitSource()
		src.CommitSha = sha
		if err := validateGitSource(src, []string{"github.com"}); err == nil {
			t.Errorf("geçersiz commit_sha kabul edildi: %q", sha)
		}
	}
}

// TestBuildContextURLCannotBePoisoned, kurulan URL'in doğrulanmış
// girdilerden beklenen biçimde çıktığını ve tehlikeli sözdizimi
// içermediğini doğrular.
func TestBuildContextURLCannotBePoisoned(t *testing.T) {
	got := BuildContextURL(validGitSource())
	want := "https://github.com/octocat/Hello-World.git#" + testSHA

	if got != want {
		t.Fatalf("URL beklenenden farklı:\n  gelen:    %s\n  beklenen: %s", got, want)
	}
	if !strings.HasPrefix(got, "https://") {
		t.Error("URL https ile başlamıyor")
	}
	// Fragment'te yalnızca bir tane olmalı ve içinde iki nokta olmamalı.
	_, frag, _ := strings.Cut(got, "#")
	if strings.ContainsAny(frag, ":#/") {
		t.Errorf("fragment tehlikeli karakter taşıyor: %q", frag)
	}
}

func TestImageTagIsConstructed(t *testing.T) {
	if got, want := ImageTag("blog", testSHA), "panely/blog:"+testSHA; got != want {
		t.Errorf("etiket %q, beklenen %q", got, want)
	}
}

// TestDockerfilePathStaysInsideRepo, Dockerfile yolunun depo kökünden
// çıkamayacağını doğrular.
func TestDockerfilePathStaysInsideRepo(t *testing.T) {
	bad := []string{
		"/etc/passwd", "../Dockerfile", "../../x", "a/../../b",
		"./Dockerfile", "a//b", "a/", ".", `C:\evil`, `a\b`,
	}
	for _, p := range bad {
		if err := validateDockerfilePath(p); err == nil {
			t.Errorf("geçersiz dockerfile_path kabul edildi: %q", p)
		}
	}

	good := []string{"", "Dockerfile", "docker/prod.Dockerfile", "a/b/c/Dockerfile"}
	for _, p := range good {
		if err := validateDockerfilePath(p); err != nil {
			t.Errorf("geçerli dockerfile_path reddedildi: %q (%v)", p, err)
		}
	}
}

// TestImageBuildHandlerValidatesFirst, handler'ın doğrulamayı GERÇEKTEN
// çağırdığını doğrular.
//
// Yukarıdaki testlerin tamamı doğrulayıcıları DOĞRUDAN çağırıyor;
// hiçbiri handler'ın onları çağırdığını kanıtlamaz. Doğrulamayı hiç
// çağırmayan bir handler bu dosyayı olduğu gibi geçirirdi.
//
// Ayırt edici sinyal iki kodun FARKLI olması: bozuk istek
// InvalidArgument, geçerli istek Unimplemented almalı.
func TestImageBuildHandlerValidatesFirst(t *testing.T) {
	srv, err := NewServer(ServerOptions{Journal: &Journal{}})
	if err != nil {
		t.Fatal(err)
	}

	bad := validBuildRequest()
	bad.Source.CommitSha = "main" // dal adı — reddedilmeli

	if got := status.Code(srv.ImageBuild(bad, nil)); got != codes.InvalidArgument {
		t.Errorf("bozuk istek %q döndü, InvalidArgument bekleniyordu — "+
			"handler doğrulamayı çağırmıyor olabilir", got)
	}

	if got := status.Code(srv.ImageBuild(validBuildRequest(), nil)); got != codes.Unimplemented {
		t.Errorf("geçerli istek %q döndü, Unimplemented bekleniyordu", got)
	}
}
