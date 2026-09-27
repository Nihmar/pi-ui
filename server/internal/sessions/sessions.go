package sessions

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/Nihmar/pi-ui/server/internal/ws"
)

// Spec describes one session to spawn (docs/spike-interfaces.md §5.3).
//
// Command is the full child argv ("pi", "--mode", "rpc", ...); when it is empty the
// supervisor falls back to Config.PiCommand. Name, SessionDir and BridgeExt are turned
// into the per-session flags --name, --session-dir and "-e <path>", and are appended to
// Command only when the caller did not already pass that flag, so a caller that needs
// --provider/--model/--thinking passes them in Command and keeps the rest here.
type Spec struct {
	CWD        string
	Name       string            // display name; also passed to pi as --name
	Command    []string          // full argv for the child; defaults resolved from Config
	Env        map[string]string // extra child environment entries (a later entry wins)
	SessionDir string            // optional --session-dir for the child
	BridgeExt  string            // optional path passed to the child as -e <path>
}

// Status is the lifecycle state of one session. A session stays listed with its final
// status after it exits or crashes (§5.3); only the live states consume a MaxSessions slot.
type Status string

const (
	StatusSpawning   Status = "spawning"
	StatusReady      Status = "ready"
	StatusStreaming  Status = "streaming"
	StatusExited     Status = "exited"
	StatusCrashed    Status = "crashed"
	StatusStopping   Status = "stopping"
	StatusRespawning Status = "respawning"
)

// Live reports whether a session in this state still owns a child process.
func (s Status) Live() bool {
	switch s {
	case StatusSpawning, StatusReady, StatusStreaming, StatusStopping, StatusRespawning:
		return true
	default:
		return false
	}
}

// Info is the session projection: the REST representation and the payload of the
// lifecycle events.
type Info struct {
	ID            string `json:"id"` // server session id, "s_" + 16 hex chars
	CWD           string `json:"cwd"`
	Name          string `json:"name,omitempty"`
	Status        Status `json:"status"`
	PID           int    `json:"pid,omitempty"`
	PiSessionID   string `json:"piSessionId,omitempty"`
	PiSessionFile string `json:"piSessionFile,omitempty"`
	ModelProvider string `json:"modelProvider,omitempty"`
	ModelID       string `json:"modelId,omitempty"`
	ThinkingLevel string `json:"thinkingLevel,omitempty"`
	ExitCode      *int   `json:"exitCode,omitempty"`
	CreatedAt     string `json:"createdAt"`
	LastEventAt   string `json:"lastEventAt,omitempty"`
}

// Config tunes the supervisor. New replaces every zero field with its default, so a
// caller only fills in what it cares about (docs/spike-interfaces.md §5.3).
type Config struct {
	PiCommand  []string // e.g. ["pi","--mode","rpc"]; fake-pi in tests
	SessionDir string   // optional base --session-dir
	BridgeExt  string   // optional -e path
	// MCPConfig is the MCP configuration file the bridge reads, when MCP is
	// configured. Empty means the bridge finds no servers to connect to.
	MCPConfig string
	// Isolation runs every child inside a container. Nil runs them on the host, which is
	// the default and needs no container runtime at all.
	Isolation     *Isolation
	MaxSessions   int           // default 8
	DialogTimeout time.Duration // default 60s
	SendTimeout   time.Duration // default 15s
	PromptLimit   int           // prompts per session per minute (default 30, 0 = off)
	// RespawnAttempts and RespawnWindow bound the automatic restart of a crashed child
	// (default 3 attempts in 10 minutes, 0 attempts disables it).
	RespawnAttempts int
	RespawnWindow   time.Duration
	// RespawnBackoff is the delay before attempt N (1-based); nil uses
	// DefaultRespawnBackoff.
	RespawnBackoff func(attempt int) time.Duration
	// IdleTimeout stops a session that has seen no event for this long (0 disables the
	// watchdog). PLAN.md §4.2: an idle session is wrapped up, not killed silently.
	IdleTimeout time.Duration
	// WrapUpPrompt is what an idle session is asked before it is stopped; empty stops
	// it immediately.
	WrapUpPrompt string
	// WrapUpBudget bounds the wait for the wrap-up turn to settle.
	WrapUpBudget time.Duration
	// WatchInterval is how often the idle watchdog looks (default: a quarter of
	// IdleTimeout, at most a minute).
	WatchInterval time.Duration
	Logger        *slog.Logger
	Hub           Publisher // injected
}

// Publisher is the subset of ws.Hub that sessions needs (injected, never a global).
type Publisher interface {
	Publish(ev ws.Event) uint64
}

// Supervisor is the session seam (docs/spike-interfaces.md §5.3). New returns a
// *Manager, which also implements the hub's CommandHandler, DialogHandler and Replayer.
type Supervisor interface {
	Start(ctx context.Context, spec Spec) (Info, error) // ErrLimit when MaxSessions reached
	Get(id string) (Info, bool)
	List() []Info
	// Send dispatches one op ("prompt", "steer", "abort", "command.raw", ...).
	Send(ctx context.Context, sessionID, op string, payload json.RawMessage) (json.RawMessage, error)
	Stop(ctx context.Context, sessionID string) error // graceful: stdin close, TERM, KILL
	Shutdown(ctx context.Context) error
}
