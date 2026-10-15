//go:build !windows

package client

import (
	"os/exec"
	"syscall"
)

// ConfigureDetachedProcess configures cmd to run in a new detached session.
func ConfigureDetachedProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}
}
