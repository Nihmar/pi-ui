//go:build unix

package main

import (
	"os"
	"syscall"
)

// terminationSignals is what an orderly shutdown listens for: Ctrl-C and SIGTERM, the signal
// a service manager sends.
func terminationSignals() []os.Signal {
	return []os.Signal{os.Interrupt, syscall.SIGTERM}
}
