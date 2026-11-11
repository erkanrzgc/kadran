package sidecar

import (
	"errors"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

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
