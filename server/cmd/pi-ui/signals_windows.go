//go:build !unix

package main

import "os"

// terminationSignals: Windows has no SIGTERM, so the console interrupt is what there is.
func terminationSignals() []os.Signal {
	return []os.Signal{os.Interrupt}
}
