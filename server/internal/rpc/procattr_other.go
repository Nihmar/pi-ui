//go:build !linux

package rpc

import (
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
