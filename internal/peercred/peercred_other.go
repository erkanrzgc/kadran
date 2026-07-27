//go:build !linux

package peercred

import "net"

// FromConn, Linux dışı platformlarda daima hata döndürür.
//
// Panely'nin sunucu bileşenleri (panelyd, panely-exec) yalnızca Linux'ta
// çalışır. Bu dosyanın var olma sebebi, iş istasyonu geliştiricisinin
// Windows veya macOS üzerinde `go build ./...` ve `go vet ./...`
// çalıştırabilmesidir — sessizce izin veren bir yedek uygulama DEĞİL.
//
// Sessizce başarılı olan bir stub, güvenlik kontrolünün yanlış platformda
// devre dışı kalmasına yol açardı; bu yüzden açıkça hata döner.
func FromConn(net.Conn) (Cred, error) { return Cred{}, ErrUnsupportedPlatform }
