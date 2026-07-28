// Package pbconv, iç veri tipleri ile protobuf mesajları arasında çeviri
// yapar.
//
// Bu paket, internal/audit'in saf kalmasını sağlamak için vardır: zincir
// matematiği protobuf'a bağımlı olmamalı ki üretilen kod olmadan test
// edilebilsin. Çeviri hem executor hem daemon tarafından kullanıldığı için
// ortak bir yere konmuştur.
package pbconv

import (
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/erkanrzgc/panely/internal/audit"
	panelyv1 "github.com/erkanrzgc/panely/internal/pb/panely/v1"
)

// AuditRecordToProto, iç denetim kaydını protobuf mesajına çevirir.
func AuditRecordToProto(r audit.Record) *panelyv1.AuditRecord {
	return &panelyv1.AuditRecord{
		Seq: r.Seq,
		Ts:  timestamppb.New(r.TS),
		Actor: &panelyv1.Actor{
			SshKeyFingerprint: r.Actor.KeyFingerprint,
			SourceIp:          r.Actor.SourceIP,
			Label:             r.Actor.Label,
			Origin:            r.Actor.Origin,
		},
		Action:     r.Action,
		Target:     r.Target,
		ParamsJson: r.ParamsJSON,
		Outcome:    outcomeToProto(r.Outcome),
		Detail:     r.Detail,
		PrevHash:   append([]byte(nil), r.PrevHash[:]...),
		Hash:       append([]byte(nil), r.Hash[:]...),
		Source:     sourceToProto(r.Source),
	}
}

// AuditRecordsToProto, kayıt dilimini çevirir.
func AuditRecordsToProto(records []audit.Record) []*panelyv1.AuditRecord {
	if len(records) == 0 {
		return nil
	}
	out := make([]*panelyv1.AuditRecord, 0, len(records))
	for _, r := range records {
		out = append(out, AuditRecordToProto(r))
	}
	return out
}

// AuditRecordFromProto, protobuf mesajını iç kayda çevirir.
//
// Hash alanları beklenen uzunlukta değilse sıfır bırakılır; doğrulama
// audit.Verifier'ın işidir ve orada zaten başarısız olur.
func AuditRecordFromProto(m *panelyv1.AuditRecord) audit.Record {
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

// AuditRecordsFromProto, protobuf kayıt dilimini çevirir.
func AuditRecordsFromProto(msgs []*panelyv1.AuditRecord) []audit.Record {
	if len(msgs) == 0 {
		return nil
	}
	out := make([]audit.Record, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, AuditRecordFromProto(m))
	}
	return out
}

func outcomeToProto(o audit.Outcome) panelyv1.AuditOutcome {
	switch o {
	case audit.OutcomeSuccess:
		return panelyv1.AuditOutcome_AUDIT_OUTCOME_SUCCESS
	case audit.OutcomeFailure:
		return panelyv1.AuditOutcome_AUDIT_OUTCOME_FAILURE
	case audit.OutcomeDenied:
		return panelyv1.AuditOutcome_AUDIT_OUTCOME_DENIED
	default:
		return panelyv1.AuditOutcome_AUDIT_OUTCOME_UNSPECIFIED
	}
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

func sourceToProto(s audit.Source) panelyv1.AuditSource {
	switch s {
	case audit.SourceDaemon:
		return panelyv1.AuditSource_AUDIT_SOURCE_DAEMON
	case audit.SourceExecutor:
		return panelyv1.AuditSource_AUDIT_SOURCE_EXECUTOR
	default:
		return panelyv1.AuditSource_AUDIT_SOURCE_UNSPECIFIED
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
