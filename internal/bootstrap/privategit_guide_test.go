package bootstrap

import (
	"os"
	"strings"
	"testing"
)

// Özel depo rehberi (K-167) bir kurulum yönergesi: yanlış komut ya da eksik
// iptal adresi kullanıcıyı sessizce kötü yola sokar. Bu test rehberin taşıdığı
// üç sözü sabitler: bağlanma komutu, iki iptal adresi ve kendi uygulamasını
// kaydeden için kadran-git drop-in'i. Eski --git-credentials yolu bir yönerge
// olarak GERİ GELMEMELİ; kaldırılmasının anılması serbest.
func TestPrivateGitGuideDocumentsDeviceFlow(t *testing.T) {
	b, err := os.ReadFile("../../docs/guides/private-git.md")
	if err != nil {
		t.Fatal(err)
	}
	guide := string(b)

	for _, want := range []string{
		"kadran github connect",
		"https://github.com/settings/apps/authorizations",
		"https://github.com/settings/installations",
		"kadran-git.service",
		"--client-id",
		"--app-slug",
	} {
		if !strings.Contains(guide, want) {
			t.Errorf("guide does not mention %q", want)
		}
	}

	// Drop-in, ExecStart'ı önce boşaltıp sonra kurmalı; yoksa systemd ikinci
	// ExecStart'ı reddeder.
	dropIn := between(guide, "```ini", "```")
	if !strings.Contains(dropIn, "ExecStart=\nExecStart=/usr/local/lib/kadran/kadran-git ") {
		t.Errorf("kadran-git drop-in must clear ExecStart= before setting it:\n%s", dropIn)
	}

	// --git-credentials yalnız kaldırma bağlamında anılabilir: hiçbir ExecStart
	// satırında ve hiçbir ini bloğunda yer almamalı.
	for line := range strings.Lines(guide) {
		if strings.HasPrefix(strings.TrimSpace(line), "ExecStart=") && strings.Contains(line, "--git-credentials") {
			t.Errorf("guide tells the reader to add --git-credentials: %q", line)
		}
	}
}
