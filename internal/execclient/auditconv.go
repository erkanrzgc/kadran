package execclient

import (
	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"

	"github.com/erkanrzgc/kadran/internal/audit"
)

// ── Neden bu dönüşümler pbconv'da DEĞİL ─────────────────────────────
//
// `internal/pbconv` her iki yönü de taşıyordu ve `cmd/kadran-exec`'in
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
func auditRecordsFromProto(msgs []*kadranv1.AuditRecord) []audit.Record {
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
func auditRecordFromProto(m *kadranv1.AuditRecord) audit.Record {
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

func outcomeFromProto(o kadranv1.AuditOutcome) audit.Outcome {
	switch o {
	case kadranv1.AuditOutcome_AUDIT_OUTCOME_SUCCESS:
		return audit.OutcomeSuccess
	case kadranv1.AuditOutcome_AUDIT_OUTCOME_FAILURE:
		return audit.OutcomeFailure
	case kadranv1.AuditOutcome_AUDIT_OUTCOME_DENIED:
		return audit.OutcomeDenied
	default:
		// Sıfır değer geçersizdir ve audit.Verifier tarafından reddedilir.
		// Sessizce geçerli bir değere eşlemek, tanımsız bir durumu zincire
		// sokmak olurdu.
		return audit.Outcome(0)
	}
}

func sourceFromProto(s kadranv1.AuditSource) audit.Source {
	switch s {
	case kadranv1.AuditSource_AUDIT_SOURCE_DAEMON:
		return audit.SourceDaemon
	case kadranv1.AuditSource_AUDIT_SOURCE_EXECUTOR:
		return audit.SourceExecutor
	default:
		return audit.Source(0)
	}
}
