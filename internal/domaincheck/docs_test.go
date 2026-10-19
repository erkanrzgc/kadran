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
	rows := strings.Split(string(b), "\n")
	for _, p := range providers {
		row := tableRow(rows, p.Name)
		if row == "" {
			t.Errorf("docs/guides/domains.md tablosunda %q satırı yok", p.Name)
			continue
		}
		for _, suf := range p.Suffixes {
			if !strings.Contains(row, "`"+suf+"`") {
				t.Errorf("docs/guides/domains.md %q satırı %q son ekini anmıyor", p.Name, suf)
			}
		}
	}
}

// tableRow, "| <name> |" ile başlayan tablo satırı; yoksa "". Düz metin anmaları
// satır sayılmaz.
func tableRow(rows []string, name string) string {
	for _, r := range rows {
		if strings.HasPrefix(r, "| "+name+" |") {
			return r
		}
	}
	return ""
}
