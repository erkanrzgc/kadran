package main

import (
	"strings"
	"testing"
	"time"

	panelyv1 "github.com/erkanrzgc/panely/internal/pb/panely/v1"
)

func TestHumanBytes(t *testing.T) {
	tests := []struct {
		in   uint64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KiB"},
		{1536, "1.5 KiB"},
		{1024 * 1024, "1.0 MiB"},
		{4080218931, "3.8 GiB"},
	}

	for _, tc := range tests {
		if got := humanBytes(tc.in); got != tc.want {
			t.Errorf("humanBytes(%d) = %q, beklenen %q", tc.in, got, tc.want)
		}
	}
}

func TestHumanDuration(t *testing.T) {
	tests := []struct {
		in   time.Duration
		want string
	}{
		{0, "0sn"},
		{500 * time.Millisecond, "0sn"},
		{45 * time.Second, "45sn"},
		{90 * time.Second, "1dk 30sn"},
		{2 * time.Hour, "2sa"},
		{2*time.Hour + 15*time.Minute, "2sa 15dk"},
		{50 * time.Hour, "2g 2sa"},
		// En fazla iki birim: "3g 4sa 17dk 3sn" okunmaz.
		{50*time.Hour + 17*time.Minute + 3*time.Second, "2g 2sa"},
	}

	for _, tc := range tests {
		if got := humanDuration(tc.in); got != tc.want {
			t.Errorf("humanDuration(%v) = %q, beklenen %q", tc.in, got, tc.want)
		}
	}
}

// TestShortFingerprintKeepsBothEnds, kısaltmanın parmak izinin iki ucunu
// da koruduğunu doğrular.
//
// Yalnızca baştan kesmek yanıltıcı olurdu: aynı önekle başlayan iki
// anahtar ekranda aynı görünürdü.
func TestShortFingerprintKeepsBothEnds(t *testing.T) {
	const full = "SHA256:AbCdEfGhIjKlMnOpQrStUvWxYz0123456789+/ab"

	short := shortFingerprint(full)
	if len(short) >= len(full) {
		t.Errorf("kısaltılmadı: %q", short)
	}
	if !strings.HasPrefix(short, "SHA256:AbCdEfGh") {
		t.Errorf("baş kısım korunmadı: %q", short)
	}
	if !strings.HasSuffix(short, "89+/ab") {
		t.Errorf("son kısım korunmadı: %q", short)
	}
}

func TestShortFingerprintLeavesShortValuesAlone(t *testing.T) {
	for _, in := range []string{"", "SHA256:kisa", "elle-yazilmis"} {
		if got := shortFingerprint(in); got != in {
			t.Errorf("shortFingerprint(%q) = %q, değiştirilmemeliydi", in, got)
		}
	}
}

// TestDescribeActorFallsBackHonestly, aktör bilgisi eksikken uydurma bir
// değer üretilmediğini doğrular.
//
// Denetim izinde boş bir alan "bilinmiyor" demektir ve dürüst bir
// kayıttır; yer tutucu bir kimlik, sonradan bakan birini gerçek bir
// aktör gördüğüne inandırırdı.
func TestDescribeActorFallsBackHonestly(t *testing.T) {
	tests := []struct {
		name  string
		actor *panelyv1.Actor
		want  string
	}{
		{"aktör yok", nil, "bilinmiyor"},
		{"boş aktör", &panelyv1.Actor{}, "bilinmiyor"},
		{"yalnızca köken", &panelyv1.Actor{Origin: "local"}, "local"},
		{"etiket kökene tercih edilir",
			&panelyv1.Actor{Origin: "ssh", Label: "erkan-dizustu"}, "erkan-dizustu"},
		{"parmak izi her şeye tercih edilir",
			&panelyv1.Actor{Origin: "ssh", Label: "x", SshKeyFingerprint: "SHA256:kisa"},
			"SHA256:kisa"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := describeActor(tc.actor); got != tc.want {
				t.Errorf("describeActor() = %q, beklenen %q", got, tc.want)
			}
		})
	}
}

// TestChainStatusLabelsAreDistinct, üç durumun ekranda birbirine
// karışmadığını doğrular.
func TestChainStatusLabelsAreDistinct(t *testing.T) {
	valid := chainStatusLabel(panelyv1.ChainStatus_CHAIN_STATUS_VALID)
	invalid := chainStatusLabel(panelyv1.ChainStatus_CHAIN_STATUS_INVALID)
	unreachable := chainStatusLabel(panelyv1.ChainStatus_CHAIN_STATUS_UNREACHABLE)

	if valid == invalid || invalid == unreachable || valid == unreachable {
		t.Errorf("etiketler ayırt edilemiyor: %q / %q / %q", valid, invalid, unreachable)
	}
	// "Doğrulanamadı" kurcalama gibi okunmamalı.
	if strings.Contains(unreachable, "GEÇERSİZ") {
		t.Errorf("erişilemez durumu geçersiz gibi okunuyor: %q", unreachable)
	}
}

func TestOutcomeLabelMarksDenied(t *testing.T) {
	// REDDEDİLDİ, güvenlik modelinin devreye girdiği durumdur ve
	// başarısızlıktan ayrı görünmelidir.
	denied := outcomeLabel(panelyv1.AuditOutcome_AUDIT_OUTCOME_DENIED)
	failure := outcomeLabel(panelyv1.AuditOutcome_AUDIT_OUTCOME_FAILURE)

	if denied == failure {
		t.Errorf("reddedilme ile başarısızlık aynı görünüyor: %q", denied)
	}
	if !strings.Contains(denied, "RED") {
		t.Errorf("reddedilme etiketi beklenmedik: %q", denied)
	}
}
