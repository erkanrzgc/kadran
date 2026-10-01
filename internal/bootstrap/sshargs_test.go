package bootstrap

import (
	"slices"
	"testing"
)

// sshArgs güvenlik taşıyor ama hiç test edilmiyordu (K-114):
//   - BatchMode=yes: kurulum hiçbir zaman parola SORMAZ. "Parola veya özel
//     anahtar istenmez; kimlik doğrulamayı ssh yapar" iddiası buna
//     dayanıyor. Düşerse ssh terminalde parola ister — sırrı görmeme
//     ilkesi sessizce delinir.
//   - Sıra: hedef bütün seçeneklerden SONRA, uzak komuttan hemen ÖNCE.
//     ssh hedeften sonrasını uzak komut sayar; sıra bozulursa bir seçenek
//     uzak komuta, uzak komut hedefe karışır.

func TestSSHArgsNeverPromptForAPassword(t *testing.T) {
	args := sshArgs(Options{Host: "root@1.2.3.4"}, "uname -m")
	i := slices.Index(args, "BatchMode=yes")
	if i < 1 || args[i-1] != "-o" {
		t.Fatalf("BatchMode=yes seçenek olarak yok — ssh parola sorabilir: %q", args)
	}
	if !slices.Contains(args, "-T") {
		t.Errorf("-T yok (uzak komut için tty ayrılmamalı): %q", args)
	}
}

// Sessizce ölen bağlantı (RST'siz) ssh'ı sonsuza dek bekletirdi ve
// yeniden bağlanma hiç tetiklenmezdi; izleme uzun süre sessiz kalabiliyor
// (K-127).
func TestSSHArgsDetectSilentlyDeadConnections(t *testing.T) {
	args := sshArgs(Options{Host: "h"}, "c")
	for _, want := range []string{"ServerAliveInterval=15", "ServerAliveCountMax=4"} {
		i := slices.Index(args, want)
		if i < 1 || args[i-1] != "-o" {
			t.Errorf("%s seçenek olarak yok: %q", want, args)
		}
	}
}

func TestSSHArgsKeepHostLastBeforeTheRemoteCommand(t *testing.T) {
	for _, port := range []int{0, 2222} {
		args := sshArgs(Options{Host: "root@1.2.3.4", Port: port}, "uname -m")
		n := len(args)
		if n < 2 || args[n-2] != "root@1.2.3.4" || args[n-1] != "uname -m" {
			t.Fatalf("port=%d: hedef ve uzak komut sonda değil: %q", port, args)
		}
		for _, a := range args[:n-2] {
			if a == "root@1.2.3.4" {
				t.Errorf("port=%d: hedef seçeneklerin arasında da geçiyor: %q", port, args)
			}
		}
	}
}

func TestSSHArgsPortOnlyWhenGiven(t *testing.T) {
	if args := sshArgs(Options{Host: "h"}, "c"); slices.Contains(args, "-p") {
		t.Errorf("port verilmediği hâlde -p var: %q", args)
	}
	args := sshArgs(Options{Host: "h", Port: 2222}, "c")
	i := slices.Index(args, "-p")
	if i < 0 || i+1 >= len(args) || args[i+1] != "2222" {
		t.Errorf("-p 2222 yok: %q", args)
	}
}

// TestArchFromUname: yanlış mimariye binary göndermek "exec format error"
// ile, sebebi gözden kaçan bir kurulumdur; tanınmayan HER ŞEY hata.
func TestArchFromUname(t *testing.T) {
	for girdi, beklenen := range map[string]string{
		"x86_64\n": "amd64", "amd64": "amd64",
		"aarch64\n": "arm64", "arm64": "arm64",
	} {
		if got, err := archFromUname(girdi); err != nil || got != beklenen {
			t.Errorf("%q → %q, %v; beklenen %q", girdi, got, err, beklenen)
		}
	}
	for _, girdi := range []string{"armv7l", "i686", "", "x86_64 aarch64"} {
		if got, err := archFromUname(girdi); err == nil {
			t.Errorf("%q kabul edildi (%q) — desteklenmeyen mimari hata olmalı", girdi, got)
		}
	}
}
