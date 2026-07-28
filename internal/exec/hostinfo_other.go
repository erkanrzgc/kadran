//go:build !linux

package exec

import (
	"context"
	"runtime"

	panelyv1 "github.com/erkanrzgc/panely/internal/pb/panely/v1"
)

// collectHostInfo, Linux dışı platformlarda yalnızca derleme yapılabilsin
// diye vardır. Executor üretimde yalnızca Linux'ta çalışır.
func collectHostInfo(context.Context, string) *panelyv1.HostInfo {
	return &panelyv1.HostInfo{
		Os:           runtime.GOOS,
		Architecture: runtime.GOARCH,
		CpuCount:     uint32(runtime.NumCPU()),
	}
}
