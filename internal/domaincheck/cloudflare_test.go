package domaincheck

import (
	"net/netip"
	"testing"
)

func TestIsCloudflare(t *testing.T) {
	for _, s := range []string{"104.16.0.1", "104.21.3.4", "172.67.1.1", "2606:4700::1", "::ffff:104.21.3.4"} {
		if !IsCloudflare(netip.MustParseAddr(s)) {
			t.Errorf("%s Cloudflare sayılmadı", s)
		}
	}
	for _, s := range []string{"203.0.113.10", "8.8.8.8", "2001:db8::1", "104.15.255.255", "172.72.0.1"} {
		if IsCloudflare(netip.MustParseAddr(s)) {
			t.Errorf("%s yanlışlıkla Cloudflare sayıldı", s)
		}
	}
}

func TestCloudflareAddrs(t *testing.T) {
	in := []netip.Addr{netip.MustParseAddr("203.0.113.10"), netip.MustParseAddr("104.21.3.4")}
	got := cloudflareAddrs(in)
	if len(got) != 1 || got[0].String() != "104.21.3.4" {
		t.Fatalf("cloudflareAddrs = %v", got)
	}
}
