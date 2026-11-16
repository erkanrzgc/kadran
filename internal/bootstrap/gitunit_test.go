package bootstrap

import (
	"strings"
	"testing"
)

// kadran-git, jeton tutan TEK süreçtir ve ağa çıkar; ayrıcalıksız
// kalması ve yerel/özel adreslere gidememesi birim dosyasındaki iki satıra
// bağlı (K-167). İkisi de bir YORUMLA değil bir TESTLE korunur.

// User= düşerse birim root olarak başlar: jeton dosyası, soket politikası
// (admin soketi root'u reddeder) ve ağ çiti anlamsızlaşır.
func TestGitBrokerRunsAsUnprivilegedUser(t *testing.T) {
	users := directive(readUnit(t, "kadran-git.service"), "User")
	if len(users) != 1 || users[0] != "kadran-git" {
		t.Fatalf("User= = %q, want exactly kadran-git (the broker must not run as root)", users)
	}
}

// IPAddressDeny düşerse GitHub'a gitmesi gereken süreç bulut metadata
// servisine (169.254.169.254, fd20:ce::254), Docker köprülerine ve loopback'e
// de gidebilir.
func TestGitBrokerDeniesLocalAndMetadataAddresses(t *testing.T) {
	deny := strings.Join(directive(readUnit(t, "kadran-git.service"), "IPAddressDeny"), " ")
	if deny == "" {
		t.Fatal("IPAddressDeny= is missing: the broker could reach the metadata service and private networks")
	}
	for _, want := range []string{"link-local", "localhost", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "100.64.0.0/10", "fc00::/7"} {
		if !strings.Contains(deny, want) {
			t.Errorf("IPAddressDeny=%q does not cover %s", deny, want)
		}
	}
	allow := strings.Join(directive(readUnit(t, "kadran-git.service"), "IPAddressAllow"), " ")
	if allow != "127.0.0.53" {
		t.Errorf("IPAddressAllow=%q, want only the local resolver stub 127.0.0.53", allow)
	}
}
