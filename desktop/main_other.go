//go:build !windows

// Kadran masaüstü bugün yalnız Windows'ta derleniyor (K-165): Wails'in
// Linux ve macOS kabukları cgo ve sistem kütüphaneleri istiyor, CI'da
// sınanmıyor. Mantık paketleri (internal/...) her sistemde test edilir.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "kadran desktop: only Windows is supported for now")
	os.Exit(1)
}
