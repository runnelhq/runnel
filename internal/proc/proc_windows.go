//go:build windows

package proc

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// prepare starts the program without a window of its own.
func prepare(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
}

// startTree starts the program in a job object, so that it can be ended with all
// processes it started (ADR 0023). Processes the program leaves behind after it ended
// normally keep running.
func startTree(cmd *exec.Cmd) (tree, error) {
	if err := cmd.Start(); err != nil {
		return tree{}, err
	}
	job, err := windows.CreateJobObject(nil, nil)
	if err == nil {
		h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid)) //nolint:gosec // process ids are DWORDs on Windows
		if err == nil {
			_ = windows.AssignProcessToJobObject(job, h)
			_ = windows.CloseHandle(h)
		}
	}
	return tree{
		kill: func() {
			if job != 0 {
				_ = windows.TerminateJobObject(job, 1)
			}
			_ = cmd.Process.Kill()
		},
		release: func() {
			if job != 0 {
				_ = windows.CloseHandle(job)
			}
		},
	}, nil
}

var procGetOEMCP = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetOEMCP")

// oemCodePage is the OEM code page of the system, e.g. 850, used by console programs.
func oemCodePage() uint32 {
	cp, _, _ := procGetOEMCP.Call()
	return uint32(cp) //nolint:gosec // GetOEMCP returns a UINT
}
