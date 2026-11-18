package main

import (
	"testing"

	"github.com/erkanrzgc/kadran/internal/peercred"
)

// Soket politikaları saf veridir; gerçek soket testleri (peer_linux_test.go)
// yalnız Linux'ta ve yalnız tek uid ile koşar, "root admin'e girebilir" ya da
// "kadran fetch'e girebilir" gibi iki yönlü sapmaları orada ölçmek mümkün değil.
func TestAdminPolicyAdmitsOnlyTheAdminUser(t *testing.T) {
	p := adminPolicy(1001)
	if !p.Allows(peercred.Cred{UID: 1001}) {
		t.Error("admin user rejected on the admin socket")
	}
	for _, uid := range []uint32{0, 1000, 1002} {
		if p.Allows(peercred.Cred{UID: uid, GID: uid}) {
			t.Errorf("uid %d accepted on the admin socket", uid)
		}
	}
}

func TestFetchPolicyAdmitsOnlyRoot(t *testing.T) {
	p := fetchPolicy()
	if !p.Allows(peercred.Cred{UID: 0}) {
		t.Error("root rejected on the fetch socket")
	}
	// Normal kullanıcı aralığının tamamı: kadran ve kadran-git'in uid'i
	// kurulumda belirlenir, tek bir sayıya güvenilemez.
	for uid := uint32(1); uid <= 2000; uid++ {
		if p.Allows(peercred.Cred{UID: uid, GID: uid}) {
			t.Errorf("uid %d accepted on the fetch socket", uid)
		}
	}
	if len(p.AllowGIDs) != 0 {
		t.Error("fetch policy admits by group")
	}
}
