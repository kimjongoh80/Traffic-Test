package gui

import (
	"os/exec"
	"syscall"
)

// hideConsole stops a helper process from flashing a console window.
func hideConsole(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
}
