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
	if got, _ := LookupRegistrar(t.Context(), rdapTestClient(srv), boot, "example.com"); got != "" {
		t.Fatalf("registrar varlığı yokken %q döndü", got)
	}
}

func TestLookupRegistrarCleansTheName(t *testing.T) {
	srv, boot := rdapServer(t, `{"entities":[{"roles":["registrar"],"vcardArray":["vcard",[["fn",{},"text","Evil\u001b[31m Registrar"]]]}]}`)
	got, _ := LookupRegistrar(t.Context(), rdapTestClient(srv), boot, "example.com")
	if strings.ContainsRune(got, 0x1b) || got != "Evil[31m Registrar" {
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

func TestLookupRegistrarLimitsTheBody(t *testing.T) {
	huge := `{"entities":[` + strings.Repeat(`{"roles":["x"]},`, 200000) + `{"roles":["registrar"]}]}`
	srv, boot := rdapServer(t, huge)
	if _, err := LookupRegistrar(t.Context(), rdapTestClient(srv), boot, "example.com"); err == nil {
		t.Fatal("1 MiB üstü cevap okundu")
	}
}
