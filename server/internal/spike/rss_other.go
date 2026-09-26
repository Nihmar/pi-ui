//go:build !linux

package spike

import "errors"

// errNoProcRSS is the platform answer on systems without /proc.
var errNoProcRSS = errors.New("spike: resident memory needs /proc (Linux only)")

// ProcessRSSMiB is the Linux-only measurement; other platforms report the missing
// capability instead of a zero that would read like a real number.
func ProcessRSSMiB(pid int) (float64, error) { return 0, errNoProcRSS }

// selfRSSMiB backs the throughput memory series; see ProcessRSSMiB.
func selfRSSMiB() (float64, error) { return 0, errNoProcRSS }
