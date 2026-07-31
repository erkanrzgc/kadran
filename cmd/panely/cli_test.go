package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	panelyv1 "github.com/erkanrzgc/panely/internal/pb/panely/v1"
)

// newTestCLI, çıktısı yakalanabilen bir cli üretir.
func newTestCLI(stdin string) (*cli, *bytes.Buffer, *bytes.Buffer) {
	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	return &cli{
		stdin:  strings.NewReader(stdin),
		stdout: out,
		stderr: errOut,
	}, out, errOut
}

func TestRunWithoutArgsIsUsageError(t *testing.T) {
	c, _, errOut := newTestCLI("")

	if code := c.run(context.Background(), nil); code != exitUsage {
		t.Errorf("çıkış kodu = %d, beklenen %d", code, exitUsage)
	}
	if !strings.Contains(errOut.String(), "Kullanım:") {
		t.Error("kullanım metni gösterilmedi")
	}
}

func TestHelpExitsSuccessfully(t *testing.T) {
	for _, arg := range []string{"-h", "--help", "help"} {
		t.Run(arg, func(t *testing.T) {
			c, _, errOut := newTestCLI("")

			// Yardım isteyen kullanıcı hata yapmıyor; çıkış kodu 0 olmalı.
			if code := c.run(context.Background(), []string{arg}); code != exitOK {
				t.Errorf("çıkış kodu = %d, beklenen %d", code, exitOK)
			}
			if !strings.Contains(errOut.String(), "Komutlar:") {
				t.Error("komut listesi gösterilmedi")
			}
		})
	}
}

// TestHelpNeverTouchesTheServer, yardım metninin sunucuya bağlanmadığını
// doğrular.
//
// Bağlantı tembel olduğu için bu zaten böyle; test, ileride birinin
// yardım yolunda bir "sunucudan komut listesi al" fikri denemesini
// engellemek için var. `panely --help` çevrimdışı çalışmalı.
func TestHelpNeverTouchesTheServer(t *testing.T) {
	c, _, _ := newTestCLI("")

	done := make(chan int, 1)
	go func() { done <- c.run(context.Background(), []string{"--help"}) }()

	select {
	case code := <-done:
		if code != exitOK {
			t.Errorf("çıkış kodu = %d", code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("yardım metni bir şeyi beklemeye takıldı — sunucuya bağlanıyor olabilir")
	}
}

func TestUnknownCommandIsUsageError(t *testing.T) {
	c, _, errOut := newTestCLI("")

	if code := c.run(context.Background(), []string{"zart"}); code != exitUsage {
		t.Errorf("çıkış kodu = %d, beklenen %d", code, exitUsage)
	}
	if !strings.Contains(errOut.String(), `bilinmeyen komut "zart"`) {
		t.Errorf("komut adı hatada geçmiyor: %s", errOut.String())
	}
}

func TestVersionPrintsProtocol(t *testing.T) {
	c, out, _ := newTestCLI("")

	if code := c.run(context.Background(), []string{"version"}); code != exitOK {
		t.Fatalf("çıkış kodu = %d", code)
	}
	if !strings.Contains(out.String(), "protokol") {
		t.Errorf("protokol sürümü yazılmadı: %s", out.String())
	}
}

func TestAuditRequiresSubcommand(t *testing.T) {
	c, _, errOut := newTestCLI("")

	if code := c.run(context.Background(), []string{"audit"}); code != exitUsage {
		t.Errorf("çıkış kodu = %d, beklenen %d", code, exitUsage)
	}
	if !strings.Contains(errOut.String(), "list veya verify") {
		t.Errorf("alt komutlar önerilmedi: %s", errOut.String())
	}
}

func TestBootstrapReportsNotImplemented(t *testing.T) {
	c, _, errOut := newTestCLI("")

	// "Bilinmeyen komut" DEĞİL: komut planlı, sadece henüz yok.
	// Bu ayrım, kullanıcının komutu yanlış yazdığını sanmasını engelliyor.
	code := c.run(context.Background(), []string{"bootstrap", "root@1.2.3.4"})
	if code != exitError {
		t.Errorf("çıkış kodu = %d, beklenen %d", code, exitError)
	}
	msg := errOut.String()
	if strings.Contains(msg, "bilinmeyen komut") {
		t.Error("bootstrap bilinmeyen komut olarak raporlandı")
	}
	if !strings.Contains(msg, "henüz uygulanmadı") {
		t.Errorf("durum açıklanmadı: %s", msg)
	}
}

// TestVerifyExitCodeSeparatesTamperFromUnreachable, doğrulama sonucunun
// çıkış koduna doğru çevrildiğini sınar.
//
// Bu eşleme betiklerin ve cron'un gördüğü tek arayüz. "Doğrulanamadı" ile
// "kırık" aynı koda düşerse ya executor'ın kapalı olduğu her an sahte
// alarm üretilir, ya da gerçek bir kurcalama gürültüde kaybolur.
func TestVerifyExitCodeSeparatesTamperFromUnreachable(t *testing.T) {
	const (
		valid       = panelyv1.ChainStatus_CHAIN_STATUS_VALID
		invalid     = panelyv1.ChainStatus_CHAIN_STATUS_INVALID
		unreachable = panelyv1.ChainStatus_CHAIN_STATUS_UNREACHABLE
		unspecified = panelyv1.ChainStatus_CHAIN_STATUS_UNSPECIFIED
	)

	tests := []struct {
		name     string
		daemon   panelyv1.ChainStatus
		executor panelyv1.ChainStatus
		want     int
	}{
		{"iki zincir de geçerli", valid, valid, exitOK},
		{"daemon kırık", invalid, valid, exitChainInvalid},
		{"executor kırık", valid, invalid, exitChainInvalid},
		{"ikisi de kırık", invalid, invalid, exitChainInvalid},
		{"executor erişilemiyor", valid, unreachable, exitError},
		{"daemon erişilemiyor", unreachable, valid, exitError},
		// Kurcalama, erişilememeyi bastırır: gerçek bulgu öncelikli.
		{"biri kırık biri erişilemiyor", invalid, unreachable, exitChainInvalid},
		{"belirsiz durum başarı sayılmaz", unspecified, valid, exitError},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := &panelyv1.VerifyAuditChainResponse{
				DaemonStatus:   tc.daemon,
				ExecutorStatus: tc.executor,
			}
			if got := verifyExitCode(resp); got != tc.want {
				t.Errorf("çıkış kodu = %d, beklenen %d", got, tc.want)
			}
		})
	}
}

// TestPrintVerifyResultShowsBothChainsSeparately, iki zincirin ayrı ayrı
// raporlandığını doğrular.
//
// Tek bir "geçerli" satırında birleştirmek, modelin tamamının dayandığı
// ayrımı gizlerdi: ele geçirilmiş bir panelyd kendi zincirini temiz
// tutabilir ama executor'ınkine dokunamaz.
func TestPrintVerifyResultShowsBothChainsSeparately(t *testing.T) {
	c, out, _ := newTestCLI("")

	c.printVerifyResult("unix:/run/panely/api.sock", &panelyv1.VerifyAuditChainResponse{
		DaemonStatus:           panelyv1.ChainStatus_CHAIN_STATUS_VALID,
		RecordsChecked:         12,
		Detail:                 "daemon zinciri geçerli",
		ExecutorStatus:         panelyv1.ChainStatus_CHAIN_STATUS_UNREACHABLE,
		ExecutorDetail:         "executor günlüğü okunamadı",
		ExecutorRecordsChecked: 0,
	})

	text := out.String()
	for _, want := range []string{"daemon zinciri", "executor zinciri", "GEÇERLİ", "DOĞRULANAMADI"} {
		if !strings.Contains(text, want) {
			t.Errorf("çıktıda %q yok:\n%s", want, text)
		}
	}
	// Erişilemeyen zincir "GEÇERSİZ" diye gösterilmemeli.
	if strings.Contains(text, "GEÇERSİZ") {
		t.Errorf("erişilemeyen zincir geçersiz olarak gösterildi:\n%s", text)
	}
}

func TestPrintVerifyResultWarnsOnBrokenChain(t *testing.T) {
	c, _, errOut := newTestCLI("")

	c.printVerifyResult("unix:/x.sock", &panelyv1.VerifyAuditChainResponse{
		DaemonStatus:    panelyv1.ChainStatus_CHAIN_STATUS_INVALID,
		RecordsChecked:  41,
		FirstInvalidSeq: 42,
		Detail:          "kayıt 42 kurcalanmış",
		ExecutorStatus:  panelyv1.ChainStatus_CHAIN_STATUS_VALID,
	})

	msg := errOut.String()
	if !strings.Contains(msg, "#42") {
		t.Errorf("kopma noktası bildirilmedi: %s", msg)
	}
	if !strings.Contains(msg, "kurcalama") {
		t.Errorf("bulgunun anlamı açıklanmadı: %s", msg)
	}
}

// TestDaemonUserCellFlagsRoot, panelyd'nin root çalıştığının sessiz
// geçilmediğini doğrular.
//
// panelyd zaten root ile başlamayı reddediyor; bu satır o kontrolün
// yedeği. İkisinin birden atlandığı bir kurulumda ürünün merkezî iddiası
// çökmüş olur ve durum ekranı bunu göstermek zorunda.
func TestDaemonUserCellFlagsRoot(t *testing.T) {
	cell := daemonUserCell("root")
	if !strings.Contains(cell, "KURULUM BOZUK") {
		t.Errorf("root sessizce gösterildi: %q", cell)
	}

	if got := daemonUserCell("panely"); got != "panely" {
		t.Errorf("normal kullanıcı = %q, beklenen \"panely\"", got)
	}
}

func TestExecutorCellReportsUnreachable(t *testing.T) {
	cell := executorCell(&panelyv1.GetSystemInfoResponse{ExecutorReachable: false})
	if !strings.Contains(cell, "ERİŞİLEMİYOR") {
		t.Errorf("erişilemeyen executor belirtilmedi: %q", cell)
	}

	cell = executorCell(&panelyv1.GetSystemInfoResponse{
		ExecutorReachable: true,
		ExecutorVersion:   "v0.1.0",
	})
	if !strings.Contains(cell, "v0.1.0") {
		t.Errorf("executor sürümü gösterilmedi: %q", cell)
	}
}
