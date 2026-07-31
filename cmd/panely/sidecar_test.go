package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// runSidecarWith, verilen satırları sidecar'a besler ve yanıtları döndürür.
func runSidecarWith(t *testing.T, input string) []rpcResponse {
	t.Helper()

	c, out, _ := newTestCLI(input)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	done := make(chan int, 1)
	go func() { done <- c.runSidecar(ctx, nil) }()

	select {
	case code := <-done:
		if code != exitOK {
			t.Fatalf("sidecar çıkış kodu = %d", code)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("sidecar stdin kapandığında sonlanmadı")
	}

	return parseResponses(t, out)
}

func parseResponses(t *testing.T, out *bytes.Buffer) []rpcResponse {
	t.Helper()

	var responses []rpcResponse
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if line == "" {
			continue
		}
		var resp rpcResponse
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			t.Fatalf("yanıt çözümlenemedi (%q): %v", line, err)
		}
		responses = append(responses, resp)
	}
	return responses
}

// TestSidecarExitsWhenStdinCloses, üst süreç gidince sidecar'ın da
// sonlandığını doğrular.
//
// Electron kabuğu kapandığında çocuk süreç ayakta kalırsa, arkada açık
// SSH bağlantıları taşıyan yetim süreçler birikirdi.
func TestSidecarExitsWhenStdinCloses(t *testing.T) {
	responses := runSidecarWith(t, "")
	if len(responses) != 0 {
		t.Errorf("boş girdiye yanıt üretildi: %+v", responses)
	}
}

func TestSidecarVersionMethod(t *testing.T) {
	responses := runSidecarWith(t,
		`{"jsonrpc":"2.0","id":1,"method":"version"}`+"\n")

	if len(responses) != 1 {
		t.Fatalf("yanıt sayısı = %d, beklenen 1", len(responses))
	}
	resp := responses[0]
	if resp.Error != nil {
		t.Fatalf("hata döndü: %+v", resp.Error)
	}
	if resp.JSONRPC != jsonrpcVersion {
		t.Errorf("jsonrpc = %q", resp.JSONRPC)
	}
	if string(resp.ID) != "1" {
		t.Errorf("id = %s, beklenen 1", resp.ID)
	}

	body, _ := json.Marshal(resp.Result)
	if !strings.Contains(string(body), "protocol") {
		t.Errorf("protokol sürümü dönmedi: %s", body)
	}
}

func TestSidecarRejectsMalformedJSON(t *testing.T) {
	responses := runSidecarWith(t, "bu JSON değil\n")

	if len(responses) != 1 {
		t.Fatalf("yanıt sayısı = %d, beklenen 1", len(responses))
	}
	if responses[0].Error == nil {
		t.Fatal("bozuk JSON kabul edildi")
	}
	if got := responses[0].Error.Code; got != codeParseError {
		t.Errorf("hata kodu = %d, beklenen %d", got, codeParseError)
	}
}

// TestSidecarRejectsWrongProtocolVersion, jsonrpc alanı yanlış olan
// isteklerin reddedildiğini doğrular.
func TestSidecarRejectsWrongProtocolVersion(t *testing.T) {
	responses := runSidecarWith(t,
		`{"jsonrpc":"1.0","id":7,"method":"version"}`+"\n")

	if len(responses) != 1 {
		t.Fatalf("yanıt sayısı = %d", len(responses))
	}
	if responses[0].Error == nil {
		t.Fatal("yanlış protokol sürümü kabul edildi")
	}
	if got := responses[0].Error.Code; got != codeInvalidRequest {
		t.Errorf("hata kodu = %d, beklenen %d", got, codeInvalidRequest)
	}
}

func TestSidecarUnknownMethod(t *testing.T) {
	responses := runSidecarWith(t,
		`{"jsonrpc":"2.0","id":2,"method":"kendini.imha.et"}`+"\n")

	if len(responses) != 1 {
		t.Fatalf("yanıt sayısı = %d", len(responses))
	}
	if responses[0].Error == nil {
		t.Fatal("bilinmeyen metot kabul edildi")
	}
	if got := responses[0].Error.Code; got != codeMethodNotFound {
		t.Errorf("hata kodu = %d, beklenen %d", got, codeMethodNotFound)
	}
	if !strings.Contains(responses[0].Error.Message, "kendini.imha.et") {
		t.Errorf("metot adı hatada geçmiyor: %s", responses[0].Error.Message)
	}
}

// TestSidecarNotificationGetsNoReply, id'siz isteklerin (bildirim)
// yanıtlanmadığını doğrular — JSON-RPC 2.0 böyle tanımlar.
func TestSidecarNotificationGetsNoReply(t *testing.T) {
	responses := runSidecarWith(t,
		`{"jsonrpc":"2.0","method":"version"}`+"\n")

	if len(responses) != 0 {
		t.Errorf("bildirime yanıt verildi: %+v", responses)
	}
}

// TestSidecarAnswersEveryRequestExactlyOnce, her isteğin tam olarak bir
// yanıt aldığını doğrular.
//
// Sıra KASTEN sınanmıyor: istekler eşzamanlı işleniyor, bu yüzden
// yanıtlar sıra dışı dönebilir. JSON-RPC bunu öngörür — eşleştirme `id`
// üzerinden yapılır. Seri işleseydik yavaş bir uzak çağrı arkasındaki her
// isteği 30 saniyeye kadar bloklardı ve GUI donardı.
//
// Sınanan asıl şey: eşzamanlı yazmalar birbirine karışmıyor. Çerçeveleme
// satır sonuna dayandığı için araya giren bir yazma, üst sürecin akışı
// yanlış bölmesine yol açardı; bozuk satırlar burada çözümlenemez ve test
// patlardı.
func TestSidecarAnswersEveryRequestExactlyOnce(t *testing.T) {
	input := `{"jsonrpc":"2.0","id":1,"method":"version"}` + "\n" +
		`{"jsonrpc":"2.0","id":2,"method":"yok"}` + "\n" +
		`{"jsonrpc":"2.0","id":3,"method":"version"}` + "\n"

	responses := runSidecarWith(t, input)

	if len(responses) != 3 {
		t.Fatalf("yanıt sayısı = %d, beklenen 3", len(responses))
	}

	byID := make(map[string]rpcResponse, len(responses))
	for _, resp := range responses {
		id := string(resp.ID)
		if _, dup := byID[id]; dup {
			t.Errorf("id %s için iki yanıt döndü", id)
		}
		byID[id] = resp
	}

	for _, id := range []string{"1", "2", "3"} {
		if _, ok := byID[id]; !ok {
			t.Errorf("id %s yanıtsız kaldı", id)
		}
	}
	if byID["2"].Error == nil {
		t.Error("bilinmeyen metot hata döndürmeliydi")
	}
	if byID["1"].Error != nil || byID["3"].Error != nil {
		t.Errorf("geçerli istekler hata döndürdü: %+v / %+v", byID["1"].Error, byID["3"].Error)
	}
}

// TestSidecarConcurrentRequestsDoNotInterleave, çok sayıda eşzamanlı
// isteğin çıktıyı bozmadığını doğrular.
//
// Yanıtlar tek satırlık JSON olarak yazılıyor. Yazma kilidi olmasaydı iki
// goroutine aynı satıra yazabilir ve üst süreç çözümlenemez bir şey
// görürdü. -race ile birlikte koşturulduğunda yarışı da yakalar.
func TestSidecarConcurrentRequestsDoNotInterleave(t *testing.T) {
	const count = 50

	var input strings.Builder
	for i := 1; i <= count; i++ {
		fmt.Fprintf(&input, `{"jsonrpc":"2.0","id":%d,"method":"version"}`+"\n", i)
	}

	// parseResponses her satırı çözümlüyor; karışmış bir çıktı orada patlar.
	responses := runSidecarWith(t, input.String())

	if len(responses) != count {
		t.Fatalf("yanıt sayısı = %d, beklenen %d", len(responses), count)
	}
	seen := make(map[string]bool, count)
	for _, resp := range responses {
		if resp.Error != nil {
			t.Fatalf("beklenmedik hata: %+v", resp.Error)
		}
		seen[string(resp.ID)] = true
	}
	if len(seen) != count {
		t.Errorf("benzersiz id sayısı = %d, beklenen %d", len(seen), count)
	}
}

// TestShouldDropConnectionOnlyOnTransportFailure, önbellekteki bağlantının
// yalnızca taşıma koptuğunda düşürüldüğünü doğrular.
//
// Bu ayrım bir hataydı: her hatada bağlantı düşürülüyordu. Anlık kilitli
// bir SQLite'tan gelen Internal hatası, sağlıklı bir SSH bağlantısını
// yıkardı — yani önbellek tam da işe yaraması gereken anda (arka arkaya
// çağrılar) devre dışı kalırdı.
func TestShouldDropConnectionOnlyOnTransportFailure(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"taşıma koptu", status.Error(codes.Unavailable, "connection refused"), true},
		{"iç hata", status.Error(codes.Internal, "database is locked"), false},
		{"geçersiz argüman", status.Error(codes.InvalidArgument, "limit"), false},
		{"bulunamadı", status.Error(codes.NotFound, "kayıt"), false},
		{"yetki yok", status.Error(codes.PermissionDenied, "grup"), false},
		{"süre aşımı", status.Error(codes.DeadlineExceeded, "yavaş"), false},
		{"gRPC olmayan hata", errors.New("düz hata"), false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldDropConnection(tc.err); got != tc.want {
				t.Errorf("shouldDropConnection(%v) = %v, beklenen %v", tc.err, got, tc.want)
			}
		})
	}
}

// TestSidecarStringIDIsEchoedUnchanged, id'nin tipinin korunduğunu
// doğrular. JSON-RPC id'si sayı, dize veya null olabilir.
func TestSidecarStringIDIsEchoedUnchanged(t *testing.T) {
	responses := runSidecarWith(t,
		`{"jsonrpc":"2.0","id":"abc-123","method":"version"}`+"\n")

	if len(responses) != 1 {
		t.Fatalf("yanıt sayısı = %d", len(responses))
	}
	if got := string(responses[0].ID); got != `"abc-123"` {
		t.Errorf("id = %s, beklenen \"abc-123\"", got)
	}
}

// TestSidecarInvalidTargetIsReportedNotCrashed, hedefi bozuk bir isteğin
// süreci düşürmediğini doğrular.
func TestSidecarInvalidTargetIsReportedNotCrashed(t *testing.T) {
	input := `{"jsonrpc":"2.0","id":1,"method":"status","params":{"target":"erkan@"}}` + "\n" +
		`{"jsonrpc":"2.0","id":2,"method":"version"}` + "\n"

	responses := runSidecarWith(t, input)

	if len(responses) != 2 {
		t.Fatalf("yanıt sayısı = %d, beklenen 2 — süreç ilk hatada düşmüş olabilir", len(responses))
	}

	// Eşzamanlı işlendikleri için yanıtlar sıra dışı dönebilir; id ile
	// eşleştiriliyor.
	byID := make(map[string]rpcResponse, len(responses))
	for _, resp := range responses {
		byID[string(resp.ID)] = resp
	}

	if byID["1"].Error == nil {
		t.Error("geçersiz hedef kabul edildi")
	}
	// İkinci istek hâlâ çalışmalı: bir hata oturumu bitirmemeli.
	if byID["2"].Error != nil {
		t.Errorf("hatadan sonra sonraki istek de başarısız: %+v", byID["2"].Error)
	}
}
