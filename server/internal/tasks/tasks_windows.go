//go:build !unix

package tasks

import (
	"os"
	"syscall"
)

// groupAttr is the portable default: the command stays in the server's own group, because
// this platform has no process-group API this code can use without a job object.
func groupAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{}
}

// signalGroup reaches only the direct child, which is what the platform allows. A command
// that spawned something else can outlive it — documented, and better than refusing to run
// background tasks at all.
func signalGroup(pid int, _ bool) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return process.Kill()
}
