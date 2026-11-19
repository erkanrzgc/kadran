//go:build windows

package client

import (
	"os/exec"
	"syscall"
)

// createNoWindow, CREATE_NO_WINDOW: konsol programı yeni konsol penceresi
// açmadan başlar (girdi/çıktı yine borulardan).
const createNoWindow = 0x08000000

// HideConsole, ssh'nin konsol penceresini kapatır. Konsolu olmayan bir GUI
// süreci konsol programı başlatınca Windows ona yeni bir pencere açar;
// borular bunu engellemez. Dışa açık: masaüstü konsolunun ssh kancası da
// aynı kuralı kullanır (kopya yerine tek yer).
func HideConsole(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}
