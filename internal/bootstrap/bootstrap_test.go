package bootstrap

import (
	"archive/tar"
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestRejectsPrivateKey, kazara özel anahtar verilmesini yakalar.
//
// Bu kontrolün olmaması felaket olurdu: özel anahtar sunucuya yüklenir ve
// authorized_keys'e yazılırdı. Panely'nin tüm güvenlik modeli anahtar
// malzemesinin iş istasyonundan hiç çıkmamasına dayanıyor.
func TestRejectsPrivateKey(t *testing.T) {
	privateKeys := []string{
		"-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1r\n-----END OPENSSH PRIVATE KEY-----",
		"-----BEGIN RSA PRIVATE KEY-----\nMIIEow\n-----END RSA PRIVATE KEY-----",
		"-----BEGIN EC PRIVATE KEY-----\nMHcCAQ\n-----END EC PRIVATE KEY-----",
	}

	for _, key := range privateKeys {
		err := validatePublicKey([]byte(key))
		if err == nil {
			t.Fatalf("özel anahtar kabul edildi:\n%s", key)
		}
		if !strings.Contains(err.Error(), "ÖZEL anahtar") {
			t.Errorf("hata özel anahtar olduğunu söylemiyor: %v", err)
		}
	}
}

func TestAcceptsPublicKeyTypes(t *testing.T) {
	valid := []string{
		"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExample erkan@dizustu",
		"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExample",
		"ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgQExample erkan@dizustu",
		"ecdsa-sha2-nistp256 AAAAE2VjZHNhLXNoYTItbmlzdHAyNTYExample x",
		"sk-ssh-ed25519@openssh.com AAAAGnNrLXNzaC1lExample yubikey",
	}

	for _, key := range valid {
		if err := validatePublicKey([]byte(key)); err != nil {
			t.Errorf("geçerli anahtar reddedildi (%q): %v", key, err)
		}
	}
}

func TestRejectsGarbage(t *testing.T) {
	invalid := []string{
		"",
		"   ",
		"merhaba",
		"ssh-ed25519",
		"bilinmeyen-tur AAAAB3Nz",
	}

	for _, key := range invalid {
		if err := validatePublicKey([]byte(key)); err == nil {
			t.Errorf("geçersiz girdi kabul edildi: %q", key)
		}
	}
}

// TestArchiveCarriesEverythingTheInstallerNeeds, paketin kurulum
// betiğinin beklediği her dosyayı taşıdığını doğrular.
//
// Eksik bir dosya uzakta, kurulumun ortasında fark edilirdi — yarım
// yapılandırılmış bir sunucu bırakarak.
func TestArchiveCarriesEverythingTheInstallerNeeds(t *testing.T) {
	repo := newFakeRepo(t)

	archive, err := buildArchive(Options{
		BinaryDir:     filepath.Join(repo, "bin"),
		RepoRoot:      repo,
		ClientKeyPath: filepath.Join(repo, "key.pub"),
	}, "arm64")
	if err != nil {
		t.Fatalf("paket üretilemedi: %v", err)
	}

	files := readArchive(t, archive)

	// install.sh bu adlarla okuyor; listeler ayrışırsa kurulum uzakta ölür.
	required := []string{
		"install.sh",
		"panelyd", "panely-exec", "panely-connect",
		"panelyd.service", "panely-exec.service", "panely-tmpfiles.conf",
		"client_key.pub",

		// Ters vekil. Dağıtımın `caddy` paketine bağlanılmadığı için
		// birim, soket, tmpfiles kuralı ve yapılandırma BURADAN gitmek
		// zorunda; biri eksikse kurulum uzakta yarıda kalır.
		"panely-caddy",
		"panely-caddy.service", "panely-caddy-admin.socket",
		"panely-caddy-tmpfiles.conf", "caddy.json",
	}
	for _, name := range required {
		if _, ok := files[name]; !ok {
			t.Errorf("pakette %q yok", name)
		}
	}

	// İş istasyonu aracı sunucuda işi olmayan bir binary: ayrıcalıklı
	// makinedeki yüzeyi gereksiz büyütür.
	if _, ok := files["panely"]; ok {
		t.Error("iş istasyonu aracı sunucu paketine girmiş")
	}
}

// TestArchiveUsesMatchingArchitecture, doğru mimarinin seçildiğini
// doğrular.
//
// Yanlış mimaride bir binary "exec format error" ile ölür ve neden
// günlükte kolayca gözden kaçar.
func TestArchiveUsesMatchingArchitecture(t *testing.T) {
	repo := newFakeRepo(t)

	for _, arch := range []string{"amd64", "arm64"} {
		archive, err := buildArchive(Options{
			BinaryDir:     filepath.Join(repo, "bin"),
			RepoRoot:      repo,
			ClientKeyPath: filepath.Join(repo, "key.pub"),
		}, arch)
		if err != nil {
			t.Fatalf("%s paketi üretilemedi: %v", arch, err)
		}

		files := readArchive(t, archive)
		want := "panelyd-" + arch
		if got := strings.TrimSpace(string(files["panelyd"])); got != want {
			t.Errorf("%s için yanlış binary: %q, beklenen %q", arch, got, want)
		}
	}
}

func TestArchiveFailsClearlyOnMissingBinary(t *testing.T) {
	repo := newFakeRepo(t)

	// arm64 var, amd64 yok.
	_, err := buildArchive(Options{
		BinaryDir:     filepath.Join(repo, "bin"),
		RepoRoot:      repo,
		ClientKeyPath: filepath.Join(repo, "key.pub"),
	}, "s390x")
	if err == nil {
		t.Fatal("olmayan mimari için paket üretildi")
	}
	// Hata ne yapılacağını söylemeli.
	if !strings.Contains(err.Error(), "build-release") {
		t.Errorf("hata çözümü göstermiyor: %v", err)
	}
}

// TestUnitFilesAreNormalizedToLF, satır sonlarının dönüştürüldüğünü
// doğrular.
//
// Geliştirme Windows'ta yapılıyor. CRLF taşıyan bir systemd birimi ya da
// kabuk betiği Linux'ta sessizce bozulur; hata mesajı da nedeni
// göstermez.
func TestUnitFilesAreNormalizedToLF(t *testing.T) {
	repo := newFakeRepo(t)

	// Birimi kasten CRLF ile yaz.
	unit := filepath.Join(repo, "deploy", "systemd", "panelyd.service")
	if err := os.WriteFile(unit, []byte("[Unit]\r\nDescription=Panely\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	archive, err := buildArchive(Options{
		BinaryDir:     filepath.Join(repo, "bin"),
		RepoRoot:      repo,
		ClientKeyPath: filepath.Join(repo, "key.pub"),
	}, "arm64")
	if err != nil {
		t.Fatalf("paket üretilemedi: %v", err)
	}

	files := readArchive(t, archive)
	if bytes.Contains(files["panelyd.service"], []byte("\r")) {
		t.Error("systemd birimi CRLF taşıyor — Linux'ta sessizce bozulur")
	}
}

// TestInstallScriptKeepsPrimaryGroupInvariant, kurulum betiğinin
// birincil grup değişmezini koruduğunu doğrular.
//
// `useradd -G panely-client` (ek grup) yazılırsa SO_PEERCRED grubu
// göremez ve HER bağlantı sessizce reddedilir — hata mesajı olmadan.
// Bu, projedeki en pahalı sessiz hata adayı.
func TestInstallScriptKeepsPrimaryGroupInvariant(t *testing.T) {
	script, err := installScript.ReadFile("install.sh")
	if err != nil {
		t.Fatal(err)
	}
	text := string(script)

	if !strings.Contains(text, "--gid panely-client") {
		t.Error("istemci kullanıcısı birincil grupla (--gid) oluşturulmuyor")
	}
	// -G / --groups ile panely-client vermek sessiz arızaya yol açar.
	if strings.Contains(text, "--groups panely-client") || strings.Contains(text, "-G panely-client") {
		t.Error("istemci kullanıcısına panely-client EK grup olarak verilmiş")
	}
	// Kurulum sonunda doğrulama yapmalı.
	if !strings.Contains(text, "id -gn panely-client") {
		t.Error("kurulum betiği birincil grubu doğrulamıyor")
	}
}

// TestInstallScriptForcesConnectCommand, SSH erişiminin zorlanmış
// komutla sınırlandığını doğrular.
func TestInstallScriptForcesConnectCommand(t *testing.T) {
	script, err := installScript.ReadFile("install.sh")
	if err != nil {
		t.Fatal(err)
	}
	text := string(script)

	if !strings.Contains(text, `command=\"$LIB_DIR/panely-connect\"`) {
		t.Error("authorized_keys satırı zorlanmış komut içermiyor")
	}
	if !strings.Contains(text, "restrict") {
		t.Error("authorized_keys satırında `restrict` yok")
	}
	// ExposeAuthInfo olmadan denetim kaydındaki parmak izi boş kalır.
	if !strings.Contains(text, "ExposeAuthInfo yes") {
		t.Error("sshd yapılandırmasında ExposeAuthInfo yok")
	}
	// sshd'yi doğrulamadan yeniden yüklemek bizi dışarıda bırakabilir.
	if !strings.Contains(text, `"$SSHD_BIN" -t`) {
		t.Error("sshd yapılandırması yeniden yüklemeden önce doğrulanmıyor")
	}
}

// TestSSHDDropInPinsUserEnvironment, denetim kimliğini taklit etmeye açan
// ayarın kapatıldığını doğrular.
//
// panely-connect aktörün parmak izini SSH_AUTH_INFO_0'dan okur.
// authorized_keys'teki `environment="..."` seçeneği sshd'nin kendi yazdığı
// değeri EZEBİLİR ("override other default environment values" — sshd(8)),
// yani açık kalırsa denetim izi taklit edilebilir. Kapatan ayar
// PermitUserEnvironment'tır; `restrict` DEĞİL (docs/decisions.md K-031).
func TestSSHDDropInPinsUserEnvironment(t *testing.T) {
	script, err := installScript.ReadFile("install.sh")
	if err != nil {
		t.Fatal(err)
	}
	dropIn := between(string(script), "cat > \"$SSHD_DROPIN\"", "SSHD\n")

	if !strings.Contains(dropIn, "PermitUserEnvironment no") {
		t.Error("sshd drop-in'inde `PermitUserEnvironment no` yok — " +
			"denetim kimliği authorized_keys üzerinden taklit edilebilir")
	}
}

// TestSSHDDropInKeywordsAreValidInTheirScope, drop-in'in sshd tarafından
// AYRIŞTIRILABİLİR olduğunu doğrular.
//
// # Bu test neden var?
//
// Somut bir hatayı yakaladı: PermitUserEnvironment `Match` bloğunun içine
// yazılmıştı. O anahtar kelime Match içinde geçerli DEĞİLDİR ve sshd
// yapılandırmayı tümden reddeder. install.sh `sshd -t` ile doğruladığı
// için sunucu kilitlenmezdi — ama bootstrap taze bir sunucuda ölürdü ve
// bunu ancak gerçek kurulumda görürdük.
//
// Match ÖNCESİ satırlar genel kapsamdadır; Match SONRASI satırlar yalnızca
// Match'in izin verdiği alt kümeden olabilir.
func TestSSHDDropInKeywordsAreValidInTheirScope(t *testing.T) {
	script, err := installScript.ReadFile("install.sh")
	if err != nil {
		t.Fatal(err)
	}
	dropIn := between(string(script), "cat > \"$SSHD_DROPIN\"", "SSHD\n")

	// sshd_config(5): Match içinde KULLANILAMAYAN, bu dosyada geçmesi
	// muhtemel anahtar kelimeler.
	invalidInMatch := []string{
		"PermitUserEnvironment",
		"AcceptEnv",
		"UsePAM",
		"ListenAddress",
	}

	inMatch := false
	for line := range strings.Lines(dropIn) {
		field := strings.Fields(strings.TrimSpace(line))
		if len(field) == 0 || strings.HasPrefix(field[0], "#") {
			continue
		}
		if strings.EqualFold(field[0], "Match") {
			inMatch = true
			continue
		}
		if !inMatch {
			continue
		}
		for _, bad := range invalidInMatch {
			if strings.EqualFold(field[0], bad) {
				t.Errorf("%q Match bloğunun içinde — sshd yapılandırmayı reddeder, "+
					"bootstrap taze sunucuda ölür", bad)
			}
		}
	}

	if !inMatch {
		t.Fatal("drop-in'de hiç Match bloğu bulunamadı — test bir şey ölçmüyor")
	}
}

// TestInstallScriptDoesNotUseNologinForClient, istemci kabuğunun
// nologin OLMADIĞINI doğrular.
//
// sshd zorlanmış komutu kullanıcının giriş kabuğu üzerinden çalıştırır;
// nologin onu reddeder ve taşıma tamamen çalışmaz. Hesabı kısıtlayan şey
// kabuk değil, `command=...,restrict` ikilisi.
func TestInstallScriptDoesNotUseNologinForClient(t *testing.T) {
	script, err := installScript.ReadFile("install.sh")
	if err != nil {
		t.Fatal(err)
	}

	clientBlock := between(string(script), "--gid panely-client", "panely-client")
	if strings.Contains(clientBlock, "NOLOGIN") {
		t.Error("istemci kullanıcısına nologin verilmiş — zorlanmış komut çalışmaz")
	}
}

// ── Hacim kökünün sertleştirilmesi ───────────────────────────────────

// volumeRoot, uygulama hacimlerinin altında toplandığı dizindir.
const volumeRoot = "/var/lib/panely/volumes"

func readVolumeMountUnit(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "deploy", "systemd",
		"var-lib-panely-volumes.mount"))
	if err != nil {
		t.Fatalf("hacim mount birimi okunamadı: %v", err)
	}
	return string(b)
}

// TestVolumeMountUnitCarriesHardeningFlags, birimin nodev ve nosuid
// taşıdığını doğrular.
//
// Bu bayraklar uygulama hacimlerindeki TEK savunmadır: sürücü konteyneri
// düz bir bind ile bağlıyor ve bind mount bayrakları kaynak mount'tan
// MİRAS ALIYOR. Buradan düşerlerse, kullanıcının yüklediği setuid-root bir
// dosya konteyner içinde gerçekten setuid çalışır.
func TestVolumeMountUnitCarriesHardeningFlags(t *testing.T) {
	unit := readVolumeMountUnit(t)

	var options string
	for line := range strings.Lines(unit) {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "Options="); ok {
			options = strings.TrimSpace(rest)
		}
	}
	if options == "" {
		t.Fatal("birimde Options= satırı yok — test bir şey ölçmüyor")
	}

	got := strings.Split(options, ",")
	for _, want := range []string{"bind", "nodev", "nosuid"} {
		if !slices.Contains(got, want) {
			t.Errorf("Options= içinde %q yok (var olan: %s)", want, options)
		}
	}
}

// TestVolumeMountUnitFileNameMatchesMountPoint, dosya adının systemd'nin
// kaçışlamasıyla birebir aynı olduğunu doğrular.
//
// systemd bir mount biriminin adını Where= değerinden TÜRETİR. Ad
// uyuşmazsa birim yüklenir, `systemctl status` onu gösterir, ama ASLA
// etkinleşmez — yani hacimler sessizce sertleştirilmeden kalır. Sessiz
// olduğu için bu testin var olması gerekiyor.
func TestVolumeMountUnitFileNameMatchesMountPoint(t *testing.T) {
	// systemd-escape -p --suffix=mount /var/lib/panely/volumes
	want := strings.TrimPrefix(volumeRoot, "/")
	want = strings.ReplaceAll(want, "/", "-") + ".mount"

	src, ok := unitFiles[want]
	if !ok {
		t.Fatalf("unitFiles içinde %q yok — systemd bu birimi Where= ile eşleştiremez", want)
	}
	if !strings.HasSuffix(src, want) {
		t.Errorf("depo yolu %q, beklenen sonek %q", src, want)
	}

	unit := readVolumeMountUnit(t)
	for _, key := range []string{"What=", "Where="} {
		if !strings.Contains(unit, key+volumeRoot+"\n") {
			t.Errorf("birimde %s%s satırı yok", key, volumeRoot)
		}
	}
}

// TestVolumeMountUnitAvoidsEarlyBootCycle, birimi yeniden başlatmada
// öldüren sıralama döngüsünün geri gelmediğini doğrular.
//
// Birimin ilk hâli `WantedBy=local-fs.target` + tmpfiles'a `Requires=`
// taşıyordu. Kurulumda kusursuz göründü — ama YENİDEN BAŞLATMAYI GEÇEMEDİ:
// systemd-tmpfiles-setup.service'in kendisi `After=local-fs.target` olduğu
// için döngü oluştu ve systemd bizim mount işimizi SİLDİ. Sonuç sessizdi;
// hacimler sertleştirilmeden açıldı (docs/decisions.md K-039).
//
// Bu testin yakaladığı şey birleşimdir: ikisinden biri tek başına
// zararsız, ikisi birlikte önyüklemeyi bozuyor.
func TestVolumeMountUnitAvoidsEarlyBootCycle(t *testing.T) {
	directives := unitDirectives(readVolumeMountUnit(t))
	if len(directives) == 0 {
		t.Fatal("birimde hiç direktif yok — test bir şey ölçmüyor")
	}

	var wantedBy int
	for _, d := range directives {
		// tmpfiles'a HİÇBİR bağımlılık kurulamaz — sıralama bile.
		//
		// `Requires=`'ı atıp `After=`'ı bırakmak YETMEDİ: systemd her
		// .mount birimine örtük `Before=local-fs.target` ekler, dolayısıyla
		// tmpfiles'a herhangi bir sıralama kenarı halkayı yine kapatır.
		for _, dep := range []string{"Requires=", "BindsTo=", "Requisite=", "After=", "Wants="} {
			if strings.HasPrefix(d, dep) && strings.Contains(d, "tmpfiles") {
				t.Errorf("%q tmpfiles'a bağımlılık kuruyor — .mount birimlerinin "+
					"örtük Before=local-fs.target'ı yüzünden erken önyükleme döngüsü", d)
			}
		}
		if strings.HasPrefix(d, "WantedBy=") {
			wantedBy++
			// local-fs.target erken önyüklemededir ve tmpfiles ondan SONRA
			// koşar; ikisini birleştirmek döngü kurar.
			if strings.Contains(d, "local-fs.target") {
				t.Errorf("%q — bu birim tmpfiles'tan sonra gelmeli, aksi hâlde "+
					"systemd döngüyü kırarken mount işini siler", d)
			}
		}
	}
	if wantedBy == 0 {
		t.Error("birimde [Install] WantedBy= yok — `enable` hiçbir şey yapmaz " +
			"ve birim yeniden başlatmadan sonra HİÇ bağlanmaz")
	}
}

// unitDirectives, birim dosyasının YORUM OLMAYAN satırlarını döndürür.
//
// Neden gerekli: bu birimin yorumları, yasaklanan direktiflerin metnini
// (`WantedBy=local-fs.target`) gerekçeleriyle birlikte ANIYOR. Dosyanın
// tamamında düz metin araması yapan ilk sürüm bu yüzden kendi
// açıklamasını ihlal sanıp yanlış alarm verdi.
func unitDirectives(unit string) []string {
	var out []string
	for line := range strings.Lines(unit) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		out = append(out, line)
	}
	return out
}

// TestInstallScriptVerifiesVolumeHardeningFromKernel, kurulumun bayrakları
// ÇEKİRDEKTEN okuduğunu doğrular.
//
// `systemctl is-active` yeterli DEĞİLDİR ve bu boş bir titizlik değil:
// Docker'ın local sürücüsüne aynı seçenekler verildiğinde hacim
// sertleştirilmeden bağlanıyor ve hiçbir hata üretmiyor — ölçüldü
// (docs/decisions.md K-038). Aynı sessiz arıza systemd tarafında da
// olabilirdi; ayıran tek şey etkin bayrakları okumaktır.
func TestInstallScriptVerifiesVolumeHardeningFromKernel(t *testing.T) {
	script, err := installScript.ReadFile("install.sh")
	if err != nil {
		t.Fatal(err)
	}
	text := string(script)

	if !strings.Contains(text, "/proc/self/mountinfo") {
		t.Error("kurulum etkin mount bayraklarını çekirdekten okumuyor")
	}
	for _, flag := range []string{"nodev", "nosuid"} {
		if !strings.Contains(text, flag) {
			t.Errorf("kurulum %q bayrağını doğrulamıyor", flag)
		}
	}
	// Doğrulama BAŞARISIZ olduğunda kurulum DURMALI; uyarıp devam etmek
	// tam olarak "korumayı iddia et ama zorlama" durumudur.
	//
	// Kapsam dar tutuluyor: bu kontrolün ilk hâli `die`'ı mountinfo
	// satırından SONRAKİ tüm metinde arıyordu ve install.sh'ın ilerideki
	// başka `die` çağrıları yüzünden fail-open mutasyonunu KAÇIRDI —
	// mutasyon testinde yakalandı. Yalnızca doğrulama bloğuna bakılır.
	block := between(text, `vol_opts="$(awk`, `say "hacim kökü`)
	if block == "" {
		t.Fatal("kurulumda hacim doğrulama bloğu bulunamadı — test bir şey ölçmüyor")
	}
	if !strings.Contains(block, "die ") {
		t.Error("bayrak doğrulaması başarısız olunca kurulum durmuyor (fail-open)")
	}
}

func between(text, start, end string) string {
	i := strings.Index(text, start)
	if i < 0 {
		return ""
	}
	rest := text[i:]
	j := strings.Index(rest[len(start):], end)
	if j < 0 {
		return rest
	}
	return rest[:len(start)+j]
}

// ── Yardımcılar ──────────────────────────────────────────────────────

// newFakeRepo, buildArchive'ın beklediği düzende sahte bir depo kurar.
func newFakeRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	for _, arch := range []string{"amd64", "arm64"} {
		dir := filepath.Join(root, "bin", "linux-"+arch)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, name := range serverBinaries {
			content := name + "-" + arch
			if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o755); err != nil {
				t.Fatal(err)
			}
		}
	}

	// Dizinler unitFiles'tan TÜRETİLİYOR, sabit yazılmıyor: varlıklar tek
	// bir dizinde değil (systemd birimleri deploy/systemd'de, ters vekilin
	// yapılandırması deploy/caddy'de). Sabit yazılsaydı yeni bir dizin
	// eklendiğinde fikstür üretimden sessizce ayrışırdı.
	for _, rel := range unitFiles {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("# sahte birim\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	key := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExample erkan@test\n"
	if err := os.WriteFile(filepath.Join(root, "key.pub"), []byte(key), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func readArchive(t *testing.T, archive []byte) map[string][]byte {
	t.Helper()

	files := make(map[string][]byte)
	tr := tar.NewReader(bytes.NewReader(archive))
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("paket okunamadı: %v", err)
		}
		content, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("%s okunamadı: %v", header.Name, err)
		}
		files[header.Name] = content
	}
	return files
}

// TestRejectsOptionLikeHost, `-` ile başlayan hedefin reddedildiğini
// doğrular.
//
// `ssh` kabuk üzerinden çağrılmıyor, yani kabuk enjeksiyonu yok. Ama
// `-` ile başlayan konumsal argümanı ssh SEÇENEK olarak okur:
// `-oProxyCommand=<komut>` iş istasyonunda keyfî yerel komut çalıştırır.
// Bootstrap'ta hedef tek parça argüman olarak geçtiği için kontrol de
// tek satır.
//
// Aynı sınıfın istemci tarafındaki karşılığı için
// internal/client/argvsafety_test.go'ya bakın.
func TestRejectsOptionLikeHost(t *testing.T) {
	anahtar := filepath.Join(t.TempDir(), "id.pub")
	if err := os.WriteFile(anahtar, []byte("ssh-ed25519 AAAAC3Example test\n"), 0o600); err != nil {
		t.Fatalf("anahtar yazılamadı: %v", err)
	}

	for _, hedef := range []string{
		"-oProxyCommand=touch /tmp/pwned",
		"-E/tmp/gunluk",
	} {
		t.Run(hedef, func(t *testing.T) {
			opts := Options{Host: hedef, ClientKeyPath: anahtar}
			if err := validate(&opts); err == nil {
				t.Fatalf("seçenek benzeri hedef kabul edildi: %q", hedef)
			}
		})
	}
}

// TestAcceptsOrdinaryHost, doğrulamanın her şeyi reddetmediğini gösterir.
//
// Bu satır olmadan yukarıdaki test, validate'in daima hata döndürdüğü
// bozuk bir durumda da geçerdi.
func TestAcceptsOrdinaryHost(t *testing.T) {
	anahtar := filepath.Join(t.TempDir(), "id.pub")
	if err := os.WriteFile(anahtar, []byte("ssh-ed25519 AAAAC3Example test\n"), 0o600); err != nil {
		t.Fatalf("anahtar yazılamadı: %v", err)
	}

	opts := Options{Host: "root@1.2.3.4", ClientKeyPath: anahtar}
	if err := validate(&opts); err != nil {
		t.Fatalf("meşru hedef reddedildi: %v", err)
	}
}

// ── Kurulumun ters vekil adımı ───────────────────────────────────────

// TestInstallScriptProvesTheModuleBoundaryWasMeasured, K-050 sınırının
// ÖLÇÜLDÜĞÜNÜN kanıtlandığını doğrular.
//
// Betik "dosya servis eden modül var mı" diye soruyor. Ama bu soru tek
// başına, binary HİÇ ÇALIŞMADIĞINDA da "yok" cevabı üretir — boş çıktıda
// grep hiçbir şey bulmaz. Cevapsızlığı istenen cevap diye okumak bu
// projede üç kez yanlış sonuç ürettirdi (K-051): Caddy yoklaması iki kez
// ölçmediği hâlde "güvenli" dedi.
//
// Bu yüzden POZİTİF KONTROL önce gelmeli: beklenen bir modülün VARLIĞI
// kanıtlanmadan yokluk iddiası anlamsızdır. Test sırayı zorluyor.
func TestInstallScriptProvesTheModuleBoundaryWasMeasured(t *testing.T) {
	script, err := installScript.ReadFile("install.sh")
	if err != nil {
		t.Fatal(err)
	}
	text := string(script)

	pozitif := strings.Index(text, "http.handlers.reverse_proxy")
	if pozitif < 0 {
		t.Fatal("pozitif kontrol yok: reverse_proxy'nin VARLIĞI hiç sınanmıyor, " +
			"dolayısıyla dosya-servisi kontrolü boş çıktıda da geçerdi")
	}

	negatif := strings.Index(text, "file_server|templates|caddyfs")
	if negatif < 0 {
		t.Fatal("dosya servis eden modüller hiç sınanmıyor — K-050 sınırı doğrulanmıyor")
	}

	if pozitif > negatif {
		t.Error("pozitif kontrol negatif kontrolden SONRA geliyor; " +
			"ölçümün yapıldığı kanıtlanmadan sonuç okunuyor")
	}
}

// TestInstallScriptEnablesReverseProxyUnits, birimlerin yalnızca
// BAŞLATILMADIĞINI, ETKİNLEŞTİRİLDİĞİNİ de doğrular.
//
// Bu ayrım gerçek bir kurulumda atlandı: admin soketi başlatılmış ama
// etkinleştirilmemişti ve eksiklik yalnızca REBOOT testinde ortaya çıktı.
// "Şu an çalışıyor" bir kabul ölçütü değil.
func TestInstallScriptEnablesReverseProxyUnits(t *testing.T) {
	script, err := installScript.ReadFile("install.sh")
	if err != nil {
		t.Fatal(err)
	}
	text := string(script)

	for _, unit := range []string{"panely-caddy-admin.socket", "panely-caddy.service"} {
		if !strings.Contains(text, "systemctl enable "+unit) {
			t.Errorf("%s etkinleştirilmiyor — yeniden başlatmadan sonra geri gelmez", unit)
		}
		// Doğrulama da olmalı: `enable` çağrılmış olması, sonucu
		// kanıtlamaz.
		if !strings.Contains(text, "systemctl is-enabled") {
			t.Error("etkinleştirmenin SONUCU doğrulanmıyor")
		}
	}
}

// TestInstallScriptVerifiesTheRunningProxyImage, çalışan İMAJIN kurulan
// binary olduğunun kanıtlandığını doğrular (K-049).
//
// `systemctl is-active` yeni ikilinin çalıştığını KANITLAMAZ: eski süreç
// ayakta kalmışsa birim yine "active" görünür. Bu tam olarak yaşandı —
// göç dosyası uygulanmadığı hâlde servis "active", journal temiz ve çıkış
// kodu 0'dı.
func TestInstallScriptVerifiesTheRunningProxyImage(t *testing.T) {
	script, err := installScript.ReadFile("install.sh")
	if err != nil {
		t.Fatal(err)
	}
	text := string(script)

	if !strings.Contains(text, "/proc/$caddy_pid/exe") {
		t.Error("çalışan imaj /proc/<pid>/exe'den okunmuyor — " +
			"birim yolu okumak yalnızca NE ÇALIŞTIRILMAK İSTENDİĞİNİ söyler")
	}
	if !strings.Contains(text, "md5sum") {
		t.Error("çalışan imaj kurulan binary ile İÇERİKÇE karşılaştırılmıyor")
	}
}

// TestInstallScriptRefusesToShareThePrivilegedGroup, ters vekil
// kullanıcısının panely grubunda OLMADIĞININ sınandığını doğrular.
func TestInstallScriptRefusesToShareThePrivilegedGroup(t *testing.T) {
	script, err := installScript.ReadFile("install.sh")
	if err != nil {
		t.Fatal(err)
	}
	text := string(script)

	if !strings.Contains(text, "id -nG panely-caddy") {
		t.Fatal("panely-caddy'nin grup üyeliği hiç sınanmıyor")
	}
	// Kurulum, exec.sock'a erişemediğini de ÖLÇMELİ.
	if !strings.Contains(text, "--reuid panely-caddy") {
		t.Error("ters vekilin exec.sock'a erişemediği ölçülmüyor — " +
			"yalnızca grup listesine bakmak, izinlerin gerçekte ne verdiğini söylemez")
	}
}
