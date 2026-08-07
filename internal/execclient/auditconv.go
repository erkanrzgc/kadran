package execclient

import (
	panelyv1 "github.com/erkanrzgc/panely/internal/pb/panely/v1"

	"github.com/erkanrzgc/panely/internal/audit"
)

// ── Neden bu dönüşümler pbconv'da DEĞİL ─────────────────────────────
//
// `internal/pbconv` her iki yönü de taşıyordu ve `cmd/panely-exec`'in
// içe aktarma grafiğinde — yani ayrıcalıklı yüzey bütçesine yazılıyor
// (scripts/check-exec-surface.sh).
//
// Ama executor o paketten YALNIZCA `AuditRecordsToProto`'yu çağırıyor:
// kendi günlüğünü dışarı serileştiriyor. Ters yönü (protobuf → iç kayıt)
// yalnızca BU paket kullanıyor, çünkü executor'ın yanıtını okuyan taraf
// daemon.
//
// Ölçüldü: ters yön 58 kod satırı ve root süreçte HİÇ ÇALIŞMIYOR.
// Ayrıcalıklı binary'nin, çalıştırmadığı çözümleme kodunu taşıması için
// bir sebep yok — hem bütçeyi hem denetlenecek yüzeyi büyütüyordu.
//
// Bu taşıma K-040'ın frenidir: sınır yükseltilmeden ÖNCE küçültme
// seçeneği aranmalı. Arandı ve bulundu (K-053).

// auditRecordsFromProto, protobuf kayıt dilimini iç kayda çevirir.
func auditRecordsFromProto(msgs []*panelyv1.AuditRecord) []audit.Record {
	if len(msgs) == 0 {
		return nil
	}
	out := make([]audit.Record, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, auditRecordFromProto(m))
	}
	return out
}

// auditRecordFromProto, protobuf mesajını iç kayda çevirir.
//
// Hash alanları beklenen uzunlukta değilse sıfır bırakılır; doğrulama
// audit.Verifier'ın işidir ve orada zaten başarısız olur.
func auditRecordFromProto(m *panelyv1.AuditRecord) audit.Record {
	if m == nil {
		return audit.Record{}
	}

	rec := audit.Record{
		Seq:        m.GetSeq(),
		TS:         m.GetTs().AsTime(),
		Action:     m.GetAction(),
		Target:     m.GetTarget(),
		ParamsJSON: m.GetParamsJson(),
		Outcome:    outcomeFromProto(m.GetOutcome()),
		Detail:     m.GetDetail(),
		Source:     sourceFromProto(m.GetSource()),
	}
	if a := m.GetActor(); a != nil {
		rec.Actor = audit.Actor{
			KeyFingerprint: a.GetSshKeyFingerprint(),
			SourceIP:       a.GetSourceIp(),
			Label:          a.GetLabel(),
			Origin:         a.GetOrigin(),
		}
	}
	copy(rec.PrevHash[:], m.GetPrevHash())
	copy(rec.Hash[:], m.GetHash())
	return rec
}

func outcomeFromProto(o panelyv1.AuditOutcome) audit.Outcome {
	switch o {
	case panelyv1.AuditOutcome_AUDIT_OUTCOME_SUCCESS:
		return audit.OutcomeSuccess
	case panelyv1.AuditOutcome_AUDIT_OUTCOME_FAILURE:
		return audit.OutcomeFailure
	case panelyv1.AuditOutcome_AUDIT_OUTCOME_DENIED:
		return audit.OutcomeDenied
	default:
		// Sıfır değer geçersizdir ve audit.Verifier tarafından reddedilir.
		// Sessizce geçerli bir değere eşlemek, tanımsız bir durumu zincire
		// sokmak olurdu.
		return audit.Outcome(0)
	}
}

func sourceFromProto(s panelyv1.AuditSource) audit.Source {
	switch s {
	case panelyv1.AuditSource_AUDIT_SOURCE_DAEMON:
		return audit.SourceDaemon
	case panelyv1.AuditSource_AUDIT_SOURCE_EXECUTOR:
		return audit.SourceExecutor
	default:
		return audit.Source(0)
	}
}
