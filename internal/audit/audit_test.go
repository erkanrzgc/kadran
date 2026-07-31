package audit

import (
	"errors"
	"testing"
	"time"
)

// buildChain, ardışık olarak mühürlenmiş bir test zinciri üretir.
func buildChain(t *testing.T, n int) []Record {
	t.Helper()

	base := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	records := make([]Record, 0, n)
	prev := GenesisHash

	for i := range n {
		r := Record{
			TS: base.Add(time.Duration(i) * time.Second),
			Actor: Actor{
				KeyFingerprint: "SHA256:AAAABBBBCCCC",
				SourceIP:       "203.0.113.7",
				Label:          "erkan@laptop",
				Origin:         "cli",
			},
			Action:     "app.deploy",
			Target:     "app/blog",
			ParamsJSON: `{"release":"abc123"}`,
			Outcome:    OutcomeSuccess,
			Detail:     "ok",
			Source:     SourceDaemon,
		}
		sealed := Seal(r, uint64(i+1), prev)
		prev = sealed.Hash
		records = append(records, sealed)
	}
	return records
}

func TestSealedChainVerifies(t *testing.T) {
	// Arrange
	records := buildChain(t, 5)

	// Act
	checked, err := VerifyAll(records)

	// Assert
	if err != nil {
		t.Fatalf("geçerli zincir doğrulanamadı: %v", err)
	}
	if checked != 5 {
		t.Errorf("doğrulanan kayıt sayısı = %d, beklenen 5", checked)
	}
}

func TestEmptyChainVerifies(t *testing.T) {
	checked, err := VerifyAll(nil)
	if err != nil {
		t.Fatalf("boş zincir doğrulanamadı: %v", err)
	}
	if checked != 0 {
		t.Errorf("doğrulanan kayıt sayısı = %d, beklenen 0", checked)
	}
}

func TestFirstRecordUsesGenesisPrevHash(t *testing.T) {
	records := buildChain(t, 1)
	if records[0].PrevHash != GenesisHash {
		t.Errorf("ilk kaydın prev_hash değeri genesis olmalı, %x bulundu", records[0].PrevHash)
	}
}

func TestVerifierDetectsTamperedField(t *testing.T) {
	// Arrange: geçerli zincirin ortasındaki kaydın hedefi değiştirilir,
	// hash'i olduğu gibi bırakılır — bir saldırganın yapacağı en basit
	// müdahale budur.
	records := buildChain(t, 5)
	records[2].Target = "app/kurbanlik"

	// Act
	checked, err := VerifyAll(records)

	// Assert
	if err == nil {
		t.Fatal("kurcalanmış kayıt tespit edilmedi")
	}
	if !errors.Is(err, ErrChainBroken) {
		t.Errorf("hata ErrChainBroken sarmalamalı, %v alındı", err)
	}
	if checked != 2 {
		t.Errorf("kurcalamadan önce %d kayıt doğrulanmalıydı, %d oldu", 2, checked)
	}
}

func TestVerifierDetectsTamperedActorIP(t *testing.T) {
	// IP hash yüküne dahil olmalı (§1.3). Değiştirilirse yakalanmalı.
	records := buildChain(t, 3)
	records[1].Actor.SourceIP = "198.51.100.99"

	if _, err := VerifyAll(records); err == nil {
		t.Fatal("aktör IP'sinin değiştirilmesi tespit edilmedi — IP hash yükünde değil")
	}
}

func TestVerifierDetectsTamperedKeyFingerprint(t *testing.T) {
	// Parmak izi gerçek kimliktir; başka birinin üzerine atılamamalı.
	records := buildChain(t, 3)
	records[1].Actor.KeyFingerprint = "SHA256:BASKASININANAHTARI"

	if _, err := VerifyAll(records); err == nil {
		t.Fatal("aktör parmak izinin değiştirilmesi tespit edilmedi")
	}
}

func TestVerifierDetectsRecomputedHashWithoutRechaining(t *testing.T) {
	// Daha akıllı bir saldırgan alanı değiştirip hash'i yeniden hesaplar
	// ama sonraki kayıtları yeniden zincirlemez. Kopma bir sonraki
	// kayıtta yakalanmalı.
	records := buildChain(t, 5)
	records[2].Detail = "sessizce degistirildi"
	records[2].Hash = ComputeHash(records[2])

	checked, err := VerifyAll(records)
	if err == nil {
		t.Fatal("yeniden zincirlenmemiş kurcalama tespit edilmedi")
	}
	if checked != 3 {
		t.Errorf("kopma 4. kayıtta yakalanmalıydı (3 geçerli), %d doğrulandı", checked)
	}
}

func TestVerifierDetectsDeletedRecord(t *testing.T) {
	// Ortadaki kaydın silinmesi hem sıra numarasını hem zinciri bozar.
	records := buildChain(t, 5)
	spliced := append(append([]Record{}, records[:2]...), records[3:]...)

	if _, err := VerifyAll(spliced); err == nil {
		t.Fatal("silinen kayıt tespit edilmedi")
	}
}

func TestVerifierDetectsReorderedRecords(t *testing.T) {
	records := buildChain(t, 5)
	records[1], records[2] = records[2], records[1]

	if _, err := VerifyAll(records); err == nil {
		t.Fatal("sırası değiştirilen kayıtlar tespit edilmedi")
	}
}

func TestVerifierRejectsInvalidOutcome(t *testing.T) {
	// Geçersiz enum değeri, hash tutarlı olsa bile reddedilmeli:
	// tanımsız bir durum zincire giremez.
	r := Record{
		TS:      time.Now(),
		Action:  "test",
		Outcome: Outcome(99),
		Source:  SourceDaemon,
	}
	sealed := Seal(r, 1, GenesisHash)

	if _, err := VerifyAll([]Record{sealed}); err == nil {
		t.Fatal("geçersiz outcome değeri kabul edildi")
	}
}

func TestVerifierRejectsInvalidSource(t *testing.T) {
	r := Record{
		TS:      time.Now(),
		Action:  "test",
		Outcome: OutcomeSuccess,
		Source:  Source(42),
	}
	sealed := Seal(r, 1, GenesisHash)

	if _, err := VerifyAll([]Record{sealed}); err == nil {
		t.Fatal("geçersiz source değeri kabul edildi")
	}
}

// TestLengthPrefixPreventsFieldBoundaryCollision, kanonik kodlamanın
// uzunluk öneki kullanmasının sebebini doğrular.
//
// Uzunluk öneki olmasaydı ("ab" + "c") ile ("a" + "bc") aynı bayt dizisine
// serileşir ve iki farklı kayıt aynı hash'i üretirdi. Bu, bir saldırganın
// alan sınırlarını kaydırarak eylemin anlamını değiştirmesine izin verirdi.
func TestLengthPrefixPreventsFieldBoundaryCollision(t *testing.T) {
	ts := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)

	a := Seal(Record{
		TS: ts, Action: "ab", Target: "c",
		Outcome: OutcomeSuccess, Source: SourceDaemon,
	}, 1, GenesisHash)

	b := Seal(Record{
		TS: ts, Action: "a", Target: "bc",
		Outcome: OutcomeSuccess, Source: SourceDaemon,
	}, 1, GenesisHash)

	if a.Hash == b.Hash {
		t.Fatal("alan sınırı çakışması: ('ab','c') ile ('a','bc') aynı hash'i üretti")
	}
}

func TestVerifierHeadTracksLastHash(t *testing.T) {
	records := buildChain(t, 4)

	v := NewVerifier()
	for _, r := range records {
		if err := v.Next(r); err != nil {
			t.Fatalf("beklenmedik doğrulama hatası: %v", err)
		}
	}

	if v.Head() != records[3].Hash {
		t.Error("Head() son kaydın hash'ini döndürmeli")
	}
	if v.NextSeq() != 5 {
		t.Errorf("NextSeq() = %d, beklenen 5", v.NextSeq())
	}
}

func TestVerifierRejectsSeqStartingAtZero(t *testing.T) {
	// Sıra numarası 1'den başlar. 0 geçersizdir.
	r := Seal(Record{
		TS: time.Now(), Action: "test",
		Outcome: OutcomeSuccess, Source: SourceDaemon,
	}, 0, GenesisHash)

	if _, err := VerifyAll([]Record{r}); err == nil {
		t.Fatal("seq=0 olan kayıt kabul edildi")
	}
}

func TestComputeHashIsDeterministic(t *testing.T) {
	r := Record{
		TS:         time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC),
		Actor:      Actor{KeyFingerprint: "SHA256:X", SourceIP: "10.0.0.1", Origin: "cli"},
		Action:     "app.deploy",
		Target:     "app/blog",
		ParamsJSON: `{"a":1}`,
		Outcome:    OutcomeSuccess,
		Source:     SourceDaemon,
		PrevHash:   GenesisHash,
	}

	// Aynı ifadenin iki kez çağrılması KASITLI: sınanan şey ComputeHash'in
	// deterministik olduğu. Zincirin tamamı buna dayanıyor — aynı kayıt
	// farklı hash üretirse doğrulama rastgele başarısız olurdu.
	//
	//nolint:staticcheck // SA4000: determinizm sınanıyor, ifade tekrarı kasıtlı
	if ComputeHash(r) != ComputeHash(r) {
		t.Fatal("ComputeHash deterministik değil")
	}
}
