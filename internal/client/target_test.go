package client

import "testing"

func TestParseTargetLocalForms(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"boş → varsayılan soket", "", DefaultSocketPath},
		{"mutlak yol", "/run/panely/api.sock", "/run/panely/api.sock"},
		{"unix şeması", "unix:///tmp/x.sock", "/tmp/x.sock"},
		{"boşluklu girdi", "  /run/panely/api.sock  ", "/run/panely/api.sock"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseTarget(tc.input)
			if err != nil {
				t.Fatalf("çözümleme başarısız: %v", err)
			}
			if !got.IsLocal() {
				t.Fatal("yerel hedef bekleniyordu")
			}
			if got.SocketPath != tc.want {
				t.Errorf("soket yolu = %q, beklenen %q", got.SocketPath, tc.want)
			}
		})
	}
}

func TestParseTargetSSHForms(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantUser string
		wantHost string
		wantPort int
	}{
		{"kullanıcı@sunucu", "erkan@1.2.3.4", "erkan", "1.2.3.4", 0},
		{"özel port", "erkan@example.com:2222", "erkan", "example.com", 2222},
		{"varsayılan kullanıcı", "example.com", DefaultSSHUser, "example.com", 0},
		{"varsayılan kullanıcı + port", "example.com:2222", DefaultSSHUser, "example.com", 2222},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseTarget(tc.input)
			if err != nil {
				t.Fatalf("çözümleme başarısız: %v", err)
			}
			if got.IsLocal() {
				t.Fatal("SSH hedefi bekleniyordu")
			}
			if got.SSHUser != tc.wantUser {
				t.Errorf("kullanıcı = %q, beklenen %q", got.SSHUser, tc.wantUser)
			}
			if got.SSHHost != tc.wantHost {
				t.Errorf("sunucu = %q, beklenen %q", got.SSHHost, tc.wantHost)
			}
			if got.SSHPort != tc.wantPort {
				t.Errorf("port = %d, beklenen %d", got.SSHPort, tc.wantPort)
			}
		})
	}
}

// TestParseTargetIPv6, IPv6 adreslerindeki iki nokta üst üstenin port
// sanılmadığını doğrular.
//
// Naif bir "ilk iki noktadan böl" yaklaşımı 2001:db8::1'i host=2001,
// port=db8::1 diye ayırırdı. Bu test o hatayı yakaladı.
func TestParseTargetIPv6(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantHost string
		wantPort int
	}{
		{"çıplak IPv6", "erkan@2001:db8::1", "2001:db8::1", 0},
		{"çıplak IPv6, varsayılan kullanıcı", "2001:db8::1", "2001:db8::1", 0},
		{"parantezli IPv6", "erkan@[2001:db8::1]", "2001:db8::1", 0},
		{"parantezli IPv6 + port", "erkan@[2001:db8::1]:2222", "2001:db8::1", 2222},
		{"loopback", "erkan@::1", "::1", 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseTarget(tc.input)
			if err != nil {
				t.Fatalf("çözümleme başarısız: %v", err)
			}
			if got.SSHHost != tc.wantHost {
				t.Errorf("sunucu = %q, beklenen %q", got.SSHHost, tc.wantHost)
			}
			if got.SSHPort != tc.wantPort {
				t.Errorf("port = %d, beklenen %d", got.SSHPort, tc.wantPort)
			}
		})
	}
}

// TestIPv6WithPortRoundTrips, port varken üretilen dizenin geri
// okunabildiğini doğrular. Köşeli parantez konmazsa String() çıktısı
// ParseTarget tarafından farklı yorumlanırdı.
func TestIPv6WithPortRoundTrips(t *testing.T) {
	original := Target{SSHUser: "erkan", SSHHost: "2001:db8::1", SSHPort: 2222}

	reparsed, err := ParseTarget(original.String())
	if err != nil {
		t.Fatalf("String() çıktısı geri okunamadı (%q): %v", original.String(), err)
	}
	if reparsed.SSHHost != original.SSHHost || reparsed.SSHPort != original.SSHPort {
		t.Errorf("gidiş-dönüşte değişti: %+v → %+v", original, reparsed)
	}
}

func TestParseTargetRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"boş kullanıcı", "@sunucu"},
		{"boş sunucu", "erkan@"},
		{"port sayı değil", "erkan@sunucu:abc"},
		{"port sıfır", "erkan@sunucu:0"},
		{"port aralık dışı", "erkan@sunucu:99999"},
		{"boş unix yolu", "unix://"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseTarget(tc.input); err == nil {
				t.Fatalf("geçersiz girdi kabul edildi: %q", tc.input)
			}
		})
	}
}

func TestTargetStringRoundTrips(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"erkan@1.2.3.4", "erkan@1.2.3.4"},
		{"erkan@example.com:2222", "erkan@example.com:2222"},
		{"erkan@example.com:22", "erkan@example.com"},
		{"/run/panely/api.sock", "unix:/run/panely/api.sock"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			target, err := ParseTarget(tc.input)
			if err != nil {
				t.Fatalf("çözümleme başarısız: %v", err)
			}
			if got := target.String(); got != tc.want {
				t.Errorf("String() = %q, beklenen %q", got, tc.want)
			}
		})
	}
}

func TestDialLocalTargetDoesNotConnectEagerly(t *testing.T) {
	// Bağlantı tembel olmalı: var olmayan bir sokete Dial hata vermemeli.
	// Bu sayede `panely --help` gibi komutlar sunucuya hiç dokunmaz.
	c, err := Dial(Target{SocketPath: "/olmayan/soket.sock"})
	if err != nil {
		t.Fatalf("tembel bağlantı hata verdi: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Errorf("kapatılamadı: %v", err)
	}
}

func TestDialRejectsEmptyTarget(t *testing.T) {
	if _, err := Dial(Target{}); err == nil {
		t.Fatal("boş hedefle bağlantı kuruldu")
	}
}
