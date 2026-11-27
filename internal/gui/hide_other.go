//go:build !windows

package gui

import "os/exec"

func hideConsole(cmd *exec.Cmd) {}
