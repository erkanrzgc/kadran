package domaincheck

import "net/netip"

// cloudflareRanges, Cloudflare'in yayımladığı aralıklar
// (https://www.cloudflare.com/ips-v4 ve /ips-v6; 15 Ekim 2026'da alındı).
// Bir A/AAAA kaydı buradaysa kayıt ya Cloudflare'de proxy'li (turuncu bulut)
// ya da Cloudflare'de barınan bir sayfayı (ör. park sayfası) gösteriyor;
// iki durumda da Kadran sunucusu doğrulanamaz (K-151).
var cloudflareRanges = mustPrefixes(
	"173.245.48.0/20", "103.21.244.0/22", "103.22.200.0/22", "103.31.4.0/22",
	"141.101.64.0/18", "108.162.192.0/18", "190.93.240.0/20", "188.114.96.0/20",
	"197.234.240.0/22", "198.41.128.0/17", "162.158.0.0/15", "104.16.0.0/13",
	"104.24.0.0/14", "172.64.0.0/13", "131.0.72.0/22",
	"2400:cb00::/32", "2606:4700::/32", "2803:f800::/32", "2405:b500::/32",
	"2405:8100::/32", "2a06:98c0::/29", "2c0f:f248::/32",
)

func mustPrefixes(ss ...string) []netip.Prefix {
	out := make([]netip.Prefix, 0, len(ss))
	for _, s := range ss {
		out = append(out, netip.MustParsePrefix(s))
	}
	return out
}

// IsCloudflare, adresin Cloudflare'in aralığında olup olmadığını söyler.
func IsCloudflare(a netip.Addr) bool {
	a = a.Unmap()
	for _, p := range cloudflareRanges {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

func cloudflareAddrs(addrs []netip.Addr) []netip.Addr {
	var out []netip.Addr
	for _, a := range addrs {
		if IsCloudflare(a) {
			out = append(out, a)
		}
	}
	return out
}
