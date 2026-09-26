package cli

import (
	"context"
	"fmt"
	"io"
	"sort"
	"sync"
)

// Command is one pi-ui subcommand: a value, not a framework. Name and Summary
// feed the usage text, Run receives the arguments that follow the command name
// plus the output writers to use.
type Command struct {
	// Name is the token that selects the command on the command line
	// ("version", "serve", "measure", ...). Unique per process.
	Name string

	// Summary is the one-line description shown by "pi-ui --help" and by the
	// command's own usage output.
	Summary string

	// Run executes the command. Normal output goes to stdout, diagnostics and
	// usage errors to stderr. Returning a non-nil error makes Run print
	// "pi-ui <name>: <error>" on stderr and exit with code 1, or with ExitUsage
	// (2) when the error is (or wraps) a *UsageError: a malformed command line is
	// not the same failure as a run that started and failed.
	//
	// Run must honour ctx cancellation (the process context is cancelled on
	// SIGINT/SIGTERM) and must not write to the real os.Stdout/os.Stderr:
	// only the injected writers.
	Run func(ctx context.Context, args []string, stdout, stderr io.Writer) error
}

// registry holds the registered commands by name. It is written only from
// init() functions before main starts (and from tests), and only read while
// serving, so the mutex is there for the benefit of concurrent tests and for
// the day registration becomes dynamic.
var (
	registryMu sync.RWMutex
	registry   = map[string]Command{}
)

// Register adds a command to the registry and is the only way a subcommand
// reaches the CLI. Call it from an init() in the file that implements the
// command.
//
// Register panics on programmer errors: an empty name or summary, a nil Run,
// or a name that is already taken. These cannot be recovered from usefully at
// start-up, so the process dies with a clear message instead of exposing a
// half-working CLI.
func Register(c Command) {
	switch {
	case c.Name == "":
		panic("cli: command registered with an empty name")
	case c.Summary == "":
		panic(fmt.Sprintf("cli: command %q registered without a summary", c.Name))
	case c.Run == nil:
		panic(fmt.Sprintf("cli: command %q registered with a nil Run", c.Name))
	}

	registryMu.Lock()
	defer registryMu.Unlock()
	if _, dup := registry[c.Name]; dup {
		panic(fmt.Sprintf("cli: command %q registered twice", c.Name))
	}
	registry[c.Name] = c
}

// lookup returns the command registered under name.
func lookup(name string) (Command, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	c, ok := registry[name]
	return c, ok
}

// commandNames returns the registered command names in lexicographic order, so
// usage output does not depend on file/init order.
func commandNames() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
