//go:build linux

package spike

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// ProcessRSSMiB reads the resident set size of pid and all of its descendants from
// /proc/<pid>/status, in MiB (docs/spike-interfaces.md §5.6).
//
// The descendant walk matters for real pi children: pi is a Node CLI, so the RSS a
// session actually costs includes the processes it spawns. A process that exits
// mid-walk is skipped — the tree it belonged to is gone — but the root must exist.
func ProcessRSSMiB(pid int) (float64, error) {
	if pid <= 0 {
		return 0, fmt.Errorf("spike: invalid pid %d", pid)
	}
	own, err := readRSSMiB(pid)
	if err != nil {
		return 0, fmt.Errorf("spike: pid %d: %w", pid, err)
	}
	children := processChildren()
	return own + descendantsRSSMiB(pid, children), nil
}

// readRSSMiB parses the VmRSS line of /proc/<pid>/status.
func readRSSMiB(pid int) (float64, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "VmRSS:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0, fmt.Errorf("malformed VmRSS line %q", line)
		}
		kb, err := strconv.ParseFloat(fields[1], 64)
		if err != nil {
			return 0, fmt.Errorf("parse VmRSS %q: %w", fields[1], err)
		}
		return kb / 1024, nil
	}
	// Kernel threads have no resident set; treating them as zero keeps a tree
	// walk from failing on a child that happens to be one.
	return 0, nil
}

// processChildren maps every live pid to its parent pid, read from field 4 of
// /proc/<pid>/stat. The command name may contain spaces and parentheses, so the
// ppid is parsed after the last ')'.
func processChildren() map[int][]int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	children := make(map[int][]int, len(entries))
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		ppid, ok := readPPID(pid)
		if !ok || ppid <= 0 {
			continue
		}
		children[ppid] = append(children[ppid], pid)
	}
	return children
}

// readPPID returns the parent pid of one process, or false when /proc vanished
// under the walk (the process exited between the listing and the read).
func readPPID(pid int) (int, bool) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, false
	}
	closing := -1
	for i := len(data) - 1; i >= 0; i-- {
		if data[i] == ')' {
			closing = i
			break
		}
	}
	if closing < 0 {
		return 0, false
	}
	fields := strings.Fields(string(data[closing+1:]))
	if len(fields) < 2 {
		return 0, false
	}
	ppid, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0, false
	}
	return ppid, true
}

// descendantsRSSMiB sums the RSS of every descendant of root.
func descendantsRSSMiB(root int, children map[int][]int) float64 {
	total := 0.0
	seen := map[int]bool{root: true}
	queue := append([]int(nil), children[root]...)
	for len(queue) > 0 {
		pid := queue[0]
		queue = queue[1:]
		if seen[pid] {
			continue
		}
		seen[pid] = true
		if rss, err := readRSSMiB(pid); err == nil {
			total += rss
		}
		queue = append(queue, children[pid]...)
	}
	return total
}

// selfRSSMiB reads the RSS of the current process without its descendants: the Go
// server's own footprint, which is what acceptance criteria C2/C3 bound.
func selfRSSMiB() (float64, error) {
	return readRSSMiB(os.Getpid())
}
