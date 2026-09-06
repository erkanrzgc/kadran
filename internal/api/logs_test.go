package api

import (
	"context"
	"errors"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	panelyv1 "github.com/erkanrzgc/panely/internal/pb/panely/v1"
	"github.com/erkanrzgc/panely/internal/store"
)

// logStream, akışa gönderilen kareleri toplar.
type logStream struct {
	grpc.ServerStreamingServer[panelyv1.StreamLogsResponse]
	ctx  context.Context
	sent []*panelyv1.StreamLogsResponse
}

func (s *logStream) Context() context.Context { return s.ctx }

func (s *logStream) Send(m *panelyv1.StreamLogsResponse) error {
	s.sent = append(s.sent, m)
	return nil
}

func newLogStream() *logStream {
	return &logStream{ctx: context.Background()}
}

// seedLiveApp, canlı sürümü olan bir uygulama kurar.
func seedLiveApp(t *testing.T, db *store.Store, appID string) string {
	t.Helper()
	rel := seedDeletableApp(t, db, appID)
	if err := db.SetActiveRelease(context.Background(), appID, rel.ID); err != nil {
		t.Fatalf("aktif sürüm yazılamadı: %v", err)
	}
	return rel.ID
}

// TestStreamLogsResolvesTheLiveReleaseItself, sürümün SUNUCUDA
// bulunduğunu doğrular.
//
// İstek yalnızca app_id taşıyor. Sürümü istemciden istemek, kullanıcıyı
// önce `app show` çalıştırıp elle kopyalamaya iterdi — ve kopyaladığı
// sürüm, komutu çalıştırdığı anda artık aktif olmayabilirdi.
func TestStreamLogsResolvesTheLiveReleaseItself(t *testing.T) {
	exec := &fakeExec{logs: []logLine{{data: "merhaba\n"}}}
	srv, db := newDeleteServer(t, exec)
	want := seedLiveApp(t, db, "blog")

	st := newLogStream()
	if err := srv.StreamLogs(&panelyv1.StreamLogsRequest{AppId: "blog"}, st); err != nil {
		t.Fatalf("akış başarısız: %v", err)
	}

	if exec.lastLogs.ReleaseID != want {
		t.Errorf("sürüm %q, aktif sürüm %q bekleniyordu — istemcinin "+
			"gönderdiği değil, kayıttaki sürüm kullanılmalı",
			exec.lastLogs.ReleaseID, want)
	}
	if exec.lastLogs.AppID != "blog" {
		t.Errorf("uygulama %q", exec.lastLogs.AppID)
	}
}

// TestStreamLogsForwardsFrames, karelerin GERÇEKTEN aktığını ve stderr
// işaretinin korunduğunu doğrular.
func TestStreamLogsForwardsFrames(t *testing.T) {
	exec := &fakeExec{logs: []logLine{
		{data: "birinci\n"},
		{data: "hata satiri\n", isStderr: true},
	}}
	srv, db := newDeleteServer(t, exec)
	seedLiveApp(t, db, "blog")

	st := newLogStream()
	if err := srv.StreamLogs(&panelyv1.StreamLogsRequest{AppId: "blog"}, st); err != nil {
		t.Fatalf("akış başarısız: %v", err)
	}

	if len(st.sent) != 2 {
		t.Fatalf("%d kare gönderildi, 2 bekleniyordu", len(st.sent))
	}
	if got := string(st.sent[0].GetData()); got != "birinci\n" {
		t.Errorf("ilk kare %q", got)
	}
	if st.sent[0].GetIsStderr() {
		t.Error("stdout karesi stderr olarak işaretlendi")
	}
	// ⚠ stderr işareti KORUNMALI: istemci onu ayrı akıma yazıyor.
	// Kaybolursa `panely logs blog > app.log` hata satırlarını da dosyaya
	// gömer ve terminalde hiçbir şey görünmez.
	if !st.sent[1].GetIsStderr() {
		t.Error("stderr karesi stdout olarak işaretlendi — istemci onu " +
			"ayrı akıma yazamaz")
	}
}

// TestStreamLogsCapsTailLines, sınırsız geçmiş istenemediğini doğrular.
//
// Sınırsız bırakmak, aylardır koşan bir konteynerin bütün geçmişini tek
// istekte executor'dan panelyd'ye, oradan SSH üzerinden istemciye
// pompalamak demekti.
func TestStreamLogsCapsTailLines(t *testing.T) {
	exec := &fakeExec{}
	srv, db := newDeleteServer(t, exec)
	seedLiveApp(t, db, "blog")

	st := newLogStream()
	if err := srv.StreamLogs(&panelyv1.StreamLogsRequest{
		AppId: "blog", TailLines: 5_000_000}, st); err != nil {
		t.Fatalf("akış başarısız: %v", err)
	}

	if exec.lastLogs.TailLines != maxTailLines {
		t.Errorf("tail %d, %d ile sınırlanmalıydı",
			exec.lastLogs.TailLines, maxTailLines)
	}
}

// TestStreamLogsPassesTailAndFollowThrough, sınırın ALTINDAKİ değerlerin
// bozulmadan geçtiğini doğrular.
//
// Kontrol grubu: olmasaydı "her zaman maxTailLines gönder" diyen bir
// uygulama da yukarıdaki testi geçerdi.
func TestStreamLogsPassesTailAndFollowThrough(t *testing.T) {
	exec := &fakeExec{}
	srv, db := newDeleteServer(t, exec)
	seedLiveApp(t, db, "blog")

	st := newLogStream()
	if err := srv.StreamLogs(&panelyv1.StreamLogsRequest{
		AppId: "blog", TailLines: 42, Follow: true}, st); err != nil {
		t.Fatalf("akış başarısız: %v", err)
	}

	if exec.lastLogs.TailLines != 42 {
		t.Errorf("tail %d, 42 bekleniyordu", exec.lastLogs.TailLines)
	}
	if !exec.lastLogs.Follow {
		t.Error("follow taşınmadı — `-f` sessizce tek seferlik okumaya döner")
	}
}

// TestStreamLogsRefusesAppWithoutLiveRelease, dağıtılmamış uygulamanın
// DOĞRU KODLA reddedildiğini doğrular.
//
// İstek kusursuz; izin vermeyen şey sistemin durumu. InvalidArgument
// gören bir istemci isteği düzeltmeye çalışır, FailedPrecondition gören
// önce dağıtması gerektiğini bilir. Aynı ayrım `app delete`'te bir kez
// canlı ölçümde yakalanmıştı.
func TestStreamLogsRefusesAppWithoutLiveRelease(t *testing.T) {
	exec := &fakeExec{}
	srv, db := newDeleteServer(t, exec)
	seedDeletableApp(t, db, "blog") // dağıtım YOK

	err := srv.StreamLogs(&panelyv1.StreamLogsRequest{AppId: "blog"}, newLogStream())
	if err == nil {
		t.Fatal("dağıtılmamış uygulamanın günlüğü akıtıldı")
	}
	if got := status.Code(err); got != codes.FailedPrecondition {
		t.Errorf("gRPC kodu %s, FailedPrecondition bekleniyordu", got)
	}
	if exec.logCalls != 0 {
		t.Error("canlı sürüm yokken executor'a gidildi")
	}
}

// TestStreamLogsReportsMissingApp, olmayan uygulamanın NotFound
// döndürdüğünü doğrular.
func TestStreamLogsReportsMissingApp(t *testing.T) {
	exec := &fakeExec{}
	srv, _ := newDeleteServer(t, exec)

	err := srv.StreamLogs(&panelyv1.StreamLogsRequest{AppId: "yok"}, newLogStream())
	if status.Code(err) != codes.NotFound {
		t.Errorf("kod %s, NotFound bekleniyordu (%v)", status.Code(err), err)
	}
	if exec.logCalls != 0 {
		t.Error("olmayan uygulama için executor'a gidildi")
	}
}

// TestStreamLogsPropagatesExecutorFailure, executor hatasının
// YUTULMADIĞINI doğrular.
func TestStreamLogsPropagatesExecutorFailure(t *testing.T) {
	exec := &fakeExec{logErr: errors.New("executor kapalı")}
	srv, db := newDeleteServer(t, exec)
	seedLiveApp(t, db, "blog")

	err := srv.StreamLogs(&panelyv1.StreamLogsRequest{AppId: "blog"}, newLogStream())
	if err == nil {
		t.Fatal("executor hatası yutuldu — istemci boş bir akış görüp " +
			"uygulamanın sessiz olduğunu sanırdı")
	}
	if !strings.Contains(err.Error(), "executor kapalı") {
		t.Errorf("sebep taşınmıyor: %v", err)
	}
}
