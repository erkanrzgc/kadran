package vaultkey

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"filippo.io/age"
)

func TestInitCreatesKeyOnceAndPrintsRecipient(t *testing.T) {
	p := filepath.Join(t.TempDir(), "vault.key")
	var ilk bytes.Buffer
	if err := Init(p, &ilk); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	id, err := age.ParseX25519Identity(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatalf("üretilen anahtar X25519 değil: %v", err)
	}
	if got := strings.TrimSpace(ilk.String()); got != id.Recipient().String() {
		t.Errorf("basılan alıcı %q, anahtarınki %q", got, id.Recipient())
	}
	if strings.Contains(ilk.String(), "AGE-SECRET-KEY") {
		t.Fatal("özel anahtar çıktıya yazıldı")
	}
	if runtime.GOOS != "windows" {
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("anahtar izni %v, 0600 bekleniyordu", fi.Mode().Perm())
		}
	}

	// İkinci koşu anahtarı DEĞİŞTİRMEMELİ: değiştirseydi var olan bütün
	// mühürlü değerler çözülemez olurdu.
	var ikinci bytes.Buffer
	if err := Init(p, &ikinci); err != nil {
		t.Fatal(err)
	}
	sonra, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, sonra) {
		t.Fatal("ikinci koşu anahtarı değiştirdi")
	}
	if ikinci.String() != ilk.String() {
		t.Errorf("ikinci koşu başka alıcı bastı: %q", ikinci.String())
	}
}

// Bozuk bir anahtar dosyası varsa üstüne yenisi YAZILMAMALI: eski anahtarın
// yerine sessizce yenisi gelirse değerler çözülemez olur.
func TestInitDoesNotReplaceBrokenKey(t *testing.T) {
	p := filepath.Join(t.TempDir(), "vault.key")
	if err := os.WriteFile(p, []byte("bozuk\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Init(p, &bytes.Buffer{}); err == nil {
		t.Fatal("bozuk anahtar varken hata vermedi")
	}
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "bozuk\n" {
		t.Fatal("bozuk anahtarın üstüne yazıldı")
	}
}

// Başkalarına açık bir anahtar dosyası, executor'ın da reddedeceği bir
// dosya: kurulum onu kullanmadan önce durmalı.
func TestInitRejectsReadableKey(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("dosya kipleri Windows'ta anlamsız")
	}
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "vault.key")
	if err := os.WriteFile(p, []byte(id.String()+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Init(p, &bytes.Buffer{}); err == nil {
		t.Fatal("herkese açık anahtar kabul edildi")
	}
}

// Anahtar hata mesajında görünmemeli: kurulum çıktısı ekrana ve günlüğe
// gidiyor.
func TestInitErrorDoesNotLeakKey(t *testing.T) {
	p := filepath.Join(t.TempDir(), "vault.key")
	bozuk := "AGE-SECRET-KEY-1" + strings.Repeat("Q", 40) + "!"
	if err := os.WriteFile(p, []byte(bozuk), 0o600); err != nil {
		t.Fatal(err)
	}
	err := Init(p, &bytes.Buffer{})
	if err == nil {
		t.Fatal("bozuk anahtar kabul edildi")
	}
	if strings.Contains(err.Error(), strings.Repeat("Q", 10)) {
		t.Fatalf("hata anahtarı içeriyor: %v", err)
	}
}
