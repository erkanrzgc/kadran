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

	"github.com/erkanrzgc/kadran/internal/audit"
	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
)

// AuditRecordToProto, iç denetim kaydını protobuf mesajına çevirir.
func AuditRecordToProto(r audit.Record) *kadranv1.AuditRecord {
	return &kadranv1.AuditRecord{
		Seq: r.Seq,
		Ts:  timestamppb.New(r.TS),
		Actor: &kadranv1.Actor{
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
func AuditRecordsToProto(records []audit.Record) []*kadranv1.AuditRecord {
	if len(records) == 0 {
		return nil
	}
	out := make([]*kadranv1.AuditRecord, 0, len(records))
	for _, r := range records {
		out = append(out, AuditRecordToProto(r))
	}
	return out
}

func outcomeToProto(o audit.Outcome) kadranv1.AuditOutcome {
	switch o {
	case audit.OutcomeSuccess:
		return kadranv1.AuditOutcome_AUDIT_OUTCOME_SUCCESS
	case audit.OutcomeFailure:
		return kadranv1.AuditOutcome_AUDIT_OUTCOME_FAILURE
	case audit.OutcomeDenied:
		return kadranv1.AuditOutcome_AUDIT_OUTCOME_DENIED
	default:
		return kadranv1.AuditOutcome_AUDIT_OUTCOME_UNSPECIFIED
	}
}

func sourceToProto(s audit.Source) kadranv1.AuditSource {
	switch s {
	case audit.SourceDaemon:
		return kadranv1.AuditSource_AUDIT_SOURCE_DAEMON
	case audit.SourceExecutor:
		return kadranv1.AuditSource_AUDIT_SOURCE_EXECUTOR
	default:
		return kadranv1.AuditSource_AUDIT_SOURCE_UNSPECIFIED
	}
}
