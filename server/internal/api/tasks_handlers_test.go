package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Nihmar/pi-ui/server/internal/fs"
	"github.com/Nihmar/pi-ui/server/internal/tasks"
)

// newTasksRouter builds a router whose tasks run in a temporary workspace.
func newTasksRouter(t *testing.T) (http.Handler, string) {
	t.Helper()
	root := t.TempDir()
	files, err := fs.New(fs.Config{Roots: []string{root}})
	if err != nil {
		t.Fatal(err)
	}
	service, err := tasks.New(tasks.Config{FS: files})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.StopAll)
	return NewRouter(Options{FS: files, Tasks: service, Auth: NewLoopbackOrToken("")}), root
}

func TestTasksRunAndReport(t *testing.T) {
	handler, root := newTasksRouter(t)

	recorder := do(t, handler, jsonRequest(t, http.MethodPost, "/api/v1/tasks",
		map[string]any{
			"name":    "greet",
			"command": "/bin/sh",
			"args":    []string{"-c", "echo hello-task"},
			"dir":     root,
		}), loopback)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body)
	}
	var started tasks.Task
	if err := json.Unmarshal(recorder.Body.Bytes(), &started); err != nil {
		t.Fatal(err)
	}
	if started.ID == "" || started.Status != tasks.StatusRunning {
		t.Fatalf("started = %+v", started)
	}

	// The listing and the single-task read are the same report, with the output.
	deadline := time.Now().Add(10 * time.Second)
	var output string
	for time.Now().Before(deadline) {
		recorder = do(t, handler, httptest.NewRequest(http.MethodGet,
			"/api/v1/tasks/"+started.ID, nil), loopback)
		if recorder.Code != http.StatusOK {
			t.Fatalf("status %d: %s", recorder.Code, recorder.Body)
		}
		var body struct {
			Task   tasks.Task `json:"task"`
			Output string     `json:"output"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		output = body.Output
		if body.Task.Status != tasks.StatusRunning {
			if body.Task.Status != tasks.StatusExited || body.Task.ExitCode != 0 {
				t.Fatalf("final task %+v", body.Task)
			}
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(output, "hello-task") {
		t.Fatalf("output %q", output)
	}

	recorder = do(t, handler, httptest.NewRequest(http.MethodGet, "/api/v1/tasks", nil), loopback)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), started.ID) {
		t.Fatalf("list: %d %s", recorder.Code, recorder.Body)
	}

	recorder = do(t, handler, httptest.NewRequest(http.MethodGet,
		"/api/v1/tasks/k_missing", nil), loopback)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("unknown task: status %d (%s)", recorder.Code, recorder.Body)
	}
}

func TestStoppingATask(t *testing.T) {
	handler, root := newTasksRouter(t)

	recorder := do(t, handler, jsonRequest(t, http.MethodPost, "/api/v1/tasks",
		map[string]any{"command": "/bin/sh", "args": []string{"-c", "sleep 30"}, "dir": root}), loopback)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body)
	}
	var started tasks.Task
	_ = json.Unmarshal(recorder.Body.Bytes(), &started)

	recorder = do(t, handler, httptest.NewRequest(http.MethodPost,
		"/api/v1/tasks/"+started.ID+"/stop", nil), loopback)
	if recorder.Code != http.StatusOK {
		t.Fatalf("stop: %d %s", recorder.Code, recorder.Body)
	}
	if !strings.Contains(recorder.Body.String(), string(tasks.StatusStopped)) {
		t.Fatalf("stop body %s", recorder.Body)
	}
}

func TestATaskOutsideTheWorkspaceIsRefused(t *testing.T) {
	handler, _ := newTasksRouter(t)

	recorder := do(t, handler, jsonRequest(t, http.MethodPost, "/api/v1/tasks",
		map[string]any{"command": "/bin/echo", "dir": "/etc"}), loopback)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body)
	}
	if !strings.Contains(recorder.Body.String(), "path_escape") {
		t.Fatalf("expected path_escape, got %s", recorder.Body)
	}
}

func TestTasksWithoutAWorkspaceAreUnsupported(t *testing.T) {
	handler := NewRouter(Options{Auth: NewLoopbackOrToken("")})

	recorder := do(t, handler, httptest.NewRequest(http.MethodGet, "/api/v1/tasks", nil), loopback)
	if recorder.Code != http.StatusNotImplemented {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body)
	}
	if recorder = do(t, handler, httptest.NewRequest(http.MethodPost,
		"/api/v1/tasks/k_1/stop", nil), loopback); recorder.Code != http.StatusNotImplemented {
		t.Fatalf("stop: status %d", recorder.Code)
	}
}
