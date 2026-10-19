//go:build !windows

package verify

import (
	"os/exec"
	"syscall"
)

// killTree makes cancelling cmd kill its whole process group, so a budget
// overrun does not leave the tool's children running. It returns a function
// to call after Start and a cleanup to call after Wait.
func killTree(cmd *exec.Cmd) (afterStart func() error, cleanup func()) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	return func() error { return nil }, func() {}
}

func shellCandidates() []string { return []string{"sh"} }
