//go:build windows

package console

import (
	"context"
	"testing"
)

// Konsoldan başlayan her ssh (root olsun olmasın) pencere açmadan başlamalı.
func TestCommandHookHidesWindow(t *testing.T) {
	const createNoWindow = 0x08000000
	for _, root := range []bool{false, true} {
		cmd := commandHook(root)(context.Background(), "ssh", "-T", "h")
		attr := cmd.SysProcAttr
		if attr == nil || !attr.HideWindow || attr.CreationFlags&createNoWindow == 0 {
			t.Fatalf("root=%v: konsol penceresi gizlenmiyor: %+v", root, attr)
		}
	}
}
