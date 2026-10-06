//go:build !linux

package dockerdrv

import "errors"

// platformChownDir, Linux dışı platformlarda daima hata döndürür.
//
// Kadran'ın ayrıcalıklı bileşeni (kadran-exec) yalnızca Linux'ta
// çalışır. Bu dosyanın var olma sebebi, iş istasyonu geliştiricisinin
// Windows veya macOS üzerinde `go build ./...` ve `go vet ./...`
// çalıştırabilmesidir — sessizce izin veren bir yedek uygulama DEĞİL.
//
// Sessizce başarılı olan bir stub daha kötü olurdu: hacim dizini
// oluşturulur, sahibi değiştirilmez, ve hiçbir hata üretmeden
// konteynerin yazamayacağı bir disk hazırlanmış olurdu. Aynı gerekçe
// internal/peercred'de de yazılı.
func platformChownDir(string, int) error {
	return errors.New("dockerdrv: volume ownership can only be set on Linux")
}
