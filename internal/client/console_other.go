//go:build !windows

package client

import "os/exec"

// hideConsole, Windows dışında bir şey yapmaz: konsol penceresi kavramı yok.
func hideConsole(*exec.Cmd) {}
