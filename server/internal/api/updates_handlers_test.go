package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Nihmar/pi-ui/server/internal/fs"
	"github.com/Nihmar/pi-ui/server/internal/tasks"
	"github.com/Nihmar/pi-ui/server/internal/updates"
)

// staleChecker answers one component from a table.
type staleChecker struct{ latest map[string]string }

func (c staleChecker) Latest(_ context.Context, component string) (string, error) {
	if value, ok := c.latest[component]; ok {
		return value, nil
	}
	return "", errors.New("nothing published")
}

// newUpdatesRouter builds a router with a fake component table and a temp workspace.
func newUpdatesRouter(t *testing.T, updateCommand string) (http.Handler, string) {
	t.Helper()
	root := t.TempDir()
	files, err := fs.New(fs.Config{Roots: []string{root}})
	if err != nil {
		t.Fatal(err)
	}
	runner, err := tasks.New(tasks.Config{FS: files})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(runner.StopAll)
	service := updates.New(updates.Config{
		Components: []updates.Component{
			{Name: updates.ComponentServer, Current: "1.0.0"},
			{Name: updates.ComponentPi, Current: "0.87.1"},
		},
		Checker: staleChecker{latest: map[string]string{updates.ComponentPi: "0.91.0"}},
	})
	return NewRouter(Options{
		FS:            files,
		Tasks:         runner,
		Updates:       service,
		UpdateCommand: updateCommand,
		Auth:          adminAuth{},
	}), root
}

func TestUpdatesReportWhatIsOutThere(t *testing.T) {
	handler, _ := newUpdatesRouter(t, "")

	recorder := do(t, handler, httptest.NewRequest(http.MethodGet, "/api/v1/updates", nil), loopback)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body)
	}
	var body struct {
		Components []updates.Report `json:"components"`
		Managed    bool             `json:"managed"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Components) != 2 || !body.Managed {
		t.Fatalf("body = %+v", body)
	}
	var pi updates.Report
	for _, report := range body.Components {
		if report.Name == updates.ComponentPi {
			pi = report
		}
	}
	if pi.Latest != "0.91.0" || !pi.UpdateAvailable {
		t.Fatalf("pi = %+v", pi)
	}
}

func TestApplyIsManagedModeWithoutACommand(t *testing.T) {
	handler, _ := newUpdatesRouter(t, "")

	recorder := do(t, handler, httptest.NewRequest(http.MethodPost, "/api/v1/updates/apply", nil), loopback)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body)
	}
	if !strings.Contains(recorder.Body.String(), "managed_mode") {
		t.Fatalf("expected managed_mode, got %s", recorder.Body)
	}
}

func TestApplyRunsTheOperatorsCommand(t *testing.T) {
	// The command is a real one: the point of the endpoint is that the operator's own
	// script runs, and its output becomes the task's output.
	script := filepath.Join(t.TempDir(), "update.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho updating $@\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	handler, _ := newUpdatesRouter(t, script)

	recorder := do(t, handler, adminRequest(http.MethodPost, "/api/v1/updates/apply",
		`{"components":["pi"]}`), loopback)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body)
	}
	var started tasks.Task
	if err := json.Unmarshal(recorder.Body.Bytes(), &started); err != nil {
		t.Fatal(err)
	}
	if started.ID == "" || started.Name != "update" {
		t.Fatalf("task = %+v", started)
	}

	// The task runs the script with the requested components as arguments.
	deadline := time.Now().Add(10 * time.Second)
	var output string
	for time.Now().Before(deadline) {
		recorder = do(t, handler, httptest.NewRequest(http.MethodGet, "/api/v1/tasks/"+started.ID, nil), loopback)
		var body struct {
			Task   tasks.Task `json:"task"`
			Output string     `json:"output"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		output = body.Output
		if body.Task.Status != tasks.StatusRunning {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(output, "updating pi") {
		t.Fatalf("output = %q", output)
	}
}

func TestUpdatesAreAdminOnly(t *testing.T) {
	root := t.TempDir()
	files, err := fs.New(fs.Config{Roots: []string{root}})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewRouter(Options{
		FS:      files,
		Updates: updates.New(updates.Config{Components: []updates.Component{{Name: updates.ComponentServer, Current: "1.0.0"}}}),
		Auth:    NewLoopbackOrToken(""),
	})

	for _, path := range []string{"/api/v1/updates"} {
		recorder := do(t, handler, httptest.NewRequest(http.MethodGet, path, nil), loopback)
		if recorder.Code != http.StatusForbidden {
			t.Fatalf("%s: status %d", path, recorder.Code)
		}
	}
	recorder := do(t, handler, httptest.NewRequest(http.MethodPost, "/api/v1/updates/apply", nil), loopback)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("apply: status %d", recorder.Code)
	}
}

func TestUpdatesWithoutAServiceAreUnsupported(t *testing.T) {
	handler := NewRouter(Options{Auth: adminAuth{}})

	recorder := do(t, handler, httptest.NewRequest(http.MethodGet, "/api/v1/updates", nil), loopback)
	if recorder.Code != http.StatusNotImplemented {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body)
	}
}
