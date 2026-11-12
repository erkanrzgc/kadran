package main

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// unitSettings, birimdeki yorum dışı `Ad=değer` satırları.
func unitSettings(t *testing.T, name string) map[string][]string {
	t.Helper()
	f, err := os.Open(filepath.Join("..", "..", "deploy", "systemd", name))
	if err != nil {
		t.Fatalf("birim okunamadı: %v", err)
	}
	defer func() { _ = f.Close() }()
	out := map[string][]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "#") {
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok {
			out[strings.TrimSpace(k)] = append(out[strings.TrimSpace(k)], strings.TrimSpace(v))
		}
	}
	return out
}

// kadrand hostun sayaçlarını /proc'tan okuyor (K-166): ProcSubset=pid
// /proc/stat'ı, meminfo'yu ve net/dev'i gizler; bellek bu yüzden yıllarca
// 0 raporlandı. Başka kullanıcıların süreçleri yine görünmez kalmalı.
func TestDaemonUnitSeesHostCounters(t *testing.T) {
	d := unitSettings(t, "kadrand.service")
	if v, ok := d["ProcSubset"]; ok {
		t.Fatalf("kadrand.service ProcSubset=%v — host sayaçları okunamaz", v)
	}
	if v := d["ProtectProc"]; len(v) != 1 || v[0] != "invisible" {
		t.Fatalf("kadrand.service ProtectProc=%v, invisible kalmalı", v)
	}
}

// Ayrıcalıklı executor'ın /proc'a ihtiyacı yok; daraltma onda kalır.
func TestExecutorUnitKeepsProcSubset(t *testing.T) {
	e := unitSettings(t, "kadran-exec.service")
	if v := e["ProcSubset"]; len(v) != 1 || v[0] != "pid" {
		t.Fatalf("kadran-exec.service ProcSubset=%v, pid kalmalı", v)
	}
}
