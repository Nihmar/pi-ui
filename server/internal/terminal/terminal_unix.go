//go:build unix

package terminal

import (
	"errors"
	"os"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// supportsPTY is true where the kernel has pseudo-terminals.
const supportsPTY = true

// setNonBlocking puts the master in non-blocking mode: the pump polls before reading, and a
// read that is already in flight must never outlive the terminal that owns it.
func setNonBlocking(file *os.File) error {
	return syscall.SetNonblock(int(file.Fd()), true)
}

// waitReadable reports whether the descriptor has something to read within the timeout.
//
// `select` is what makes a close interruptible: the master is opened blocking by the PTY
// library, and a blocking read on it is not interrupted by closing the file, so a terminal
// whose shell stopped producing output would hold a goroutine and a descriptor forever.
func waitReadable(file *os.File, timeout time.Duration) (bool, error) {
	// `x/sys/unix` and not `syscall`: the two disagree on the shape of Select across the
	// unixes, and a poll has one shape everywhere this server builds.
	poll := []unix.PollFd{{Fd: int32(file.Fd()), Events: unix.POLLIN}}
	ready, err := unix.Poll(poll, int(timeout.Milliseconds()))
	if errors.Is(err, syscall.EINTR) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return ready > 0, nil
}

// signalGroup signals the shell's whole process group: SIGHUP because that is what a shell
// reads as "your terminal is gone" (an interactive shell ignores SIGTERM by design), and
// SIGKILL for the abrupt end of the grace period.
func signalGroup(pid int, abrupt bool) error {
	signal := syscall.SIGHUP
	if abrupt {
		signal = syscall.SIGKILL
	}
	return syscall.Kill(-pid, signal)
}

// wouldBlock reports the "nothing to read right now" errno of a non-blocking descriptor.
func wouldBlock(err error) bool {
	return errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK)
}
