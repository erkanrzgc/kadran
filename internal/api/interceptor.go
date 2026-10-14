package api

import (
	"context"
	"log/slog"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

// LoggingInterceptor, her isteği çağıranın kimliğiyle birlikte günlüğe yazar.
//
// # Neden denetim kaydı değil de günlük?
//
// Faz 0'da istemciye açılan RPC'lerin hepsi SALT OKUNURDUR (Ping,
// GetSystemInfo, ListAuditRecords, VerifyAuditChain). Bunları denetim
// zincirine yazmak, zinciri durum ekranının yenilenme gürültüsüyle
// doldurur ve asıl önemli olan ayrıcalıklı işlemleri görünmez kılardı —
// her şeyi kaydeden bir günlük, hiçbir şey kaydetmemeye yaklaşır.
//
// Faz 1'de durum değiştiren RPC'ler geldiğinde onlar zincire yazılacak;
// bu interceptor'ın topladığı aktör bilgisi o kayıtların "kim yaptı"
// alanını dolduracak (bkz. actorFromContext).
func LoggingInterceptor() grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		started := time.Now()
		resp, err := handler(ctx, req)

		actor := actorFromContext(ctx)
		attrs := []any{
			"method", info.FullMethod,
			"duration_ms", time.Since(started).Milliseconds(),
			"source_ip", orUnknown(actor.SourceIP),
			"key", orUnknown(actor.KeyFingerprint),
			"origin", actor.Origin,
		}

		// SO_PEERCRED ile çekirdekten alınan unix kimliği. Bunu ve SSH
		// kimliğini birlikte günlüğe yazmak, ikisi arasında beklenmedik bir
		// tutarsızlık oluşursa fark edilmesini sağlar.
		if info, ok := callerFromContext(ctx); ok {
			attrs = append(attrs, "unix_uid", info.Unix.UID, "unix_pid", info.Unix.PID)
		}

		if err != nil {
			attrs = append(attrs, "code", status.Code(err).String(), "error", err.Error())
			slog.Warn("request failed", attrs...)
			return resp, err
		}
		slog.Debug("request completed", attrs...)
		return resp, nil
	}
}

func orUnknown(s string) string {
	if s == "" {
		return "bilinmiyor"
	}
	return s
}
