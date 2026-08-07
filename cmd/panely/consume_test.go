package main

import (
	"errors"
	"io"
	"strings"
	"testing"

	"google.golang.org/grpc"

	panelyv1 "github.com/erkanrzgc/panely/internal/pb/panely/v1"
)

// ── consumeDeploy: istemci de POZİTİF ölçüt kullanıyor mu ────────────

// fakeDeployStream, sunucu akışını taklit eder.
//
// grpc.ClientStream KASTEN nil bırakılıyor: bu testin kullanmadığı bir
// metot çağrılırsa sessizce sıfır değer dönmek yerine gürültülü biçimde
// panikler. Sınanmayan bir yolun sınanmış görünmesi daha kötüdür.
type fakeDeployStream struct {
	grpc.ClientStream
	msgs []*panelyv1.DeployResponse
	err  error // mesajlar tükendikten sonra dönecek hata; nil ise EOF
	i    int
}

func (f *fakeDeployStream) Recv() (*panelyv1.DeployResponse, error) {
	if f.i < len(f.msgs) {
		m := f.msgs[f.i]
		f.i++
		return m, nil
	}
	if f.err != nil {
		return nil, f.err
	}
	return nil, io.EOF
}

func acceptedMsg(id string) *panelyv1.DeployResponse {
	return &panelyv1.DeployResponse{Event: &panelyv1.DeployResponse_Accepted{
		Accepted: &panelyv1.DeployAccepted{ReleaseId: id, CommitSha: measuredMainSHA},
	}}
}

func outputMsg(s string) *panelyv1.DeployResponse {
	return &panelyv1.DeployResponse{Event: &panelyv1.DeployResponse_Output{
		Output: &panelyv1.BuildOutput{Data: []byte(s)},
	}}
}

func succeededMsg(id, img string) *panelyv1.DeployResponse {
	return &panelyv1.DeployResponse{Event: &panelyv1.DeployResponse_Succeeded{
		Succeeded: &panelyv1.DeploySucceeded{ReleaseId: id, ImageId: img},
	}}
}

// TestConsumeDeployRequiresSuccessFrame, "akış hatasız bitti" durumunun
// BAŞARI SAYILMADIĞINI doğrular.
//
// Bu, K-042'nin istemci ucudur. Sunucu başarıyı yalnızca son mesajla
// bildiriyor; hata yokluğuna bakan bir istemci, sunucu tarafında sessizce
// değişen bir davranışı fark etmezdi. İki uç aynı ölçütü kullanmazsa
// "başarılı" kelimesinin anlamı ikisinde ayrışır.
func TestConsumeDeployRequiresSuccessFrame(t *testing.T) {
	c, _, errBuf := newTestCLI("")

	// Hata YOK, çıktı akmış, akış temiz bitmiş — ama başarı karesi yok.
	code := c.consumeDeploy(&fakeDeployStream{msgs: []*panelyv1.DeployResponse{
		acceptedMsg("r1"), outputMsg("Step 1/2\n"), outputMsg("Step 2/2\n"),
	}})

	if code == exitOK {
		t.Fatal("başarı karesi olmadan çıkış kodu 0 döndü")
	}
	if !strings.Contains(errBuf.String(), "KANITLANAMADI") {
		t.Errorf("hata sebebi açıklanmıyor: %q", errBuf.String())
	}
}

func TestConsumeDeployReportsSuccess(t *testing.T) {
	c, out, errBuf := newTestCLI("")

	code := c.consumeDeploy(&fakeDeployStream{msgs: []*panelyv1.DeployResponse{
		acceptedMsg("r7"), outputMsg("Step 1/1\n"), succeededMsg("r7", "sha256:abcdef012345"),
	}})

	if code != exitOK {
		t.Fatalf("çıkış kodu %d, beklenen 0", code)
	}
	if !strings.Contains(out.String(), "r7") || !strings.Contains(out.String(), "abcdef012345") {
		t.Errorf("stdout sürüm/imaj bildirmiyor: %q", out.String())
	}
	// Derleme çıktısı stdout'u KİRLETMEMELİ: `panely deploy … | jq`
	// bozulmamalı.
	if strings.Contains(out.String(), "Step 1/1") {
		t.Errorf("derleme çıktısı stdout'a yazıldı: %q", out.String())
	}
	if !strings.Contains(errBuf.String(), "Step 1/1") {
		t.Errorf("derleme çıktısı stderr'e ulaşmadı: %q", errBuf.String())
	}
}

// TestConsumeDeployNamesTheReleaseOnFailure, hata durumunda sürüm
// kimliğinin kullanıcıya söylendiğini doğrular: `panely app show` ile
// sebebe ulaşabilmesi buna bağlı.
func TestConsumeDeployNamesTheReleaseOnFailure(t *testing.T) {
	c, _, errBuf := newTestCLI("")

	code := c.consumeDeploy(&fakeDeployStream{
		msgs: []*panelyv1.DeployResponse{acceptedMsg("r3"), outputMsg("Step 1/2\n")},
		err:  errors.New("rpc error: derleme başarısız"),
	})

	if code == exitOK {
		t.Fatal("başarısız akışta çıkış kodu 0")
	}
	if !strings.Contains(errBuf.String(), "r3") {
		t.Errorf("başarısız sürümün kimliği bildirilmedi: %q", errBuf.String())
	}
}
