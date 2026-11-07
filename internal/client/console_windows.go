//go:build windows

package client

import (
	"os/exec"
	"syscall"
)

// createNoWindow, CREATE_NO_WINDOW: konsol programı yeni konsol penceresi
// açmadan başlar (girdi/çıktı yine borulardan).
const createNoWindow = 0x08000000

// hideConsole, ssh'nin konsol penceresini kapatır. Konsolu olmayan bir GUI
// süreci konsol programı başlatınca Windows ona yeni bir pencere açar;
// borular bunu engellemez.
func hideConsole(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}
