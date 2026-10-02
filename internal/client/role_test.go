package client

import (
	"testing"

	"github.com/erkanrzgc/kadran/internal/connproto"
)

// TestLocalIdentityIsAdmin: panelyd rolsüz önsözü el sıkışmada reddediyor
// (K-131). Yerel yol rolü yazmayı unutursa sunucuda `kadran -local`
// tamamen çalışmaz; bunu canlıda değil burada görmek gerek.
func TestLocalIdentityIsAdmin(t *testing.T) {
	id := localIdentity()
	if id.Role != connproto.RoleAdmin {
		t.Fatalf("yerel kimliğin rolü = %q, %q bekleniyordu", id.Role, connproto.RoleAdmin)
	}
	if err := id.CheckRole(); err != nil {
		t.Fatalf("yerel kimlik panelyd'nin denetiminden geçmiyor: %v", err)
	}
}
