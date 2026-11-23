//go:build !windows

package proc

import (
	"os/exec"
	"syscall"
)

// prepare puts the program in a process group of its own, so that it can be ended with
// all processes it started.
func prepare(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func startTree(cmd *exec.Cmd) (tree, error) {
	if err := cmd.Start(); err != nil {
		return tree{}, err
	}
	pid := cmd.Process.Pid
	return tree{
		kill:    func() { _ = syscall.Kill(-pid, syscall.SIGKILL) },
		release: func() {},
	}, nil
}

func oemCodePage() uint32 { return 0 }
