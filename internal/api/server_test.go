package api

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/erkanrzgc/panely/internal/audit"
	"github.com/erkanrzgc/panely/internal/execclient"
	panelyv1 "github.com/erkanrzgc/panely/internal/pb/panely/v1"
	"github.com/erkanrzgc/panely/internal/store"
	"github.com/erkanrzgc/panely/internal/version"
)

func newTestServer(t *testing.T) (*Server, *store.Store) {
	t.Helper()

	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "panely.db"))
	if err != nil {
		t.Fatalf("veritabanı açılamadı: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// grpc.NewClient tembeldir: bağlantı ilk RPC'de kurulur, bu yüzden
	// var olmayan bir sokete "bağlanmak" hata vermez. Executor'a
	// dokunmayan testler için yeterli.
	exec, err := execclient.Dial(filepath.Join(t.TempDir(), "yok.sock"))
	if err != nil {
		t.Fatalf("executor istemcisi oluşturulamadı: %v", err)
	}
	t.Cleanup(func() { _ = exec.Close() })

	srv, err := NewServer(ServerOptions{
		Store: db, Executor: exec,
		Rollout: &fakeRollout{}, Reconciler: &fakeReconciler{},
	})
	if err != nil {
		t.Fatalf("sunucu oluşturulamadı: %v", err)
	}
	return srv, db
}

func TestNewServerRequiresStore(t *testing.T) {
	exec, err := execclient.Dial(filepath.Join(t.TempDir(), "yok.sock"))
	if err != nil {
		t.Fatalf("istemci oluşturulamadı: %v", err)
	}
	defer func() { _ = exec.Close() }()

	if _, err := NewServer(ServerOptions{Executor: exec, Rollout: &fakeRollout{}}); err == nil {
		t.Fatal("veritabanı olmadan sunucu oluşturuldu")
	}
}

func TestNewServerRequiresExecutor(t *testing.T) {
	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "panely.db"))
	if err != nil {
		t.Fatalf("veritabanı açılamadı: %v", err)
	}
	defer func() { _ = db.Close() }()

	if _, err := NewServer(ServerOptions{Store: db, Rollout: &fakeRollout{}}); err == nil {
		t.Fatal("executor olmadan sunucu oluşturuldu")
	}
}

func TestPingReportsVersionAndProtocol(t *testing.T) {
	srv, _ := newTestServer(t)

	resp, err := srv.Ping(context.Background(), &panelyv1.PingRequest{})
	if err != nil {
		t.Fatalf("ping başarısız: %v", err)
	}

	if resp.GetDaemonVersion() != version.Version {
		t.Errorf("sürüm = %q, beklenen %q", resp.GetDaemonVersion(), version.Version)
	}
	if resp.GetProtocolVersion() != version.Protocol {
		t.Errorf("protokol = %d, beklenen %d", resp.GetProtocolVersion(), version.Protocol)
	}
	if resp.GetServerTime() == nil {
		t.Error("sunucu zamanı boş")
	}
	if resp.GetCompatibilityWarning() != "" {
		t.Errorf("beklenmedik uyumluluk uyarısı: %q", resp.GetCompatibilityWarning())
	}
}

func TestPingWarnsOnVersionMismatch(t *testing.T) {
	srv, _ := newTestServer(t)

	resp, err := srv.Ping(context.Background(), &panelyv1.PingRequest{
		ClientVersion: "v0.0.1-eski",
	})
	if err != nil {
		t.Fatalf("ping başarısız: %v", err)
	}

	warning := resp.GetCompatibilityWarning()
	if warning == "" {
		t.Fatal("sürüm uyumsuzluğunda uyarı verilmedi")
	}
	if !strings.Contains(warning, "v0.0.1-eski") {
		t.Errorf("uyarı istemci sürümünü içermiyor: %q", warning)
	}
}

// TestPingDoesNotRejectVersionMismatch, sürüm farkının bağlantıyı
// KESMEDİĞİNİ doğrular.
//
// Protokol sürümü aynı olduğu sürece iletişim geçerlidir. Yama sürümü
// farklı diye bağlantıyı reddetmek, operatörü tam da güncelleme yapmak
// istediği anda panelden kilitler.
func TestPingDoesNotRejectVersionMismatch(t *testing.T) {
	srv, _ := newTestServer(t)

	if _, err := srv.Ping(context.Background(), &panelyv1.PingRequest{
		ClientVersion: "v99.0.0",
	}); err != nil {
		t.Fatalf("sürüm farkı bağlantıyı kesti: %v", err)
	}
}

func TestGetSystemInfoReportsUnreachableExecutor(t *testing.T) {
	srv, _ := newTestServer(t)

	// Executor yok; yanıt hata DÖNDÜRMEMELİ, durumu bildirmeli.
	resp, err := srv.GetSystemInfo(context.Background(), &panelyv1.GetSystemInfoRequest{})
	if err != nil {
		t.Fatalf("executor erişilemezken hata döndü: %v", err)
	}
	if resp.GetExecutorReachable() {
		t.Error("var olmayan executor erişilebilir bildirildi")
	}
	if resp.GetDaemonVersion() != version.Version {
		t.Errorf("sürüm = %q", resp.GetDaemonVersion())
	}
	if resp.GetRunningAsUser() == "" {
		t.Error("çalışılan kullanıcı bildirilmedi")
	}
}

func TestListAuditRecordsReturnsChain(t *testing.T) {
	srv, db := newTestServer(t)
	ctx := context.Background()

	for range 3 {
		if _, err := db.AppendAudit(ctx, audit.Record{
			Actor:   audit.SystemActor("test"),
			Action:  "daemon.start",
			Outcome: audit.OutcomeSuccess,
			Source:  audit.SourceDaemon,
		}); err != nil {
			t.Fatalf("kayıt eklenemedi: %v", err)
		}
	}

	resp, err := srv.ListAuditRecords(ctx, &panelyv1.ListAuditRecordsRequest{})
	if err != nil {
		t.Fatalf("kayıtlar okunamadı: %v", err)
	}
	if len(resp.GetRecords()) != 3 {
		t.Errorf("kayıt sayısı = %d, beklenen 3", len(resp.GetRecords()))
	}
	if resp.GetLatestSeq() != 3 {
		t.Errorf("son sıra = %d, beklenen 3", resp.GetLatestSeq())
	}
}

func TestListAuditRecordsRespectsAfterSeq(t *testing.T) {
	srv, db := newTestServer(t)
	ctx := context.Background()

	for range 5 {
		if _, err := db.AppendAudit(ctx, audit.Record{
			Actor:   audit.SystemActor("test"),
			Action:  "x",
			Outcome: audit.OutcomeSuccess,
			Source:  audit.SourceDaemon,
		}); err != nil {
			t.Fatalf("kayıt eklenemedi: %v", err)
		}
	}

	resp, err := srv.ListAuditRecords(ctx, &panelyv1.ListAuditRecordsRequest{AfterSeq: 3})
	if err != nil {
		t.Fatalf("kayıtlar okunamadı: %v", err)
	}
	if len(resp.GetRecords()) != 2 {
		t.Fatalf("kayıt sayısı = %d, beklenen 2", len(resp.GetRecords()))
	}
	if resp.GetRecords()[0].GetSeq() != 4 {
		t.Errorf("ilk sıra = %d, beklenen 4", resp.GetRecords()[0].GetSeq())
	}
}

// TestVerifyAuditChainSeparatesTwoChains, iki zincirin BAĞIMSIZ
// raporlandığını doğrular.
//
// Daemon zinciri geçerli, executor'a ulaşılamıyor: sonuç "daemon geçerli,
// executor okunamadı" olmalı. İkisini tek bir boolean'da birleştirmek,
// panelyd'nin kendi zincirini doğrulayıp executor'ınkini atlamasını
// gizlerdi — oysa modelin tamamı ikisinin ayrı olmasına dayanıyor.
func TestVerifyAuditChainSeparatesTwoChains(t *testing.T) {
	srv, db := newTestServer(t)
	ctx := context.Background()

	if _, err := db.AppendAudit(ctx, audit.Record{
		Actor:   audit.SystemActor("test"),
		Action:  "daemon.start",
		Outcome: audit.OutcomeSuccess,
		Source:  audit.SourceDaemon,
	}); err != nil {
		t.Fatalf("kayıt eklenemedi: %v", err)
	}

	resp, err := srv.VerifyAuditChain(ctx, &panelyv1.VerifyAuditChainRequest{})
	if err != nil {
		t.Fatalf("doğrulama başarısız: %v", err)
	}

	if got := resp.GetDaemonStatus(); got != panelyv1.ChainStatus_CHAIN_STATUS_VALID {
		t.Errorf("daemon durumu = %v, beklenen VALID: %s", got, resp.GetDetail())
	}
	if resp.GetRecordsChecked() != 1 {
		t.Errorf("doğrulanan kayıt = %d, beklenen 1", resp.GetRecordsChecked())
	}

	// Executor yok → ayrı alanda raporlanmalı, daemon sonucunu bozmamalı.
	//
	// Ve UNREACHABLE olmalı, INVALID DEĞİL: erişilemeyen bir executor
	// kurcalama kanıtı değildir. İkisini karıştırmak, executor'ın kapalı
	// olduğu her an sahte bir güvenlik alarmı üretirdi.
	if got := resp.GetExecutorStatus(); got != panelyv1.ChainStatus_CHAIN_STATUS_UNREACHABLE {
		t.Errorf("erişilemeyen executor durumu = %v, beklenen UNREACHABLE", got)
	}
	if resp.GetExecutorDetail() == "" {
		t.Error("executor durumu açıklanmadı")
	}
}

func TestVerifyAuditChainOnEmptyDatabase(t *testing.T) {
	srv, _ := newTestServer(t)

	resp, err := srv.VerifyAuditChain(context.Background(), &panelyv1.VerifyAuditChainRequest{})
	if err != nil {
		t.Fatalf("doğrulama başarısız: %v", err)
	}
	if got := resp.GetDaemonStatus(); got != panelyv1.ChainStatus_CHAIN_STATUS_VALID {
		t.Errorf("boş zincir durumu = %v, beklenen VALID", got)
	}
	if resp.GetRecordsChecked() != 0 {
		t.Errorf("doğrulanan kayıt = %d, beklenen 0", resp.GetRecordsChecked())
	}
}

// fakeRollout, dağıtımın trafik yarısını taklit eder.
//
// Sınanan şey "Run çağrıldı mı" değil, HANGİ sürümle çağrıldığı: yanlış
// sürümü canlıya alan bir dağıtım, testlerde "çağrıldı" diye yeşil
// geçerdi.
type fakeRollout struct {
	calls []string
	err   error

	// rollbackCalls, geri alma çağrılarını AYRI tutar.
	//
	// `calls` ile birleştirilseydi, dağıtım yerine geri alma çağıran (ya
	// da tersini yapan) bir sunucu hatası testlerde görünmezdi.
	rollbackCalls []string
	rollbackErr   error
	recreated     bool

	// activations, verilirse aktif sürüm GERÇEKTEN yazılır.
	//
	// ── Neden sahte bunu yapmak zorunda? ──────────────────────────────
	//
	// Gerçek Rollout, kapıdan sonra SetActiveRelease çağırıyor; dağıtım
	// geçmişi oradan doğuyor. Sahte bunu atlarsa hiçbir dağıtım geçmişe
	// girmez ve geçmişe dayanan her şey — geri almanın hedefi, ardışık
	// geri almalar — API katmanında SINANAMAZ hâle gelir. Yani bu, sahteyi
	// gerçeğe yaklaştırmak değil, sahtenin YALAN SÖYLEMESİNİ engellemek.
	activations *store.Store
}

func (f *fakeRollout) Run(_ context.Context, app store.App, rel store.Release) error {
	f.calls = append(f.calls, app.ID+"/"+rel.ID)
	if f.err != nil {
		return f.err
	}
	return f.activate(app.ID, rel.ID)
}

func (f *fakeRollout) Rollback(
	_ context.Context, app store.App, rel store.Release,
) (bool, error) {
	f.rollbackCalls = append(f.rollbackCalls, app.ID+"/"+rel.ID)
	if f.rollbackErr != nil {
		return f.recreated, f.rollbackErr
	}
	return f.recreated, f.activate(app.ID, rel.ID)
}

func (f *fakeRollout) activate(appID, releaseID string) error {
	if f.activations == nil {
		return nil
	}
	return f.activations.SetActiveRelease(context.Background(), appID, releaseID)
}
