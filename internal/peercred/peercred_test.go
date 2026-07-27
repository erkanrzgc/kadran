package peercred

import (
	"errors"
	"testing"
)

func TestEmptyPolicyDeniesEverything(t *testing.T) {
	// Varsayılanın "her şeyi reddet" olması kasıtlıdır: yapılandırma
	// hatası sistemi açık değil kapalı bırakmalıdır.
	var p Policy

	if !p.IsEmpty() {
		t.Error("sıfır değerli Policy boş sayılmalı")
	}
	for _, c := range []Cred{
		{UID: 0, GID: 0},
		{UID: 1000, GID: 1000},
		{UID: 65534, GID: 65534},
	} {
		if p.Allows(c) {
			t.Errorf("boş politika %s kimliğini kabul etti", c)
		}
	}
}

func TestPolicyAllowsMatchingUID(t *testing.T) {
	p := Policy{AllowUIDs: []uint32{1001}}

	if !p.Allows(Cred{UID: 1001, GID: 50}) {
		t.Error("izinli uid reddedildi")
	}
	if p.Allows(Cred{UID: 1002, GID: 50}) {
		t.Error("izinsiz uid kabul edildi")
	}
}

func TestPolicyAllowsMatchingGID(t *testing.T) {
	p := Policy{AllowGIDs: []uint32{2002}}

	if !p.Allows(Cred{UID: 5000, GID: 2002}) {
		t.Error("izinli gid reddedildi")
	}
	if p.Allows(Cred{UID: 5000, GID: 2003}) {
		t.Error("izinsiz gid kabul edildi")
	}
}

// TestPolicyDoesNotAllowRootImplicitly, root'un otomatik geçmediğini
// doğrular. Bu önemli: executor'a bağlanabilecek tek kimlik panelyd'dir
// ve root'a örtük ayrıcalık tanımak modeli delerdi.
func TestPolicyDoesNotAllowRootImplicitly(t *testing.T) {
	p := Policy{AllowUIDs: []uint32{1001}}

	if p.Allows(Cred{UID: 0, GID: 0}) {
		t.Error("root örtük olarak kabul edildi")
	}
}

func TestTransportCredentialsRejectsEmptyPolicy(t *testing.T) {
	if _, err := TransportCredentials(Policy{}); err == nil {
		t.Fatal("boş politikayla taşıma kimlik bilgisi oluşturuldu")
	}
}

func TestTransportCredentialsAcceptsNonEmptyPolicy(t *testing.T) {
	tc, err := TransportCredentials(Policy{AllowUIDs: []uint32{1001}})
	if err != nil {
		t.Fatalf("taşıma kimlik bilgisi oluşturulamadı: %v", err)
	}
	if tc.Info().SecurityProtocol != "peercred" {
		t.Errorf("güvenlik protokolü = %q, beklenen \"peercred\"", tc.Info().SecurityProtocol)
	}
}

// TestCloneDoesNotShareSlices, Clone()'un derin kopya yaptığını doğrular.
// gRPC kimlik bilgilerini klonlar; dilim paylaşımı bir sunucunun
// politikasını değiştirmenin diğerini de etkilemesine yol açardı.
func TestCloneDoesNotShareSlices(t *testing.T) {
	original := Policy{AllowUIDs: []uint32{1001}, AllowGIDs: []uint32{2002}}
	tc, err := TransportCredentials(original)
	if err != nil {
		t.Fatalf("oluşturulamadı: %v", err)
	}

	clone, ok := tc.Clone().(*transportCreds)
	if !ok {
		t.Fatal("Clone beklenen tipi döndürmedi")
	}
	clone.policy.AllowUIDs[0] = 9999

	src, ok := tc.(*transportCreds)
	if !ok {
		t.Fatal("orijinal beklenen tipte değil")
	}
	if src.policy.AllowUIDs[0] != 1001 {
		t.Error("Clone dilimleri paylaşıyor — derin kopya değil")
	}
}

func TestClientHandshakeIsRefused(t *testing.T) {
	tc, err := TransportCredentials(Policy{AllowUIDs: []uint32{1001}})
	if err != nil {
		t.Fatalf("oluşturulamadı: %v", err)
	}
	if _, _, err := tc.ClientHandshake(t.Context(), "", nil); err == nil {
		t.Error("istemci tarafı el sıkışması reddedilmedi")
	}
}

func TestOverrideServerNameIsRefused(t *testing.T) {
	tc, err := TransportCredentials(Policy{AllowUIDs: []uint32{1001}})
	if err != nil {
		t.Fatalf("oluşturulamadı: %v", err)
	}
	if err := tc.OverrideServerName("herhangi"); err == nil {
		t.Error("sunucu adı geçersiz kılma reddedilmedi")
	}
}

func TestErrorsAreDistinguishable(t *testing.T) {
	// Çağıran tarafın hata sınıflarını ayırt edebilmesi gerekir:
	// "desteklenmeyen platform" ile "reddedildi" farklı olaylardır.
	if errors.Is(ErrDenied, ErrUnsupportedPlatform) {
		t.Error("ErrDenied ve ErrUnsupportedPlatform ayırt edilemiyor")
	}
	if errors.Is(ErrNotUnixConn, ErrDenied) {
		t.Error("ErrNotUnixConn ve ErrDenied ayırt edilemiyor")
	}
}
