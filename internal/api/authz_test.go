package api

import (
	"context"
	"errors"
	"net"
	"sort"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/erkanrzgc/kadran/internal/connproto"
	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
)

// ── Yetki ayrımı (K-131) ─────────────────────────────────────────────
//
// Bu testler yetki fonksiyonunu değil KABLOLAMAYI sınar: sunucu,
// kadrand'nin main.go'da kullandığı NewGRPCServer ile kuruluyor ve
// istekler gerçek gRPC'den, gerçek önsözle geçiyor. Yalnızca SO_PEERCRED
// aşaması sahte (kabulEdenPeer); unix soketi Windows'ta yok.
//
// Gerekçe: yetki bir önleyicide duruyor ve önleyicinin birim testi,
// önleyici HİÇ kaydedilmese de geçerdi. Bugünkü sunucu yalnızca tekli
// önleyici zinciri kuruyordu; akış RPC'leri (Deploy, StreamLogs) ondan
// geçmiyordu.

// yetkiIstemcisi, verilen kimlikle bağlanan bir gRPC istemcisi kurar.
func yetkiIstemcisi(t *testing.T, id connproto.Identity) *grpc.ClientConn {
	t.Helper()
	srv, _ := newTestServer(t)
	gs := NewGRPCServer(srv, &callerCreds{peer: kabulEdenPeer{}})

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("dinlenemedi: %v", err)
	}
	go func() { _ = gs.Serve(lis) }()
	t.Cleanup(gs.Stop)

	addr := lis.Addr().String()
	conn, err := grpc.NewClient("passthrough:///yetki",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			var d net.Dialer
			c, err := d.DialContext(ctx, "tcp", addr)
			if err != nil {
				return nil, err
			}
			if err := connproto.Write(c, id); err != nil {
				_ = c.Close()
				return nil, err
			}
			return c, nil
		}))
	if err != nil {
		t.Fatalf("istemci kurulamadı: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// cagir, servisin bir yöntemini boş bir mesajla çağırır ve durum kodunu
// döner. Boş mesaj her istek tipine çözülür; yetki denetimi de istek
// gövdesinden önce yöntem adına bakıyor.
func cagir(t *testing.T, conn *grpc.ClientConn, method string, stream *grpc.StreamDesc) codes.Code {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	full := "/" + kadranv1.KadranService_ServiceDesc.ServiceName + "/" + method
	if stream == nil {
		return status.Code(conn.Invoke(ctx, full, &emptypb.Empty{}, &emptypb.Empty{}))
	}

	st, err := conn.NewStream(ctx, stream, full)
	if err != nil {
		return status.Code(err)
	}
	if err := st.SendMsg(&emptypb.Empty{}); err != nil {
		return status.Code(err)
	}
	if err := st.CloseSend(); err != nil {
		return status.Code(err)
	}
	for {
		if err := st.RecvMsg(&emptypb.Empty{}); err != nil {
			return status.Code(err)
		}
	}
}

// herYontem, servisin TÜM yöntemlerini çağırır. Servis tanımından
// okunduğu için sonradan eklenen bir RPC de kendiliğinden kapsanır.
func herYontem(t *testing.T, conn *grpc.ClientConn) map[string]codes.Code {
	t.Helper()
	got := map[string]codes.Code{}
	for _, m := range kadranv1.KadranService_ServiceDesc.Methods {
		got[m.MethodName] = cagir(t, conn, m.MethodName, nil)
	}
	for _, s := range kadranv1.KadranService_ServiceDesc.Streams {
		desc := &grpc.StreamDesc{
			StreamName: s.StreamName, ServerStreams: s.ServerStreams, ClientStreams: s.ClientStreams,
		}
		got[s.StreamName] = cagir(t, conn, s.StreamName, desc)
	}
	return got
}

// TestDeployKeyReachesOnlyPingAndDeploy: dağıtım anahtarı Ping ve Deploy
// dışında HER yöntemde PermissionDenied almalı.
func TestDeployKeyReachesOnlyPingAndDeploy(t *testing.T) {
	conn := yetkiIstemcisi(t, connproto.Identity{
		Origin: "ssh", Fingerprint: "SHA256:ci", Role: connproto.RoleDeploy, Apps: []string{"web"},
	})

	for method, code := range herYontem(t, conn) {
		izinli := method == "Ping" || method == "Deploy"
		switch {
		case izinli && method == "Ping" && code != codes.OK:
			t.Errorf("Ping = %v; dağıtım anahtarı bağlanırken protokolü sorabilmeli", code)
		case !izinli && code != codes.PermissionDenied:
			t.Errorf("%s = %v; dağıtım anahtarı için PermissionDenied bekleniyordu", method, code)
		}
	}
}

// TestDeployKeyScope: kapsam DIŞINDAKİ uygulamaya dağıtım reddedilir;
// kapsam İÇİNDEKİ istek yetkiyi geçer (sonrasında başka bir sebeple
// düşmesi burada önemsiz — ölçülen PermissionDenied OLMAMASI).
func TestDeployKeyScope(t *testing.T) {
	conn := yetkiIstemcisi(t, connproto.Identity{
		Origin: "ssh", Role: connproto.RoleDeploy, Apps: []string{"web"},
	})
	rpc := kadranv1.NewKadranServiceClient(conn)

	kod := func(app string) codes.Code {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		st, err := rpc.Deploy(ctx, &kadranv1.DeployRequest{
			AppId: app, CommitSha: strings.Repeat("a", 40),
		})
		if err != nil {
			return status.Code(err)
		}
		for {
			if _, err := st.Recv(); err != nil {
				return status.Code(err)
			}
		}
	}

	if got := kod("baska"); got != codes.PermissionDenied {
		t.Errorf("kapsam dışı dağıtım = %v, PermissionDenied bekleniyordu", got)
	}
	if got := kod(""); got != codes.PermissionDenied {
		t.Errorf("boş uygulama adı = %v, PermissionDenied bekleniyordu", got)
	}
	if got := kod("web"); got == codes.PermissionDenied {
		t.Errorf("kapsam içi dağıtım reddedildi — kontrol grubu: ret her şeye uygulanıyor")
	}
}

// TestAdminReachesEverything: kontrol grubu. Yöneticinin hiçbir yöntemde
// PermissionDenied almaması, dağıtım testindeki retlerin yetkiden
// geldiğini (başka bir arızadan değil) gösterir.
func TestAdminReachesEverything(t *testing.T) {
	conn := yetkiIstemcisi(t, connproto.Identity{Origin: "ssh", Role: connproto.RoleAdmin})
	got := herYontem(t, conn)
	if len(got) == 0 {
		t.Fatal("hiç yöntem çağrılmadı")
	}
	for method, code := range got {
		if code == codes.PermissionDenied {
			t.Errorf("yönetici %s çağıramadı", method)
		}
	}
}

// TestDeployAllowlistIsExact: izin listesine eklenen her yöntem bir
// güvenlik kararıdır ve bu test onu görünür kılar. GetApp özellikle
// DIŞARIDA: ortam değişkenlerinin değerlerini döndürüyor (apps.go).
func TestDeployAllowlistIsExact(t *testing.T) {
	var got []string
	for m := range deployAllowed {
		got = append(got, m)
	}
	sort.Strings(got)
	want := []string{
		kadranv1.KadranService_Deploy_FullMethodName,
		kadranv1.KadranService_Ping_FullMethodName,
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("dağıtım izin listesi = %q, beklenen %q", got, want)
	}
}

// TestInvalidRoleIsRejectedAtHandshake: rolsüz ya da geçersiz önsöz el
// sıkışmada düşer ve günlüğe yazılır (K-095).
func TestInvalidRoleIsRejectedAtHandshake(t *testing.T) {
	for _, id := range []connproto.Identity{
		{Origin: "ssh"},                             // rolsüz: eski kadran-connect
		{Origin: "ssh", Role: "root"},               // bilinmeyen
		{Origin: "ssh", Role: connproto.RoleDeploy}, // kapsamsız
	} {
		buf := logYakala(t)
		creds := &callerCreds{peer: kabulEdenPeer{}}
		istemci, sunucu := net.Pipe()

		go func() { _ = connproto.Write(istemci, id) }()
		_, _, err := creds.ServerHandshake(sunucu)
		_ = istemci.Close()
		_ = sunucu.Close()

		if !errors.Is(err, connproto.ErrInvalidRole) {
			t.Errorf("%+v: el sıkışma hatası = %v, ErrInvalidRole bekleniyordu", id, err)
		}
		if !strings.Contains(buf.String(), "reddedildi") {
			t.Errorf("%+v: ret günlüğe yazılmadı:\n%s", id, buf.String())
		}
	}
}

// birimCagri, tekli önleyiciyi doğrudan çağırır ve işleyicinin koşup
// koşmadığını döner.
func birimCagri(id connproto.Identity, method string, req any) (bool, error) {
	ran := false
	_, err := AuthzUnaryInterceptor()(ctxWithCaller(id), req,
		&grpc.UnaryServerInfo{FullMethod: method},
		func(context.Context, any) (any, error) { ran = true; return nil, nil })
	return ran, err
}

// TestInterceptorRejectsInvalidRolePastHandshake: el sıkışma geçersiz rolü
// zaten reddediyor; önleyici İKİNCİ kat. Bir gün el sıkışma denetimi
// kaybolursa bu kat tek başına kapalı kalmalı.
func TestInterceptorRejectsInvalidRolePastHandshake(t *testing.T) {
	for _, id := range []connproto.Identity{
		{Origin: "ssh"},
		{Origin: "ssh", Role: "root"},
	} {
		ran, err := birimCagri(id, kadranv1.KadranService_Ping_FullMethodName, &kadranv1.PingRequest{})
		if ran || status.Code(err) != codes.PermissionDenied {
			t.Errorf("%+v: işleyici koştu=%v, hata=%v; PermissionDenied bekleniyordu", id, ran, err)
		}
	}
	if ran, err := birimCagri(connproto.Identity{}, kadranv1.KadranService_Ping_FullMethodName, nil); ran || err == nil {
		t.Errorf("kimliksiz bağlam kabul edildi (koştu=%v, hata=%v)", ran, err)
	}
}

// TestUnaryScopeIsChecked: tekli yoldaki kapsam denetimi. Bugün kapsamlı
// tek yöntem (Deploy) bir akış; tekli yol, kapsamlı bir tekli yöntem
// eklendiği gün delik olmasın diye burada Deploy'un denetimiyle sınanıyor.
func TestUnaryScopeIsChecked(t *testing.T) {
	id := connproto.Identity{Origin: "ssh", Role: connproto.RoleDeploy, Apps: []string{"web"}}
	m := kadranv1.KadranService_Deploy_FullMethodName

	if ran, err := birimCagri(id, m, &kadranv1.DeployRequest{AppId: "baska"}); ran || status.Code(err) != codes.PermissionDenied {
		t.Errorf("kapsam dışı: koştu=%v, hata=%v", ran, err)
	}
	if ran, err := birimCagri(id, m, &kadranv1.PingRequest{}); ran || status.Code(err) != codes.PermissionDenied {
		t.Errorf("yanlış istek tipi: koştu=%v, hata=%v", ran, err)
	}
	if ran, err := birimCagri(id, m, &kadranv1.DeployRequest{AppId: "web"}); !ran || err != nil {
		t.Errorf("kapsam içi: koştu=%v, hata=%v — kontrol grubu", ran, err)
	}
}

// TestAppIDPatternMatchesConnproto: iki desen aynı olmalı. Sapma yalnızca
// redde yol açar ama sessiz bir "bu anahtar neden dağıtamıyor" sorusu
// üretir.
func TestAppIDPatternMatchesConnproto(t *testing.T) {
	if appIDPattern.String() != connproto.AppIDPattern() {
		t.Fatalf("uygulama adı desenleri ayrıştı: api %q, connproto %q",
			appIDPattern.String(), connproto.AppIDPattern())
	}
}
