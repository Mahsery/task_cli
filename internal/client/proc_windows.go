//go:build windows

package client

import (
	"os/exec"
	"syscall"
)

// ConfigureDetachedProcess configures cmd to run in a separate process group on Windows.
func ConfigureDetachedProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}
}
