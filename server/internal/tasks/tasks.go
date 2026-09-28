// Package tasks runs background commands on the host, in a directory a workspace
// allows, and keeps what they printed.
//
// A task is not a session: it has no model, no conversation and no pi process — it is
// the "run this and tell me later" capability the plan asks for (a build, a test run, a
// dev server). Like every other host capability it goes through internal/fs before a
// process starts, and like the terminal it kills the whole process group when it is
// stopped.
package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Nihmar/pi-ui/server/internal/audit"
	"github.com/Nihmar/pi-ui/server/internal/fs"
	"github.com/Nihmar/pi-ui/server/internal/sessions"
	"github.com/Nihmar/pi-ui/server/internal/ws"
)

// Defaults and bounds of the task runner.
const (
	// DefaultMaxTasks bounds concurrent tasks: a client that starts a hundred builds
	// must not take the host down.
	DefaultMaxTasks = 4
	// DefaultOutputBytes is how much output is kept per task. It is a ring: the
	// interesting part of a build is its end, so the oldest bytes are dropped.
	DefaultOutputBytes = 256 << 10
	// StopGrace is how long a stop waits for SIGTERM before SIGKILL.
	StopGrace = 5 * time.Second
	// maxNameLength bounds the name a client gives a task, so the listing stays a
	// listing.
	maxNameLength = 120
)

// Status is where a task is.
type Status string

// The four states a task can be in. `stopped` is different from `exited`: one was
// asked for, the other happened.
const (
	StatusRunning Status = "running"
	StatusExited  Status = "exited"
	StatusFailed  Status = "failed"
	StatusStopped Status = "stopped"
)

// Spec is what a client asks to run.
type Spec struct {
	// Name is the label shown in a list (`tests`, `build`).
	Name string
	// Command is the program to run.
	Command string
	// Args are its arguments, kept apart so nothing is parsed through a shell.
	Args []string
	// Dir is the working directory; it is confined to the workspaces.
	Dir string
}

// Task is one background command as the API reports it.
type Task struct {
	// ID is the server-side id (`k_…`).
	ID string `json:"id"`
	// Name is the label the client chose.
	Name string `json:"name"`
	// Command and Args are what runs.
	Command string   `json:"command"`
	Args    []string `json:"args"`
	// Dir is the confined working directory.
	Dir string `json:"dir"`
	// PID is the process id while it runs.
	PID int `json:"pid,omitempty"`
	// Status is the current state.
	Status Status `json:"status"`
	// ExitCode is set once it is over, -1 when a signal ended it.
	ExitCode int `json:"exitCode,omitempty"`
	// StartedAt and EndedAt bound its life.
	StartedAt string `json:"startedAt"`
	EndedAt   string `json:"endedAt,omitempty"`
	// Truncated is true when the output ring dropped bytes.
	Truncated bool `json:"truncated"`
	// Owner names the device that started it, for the listing.
	Owner string `json:"owner,omitempty"`
}

// Config wires the runner.
type Config struct {
	// FS is the confinement every directory goes through (required).
	FS *fs.Service
	// Hub publishes the change events, when one is wired.
	Hub Publisher
	// MaxTasks bounds concurrent tasks (default DefaultMaxTasks).
	MaxTasks int
	// OutputBytes bounds the kept output per task (default DefaultOutputBytes).
	OutputBytes int
	// Command overrides process construction, for a test.
	Command func(context.Context, string, ...string) *exec.Cmd
}

// Publisher is the subset of the WebSocket hub this package needs.
type Publisher interface {
	Publish(ev ws.Event) uint64
}

// EventTasksChanged is published whenever a task starts, ends or is stopped.
const EventTasksChanged = "server.tasks.changed"

// Service owns every task of one server.
type Service struct {
	cfg Config

	mu    sync.Mutex
	tasks map[string]*task
	order []string
	// pending counts the commands being started right now: the slot is taken before the
	// spawn, because a task only reaches the registry after fork and exec.
	pending int
	next    atomic.Uint64
}

// task is one running (or finished) command.
type task struct {
	spec    Spec
	id      string
	owner   string
	started time.Time

	cmd *exec.Cmd

	mu        sync.Mutex
	status    Status
	exitCode  int
	endedAt   time.Time
	output    []byte
	truncated bool
	done      chan struct{}
	// collected is closed once the output pipe reached EOF: the end of a task is
	// reported after its last byte, never before.
	collected chan struct{}
}

// New builds the runner.
func New(cfg Config) (*Service, error) {
	if cfg.FS == nil {
		return nil, errors.New("tasks: the filesystem service is required")
	}
	if cfg.MaxTasks <= 0 {
		// The reserved-error check happens here rather than in Start so a limit that
		// only exists on the happy path is impossible.
		cfg.MaxTasks = DefaultMaxTasks
	}
	if cfg.OutputBytes <= 0 {
		cfg.OutputBytes = DefaultOutputBytes
	}
	return &Service{cfg: cfg, tasks: map[string]*task{}}, nil
}

// Start launches one command and returns its task.
func (s *Service) Start(ctx context.Context, spec Spec, owner string) (Task, error) {
	name := strings.TrimSpace(spec.Name)
	if name == "" {
		name = filepath.Base(spec.Command)
	}
	if len(name) > maxNameLength {
		name = name[:maxNameLength]
	}
	if strings.TrimSpace(spec.Command) == "" {
		return Task{}, sessions.Codedf(sessions.CodeBadRequest, "a task needs a command")
	}
	dir, err := s.cfg.FS.Resolve(spec.Dir)
	if err != nil {
		return Task{}, err
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return Task{}, sessions.Codedf(sessions.CodeNotFound, "%s is not a directory", spec.Dir)
	}

	s.mu.Lock()
	running := 0
	for _, existing := range s.tasks {
		existing.mu.Lock()
		if existing.status == StatusRunning {
			running++
		}
		existing.mu.Unlock()
	}
	// The slot is reserved here, not after the spawn: fork plus exec is a window wide
	// enough for a burst of starts to read the same count and all get through, which is
	// exactly the "hundred builds" the limit exists for. Every path below either releases
	// the reservation or registers a task that counts by itself.
	if running+s.pending >= s.cfg.MaxTasks {
		busy := running + s.pending
		s.mu.Unlock()
		return Task{}, sessions.Codedf(sessions.CodeSessionLimit,
			"%d tasks are already running", busy)
	}
	s.pending++
	id := fmt.Sprintf("k_%016x", s.next.Add(1))
	s.mu.Unlock()

	var command *exec.Cmd
	if s.cfg.Command != nil {
		command = s.cfg.Command(ctx, spec.Command, spec.Args...)
	} else {
		command = exec.CommandContext(ctx, spec.Command, spec.Args...)
	}
	command.Dir = dir
	// Its own process group, so a stop reaches what the command started too.
	command.SysProcAttr = groupAttr()
	// The output goes through a pipe this process owns, not through StdoutPipe: `Wait`
	// closes the pipe it created, so a reader racing it can lose the last bytes — or all of
	// them — which is exactly what a test caught. With an io.Writer, `exec` copies the
	// child's output itself and `Wait` waits for that copy, so closing our writer is the
	// one thing that ends the stream, and it happens after the process is gone.
	stdout, writer := io.Pipe()
	command.Stdout = writer
	command.Stderr = writer

	if err := command.Start(); err != nil {
		s.releaseSlot()
		return Task{}, sessions.Codedf(sessions.CodeBadRequest, "%s cannot be started: %v", spec.Command, err)
	}
	record := &task{
		spec:      Spec{Name: name, Command: spec.Command, Args: append([]string(nil), spec.Args...), Dir: dir},
		id:        id,
		owner:     owner,
		started:   time.Now(),
		cmd:       command,
		status:    StatusRunning,
		done:      make(chan struct{}),
		collected: make(chan struct{}),
	}
	s.mu.Lock()
	s.tasks[id] = record
	s.order = append(s.order, id)
	s.pending--
	s.mu.Unlock()

	go s.collect(record, stdout)
	go s.wait(record, writer)
	s.publish(record)
	return record.snapshot(), nil
}

// releaseSlot gives back a slot reserved by Start without a task to show for it (a spawn
// that failed). The path that registers a task releases the reservation itself.
func (s *Service) releaseSlot() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.pending > 0 {
		s.pending--
	}
}

// Get returns one task's report.
func (s *Service) Get(id string) (Task, bool) {
	record, ok := s.lookup(id)
	if !ok {
		return Task{}, false
	}
	return record.snapshot(), true
}

// List returns every task, newest first.
func (s *Service) List() []Task {
	s.mu.Lock()
	ids := append([]string(nil), s.order...)
	s.mu.Unlock()

	// Newest first: a list of tasks is read to see what is happening now.
	sort.SliceStable(ids, func(i, j int) bool { return ids[i] > ids[j] })
	list := make([]Task, 0, len(ids))
	for _, id := range ids {
		if record, ok := s.lookup(id); ok {
			list = append(list, record.snapshot())
		}
	}
	return list
}

// Output returns the kept output of one task, oldest bytes first.
func (s *Service) Output(id string) (string, bool) {
	record, ok := s.lookup(id)
	if !ok {
		return "", false
	}
	record.mu.Lock()
	defer record.mu.Unlock()

	return string(record.output), true
}

// Stop ends one task and its process group.
func (s *Service) Stop(id string) (Task, error) {
	record, ok := s.lookup(id)
	if !ok {
		return Task{}, sessions.Codedf(sessions.CodeNotFound, "no task %s", id)
	}
	record.mu.Lock()
	running := record.status == StatusRunning
	record.mu.Unlock()
	if !running {
		return record.snapshot(), nil
	}

	record.mu.Lock()
	if record.status == StatusRunning {
		record.status = StatusStopped
	}
	command := record.cmd
	record.mu.Unlock()

	if command != nil && command.Process != nil {
		_ = signalGroup(command.Process.Pid, false)
		select {
		case <-record.done:
		case <-time.After(StopGrace):
			_ = signalGroup(command.Process.Pid, true)
		}
	}
	return record.snapshot(), nil
}

// StopAll ends every running task: the server is going down and no build outlives it.
func (s *Service) StopAll() {
	for _, task := range s.List() {
		if task.Status == StatusRunning {
			_, _ = s.Stop(task.ID)
		}
	}
}

// collect reads the combined output into the ring until the command ends.
func (s *Service) collect(record *task, stdout io.Reader) {
	defer close(record.collected)
	buffer := make([]byte, 32<<10)
	limit := s.cfg.OutputBytes
	for {
		read, err := stdout.Read(buffer)
		if read > 0 {
			record.mu.Lock()
			record.output = append(record.output, buffer[:read]...)
			if len(record.output) > limit {
				// The end of a build is what someone reads, so the oldest bytes go.
				record.output = record.output[len(record.output)-limit:]
				record.truncated = true
			}
			record.mu.Unlock()
		}
		if err != nil {
			return
		}
	}
}

// wait reaps the process and records how it ended.
func (s *Service) wait(record *task, writer *io.PipeWriter) {
	err := record.cmd.Wait()
	// The process is gone and its output has been copied; closing our end is what tells the
	// collector the stream is over.
	_ = writer.Close()

	// Waiting for the collector is what makes "not running any more" mean "the output is
	// complete" for a client that polls the status and then reads what it printed.
	select {
	case <-record.collected:
	case <-time.After(StopGrace):
		// A reader that will not finish must not hold the ending hostage.
	}

	record.mu.Lock()
	code := -1
	if state := record.cmd.ProcessState; state != nil {
		code = state.ExitCode()
	}
	record.exitCode = code
	record.endedAt = time.Now()
	status := record.status
	switch {
	case status == StatusStopped:
		// Who asked wins over what the exit status says: a stop is a stop.
	case err == nil && code == 0:
		record.status = StatusExited
	case code >= 0:
		record.status = StatusFailed
	default:
		record.status = StatusFailed
	}
	record.mu.Unlock()

	close(record.done)
	s.publish(record)
}

// lookup finds a record under the lock.
func (s *Service) lookup(id string) (*task, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	record, ok := s.tasks[id]
	return record, ok
}

// publish announces one task change, if a hub is wired.
func (s *Service) publish(record *task) {
	if s.cfg.Hub == nil {
		return
	}
	payload, err := json.Marshal(record.snapshot())
	if err != nil {
		return
	}
	s.cfg.Hub.Publish(ws.Event{Type: EventTasksChanged, Payload: payload})
}

// snapshot is the API view of a task.
func (t *task) snapshot() Task {
	t.mu.Lock()
	defer t.mu.Unlock()

	return Task{
		ID:        t.id,
		Name:      t.spec.Name,
		Command:   t.spec.Command,
		Args:      append([]string(nil), t.spec.Args...),
		Dir:       t.spec.Dir,
		PID:       t.pid(),
		Status:    t.status,
		ExitCode:  t.exitCode,
		StartedAt: t.started.UTC().Format(time.RFC3339),
		EndedAt:   endedAt(t.endedAt),
		Truncated: t.truncated,
		Owner:     t.owner,
	}
}

// pid reads the process id, or 0 once it is gone.
func (t *task) pid() int {
	if t.cmd == nil || t.cmd.Process == nil {
		return 0
	}
	return t.cmd.Process.Pid
}

// endedAt renders a finishing time, empty while the task runs.
func endedAt(at time.Time) string {
	if at.IsZero() {
		return ""
	}
	return at.UTC().Format(time.RFC3339)
}

// AuditStart and AuditStop are the trail actions of the two mutating endpoints.
const (
	AuditStart = audit.ActionTaskStart
	AuditStop  = audit.ActionTaskStop
)
