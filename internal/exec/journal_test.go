package exec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/erkanrzgc/panely/internal/audit"
)

func newTestJournal(t *testing.T) (*Journal, string) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "exec-audit.log")
	j, err := OpenJournal(JournalOptions{Path: path})
	if err != nil {
		t.Fatalf("günlük açılamadı: %v", err)
	}
	t.Cleanup(func() { _ = j.Close() })
	return j, path
}

func execRecord(action string) audit.Record {
	return audit.Record{
		Actor:      audit.SystemActor("executor"),
		Action:     action,
		Target:     "container/blog-1",
		ParamsJSON: `{"image":"panely/blog:abc"}`,
		Outcome:    audit.OutcomeSuccess,
	}
}

func TestOpenJournalCreatesEmptyChain(t *testing.T) {
	j, path := newTestJournal(t)

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("günlük dosyası oluşturulmadı: %v", err)
	}

	seq, head := j.Head()
	if seq != 0 {
		t.Errorf("boş günlükte seq = %d, beklenen 0", seq)
	}
	if head != audit.GenesisHash {
		t.Error("boş günlükte baş genesis olmalı")
	}
}

func TestAppendChainsRecords(t *testing.T) {
	j, _ := newTestJournal(t)

	first, err := j.Append(execRecord("container.create"))
	if err != nil {
		t.Fatalf("ilk kayıt eklenemedi: %v", err)
	}
	second, err := j.Append(execRecord("container.start"))
	if err != nil {
		t.Fatalf("ikinci kayıt eklenemedi: %v", err)
	}

	if first.Seq != 1 || second.Seq != 2 {
		t.Errorf("sıra numaraları beklenmedik: %d, %d", first.Seq, second.Seq)
	}
	if first.PrevHash != audit.GenesisHash {
		t.Error("ilk kaydın prev_hash değeri genesis olmalı")
	}
	if second.PrevHash != first.Hash {
		t.Error("ikinci kayıt ilkine zincirlenmemiş")
	}
}

// TestAppendForcesExecutorSource, çağıranın kaynağı yanlış beyan
// edemeyeceğini doğrular. Executor'ın yazdığı her kayıt executor
// kaynaklıdır; aksi hâlde ayrıcalıklı bir işlem daemon eylemi gibi
// görünebilirdi.
func TestAppendForcesExecutorSource(t *testing.T) {
	j, _ := newTestJournal(t)

	rec := execRecord("container.create")
	rec.Source = audit.SourceDaemon

	sealed, err := j.Append(rec)
	if err != nil {
		t.Fatalf("kayıt eklenemedi: %v", err)
	}
	if sealed.Source != audit.SourceExecutor {
		t.Errorf("kaynak = %v, beklenen executor", sealed.Source)
	}
}

func TestAppendIgnoresCallerSuppliedSeq(t *testing.T) {
	j, _ := newTestJournal(t)

	rec := execRecord("container.create")
	rec.Seq = 500
	rec.PrevHash = [audit.HashSize]byte{0xAB}

	sealed, err := j.Append(rec)
	if err != nil {
		t.Fatalf("kayıt eklenemedi: %v", err)
	}
	if sealed.Seq != 1 {
		t.Errorf("çağıranın verdiği seq kullanıldı: %d", sealed.Seq)
	}
	if sealed.PrevHash != audit.GenesisHash {
		t.Error("çağıranın verdiği prev_hash kullanıldı")
	}
}

func TestAppendRejectsInvalidRecords(t *testing.T) {
	j, _ := newTestJournal(t)

	t.Run("boş action", func(t *testing.T) {
		rec := execRecord("x")
		rec.Action = ""
		if _, err := j.Append(rec); err == nil {
			t.Fatal("boş action kabul edildi")
		}
	})
	t.Run("geçersiz outcome", func(t *testing.T) {
		rec := execRecord("x")
		rec.Outcome = audit.Outcome(0)
		if _, err := j.Append(rec); err == nil {
			t.Fatal("geçersiz outcome kabul edildi")
		}
	})
}

// TestReopenRestoresChainState, günlüğün kalıcı olduğunu ve yeniden
// açılışta zincirin doğru yerden devam ettiğini doğrular.
func TestReopenRestoresChainState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "exec-audit.log")

	j1, err := OpenJournal(JournalOptions{Path: path})
	if err != nil {
		t.Fatalf("ilk açılış başarısız: %v", err)
	}
	var lastHash [audit.HashSize]byte
	for range 3 {
		rec, err := j1.Append(execRecord("container.create"))
		if err != nil {
			t.Fatalf("kayıt eklenemedi: %v", err)
		}
		lastHash = rec.Hash
	}
	if err := j1.Close(); err != nil {
		t.Fatalf("kapatılamadı: %v", err)
	}

	j2, err := OpenJournal(JournalOptions{Path: path})
	if err != nil {
		t.Fatalf("yeniden açılış başarısız: %v", err)
	}
	defer func() { _ = j2.Close() }()

	seq, head := j2.Head()
	if seq != 3 {
		t.Errorf("yeniden açılışta seq = %d, beklenen 3", seq)
	}
	if head != lastHash {
		t.Error("yeniden açılışta zincir başı korunmadı")
	}

	next, err := j2.Append(execRecord("container.stop"))
	if err != nil {
		t.Fatalf("devam kaydı eklenemedi: %v", err)
	}
	if next.Seq != 4 {
		t.Errorf("devam kaydının sırası = %d, beklenen 4", next.Seq)
	}
	if next.PrevHash != lastHash {
		t.Error("devam kaydı önceki zincire bağlanmadı")
	}
}

// TestOpenJournalRefusesTamperedChain, açılışta bütünlük doğrulamasının
// çalıştığını gösterir.
//
// Bu davranış kasıtlı olarak serttir: zincir doğrulanamıyorsa executor
// BAŞLAMAZ. Denetim bütünlüğü şüpheliyken ayrıcalıklı işlem yapmak,
// hizmet vermemekten daha kötüdür.
func TestOpenJournalRefusesTamperedChain(t *testing.T) {
	path := filepath.Join(t.TempDir(), "exec-audit.log")

	j, err := OpenJournal(JournalOptions{Path: path})
	if err != nil {
		t.Fatalf("açılamadı: %v", err)
	}
	for range 3 {
		if _, err := j.Append(execRecord("container.create")); err != nil {
			t.Fatalf("kayıt eklenemedi: %v", err)
		}
	}
	if err := j.Close(); err != nil {
		t.Fatalf("kapatılamadı: %v", err)
	}

	// Ortadaki kaydın hedefini değiştir, hash'e dokunma.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("dosya okunamadı: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("beklenen 3 satır, %d bulundu", len(lines))
	}
	lines[1] = strings.Replace(lines[1], `"container/blog-1"`, `"container/kurban"`, 1)
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("dosya yazılamadı: %v", err)
	}

	if _, err := OpenJournal(JournalOptions{Path: path}); err == nil {
		t.Fatal("kurcalanmış günlük açıldı — bütünlük kontrolü çalışmıyor")
	}
}

func TestOpenJournalRefusesCorruptLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "exec-audit.log")
	if err := os.WriteFile(path, []byte("bu gecerli json degil\n"), 0o600); err != nil {
		t.Fatalf("dosya yazılamadı: %v", err)
	}

	if _, err := OpenJournal(JournalOptions{Path: path}); err == nil {
		t.Fatal("bozuk satır içeren günlük açıldı")
	}
}

func TestReadReturnsRecordsAfterSeq(t *testing.T) {
	j, _ := newTestJournal(t)

	for range 5 {
		if _, err := j.Append(execRecord("container.create")); err != nil {
			t.Fatalf("kayıt eklenemedi: %v", err)
		}
	}

	all, err := j.Read(0, 100)
	if err != nil {
		t.Fatalf("okunamadı: %v", err)
	}
	if len(all) != 5 {
		t.Fatalf("kayıt sayısı = %d, beklenen 5", len(all))
	}

	rest, err := j.Read(3, 100)
	if err != nil {
		t.Fatalf("okunamadı: %v", err)
	}
	if len(rest) != 2 {
		t.Fatalf("seq>3 kayıt sayısı = %d, beklenen 2", len(rest))
	}
	if rest[0].Seq != 4 {
		t.Errorf("ilk kaydın sırası = %d, beklenen 4", rest[0].Seq)
	}
}

func TestReadRespectsLimit(t *testing.T) {
	j, _ := newTestJournal(t)

	for range 10 {
		if _, err := j.Append(execRecord("container.create")); err != nil {
			t.Fatalf("kayıt eklenemedi: %v", err)
		}
	}

	got, err := j.Read(0, 3)
	if err != nil {
		t.Fatalf("okunamadı: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("kayıt sayısı = %d, beklenen 3", len(got))
	}
}

// TestReadDoesNotDisturbAppendPosition, okumanın yazma konumunu
// bozmadığını doğrular. O_APPEND bunu garanti eder ama davranışa
// bağımlıyız, o yüzden test edilir.
func TestReadDoesNotDisturbAppendPosition(t *testing.T) {
	j, _ := newTestJournal(t)

	if _, err := j.Append(execRecord("first")); err != nil {
		t.Fatalf("kayıt eklenemedi: %v", err)
	}
	if _, err := j.Read(0, 100); err != nil {
		t.Fatalf("okunamadı: %v", err)
	}
	if _, err := j.Append(execRecord("second")); err != nil {
		t.Fatalf("okuma sonrası ekleme başarısız: %v", err)
	}

	all, err := j.Read(0, 100)
	if err != nil {
		t.Fatalf("okunamadı: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("kayıt sayısı = %d, beklenen 2", len(all))
	}
	if all[0].Action != "first" || all[1].Action != "second" {
		t.Errorf("kayıtlar bozuldu: %q, %q", all[0].Action, all[1].Action)
	}
}

func TestRoundTripPreservesAllFields(t *testing.T) {
	j, _ := newTestJournal(t)

	want := audit.Record{
		Actor: audit.Actor{
			KeyFingerprint: "SHA256:ABC",
			SourceIP:       "203.0.113.9",
			Label:          "erkan@laptop",
			Origin:         "cli",
		},
		Action:     "volume.remove",
		Target:     "volume/blog-data",
		ParamsJSON: `{"name":"blog-data"}`,
		Outcome:    audit.OutcomeDenied,
		Detail:     "hacim halen kullanımda",
	}

	sealed, err := j.Append(want)
	if err != nil {
		t.Fatalf("kayıt eklenemedi: %v", err)
	}

	got, err := j.Read(0, 10)
	if err != nil {
		t.Fatalf("okunamadı: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("kayıt sayısı = %d, beklenen 1", len(got))
	}

	g := got[0]
	if g.Actor != sealed.Actor || g.Action != sealed.Action || g.Target != sealed.Target ||
		g.ParamsJSON != sealed.ParamsJSON || g.Outcome != sealed.Outcome ||
		g.Detail != sealed.Detail || g.Source != sealed.Source ||
		g.Hash != sealed.Hash || g.PrevHash != sealed.PrevHash {
		t.Errorf("kayıt gidiş-dönüşte değişti:\nyazılan: %+v\nokunan:  %+v", sealed, g)
	}
	if !g.TS.Equal(sealed.TS) {
		t.Errorf("zaman damgası değişti: %v != %v", g.TS, sealed.TS)
	}
}

// TestReadRejectsLineForgedWhileOpen, açılış doğrulamasının YETMEDİĞİ
// senaryoyu kapsar.
//
// Executor çalışırken günlük dosyasına sahte bir satır eklenirse, Read
// bunu "gerçek" gibi teslim ederse panelyd'nin çapraz doğrulaması iki
// doğrulanmamış zinciri karşılaştırmış olur — yani hiçbir şey kanıtlamaz.
// Bu yüzden Read her çağrıda zinciri seq 1'den yeniden doğrular.
func TestReadRejectsLineForgedWhileOpen(t *testing.T) {
	j, path := newTestJournal(t)

	if _, err := j.Append(execRecord("container.create")); err != nil {
		t.Fatalf("kayıt eklenemedi: %v", err)
	}

	// Günlük AÇIKKEN dosyaya doğrudan sahte satır ekle.
	forged := `{"seq":2,"ts":0,"action":"sahte.kayit","outcome":1,"source":2,` +
		`"prev":"0000000000000000000000000000000000000000000000000000000000000000",` +
		`"hash":"0000000000000000000000000000000000000000000000000000000000000000"}`
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatalf("dosya açılamadı: %v", err)
	}
	if _, err := f.WriteString(forged + "\n"); err != nil {
		t.Fatalf("sahte satır yazılamadı: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("dosya kapatılamadı: %v", err)
	}

	if _, err := j.Read(0, 100); err == nil {
		t.Fatal("çalışma anında eklenen sahte kayıt Read tarafından teslim edildi")
	}
}

// TestReadVerifiesFromSeqOneEvenWhenPaging, sayfalama yaparken de
// doğrulamanın baştan başladığını gösterir: zincir ancak baştan
// takip edilerek kanıtlanabilir.
func TestReadVerifiesFromSeqOneEvenWhenPaging(t *testing.T) {
	j, path := newTestJournal(t)

	for range 5 {
		if _, err := j.Append(execRecord("container.create")); err != nil {
			t.Fatalf("kayıt eklenemedi: %v", err)
		}
	}
	if err := j.Close(); err != nil {
		t.Fatalf("kapatılamadı: %v", err)
	}

	// İLK kaydı boz. afterSeq=3 ile okurken ilk kayıt döndürülmez ama
	// zincirin geçerliliği ona bağlıdır, dolayısıyla yakalanmalıdır.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("dosya okunamadı: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	lines[0] = strings.Replace(lines[0], `"container/blog-1"`, `"container/kurban"`, 1)
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("dosya yazılamadı: %v", err)
	}

	// Açılış da reddeder; Read'i doğrudan sınamak için yapıyı elle kur.
	j2 := &Journal{path: path}
	if _, err := j2.Read(3, 100); err == nil {
		t.Fatal("döndürülmeyen ama zinciri taşıyan bozuk kayıt yakalanmadı")
	}
}

func TestOpenJournalRequiresPath(t *testing.T) {
	if _, err := OpenJournal(JournalOptions{}); err == nil {
		t.Fatal("boş yolla günlük açıldı")
	}
}
