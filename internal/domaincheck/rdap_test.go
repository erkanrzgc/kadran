package domaincheck

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// rdapServer: önyükleme dosyası "com" uzantısını kendine yönlendiriyor;
// domain/<ad> cevabı registrar varlığını taşıyor.
func rdapServer(t *testing.T, domainBody string) (*httptest.Server, string) {
	t.Helper()
	mux := http.NewServeMux()
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)
	mux.HandleFunc("/bootstrap.json", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"services":[[["net","com"],["%s/rdap/"]],[["dev"],["https://elsewhere.example/"]]]}`, srv.URL)
	})
	mux.HandleFunc("/rdap/domain/example.com", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(domainBody))
	})
	return srv, srv.URL + "/bootstrap.json"
}

func rdapTestClient(srv *httptest.Server) *http.Client {
	hc := srv.Client()
	hc.CheckRedirect = RDAPClient().CheckRedirect
	return hc
}

const registrarBody = `{"entities":[
 {"roles":["registrant"],"vcardArray":["vcard",[["fn",{},"text","Someone"]]]},
 {"roles":["registrar"],"vcardArray":["vcard",[["version",{},"text","4.0"],["fn",{},"text","Name.com, Inc."]]]}]}`

func TestLookupRegistrar(t *testing.T) {
	srv, boot := rdapServer(t, registrarBody)
	got, err := LookupRegistrar(t.Context(), rdapTestClient(srv), boot, "app.example.com")
	if err != nil || got != "Name.com, Inc." {
		t.Fatalf("LookupRegistrar = %q, %v", got, err)
	}
}

func TestLookupRegistrarWithoutRDAPForTheTLD(t *testing.T) {
	srv, boot := rdapServer(t, registrarBody)
	got, err := LookupRegistrar(t.Context(), rdapTestClient(srv), boot, "example.com.tr")
	if err != nil || got != "" {
		t.Fatalf(".tr için satır beklenmiyordu: %q, %v", got, err)
	}
}

func TestLookupRegistrarWithoutRegistrarEntity(t *testing.T) {
	srv, boot := rdapServer(t, `{"entities":[{"roles":["registrant"]}]}`)
	got, err := LookupRegistrar(t.Context(), rdapTestClient(srv), boot, "example.com")
	if err != nil || got != "" {
		t.Fatalf("registrar varlığı yokken (%q, %v) döndü; beklenen boş ad ve nil hata", got, err)
	}
}

func TestLookupRegistrarCleansTheName(t *testing.T) {
	srv, boot := rdapServer(t, `{"entities":[{"roles":["registrar"],"vcardArray":["vcard",[["fn",{},"text","Evil\u001b[31m Registrar"]]]}]}`)
	got, err := LookupRegistrar(t.Context(), rdapTestClient(srv), boot, "example.com")
	if err != nil || strings.ContainsRune(got, 0x1b) || got != "Evil[31m Registrar" {
		t.Fatalf("kaçış dizisi temizlenmedi: %q", got)
	}
}

// Yönlendirme düz http üzerindeki ÇALIŞAN bir önyükleme dosyasına gidiyor:
// izlenseydi sorgu başarılı olurdu. Hata yalnız yönlendirme reddedildiği için
// gelir; ağ hatası bu testi yanlışlıkla geçiremez (mutant "https olmayan
// yönlendirme izleniyor" böyle yakalanıyor).
func TestLookupRegistrarRefusesPlainHTTPRedirect(t *testing.T) {
	tlsSrv, _ := rdapServer(t, registrarBody)
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"services":[[["com"],["%s/rdap/"]]]}`, tlsSrv.URL)
	}))
	t.Cleanup(plain.Close)
	mux := http.NewServeMux()
	redirector := httptest.NewTLSServer(mux)
	t.Cleanup(redirector.Close)
	mux.HandleFunc("/bootstrap.json", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, plain.URL+"/boot.json", http.StatusFound)
	})
	// Bütün httptest TLS sunucuları aynı test sertifikasını kullanıyor:
	// bir sunucunun istemcisi ötekine de güvenir.
	got, err := LookupRegistrar(t.Context(), rdapTestClient(redirector), redirector.URL+"/bootstrap.json", "example.com")
	if err == nil {
		t.Fatalf("https olmayan yönlendirme izlendi (cevap %q)", got)
	}
}

// Sınır 1 MiB: ~1,5 MiB cevap reddedilir, 1 MiB altındaki kabul edilir.
func TestLookupRegistrarLimitsTheBody(t *testing.T) {
	const head = `{"entities":[{"roles":["registrar"],"vcardArray":["vcard",[["fn",{},"text","Name.com, Inc."]]]}],"pad":"`
	const tail = `"}`
	body := func(total int) string {
		return head + strings.Repeat("x", total-len(head)-len(tail)) + tail
	}

	srv, boot := rdapServer(t, body(1<<20-1024))
	if got, err := LookupRegistrar(t.Context(), rdapTestClient(srv), boot, "example.com"); err != nil || got != "Name.com, Inc." {
		t.Fatalf("1 MiB altındaki cevap okunamadı: %q, %v", got, err)
	}

	srv, boot = rdapServer(t, body(3<<19)) // 1,5 MiB
	if got, err := LookupRegistrar(t.Context(), rdapTestClient(srv), boot, "example.com"); err == nil || got != "" {
		t.Fatalf("1,5 MiB cevap okundu: %q, %v", got, err)
	}
}

// Önyükleme dosyası yalnız düz http taban adresi veriyorsa o adrese HİÇ
// istek gitmez: sonuç "" ve nil.
func TestLookupRegistrarIgnoresPlainHTTPBaseURL(t *testing.T) {
	hits := 0
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		_, _ = w.Write([]byte(registrarBody))
	}))
	t.Cleanup(plain.Close)
	boot := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"services":[[["com"],["%s/"]]]}`, plain.URL)
	}))
	t.Cleanup(boot.Close)

	got, err := LookupRegistrar(t.Context(), rdapTestClient(boot), boot.URL, "example.com")

	if err != nil || got != "" {
		t.Fatalf("düz http taban adresi için (%q, %v) döndü", got, err)
	}
	if hits != 0 {
		t.Fatalf("düz http sunucusuna %d istek gitti", hits)
	}
}

// getJSON da https olmayan adresi reddeder (önyükleme adresi düz http).
func TestLookupRegistrarRefusesPlainHTTPBootstrap(t *testing.T) {
	hits := 0
	plain := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits++ }))
	t.Cleanup(plain.Close)

	if _, err := LookupRegistrar(t.Context(), plain.Client(), plain.URL, "example.com"); err == nil {
		t.Fatal("düz http önyükleme adresi kabul edildi")
	}
	if hits != 0 {
		t.Fatalf("düz http sunucusuna %d istek gitti", hits)
	}
}

// cleanText: kontrol/biçim karakterleri atılır, uzunluk 100 rune ile sınırlı.
func TestLookupRegistrarCleansAndTruncatesLongNames(t *testing.T) {
	name := `A‮B\u009bC` + strings.Repeat("d", 300)
	srv, boot := rdapServer(t, `{"entities":[{"roles":["registrar"],"vcardArray":["vcard",[["fn",{},"text","`+name+`"]]]}]}`)

	got, err := LookupRegistrar(t.Context(), rdapTestClient(srv), boot, "example.com")

	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsRune(got, 0x202e) || strings.ContainsRune(got, 0x9b) {
		t.Errorf("kontrol/biçim karakteri kaldı: %q", got)
	}
	if n := len([]rune(got)); n == 0 || n > maxRegistrarLen {
		t.Errorf("uzunluk %d, beklenen 1..%d", n, maxRegistrarLen)
	}
	if !strings.HasPrefix(got, "ABC") {
		t.Errorf("temiz harfler korunmadı: %q", got)
	}
}
