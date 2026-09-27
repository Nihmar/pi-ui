//go:build unix

package tasks

import (
	"syscall"
)

// groupAttr puts the command in its own process group: a stop signals the group, so a build
// that spawned a test runner takes it down too, and the server's own group is never touched.
func groupAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}

// signalGroup signals the whole group: SIGTERM and, when the grace period is over, SIGKILL.
func signalGroup(pid int, abrupt bool) error {
	signal := syscall.SIGTERM
	if abrupt {
		signal = syscall.SIGKILL
	}
	return syscall.Kill(-pid, signal)
}
