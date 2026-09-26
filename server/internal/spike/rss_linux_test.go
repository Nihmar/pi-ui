//go:build linux

package spike

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

// rssHelperEnv turns a run of this test binary into a process that holds memory.
// The parent mode allocates 48 MiB, spawns itself in child mode (another 48 MiB)
// and prints both pids; the child mode allocates and prints its own pid. Both stay
// alive until the test kills them, so ProcessRSSMiB can walk the tree.
const rssHelperEnv = "SPIKE_RSS_HELPER"

func TestProcessRSSHelperProcess(t *testing.T) {
	mode := os.Getenv(rssHelperEnv)
	if mode == "" {
		t.Skip("helper process for TestProcessRSSMiBIncludesDescendants")
	}

	switch mode {
	case "child":
		hold := allocateMiB(48)
		fmt.Printf("pid=%d\n", os.Getpid())
		time.Sleep(20 * time.Second)
		runtime.KeepAlive(hold)
	case "parent":
		child := exec.Command(os.Args[0], "-test.run=TestProcessRSSHelperProcess$")
		child.Env = append(os.Environ(), rssHelperEnv+"=child")
		stdout, err := child.StdoutPipe()
		if err != nil {
			fmt.Fprintf(os.Stderr, "helper: %v\n", err)
			os.Exit(2)
		}
		if err := child.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "helper: %v\n", err)
			os.Exit(2)
		}
		scanner := bufio.NewScanner(stdout)
		if !scanner.Scan() {
			fmt.Fprintf(os.Stderr, "helper: child printed nothing: %v\n", scanner.Err())
			os.Exit(2)
		}
		var childPID int
		if _, err := fmt.Sscanf(strings.TrimSpace(scanner.Text()), "pid=%d", &childPID); err != nil {
			fmt.Fprintf(os.Stderr, "helper: child pid: %v\n", err)
			os.Exit(2)
		}
		hold := allocateMiB(48)
		fmt.Printf("pid=%d child=%d\n", os.Getpid(), childPID)
		time.Sleep(20 * time.Second)
		runtime.KeepAlive(hold)
	default:
		fmt.Fprintf(os.Stderr, "helper: unknown mode %q\n", mode)
		os.Exit(2)
	}
	os.Exit(0)
}

func TestProcessRSSMiBIncludesDescendants(t *testing.T) {
	parent := exec.Command(os.Args[0], "-test.run=TestProcessRSSHelperProcess$")
	parent.Env = append(os.Environ(), rssHelperEnv+"=parent")
	stdout, err := parent.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	if err := parent.Start(); err != nil {
		t.Fatalf("start helper: %v", err)
	}
	kill := func() {
		_ = parent.Process.Kill()
		_ = parent.Wait()
	}
	defer kill()

	scanner := bufio.NewScanner(stdout)
	if !scanner.Scan() {
		t.Fatalf("helper printed no pid line: %v", scanner.Err())
	}
	var parentPID, childPID int
	if _, err := fmt.Sscanf(strings.TrimSpace(scanner.Text()), "pid=%d child=%d", &parentPID, &childPID); err != nil {
		t.Fatalf("parse %q: %v", scanner.Text(), err)
	}

	tree, err := ProcessRSSMiB(parentPID)
	if err != nil {
		t.Fatalf("ProcessRSSMiB(parent) = %v", err)
	}
	child, err := ProcessRSSMiB(childPID)
	if err != nil {
		t.Fatalf("ProcessRSSMiB(child) = %v", err)
	}
	t.Logf("parent tree = %.1f MiB, child tree = %.1f MiB", tree, child)

	// Both processes hold 48 MiB each; a tree read that ignored descendants
	// could not get near 90 MiB.
	if tree < 90 {
		t.Errorf("parent tree = %.1f MiB, want >= 90 MiB (own 48 MiB + child 48 MiB)", tree)
	}
	if tree < child {
		t.Errorf("parent tree = %.1f MiB is smaller than its child's %.1f MiB", tree, child)
	}
}

func TestProcessRSSMiBSelf(t *testing.T) {
	self, err := ProcessRSSMiB(os.Getpid())
	if err != nil {
		t.Fatalf("ProcessRSSMiB(self): %v", err)
	}
	if self <= 0 {
		t.Fatalf("self RSS = %v MiB, want > 0", self)
	}
	if own, err := selfRSSMiB(); err != nil || own <= 0 {
		t.Fatalf("selfRSSMiB = %v, %v; want > 0", own, err)
	}
}

func TestProcessRSSMiBRejectsInvalidPID(t *testing.T) {
	if _, err := ProcessRSSMiB(0); err == nil {
		t.Fatal("pid 0 must fail")
	}
	if _, err := ProcessRSSMiB(-1); err == nil {
		t.Fatal("pid -1 must fail")
	}
}

// allocateMiB returns a slice of mib MiB with every page touched, so the memory is
// really resident and not just reserved by the allocator.
func allocateMiB(mib int) []byte {
	buf := make([]byte, mib<<20)
	for i := 0; i < len(buf); i += 4096 {
		buf[i] = 1
	}
	return buf
}
