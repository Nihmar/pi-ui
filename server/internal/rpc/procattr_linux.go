//go:build linux

package rpc

import (
	"bytes"
	"os"
	"os/exec"
	"strconv"
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

// processGone reports whether the process is gone for good. Signal 0 cannot answer that
// question: an unreaped child is a zombie, which still accepts signal 0, so a killed child
// whose reader goroutine is parked would look alive forever and never be reaped. The state
// field of /proc/<pid>/stat is authoritative instead — 'Z' is a zombie (exited, waiting for
// Wait) and 'X' is a process being torn down.
func processGone(pid int) bool {
	if pid <= 0 {
		return true
	}
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		// No /proc entry: the process either never existed or was already reaped.
		return true
	}
	// stat is "<pid> (<comm>) <state> …", and comm is the executable name in parentheses
	// and may contain spaces and parentheses of its own, so the state is read relative to
	// the last ')'.
	end := bytes.LastIndexByte(data, ')')
	if end < 0 || end+2 >= len(data) {
		return false
	}
	switch data[end+2] {
	case 'Z', 'X', 'x':
		return true
	default:
		return false
	}
}
