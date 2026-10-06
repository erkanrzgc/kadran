package api

import (
	"context"
	"log/slog"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"

	"github.com/erkanrzgc/kadran/internal/connproto"
	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
)

// ── Yetki ayrımı (K-131) ─────────────────────────────────────────────
//
// İki rol var: yönetici her şeyi yapar; dağıtım anahtarı yalnızca
// kapsamındaki uygulamaları dağıtır. Rol, el sıkışmada önsözden okunuyor
// ve orada doğrulanıyor (credentials.go); buraya geçersiz bir rol ulaşmaz.
//
// İzin listesi VARSAYILAN RET: listede olmayan her yöntem yalnızca
// yöneticiye açık. Sonradan eklenen bir RPC kendiliğinden yöneticiye
// kalır; dağıtım anahtarına açmak bilinçli bir satır ister.

// scopeCheck, isteğin dağıtım kapsamına uyup uymadığını söyler. nil ise
// yöntem kapsamdan bağımsızdır (Ping).
type scopeCheck func(id connproto.Identity, req any) bool

// deployAllowed, dağıtım anahtarının çağırabileceği yöntemler.
//
// GetApp BİLEREK yok: ortam değişkenlerinin ve derleme argümanlarının
// DEĞERLERİNİ döndürüyor. Dağıtım anahtarı `kadran deploy -commit <sha>`
// ile çalışır; CI commit'i zaten biliyor ($GITHUB_SHA).
var deployAllowed = map[string]scopeCheck{
	// Bağlanırken protokol sürümü soruluyor (client.go).
	kadranv1.KadranService_Ping_FullMethodName: nil,

	kadranv1.KadranService_Deploy_FullMethodName: func(id connproto.Identity, req any) bool {
		r, ok := req.(*kadranv1.DeployRequest)
		return ok && id.CanDeploy(r.GetAppId())
	},
}

// authorize, çağıranın yöntemi çağırabilip çağıramayacağına karar verir.
//
// İkinci dönüş, istek gövdesi okunduktan sonra uygulanacak kapsam
// denetimidir; nil ise gövdeye bakmaya gerek yok.
func authorize(ctx context.Context, method string) (connproto.Identity, scopeCheck, error) {
	info, ok := callerFromContext(ctx)
	if !ok {
		return connproto.Identity{}, nil, status.Error(codes.Unauthenticated, "no caller identity")
	}
	id := info.Identity

	switch id.Role {
	case connproto.RoleAdmin:
		return id, nil, nil
	case connproto.RoleDeploy:
		check, listed := deployAllowed[method]
		if !listed {
			return id, nil, deny(id, method, "this key can only deploy")
		}
		return id, check, nil
	default:
		// El sıkışma bunu zaten reddediyor; burası ikinci kat.
		return id, nil, deny(id, method, "invalid role")
	}
}

// deny, reddi günlüğe yazar ve PermissionDenied üretir.
//
// Günlük satırı burada, önleyici zincirinin sırasına bağlı değil: akış
// RPC'leri günlük önleyicisinden geçmiyor ve ret sessiz kalmamalı (K-095).
func deny(id connproto.Identity, method, why string) error {
	slog.Warn("yetki reddedildi",
		"metot", method, "rol", id.Role, "kapsam", id.Apps,
		"anahtar", orUnknown(id.Fingerprint), "kaynak_ip", orUnknown(id.SourceIP))
	return status.Errorf(codes.PermissionDenied, "%s (%s)", why, method)
}

// AuthzUnaryInterceptor, tekli RPC'lerde yetkiyi uygular.
func AuthzUnaryInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		id, check, err := authorize(ctx, info.FullMethod)
		if err != nil {
			return nil, err
		}
		if check != nil && !check(id, req) {
			return nil, deny(id, info.FullMethod, "app is outside this key's scope")
		}
		return handler(ctx, req)
	}
}

// AuthzStreamInterceptor, akış RPC'lerinde yetkiyi uygular.
//
// Kapsam denetimi istek GÖVDESİNE bakıyor ve akışta gövde, işleyici
// RecvMsg çağırınca okunuyor. Üretilen işleyici RecvMsg'i srv.Deploy'dan
// ÖNCE çağırıyor (api_grpc.pb.go); sarmalayıcı her mesajı orada denetler,
// yani kapsam dışı bir istekte Deploy'un tek satırı bile çalışmaz.
func AuthzStreamInterceptor() grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		id, check, err := authorize(ss.Context(), info.FullMethod)
		if err != nil {
			return err
		}
		if check != nil {
			ss = &scopedStream{ServerStream: ss, id: id, method: info.FullMethod, check: check}
		}
		return handler(srv, ss)
	}
}

type scopedStream struct {
	grpc.ServerStream
	id     connproto.Identity
	method string
	check  scopeCheck
}

func (s *scopedStream) RecvMsg(m any) error {
	if err := s.ServerStream.RecvMsg(m); err != nil {
		return err
	}
	if !s.check(s.id, m) {
		return deny(s.id, s.method, "app is outside this key's scope")
	}
	return nil
}

// NewGRPCServer, api.sock'ta dinleyen gRPC sunucusunu kurar.
//
// main.go ile testler AYNI kurucuyu kullanıyor: yetki önleyicisinin
// kayıtlı olduğunu ancak böyle bir test kanıtlayabilir.
func NewGRPCServer(service *Server, creds credentials.TransportCredentials) *grpc.Server {
	server := grpc.NewServer(
		grpc.Creds(creds),
		grpc.ChainUnaryInterceptor(LoggingInterceptor(), AuthzUnaryInterceptor()),
		grpc.ChainStreamInterceptor(AuthzStreamInterceptor()),
	)
	kadranv1.RegisterKadranServiceServer(server, service)
	return server
}
