package domaincheck

import (
	"os"
	"strings"
	"testing"
)

// Rehber, koddaki her firmayı adıyla anmalı: firma eklenip rehber
// unutulursa kullanıcı "Guide" bağlantısını görür ama rehberde bulamaz.
func TestDomainsGuideListsEveryProvider(t *testing.T) {
	b, err := os.ReadFile("../../docs/guides/domains.md")
	if err != nil {
		t.Fatalf("rehber okunamadı: %v", err)
	}
	doc := string(b)
	for _, p := range providers {
		if !strings.Contains(doc, p.Name) {
			t.Errorf("docs/guides/domains.md %q firmasını anmıyor", p.Name)
		}
	}
}
