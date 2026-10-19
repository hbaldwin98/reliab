//go:build windows

package verify

import (
	"os"
	"os/exec"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

// killTree places cmd in a job object once started, so cancelling it ends
// the whole process tree at once (taskkill /T can take many seconds to walk
// a tree). Closing the job also kills anything the command left running.
// It returns a function to call after Start and a cleanup to call after
// Wait.
func killTree(cmd *exec.Cmd) (afterStart func() error, cleanup func()) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return func() error { return nil }, func() {}
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	// Best effort: without kill-on-close, cancelling still terminates the job.
	_, _ = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)))
	cmd.Cancel = func() error { return windows.TerminateJobObject(job, 1) }
	afterStart = func() error {
		h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
		if err != nil {
			return err
		}
		defer func() { _ = windows.CloseHandle(h) }()
		return windows.AssignProcessToJobObject(job, h)
	}
	return afterStart, func() { _ = windows.CloseHandle(job) }
}

// shellCandidates are tried in order: sh on PATH (Git Bash), then Git for
// Windows' default install location.
func shellCandidates() []string {
	out := []string{"sh"}
	for _, env := range []string{"ProgramFiles", "ProgramW6432"} {
		if dir := os.Getenv(env); dir != "" {
			out = append(out, filepath.Join(dir, "Git", "bin", "sh.exe"))
		}
	}
	return out
}
