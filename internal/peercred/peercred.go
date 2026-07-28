// Package peercred, unix soketi üzerinden bağlanan sürecin kimliğini
// çekirdekten doğrular ve bunu gRPC'ye taşıma kimlik bilgisi olarak bağlar.
//
// # Neden soket izinleri yetmiyor?
//
// Soket dosyasının mod ve sahiplik bitleri ilk savunmadır, ama tek başına
// yeterli değildir:
//
//   - Dizin izinleri yanlış kurulursa (bootstrap hatası, elle müdahale,
//     bir göç betiği) soket beklenmedik biçimde erişilebilir hale gelir ve
//     bunu fark eden hiçbir mekanizma olmaz.
//   - İzinler kurulumda doğrulanır ama bağlantı anında doğrulanmaz.
//
// SO_PEERCRED, bağlantının diğer ucundaki sürecin uid/gid'ini doğrudan
// çekirdekten alır. Kandırılamaz ve her bağlantıda yeniden kontrol edilir.
//
// # Neden interceptor değil?
//
// gRPC interceptor'ları istek gövdesi çözümlendikten SONRA çalışır.
// TransportCredentials.ServerHandshake ise bağlantı kurulurken, henüz
// hiçbir uygulama verisi okunmadan çalışır. Yetkisiz çağıran tek bir RPC
// baytı bile gönderemeden reddedilir.
package peercred

import (
	"context"
	"errors"
	"fmt"
	"net"

	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
)

// ErrUnsupportedPlatform, SO_PEERCRED bulunmayan platformlarda döner.
// Panely sunucu bileşenleri yalnızca Linux'ta çalışır; bu hata Windows
// veya macOS üzerinde derleme yapılabilsin diye vardır.
var ErrUnsupportedPlatform = errors.New("peercred: bu platformda desteklenmiyor")

// ErrNotUnixConn, bağlantı bir unix soketi olmadığında döner.
var ErrNotUnixConn = errors.New("peercred: bağlantı unix soketi değil")

// ErrDenied, çağıranın kimliği politikaya uymadığında döner.
var ErrDenied = errors.New("peercred: çağıran reddedildi")

// Cred, çekirdekten doğrulanmış çağıran kimliğidir.
type Cred struct {
	PID int32
	UID uint32
	GID uint32
}

func (c Cred) String() string {
	return fmt.Sprintf("pid=%d uid=%d gid=%d", c.PID, c.UID, c.GID)
}

// Policy, hangi çağıranların kabul edileceğini belirler.
//
// Boş bir Policy hiçbir şeyi kabul etmez. Varsayılanın "her şeyi reddet"
// olması kasıtlıdır: yapılandırma hatası sistemi açık bırakmamalı, kapalı
// bırakmalıdır.
type Policy struct {
	// AllowUIDs, kabul edilen kullanıcı kimlikleri.
	//
	// Executor bunu kullanır: yalnızca `panely` kullanıcısı (yani panelyd)
	// bağlanabilir.
	AllowUIDs []uint32

	// AllowGIDs, kabul edilen birincil grup kimlikleri.
	//
	// Daemon bunu kullanır: `panely-client` grubundaki herhangi bir
	// istemci kullanıcısı bağlanabilir.
	//
	// UYARI: SO_PEERCRED yalnızca sürecin BİRİNCİL grubunu döndürür, ek
	// grup üyeliklerini değil. Bootstrap bu yüzden istemci SSH kullanıcısını
	// birincil grubu `panely-client` olacak şekilde oluşturur.
	AllowGIDs []uint32
}

// Allows, verilen kimliğin politikaya uyup uymadığını söyler.
func (p Policy) Allows(c Cred) bool {
	for _, uid := range p.AllowUIDs {
		if c.UID == uid {
			return true
		}
	}
	for _, gid := range p.AllowGIDs {
		if c.GID == gid {
			return true
		}
	}
	return false
}

// IsEmpty, politikanın hiçbir çağıranı kabul etmediğini söyler.
func (p Policy) IsEmpty() bool { return len(p.AllowUIDs) == 0 && len(p.AllowGIDs) == 0 }

// AuthInfo, doğrulanmış çağıran kimliğini gRPC'nin credentials.AuthInfo
// arayüzü üzerinden handler'lara taşır.
type AuthInfo struct {
	Cred Cred
}

// AuthType, credentials.AuthInfo arayüzünü karşılar.
func (AuthInfo) AuthType() string { return "peercred" }

// AuthInfoFromContext, gRPC bağlamından doğrulanmış çağıran kimliğini çıkarır.
//
// Handler'lar bunu, çağıranın çekirdek tarafından doğrulanmış unix kimliğini
// öğrenmek için kullanır. İstemciden gelen metadata'nın aksine bu değer
// uydurulamaz: bağlantı kurulurken SO_PEERCRED ile okunmuştur.
//
// İkinci dönüş değeri false ise bağlantı bu kimlik bilgisiyle kurulmamıştır
// (örneğin testlerde bellek içi bir dinleyici kullanılıyordur).
func AuthInfoFromContext(ctx context.Context) (AuthInfo, bool) {
	p, ok := peer.FromContext(ctx)
	if !ok {
		return AuthInfo{}, false
	}
	info, ok := p.AuthInfo.(AuthInfo)
	return info, ok
}

// TransportCredentials, unix soketi bağlantılarında SO_PEERCRED doğrulaması
// yapan bir gRPC taşıma kimlik bilgisi döndürür.
//
// Politika boşsa hata döner: "hiçbir şeyi kabul etme" geçerli bir sunucu
// yapılandırması değildir ve büyük olasılıkla bir kurulum hatasıdır.
func TransportCredentials(p Policy) (credentials.TransportCredentials, error) {
	if p.IsEmpty() {
		return nil, errors.New("peercred: boş politika — en az bir uid veya gid gerekli")
	}
	return &transportCreds{policy: p}, nil
}

type transportCreds struct {
	policy Policy
}

func (t *transportCreds) ServerHandshake(raw net.Conn) (net.Conn, credentials.AuthInfo, error) {
	cred, err := FromConn(raw)
	if err != nil {
		_ = raw.Close()
		return nil, nil, err
	}
	if !t.policy.Allows(cred) {
		_ = raw.Close()
		return nil, nil, fmt.Errorf("%w: %s", ErrDenied, cred)
	}
	return raw, AuthInfo{Cred: cred}, nil
}

// ClientHandshake, istemci tarafında kullanılmaz. Panely'de bu kimlik
// bilgisi yalnızca sunucu tarafındadır; istemci tarafında insecure taşıma
// kullanılır çünkü güven sınırı zaten dosya sistemi ve SSH'tır.
func (t *transportCreds) ClientHandshake(_ context.Context, _ string, raw net.Conn) (net.Conn, credentials.AuthInfo, error) {
	return raw, nil, errors.New("peercred: istemci tarafında kullanılamaz")
}

func (t *transportCreds) Info() credentials.ProtocolInfo {
	return credentials.ProtocolInfo{SecurityProtocol: "peercred"}
}

func (t *transportCreds) Clone() credentials.TransportCredentials {
	clone := *t
	clone.policy.AllowUIDs = append([]uint32(nil), t.policy.AllowUIDs...)
	clone.policy.AllowGIDs = append([]uint32(nil), t.policy.AllowGIDs...)
	return &clone
}

func (t *transportCreds) OverrideServerName(string) error {
	return errors.New("peercred: sunucu adı geçersiz kılınamaz")
}
