package api

import (
	"context"
	"strings"
	"testing"

	"filippo.io/age"

	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
	"github.com/erkanrzgc/kadran/internal/store"
	"github.com/erkanrzgc/kadran/internal/vault/vaulttest"
)

// apiTestIdentity, API testlerindeki depoların kasa anahtarı (K-123).
var apiTestIdentity = func() *age.X25519Identity {
	id, err := age.GenerateX25519Identity()
	if err != nil {
		panic(err)
	}
	return id
}()

func enableTestVault(t *testing.T, db *store.Store) {
	t.Helper()
	if _, err := db.EnableVault(context.Background(), vaulttest.Sealer(t, apiTestIdentity)); err != nil {
		t.Fatalf("kasa açılamadı: %v", err)
	}
}

// Güncelleme birleşik tanımı doğruluyor; var olan değerler mühürlü. Boyut
// sınırı mühürlü metinle ölçülseydi 24 KiB'lık bir değeri olan uygulama
// (mühürlüsü ~32 KiB) hiçbir değişken ekleyemezdi. Düz boyutla ölçülmeli,
// ve sınır yine işlemeli.
func TestUpdateAppMeasuresSealedEnvByPlainSize(t *testing.T) {
	srv, _ := newUpdateServer(t, &fakeReconciler{})
	spec := testSpec()
	spec.Env = map[string]string{"BUYUK": strings.Repeat("x", 24<<10)}
	mustCreateApp(t, srv, spec)

	if _, err := srv.UpdateApp(t.Context(), &kadranv1.UpdateAppRequest{
		AppId: "blog", Env: map[string]string{"KUCUK": "1"},
	}); err != nil {
		t.Fatalf("sınırın altındaki güncelleme reddedildi: %v", err)
	}
	_, err := srv.UpdateApp(t.Context(), &kadranv1.UpdateAppRequest{
		AppId: "blog", Env: map[string]string{"TASAN": strings.Repeat("y", 10<<10)},
	})
	if err == nil {
		t.Fatal("düz toplamı 32 KiB'ı aşan güncelleme kabul edildi")
	}
}
