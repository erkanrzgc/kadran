// Package client, iş istasyonundan panelyd'ye bağlanmayı sağlar.
//
// İki taşıma desteklenir:
//
//   - SSH: `ssh -T panely-client@host` alt süreç olarak çalıştırılır ve
//     borularının üzerinden gRPC konuşulur. Sunucuda sshd bu boruları
//     zorlanmış komuta (panely-connect) bağlar.
//   - Yerel unix soketi: sunucunun kendisinde çalışırken kullanılır.
//
// Hiçbir durumda ağ portu açılmaz.
package client

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/erkanrzgc/panely/internal/connproto"
	panelyv1 "github.com/erkanrzgc/panely/internal/pb/panely/v1"
	"github.com/erkanrzgc/panely/internal/version"
)

// DefaultSSHUser, bootstrap'in oluşturduğu yetkisiz istemci kullanıcısıdır.
const DefaultSSHUser = "panely-client"

// DefaultSocketPath, sunucudaki api soketidir.
const DefaultSocketPath = "/run/panely/api.sock"

// sshExitGrace, bağlantı kapandıktan sonra ssh alt sürecinin kendiliğinden
// çıkması için tanınan süre. Dolarsa süreç öldürülür; asılı bir süreç
// bırakmak sızıntıdır.
const sshExitGrace = 5 * time.Second

// Target, bağlanılacak hedefi tanımlar.
type Target struct {
	// SocketPath doluysa yerel unix soketi kullanılır.
	SocketPath string

	// SSHUser/SSHHost doluysa SSH taşıması kullanılır.
	SSHUser string
	SSHHost string
	SSHPort int
}

// IsLocal, hedefin yerel soket olup olmadığını söyler.
func (t Target) IsLocal() bool { return t.SocketPath != "" }

// String, hedefi insan tarafından okunabilir biçimde döndürür.
func (t Target) String() string {
	if t.IsLocal() {
		return "unix:" + t.SocketPath
	}
	if t.SSHPort != 0 && t.SSHPort != 22 {
		host := t.SSHHost
		// Port varken IPv6 köşeli paranteze alınmalı; yoksa üretilen dize
		// ParseTarget tarafından geri okunamaz.
		if strings.Contains(host, ":") {
			host = "[" + host + "]"
		}
		return fmt.Sprintf("%s@%s:%d", t.SSHUser, host, t.SSHPort)
	}
	return t.SSHUser + "@" + t.SSHHost
}

// ParseTarget, komut satırından gelen hedef dizesini çözümler.
//
// Kabul edilen biçimler:
//
//	/run/panely/api.sock        → yerel soket (mutlak yol)
//	unix:///run/panely/api.sock → yerel soket (açık)
//	kullanici@sunucu            → SSH
//	kullanici@sunucu:2222       → SSH, özel port
//	sunucu                      → SSH, varsayılan kullanıcı
//
// Boş dize yerel varsayılan sokete çözümlenir; sunucuda `panely status`
// yazmak yeterli olsun diye.
func ParseTarget(s string) (Target, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Target{SocketPath: DefaultSocketPath}, nil
	}

	if path, ok := strings.CutPrefix(s, "unix://"); ok {
		if path == "" {
			return Target{}, errors.New("client: unix:// hedefinde yol yok")
		}
		return Target{SocketPath: path}, nil
	}

	// Mutlak yol (POSIX veya Windows) yerel soket sayılır.
	if strings.HasPrefix(s, "/") || strings.HasPrefix(s, `\`) {
		return Target{SocketPath: s}, nil
	}

	user := DefaultSSHUser
	host := s
	if u, h, ok := strings.Cut(s, "@"); ok {
		if u == "" {
			return Target{}, errors.New("client: hedefte kullanıcı adı boş")
		}
		user, host = u, h
	}

	host, port, err := splitHostPort(host)
	if err != nil {
		return Target{}, err
	}
	if host == "" {
		return Target{}, errors.New("client: hedefte sunucu adı boş")
	}
	if err := rejectOptionLike(user, host); err != nil {
		return Target{}, err
	}
	return Target{SSHUser: user, SSHHost: host, SSHPort: port}, nil
}

// rejectOptionLike, `-` ile başlayan kullanıcı/sunucu adlarını reddeder.
//
// # Kabuk yok ama argüman enjeksiyonu var
//
// `ssh` bir kabuk üzerinden çağrılmıyor; argümanlar exec'e dizi olarak
// veriliyor. Bu, kabuk enjeksiyonunu tamamen kapatır — ama BAŞKA bir
// sınıfı açık bırakır. `-` ile başlayan bir konumsal argümanı ssh
// SEÇENEK olarak okur, ve `-oProxyCommand=<komut>` keyfî YEREL komut
// çalıştırır.
//
// Somut olarak: `strings.Cut(s, "@")` ilk @'te böldüğü için
// `-oProxyCommand=touch /tmp/pwned@sunucu` girdisinde kullanıcı adı
// saldırganın denetimine geçiyordu ve birleştirilen argüman `-` ile
// başlıyordu.
//
// Hedef dizesi yalnızca komut satırından gelmiyor: sidecar hedefleri
// GUI profillerinden alıyor. Yani "kullanıcı kendi ayağına sıkar"
// savunması geçerli değil.
//
// Neden `--` ile ayırmak yerine reddetmek? `--` desteği OpenSSH
// sürümüne göre değişir; taşınabilir ve kesin olan, girdiyi kaynağında
// reddetmektir. Meşru hiçbir kullanıcı veya sunucu adı `-` ile başlamaz.
func rejectOptionLike(user, host string) error {
	if strings.HasPrefix(user, "-") {
		return fmt.Errorf(
			"client: kullanıcı adı `-` ile başlayamaz (%q) — "+
				"ssh bunu seçenek olarak yorumlar", user)
	}
	if strings.HasPrefix(host, "-") {
		return fmt.Errorf(
			"client: sunucu adı `-` ile başlayamaz (%q) — "+
				"ssh bunu seçenek olarak yorumlar", host)
	}
	return nil
}

// splitHostPort, sunucu adını ve varsa portu ayırır.
//
// # IPv6 neden özel?
//
// Çıplak bir IPv6 adresi zaten iki nokta üst üste içerir (2001:db8::1).
// Naif bir "ilk iki noktadan böl" yaklaşımı bunu host=2001, port=db8::1
// diye ayırır. Ayrım kuralı standart URL semantiğiyle aynıdır:
//
//   - Köşeli parantezli ([2001:db8::1]:2222) → port olabilir
//   - Birden fazla iki nokta, parantezsiz → çıplak IPv6, port yok
//   - Tek iki nokta → sunucu:port
func splitHostPort(s string) (host string, port int, err error) {
	if strings.HasPrefix(s, "[") {
		closing := strings.LastIndex(s, "]")
		if closing < 0 {
			return "", 0, fmt.Errorf("client: kapanmamış köşeli parantez: %q", s)
		}
		host = s[1:closing]
		rest := s[closing+1:]
		if rest == "" {
			return host, 0, nil
		}
		p, ok := strings.CutPrefix(rest, ":")
		if !ok {
			return "", 0, fmt.Errorf("client: köşeli parantezden sonra beklenmedik metin: %q", rest)
		}
		port, err = parsePort(p)
		return host, port, err
	}

	if strings.Count(s, ":") > 1 {
		// Çıplak IPv6. Port belirtmek için köşeli parantez gerekir.
		return s, 0, nil
	}

	h, p, ok := strings.Cut(s, ":")
	if !ok {
		return s, 0, nil
	}
	port, err = parsePort(p)
	return h, port, err
}

func parsePort(s string) (int, error) {
	port, err := strconv.Atoi(s)
	if err != nil || port <= 0 || port > 65535 {
		return 0, fmt.Errorf("client: geçersiz port: %q", s)
	}
	return port, nil
}

// Client, panelyd'ye bağlı bir istemcidir.
type Client struct {
	conn   *grpc.ClientConn
	rpc    panelyv1.PanelyServiceClient
	target Target
}

// Dial, hedefe bağlanır.
//
// Bağlantı tembeldir: gerçek bağlantı ilk RPC'de kurulur. Bu kasıtlıdır —
// SSH alt sürecini ancak gerçekten ihtiyaç duyulduğunda başlatmak, `panely
// --help` gibi komutların sunucuya dokunmamasını sağlar.
func Dial(target Target) (*Client, error) {
	dialer, err := dialerFor(target)
	if err != nil {
		return nil, err
	}

	conn, err := grpc.NewClient("passthrough:///panely",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(dialer),
	)
	if err != nil {
		return nil, fmt.Errorf("client: bağlantı kurulamadı: %w", err)
	}

	return &Client{
		conn:   conn,
		rpc:    panelyv1.NewPanelyServiceClient(conn),
		target: target,
	}, nil
}

// Close, bağlantıyı ve varsa SSH alt sürecini kapatır.
func (c *Client) Close() error { return c.conn.Close() }

// Target, bağlanılan hedefi döndürür.
func (c *Client) Target() Target { return c.target }

// RPC, alt seviye gRPC istemcisini döndürür.
func (c *Client) RPC() panelyv1.PanelyServiceClient { return c.rpc }

// dialerFor, hedefe uygun bağlantı kurucuyu üretir.
//
// # Önsöz asimetrisi — kasıtlı
//
// Yerel yol kimlik önsözünü KENDİ yazar; SSH yolu yazmaz. SSH'ta önsözü
// sunucu tarafında panely-connect yazıyor (bkz. cmd/panely-connect).
// Burada da yazmak iki önsöz üretirdi: panelyd ilkini okur, ardından
// HTTP/2 beklediği yerde dört baytlık bir uzunluk artı JSON bulurdu ve
// bağlantı kurulmadan ölürdü.
func dialerFor(t Target) (func(context.Context, string) (net.Conn, error), error) {
	if t.IsLocal() {
		return func(ctx context.Context, _ string) (net.Conn, error) {
			return dialLocal(ctx, t.SocketPath)
		}, nil
	}
	if t.SSHHost == "" {
		return nil, errors.New("client: hedef belirtilmedi")
	}
	return func(ctx context.Context, _ string) (net.Conn, error) {
		return dialSSH(ctx, t)
	}, nil
}

// dialLocal, yerel unix soketine bağlanır ve kimlik önsözünü yazar.
//
// # Önsöz neden burada, Dial() içinde değil?
//
// gRPC bu kurucuyu bağlantı başına çağırır: ilk bağlantıda, ve kopma
// sonrası her yeniden bağlanmada (GOAWAY, geçici hata). Önsözü Dial()
// içinde bir kez yazmak ilk bağlantıda çalışır, sonrakilerin hepsinde
// sessizce bozulurdu.
//
// Bağlantı gRPC'ye teslim edilmeden önce yazıldığı için sıralama garanti
// altındadır; yarış yoktur.
func dialLocal(ctx context.Context, path string) (net.Conn, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", path)
	if err != nil {
		return nil, fmt.Errorf("client: yerel sokete bağlanılamadı (%s): %w", path, err)
	}

	// Önsöz yazılamazsa bağlantı KAPATILMALIDIR. gRPC kurucuyu yeniden
	// deneyecek; kapatılmayan her başarısız deneme bir dosya tanıtıcısı
	// sızdırır ve sınır yoktur.
	if err := connproto.Write(conn, localIdentity()); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

// localIdentity, yerel soket bağlantısının kimliğidir.
//
// Origin sabittir, parmak izi yoktur ve geçersiz kılacak bir bayrak
// bulunmaz.
//
// # Bu, önsözü uydurulabilir yapmıyor mu?
//
// Hayır — çünkü uydurma zaten mümkündü ve bu kod onu kolaylaştırmıyor.
// Önsözün bütünlüğü "api.sock'a yalnızca panely-connect yazabilir"
// varsayımına DAYANMAZ; "SSH_AUTH_INFO_0'ı yalnızca sshd ayarlayabilir"
// varsayımına dayanır. İstemci kullanıcısı olarak rastgele kod
// çalıştırabilen biri panely-connect'i düzmece bir ortamla çağırıp
// istediği kimliği zaten yazdırabilir; yerel yol yeni bir yüzey açmıyor.
//
// Sabit tutmanın nedeni budur: kimlik uydurmak bir sömürü adımı olarak
// kalmalı, hazır bir kod yolu haline gelmemeli.
func localIdentity() connproto.Identity {
	return connproto.Identity{Origin: "local"}
}

// sshCommand, çalıştırılacak SSH istemcisinin adıdır.
//
// Değişken olmasının tek nedeni testtir: sahte bir `ssh` ile değiştirilip
// istemcinin boruya YAZDIĞI ilk baytlar doğrulanabiliyor. Üretimde asla
// değişmez ve dışarıdan ayarlanamaz.
var sshCommand = "ssh"

// dialSSH, `ssh` alt sürecini başlatır ve borularını net.Conn'a sarar.
//
// Buraya kimlik önsözü YAZILMAZ; sunucuda panely-connect yazıyor.
// Gerekçe için dialerFor'daki "önsöz asimetrisi" notuna bakın.
func dialSSH(ctx context.Context, t Target) (net.Conn, error) {
	// ParseTarget tek savunma olsaydı, doğrudan kurulan bir Target
	// (diskteki profilden okunan alanlar gibi) korumayı baypas ederdi.
	// Doğrulama argv'nin kurulduğu yerde de duruyor; bkz. rejectOptionLike.
	if err := rejectOptionLike(t.SSHUser, t.SSHHost); err != nil {
		return nil, err
	}

	args := []string{
		"-T", // pty isteme: zorlanmış komut zaten kabuk vermiyor
		// BatchMode: parola sorulmasın. Anahtar çalışmıyorsa sonsuza
		// kadar bir istemde asılı kalmak yerine hemen hata versin.
		"-o", "BatchMode=yes",
		// Bağlantı kurulamıyorsa uzun uzun beklemesin.
		"-o", "ConnectTimeout=10",
	}
	if t.SSHPort != 0 {
		args = append(args, "-p", strconv.Itoa(t.SSHPort))
	}
	args = append(args, t.SSHUser+"@"+t.SSHHost)

	// # Neden CommandContext DEĞİL?
	//
	// Buradaki ctx gRPC'nin BAĞLANTI DENEMESİ bağlamıdır ve gRPC onu el
	// sıkışma biter bitmez iptal eder. `exec.CommandContext` iptalde
	// süreci ÖLDÜRÜR — yani ssh, bağlantı kurulur kurulmaz ölürdü ve
	// istemci şunu görürdü:
	//
	//	error reading server preface: EOF
	//
	// Alt sürecin ömrü BAĞLANTIYA bağlı olmalı, tek bir denemeye değil.
	// Toplanması Close() üzerinden yapılıyor (aşağıdaki cleanup).
	//
	// Yerel yol bu hatadan etkilenmiyordu: unix soketine bağlandıktan
	// sonra bağlamın iptali bağlantıyı etkilemez. Hata bu yüzden ancak
	// gerçek sunucuya SSH ile bağlanınca ortaya çıktı.
	//
	// Bkz. TestSSHProcessSurvivesDialContextCancel.
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("client: bağlanmadan önce iptal edildi: %w", err)
	}
	cmd := exec.Command(sshCommand, args...) //nolint:noctx // gerekçe yukarıda

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("client: ssh stdout borusu açılamadı: %w", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("client: ssh stdin borusu açılamadı: %w", err)
	}

	// ssh'ın stderr'i yakalanır: "Permission denied", "Host key
	// verification failed" gibi asıl teşhis oradan gelir. Kullanıcıya
	// "bağlantı kapandı" demek yerine gerçek sebebi göstermek istiyoruz.
	stderr := &syncBuffer{}
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return nil, errors.New("client: `ssh` komutu bulunamadı — OpenSSH istemcisi kurulu mu?")
		}
		return nil, fmt.Errorf("client: ssh başlatılamadı: %w", err)
	}

	// cleanup, bağlantı kapanınca alt süreci toplar.
	//
	// Süreç artık dial bağlamına bağlı olmadığı için toplanması TAMAMEN
	// buraya kaldı. pipeConn önce yazma ucunu kapatıyor; ssh normalde
	// EOF görüp kendiliğinden çıkar. Çıkmazsa süresiz beklemek her
	// bağlantıda asılı bir süreç bırakırdı — o yüzden süre sınırı var.
	cleanup := func() error {
		bitti := make(chan error, 1)
		go func() { bitti <- cmd.Wait() }()

		var err error
		select {
		case err = <-bitti:
		case <-time.After(sshExitGrace):
			_ = cmd.Process.Kill()
			err = <-bitti
		}

		if err == nil {
			return nil
		}
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("ssh: %s", msg)
		}
		return fmt.Errorf("ssh sonlandı: %w", err)
	}

	return newPipeConn(stdout, stdin, t.String(), cleanup), nil
}

// syncBuffer, alt sürecin stderr'ini eşzamanlı okuma/yazmaya karşı güvenli
// biçimde biriktirir.
//
// exec paketi stderr'e ayrı bir goroutine'den yazar; cleanup ise başka bir
// goroutine'den okuyabilir. Kilitsiz bir bytes.Buffer burada yarış üretirdi.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// CheckProtocol, sunucuyla sürüm uyumunu doğrular.
//
// Protokol sürümü farklıysa bağlantı reddedilir: uyumsuz sözleşmelerle
// konuşmak, sessizce yanlış davranmaktan iyidir.
func (c *Client) CheckProtocol(ctx context.Context) (*panelyv1.PingResponse, error) {
	resp, err := c.rpc.Ping(ctx, &panelyv1.PingRequest{ClientVersion: version.Version})
	if err != nil {
		return nil, err
	}
	if resp.GetProtocolVersion() != version.Protocol {
		return nil, fmt.Errorf(
			"protokol uyumsuzluğu: istemci %d, sunucu %d — "+
				"iki tarafın binary'leri aynı sürümden olmalı, sunucudakileri güncelleyin",
			version.Protocol, resp.GetProtocolVersion())
	}
	return resp, nil
}

// SSHAvailable, `ssh` komutunun bulunup bulunmadığını söyler.
func SSHAvailable() bool {
	_, err := exec.LookPath("ssh")
	return err == nil
}
