package exec

import (
	"strings"
	"testing"

	"github.com/erkanrzgc/panely/internal/audit"
	panelyv1 "github.com/erkanrzgc/panely/internal/pb/panely/v1"
)

// records, günlüğün tamamını okur.
func records(t *testing.T, srv *Server) []audit.Record {
	t.Helper()
	recs, err := srv.journal.Read(0, 1000)
	if err != nil {
		t.Fatalf("günlük okunamadı: %v", err)
	}
	return recs
}

// TestRejectedRequestIsAudited, REDDEDİLEN isteğin zincire
// AUDIT_OUTCOME_DENIED ile yazıldığını doğrular.
//
// Reddedilen istek ayrıcalıklı hiçbir şey yapmaz, ama güvenlik modelinin
// DEVREYE GİRDİĞİ andır: ele geçirilmiş bir panelyd'nin şemayı zorlama
// denemeleri ancak böyle görünür olur. Kaydedilmezse saldırı denemesi
// hiçbir iz bırakmaz.
func TestRejectedRequestIsAudited(t *testing.T) {
	srv := newTestServer(t)

	bad := validCreateRequest()
	bad.Volumes[0].MountPath = "/proc/self"

	if _, err := srv.ContainerCreate(t.Context(), bad); err == nil {
		t.Fatal("kaçış denemesi kabul edildi")
	}

	recs := records(t, srv)
	if len(recs) != 1 {
		t.Fatalf("%d kayıt yazıldı, 1 bekleniyordu", len(recs))
	}
	if recs[0].Outcome != audit.OutcomeDenied {
		t.Errorf("outcome %v, denied bekleniyordu", recs[0].Outcome)
	}
	if recs[0].Action != "container.create" {
		t.Errorf("action %q", recs[0].Action)
	}
	if recs[0].Source != audit.SourceExecutor {
		t.Errorf("source %v — executor kendi kaydını yazmalı", recs[0].Source)
	}
}

// TestAuditNeverCarriesEnvValues, ortam değişkeni DEĞERLERİNİN zincire
// hiç girmediğini doğrular.
//
// Zincir ekle-sadece'dir ve kayıtlar hash'lenir: buraya bir kez düz metin
// sır yazılırsa GERİ ALINAMAZ — silmek zinciri koparır. Bu yüzden
// redaksiyon varsayılan-reddet: anahtar adları kalır, değerler gitmez.
func TestAuditNeverCarriesEnvValues(t *testing.T) {
	srv := newTestServer(t)

	const secret = "s3cret-tokeni-asla-gorunmemeli"
	req := validCreateRequest()
	req.Env = map[string]string{
		"DATABASE_URL": secret,
		// Adı masum olan bir değişken de redakte edilmeli: "hassas
		// görünen anahtarları gizle" yaklaşımı, adı masum olan sırları
		// kaçırır.
		"PORT":     secret,
		"HARMLESS": secret,
	}
	// Doğrulamayı KASTEN düşürüyoruz ki reddedilen yolda da redaksiyonun
	// uygulandığı görülsün — sır, isteğin kabul edilip edilmemesinden
	// bağımsız olarak kayda girmemeli.
	req.ContainerPort = 0

	if _, err := srv.ContainerCreate(t.Context(), req); err == nil {
		t.Fatal("geçersiz port kabul edildi")
	}

	recs := records(t, srv)
	if len(recs) == 0 {
		t.Fatal("hiç kayıt yok — test bir şey ölçmüyor")
	}
	for _, r := range recs {
		if strings.Contains(r.ParamsJSON, secret) {
			t.Fatalf("SIR DENETİM ZİNCİRİNE SIZDI: %s", r.ParamsJSON)
		}
	}

	// Pozitif kontrol: anahtar ADLARI kalmalı, yoksa yukarıdaki kontrol
	// "params boş olduğu için" geçiyor olabilirdi.
	if !strings.Contains(recs[0].ParamsJSON, "DATABASE_URL") {
		t.Errorf("anahtar adları da kaybolmuş — kayıt işe yaramaz: %s", recs[0].ParamsJSON)
	}
}

// TestAuditKeepsCommitSHA, commit_sha'nın kayda AÇIK girdiğini doğrular.
//
// release_id ↔ commit_sha bağı bir DAEMON değişmezidir; executor'ın
// veritabanı yoktur ve doğrulayamaz. Sapmanın sonradan yakalanabilmesi
// için ikisinin de kayıtta olması gerekir (exec.proto'daki nota bakınız).
func TestAuditKeepsCommitSHA(t *testing.T) {
	srv := newTestServer(t)

	req := validCreateRequest()
	req.ContainerPort = 0 // reddedilsin; kayıt yine yazılır
	if _, err := srv.ContainerCreate(t.Context(), req); err == nil {
		t.Fatal("geçersiz istek kabul edildi")
	}

	recs := records(t, srv)
	if len(recs) == 0 {
		t.Fatal("hiç kayıt yok")
	}
	if !strings.Contains(recs[0].ParamsJSON, req.GetCommitSha()) {
		t.Errorf("commit_sha kayda girmemiş: %s", recs[0].ParamsJSON)
	}
	if want := "release/blog/01hx9k2m"; recs[0].Target != want {
		t.Errorf("target %q, %q bekleniyordu", recs[0].Target, want)
	}
}

// TestReadOnlyCallsAreNotAudited, salt okunur uçların günlüğe
// YAZMADIĞINI doğrular.
//
// panelyd bu uçları durum ekranı için düzenli olarak çağırır. Her çağrıyı
// kaydetmek günlüğü gürültüyle doldurur ve asıl ayrıcalıklı işlemleri
// görünmez kılar; her şeyi kaydeden bir günlük hiçbir şey kaydetmemeye
// yaklaşır.
func TestReadOnlyCallsAreNotAudited(t *testing.T) {
	srv := newTestServer(t)

	if _, err := srv.Ping(t.Context(), &panelyv1.ExecutorServicePingRequest{}); err != nil {
		t.Fatal(err)
	}
	// ContainerList sürücüye ulaşır ve orada hata alır (soket yok); yine
	// de kaydedilmemeli.
	_, _ = srv.ContainerList(t.Context(), &panelyv1.ContainerListRequest{AppId: "blog"})

	if recs := records(t, srv); len(recs) != 0 {
		t.Errorf("salt okunur çağrılar %d kayıt yazdı", len(recs))
	}
}

// TestChainStaysVerifiableAfterDenials, arka arkaya reddedilen
// isteklerden sonra zincirin GEÇERLİ kaldığını doğrular.
//
// Kayıt yazma yolu bozuk olsaydı (sıra numarası atlanması, prev_hash
// hatası) bu, ancak zincir doğrulanmaya çalışıldığında ortaya çıkardı.
func TestChainStaysVerifiableAfterDenials(t *testing.T) {
	srv := newTestServer(t)

	for range 5 {
		bad := validCreateRequest()
		bad.Volumes[0].MountPath = "/dev/shm"
		if _, err := srv.ContainerCreate(t.Context(), bad); err == nil {
			t.Fatal("kaçış denemesi kabul edildi")
		}
	}

	recs := records(t, srv)
	if len(recs) != 5 {
		t.Fatalf("%d kayıt, 5 bekleniyordu", len(recs))
	}
	if _, err := audit.VerifyAll(recs); err != nil {
		t.Fatalf("zincir bozuldu: %v", err)
	}
}
