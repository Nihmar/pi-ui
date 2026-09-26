package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
)

// runFunc matches Command.Run.
type runFunc func(ctx context.Context, args []string, stdout, stderr io.Writer) error

func noopRun(context.Context, []string, io.Writer, io.Writer) error { return nil }

// registerTestCommand registers a command once per process: the registry is
// global and never unregisters, so re-registering under `go test -count=N`
// would panic.
func registerTestCommand(t *testing.T, name, summary string, run runFunc) {
	t.Helper()
	if _, ok := lookup(name); ok {
		return
	}
	Register(Command{Name: name, Summary: summary, Run: run})
}

func TestRun(t *testing.T) {
	registerTestCommand(t, "test_ok", "write to both streams", readArgsRun)
	registerTestCommand(t, "test_fail", "always fails", errorRun)
	registerTestCommand(t, "test_cancel", "returns a wrapped context.Canceled", canceledRun)

	tests := []struct {
		name         string
		args         []string
		wantCode     int
		wantStdout   []string
		wantStderr   []string
		wantEmptyOut bool
		wantEmptyErr bool
	}{
		{
			name:         "no arguments prints usage",
			wantCode:     ExitOK,
			wantStdout:   []string{"usage:", "commands:", "test_ok", "write to both streams"},
			wantEmptyErr: true,
		},
		{
			name:         "-h prints usage",
			args:         []string{"-h"},
			wantCode:     ExitOK,
			wantStdout:   []string{"usage:", "test_ok"},
			wantEmptyErr: true,
		},
		{
			name:         "--help prints usage",
			args:         []string{"--help"},
			wantCode:     ExitOK,
			wantStdout:   []string{"usage:", "test_ok"},
			wantEmptyErr: true,
		},
		{
			name:         "help prints usage",
			args:         []string{"help"},
			wantCode:     ExitOK,
			wantStdout:   []string{"usage:", "test_ok"},
			wantEmptyErr: true,
		},
		{
			name:         "help <command> prints name and summary",
			args:         []string{"help", "test_ok"},
			wantCode:     ExitOK,
			wantStdout:   []string{"test_ok", "write to both streams"},
			wantEmptyErr: true,
		},
		{
			name:         "help <unknown> is a usage error",
			args:         []string{"help", "test_no_such_command"},
			wantCode:     ExitUsage,
			wantStderr:   []string{"pi-ui help: unknown command", `"test_no_such_command"`},
			wantEmptyOut: true,
		},
		{
			name:         "help with extra arguments is a usage error",
			args:         []string{"help", "test_ok", "test_fail"},
			wantCode:     ExitUsage,
			wantStderr:   []string{"at most one command name"},
			wantEmptyOut: true,
		},
		{
			name:         "unknown command",
			args:         []string{"test_no_such_command"},
			wantCode:     ExitUsage,
			wantStderr:   []string{`pi-ui: unknown command "test_no_such_command"`},
			wantEmptyOut: true,
		},
		{
			name:       "command succeeds and receives its arguments",
			args:       []string{"test_ok", "a", "b"},
			wantCode:   ExitOK,
			wantStdout: []string{"stdout:a,b"},
			wantStderr: []string{"stderr:a,b"},
		},
		{
			name:         "command error",
			args:         []string{"test_fail"},
			wantCode:     ExitError,
			wantStderr:   []string{"pi-ui test_fail: boom"},
			wantEmptyOut: true,
		},
		{
			name:         "canceled command",
			args:         []string{"test_cancel"},
			wantCode:     ExitInterrupt,
			wantStderr:   []string{"pi-ui test_cancel: interrupted"},
			wantEmptyOut: true,
		},
		{
			name:         "malformed command line is a usage error",
			args:         []string{"measure"},
			wantCode:     ExitUsage,
			wantStderr:   []string{"pi-ui measure:", "one of --pi or --fake-pi is required"},
			wantEmptyOut: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			got := Run(context.Background(), tt.args, &stdout, &stderr)

			if got != tt.wantCode {
				t.Errorf("Run(%q) = %d, want %d (stdout %q, stderr %q)", tt.args, got, tt.wantCode, stdout.String(), stderr.String())
			}
			if tt.wantEmptyOut && stdout.Len() != 0 {
				t.Errorf("stdout = %q, want it empty", stdout.String())
			}
			if tt.wantEmptyErr && stderr.Len() != 0 {
				t.Errorf("stderr = %q, want it empty", stderr.String())
			}
			for _, want := range tt.wantStdout {
				if !strings.Contains(stdout.String(), want) {
					t.Errorf("stdout = %q, want it to contain %q", stdout.String(), want)
				}

			}
			for _, want := range tt.wantStderr {
				if !strings.Contains(stderr.String(), want) {
					t.Errorf("stderr = %q, want it to contain %q", stderr.String(), want)
				}
			}
		})
	}
}

// TestRunContextReachesCommand pins the seam: the command sees the context Run
// was called with, not a fresh one.
func TestRunContextReachesCommand(t *testing.T) {
	type ctxKey struct{}
	registerTestCommand(t, "test_ctx", "inspect the context", func(ctx context.Context, _ []string, stdout, _ io.Writer) error {
		fmt.Fprintf(stdout, "value:%v\n", ctx.Value(ctxKey{}))
		return nil
	})

	ctx := context.WithValue(context.Background(), ctxKey{}, "carried")
	var stdout, stderr bytes.Buffer
	if got := Run(ctx, []string{"test_ctx"}, &stdout, &stderr); got != ExitOK {
		t.Fatalf("Run = %d, want %d (stderr %q)", got, ExitOK, stderr.String())
	}
	if want := "value:carried\n"; stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestRegisterDuplicatePanics(t *testing.T) {
	const name = "test_register_duplicate"
	registerTestCommand(t, name, "duplicate registration probe", noopRun)

	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("registering %q twice did not panic", name)
		}
		if msg, ok := r.(string); !ok || !strings.Contains(msg, "registered twice") {
			t.Fatalf("panic = %v, want a message containing %q", r, "registered twice")
		}
	}()
	Register(Command{Name: name, Summary: "duplicate registration probe", Run: noopRun})
}

func TestRegisterEmptyNamePanics(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("registering a command with an empty name did not panic")
		}
		if msg, ok := r.(string); !ok || !strings.Contains(msg, "empty name") {
			t.Fatalf("panic = %v, want a message containing %q", r, "empty name")
		}
	}()
	Register(Command{Name: "", Summary: "empty name probe", Run: noopRun})
}

func readArgsRun(_ context.Context, args []string, stdout, stderr io.Writer) error {
	joined := strings.Join(args, ",")
	fmt.Fprintf(stdout, "stdout:%s\n", joined)
	fmt.Fprintf(stderr, "stderr:%s\n", joined)
	return nil
}

func errorRun(context.Context, []string, io.Writer, io.Writer) error {
	return errors.New("boom")
}

func canceledRun(context.Context, []string, io.Writer, io.Writer) error {
	return fmt.Errorf("waiting for the child: %w", context.Canceled)
}
