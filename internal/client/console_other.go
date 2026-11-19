//go:build !windows

package client

import "os/exec"

// HideConsole, Windows dışında bir şey yapmaz: konsol penceresi kavramı yok.
func HideConsole(*exec.Cmd) {}
