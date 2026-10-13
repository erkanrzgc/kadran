package bootstrap

import (
	"os"
	"strings"
	"testing"
)

// Özel Git ağ drop-in'i root executor'a ağ çıkışı veriyor. Bulut meta veri
// servisi (GCP'de örnek kimlik belirteci) o çıkıştan erişilemez olmalı (B2/I7).
// Drop-in kodda kurulmuyor, yalnızca docs/guides/private-git.md'de belgeli;
// bu test belgedeki bloğun kısıtı taşıdığını sabitler.
func TestPrivateGitDropInDeniesMetadataAndLinkLocal(t *testing.T) {
	b, err := os.ReadFile("../../docs/guides/private-git.md")
	if err != nil {
		t.Fatal(err)
	}
	block := between(string(b), "RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6", "```")
	var deny string
	for line := range strings.Lines(block) {
		if strings.HasPrefix(line, "IPAddressDeny=") {
			deny = strings.TrimSpace(strings.TrimPrefix(line, "IPAddressDeny="))
		}
	}
	if deny == "" {
		t.Fatal("private Git drop-in has no IPAddressDeny=; the root executor could reach the cloud metadata service")
	}
	for _, want := range []string{"169.254.0.0/16", "fe80::/10"} {
		if !strings.Contains(deny, want) {
			t.Errorf("IPAddressDeny=%q does not cover %s", deny, want)
		}
	}
	// Loopback kapatılmamalı: systemd-resolved'ın 127.0.0.53 saplaması DNS'tir.
	if strings.Contains(deny, "127.") || strings.Contains(deny, "::1") {
		t.Errorf("IPAddressDeny=%q breaks the local DNS stub", deny)
	}
}
