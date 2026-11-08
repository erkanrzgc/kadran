//go:build !linux

package hostmetrics

import kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"

// readStatfs: kadrand yalnız Linux'ta çalışıyor; başka sistemde alanlar
// boş kalır ("ölçülemedi").
func readStatfs(string, *kadranv1.HostUsage) {}
