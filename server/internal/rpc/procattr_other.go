//go:build !linux

package rpc

import (
	"os"
	"os/exec"
	"syscall"
)

// sysProcAttr is the portable default: the child stays in the server's own group.
func sysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{}
}

// applyDeathSignal is a no-op where the kernel has no parent-death signal.
func applyDeathSignal(*exec.Cmd) {}

// killGroup cannot reach a process group on this platform. Close falls back to signalling
// the direct child, which is all the Windows and darwin ports can do without a job object
// or a process-group API.
func killGroup(int, syscall.Signal) error {
	return errNoGroupSignal
}

// processGone reports whether the process is gone. Without /proc there is no zombie state
// to read, so this is the portable approximation Close used before: a process that refuses
// signal 0 is gone, and an unreaped child counts as existing until Wait reaps it.
func processGone(pid int) bool {
	if pid <= 0 {
		return true
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return true
	}
	return process.Signal(syscall.Signal(0)) != nil
}
