package tasks

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Nihmar/pi-ui/server/internal/fs"
	"github.com/Nihmar/pi-ui/server/internal/sessions"
	"github.com/Nihmar/pi-ui/server/internal/ws"
)

// recordingHub collects the change events.
type recordingHub struct {
	mu     sync.Mutex
	events []ws.Event
}

func (h *recordingHub) Publish(event ws.Event) uint64 {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.events = append(h.events, event)
	return uint64(len(h.events))
}

func (h *recordingHub) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()

	return len(h.events)
}

func newService(t *testing.T, mutate func(*Config)) (*Service, string, *recordingHub) {
	t.Helper()
	root := t.TempDir()
	files, err := fs.New(fs.Config{Roots: []string{root}})
	if err != nil {
		t.Fatal(err)
	}
	cfg := Config{FS: files, Hub: &recordingHub{}}
	if mutate != nil {
		mutate(&cfg)
	}
	service, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return service, root, cfg.Hub.(*recordingHub)
}

// waitFor waits until a task reaches one status.
func waitFor(t *testing.T, service *Service, id string, want Status) Task {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		task, ok := service.Get(id)
		if !ok {
			t.Fatalf("task %s disappeared", id)
		}
		if task.Status == want {
			return task
		}
		time.Sleep(10 * time.Millisecond)
	}
	deadline = time.Time{}
	_ = deadline
	final, _ := service.Get(id)
	t.Fatalf("task stayed %s (wanted %s): %+v", final.Status, want, final)
	return Task{}
}

func TestATaskRunsAndKeepsItsOutput(t *testing.T) {
	service, root, hub := newService(t, nil)

	task, err := service.Start(context.Background(), Spec{
		Name:    "greet",
		Command: "/bin/sh",
		Args:    []string{"-c", "echo first; echo second; exit 0"},
		Dir:     root,
	}, "d_1")
	if err != nil {
		t.Fatal(err)
	}
	// A command that exits instantly may already be done when Start returns: what is
	// guaranteed is that the task exists, is named and is reported, not that it is still
	// running at that microsecond.
	if task.ID == "" || task.Name != "greet" || task.PID == 0 {
		t.Fatalf("unexpected task %+v", task)
	}
	if task.Status != StatusRunning && task.Status != StatusExited {
		t.Fatalf("a freshly started task is running or already done: %+v", task)
	}
	final := waitFor(t, service, task.ID, StatusExited)
	if final.ExitCode != 0 || final.EndedAt == "" {
		t.Fatalf("final task %+v", final)
	}
	output, ok := service.Output(task.ID)
	if !ok || !strings.Contains(output, "first") || !strings.Contains(output, "second") {
		t.Fatalf("output %q (%v)", output, ok)
	}
	// The end event is published by the reaper, which runs after the status flips: wait
	// for the event rather than assuming the two are simultaneous.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && hub.count() < 2 {
		time.Sleep(10 * time.Millisecond)
	}
	if hub.count() < 2 {
		t.Fatalf("a start and an end are worth an event, got %d", hub.count())
	}
	event := hub.events[0]
	if event.Type != EventTasksChanged {
		t.Fatalf("event type %q", event.Type)
	}
	payload := map[string]any{}
	if err := json.Unmarshal(hub.events[len(hub.events)-1].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["status"] != string(StatusExited) {
		t.Fatalf("last event payload %v", payload)
	}
}

func TestAFailingTaskReportsItsExitCode(t *testing.T) {
	service, root, _ := newService(t, nil)

	task, err := service.Start(context.Background(), Spec{
		Command: "/bin/sh",
		Args:    []string{"-c", "echo boom >&2; exit 3"},
		Dir:     root,
	}, "d_1")
	if err != nil {
		t.Fatal(err)
	}
	final := waitFor(t, service, task.ID, StatusFailed)
	if final.ExitCode != 3 {
		t.Fatalf("exit code %d, want 3", final.ExitCode)
	}
	if final.Name != "sh" {
		t.Fatalf("a task without a name is named after its command: %q", final.Name)
	}
	output, _ := service.Output(task.ID)
	if !strings.Contains(output, "boom") {
		t.Fatalf("stderr is part of the output: %q", output)
	}
}

func TestStopEndsTheTaskAndItsChildren(t *testing.T) {
	service, root, _ := newService(t, nil)

	task, err := service.Start(context.Background(), Spec{
		Name:    "long",
		Command: "/bin/sh",
		Args:    []string{"-c", "sleep 30 & wait"},
		Dir:     root,
	}, "d_1")
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
	stopped, err := service.Stop(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stopped.Status != StatusStopped {
		t.Fatalf("status %q, want stopped", stopped.Status)
	}
	final := waitFor(t, service, task.ID, StatusStopped)
	if final.EndedAt == "" {
		t.Fatalf("a stopped task has an end: %+v", final)
	}
	// Stopping again is harmless: it already ended.
	if _, err := service.Stop(task.ID); err != nil {
		t.Fatalf("a second stop must be harmless: %v", err)
	}
}

func TestTheLimitIsEnforcedAndFreed(t *testing.T) {
	service, root, _ := newService(t, func(cfg *Config) { cfg.MaxTasks = 1 })

	first, err := service.Start(context.Background(), Spec{
		Command: "/bin/sh", Args: []string{"-c", "sleep 5"}, Dir: root,
	}, "d_1")
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Start(context.Background(), Spec{
		Command: "/bin/echo", Dir: root,
	}, "d_1")
	if code := sessions.CodeOf(err); code != sessions.CodeSessionLimit {
		t.Fatalf("expected session_limit, got %q (%v)", code, err)
	}
	if _, err := service.Stop(first.ID); err != nil {
		t.Fatal(err)
	}
	second, err := service.Start(context.Background(), Spec{
		Command: "/bin/echo", Args: []string{"now"}, Dir: root,
	}, "d_1")
	if err != nil {
		t.Fatalf("a stopped task must free its slot: %v", err)
	}
	waitFor(t, service, second.ID, StatusExited)
}

func TestTheOutputRingKeepsTheEnd(t *testing.T) {
	service, root, _ := newService(t, func(cfg *Config) { cfg.OutputBytes = 64 })

	task, err := service.Start(context.Background(), Spec{
		Command: "/bin/sh",
		Args:    []string{"-c", "printf 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'; echo END"},
		Dir:     root,
	}, "d_1")
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, service, task.ID, StatusExited)
	output, _ := service.Output(task.ID)
	if len(output) > 64 {
		t.Fatalf("the ring grew to %d bytes", len(output))
	}
	if !strings.Contains(output, "END") {
		t.Fatalf("the end of the output is what someone reads: %q", output)
	}
	final, _ := service.Get(task.ID)
	if !final.Truncated {
		t.Fatal("dropping bytes must be reported")
	}
}

func TestADirectoryOutsideTheWorkspacesIsRefused(t *testing.T) {
	service, _, _ := newService(t, nil)

	_, err := service.Start(context.Background(), Spec{
		Command: "/bin/echo", Dir: "/etc",
	}, "d_1")
	if code := sessions.CodeOf(err); code != "path_escape" {
		t.Fatalf("expected path_escape, got %q (%v)", code, err)
	}
	_, err = service.Start(context.Background(), Spec{Dir: "/etc"}, "d_1")
	if code := sessions.CodeOf(err); code != sessions.CodeBadRequest {
		t.Fatalf("a task without a command is a bad request, got %q", code)
	}
}

func TestListIsNewestFirst(t *testing.T) {
	service, root, _ := newService(t, nil)

	first, err := service.Start(context.Background(), Spec{
		Name: "one", Command: "/bin/echo", Dir: root,
	}, "d_1")
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, service, first.ID, StatusExited)
	second, err := service.Start(context.Background(), Spec{
		Name: "two", Command: "/bin/echo", Dir: root,
	}, "d_1")
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, service, second.ID, StatusExited)

	list := service.List()
	if len(list) != 2 || list[0].Name != "two" {
		t.Fatalf("list = %+v", list)
	}
	if _, ok := service.Get("k_missing"); ok {
		t.Fatal("an unknown id must not be found")
	}
	if _, err := service.Stop("k_missing"); err == nil {
		t.Fatal("an unknown id cannot be stopped")
	}
}

func TestStopAllEndsEveryRunningTask(t *testing.T) {
	service, root, _ := newService(t, nil)

	task, err := service.Start(context.Background(), Spec{
		Command: "/bin/sh", Args: []string{"-c", "sleep 30"}, Dir: root,
	}, "d_1")
	if err != nil {
		t.Fatal(err)
	}
	service.StopAll()
	waitFor(t, service, task.ID, StatusStopped)
	if _, err := os.Stat(filepath.Join(root, "unused")); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func TestTaskWithoutAConfinementIsImpossible(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatal("a runner without a filesystem service must not build")
	}
}
