package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestSidecarErrorShapeMatchesGolden, masaüstüne giden hata yanıtının
// biçimini altın dosyayla sabitler: `message` sabit sınıf adı, gerçek sebep
// `data`'da. Masaüstü sahte sidecar'ı aynı dosyayı okur; biçim değişirse bu
// test ve masaüstü testleri birlikte düşer.
//
// "connect" GERÇEK yoldan (geçersiz hedefli bir status isteği) alınır;
// "call", gerçek callFailure kurucusundan (başarılı bağlantı gerektirdiği
// için uçtan uca üretilemez).
func TestSidecarErrorShapeMatchesGolden(t *testing.T) {
	responses := runSidecarWith(t, `{"jsonrpc":"2.0","id":1,"method":"status","params":{"target":"erkan@"}}`+"\n")
	if len(responses) != 1 || responses[0].Error == nil {
		t.Fatalf("beklenen tek hata yanıtı, gelen %+v", responses)
	}
	got := map[string]*rpcError{
		"connect": responses[0].Error,
		"call":    callFailure(status.Error(codes.Unavailable, "connection reset by peer")),
	}

	path := filepath.Join("..", "..", "desktop", "test", "fixtures", "sidecar-error.json")
	if os.Getenv("KADRAN_UPDATE_FIXTURES") == "1" {
		body, err := json.MarshalIndent(got, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(body, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s okunamadı (KADRAN_UPDATE_FIXTURES=1 ile üret): %v", path, err)
	}
	var want map[string]*rpcError
	if err := json.Unmarshal(body, &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("hata biçimi altın dosyadan sapıyor; bilinçli değişiklikse KADRAN_UPDATE_FIXTURES=1 ile yenile\ngot:  %+v\nwant: %+v", got, want)
	}
	for name, e := range got {
		if e.Code != codeInternalError || e.Message == "" || e.Data == "" {
			t.Errorf("%s: code/message/data üçlüsü eksik: %+v", name, e)
		}
	}
}
