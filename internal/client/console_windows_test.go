//go:build windows

package client

import (
	"os/exec"
	"testing"
)

func TestHideConsoleSetsNoWindow(t *testing.T) {
	cmd := exec.Command("ssh")
	hideConsole(cmd)
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.HideWindow || cmd.SysProcAttr.CreationFlags&createNoWindow == 0 {
		t.Fatalf("konsol penceresi kapanmıyor: %+v", cmd.SysProcAttr)
	}
}
