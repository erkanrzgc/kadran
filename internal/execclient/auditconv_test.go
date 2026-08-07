// Gidiş-dönüş testleri BU pakette, pbconv'da değil.
//
// Çözümleme yönü (protobuf → iç kayıt) buraya taşındı: ayrıcalıklı
// binary onu hiç çalıştırmıyordu ve `internal/pbconv` root süreç
// bütçesine yazılıyor (docs/decisions.md K-053). Gidiş-dönüş testi iki
// yöne de ihtiyaç duyduğu için, ikisinin de görünür olduğu tek yer
// burası.

package execclient

import (
	"testing"
	"time"

	"github.com/erkanrzgc/panely/internal/audit"
	"github.com/erkanrzgc/panely/internal/pbconv"
)

// TestRoundTripPreservesChainIntegrity, çevirinin zinciri BOZMADIĞINI
// doğrular.
//
// Bu testin asıl konusu alanların eşitliği değil, hash'in korunmasıdır:
// panelyd, executor'ın kayıtlarını protobuf üzerinden alıp kendi
// doğrulayıcısından geçirir. Çeviri tek bir alanı bile düşürürse
// ComputeHash farklı sonuç verir ve çapraz doğrulama yanlış yere
// "kurcalanmış" der.
func TestRoundTripPreservesChainIntegrity(t *testing.T) {
	original := audit.Seal(audit.Record{
		TS: time.Date(2026, 7, 27, 12, 34, 56, 789, time.UTC),
		Actor: audit.Actor{
			KeyFingerprint: "SHA256:AAAABBBB",
			SourceIP:       "203.0.113.7",
			Label:          "erkan@laptop",
			Origin:         "cli",
		},
		Action:     "container.create",
		Target:     "container/blog-1",
		ParamsJSON: `{"image":"panely/blog:abc","env":"[REDACTED]"}`,
		Outcome:    audit.OutcomeDenied,
		Detail:     "hacim adı geçersiz",
		Source:     audit.SourceExecutor,
	}, 7, [audit.HashSize]byte{1, 2, 3})

	got := auditRecordFromProto(pbconv.AuditRecordToProto(original))

	if got.Hash != original.Hash {
		t.Error("hash gidiş-dönüşte korunmadı")
	}
	if want := audit.ComputeHash(got); want != original.Hash {
		t.Error("çevrilen kayıttan hesaplanan hash orijinaliyle uyuşmuyor — bir alan düşüyor")
	}
}

func TestRoundTripPreservesEveryField(t *testing.T) {
	original := audit.Seal(audit.Record{
		TS: time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC),
		Actor: audit.Actor{
			KeyFingerprint: "SHA256:X",
			SourceIP:       "198.51.100.4",
			Label:          "etiket",
			Origin:         "scheduler",
		},
		Action:     "app.deploy",
		Target:     "app/blog",
		ParamsJSON: `{"release":"r1"}`,
		Outcome:    audit.OutcomeSuccess,
		Detail:     "tamam",
		Source:     audit.SourceDaemon,
	}, 3, [audit.HashSize]byte{9})

	got := auditRecordFromProto(pbconv.AuditRecordToProto(original))

	if got.Seq != original.Seq {
		t.Errorf("seq = %d, beklenen %d", got.Seq, original.Seq)
	}
	if !got.TS.Equal(original.TS) {
		t.Errorf("ts = %v, beklenen %v", got.TS, original.TS)
	}
	if got.Actor != original.Actor {
		t.Errorf("aktör = %+v, beklenen %+v", got.Actor, original.Actor)
	}
	if got.Action != original.Action || got.Target != original.Target ||
		got.ParamsJSON != original.ParamsJSON || got.Detail != original.Detail {
		t.Error("metin alanları korunmadı")
	}
	if got.Outcome != original.Outcome {
		t.Errorf("outcome = %v, beklenen %v", got.Outcome, original.Outcome)
	}
	if got.Source != original.Source {
		t.Errorf("source = %v, beklenen %v", got.Source, original.Source)
	}
	if got.PrevHash != original.PrevHash {
		t.Error("prev_hash korunmadı")
	}
}

func TestAllOutcomesRoundTrip(t *testing.T) {
	for _, o := range []audit.Outcome{
		audit.OutcomeSuccess, audit.OutcomeFailure, audit.OutcomeDenied,
	} {
		rec := audit.Record{TS: time.Now().UTC(), Action: "x", Outcome: o, Source: audit.SourceDaemon}
		if got := auditRecordFromProto(pbconv.AuditRecordToProto(rec)); got.Outcome != o {
			t.Errorf("outcome %v gidiş-dönüşte %v oldu", o, got.Outcome)
		}
	}
}

func TestAllSourcesRoundTrip(t *testing.T) {
	for _, s := range []audit.Source{audit.SourceDaemon, audit.SourceExecutor} {
		rec := audit.Record{TS: time.Now().UTC(), Action: "x", Outcome: audit.OutcomeSuccess, Source: s}
		if got := auditRecordFromProto(pbconv.AuditRecordToProto(rec)); got.Source != s {
			t.Errorf("source %v gidiş-dönüşte %v oldu", s, got.Source)
		}
	}
}

// TestUnspecifiedEnumsStayInvalid, tanımsız enum değerlerinin sessizce
// geçerli bir değere eşlenmediğini doğrular.
//
// UNSPECIFIED'ı SUCCESS'e çevirmek, bozuk bir kaydı zincire meşru gibi
// sokardı. Geçersiz kalması doğrudur: audit.Verifier onu reddeder.
func TestUnspecifiedEnumsStayInvalid(t *testing.T) {
	var zero audit.Record
	got := auditRecordFromProto(pbconv.AuditRecordToProto(zero))

	if got.Outcome.Valid() {
		t.Error("tanımsız outcome geçerli bir değere eşlendi")
	}
	if got.Source.Valid() {
		t.Error("tanımsız source geçerli bir değere eşlendi")
	}
}

func TestNilProtoIsSafe(t *testing.T) {
	if got := auditRecordFromProto(nil); got.Seq != 0 {
		t.Error("nil mesaj boş kayıt döndürmeli")
	}
}

func TestEmptySlicesRoundTrip(t *testing.T) {
	if got := pbconv.AuditRecordsToProto(nil); got != nil {
		t.Error("boş dilim nil döndürmeli")
	}
	if got := auditRecordsFromProto(nil); got != nil {
		t.Error("boş dilim nil döndürmeli")
	}
}

// TestSliceRoundTripKeepsChainVerifiable, tam bir zincirin protobuf'tan
// geçtikten sonra hâlâ doğrulanabilir olduğunu gösterir. panelyd'nin
// executor günlüğünü çapraz doğrulaması tam olarak bunu yapar.
func TestSliceRoundTripKeepsChainVerifiable(t *testing.T) {
	var (
		chain []audit.Record
		prev  = audit.GenesisHash
	)
	for i := range 5 {
		rec := audit.Seal(audit.Record{
			TS:      time.Date(2026, 7, 27, 12, 0, i, 0, time.UTC),
			Action:  "container.create",
			Outcome: audit.OutcomeSuccess,
			Source:  audit.SourceExecutor,
		}, uint64(i+1), prev)
		prev = rec.Hash
		chain = append(chain, rec)
	}

	got := auditRecordsFromProto(pbconv.AuditRecordsToProto(chain))

	checked, err := audit.VerifyAll(got)
	if err != nil {
		t.Fatalf("protobuf'tan geçen zincir doğrulanamadı: %v", err)
	}
	if checked != 5 {
		t.Errorf("doğrulanan kayıt sayısı = %d, beklenen 5", checked)
	}
}
