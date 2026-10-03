//go:build !linux

package exec

import (
	"context"
	"runtime"

	"github.com/erkanrzgc/kadran/internal/dockerdrv"
	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
)

// collectHostInfo, Linux dışı platformlarda yalnızca derleme yapılabilsin
// diye vardır. Executor üretimde yalnızca Linux'ta çalışır.
func collectHostInfo(context.Context, *dockerdrv.Client) *kadranv1.HostInfo {
	return &kadranv1.HostInfo{
		Os:           runtime.GOOS,
		Architecture: runtime.GOARCH,
		CpuCount:     uint32(runtime.NumCPU()),
	}
}
