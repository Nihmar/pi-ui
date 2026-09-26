package spike

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
)

// Machine describes the host a measurement ran on, so every number in the spike
// report can be attributed to one machine (docs/spike-interfaces.md §5.6).
type Machine struct {
	CPU         string  `json:"cpu"`
	Cores       int     `json:"cores"`
	MemTotalMiB float64 `json:"memTotalMiB"`
	Kernel      string  `json:"kernel"`
}

// CurrentMachine reads the host facts from /proc. It fails only when /proc/cpuinfo
// is unreadable — the one file that names the CPU; the remaining fields degrade to
// their zero value so a report can still be written on an unusual kernel.
func CurrentMachine() (Machine, error) {
	m := Machine{Cores: runtime.NumCPU()}

	cpuinfo, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return m, fmt.Errorf("spike: /proc/cpuinfo: %w", err)
	}
	m.CPU = cpuModelName(string(cpuinfo))

	if meminfo, err := os.ReadFile("/proc/meminfo"); err == nil {
		m.MemTotalMiB = memTotalMiB(string(meminfo))
	}
	if release, err := os.ReadFile("/proc/sys/kernel/osrelease"); err == nil {
		m.Kernel = strings.TrimSpace(string(release))
	}
	return m, nil
}

// cpuModelName returns the value of the first "model name" line of /proc/cpuinfo.
func cpuModelName(cpuinfo string) string {
	for _, line := range strings.Split(cpuinfo, "\n") {
		key, value, found := strings.Cut(line, ":")
		if !found || strings.TrimSpace(key) != "model name" {
			continue
		}
		return strings.TrimSpace(value)
	}
	return ""
}

// memTotalMiB returns the value of the MemTotal line of /proc/meminfo in MiB.
func memTotalMiB(meminfo string) float64 {
	for _, line := range strings.Split(meminfo, "\n") {
		if !strings.HasPrefix(line, "MemTotal:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0
		}
		kb, err := strconv.ParseFloat(fields[1], 64)
		if err != nil {
			return 0
		}
		return kb / 1024
	}
	return 0
}
