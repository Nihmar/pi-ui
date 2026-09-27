//go:build !unix

package terminal

import (
	"errors"
	"os"
	"time"
)

// supportsPTY is false on a host without pseudo-terminals. The terminal surface is then an
// explicit `unsupported` instead of a broken one: ConPTY would be its own project, and a
// half-working shell is worse than a documented "not here".
const supportsPTY = false

// setNonBlocking has nothing to do where the PTY does not exist.
func setNonBlocking(*os.File) error { return nil }

// waitReadable never runs on this platform: `Open` refuses before a master exists.
func waitReadable(*os.File, time.Duration) (bool, error) { return false, errNoPTY }

// signalGroup never runs on this platform, for the same reason.
func signalGroup(int, bool) error { return errNoPTY }

// wouldBlock is false: the errno this asks about belongs to a non-blocking descriptor.
func wouldBlock(error) bool { return false }

// errNoPTY is what the platform hooks report when they are reached anyway.
var errNoPTY = errors.New("terminal: this host has no pseudo-terminals")
