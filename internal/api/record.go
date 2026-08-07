package api

import (
	"context"
	"log/slog"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/erkanrzgc/panely/internal/audit"
)

// ── Daemon tarafı denetim kayıtları ──────────────────────────────────
//
// Bu yardımcılar internal/exec/record.go'daki eşlerini AYNA gibi izler
// ama zincirleri AYRIDIR ve ayrı kalmalıdır:
//
//   daemon   → SQLite'taki audit_log (bu dosya)
//   executor → kendi dosyası, 0640 root:panely (panelyd YAZAMAZ)
//
// Ayrılık tehdit modelinin merkezinde: panelyd ele geçirilirse kendi
// kayıtlarını hiç yazmayabilir, ama executor'ınkileri değiştiremez.
// İki zincirin KARŞILAŞTIRILMASI bu farkı ortaya çıkarır — daemon
// tarafında karşılığı olmayan bir executor kaydı, panelyd'nin sustuğu
// anlamına gelir.
//
// LoggingInterceptor bunu yazmıyor ve yazmamalı: Faz 0'ın salt okunur
// RPC'lerini zincire koymak, durum ekranının yenilenme gürültüsüyle asıl
// önemli olan durum değiştiren işlemleri görünmez kılardı. Zincire
// YALNIZCA durum değiştiren eylemler girer.

// recordAction, daemon zincirine bir kayıt ekler.
//
// Kayıt yazılamazsa hata döner ve çağıran işlemi başarısız sayar:
// sessizce kayıtsız kalmasındansa çağıranın bunu bilmesi yeğdir.
func (s *Server) recordAction(
	ctx context.Context,
	action, target string,
	params map[string]string,
	outcome audit.Outcome,
	detail string,
) error {
	paramsJSON, err := audit.MarshalParams(audit.RedactSensitive(params))
	if err != nil {
		return status.Errorf(codes.Internal, "denetim parametreleri kodlanamadı: %v", err)
	}

	rec := audit.Record{
		Actor:      actorFromContext(ctx),
		Action:     action,
		Target:     target,
		ParamsJSON: paramsJSON,
		Outcome:    outcome,
		Detail:     detail,
		Source:     audit.SourceDaemon,
	}

	// ⚠ Bağlam KASTEN devredilmiyor: context.WithoutCancel.
	//
	// Denetim kaydı, isteğin ömrüne bağlı OLAMAZ. Dağıtım akışında
	// istemci koptuğunda bağlam iptal olur ve kayıt tam o anda —
	// yani en çok ihtiyaç duyulduğu anda — yazılamazdı. "Kullanıcı
	// bağlantıyı keserek kaydı engelleyebiliyor" bir denetim günlüğü
	// için kabul edilemez.
	if _, err := s.store.AppendAudit(context.WithoutCancel(ctx), rec); err != nil {
		slog.Error("denetim kaydı yazılamadı",
			"eylem", action, "hedef", target, "hata", err)
		return status.Errorf(codes.Internal, "denetim kaydı yazılamadı: %v", err)
	}
	return nil
}

// denied, doğrulamada reddedilen bir isteği kaydeder ve gRPC hatası döner.
//
// Reddetme sebebi bizim ürettiğimiz doğrulama mesajıdır — kullanıcı
// verisi değil — bu yüzden detaya yazılabilir.
func (s *Server) denied(
	ctx context.Context, action, target string,
	params map[string]string, cause error,
) error {
	_ = s.recordAction(ctx, action, target, params, audit.OutcomeDenied, cause.Error())
	return status.Error(codes.InvalidArgument, cause.Error())
}

// completed, gerçekleşmiş bir işlemi kaydeder.
//
// opErr nil ise başarı, değilse başarısızlık yazılır.
func (s *Server) completed(
	ctx context.Context, action, target string,
	params map[string]string, opErr error,
) error {
	outcome, detail := audit.OutcomeSuccess, ""
	if opErr != nil {
		outcome = audit.OutcomeFailure
		// ⚠ opErr'in METNİ kayda GİRMEZ. Derleme hatası kullanıcının
		// deposundan gelen metni taşıyabilir (Dockerfile satırları,
		// derleyici çıktısı) ve zincir ekle-sadece'dir: bir kez yazılan
		// sır geri alınamaz. Ayrıntı çağırana döner.
		detail = "işlem başarısız (ayrıntı çağırana döndü, kayda yazılmadı)"
	}
	if err := s.recordAction(ctx, action, target, params, outcome, detail); err != nil {
		return err
	}
	return opErr
}

// auditFailure, denetim sonucu sabitine kısa ad. Bu dosyadaki
// yardımcıların dışında doğrudan recordAction çağıran birkaç yer var
// (dağıtımda arama başarısızlıkları) ve orada tam nitelikli ad satırı
// gereksiz uzatıyor.
const auditFailure = audit.OutcomeFailure
