package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
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

// TestSidecarHandlesMultipleRequestsInOrder, ardışık isteklerin sırayla
// yanıtlandığını doğrular.
//
// Çerçeveleme satır sonuna dayanıyor: bir yanıtın içine kaçmamış bir
// satır sonu sızarsa üst süreç akışı yanlış bölerdi.
func TestSidecarHandlesMultipleRequestsInOrder(t *testing.T) {
	input := `{"jsonrpc":"2.0","id":1,"method":"version"}` + "\n" +
		`{"jsonrpc":"2.0","id":2,"method":"yok"}` + "\n" +
		`{"jsonrpc":"2.0","id":3,"method":"version"}` + "\n"

	responses := runSidecarWith(t, input)

	if len(responses) != 3 {
		t.Fatalf("yanıt sayısı = %d, beklenen 3", len(responses))
	}
	for i, want := range []string{"1", "2", "3"} {
		if string(responses[i].ID) != want {
			t.Errorf("%d. yanıtın id'si = %s, beklenen %s", i+1, responses[i].ID, want)
		}
	}
	if responses[1].Error == nil {
		t.Error("ikinci istek hata döndürmeliydi")
	}
	if responses[0].Error != nil || responses[2].Error != nil {
		t.Error("geçerli istekler hata döndürdü")
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
	if responses[0].Error == nil {
		t.Error("geçersiz hedef kabul edildi")
	}
	// İkinci istek hâlâ çalışmalı: bir hata oturumu bitirmemeli.
	if responses[1].Error != nil {
		t.Errorf("hatadan sonra sonraki istek de başarısız: %+v", responses[1].Error)
	}
}
