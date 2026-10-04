// Package vaulttest, testlerin mühürlü değerleri açmasına yarar. Üretim
// kodu bu paketi içe AKTARMAMALI: daemon'da çözme kodu olmamalı.
package vaulttest

import (
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"strings"
	"testing"

	"filippo.io/age"

	"github.com/erkanrzgc/kadran/internal/vault"
)

// NewIdentity, teste özel bir X25519 anahtarı üretir.
func NewIdentity(t testing.TB) *age.X25519Identity {
	t.Helper()
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("anahtar üretilemedi: %v", err)
	}
	return id
}

// Sealer, kimliğin alıcısına mühürleyen bir Sealer döndürür.
func Sealer(t testing.TB, id *age.X25519Identity) *vault.Sealer {
	t.Helper()
	s, err := vault.NewSealer(id.Recipient().String())
	if err != nil {
		t.Fatalf("sealer kurulamadı: %v", err)
	}
	return s
}

// Open, mühürlü değeri açar ve bağı ayrıştırır; açılamazsa testi düşürür.
func Open(t testing.TB, id age.Identity, sealed string) (appID, key, value string) {
	t.Helper()
	appID, key, value, err := TryOpen(id, sealed)
	if err != nil {
		t.Fatalf("mühürlü değer açılamadı: %v", err)
	}
	return appID, key, value
}

// TryOpen, Open'ın hata döndüren hâli.
func TryOpen(id age.Identity, sealed string) (appID, key, value string, err error) {
	b64, ok := strings.CutPrefix(sealed, vault.Prefix)
	if !ok {
		return "", "", "", errors.New("önek yok")
	}
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return "", "", "", err
	}
	r, err := age.Decrypt(bytes.NewReader(raw), id)
	if err != nil {
		return "", "", "", err
	}
	plain, err := io.ReadAll(r)
	if err != nil {
		return "", "", "", err
	}
	parts := strings.SplitN(string(plain), "\x00", 3)
	if len(parts) != 3 {
		return "", "", "", errors.New("bağ biçimi bozuk")
	}
	return parts[0], parts[1], parts[2], nil
}
