package main

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	panelyv1 "github.com/erkanrzgc/kadran/internal/pb/panely/v1"
)

// K-112: `app show`'un DURUM sütunu derlemenin durumunu gösteriyordu,
// hangi sürümün CANLI olduğunu değil. Geri almadan sonra en üstteki
// "derlendi" satırı trafiği almıyor olabilir.

func showResp(active *string, ids ...string) *panelyv1.GetAppResponse {
	rels := make([]*panelyv1.Release, 0, len(ids))
	for _, id := range ids {
		rels = append(rels, &panelyv1.Release{
			ReleaseId: id, AppId: "blog",
			CommitSha: strings.Repeat("a", 40),
			Status:    panelyv1.ReleaseStatus_RELEASE_STATUS_BUILT,
			ImageId:   "sha256:" + strings.Repeat("b", 64),
		})
	}
	return &panelyv1.GetAppResponse{
		App:             &panelyv1.App{Spec: &panelyv1.AppSpec{AppId: "blog"}},
		Releases:        rels,
		ActiveReleaseId: active,
	}
}

// rowOf, tablodaki sürüm satırını döndürür.
func rowOf(t *testing.T, out, id string) string {
	t.Helper()
	for _, l := range strings.Split(out, "\n") {
		if f := strings.Fields(l); len(f) > 0 && f[0] == id {
			return l
		}
	}
	t.Fatalf("%s satırı yok:\n%s", id, out)
	return ""
}

func TestPrintAppMarksTheLiveRelease(t *testing.T) {
	c, out, _ := newTestCLI("")
	c.printApp(showResp(proto.String("r2"), "r3", "r2", "r1"))
	s := out.String()

	if !strings.Contains(s, "DERLEME") || !strings.Contains(s, "TRAFİK") {
		t.Errorf("başlık derleme durumunu trafikten ayırmıyor:\n%s", s)
	}
	if !strings.Contains(s, "Canlı    : r2") {
		t.Errorf("canlı sürüm satırı yok:\n%s", s)
	}
	if !strings.Contains(rowOf(t, s, "r2"), "canlı") {
		t.Errorf("r2 canlı işaretlenmedi:\n%s", s)
	}
	for _, id := range []string{"r3", "r1"} {
		if strings.Contains(rowOf(t, s, id), "canlı") {
			t.Errorf("%s canlı DEĞİL ama işaretlendi:\n%s", id, s)
		}
	}
}

// TestPrintAppShowsALiveReleaseOutsideTheList: geri almadan sonra canlı
// sürüm, --releases ile kesilmiş listenin dışında kalabilir.
func TestPrintAppShowsALiveReleaseOutsideTheList(t *testing.T) {
	c, out, _ := newTestCLI("")
	c.printApp(showResp(proto.String("r1"), "r3"))
	s := out.String()

	if !strings.Contains(s, "Canlı    : r1") || !strings.Contains(s, "listede yok") {
		t.Errorf("listenin dışındaki canlı sürüm söylenmedi:\n%s", s)
	}
	if strings.Contains(rowOf(t, s, "r3"), "canlı") {
		t.Errorf("r3 canlı DEĞİL ama işaretlendi:\n%s", s)
	}
}

func TestPrintAppSaysWhenNothingIsLive(t *testing.T) {
	c, out, _ := newTestCLI("")
	c.printApp(showResp(proto.String(""), "r1"))
	s := out.String()

	if !strings.Contains(s, "Canlı    : yok") {
		t.Errorf("canlı sürüm olmadığı söylenmedi:\n%s", s)
	}
	if strings.Contains(rowOf(t, s, "r1"), "canlı") {
		t.Errorf("canlı sürüm yokken r1 işaretlendi:\n%s", s)
	}
}

// TestPrintAppDoesNotClaimNothingIsLiveOnAnOldServer: alan eklemek
// protokol sürümünü artırmıyor (internal/version), yani yeni CLI eski
// (v0.1.0) sunucuyla konuşabilir ve o sunucu canlı sürümü HİÇ göndermez.
// İlk hâli bunu "boş" okuyup her canlı uygulama için "yok — trafik
// yönlendirilmiyor" diyordu: trafik akarken akmıyor demek. CI bunu
// göremezdi, orada CLI ile sunucu hep aynı commit'ten.
func TestPrintAppDoesNotClaimNothingIsLiveOnAnOldServer(t *testing.T) {
	c, out, _ := newTestCLI("")
	c.printApp(showResp(nil, "r2", "r1"))
	s := out.String()

	if strings.Contains(s, "Canlı    : yok") {
		t.Errorf("eski sunucuda 'canlı sürüm yok' dendi — bilinmeyen yok sayıldı:\n%s", s)
	}
	if !strings.Contains(s, "Canlı    : bilinmiyor") {
		t.Errorf("eski sunucu durumu söylenmedi:\n%s", s)
	}
	for _, id := range []string{"r2", "r1"} {
		if f := strings.Fields(rowOf(t, s, id)); len(f) < 4 || f[3] != "?" {
			t.Errorf("%s satırının TRAFİK hücresi '?' değil: %q", id, rowOf(t, s, id))
		}
	}
}
