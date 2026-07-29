package connproto

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	want := Identity{
		Fingerprint: "SHA256:AAAABBBBCCCC",
		SourceIP:    "203.0.113.7",
		Label:       "erkan@laptop",
		Origin:      "ssh",
	}

	var buf bytes.Buffer
	if err := Write(&buf, want); err != nil {
		t.Fatalf("yazılamadı: %v", err)
	}

	got, err := Read(&buf)
	if err != nil {
		t.Fatalf("okunamadı: %v", err)
	}
	if got != want {
		t.Errorf("gidiş-dönüşte değişti:\nyazılan: %+v\nokunan:  %+v", want, got)
	}
}

// TestReadConsumesExactlyThePreamble, okuyucunun önsözden SONRAKİ baytlara
// DOKUNMADIĞINI doğrular.
//
// Bu, protokolün en kritik özelliğidir: önsözden sonra gelen baytlar
// gRPC'nin HTTP/2 akışıdır. Bir bayt fazla tüketilirse gRPC el sıkışması
// bozulur ve hata mesajı ("bad frame") sebebi hiç göstermez.
func TestReadConsumesExactlyThePreamble(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, Identity{Origin: "ssh", Fingerprint: "SHA256:X"}); err != nil {
		t.Fatalf("yazılamadı: %v", err)
	}

	// gRPC akışını taklit eden baytlar.
	const stream = "PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n"
	buf.WriteString(stream)

	if _, err := Read(&buf); err != nil {
		t.Fatalf("okunamadı: %v", err)
	}

	rest, err := io.ReadAll(&buf)
	if err != nil {
		t.Fatalf("kalan okunamadı: %v", err)
	}
	if string(rest) != stream {
		t.Errorf("önsöz okuyucusu akıştan bayt yedi:\nbeklenen: %q\nkalan:    %q", stream, rest)
	}
}

func TestReadRejectsOversizedPreamble(t *testing.T) {
	// Devasa bir uzunluk bildiren ama veriyi göndermeyen saldırgan.
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], MaxPreambleSize+1)

	_, err := Read(bytes.NewReader(header[:]))
	if !errors.Is(err, ErrPreambleTooLarge) {
		t.Fatalf("ErrPreambleTooLarge bekleniyordu, %v alındı", err)
	}
}

func TestWriteRejectsOversizedIdentity(t *testing.T) {
	huge := Identity{
		Origin: "ssh",
		Label:  strings.Repeat("a", MaxPreambleSize+100),
	}
	if err := Write(io.Discard, huge); !errors.Is(err, ErrPreambleTooLarge) {
		t.Fatalf("ErrPreambleTooLarge bekleniyordu, %v alındı", err)
	}
}

func TestReadRejectsZeroLength(t *testing.T) {
	var header [4]byte // uzunluk = 0
	if _, err := Read(bytes.NewReader(header[:])); err == nil {
		t.Fatal("sıfır uzunluklu önsöz kabul edildi")
	}
}

func TestReadRejectsTruncatedHeader(t *testing.T) {
	if _, err := Read(bytes.NewReader([]byte{0, 0})); err == nil {
		t.Fatal("eksik başlık kabul edildi")
	}
}

func TestReadRejectsTruncatedBody(t *testing.T) {
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], 100)

	// Başlık 100 bayt vaat ediyor, gövde 5 bayt.
	data := append(header[:], []byte("kısa")...)
	if _, err := Read(bytes.NewReader(data)); err == nil {
		t.Fatal("eksik gövde kabul edildi")
	}
}

func TestReadRejectsInvalidJSON(t *testing.T) {
	body := []byte("bu json degil")
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(body)))

	if _, err := Read(bytes.NewReader(append(header[:], body...))); err == nil {
		t.Fatal("geçersiz JSON kabul edildi")
	}
}

func TestReadDefaultsMissingOriginToUnknown(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, Identity{Fingerprint: "SHA256:X"}); err != nil {
		t.Fatalf("yazılamadı: %v", err)
	}

	got, err := Read(&buf)
	if err != nil {
		t.Fatalf("okunamadı: %v", err)
	}
	if got.Origin != "unknown" {
		t.Errorf("köken = %q, beklenen \"unknown\"", got.Origin)
	}
}

// TestWriteEmitsSinglePacket, başlık ve gövdenin TEK yazmada gittiğini
// doğrular. İki ayrı Write, araya başka bir yazıcı girerse bağlantıyı
// bozabilirdi.
func TestWriteEmitsSinglePacket(t *testing.T) {
	counter := &writeCounter{}
	if err := Write(counter, Identity{Origin: "ssh"}); err != nil {
		t.Fatalf("yazılamadı: %v", err)
	}
	if counter.calls != 1 {
		t.Errorf("Write çağrısı sayısı = %d, beklenen 1", counter.calls)
	}
}

type writeCounter struct{ calls int }

func (w *writeCounter) Write(p []byte) (int, error) {
	w.calls++
	return len(p), nil
}

func TestEmptyIdentityStillRoundTrips(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, Identity{}); err != nil {
		t.Fatalf("yazılamadı: %v", err)
	}
	got, err := Read(&buf)
	if err != nil {
		t.Fatalf("okunamadı: %v", err)
	}
	if got.Origin != "unknown" {
		t.Errorf("köken = %q", got.Origin)
	}
}
