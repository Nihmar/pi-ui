package rpc

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"time"
)

// Windows and graces of the child lifecycle.
const (
	// TermGrace is the time between SIGTERM and SIGKILL on the process group. It is
	// exported because a caller that bounds a whole graceful shutdown (sessions.Shutdown)
	// has to size its budget against the real worst case instead of guessing the value.
	TermGrace = time.Second
	// stderrGrace bounds the wait for the child's last diagnostics before Wait closes the
	// pipes it created. The child has exited by then, so this only covers a grandchild
	// that inherited stderr.
	stderrGrace = time.Second
	// stderrBufferStart and maxStderrLine bound the stderr scanner: a diagnostic line
	// longer than maxStderrLine stops line parsing (and is reported) instead of killing
	// the child with a blocked stderr pipe.
	stderrBufferStart = 64 << 10
	maxStderrLine     = 1 << 20
)

// errNoGroupSignal reports that the platform has no process-group signalling (see
// procattr_other.go); Close falls back to the direct child.
var errNoGroupSignal = errors.New("rpc: process-group signals are not supported on this platform")

// Start spawns the child. argv[0] is resolved with exec.LookPath, Spec.Env entries are
// merged over os.Environ() (a later entry wins) and the child runs in Spec.Dir with its
// own process group. ctx gates the call itself: the child outlives it, Close owns the
// shutdown. A failed spawn leaves the bridge idle, so a retry is possible.
func (b *bridge) Start(ctx context.Context) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if len(b.spec.Command) == 0 {
		return errors.New("rpc: spec.Command is empty")
	}
	path, err := exec.LookPath(b.spec.Command[0])
	if err != nil {
		return fmt.Errorf("rpc: resolve command %q: %w", b.spec.Command[0], err)
	}
	if err := b.claimStart(); err != nil {
		return err
	}

	cmd, pipes, err := b.spawn(path)
	if err != nil {
		b.releaseStart()
		return err
	}
	b.register(cmd, pipes.stdin)
	go b.readLoop(pipes.stdout)
	go b.drainStderr(pipes.stderr)
	return nil
}

// Wait blocks until the child exits and returns its exit error (nil on status 0). It does
// not close the bridge: Records keeps draining and Send keeps failing with ErrClosed.
func (b *bridge) Wait() error {
	b.mu.Lock()
	started := b.started
	b.mu.Unlock()
	if !started {
		return errNotStarted
	}
	<-b.procDone
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.exitErr
}

// Close shuts the child down and is safe to call twice. It closes stdin first — pi's
// documented orderly shutdown — waits KillGrace for the child to exit by itself, then
// SIGTERMs the process group and SIGKILLs it after termGrace. The child is always reaped.
// A bridge that never started closes as a no-op.
func (b *bridge) Close() error {
	var err error
	b.closeOnce.Do(func() { err = b.shutdown() })
	return err
}

// spawn builds and starts the child, returning its pipes.
func (b *bridge) spawn(path string) (*exec.Cmd, childPipes, error) {
	cmd := exec.Command(path, b.spec.Command[1:]...)
	cmd.Dir = b.spec.Dir
	cmd.Env = mergeEnv(os.Environ(), b.spec.Env)
	cmd.SysProcAttr = sysProcAttr()

	var pipes childPipes
	var err error
	if pipes.stdin, err = cmd.StdinPipe(); err != nil {
		return nil, pipes, fmt.Errorf("rpc: child stdin pipe: %w", err)
	}
	if pipes.stdout, err = cmd.StdoutPipe(); err != nil {
		return nil, pipes, fmt.Errorf("rpc: child stdout pipe: %w", err)
	}
	if pipes.stderr, err = cmd.StderrPipe(); err != nil {
		return nil, pipes, fmt.Errorf("rpc: child stderr pipe: %w", err)
	}
	if err := startChild(cmd); err != nil {
		pipes.close()
		return nil, pipes, fmt.Errorf("rpc: start %s: %w", path, err)
	}
	return cmd, pipes, nil
}

// startChild starts the child with the death signal armed. The goroutine is pinned to one
// OS thread across applyDeathSignal and Start, because Pdeathsig belongs to the thread
// that forks (see procattr_linux.go).
func startChild(cmd *exec.Cmd) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	applyDeathSignal(cmd)
	return cmd.Start()
}

// shutdown is Close's body, under the close-once guard.
func (b *bridge) shutdown() error {
	b.mu.Lock()
	stdin, started, cmd := b.stdin, b.started, b.cmd
	b.closed = true
	b.mu.Unlock()
	var proc *os.Process
	if cmd != nil {
		proc = cmd.Process
	}
	if !started {
		return nil
	}

	if stdin != nil {
		// Orderly shutdown: pi disposes its runtime and exits after stdin EOF.
		if err := stdin.Close(); err != nil {
			b.logf("rpc: close child %d stdin: %v", b.PID(), err)
		}
	}
	if b.waitForExit(b.opts.KillGrace) {
		return nil
	}

	b.logf("rpc: child %d is still running after stdin EOF; terminating its process group", b.PID())
	b.terminate(proc)
	if b.waitForExit(TermGrace) {
		return nil
	}
	b.logf("rpc: child %d ignored SIGTERM; killing its process group", b.PID())
	b.kill(proc)
	// The reader goroutine is the usual reaper, but it is parked whenever the consumer
	// stopped draining Records: wait for the kill to land and reap here, so a child we
	// just killed can never stay a zombie until the server exits.
	if b.waitForExit(TermGrace) {
		return nil
	}
	b.logf("rpc: child %d was killed but has not exited yet; the reader goroutine owns its reap", b.PID())
	return nil
}

// waitForExit reports whether the child was reaped within grace. When a consumer stopped
// draining Records(), the reader goroutine is parked and cannot reap; a child that already
// exited is then reaped here, because a killed child must never stay a zombie. "Already
// exited" is processGone, not "signal 0 was refused": an unreaped child is a zombie and
// still accepts signal 0, which is exactly the case this fallback exists for.
func (b *bridge) waitForExit(grace time.Duration) bool {
	select {
	case <-b.procDone:
		return true
	case <-time.After(grace):
	}
	if !processGone(b.PID()) {
		return false
	}
	b.reap()
	return true
}

// terminate asks the child to stop: SIGTERM to the process group where the platform has
// process groups, otherwise to the direct child.
func (b *bridge) terminate(proc *os.Process) {
	if err := killGroup(b.PID(), syscall.SIGTERM); err != nil {
		b.logf("rpc: SIGTERM process group %d: %v", b.PID(), err)
		if proc != nil {
			_ = proc.Signal(syscall.SIGTERM)
		}
	}
}

// kill forces the child to stop: SIGKILL to the process group where available, otherwise
// os.Process.Kill on the direct child (the Windows path, where SIGTERM does not exist).
func (b *bridge) kill(proc *os.Process) {
	if err := killGroup(b.PID(), syscall.SIGKILL); err != nil {
		b.logf("rpc: SIGKILL process group %d: %v", b.PID(), err)
		if proc != nil {
			_ = proc.Kill()
		}
	}
}

// reap calls Wait once for the whole bridge and releases everything that waited on the
// child: pending senders learn the reason, procDone unblocks Wait and Close.
func (b *bridge) reap() {
	b.reapOnce.Do(func() {
		cmd := b.command()
		err := errNotStarted
		if cmd != nil {
			err = cmd.Wait()
		}
		b.mu.Lock()
		b.exited = true
		b.exitErr = err
		b.mu.Unlock()
		b.releaseWaiters()
		close(b.procDone)
	})
}

// readLoop consumes the child's stdout until the stream ends, then finishes the bridge: it
// reaps the child and closes Records exactly once.
func (b *bridge) readLoop(stdout io.ReadCloser) {
	defer stdout.Close()
	reader := newRecordReader(stdout)
	for {
		raw, err := reader.Next()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				b.logf("rpc: read child %d stdout: %v", b.PID(), err)
			}
			break
		}
		b.route(parseRecord(raw))
	}
	b.waitForStderrDrain()
	b.reap()
	close(b.records)
}

// drainStderr forwards the child's diagnostics line by line. Without a hook the stream is
// still drained: a full stderr pipe would block the child.
func (b *bridge) drainStderr(stderr io.Reader) {
	defer close(b.stderrDone)
	if b.spec.Stderr == nil {
		_, _ = io.Copy(io.Discard, stderr)
		return
	}

	scanner := bufio.NewScanner(stderr)
	scanner.Buffer(make([]byte, 0, stderrBufferStart), maxStderrLine)
	for scanner.Scan() {
		// The scanner reuses its buffer, so the hook gets its own copy.
		line := append([]byte(nil), scanner.Bytes()...)
		b.spec.Stderr(line)
	}
	if err := scanner.Err(); err != nil {
		b.logf("rpc: child %d stderr: %v", b.PID(), err)
		_, _ = io.Copy(io.Discard, stderr)
	}
}

// waitForStderrDrain gives the stderr reader a bounded window to flush the last
// diagnostics before Wait closes the pipes it created.
func (b *bridge) waitForStderrDrain() {
	select {
	case <-b.stderrDone:
	case <-time.After(stderrGrace):
	}
}

// mergeEnv returns base with extra applied on top: a later entry wins for the same
// variable name, so a caller can override the server's environment for one child.
func mergeEnv(base, extra []string) []string {
	if len(extra) == 0 {
		return base
	}
	merged := append([]string(nil), base...)
	position := make(map[string]int, len(merged)+len(extra))
	for index, entry := range merged {
		position[envName(entry)] = index
	}
	for _, entry := range extra {
		name := envName(entry)
		if index, ok := position[name]; ok {
			merged[index] = entry
			continue
		}
		position[name] = len(merged)
		merged = append(merged, entry)
	}
	return merged
}

// envName is the variable name of an "NAME=value" entry, or the entry itself for the
// pass-through form exec accepts.
func envName(entry string) string {
	if index := strings.IndexByte(entry, '='); index >= 0 {
		return entry[:index]
	}
	return entry
}

// childPipes are the three parent ends of the child's stdio.
type childPipes struct {
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser
}

// close releases the pipes of a spawn that failed; the child never ran.
func (p childPipes) close() {
	for _, closer := range []io.Closer{p.stdin, p.stdout, p.stderr} {
		if closer != nil {
			_ = closer.Close()
		}
	}
}
