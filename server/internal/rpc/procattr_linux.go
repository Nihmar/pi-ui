//go:build linux

package rpc

import (
	"os/exec"
	"syscall"
)

// sysProcAttr puts the child in its own process group: Close signals the whole group
// (pi spawns helpers), and the server's own group is never touched.
func sysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}

// applyDeathSignal arms Pdeathsig. The kernel arms it in the child at fork time and
// delivers SIGKILL when the creating thread dies, so a server that is killed without
// cleanup leaves no orphaned pi behind. startChild holds runtime.LockOSThread across this
// call and the fork, which is what makes the thread binding hold.
func applyDeathSignal(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = sysProcAttr()
	}
	cmd.SysProcAttr.Pdeathsig = syscall.SIGKILL
}

// killGroup signals the child's process group (its pgid equals its pid because of
// Setpgid).
func killGroup(pid int, sig syscall.Signal) error {
	return syscall.Kill(-pid, sig)
}
